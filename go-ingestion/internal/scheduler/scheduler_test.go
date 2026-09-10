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

// TestOverlappingJobIsSkipped は、前回起動のジョブがスケジュール間隔より長くかかる場合に
// 次のトリガーが並行実行されず（SkipIfStillRunning）、同時実行数が常に 1 以下であることを
// 確認する（#90）。
func TestOverlappingJobIsSkipped(t *testing.T) {
	var running atomic.Int32
	var maxConcurrent atomic.Int32
	var starts atomic.Int32

	s := New(context.Background(), nil)
	src := config.DataSource{Name: "x", Schedule: "@every 200ms"}
	if err := s.Register(src, func(_ context.Context, _ config.DataSource) error {
		starts.Add(1)
		n := running.Add(1)
		for {
			old := maxConcurrent.Load()
			if n <= old || maxConcurrent.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(500 * time.Millisecond) // スケジュール間隔より長い
		running.Add(-1)
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s.Start()
	time.Sleep(2500 * time.Millisecond)
	s.Stop()

	if starts.Load() < 2 {
		t.Fatalf("expected the job to have been triggered at least twice, got %d", starts.Load())
	}
	if maxConcurrent.Load() > 1 {
		t.Errorf("expected max concurrent executions <= 1, got %d", maxConcurrent.Load())
	}
}

// TestStopWaitsForRunningJob は Stop() が実行中ジョブの完了を待ってから
// 戻ることを確認する（#91）。
func TestStopWaitsForRunningJob(t *testing.T) {
	var started, finished atomic.Bool

	s := New(context.Background(), nil)
	src := config.DataSource{Name: "x", Schedule: "@every 50ms"}
	if err := s.Register(src, func(_ context.Context, _ config.DataSource) error {
		started.Store(true)
		time.Sleep(300 * time.Millisecond)
		finished.Store(true)
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s.Start()

	// ジョブが実際に開始するまで待つ（固定 sleep だとティックの前に Stop してしまい
	// 「ジョブが一度も走らないまま Stop が即座に返る」フレークになる）。
	deadline := time.Now().Add(2 * time.Second)
	for !started.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !started.Load() {
		t.Fatal("job never started within 2s")
	}

	s.Stop()
	if !finished.Load() {
		t.Error("Stop() returned before the running job finished")
	}
}

// TestStopTimesOutIfJobHangs は StopTimeout を超えるジョブに対し、Stop() が
// 完了を待たずタイムアウトで戻ることを確認する（プロセス終了自体をブロックしないため）。
func TestStopTimesOutIfJobHangs(t *testing.T) {
	orig := StopTimeout
	StopTimeout = 50 * time.Millisecond
	defer func() { StopTimeout = orig }()

	blockCtx, unblock := context.WithCancel(context.Background())
	defer unblock()

	s := New(context.Background(), nil)
	src := config.DataSource{Name: "x", Schedule: "@every 20ms"}
	if err := s.Register(src, func(_ context.Context, _ config.DataSource) error {
		<-blockCtx.Done() // Stop がタイムアウトするまで戻らない
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s.Start()
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	s.Stop()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Stop() should return promptly after StopTimeout, took %v", elapsed)
	}
}
