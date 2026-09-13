// Package server は gin ベースの REST API サーバーを提供する。
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/flipslidersand/dataguard-rail/internal/alert"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/flipslidersand/dataguard-rail/internal/store"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Storer は violations / schema の読み書きを抽象化する。テストで差し替え可能。
type Storer interface {
	ListViolationsPaged(limit, offset int) ([]engine.Violation, error)
	ListViolationsByTablePaged(table string, limit, offset int) ([]engine.Violation, error)
	CountViolations() (int, error)
	CountViolationsByTable(table string) (int, error)
	LatestDiff(table string) (*store.SchemaDiff, error)
	ListDiffs() ([]store.SchemaDiff, error)
}

// Runner は Rust engine の呼び出しを抽象化する。
type Runner interface {
	Analyze(ctx context.Context, sqlPath string) (json.RawMessage, error)
}

// Server は HTTP サーバーの依存セットを保持する。
type Server struct {
	store    Storer
	runner   Runner
	notifier alert.Notifier
	engine   *gin.Engine
	apiKey   string
	log      *zap.Logger
}

// New はサーバーインスタンスを生成する。apiKey が空の場合、認証は無効化される
// (ローカル開発用途のみを想定。本番運用では必ず指定すること)。
func New(st Storer, runner Runner, notifier alert.Notifier, apiKey string, log *zap.Logger) *Server {
	if notifier == nil {
		notifier = alert.NoopNotifier{}
	}
	if log == nil {
		log = zap.NewNop()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{store: st, runner: runner, notifier: notifier, engine: r, apiKey: apiKey, log: log}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	var protected []gin.HandlerFunc
	if s.apiKey != "" {
		protected = append(protected, s.authMiddleware())
	}

	s.engine.GET("/", append(protected, s.handleDashboard)...)

	v1 := s.engine.Group("/api", protected...)
	v1.GET("/violations", s.handleViolations)
	v1.GET("/lineage", s.handleLineage)
	v1.GET("/schema-diff", s.handleSchemaDiff)
}

// authMiddleware は Authorization: Bearer <apiKey> ヘッダを検証する。
// 一致しない・欠落している場合は 401 で中断する。
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// Run は addr でリッスンを開始する。
// Slowloris 型のリソース枯渇を防ぐため、明示的な http.Server でタイムアウトを設定する。
func (s *Server) Run(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return srv.ListenAndServe()
}

// Handler はテスト用に gin.Engine を返す。
func (s *Server) Handler() http.Handler {
	return s.engine
}
