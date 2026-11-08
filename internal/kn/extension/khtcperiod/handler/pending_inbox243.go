package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h Handler) pendingReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	query := c.Request.URL.Query()
	q := app.PendingReviewQuery{}
	for _, field := range []struct {
		name   string
		target *uuid.UUID
	}{{"before", &q.Before}, {"period_id", &q.PeriodID}} {
		if values, exists := query[field.name]; exists {
			if len(values) != 1 || len(values[0]) != 36 {
				httpx.Fail(c, problem.BadRequest.WithField(field.name, "định danh kỳ không hợp lệ"))
				return
			}
			id, err := uuid.Parse(values[0])
			if err != nil || id == uuid.Nil {
				httpx.Fail(c, problem.BadRequest.WithField(field.name, "định danh kỳ không hợp lệ"))
				return
			}
			*field.target = id
		}
	}
	for _, field := range []struct {
		name   string
		target *string
		max    int
	}{{"report_type", &q.ReportType, 7}, {"department", &q.Department, 40}} {
		if values, exists := query[field.name]; exists {
			if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > field.max {
				httpx.Fail(c, problem.BadRequest.WithField(field.name, "bộ lọc không hợp lệ"))
				return
			}
			*field.target = values[0]
		}
	}
	page, err := h.Svc.PendingReviewFiltered(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}
