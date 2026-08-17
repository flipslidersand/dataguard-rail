package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleViolations は GET /api/violations を処理する。
// クエリパラメータ ?table=xxx でフィルタ可能。
func (s *Server) handleViolations(c *gin.Context) {
	all, err := s.store.ListViolations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	table := c.Query("table")
	if table == "" {
		c.JSON(http.StatusOK, all)
		return
	}

	filtered := all[:0]
	for _, v := range all {
		if v.Table == table {
			filtered = append(filtered, v)
		}
	}
	c.JSON(http.StatusOK, filtered)
}
