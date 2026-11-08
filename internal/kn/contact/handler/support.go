package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

const supportBodyLimit = 12 << 10

// RegisterPublic reuses the application's public group and adds the intake-specific IP limit.
func (h Handler) RegisterPublic(rg *gin.RouterGroup) {
	rg.POST("/contacts/support-requests", httpx.RateLimitByIP(h.Svc.Clock, 1, 5), h.supportSubmit)
}

func supportJSON(c *gin.Context, v any) bool {
	noStore(c)
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		httpx.Fail(c, problem.New(http.StatusUnsupportedMediaType, "contact.support_json_required", "Chỉ nhận nội dung JSON, không nhận tệp"))
		return false
	}
	if c.Request.ContentLength > supportBodyLimit {
		httpx.Fail(c, problem.New(http.StatusRequestEntityTooLarge, "contact.support_too_large", "Nội dung yêu cầu quá dài"))
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, supportBodyLimit)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	err = dec.Decode(v)
	if err == nil {
		var extra any
		if e := dec.Decode(&extra); !errors.Is(e, io.EOF) {
			err = e
			if e == nil {
				err = errors.New("extra JSON")
			}
		}
	}
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			httpx.Fail(c, problem.New(http.StatusRequestEntityTooLarge, "contact.support_too_large", "Nội dung yêu cầu quá dài"))
		} else {
			httpx.Fail(c, problem.Validation.WithDetail("thân yêu cầu không đúng dạng JSON của API"))
		}
		return false
	}
	return true
}

func (h Handler) supportSubmit(c *gin.Context) {
	var in domain.SupportInput
	if !supportJSON(c, &in) {
		return
	}
	if err := h.Svc.SubmitSupport(c.Request.Context(), in, c.GetHeader(idempotency.Header)); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (h Handler) registerSupport(rg *gin.RouterGroup) {
	rg.GET("/contacts/support-requests", h.supportList)
	rg.GET("/contacts/support-requests/:id", h.supportGet)
	rg.PUT("/contacts/support-requests/:id/resolution", h.supportResolve)
}

func (h Handler) supportList(c *gin.Context) {
	noStore(c)
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("offset", "số nguyên"))
			return
		}
		offset = n
	}
	rows, err := h.Svc.ListSupport(c.Request.Context(), c.Query("state"), offset)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if rows == nil {
		c.JSON(http.StatusOK, gin.H{"items": []any{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

func (h Handler) supportGet(c *gin.Context) {
	noStore(c)
	id, ok := idParam(c)
	if !ok {
		return
	}
	detail, err := h.Svc.GetSupport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h Handler) supportResolve(c *gin.Context) {
	noStore(c)
	id, ok := idParam(c)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !supportJSON(c, &in) {
		return
	}
	result, err := h.Svc.ResolveSupport(c.Request.Context(), id, in.ExpectedVersion, c.GetHeader(idempotency.Header))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
