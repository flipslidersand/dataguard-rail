package ingester

import (
	"os"
	"path/filepath"
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

func TestLoadCSVErrors(t *testing.T) {
	if _, err := LoadCSV(filepath.Join(t.TempDir(), "nope.csv")); err == nil {
		t.Error("expected error for missing file")
	}
	if _, err := LoadCSV(writeCSV(t, "")); err == nil {
		t.Error("expected error for empty file")
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
