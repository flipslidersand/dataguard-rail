package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flipslidersand/dataguard-rail/internal/config"
)

func TestRegisterInvalidCron(t *testing.T) {
	s := New(context.Background(), nil)
	src := config.DataSource{Name: "x", Schedule: "not-a-cron"}
	if err := s.Register(src, func(_ context.Context, _ config.DataSource) error { return nil }); err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestRegisterEmptyScheduleSkips(t *testing.T) {
	s := New(context.Background(), nil)
	src := config.DataSource{Name: "x", Schedule: ""}
	if err := s.Register(src, func(_ context.Context, _ config.DataSource) error { return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.HasJobs() {
		t.Error("no jobs should be registered for empty schedule")
	}
}

func TestHasJobsAfterRegister(t *testing.T) {
	s := New(context.Background(), nil)
	src := config.DataSource{Name: "x", Schedule: "@every 1h"}
	if err := s.Register(src, func(_ context.Context, _ config.DataSource) error { return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.HasJobs() {
		t.Error("expected HasJobs() == true after register")
	}
}

func TestScheduledJobFires(t *testing.T) {
	var count atomic.Int32
	s := New(context.Background(), nil)
	src := config.DataSource{Name: "x", Schedule: "@every 500ms"}
	if err := s.Register(src, func(_ context.Context, _ config.DataSource) error {
		count.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s.Start()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if count.Load() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.Stop()
	if count.Load() < 2 {
		t.Errorf("expected >= 2 executions within 3s, got %d", count.Load())
	}
}
