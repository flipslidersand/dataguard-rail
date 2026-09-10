package engine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/flipslidersand/dataguard-rail/internal/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// GrpcRunner は dataguard-engine gRPC サーバーに接続するクライアント。
// exec 版の Runner と同じ interface を満たす。
type GrpcRunner struct {
	conn   *grpc.ClientConn
	client pb.DataGuardClient
}

// GrpcTLSConfig は Go↔Rust gRPC チャネルの TLS/mTLS 設定。
// CAFile が空の場合は TLS を要求しない（ループバック外では Insecure が明示されない限り拒否する）。
type GrpcTLSConfig struct {
	CAFile   string // サーバー証明書を検証する CA 証明書 (PEM)
	CertFile string // mTLS 用クライアント証明書 (PEM、任意)
	KeyFile  string // mTLS 用クライアント秘密鍵 (PEM、任意)
	Insecure bool   // ループバック外でも平文通信を明示的に許可する
}

// NewGrpc は addr の gRPC サーバーに接続する。
func NewGrpc(addr string, tlsCfg GrpcTLSConfig) (*GrpcRunner, error) {
	creds, err := buildTransportCredentials(addr, tlsCfg)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %q: %w", addr, err)
	}
	return &GrpcRunner{conn: conn, client: pb.NewDataGuardClient(conn)}, nil
}

// buildTransportCredentials は tlsCfg から gRPC 用の TransportCredentials を組み立てる。
// CAFile 未指定（TLS 無効）でループバック外の addr かつ Insecure でない場合はエラーを返し、
// 平文で中間者攻撃に晒される接続を拒否する。
func buildTransportCredentials(addr string, tlsCfg GrpcTLSConfig) (credentials.TransportCredentials, error) {
	if tlsCfg.CAFile == "" {
		if !isLoopbackAddr(addr) && !tlsCfg.Insecure {
			return nil, fmt.Errorf(
				"refusing plaintext gRPC connection to non-loopback addr %q: "+
					"specify --grpc-tls-ca or pass --grpc-insecure to opt in explicitly", addr)
		}
		warnIfNotLoopback(addr)
		return insecure.NewCredentials(), nil
	}

	caPEM, err := os.ReadFile(tlsCfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read --grpc-tls-ca %q: %w", tlsCfg.CAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("--grpc-tls-ca %q contains no valid PEM certificate", tlsCfg.CAFile)
	}
	tlsConf := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}

	if tlsCfg.CertFile != "" || tlsCfg.KeyFile != "" {
		if tlsCfg.CertFile == "" || tlsCfg.KeyFile == "" {
			return nil, fmt.Errorf("--grpc-tls-cert and --grpc-tls-key must be set together for mTLS")
		}
		cert, err := tls.LoadX509KeyPair(tlsCfg.CertFile, tlsCfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		tlsConf.Certificates = []tls.Certificate{cert}
	}

	return credentials.NewTLS(tlsConf), nil
}

// warnIfNotLoopback はループバック外へ接続しようとした際に stderr へ警告を出す。
// この gRPC チャネルは TLS 未対応の平文通信であり、信頼できないネットワーク上での
// 中間者攻撃（csv_path/sql_path 改ざん、violations_json 偽装）を許してしまうため。
func warnIfNotLoopback(addr string) {
	if isLoopbackAddr(addr) {
		return
	}
	fmt.Fprintf(os.Stderr,
		"WARNING: --grpc-addr %q はループバック外です。この gRPC チャネルは TLS 未対応の平文通信です。"+
			"信頼できないネットワークを経由させず、SSH トンネル等で保護してください。\n", addr)
}

// isLoopbackAddr は host:port もしくは host 単体の addr がループバックを指すか判定する。
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Close は gRPC 接続を閉じる。
func (g *GrpcRunner) Close() error {
	return g.conn.Close()
}

// Analyze は Rust engine の analyze RPC を呼び出し、リネージュ JSON を返す。
func (g *GrpcRunner) Analyze(ctx context.Context, sqlPath string) (json.RawMessage, error) {
	resp, err := g.client.Analyze(ctx, &pb.AnalyzeRequest{SqlPath: sqlPath})
	if err != nil {
		return nil, fmt.Errorf("grpc Analyze: %w", err)
	}
	return json.RawMessage(resp.LineageJson), nil
}

// Check は Rust engine の check RPC を呼び出し、violations を返す。
func (g *GrpcRunner) Check(ctx context.Context, csvPath, rulesPath string) ([]Violation, error) {
	resp, err := g.client.Check(ctx, &pb.CheckRequest{
		CsvPath:   csvPath,
		RulesPath: rulesPath,
	})
	if err != nil {
		return nil, fmt.Errorf("grpc Check: %w", err)
	}
	var vs []Violation
	if err := json.Unmarshal([]byte(resp.ViolationsJson), &vs); err != nil {
		return nil, fmt.Errorf("unmarshal violations: %w", err)
	}
	return vs, nil
}
