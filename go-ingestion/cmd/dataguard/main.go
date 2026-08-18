package main

import (
	"context"
	"fmt"
	"os"

	"github.com/flipslidersand/dataguard-rail/internal/alert"
	"github.com/flipslidersand/dataguard-rail/internal/config"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/ingester"
	"github.com/flipslidersand/dataguard-rail/internal/pipeline"
	"github.com/flipslidersand/dataguard-rail/internal/server"
	"github.com/flipslidersand/dataguard-rail/internal/store"
	"github.com/flipslidersand/dataguard-rail/internal/telemetry"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func main() {
	root := &cobra.Command{
		Use:   "dataguard",
		Short: "DataGuard Rail — realtime data quality & lineage",
	}
	// グローバルフラグ: OTel / Slack
	root.PersistentFlags().String("otel-endpoint", "", "OTel OTLP エンドポイント（空=stdout）")
	root.PersistentFlags().String("slack-webhook", "", "Slack Incoming Webhook URL（空=通知なし）")
	root.AddCommand(newIngestCmd(), newServeCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newIngestCmd() *cobra.Command {
	var (
		configPath string
		rulesPath  string
		dbPath     string
		engineBin  string
		grpcAddr   string
		tmpDir     string
	)
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest data sources, run quality checks via the Rust engine, persist violations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			otelEndpoint, _ := cmd.Root().PersistentFlags().GetString("otel-endpoint")
			slackWebhook, _ := cmd.Root().PersistentFlags().GetString("slack-webhook")
			return runIngest(configPath, rulesPath, dbPath, engineBin, grpcAddr, tmpDir, otelEndpoint, slackWebhook)
		},
	}
	f := cmd.Flags()
	f.StringVar(&configPath, "config", "sources.yaml", "データソース定義 YAML")
	f.StringVar(&rulesPath, "rules", "rules.yaml", "品質ルール YAML")
	f.StringVar(&dbPath, "db", "data/violations", "BadgerDB のパス")
	f.StringVar(&engineBin, "engine-bin", engine.DefaultBin, "dataguard-engine バイナリのパス（--grpc-addr 未指定時）")
	f.StringVar(&grpcAddr, "grpc-addr", "", "dataguard-engine gRPC アドレス（例: localhost:50051）")
	f.StringVar(&tmpDir, "tmp", "", "一時 CSV の出力先 (既定: OS の一時ディレクトリ)")
	return cmd
}

func runIngest(configPath, rulesPath, dbPath, engineBin, grpcAddr, tmpDir, otelEndpoint, slackWebhook string) error {
	ctx := context.Background()
	log, _ := zap.NewProduction()
	defer func() { _ = log.Sync() }()

	shutdown, err := telemetry.Init(ctx, otelEndpoint)
	if err != nil {
		log.Warn("otel init failed", zap.Error(err))
	} else {
		defer shutdown()
	}

	notifier := alert.NewSlack(slackWebhook)

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	td, err := ingester.TempDir(tmpDir)
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	var runner pipeline.Checker
	if grpcAddr != "" {
		gr, err := engine.NewGrpc(grpcAddr)
		if err != nil {
			return err
		}
		defer gr.Close()
		runner = gr
	} else {
		runner = engine.New(engineBin)
	}

	results, err := pipeline.Run(ctx, cfg, rulesPath, td, pipeline.DefaultLoader{}, runner, st, notifier, log)
	if err != nil {
		return err
	}

	total := 0
	for _, r := range results {
		if r.Skipped {
			fmt.Printf("- %s: skipped (%s)\n", r.Source, r.Reason)
			continue
		}
		total += r.Violations
		fmt.Printf("- %s: %d violation(s)\n", r.Source, r.Violations)
	}
	fmt.Printf("ingest complete: %d violation(s) across %d source(s) → %s\n", total, len(results), dbPath)
	return nil
}

func newServeCmd() *cobra.Command {
	var (
		addr      string
		dbPath    string
		engineBin string
		grpcAddr  string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start REST API server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			otelEndpoint, _ := cmd.Root().PersistentFlags().GetString("otel-endpoint")
			slackWebhook, _ := cmd.Root().PersistentFlags().GetString("slack-webhook")
			return runServe(addr, dbPath, engineBin, grpcAddr, otelEndpoint, slackWebhook)
		},
	}
	f := cmd.Flags()
	f.StringVar(&addr, "addr", ":8080", "リッスンアドレス")
	f.StringVar(&dbPath, "db", "data/violations", "BadgerDB のパス")
	f.StringVar(&engineBin, "engine-bin", engine.DefaultBin, "dataguard-engine バイナリのパス（--grpc-addr 未指定時）")
	f.StringVar(&grpcAddr, "grpc-addr", "", "dataguard-engine gRPC アドレス（例: localhost:50051）")
	return cmd
}

func runServe(addr, dbPath, engineBin, grpcAddr, otelEndpoint, slackWebhook string) error {
	ctx := context.Background()
	log, _ := zap.NewProduction()
	defer func() { _ = log.Sync() }()

	shutdown, err := telemetry.Init(ctx, otelEndpoint)
	if err != nil {
		log.Warn("otel init failed", zap.Error(err))
	} else {
		defer shutdown()
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	var runner server.Runner
	if grpcAddr != "" {
		gr, err := engine.NewGrpc(grpcAddr)
		if err != nil {
			return err
		}
		defer gr.Close()
		runner = gr
	} else {
		runner = engine.New(engineBin)
	}

	notifier := alert.NewSlack(slackWebhook)
	srv := server.New(st, runner, notifier)

	log.Info("starting server", zap.String("addr", addr))
	return srv.Run(addr)
}
