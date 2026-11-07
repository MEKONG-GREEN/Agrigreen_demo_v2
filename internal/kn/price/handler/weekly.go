package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// WeeklyHandler exposes FN-TI-02.13, with the same decimal-string contract for manual and file input.
type WeeklyHandler struct{ Svc *app.WeeklyService }

// RegisterApp registers only the two program keys, validated again by the service.
func (h WeeklyHandler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/weekly-programs/:program", h.read)
	rg.POST("/price/weekly-programs/:program/actions", h.write)
	rg.GET("/price/weekly-programs/:program/export", h.export)
	rg.GET("/price/weekly-programs/:program/template", h.template)
	rg.GET("/price/weekly-programs/:program/imports/issues", h.issues)
	for _, suffix := range []string{"preview", "confirm"} {
		httpx.AllowBody(rg, http.MethodPost, "/price/weekly-programs/:program/imports/"+suffix, 5<<20+128<<10)
	}
	rg.POST("/price/weekly-programs/:program/imports/preview", h.preview)
	rg.POST("/price/weekly-programs/:program/imports/confirm", h.commit)
}

func (h WeeklyHandler) read(c *gin.Context) {
	v, err := h.Svc.Read(c.Request.Context(), c.Query("profile"), c.Param("program"), c.Query("week_id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func weeklyKey(c *gin.Context) string {
	key := idempotency.KeyFromContext(c.Request.Context())
	if key == "" {
		key = c.GetHeader(idempotency.Header)
	}
	return key
}

func (h WeeklyHandler) write(c *gin.Context) {
	if err := h.Svc.AuthorizeWeekly(c.Request.Context(), c.Query("profile")); err != nil {
		httpx.Fail(c, err)
		return
	}
	var input app.WeeklyCommand
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ; số liệu phải là chuỗi thập phân"))
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		httpx.Fail(c, problem.Validation.WithDetail("chỉ nhận một đối tượng JSON"))
		return
	}
	if input.Action == "import" {
		httpx.Fail(c, problem.Validation.WithField("action", "nhập Excel qua bước xem trước và ghi nhận"))
		return
	}
	v, err := h.Svc.Write(c.Request.Context(), c.Query("profile"), c.Param("program"), weeklyKey(c), input)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h WeeklyHandler) export(c *gin.Context) {
	x, err := h.Svc.ExportWeekly(c.Request.Context(), c.Query("profile"), c.Param("program"), c.Query("week_id"), false)
	sendFile(c, x, err)
}
func (h WeeklyHandler) template(c *gin.Context) {
	x, err := h.Svc.ExportWeekly(c.Request.Context(), c.Query("profile"), c.Param("program"), c.Query("week_id"), true)
	sendFile(c, x, err)
}
func (h WeeklyHandler) issues(c *gin.Context) {
	x, err := h.Svc.ExportIssues(c.Request.Context(), c.Query("profile"), c.Param("program"), c.Query("week_id"))
	sendFile(c, x, err)
}

func (h WeeklyHandler) upload(c *gin.Context) (version int64, data []byte, ok bool) {
	if err := h.Svc.AuthorizeWeekly(c.Request.Context(), c.Query("profile")); err != nil {
		httpx.Fail(c, err)
		return 0, nil, false
	}
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "tệp Excel/CSV tối đa 5 MB"))
		return 0, nil, false
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "cần chọn tệp .xlsx hoặc .csv"))
		return 0, nil, false
	}
	defer func() { _ = file.Close() }()
	data, err = io.ReadAll(io.LimitReader(file, 5<<20+1))
	if err != nil || len(data) > 5<<20 {
		httpx.Fail(c, problem.Validation.WithField("file", "tệp tối đa 5 MB"))
		return 0, nil, false
	}
	version, err = strconv.ParseInt(c.PostForm("version"), 10, 64)
	if err != nil || version < 1 {
		httpx.Fail(c, problem.Validation.WithField("version", "cần phiên bản đang xem"))
		return 0, nil, false
	}
	if c.PostForm("week_id") == "" {
		httpx.Fail(c, problem.Validation.WithField("week_id", "cần kỳ báo cáo đang xem"))
		return 0, nil, false
	}
	return version, data, true
}

func (h WeeklyHandler) preview(c *gin.Context) {
	version, data, ok := h.upload(c)
	if !ok {
		return
	}
	v, err := h.Svc.PreviewImport(c.Request.Context(), c.Query("profile"), c.Param("program"), c.PostForm("week_id"), version, data)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h WeeklyHandler) commit(c *gin.Context) {
	version, data, ok := h.upload(c)
	if !ok {
		return
	}
	v, err := h.Svc.CommitImport(c.Request.Context(), c.Query("profile"), c.Param("program"), c.PostForm("week_id"), weeklyKey(c), version, c.PostForm("sha256"), c.PostForm("confirm_overwrite") == "true", data)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
