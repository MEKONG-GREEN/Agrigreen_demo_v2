package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerAppraisals(rg *gin.RouterGroup) {
	rg.GET("/hrm/appraisals/templates/:kind", h.appraisalTemplates)
	rg.GET("/hrm/appraisals/templates/:kind/:code/history", h.appraisalTemplateHistory)
	rg.POST("/hrm/appraisals/templates/:kind/:code", h.appraisalSaveTemplate)
	rg.GET("/hrm/appraisals/periods", h.appraisalPeriods)
	rg.POST("/hrm/appraisals/periods", h.appraisalOpenPeriod)
	rg.GET("/hrm/appraisals/periods/:period/entries", h.appraisalEntries)
	rg.GET("/hrm/appraisals/periods/:period/entries/:staff", h.appraisalEntry)
	rg.POST("/hrm/appraisals/periods/:period/entries/:staff", h.appraisalChangeEntry)
	rg.POST("/hrm/appraisals/periods/:period/entries/:staff/guest-link", h.appraisalGuestLink)
}

func appraisalDecode(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.Fail(c, problem.PayloadTooLarge.WithDetail("yêu cầu đánh giá tối đa 2 MiB"))
		} else {
			httpx.Fail(c, problem.Validation.WithDetail("thân yêu cầu đánh giá không hợp lệ"))
		}
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		httpx.Fail(c, problem.Validation.WithDetail("chỉ gửi một đối tượng JSON"))
		return false
	}
	return true
}

func (h Handler) appraisalTemplates(c *gin.Context) {
	v, err := h.Svc.AppraisalTemplates(c.Request.Context(), c.Param("kind"), "", false)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": v})
}
func (h Handler) appraisalTemplateHistory(c *gin.Context) {
	v, err := h.Svc.AppraisalTemplates(c.Request.Context(), c.Param("kind"), c.Param("code"), true)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": v})
}
func appraisalIdem(c *gin.Context) string {
	if key := idempotency.KeyFromContext(c.Request.Context()); key != "" {
		return key
	}
	// Cùng header mà middleware dùng; giúp các handler harness độc lập kiểm được khóa bền của sổ.
	return c.GetHeader(idempotency.Header)
}
func (h Handler) appraisalSaveTemplate(c *gin.Context) {
	var in app.AppraisalTemplateChange
	if !appraisalDecode(c, &in) {
		return
	}
	v, err := h.Svc.SaveAppraisalTemplate(c.Request.Context(), c.Param("kind"), c.Param("code"), appraisalIdem(c), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h Handler) appraisalPeriods(c *gin.Context) {
	v, err := h.Svc.AppraisalPeriods(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": v})
}
func (h Handler) appraisalOpenPeriod(c *gin.Context) {
	var in app.AppraisalPeriodInput
	if !appraisalDecode(c, &in) {
		return
	}
	v, err := h.Svc.OpenAppraisalPeriod(c.Request.Context(), appraisalIdem(c), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h Handler) appraisalEntries(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	items, err := h.Svc.AppraisalEntries(c.Request.Context(), period)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) appraisalEntry(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	staff, ok := uuidParam(c, "staff", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.AppraisalEntryDetail(c.Request.Context(), period, staff)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h Handler) appraisalChangeEntry(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	staff, ok := uuidParam(c, "staff", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	var in domain.AppraisalEntryChange
	if !appraisalDecode(c, &in) {
		return
	}
	v, err := h.Svc.ChangeAppraisalEntry(c.Request.Context(), period, staff, appraisalIdem(c), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h Handler) appraisalGuestLink(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	staff, ok := uuidParam(c, "staff", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	httpx.Fail(c, h.Svc.AppraisalGuestLink(c.Request.Context(), period, staff))
}
