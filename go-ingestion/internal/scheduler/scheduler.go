// Package scheduler は sources.yaml の schedule フィールドに基づいて
// pipeline.Run を定期実行する cron ループを提供する。
package scheduler

import (
	"context"
	"fmt"

	"github.com/flipslidersand/dataguard-rail/internal/alert"
	"github.com/flipslidersand/dataguard-rail/internal/config"
	"github.com/flipslidersand/dataguard-rail/internal/pipeline"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// Runner はスケジューラが使う pipeline.Run と同じシグネチャ。テストで差し替え可能。
type Runner func(ctx context.Context, src config.DataSource) error

// Scheduler は cron ジョブを管理する。
type Scheduler struct {
	c      *cron.Cron
	ctx    context.Context
	cancel context.CancelFunc
	log    *zap.Logger
}

// New は Scheduler を生成する。
// ctx はジョブ全体のライフサイクルを制御する cancelable context を渡す。
// Stop() を呼ぶと ctx がキャンセルされ、実行中ジョブにシャットダウンシグナルが伝播する。
func New(ctx context.Context, log *zap.Logger) *Scheduler {
	if log == nil {
		log = zap.NewNop()
	}
	jobCtx, cancel := context.WithCancel(ctx)
	return &Scheduler{
		c:      cron.New(),
		ctx:    jobCtx,
		cancel: cancel,
		log:    log,
	}
}

// Register は DataSource を cron に登録する。
// schedule が空の場合はスキップ（呼び出し元が即時実行する）。
func (s *Scheduler) Register(src config.DataSource, run Runner) error {
	if src.Schedule == "" {
		return nil
	}
	log := s.log
	ctx := s.ctx
	_, err := s.c.AddFunc(src.Schedule, func() {
		log.Info("scheduled ingest start", zap.String("source", src.Name), zap.String("schedule", src.Schedule))
		if err := run(ctx, src); err != nil {
			log.Error("scheduled ingest error", zap.String("source", src.Name), zap.Error(err))
		} else {
			log.Info("scheduled ingest done", zap.String("source", src.Name))
		}
	})
	if err != nil {
		return fmt.Errorf("cron register %q (%s): %w", src.Name, src.Schedule, err)
	}
	return nil
}

// Start は cron ループを開始する。
func (s *Scheduler) Start() { s.c.Start() }

// Stop は実行中ジョブの ctx をキャンセルし、cron ループを停止して完了を待つ。
func (s *Scheduler) Stop() {
	s.cancel()
	s.c.Stop()
}

// HasJobs はスケジュール登録済みジョブがあるかを返す。
func (s *Scheduler) HasJobs() bool { return len(s.c.Entries()) > 0 }

// BuildRunner は pipeline の依存を束ねて Runner を返す。
func BuildRunner(
	rulesPath, tmpDir string,
	loader pipeline.Loader,
	checker pipeline.Checker,
	saver pipeline.Saver,
	notifier alert.Notifier,
	log *zap.Logger,
) Runner {
	return func(ctx context.Context, src config.DataSource) error {
		cfg := &config.Config{Sources: []config.DataSource{src}}
		_, err := pipeline.Run(ctx, cfg, rulesPath, tmpDir, loader, checker, saver, notifier, log)
		return err
	}
}
