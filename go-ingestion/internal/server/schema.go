package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var schemaTracer = otel.Tracer("dataguard-rail/server")

// handleSchemaDiff は GET /api/schema-diff を処理する。
// ?table=xxx を指定した場合はそのテーブルの差分のみ返す。
func (s *Server) handleSchemaDiff(c *gin.Context) {
	ctx, span := schemaTracer.Start(c.Request.Context(), "schema_diff_detected")
	defer span.End()

	table := c.Query("table")

	if table != "" {
		diff, err := s.store.LatestDiff(table)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if diff == nil {
			c.JSON(http.StatusOK, gin.H{"message": "スナップショットが不足しています（2件以上必要）"})
			return
		}
		span.SetAttributes(
			attribute.String("table", diff.Table),
			attribute.Int("added", len(diff.Added)),
			attribute.Int("dropped", len(diff.Dropped)),
			attribute.Int("changed", len(diff.Changed)),
		)
		if len(diff.Added)+len(diff.Dropped)+len(diff.Changed) > 0 {
			_ = s.notifier.Notify(ctx, fmt.Sprintf("[dataguard] schema diff detected on %s: +%d -%d ~%d",
				diff.Table, len(diff.Added), len(diff.Dropped), len(diff.Changed)))
		}
		c.JSON(http.StatusOK, diff)
		return
	}

	diffs, err := s.store.ListDiffs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	span.SetAttributes(attribute.Int("diff.count", len(diffs)))
	_ = ctx
	c.JSON(http.StatusOK, diffs)
}
