package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleLineage は GET /api/lineage?sql=path/to/file.sql を処理する。
func (s *Server) handleLineage(c *gin.Context) {
	sqlPath := c.Query("sql")
	if sqlPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sql パラメータは必須です"})
		return
	}

	raw, err := s.runner.Analyze(sqlPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Rust engine の出力をそのまま返す（再エンコードによる型変換を避ける）。
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}
