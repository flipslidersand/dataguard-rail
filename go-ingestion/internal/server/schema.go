package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleSchemaDiff は GET /api/schema-diff を処理する。
// ?table=xxx を指定した場合はそのテーブルの差分のみ返す。
func (s *Server) handleSchemaDiff(c *gin.Context) {
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
		c.JSON(http.StatusOK, diff)
		return
	}

	diffs, err := s.store.ListDiffs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, diffs)
}
