// Package server は gin ベースの REST API サーバーを提供する。
package server

import (
	"encoding/json"
	"net/http"

	"github.com/flipslidersand/dataguard-rail/internal/alert"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/store"
	"github.com/gin-gonic/gin"
)

// Storer は violations / schema の読み書きを抽象化する。テストで差し替え可能。
type Storer interface {
	ListViolations() ([]engine.Violation, error)
	LatestDiff(table string) (*store.SchemaDiff, error)
	ListDiffs() ([]store.SchemaDiff, error)
}

// Runner は Rust engine の呼び出しを抽象化する。
type Runner interface {
	Analyze(sqlPath string) (json.RawMessage, error)
}

// Server は HTTP サーバーの依存セットを保持する。
type Server struct {
	store    Storer
	runner   Runner
	notifier alert.Notifier
	engine   *gin.Engine
}

// New はサーバーインスタンスを生成する。
func New(st Storer, runner Runner, notifier alert.Notifier) *Server {
	if notifier == nil {
		notifier = alert.NoopNotifier{}
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{store: st, runner: runner, notifier: notifier, engine: r}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := s.engine.Group("/api")
	v1.GET("/violations", s.handleViolations)
	v1.GET("/lineage", s.handleLineage)
	v1.GET("/schema-diff", s.handleSchemaDiff)
}

// Run は addr でリッスンを開始する。
func (s *Server) Run(addr string) error {
	return s.engine.Run(addr)
}

// Handler はテスト用に gin.Engine を返す。
func (s *Server) Handler() http.Handler {
	return s.engine
}
