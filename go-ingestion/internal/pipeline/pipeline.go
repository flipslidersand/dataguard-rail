// Package pipeline は ingest の中核フローを配線する:
// config → source 読み込み → 一時 CSV → engine.Check(exec) → store 永続化。
package pipeline

import (
	"fmt"

	"github.com/flipslidersand/dataguard-rail/internal/config"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/ingester"
	"go.uber.org/zap"
)

// Checker は品質チェックの実行者 (engine.Runner が満たす)。テストで差し替え可能。
type Checker interface {
	Check(inputCSV, rulesPath string) ([]engine.Violation, error)
}

// Saver は violations の永続化先 (store.Store が満たす)。
type Saver interface {
	SaveViolations([]engine.Violation) error
}

// Result はソースごとの取込み結果。
type Result struct {
	Source     string
	Violations int
	Skipped    bool
	Reason     string
}

// Run は全 DataSource を順に処理し、ソースごとの結果を返す。
// CSV ソースを処理し、PostgreSQL は #10 実装までスキップする。
func Run(cfg *config.Config, rulesPath, tmpDir string, chk Checker, saver Saver, log *zap.Logger) ([]Result, error) {
	if log == nil {
		log = zap.NewNop()
	}
	results := make([]Result, 0, len(cfg.Sources))

	for _, src := range cfg.Sources {
		if src.Type != config.CSV {
			log.Warn("skip non-csv source (未実装)", zap.String("source", src.Name), zap.String("type", string(src.Type)))
			results = append(results, Result{Source: src.Name, Skipped: true, Reason: "postgres は #10 で対応"})
			continue
		}

		n, err := processCSV(src, rulesPath, tmpDir, chk, saver)
		if err != nil {
			return results, fmt.Errorf("source %q: %w", src.Name, err)
		}
		log.Info("ingested", zap.String("source", src.Name), zap.Int("violations", n))
		results = append(results, Result{Source: src.Name, Violations: n})
	}
	return results, nil
}

// processCSV は 1 つの CSV ソースを取込み、チェックし、保存して violation 数を返す。
func processCSV(src config.DataSource, rulesPath, tmpDir string, chk Checker, saver Saver) (int, error) {
	ds, err := ingester.LoadCSV(src.Path)
	if err != nil {
		return 0, err
	}
	csvPath, cleanup, err := ingester.WriteTempCSV(ds, tmpDir)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	violations, err := chk.Check(csvPath, rulesPath)
	if err != nil {
		return 0, err
	}
	if err := saver.SaveViolations(violations); err != nil {
		return 0, err
	}
	return len(violations), nil
}
