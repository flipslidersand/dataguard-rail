// Package pipeline は ingest の中核フローを配線する:
// config → source 読み込み → 一時 CSV → engine.Check(exec) → store 永続化。
package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/flipslidersand/dataguard-rail/internal/alert"
	"github.com/flipslidersand/dataguard-rail/internal/config"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/ingester"
	"github.com/flipslidersand/dataguard-rail/internal/telemetry"
	"go.uber.org/zap"
)

// Checker は品質チェックの実行者 (engine.Runner が満たす)。テストで差し替え可能。
type Checker interface {
	Check(ctx context.Context, inputCSV, rulesPath string) ([]engine.Violation, error)
}

// Saver は violations の永続化先 (store.Store が満たす)。
type Saver interface {
	SaveViolations([]engine.Violation) error
}

// Loader は DataSource を Dataset に読み込む。DB 接続を伴う postgres を
// テストで差し替えられるよう interface 化している。
type Loader interface {
	Load(ctx context.Context, src config.DataSource) (*ingester.Dataset, error)
}

// DefaultLoader は type に応じて CSV / PostgreSQL を読み込む本番用ローダ。
type DefaultLoader struct{}

// Load は src.Type に応じて Dataset を返す。
func (DefaultLoader) Load(ctx context.Context, src config.DataSource) (*ingester.Dataset, error) {
	switch src.Type {
	case config.CSV:
		return ingester.LoadCSV(src.Path)
	case config.JSONL:
		return ingester.LoadJSONL(src.Path)
	case config.Postgres:
		return ingester.LoadPostgres(ctx, src.DSN, src.Query, src.EffectiveQueryTimeout())
	default:
		return nil, fmt.Errorf("未対応の source type %q", src.Type)
	}
}

// Result はソースごとの取込み結果。Err が非nilの場合、そのソースの処理は
// 失敗しており Violations は 0 のまま意味を持たない。
type Result struct {
	Source     string
	Violations int
	Skipped    bool
	Reason     string
	Err        error
}

// Run は全 DataSource を順に処理し、ソースごとの結果を返す。
// 1つのソースが失敗しても残りのソースの処理は継続し、失敗は各 Result.Err に
// 記録した上で、全ソース処理後に errors.Join した複合エラーを返す
// （呼び出し元は err != nil でも results を破棄せず先行成功分を利用できる）。
func Run(ctx context.Context, cfg *config.Config, rulesPath, tmpDir string, loader Loader, chk Checker, saver Saver, notifier alert.Notifier, log *zap.Logger) ([]Result, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if loader == nil {
		loader = DefaultLoader{}
	}
	if notifier == nil {
		notifier = alert.NoopNotifier{}
	}
	results := make([]Result, 0, len(cfg.Sources))
	var errs []error

	for _, src := range cfg.Sources {
		n, err := process(ctx, src, rulesPath, tmpDir, loader, chk, saver)
		if err != nil {
			wrapped := fmt.Errorf("source %q: %w", src.Name, err)
			log.Error("ingest failed", zap.String("source", src.Name), zap.Error(err))
			errs = append(errs, wrapped)
			results = append(results, Result{Source: src.Name, Err: wrapped})
			continue
		}
		log.Info("ingested", zap.String("source", src.Name), zap.String("type", string(src.Type)), zap.Int("violations", n))
		telemetry.RecordIngest(ctx, src.Name, string(src.Type), int64(n))
		if n > 0 {
			if err := notifier.Notify(ctx, fmt.Sprintf("[dataguard] %s: %d violation(s) detected", src.Name, n)); err != nil {
				log.Warn("notify failed", zap.String("source", src.Name), zap.Error(err))
			}
		}
		results = append(results, Result{Source: src.Name, Violations: n})
	}
	return results, errors.Join(errs...)
}

// process は 1 ソースを読み込み→一時 CSV→チェック→保存し violation 数を返す。
func process(ctx context.Context, src config.DataSource, rulesPath, tmpDir string, loader Loader, chk Checker, saver Saver) (int, error) {
	ds, err := loader.Load(ctx, src)
	if err != nil {
		return 0, err
	}
	csvPath, cleanup, err := ingester.WriteTempCSV(ds, tmpDir)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	violations, err := chk.Check(ctx, csvPath, rulesPath)
	if err != nil {
		return 0, err
	}
	if err := saver.SaveViolations(violations); err != nil {
		return 0, err
	}
	return len(violations), nil
}
