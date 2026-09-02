package server

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var lineageTracer = otel.Tracer("dataguard-rail/server")

// validateSQLPath はパストラバーサルを防ぐため入力パスを検証する。
// ".." を含むパスおよび ".sql" 以外の拡張子を拒否する。
func validateSQLPath(p string) bool {
	if strings.Contains(p, "..") {
		return false
	}
	if !strings.HasSuffix(p, ".sql") {
		return false
	}
	// filepath.Clean で正規化後も ".." が現れないことを確認
	cleaned := filepath.Clean(p)
	return !strings.Contains(cleaned, "..")
}

// handleLineage は GET /api/lineage?sql=path/to/file.sql を処理する。
func (s *Server) handleLineage(c *gin.Context) {
	_, span := lineageTracer.Start(c.Request.Context(), "lineage.analyze")
	defer span.End()

	sqlPath := c.Query("sql")
	if sqlPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sql パラメータは必須です"})
		return
	}
	if !validateSQLPath(sqlPath) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無効な sql パスです"})
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
