// Package alert は外部通知（Slack Webhook 等）を提供する。
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Notifier は通知先を抽象化する。
type Notifier interface {
	Notify(ctx context.Context, msg string) error
}

// SlackNotifier は Slack Incoming Webhook に通知する。
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewSlack は SlackNotifier を返す。webhookURL が空の場合は noop を返す。
func NewSlack(webhookURL string) Notifier {
	if webhookURL == "" {
		return NoopNotifier{}
	}
	return &SlackNotifier{webhookURL: webhookURL, client: &http.Client{Timeout: 10 * time.Second}}
}

// Notify は Slack Webhook に msg を送信する。
func (s *SlackNotifier) Notify(ctx context.Context, msg string) error {
	body, err := json.Marshal(map[string]string{"text": msg})
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	return nil
}

// NoopNotifier は通知を無視する。
type NoopNotifier struct{}

func (NoopNotifier) Notify(_ context.Context, _ string) error { return nil }
