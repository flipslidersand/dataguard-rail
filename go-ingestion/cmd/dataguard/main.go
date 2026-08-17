package main

import (
	"context"
	"fmt"
	"os"

	"github.com/flipslidersand/dataguard-rail/internal/config"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/ingester"
	"github.com/flipslidersand/dataguard-rail/internal/pipeline"
	"github.com/flipslidersand/dataguard-rail/internal/store"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func main() {
	root := &cobra.Command{
		Use:   "dataguard",
		Short: "DataGuard Rail — realtime data quality & lineage",
	}
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
		tmpDir     string
	)
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest data sources, run quality checks via the Rust engine, persist violations",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runIngest(configPath, rulesPath, dbPath, engineBin, tmpDir)
		},
	}
	f := cmd.Flags()
	f.StringVar(&configPath, "config", "sources.yaml", "データソース定義 YAML")
	f.StringVar(&rulesPath, "rules", "rules.yaml", "品質ルール YAML")
	f.StringVar(&dbPath, "db", "data/violations", "BadgerDB のパス")
	f.StringVar(&engineBin, "engine-bin", engine.DefaultBin, "dataguard-engine バイナリのパス")
	f.StringVar(&tmpDir, "tmp", "", "一時 CSV の出力先 (既定: OS の一時ディレクトリ)")
	return cmd
}

func runIngest(configPath, rulesPath, dbPath, engineBin, tmpDir string) error {
	log, _ := zap.NewProduction()
	defer func() { _ = log.Sync() }()

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

	runner := engine.New(engineBin)

	results, err := pipeline.Run(context.Background(), cfg, rulesPath, td, pipeline.DefaultLoader{}, runner, st, log)
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
	return &cobra.Command{
		Use:   "serve",
		Short: "Start REST API server (Phase 4)",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("serve — not yet implemented (Phase 4)")
			return nil
		},
	}
}
