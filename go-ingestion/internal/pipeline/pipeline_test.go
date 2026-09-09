package pipeline

import (
	"context"
	"fmt"
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

// failingNotifier は常にエラーを返す Notifier。
type failingNotifier struct{ err error }

func (f failingNotifier) Notify(_ context.Context, _ string) error { return f.err }

// TestRunLogsNotifyFailure は notifier.Notify が失敗した際、Run がそのエラーを
// 握りつぶさずログに記録することを確認する（#88）。
func TestRunLogsNotifyFailure(t *testing.T) {
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
	notifyErr := fmt.Errorf("slack webhook: 429 too many requests")
	notifier := failingNotifier{err: notifyErr}

	core, logs := observer.New(zap.WarnLevel)
	log := zap.New(core)

	results, err := Run(context.Background(), cfg, "rules.yaml", dir, nil, chk, saver, notifier, log)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Violations != 1 {
		t.Fatalf("unexpected results: %+v", results)
	}

	entries := logs.FilterMessage("notify failed").All()
	if len(entries) != 1 {
		t.Fatalf("want 1 'notify failed' log entry, got %d: %+v", len(entries), logs.All())
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
