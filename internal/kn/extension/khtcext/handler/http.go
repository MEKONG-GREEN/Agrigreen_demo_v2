// Package handler: HTTP của "Số liệu nguồn ngoài" KHTC (W11.67, FN-TI-02.11) — /v1/extension/khtc/external/*.
// Tệp nạp và tệp minh chứng gửi thẳng thân request (không multipart), tên tệp ở query file_name.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Handler gắn route của số liệu nguồn ngoài.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API (cần header X-AG-Context).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/khtc/external")
	httpx.AllowBody(g, http.MethodPost, "/evidence", domain.MaxEvidenceSize+1024)
	httpx.AllowBody(g, http.MethodPost, "/imports", domain.MaxImportBytes+1024)
	g.GET("/indicators", h.indicators)
	g.POST("/indicators", h.addIndicator)
	g.GET("/figures", h.list)
	g.POST("/figures", h.save)
	g.POST("/figures/withdraw", h.withdraw)
	g.GET("/history", h.history)
	g.GET("/template", h.template)
	g.POST("/imports", h.stage)
	g.GET("/imports/:importID", h.getImport)
	g.POST("/imports/:importID/commit", h.commit)
	g.POST("/evidence", h.upload)
	g.GET("/evidence/:evidenceID", h.download)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bind(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân JSON hoặc trường nhập không hợp lệ"))
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ gửi một đối tượng JSON"))
		return false
	}
	return true
}

func pathID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func year(c *gin.Context) (int, bool) {
	y, err := strconv.Atoi(c.Query("year"))
	if err != nil {
		httpx.Fail(c, problem.BadRequest.WithField("year", "năm kế hoạch bắt buộc"))
		return 0, false
	}
	return y, true
}

func optPeriod(c *gin.Context) (*uuid.UUID, bool) {
	raw := c.Query("period_id")
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.Fail(c, problem.BadRequest.WithField("period_id", "mã kỳ không hợp lệ"))
		return nil, false
	}
	return &id, true
}

func readBody(c *gin.Context) ([]byte, bool) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, problem.PayloadTooLarge)
			return nil, false
		}
		httpx.Fail(c, problem.BadRequest.WithDetail("không đọc được thân request"))
		return nil, false
	}
	return raw, true
}

type indicatorDTO struct {
	ID    uuid.UUID `json:"id"`
	Code  string    `json:"code"`
	Name  string    `json:"name"`
	Unit  string    `json:"unit"`
	Group string    `json:"group"`
	At    time.Time `json:"created_at"`
}

func indicatorOf(i store.Indicator) indicatorDTO {
	return indicatorDTO{ID: i.ID, Code: i.Code, Name: i.Name, Unit: i.Unit, Group: i.Group, At: i.At}
}

func (h Handler) indicators(c *gin.Context) {
	list, err := h.Svc.Indicators(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]indicatorDTO, 0, len(list))
	for _, i := range list {
		items = append(items, indicatorOf(i))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) addIndicator(c *gin.Context) {
	var in domain.IndicatorInput
	if !bind(c, &in) {
		return
	}
	row, err := h.Svc.AddIndicator(c.Request.Context(), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, indicatorOf(row))
}

func (h Handler) list(c *gin.Context) {
	y, ok := year(c)
	if !ok {
		return
	}
	p, ok := optPeriod(c)
	if !ok {
		return
	}
	items, err := h.Svc.List(c.Request.Context(), app.Query{Year: y, PeriodID: p, Agency: c.Query("agency"), Search: c.Query("q")})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) save(c *gin.Context) {
	var in app.SaveInput
	if !bind(c, &in) {
		return
	}
	if in.Evidence == nil {
		in.Evidence = []uuid.UUID{}
	}
	v, err := h.Svc.Save(c.Request.Context(), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h Handler) withdraw(c *gin.Context) {
	var in app.WithdrawInput
	if !bind(c, &in) {
		return
	}
	if err := h.Svc.Withdraw(c.Request.Context(), in, idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h Handler) history(c *gin.Context) {
	y, ok := year(c)
	if !ok {
		return
	}
	p, ok := optPeriod(c)
	if !ok {
		return
	}
	items, err := h.Svc.History(c.Request.Context(), y, p, 200)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) template(c *gin.Context) {
	y, ok := year(c)
	if !ok {
		return
	}
	body, name, err := h.Svc.Template(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"; filename*=UTF-8''`+url.PathEscape(name))
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, body)
}

func (h Handler) stage(c *gin.Context) {
	y, ok := year(c)
	if !ok {
		return
	}
	raw, ok := readBody(c)
	if !ok {
		return
	}
	v, err := h.Svc.Stage(c.Request.Context(), y, c.Query("file_name"), raw, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h Handler) getImport(c *gin.Context) {
	id, ok := pathID(c, "importID")
	if !ok {
		return
	}
	v, err := h.Svc.GetImport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h Handler) commit(c *gin.Context) {
	id, ok := pathID(c, "importID")
	if !ok {
		return
	}
	var in app.CommitInput
	if !bind(c, &in) {
		return
	}
	v, err := h.Svc.Commit(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

type evidenceDTO struct {
	ID          uuid.UUID `json:"id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	At          time.Time `json:"created_at"`
}

func (h Handler) upload(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	ev, replayed, err := h.Svc.UploadEvidence(c.Request.Context(), c.Query("file_name"), raw, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, evidenceDTO{ID: ev.ID, FileName: ev.FileName, ContentType: ev.ContentType, Size: ev.Size, SHA256: ev.SHA256, At: ev.At})
}

func (h Handler) download(c *gin.Context) {
	id, ok := pathID(c, "evidenceID")
	if !ok {
		return
	}
	ev, body, err := h.Svc.OpenEvidence(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", `attachment; filename*=UTF-8''`+url.PathEscape(ev.FileName))
	c.Data(http.StatusOK, ev.ContentType, body)
}
