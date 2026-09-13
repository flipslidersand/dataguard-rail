package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/flipslidersand/dataguard-rail/internal/alert"
	"github.com/flipslidersand/dataguard-rail/internal/config"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/scheduler"
	"github.com/flipslidersand/dataguard-rail/internal/ingester"
	"github.com/flipslidersand/dataguard-rail/internal/pipeline"
	"github.com/flipslidersand/dataguard-rail/internal/server"
	"github.com/flipslidersand/dataguard-rail/internal/store"
	"github.com/flipslidersand/dataguard-rail/internal/telemetry"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

// addGrpcTLSFlags は Go↔Rust gRPC チャネルの TLS/mTLS フラグを登録する。
// ingest / serve の両サブコマンドで共通。
func addGrpcTLSFlags(f *pflag.FlagSet, tlsCfg *engine.GrpcTLSConfig) {
	f.StringVar(&tlsCfg.CAFile, "grpc-tls-ca", "", "dataguard-engine サーバー証明書を検証する CA 証明書 (PEM)")
	f.StringVar(&tlsCfg.CertFile, "grpc-tls-cert", "", "mTLS 用クライアント証明書 (PEM、--grpc-tls-key と併用)")
	f.StringVar(&tlsCfg.KeyFile, "grpc-tls-key", "", "mTLS 用クライアント秘密鍵 (PEM、--grpc-tls-cert と併用)")
	f.BoolVar(&tlsCfg.Insecure, "grpc-insecure", false, "ループバック外でも TLS なし平文通信を明示的に許可する（非推奨）")
}

func newIngestCmd() *cobra.Command {
	var (
		configPath string
		rulesPath  string
		dbPath     string
		engineBin  string
		grpcAddr   string
		tmpDir     string
		daemon     bool
		grpcTLS    engine.GrpcTLSConfig
	)
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest data sources, run quality checks via the Rust engine, persist violations",
		Long: `sources.yaml のデータソースを読み込み、rules.yaml の品質ルールを Rust engine
（exec または --grpc-addr 指定時は gRPC）で評価し、violation を BadgerDB へ保存する。
違反が検知されると Slack Webhook（--slack-webhook / グローバルフラグ）へ通知する。

--daemon を付けると、schedule フィールドを持つソースを cron で定期実行するデーモン
モードになる（schedule なしのソースは起動時に一度だけ即時実行される）。--daemon が
無い場合は全ソースを一度だけ実行して終了する。`,
		Example: `  dataguard ingest --config sources.yaml --rules rules.yaml
  dataguard ingest --config sources.yaml --rules rules.yaml --daemon
  dataguard ingest --config sources.yaml --rules rules.yaml --grpc-addr localhost:50051`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			otelEndpoint, _ := cmd.Root().PersistentFlags().GetString("otel-endpoint")
			slackWebhook, _ := cmd.Root().PersistentFlags().GetString("slack-webhook")
			if daemon {
				return runDaemon(configPath, rulesPath, dbPath, engineBin, grpcAddr, tmpDir, otelEndpoint, slackWebhook, grpcTLS)
			}
			return runIngest(configPath, rulesPath, dbPath, engineBin, grpcAddr, tmpDir, otelEndpoint, slackWebhook, grpcTLS)
		},
	}
	f := cmd.Flags()
	f.StringVar(&configPath, "config", "sources.yaml", "データソース定義 YAML")
	f.StringVar(&rulesPath, "rules", "rules.yaml", "品質ルール YAML")
	f.StringVar(&dbPath, "db", "data/violations", "BadgerDB のパス")
	f.StringVar(&engineBin, "engine-bin", engine.DefaultBin, "dataguard-engine バイナリのパス（--grpc-addr 未指定時）")
	f.StringVar(&grpcAddr, "grpc-addr", "", "dataguard-engine gRPC アドレス（例: localhost:50051）")
	f.StringVar(&tmpDir, "tmp", "", "一時 CSV の出力先 (既定: OS の一時ディレクトリ)")
	f.BoolVar(&daemon, "daemon", false, "sources.yaml の schedule に従って定期実行するデーモンモード")
	addGrpcTLSFlags(f, &grpcTLS)
	return cmd
}

func runIngest(configPath, rulesPath, dbPath, engineBin, grpcAddr, tmpDir, otelEndpoint, slackWebhook string, grpcTLS engine.GrpcTLSConfig) error {
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
		gr, err := engine.NewGrpc(grpcAddr, grpcTLS)
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

func runDaemon(configPath, rulesPath, dbPath, engineBin, grpcAddr, tmpDir, otelEndpoint, slackWebhook string, grpcTLS engine.GrpcTLSConfig) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
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
		gr, err := engine.NewGrpc(grpcAddr, grpcTLS)
		if err != nil {
			return err
		}
		defer gr.Close()
		runner = gr
	} else {
		runner = engine.New(engineBin)
	}

	jobRunner := scheduler.BuildRunner(rulesPath, td, pipeline.DefaultLoader{}, runner, st, notifier, log)

	// schedule なしのソースを即時実行
	var onDemand []config.DataSource
	for _, src := range cfg.Sources {
		if src.Schedule == "" {
			onDemand = append(onDemand, src)
		}
	}
	if len(onDemand) > 0 {
		immediateCfg := &config.Config{Sources: onDemand}
		if _, err := pipeline.Run(ctx, immediateCfg, rulesPath, td, pipeline.DefaultLoader{}, runner, st, notifier, log); err != nil {
			log.Warn("immediate ingest failed", zap.Error(err))
		}
	}

	sched := scheduler.New(ctx, log)
	for _, src := range cfg.Sources {
		if err := sched.Register(src, jobRunner); err != nil {
			return err
		}
	}

	if !sched.HasJobs() {
		log.Info("no scheduled sources found — exiting (use schedule field in sources.yaml for daemon mode)")
		return nil
	}

	sched.Start()
	log.Info("scheduler started — waiting for SIGINT/SIGTERM")

	<-ctx.Done()

	log.Info("shutting down scheduler…")
	sched.Stop()
	return nil
}

func newServeCmd() *cobra.Command {
	var (
		addr      string
		dbPath    string
		engineBin string
		grpcAddr  string
		apiKey    string
		grpcTLS   engine.GrpcTLSConfig
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start REST API server",
		Long: `violations・schema-diff・lineage を閲覧できる REST API + Web ダッシュボード
（GET /）を起動する。バックエンドの解析は --grpc-addr 指定時は常駐 gRPC サーバー、
未指定時は engine バイナリの都度 exec で行う。

--api-key は必須。未指定のまま起動すると（環境変数 DATAGUARD_ALLOW_INSECURE=1 を
明示しない限り）起動を拒否する — /api と / の無認証公開を防ぐため。`,
		Example: `  dataguard serve --addr :8080 --api-key "$(gopass show -o infra/dataguard-rail/api-key)"
  dataguard serve --addr :8080 --grpc-addr localhost:50051 --api-key "$API_KEY"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			otelEndpoint, _ := cmd.Root().PersistentFlags().GetString("otel-endpoint")
			slackWebhook, _ := cmd.Root().PersistentFlags().GetString("slack-webhook")
			return runServe(addr, dbPath, engineBin, grpcAddr, otelEndpoint, slackWebhook, apiKey, grpcTLS)
		},
	}
	f := cmd.Flags()
	f.StringVar(&addr, "addr", ":8080", "リッスンアドレス")
	f.StringVar(&dbPath, "db", "data/violations", "BadgerDB のパス")
	f.StringVar(&engineBin, "engine-bin", engine.DefaultBin, "dataguard-engine バイナリのパス（--grpc-addr 未指定時）")
	f.StringVar(&grpcAddr, "grpc-addr", "", "dataguard-engine gRPC アドレス（例: localhost:50051）")
	f.StringVar(&apiKey, "api-key", "", "API 認証キー（未指定=認証無効。本番運用では必須）")
	addGrpcTLSFlags(f, &grpcTLS)
	return cmd
}

// checkAPIKeyRequirement は apiKey 未指定時の無認証起動を明示的なオプトインなしには許可しない。
// allowInsecureEnv には DATAGUARD_ALLOW_INSECURE 環境変数の値を渡す ("1" のみ許可)。
func checkAPIKeyRequirement(apiKey, allowInsecureEnv string) error {
	if apiKey != "" {
		return nil
	}
	if allowInsecureEnv != "1" {
		return fmt.Errorf(
			"--api-key が未指定です。無認証で violations/lineage/schema-diff が公開されます。" +
				"意図的な場合は環境変数 DATAGUARD_ALLOW_INSECURE=1 を設定して起動してください",
		)
	}
	return nil
}

func runServe(addr, dbPath, engineBin, grpcAddr, otelEndpoint, slackWebhook, apiKey string, grpcTLS engine.GrpcTLSConfig) error {
	ctx := context.Background()
	log, _ := zap.NewProduction()
	defer func() { _ = log.Sync() }()

	if err := checkAPIKeyRequirement(apiKey, os.Getenv("DATAGUARD_ALLOW_INSECURE")); err != nil {
		return err
	}
	if apiKey == "" {
		log.Warn("DATAGUARD_ALLOW_INSECURE=1 のため --api-key 未指定のまま起動します。/api と / は認証なしで公開されます")
	}

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
		gr, err := engine.NewGrpc(grpcAddr, grpcTLS)
		if err != nil {
			return err
		}
		defer gr.Close()
		runner = gr
	} else {
		runner = engine.New(engineBin)
	}

	notifier := alert.NewSlack(slackWebhook)
	srv := server.New(st, runner, notifier, apiKey, log)

	log.Info("starting server", zap.String("addr", addr))
	return srv.Run(addr)
}
