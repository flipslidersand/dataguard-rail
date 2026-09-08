package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/flipslidersand/dataguard-rail/internal/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GrpcRunner は dataguard-engine gRPC サーバーに接続するクライアント。
// exec 版の Runner と同じ interface を満たす。
type GrpcRunner struct {
	conn   *grpc.ClientConn
	client pb.DataGuardClient
}

// NewGrpc は addr の gRPC サーバーに接続する。
func NewGrpc(addr string) (*GrpcRunner, error) {
	warnIfNotLoopback(addr)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %q: %w", addr, err)
	}
	return &GrpcRunner{conn: conn, client: pb.NewDataGuardClient(conn)}, nil
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
