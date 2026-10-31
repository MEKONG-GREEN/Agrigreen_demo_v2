package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) appraisalEntryHistory(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	staff, ok := uuidParam(c, "staff", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	var before int64
	if raw := c.Query("before"); raw != "" {
		if len(raw) > 18 || strings.Trim(raw, "0123456789") != "" {
			httpx.Fail(c, problem.Validation.WithField("before", "mốc lịch sử là số nguyên dương tối đa 18 chữ số"))
			return
		}
		value, err := strconv.ParseInt(raw, 10, 64) // đã chặn cú pháp và độ dài trước ParseInt
		if err != nil || value < 1 {
			httpx.Fail(c, problem.Validation.WithField("before", "mốc lịch sử phải dương"))
			return
		}
		before = value
	}
	v, err := h.Svc.AppraisalHistory(c.Request.Context(), period, staff, before)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h Handler) appraisalEntryVersion(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	staff, ok := uuidParam(c, "staff", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	version, ok := uuidParam(c, "version", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.AppraisalEntryVersion(c.Request.Context(), period, staff, version)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
