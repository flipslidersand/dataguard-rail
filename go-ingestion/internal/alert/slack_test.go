package alert

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoopNotifier(t *testing.T) {
	n := NewSlack("")
	if err := n.Notify(context.Background(), "hello"); err != nil {
		t.Errorf("noop should not error: %v", err)
	}
}

func TestSlackNotifier(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewSlack(srv.URL)
	if err := n.Notify(context.Background(), "test message"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody == "" {
		t.Error("expected body to be sent")
	}
	expected := `{"text":"test message"}`
	if gotBody != expected {
		t.Errorf("want %q, got %q", expected, gotBody)
	}
}

func TestSlackNotifierNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewSlack(srv.URL)
	if err := n.Notify(context.Background(), "msg"); err == nil {
		t.Error("expected error on non-200 response")
	}
}
