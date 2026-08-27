package store

import (
	"fmt"
	"testing"

	"github.com/flipslidersand/dataguard-rail/internal/engine"
)

func TestSaveAndListViolations(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	in := []engine.Violation{
		{ID: "viol-1", Rule: "positive_price", Table: "products", Row: 2, Column: "sale_price", Value: "-5"},
		{ID: "viol-2", Rule: "no_null_email", Table: "products", Row: 3, Column: "email", Value: ""},
		{ID: "viol-1", Rule: "count", Table: "stock", Row: 1, Column: "stock_id", Value: "A"},
	}
	if err := s.SaveViolations(in); err != nil {
		t.Fatalf("SaveViolations: %v", err)
	}

	got, err := s.ListViolations()
	if err != nil {
		t.Fatalf("ListViolations: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 violations, got %d", len(got))
	}
	// key は table+id で一意。table 違いの同一 id が衝突しないことを確認。
	found := map[string]bool{}
	for _, v := range got {
		found[v.Table+"/"+v.ID] = true
	}
	for _, k := range []string{"products/viol-1", "products/viol-2", "stock/viol-1"} {
		if !found[k] {
			t.Errorf("missing violation %s", k)
		}
	}
}

// TestSaveViolationsLargeBatch はバッチ境界をまたぐ件数（txnBatchSize+1）で
// SaveViolations が全件保存できることを確認する。
func TestSaveViolationsLargeBatch(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	n := txnBatchSize + 1
	vs := make([]engine.Violation, n)
	for i := 0; i < n; i++ {
		vs[i] = engine.Violation{
			ID:    fmt.Sprintf("v%d", i),
			Rule:  "rule",
			Table: "t",
			Row:   i,
		}
	}
	if err := s.SaveViolations(vs); err != nil {
		t.Fatalf("SaveViolations: %v", err)
	}
	got, err := s.ListViolations()
	if err != nil {
		t.Fatalf("ListViolations: %v", err)
	}
	if len(got) != n {
		t.Fatalf("want %d violations, got %d", n, len(got))
	}
}

func TestListEmpty(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	got, err := s.ListViolations()
	if err != nil {
		t.Fatalf("ListViolations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}
