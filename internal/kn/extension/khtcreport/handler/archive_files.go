package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h Handler) previewExport(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	exportID, ok := path(c, "exportID")
	if !ok {
		return
	}
	out, err := h.Svc.PreviewExport(c.Request.Context(), id, exportID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}
func (h Handler) archiveZIP(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var in struct {
		Items []app.ArchiveSelection `json:"items"`
	}
	if !bind(c, &in) {
		return
	}
	body, err := h.Svc.ArchiveZIP(c.Request.Context(), in.Items)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="bao-cao-khtc.zip"`)
	c.Data(http.StatusOK, "application/zip", body)
}
