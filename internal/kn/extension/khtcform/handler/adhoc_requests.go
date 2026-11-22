package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h Handler) registerAdhocRequests(rg *gin.RouterGroup) {
	rg.GET("/extension/khtc/adhoc-requests/inbox", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		rows, err := h.Svc.AdhocInbox(c.Request.Context())
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": rows})
	})
	g := rg.Group("/extension/khtc/periods/:id/adhoc-requests")
	g.GET("", h.adhocRequests)
	g.POST("", h.adhocRequestCreate)
	g.POST("/:requestID/reconcile", h.adhocRequestResume)
}
func requestPeriod(c *gin.Context) (uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrFormNotFound)
		return uuid.Nil, false
	}
	return id, true
}
func requestBody(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("Nội dung yêu cầu không hợp lệ hoặc quá lớn"))
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest)
		return false
	}
	return true
}
func (h Handler) adhocRequests(c *gin.Context) {
	id, ok := requestPeriod(c)
	if !ok {
		return
	}
	rows, err := h.Svc.AdhocRequests(c.Request.Context(), id, "")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}
func (h Handler) adhocRequestCreate(c *gin.Context) {
	id, ok := requestPeriod(c)
	if !ok {
		return
	}
	var in struct {
		Department string `json:"department"`
		app.AdhocRequestInput
	}
	if !requestBody(c, &in) {
		return
	}
	if len(in.Department) < 1 || len(in.Department) > 64 {
		httpx.Fail(c, problem.Validation.WithField("department", "Chọn phòng được giao"))
		return
	}
	row, err := h.Svc.RequestSupplement(c.Request.Context(), id, in.Department, in.AdhocRequestInput, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if row.State == "failed" || row.State == "cancelled" {
		httpx.Fail(c, app.ErrNotReviewable.WithDetail("Yêu cầu không còn phát hành được"))
		return
	}
	c.JSON(http.StatusCreated, row)
}
func (h Handler) adhocRequestResume(c *gin.Context) {
	id, ok := requestPeriod(c)
	if !ok {
		return
	}
	request, err := uuid.Parse(c.Param("requestID"))
	if err != nil {
		httpx.Fail(c, app.ErrFormNotFound)
		return
	}
	row, err := h.Svc.ResumeAdhocRequest(c.Request.Context(), id, request)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}
