package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeBin は指定した stdout / 終了コードを返す実行可能スクリプトを作る。
func fakeBin(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-engine")
	script := "#!/bin/sh\ncat <<'JSON'\n" + stdout + "\nJSON\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "1"
}

func TestCheckParsesViolations(t *testing.T) {
	json := `[{"id":"viol-1","rule":"positive_price","table":"products","row":2,"column":"sale_price","value":"-5","detected_at":"2026-08-17T00:00:00+00:00"}]`
	r := New(fakeBin(t, json, 0))
	vs, err := r.Check(context.Background(), "in.csv", "rules.yaml")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d", len(vs))
	}
	if vs[0].Rule != "positive_price" || vs[0].Row != 2 || vs[0].Value != "-5" {
		t.Errorf("violation mismatch: %+v", vs[0])
	}
}

func TestCheckEmpty(t *testing.T) {
	r := New(fakeBin(t, "[]", 0))
	vs, err := r.Check(context.Background(), "in.csv", "rules.yaml")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("want 0 violations, got %d", len(vs))
	}
}

func TestCheckNonZeroExit(t *testing.T) {
	r := New(fakeBin(t, "boom", 1))
	if _, err := r.Check(context.Background(), "in.csv", "rules.yaml"); err == nil {
		t.Error("expected error on non-zero exit")
	}
}

func TestCheckBadJSON(t *testing.T) {
	r := New(fakeBin(t, "not json", 0))
	if _, err := r.Check(context.Background(), "in.csv", "rules.yaml"); err == nil {
		t.Error("expected error on invalid json")
	}
}

func TestNewDefaultsBin(t *testing.T) {
	if New("").Bin != DefaultBin {
		t.Errorf("New(\"\") should default to %q", DefaultBin)
	}
}

func TestCheckRejectsRelativeBin(t *testing.T) {
	// 相対パスの Bin は PATH ハイジャック防止のため実行前にエラーになること。
	r := &Runner{Bin: "dataguard-engine"}
	if _, err := r.Check(context.Background(), "in.csv", "rules.yaml"); err == nil {
		t.Error("expected error when Bin is not an absolute path")
	}
}

func TestAnalyzeRejectsRelativeBin(t *testing.T) {
	r := &Runner{Bin: "dataguard-engine"}
	if _, err := r.Analyze(context.Background(), "schema.sql"); err == nil {
		t.Error("expected error when Bin is not an absolute path")
	}
}
