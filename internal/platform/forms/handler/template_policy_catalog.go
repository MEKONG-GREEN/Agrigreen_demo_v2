package handler

import (
	"net/http"
	"strconv"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

func (h Handler) templateVersions(c *gin.Context) {
	before := int64(0)
	if raw := c.Query("before_version"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			httpx.Fail(c, problem.Validation.WithField("before_version", "mốc phiên bản không hợp lệ"))
			return
		}
		before = parsed
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			httpx.Fail(c, problem.Validation.WithField("limit", "giới hạn từ 1 đến 200"))
			return
		}
		limit = parsed
	}
	ts, err := h.Svc.TemplateHistory(c.Request.Context(), c.Query("profile"), c.Query("code"), before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]templateDTO, 0, len(ts))
	for _, t := range ts {
		out = append(out, templateOf(t))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) templateVersion(c *gin.Context) {
	version, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || version < 1 {
		httpx.Fail(c, problem.Validation.WithField("version", "phiên bản phải là số nguyên dương"))
		return
	}
	t, err := h.Svc.TemplateVersion(c.Request.Context(), c.Query("profile"), c.Query("code"), version)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, templateOf(t))
}
