// Package handler exposes the Admin fixed-template lifecycle through the authenticated KN API.
package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

type Handler struct{ Svc *app.Service }

func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/khtc/templates")
	g.GET("", h.catalog)
	g.GET("/:key", h.detail)
	g.POST("/:key/versions", h.save)
	g.POST("/:key/restore", h.restore)
	g.POST("/:key/publish", h.publish)
	g.POST("/:key/preview", h.preview)
}
func bind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, domain.MaxDefinitionBytes+65536)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân JSON không hợp lệ hoặc vượt giới hạn"))
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ gửi một đối tượng JSON"))
		return false
	}
	return true
}
func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }
func (h Handler) catalog(c *gin.Context) {
	out, err := h.Svc.Catalog(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
func (h Handler) detail(c *gin.Context) {
	out, err := h.Svc.Detail(c.Request.Context(), c.Param("key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
func (h Handler) save(c *gin.Context) {
	var in app.SaveInput
	if !bind(c, &in) {
		return
	}
	out, err := h.Svc.Save(c.Request.Context(), c.Param("key"), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func (h Handler) restore(c *gin.Context) {
	var in app.RestoreInput
	if !bind(c, &in) {
		return
	}
	out, err := h.Svc.Restore(c.Request.Context(), c.Param("key"), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func (h Handler) publish(c *gin.Context) {
	var in app.PublishInput
	if !bind(c, &in) {
		return
	}
	out, err := h.Svc.Publish(c.Request.Context(), c.Param("key"), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func (h Handler) preview(c *gin.Context) {
	var in app.PreviewInput
	if !bind(c, &in) {
		return
	}
	out, err := h.Svc.Preview(c.Request.Context(), c.Param("key"), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
