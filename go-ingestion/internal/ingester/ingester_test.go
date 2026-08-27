package ingester

import (
	"errors"
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.csv")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadCSV(t *testing.T) {
	ds, err := LoadCSV(writeCSV(t, "id,price\n1,100\n2,-5\n"))
	if err != nil {
		t.Fatalf("LoadCSV: %v", err)
	}
	if len(ds.Headers) != 2 || ds.Headers[0] != "id" {
		t.Errorf("headers mismatch: %v", ds.Headers)
	}
	if len(ds.Rows) != 2 || ds.Rows[1][1] != "-5" {
		t.Errorf("rows mismatch: %v", ds.Rows)
	}
}

func writeJSONL(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.jsonl")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadJSONL(t *testing.T) {
	src := `{"id":"1","price":"100","email":"a@x.com"}` + "\n" +
		`{"id":"2","price":"-5","email":""}` + "\n"
	ds, err := LoadJSONL(writeJSONL(t, src))
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(ds.Headers) != 3 {
		t.Fatalf("want 3 headers, got %v", ds.Headers)
	}
	if ds.Headers[0] != "id" || ds.Headers[1] != "price" || ds.Headers[2] != "email" {
		t.Errorf("header order mismatch: %v", ds.Headers)
	}
	if len(ds.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(ds.Rows))
	}
	if ds.Rows[0][1] != "100" {
		t.Errorf("want price=100, got %q", ds.Rows[0][1])
	}
	if ds.Rows[1][1] != "-5" {
		t.Errorf("want price=-5, got %q", ds.Rows[1][1])
	}
}

func TestLoadJSONLNumericValues(t *testing.T) {
	src := `{"count":42,"ratio":3.14,"active":true}` + "\n"
	ds, err := LoadJSONL(writeJSONL(t, src))
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if ds.Rows[0][0] != "42" {
		t.Errorf("want count=42, got %q", ds.Rows[0][0])
	}
	if ds.Rows[0][1] != "3.14" {
		t.Errorf("want ratio=3.14, got %q", ds.Rows[0][1])
	}
	if ds.Rows[0][2] != "true" {
		t.Errorf("want active=true, got %q", ds.Rows[0][2])
	}
}

func TestLoadJSONLSkipsBlankLines(t *testing.T) {
	src := "\n" + `{"x":"1"}` + "\n\n" + `{"x":"2"}` + "\n"
	ds, err := LoadJSONL(writeJSONL(t, src))
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(ds.Rows) != 2 {
		t.Errorf("want 2 rows, got %d", len(ds.Rows))
	}
}

func TestLoadJSONLErrors(t *testing.T) {
	// 存在しないファイル
	if _, err := LoadJSONL(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Error("expected error for missing file")
	}
	// 空ファイル
	if _, err := LoadJSONL(writeJSONL(t, "")); err == nil {
		t.Error("expected error for empty file")
	}
	// 不正 JSON
	if _, err := LoadJSONL(writeJSONL(t, "not-json\n")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadCSVErrors(t *testing.T) {
	if _, err := LoadCSV(filepath.Join(t.TempDir(), "nope.csv")); err == nil {
		t.Error("expected error for missing file")
	}
	if _, err := LoadCSV(writeCSV(t, "")); err == nil {
		t.Error("expected error for empty file")
	}
}

// TestLoadJSONLLargeLine は 64KB を超える行が正常にパースできることを確認する (#59)。
func TestLoadJSONLLargeLine(t *testing.T) {
	// 100KB のダミー文字列を値に持つ JSON 行を作成する。
	largeValue := strings.Repeat("x", 100*1024)
	src := `{"key":"` + largeValue + `"}` + "\n"
	ds, err := LoadJSONL(writeJSONL(t, src))
	if err != nil {
		t.Fatalf("LoadJSONL with 100KB line: %v", err)
	}
	if len(ds.Rows) != 1 || ds.Rows[0][0] != largeValue {
		t.Errorf("large-line value mismatch (len=%d)", len(ds.Rows[0][0]))
	}
}

// TestLoadJSONLTooLongReturnsHint は jsonlMaxLineBytes を超えた行がヒント付きエラーを返すことを確認する (#59)。
func TestLoadJSONLTooLongReturnsHint(t *testing.T) {
	// jsonlMaxLineBytes+1 バイトの値を持つ行を作成する。
	oversized := strings.Repeat("y", jsonlMaxLineBytes+1)
	src := `{"key":"` + oversized + `"}` + "\n"
	_, err := LoadJSONL(writeJSONL(t, src))
	if err == nil {
		t.Fatal("expected error for oversized line, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("expected bufio.ErrTooLong in error chain, got: %v", err)
	}
	if !strings.Contains(err.Error(), "行サイズが上限") {
		t.Errorf("expected hint message in error, got: %v", err)
	}
}

func TestWriteTempCSVRoundTrip(t *testing.T) {
	ds := &Dataset{Headers: []string{"a", "b"}, Rows: [][]string{{"1", "2"}, {"3", "4"}}}
	path, cleanup, err := WriteTempCSV(ds, t.TempDir())
	if err != nil {
		t.Fatalf("WriteTempCSV: %v", err)
	}
	defer cleanup()

	got, err := LoadCSV(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Headers[1] != "b" || got.Rows[1][0] != "3" {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("cleanup should remove temp file")
	}
}
