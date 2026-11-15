package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

func decodeTemplateJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("definition: trailing JSON")
	}
	return nil
}
func bindTemplateJSON(c *gin.Context, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil || len(raw) > 1<<20 || decodeTemplateJSON(raw, out) != nil {
		httpx.Fail(c, problem.BadRequest)
		return false
	}
	return true
}
func readTemplateCopy(c *gin.Context, out *port.TemplateCopy) bool {
	if !bindTemplateJSON(c, out) {
		return false
	}
	if out.Source.ID.String() != c.Param("id") {
		httpx.Fail(c, problem.Validation.WithField("source", "mã nguồn phải khớp đường dẫn"))
		return false
	}
	return true
}
func (h Handler) copyTemplate(c *gin.Context) {
	var req port.TemplateCopy
	if !readTemplateCopy(c, &req) {
		return
	}
	saved, err := h.Svc.CopyTemplate(c.Request.Context(), req, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, templateOf(saved))
}
func (h Handler) templateUsage(c *gin.Context) {
	id, ok := pathID(c, port.ErrTemplateNotFound)
	if !ok {
		return
	}
	usage, err := h.Svc.TemplateUsage(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, usage)
}
