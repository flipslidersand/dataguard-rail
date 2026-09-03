package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"github.com/gin-gonic/gin"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// handleViolations は GET /api/violations を処理する。
// クエリパラメータ: ?table=xxx&limit=100&offset=0
// レスポンスヘッダ: X-Total-Count (フィルタ前の保存済み総件数)
func (s *Server) handleViolations(c *gin.Context) {
	limit, offset, err := parsePagination(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	table := c.Query("table")

	if table == "" {
		total, err := s.store.CountViolations()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("X-Total-Count", strconv.Itoa(total))

		violations, err := s.store.ListViolationsPaged(limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, violations)
		return
	}

	// table フィルタ付き: 全件取得後にフィルタ・ページネーション。
	all, err := s.store.ListViolations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	matched := make([]engine.Violation, 0)
	for _, v := range all {
		if v.Table == table {
			matched = append(matched, v)
		}
	}
	c.Header("X-Total-Count", strconv.Itoa(len(matched)))

	start := offset
	if start > len(matched) {
		start = len(matched)
	}
	end := start + limit
	if limit <= 0 || end > len(matched) {
		end = len(matched)
	}
	c.JSON(http.StatusOK, matched[start:end])
}

func parsePagination(c *gin.Context) (limit, offset int, err error) {
	limit = defaultLimit
	offset = 0

	if l := c.Query("limit"); l != "" {
		limit, err = strconv.Atoi(l)
		if err != nil || limit < 0 {
			return 0, 0, fmt.Errorf("limit must be a non-negative integer")
		}
		if limit > maxLimit {
			return 0, 0, fmt.Errorf("limit must not exceed %d", maxLimit)
		}
	}
	if o := c.Query("offset"); o != "" {
		offset, err = strconv.Atoi(o)
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
	}
	return limit, offset, nil
}
