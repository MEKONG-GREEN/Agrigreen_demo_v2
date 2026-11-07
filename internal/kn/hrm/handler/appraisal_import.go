package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/gin-gonic/gin"
)

var appraisalImportVersion = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

func (h Handler) appraisalImport(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 6<<20)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "chọn một tệp Excel .xlsx tối đa 5 MiB"))
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	raw := c.PostForm("version")
	if !appraisalImportVersion.MatchString(raw) {
		httpx.Fail(c, problem.Validation.WithField("version", "phiên bản là số nguyên dương, tối đa 18 chữ số"))
		return
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("version", "phiên bản không hợp lệ"))
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "chọn tệp Excel"))
		return
	}
	defer func() { _ = file.Close() }()
	table, err := tabular.Read(file, tabular.Limits{MaxBytes: 5 << 20, MaxRows: 5000, MaxCols: 8})
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", err.Error()))
		return
	}
	if table.Format != tabular.FormatXLSX {
		httpx.Fail(c, problem.Validation.WithField("file", "chỉ nhận Excel .xlsx"))
		return
	}
	result, err := h.Svc.ImportAppraisalProposals(c.Request.Context(), period, appraisalIdem(c), version, table)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
func (h Handler) appraisalImportTemplate(c *gin.Context) {
	period, ok := uuidParam(c, "period", port.ErrAppraisalNotFound)
	if !ok {
		return
	}
	data, err := h.Svc.AppraisalImportTemplate(c.Request.Context(), period, c.Param("kind"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="Nhap-%s-%s.xlsx"`, c.Param("kind"), period))
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, data)
}
