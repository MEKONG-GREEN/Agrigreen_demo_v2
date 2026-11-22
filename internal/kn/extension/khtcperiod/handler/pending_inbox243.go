package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h Handler) pendingReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	before := uuid.Nil
	if values, exists := c.Request.URL.Query()["before"]; exists {
		if len(values) != 1 || len(values[0]) != 36 {
			httpx.Fail(c, problem.BadRequest.WithField("before", "con trỏ kỳ không hợp lệ"))
			return
		}
		id, err := uuid.Parse(values[0])
		if err != nil || id == uuid.Nil {
			httpx.Fail(c, problem.BadRequest.WithField("before", "con trỏ kỳ không hợp lệ"))
			return
		}
		before = id
	}
	page, err := h.Svc.PendingReview(c.Request.Context(), before)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}
