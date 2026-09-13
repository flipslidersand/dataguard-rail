package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/flipslidersand/dataguard-rail/internal/config"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/ingester"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// fakeChecker は与えられた CSV パスの存在を確認し、固定の violation を返す。
type fakeChecker struct{ lastInput string }

func (f *fakeChecker) Check(_ context.Context, inputCSV, _ string) ([]engine.Violation, error) {
	f.lastInput = inputCSV
	if _, err := os.Stat(inputCSV); err != nil {
		return nil, err
	}
	return []engine.Violation{{ID: "viol-1", Rule: "r", Table: "products", Row: 1, Column: "c", Value: "-1"}}, nil
}

type fakeSaver struct{ saved []engine.Violation }

func (f *fakeSaver) SaveViolations(vs []engine.Violation) error {
	f.saved = append(f.saved, vs...)
	return nil
}

// fakeLoader は DB 無しで postgres 経路を検証するためのローダ。
type fakeLoader struct{ ds *ingester.Dataset }

func (f fakeLoader) Load(_ context.Context, _ config.DataSource) (*ingester.Dataset, error) {
	return f.ds, nil
}

func TestRunCSVSource(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "products.csv")
	if err := os.WriteFile(csvPath, []byte("id,price\n1,-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Sources: []config.DataSource{
		{Name: "products", Type: config.CSV, Path: csvPath},
	}}
	chk := &fakeChecker{}
	saver := &fakeSaver{}

	// loader=nil → DefaultLoader (実 CSV 読込み)
	results, err := Run(context.Background(), cfg, "rules.yaml", dir, nil, chk, saver, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Violations != 1 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if len(saver.saved) != 1 {
		t.Errorf("want 1 saved violation, got %d", len(saver.saved))
	}
	// engine には元ファイルではなく一時 CSV が渡る (postgres と共通経路)。
	if chk.lastInput == csvPath {
		t.Errorf("checker should receive a temp csv, got original path")
	}
}

func TestRunPostgresSource(t *testing.T) {
	cfg := &config.Config{Sources: []config.DataSource{
		{Name: "orders", Type: config.Postgres, DSN: "x", Query: "SELECT 1"},
	}}
	loader := fakeLoader{ds: &ingester.Dataset{
		Headers: []string{"id", "price"},
		Rows:    [][]string{{"1", "-1"}, {"2", "5"}},
	}}
	chk := &fakeChecker{}
	saver := &fakeSaver{}

	results, err := Run(context.Background(), cfg, "rules.yaml", t.TempDir(), loader, chk, saver, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Violations != 1 {
		t.Errorf("postgres source should flow to checker: %+v", results)
	}
	if len(saver.saved) != 1 {
		t.Errorf("want 1 saved violation, got %d", len(saver.saved))
	}
}

// failingNotifier は常にエラーを返す alert.Notifier のテスト実装。
type failingNotifier struct{}

func (failingNotifier) Notify(_ context.Context, _ string) error {
	return errors.New("webhook unreachable")
}

// TestRunLogsNotifyFailure は通知失敗が握りつぶされず log.Warn に記録されることを確認する。
func TestRunLogsNotifyFailure(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "products.csv")
	if err := os.WriteFile(csvPath, []byte("id,price\n1,-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Sources: []config.DataSource{
		{Name: "products", Type: config.CSV, Path: csvPath},
	}}

	core, logs := observer.New(zap.WarnLevel)
	log := zap.New(core)

	results, err := Run(context.Background(), cfg, "rules.yaml", dir, nil, &fakeChecker{}, &fakeSaver{}, failingNotifier{}, log)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Violations != 1 {
		t.Fatalf("unexpected results: %+v", results)
	}

	entries := logs.FilterMessage("notify failed").All()
	if len(entries) != 1 {
		t.Fatalf("want 1 'notify failed' warn log, got %d: %+v", len(entries), logs.All())
	}
}

// failingLoader は常にエラーを返す Loader のテスト実装。
type failingLoader struct{ err error }

func (f failingLoader) Load(_ context.Context, _ config.DataSource) (*ingester.Dataset, error) {
	return nil, f.err
}

// multiLoader は source 名で振り分ける Loader のテスト実装。
type multiLoader struct{ byName map[string]Loader }

func (m multiLoader) Load(ctx context.Context, src config.DataSource) (*ingester.Dataset, error) {
	return m.byName[src.Name].Load(ctx, src)
}

// TestRunContinuesAfterSourceFailure は #135: 1ソースが失敗しても後続ソースの
// 処理を継続し、成功分は Result に残しつつ失敗は Result.Err と複合エラーの
// 両方に反映されることを確認する。
func TestRunContinuesAfterSourceFailure(t *testing.T) {
	okDS := &ingester.Dataset{Headers: []string{"id", "price"}, Rows: [][]string{{"1", "-1"}}}
	loadErr := errors.New("connection refused")

	cfg := &config.Config{Sources: []config.DataSource{
		{Name: "broken", Type: config.Postgres, DSN: "x", Query: "SELECT 1"},
		{Name: "healthy", Type: config.Postgres, DSN: "x", Query: "SELECT 1"},
	}}
	loader := multiLoader{byName: map[string]Loader{
		"broken":  failingLoader{err: loadErr},
		"healthy": fakeLoader{ds: okDS},
	}}
	chk := &fakeChecker{}
	saver := &fakeSaver{}

	results, err := Run(context.Background(), cfg, "rules.yaml", t.TempDir(), loader, chk, saver, nil, nil)

	if err == nil {
		t.Fatal("Run should return a non-nil error when a source fails")
	}
	if !errors.Is(err, loadErr) {
		t.Errorf("Run error should wrap the underlying load error, got: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results (failed + succeeded), got %d: %+v", len(results), results)
	}
	if results[0].Source != "broken" || results[0].Err == nil {
		t.Errorf("first result should record the failure: %+v", results[0])
	}
	if results[1].Source != "healthy" || results[1].Err != nil || results[1].Violations != 1 {
		t.Errorf("second source should still be processed successfully: %+v", results[1])
	}
	if len(saver.saved) != 1 {
		t.Errorf("want 1 saved violation from the healthy source, got %d", len(saver.saved))
	}
}
