package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/store"
)

// fakeStore はテスト用の Storer 実装。
type fakeStore struct {
	violations []engine.Violation
	diff       *store.SchemaDiff
	diffs      []store.SchemaDiff
}

func (f *fakeStore) ListViolations() ([]engine.Violation, error) { return f.violations, nil }
func (f *fakeStore) CountViolations() (int, error)               { return len(f.violations), nil }
func (f *fakeStore) ListViolationsPaged(limit, offset int) ([]engine.Violation, error) {
	all := f.violations
	if offset >= len(all) {
		return []engine.Violation{}, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}
func (f *fakeStore) LatestDiff(_ string) (*store.SchemaDiff, error) { return f.diff, nil }
func (f *fakeStore) ListDiffs() ([]store.SchemaDiff, error)         { return f.diffs, nil }

// fakeRunner はテスト用の Runner 実装。
type fakeRunner struct{ payload json.RawMessage }

func (f *fakeRunner) Analyze(_ context.Context, _ string) (json.RawMessage, error) {
	return f.payload, nil
}

func newTestServer(st Storer, runner Runner) *Server {
	return New(st, runner, nil)
}

func TestHealth(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestViolationsAll(t *testing.T) {
	st := &fakeStore{
		violations: []engine.Violation{
			{ID: "v1", Table: "products", Rule: "r"},
			{ID: "v2", Table: "orders", Rule: "r"},
		},
	}
	srv := newTestServer(st, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/violations", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var got []engine.Violation
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 violations, got %d", len(got))
	}
}

func TestViolationsFilter(t *testing.T) {
	st := &fakeStore{
		violations: []engine.Violation{
			{ID: "v1", Table: "products"},
			{ID: "v2", Table: "orders"},
		},
	}
	srv := newTestServer(st, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/violations?table=products", nil)
	srv.Handler().ServeHTTP(w, req)
	var got []engine.Violation
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 1 || got[0].Table != "products" {
		t.Errorf("unexpected filter result: %+v", got)
	}
}

func TestViolationsPagination(t *testing.T) {
	viols := make([]engine.Violation, 5)
	for i := range viols {
		viols[i] = engine.Violation{ID: fmt.Sprintf("v%d", i), Table: "t"}
	}
	st := &fakeStore{violations: viols}
	srv := newTestServer(st, &fakeRunner{})

	// limit=2&offset=1 → v1,v2
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/violations?limit=2&offset=1", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if w.Header().Get("X-Total-Count") != "5" {
		t.Errorf("X-Total-Count: want 5, got %s", w.Header().Get("X-Total-Count"))
	}
	var got []engine.Violation
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Errorf("want 2, got %d", len(got))
	}
}

func TestViolationsPaginationInvalidLimit(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/violations?limit=abc", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestViolationsPaginationExceedMaxLimit(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/violations?limit=9999", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestLineageMissingParam(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/lineage", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestLineageOK(t *testing.T) {
	payload := json.RawMessage(`{"target":"t","sources":["s"],"has_cycle":false}`)
	srv := newTestServer(&fakeStore{}, &fakeRunner{payload: payload})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/lineage?sql=x.sql", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if w.Body.String() != string(payload) {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestSchemaDiffAll(t *testing.T) {
	st := &fakeStore{
		diffs: []store.SchemaDiff{{Table: "products", DetectedAt: "2026-01-02T00:00:00Z"}},
	}
	srv := newTestServer(st, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/schema-diff", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestSchemaDiffTable(t *testing.T) {
	diff := &store.SchemaDiff{Table: "products", DetectedAt: "2026-01-02T00:00:00Z",
		Added: []store.ColumnDef{{Name: "discount", Type: "numeric"}},
	}
	srv := newTestServer(&fakeStore{diff: diff}, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/schema-diff?table=products", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var got store.SchemaDiff
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Added) != 1 {
		t.Errorf("want 1 added column, got %d", len(got.Added))
	}
}

func TestDashboard(t *testing.T) {
	srv := newTestServer(&fakeStore{}, &fakeRunner{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %s", ct)
	}
	body := w.Body.String()
	if !containsAll(body, "DataGuard Rail", "/api/violations", "/api/schema-diff") {
		t.Error("dashboard HTML missing expected content")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
