package engine

import (
	"context"
	"encoding/json"
	"fmt"

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
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %q: %w", addr, err)
	}
	return &GrpcRunner{conn: conn, client: pb.NewDataGuardClient(conn)}, nil
}

// Close は gRPC 接続を閉じる。
func (g *GrpcRunner) Close() error {
	return g.conn.Close()
}

// Analyze は Rust engine の analyze RPC を呼び出し、リネージュ JSON を返す。
func (g *GrpcRunner) Analyze(sqlPath string) (json.RawMessage, error) {
	resp, err := g.client.Analyze(context.Background(), &pb.AnalyzeRequest{SqlPath: sqlPath})
	if err != nil {
		return nil, fmt.Errorf("grpc Analyze: %w", err)
	}
	return json.RawMessage(resp.LineageJson), nil
}

// Check は Rust engine の check RPC を呼び出し、violations を返す。
func (g *GrpcRunner) Check(csvPath, rulesPath string) ([]Violation, error) {
	resp, err := g.client.Check(context.Background(), &pb.CheckRequest{
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
