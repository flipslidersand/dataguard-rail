package server

import (
	"encoding/json"
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
func (f *fakeStore) LatestDiff(_ string) (*store.SchemaDiff, error) { return f.diff, nil }
func (f *fakeStore) ListDiffs() ([]store.SchemaDiff, error)         { return f.diffs, nil }

// fakeRunner はテスト用の Runner 実装。
type fakeRunner struct{ payload json.RawMessage }

func (f *fakeRunner) Analyze(_ string) (json.RawMessage, error) { return f.payload, nil }

func newTestServer(st Storer, runner Runner) *Server {
	return New(st, runner)
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
