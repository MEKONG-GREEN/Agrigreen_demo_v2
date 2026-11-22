package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

func (h Handler) appraisalPaper(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.PreviewAppraisal(c.Request.Context(), period, c.Param("kind"), c.Query("unit"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
func (h Handler) appraisalExports(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.AppraisalExports(c.Request.Context(), period)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": v})
}
func (h Handler) appraisalExport(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	var in app.AppraisalExportInput
	if !appraisalDecode(c, &in) {
		return
	}
	v, err := h.Svc.ExportAppraisal(c.Request.Context(), period, appraisalIdem(c), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}
func (h Handler) appraisalExportFile(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	id, ok := uuidParam(c, "export", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.DownloadAppraisalExport(c.Request.Context(), period, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, v.FileName))
	c.Header("X-Content-SHA256", v.SHA256)
	if v.Drift {
		c.Header("X-Data-Drift", "true")
	}
	c.Data(http.StatusOK, v.ContentType, v.Data)
}
