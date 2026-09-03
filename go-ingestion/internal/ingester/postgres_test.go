package ingester

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeRows は queryRows を実装する DB 不要のスタブ。
type fakeRows struct {
	fields []string
	data   [][]any
	idx    int
	err    error
}

func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	fd := make([]pgconn.FieldDescription, len(f.fields))
	for i, n := range f.fields {
		fd[i] = pgconn.FieldDescription{Name: n}
	}
	return fd
}

func (f *fakeRows) Next() bool {
	if f.idx >= len(f.data) {
		return false
	}
	f.idx++
	return true
}

func (f *fakeRows) Values() ([]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.data[f.idx-1], nil
}

func (f *fakeRows) Err() error { return f.err }

func TestRowsToDataset(t *testing.T) {
	rows := &fakeRows{
		fields: []string{"id", "price", "email"},
		data: [][]any{
			{1, 100, "a@x.com"},
			{2, -5, nil},                 // NULL → ""
			{3, int64(0), []byte("raw")}, // []byte → string
		},
	}
	ds, err := rowsToDataset(rows)
	if err != nil {
		t.Fatalf("rowsToDataset: %v", err)
	}
	if len(ds.Headers) != 3 || ds.Headers[2] != "email" {
		t.Errorf("headers mismatch: %v", ds.Headers)
	}
	if len(ds.Rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(ds.Rows))
	}
	if ds.Rows[0][1] != "100" {
		t.Errorf("int cell mismatch: %q", ds.Rows[0][1])
	}
	if ds.Rows[1][2] != "" {
		t.Errorf("NULL should be empty string, got %q", ds.Rows[1][2])
	}
	if ds.Rows[2][2] != "raw" {
		t.Errorf("[]byte cell mismatch: %q", ds.Rows[2][2])
	}
}

func TestRowsToDatasetError(t *testing.T) {
	rows := &fakeRows{fields: []string{"id"}, data: [][]any{{1}}, err: errors.New("boom")}
	if _, err := rowsToDataset(rows); err == nil {
		t.Error("expected error propagation")
	}
}

func TestRowsToDatasetMaxRows(t *testing.T) {
	data := make([][]any, MaxPostgresRows+1)
	for i := range data {
		data[i] = []any{fmt.Sprintf("row%d", i)}
	}
	rows := &fakeRows{fields: []string{"id"}, data: data}
	_, err := rowsToDataset(rows)
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Errorf("expected max rows error, got: %v", err)
	}
}

func TestCellToString(t *testing.T) {
	cases := map[string]struct {
		in   any
		want string
	}{
		"nil":    {nil, ""},
		"string": {"hi", "hi"},
		"bytes":  {[]byte("b"), "b"},
		"int":    {42, "42"},
		"float":  {3.5, "3.5"},
		"bool":   {true, "true"},
	}
	for name, c := range cases {
		if got := cellToString(c.in); got != c.want {
			t.Errorf("%s: cellToString(%v)=%q want %q", name, c.in, got, c.want)
		}
	}
}
