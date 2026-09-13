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

// TestListAndCountViolationsByTable は table ごとの prefix scan が、他 table の
// violation を混入させず正しく件数・ページングを返すことを確認する（#99）。
func TestListAndCountViolationsByTable(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	in := []engine.Violation{
		{ID: "v1", Rule: "r", Table: "products", Row: 1},
		{ID: "v2", Rule: "r", Table: "products", Row: 2},
		{ID: "v3", Rule: "r", Table: "products", Row: 3},
		{ID: "v1", Rule: "r", Table: "stock", Row: 1},
	}
	if err := s.SaveViolations(in); err != nil {
		t.Fatalf("SaveViolations: %v", err)
	}

	count, err := s.CountViolationsByTable("products")
	if err != nil {
		t.Fatalf("CountViolationsByTable: %v", err)
	}
	if count != 3 {
		t.Errorf("want 3 products violations, got %d", count)
	}

	if count, err := s.CountViolationsByTable("stock"); err != nil || count != 1 {
		t.Errorf("want 1 stock violation, got %d (err=%v)", count, err)
	}

	if count, err := s.CountViolationsByTable("nonexistent"); err != nil || count != 0 {
		t.Errorf("want 0 for nonexistent table, got %d (err=%v)", count, err)
	}

	page, err := s.ListViolationsByTablePaged("products", 2, 1)
	if err != nil {
		t.Fatalf("ListViolationsByTablePaged: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("want 2 violations in page, got %d", len(page))
	}
	for _, v := range page {
		if v.Table != "products" {
			t.Errorf("expected only products violations, got table=%s", v.Table)
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

// TestListViolationsExceedsLimitErrors は保存件数が maxListViolations を超える場合、
// ListViolations が全件メモリ展開を避けてエラーを返すことを確認する
// (呼び出し元は ListViolationsPaged を使うべきことを示す)。
func TestListViolationsExceedsLimitErrors(t *testing.T) {
	orig := maxListViolations
	maxListViolations = 5
	defer func() { maxListViolations = orig }()

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	vs := make([]engine.Violation, maxListViolations+1)
	for i := range vs {
		vs[i] = engine.Violation{ID: fmt.Sprintf("v%d", i), Rule: "r", Table: "t"}
	}
	if err := s.SaveViolations(vs); err != nil {
		t.Fatalf("SaveViolations: %v", err)
	}

	if _, err := s.ListViolations(); err == nil {
		t.Fatal("want error when violation count exceeds maxListViolations, got nil")
	}

	// 上限以下ならこれまで通り成功する。
	if err := s.db.DropAll(); err != nil {
		t.Fatalf("DropAll: %v", err)
	}
	if err := s.SaveViolations(vs[:maxListViolations]); err != nil {
		t.Fatalf("SaveViolations: %v", err)
	}
	got, err := s.ListViolations()
	if err != nil {
		t.Fatalf("ListViolations: %v", err)
	}
	if len(got) != maxListViolations {
		t.Fatalf("want %d violations, got %d", maxListViolations, len(got))
	}
}
