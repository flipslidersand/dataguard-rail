package store

import (
	"testing"
)

func TestDiffSnapshots(t *testing.T) {
	prev := SchemaSnapshot{
		Table:      "products",
		CapturedAt: "2026-01-01T00:00:00Z",
		Columns: []ColumnDef{
			{Name: "id", Type: "int4"},
			{Name: "sale_price", Type: "numeric"},
			{Name: "old_col", Type: "text"},
		},
	}
	curr := SchemaSnapshot{
		Table:      "products",
		CapturedAt: "2026-01-02T00:00:00Z",
		Columns: []ColumnDef{
			{Name: "id", Type: "int4"},
			{Name: "sale_price", Type: "float8"}, // changed
			{Name: "discount", Type: "numeric"},  // added
			// old_col dropped
		},
	}

	diff := diffSnapshots(prev, curr)
	if diff.Table != "products" {
		t.Errorf("unexpected table: %s", diff.Table)
	}
	if len(diff.Added) != 1 || diff.Added[0].Name != "discount" {
		t.Errorf("unexpected added: %+v", diff.Added)
	}
	if len(diff.Dropped) != 1 || diff.Dropped[0].Name != "old_col" {
		t.Errorf("unexpected dropped: %+v", diff.Dropped)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].Name != "sale_price" {
		t.Errorf("unexpected changed: %+v", diff.Changed)
	}
	if diff.Changed[0].From != "numeric" || diff.Changed[0].To != "float8" {
		t.Errorf("unexpected change: %+v", diff.Changed[0])
	}
}

func TestLatestDiffNotEnoughSnapshots(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_ = st.SaveSnapshot(SchemaSnapshot{
		Table:      "t",
		CapturedAt: "2026-01-01T00:00:00Z",
		Columns:    []ColumnDef{{Name: "id", Type: "int4"}},
	})

	diff, err := st.LatestDiff("t")
	if err != nil {
		t.Fatal(err)
	}
	if diff != nil {
		t.Errorf("want nil diff with single snapshot, got %+v", diff)
	}
}

func TestLatestDiffTwoSnapshots(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_ = st.SaveSnapshot(SchemaSnapshot{
		Table:      "t",
		CapturedAt: "2026-01-01T00:00:00Z",
		Columns:    []ColumnDef{{Name: "id", Type: "int4"}},
	})
	_ = st.SaveSnapshot(SchemaSnapshot{
		Table:      "t",
		CapturedAt: "2026-01-02T00:00:00Z",
		Columns:    []ColumnDef{{Name: "id", Type: "int4"}, {Name: "name", Type: "text"}},
	})

	diff, err := st.LatestDiff("t")
	if err != nil {
		t.Fatal(err)
	}
	if diff == nil {
		t.Fatal("want non-nil diff")
	}
	if len(diff.Added) != 1 || diff.Added[0].Name != "name" {
		t.Errorf("unexpected diff: %+v", diff)
	}
}
