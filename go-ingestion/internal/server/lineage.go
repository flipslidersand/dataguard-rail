package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var lineageTracer = otel.Tracer("dataguard-rail/server")

// handleLineage は GET /api/lineage?sql=path/to/file.sql を処理する。
func (s *Server) handleLineage(c *gin.Context) {
	_, span := lineageTracer.Start(c.Request.Context(), "lineage.analyze")
	defer span.End()

	sqlPath := c.Query("sql")
	if sqlPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sql パラメータは必須です"})
		return
	}
	span.SetAttributes(attribute.String("sql.path", sqlPath))

	raw, err := s.runner.Analyze(sqlPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}
