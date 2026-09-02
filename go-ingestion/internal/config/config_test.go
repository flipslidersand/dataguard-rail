package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sources.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadCSVAndPostgres(t *testing.T) {
	p := writeTemp(t, `
sources:
  - name: products_csv
    type: csv
    path: ./data/products.csv
    schedule: "0 * * * *"
  - name: orders_db
    type: postgres
    dsn: "postgres://u:p@localhost/shop"
    query: "SELECT * FROM orders"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("want 2 sources, got %d", len(cfg.Sources))
	}
	if cfg.Sources[0].Type != CSV || cfg.Sources[0].Path != "./data/products.csv" {
		t.Errorf("csv source mismatch: %+v", cfg.Sources[0])
	}
	if cfg.Sources[1].Type != Postgres || cfg.Sources[1].Query == "" {
		t.Errorf("postgres source mismatch: %+v", cfg.Sources[1])
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"empty":            "sources: []",
		"csv missing path": "sources:\n  - name: x\n    type: csv\n",
		"pg missing dsn":   "sources:\n  - name: x\n    type: postgres\n    query: q\n",
		"unknown type":     "sources:\n  - name: x\n    type: kafka\n",
		"no name":          "sources:\n  - type: csv\n    path: p\n",
		"bad schedule":     "sources:\n  - name: x\n    type: csv\n    path: p\n    schedule: \"not-a-cron\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

func TestLoadValidSchedule(t *testing.T) {
	// schedule が正しい cron 書式であれば Load が成功することを確認する。
	p := writeTemp(t, `
sources:
  - name: daily_csv
    type: csv
    path: ./data/daily.csv
    schedule: "0 0 * * *"
`)
	if _, err := Load(p); err != nil {
		t.Fatalf("valid schedule should not error: %v", err)
	}
}

func TestLoadEmptyScheduleOK(t *testing.T) {
	// schedule が未指定でも Load が成功することを確認する。
	p := writeTemp(t, `
sources:
  - name: no_sched
    type: csv
    path: ./data/file.csv
`)
	if _, err := Load(p); err != nil {
		t.Fatalf("empty schedule should not error: %v", err)
	}
}
