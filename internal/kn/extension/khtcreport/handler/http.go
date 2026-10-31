package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ Svc *app.Service }

func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/extension/khtc/exported-reports", h.archive)
	g := rg.Group("/extension/khtc/periods/:id/report")
	g.GET("", h.get)
	g.POST("/draft", h.save)
	g.POST("/refresh", h.refresh)
	g.GET("/exports", h.history)
	g.POST("/exports", h.export)
	g.GET("/exports/:exportID/download", h.download)
	g.POST("/exports/:exportID/final", h.finalize)
	// W10.17: Word (.docx) qua pf/export, dựng từ nội dung đóng băng của một bản Excel.
	g.POST("/exports/:exportID/word", h.exportWord)
	g.GET("/words", h.wordHistory)
	g.GET("/words/:runID/download", h.wordDownload)
}
func path(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}
func bind(c *gin.Context, value any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân JSON hoặc trường nhập không hợp lệ"))
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ gửi một đối tượng JSON"))
		return false
	}
	return true
}
func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

type finalDTO struct {
	ExportID uuid.UUID `json:"export_id"`
	Actor    uuid.UUID `json:"actor_ref"`
	At       time.Time `json:"at"`
}
type detailDTO struct {
	PeriodID      uuid.UUID                `json:"period_id"`
	DraftID       *uuid.UUID               `json:"draft_id"`
	Version       int64                    `json:"version"`
	Content       domain.Draft             `json:"content"`
	Sections      []domain.Section         `json:"sections"`
	Final         *finalDTO                `json:"final"`
	Editable      bool                     `json:"editable"`
	CanRefresh    bool                     `json:"can_refresh"`
	SourceChanged bool                     `json:"source_changed"`
	SourceError   string                   `json:"source_error"`
	Terminal      string                   `json:"terminal"`
	Document      *templatedomain.Rendered `json:"template_document,omitempty"`
	LockedAdmin   []string                 `json:"locked_admin,omitempty"`
}

func detailOf(d app.Detail) detailDTO {
	out := detailDTO{PeriodID: d.PeriodID, DraftID: d.DraftID, Version: d.Version, Content: d.Content, Sections: d.Sections, Document: d.Document, LockedAdmin: d.LockedAdmin, Editable: d.Editable, CanRefresh: d.CanRefresh, SourceChanged: d.SourceChanged, SourceError: d.SourceError, Terminal: d.Terminal}
	if d.Final != nil {
		out.Final = &finalDTO{ExportID: d.Final.ExportID, Actor: d.Final.ActorRef, At: d.Final.At}
	}
	return out
}

type exportDTO struct {
	ID          uuid.UUID     `json:"id"`
	PeriodID    uuid.UUID     `json:"period_id"`
	Version     int64         `json:"version"`
	DraftID     uuid.UUID     `json:"draft_id"`
	FileName    string        `json:"file_name"`
	BodyHash    string        `json:"body_hash"`
	InputHash   string        `json:"input_hash"`
	Provisional bool          `json:"provisional"`
	Filter      domain.Filter `json:"filter"`
	Actor       uuid.UUID     `json:"actor_ref"`
	At          time.Time     `json:"at"`
}

func exportOf(x store.Export) exportDTO {
	return exportDTO{ID: x.ID, PeriodID: x.PeriodID, Version: x.Version, DraftID: x.DraftID, FileName: x.FileName, BodyHash: x.BodyHash, InputHash: x.InputHash, Provisional: x.Provisional, Filter: x.Filter, Actor: x.ActorRef, At: x.At}
}
func (h Handler) get(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	d, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, detailOf(d))
}
func (h Handler) save(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	var in app.SaveInput
	if !bind(c, &in) {
		return
	}
	d, err := h.Svc.Save(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, detailOf(d))
}
func (h Handler) refresh(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	var in struct {
		BaseVersion int64 `json:"base_version"`
	}
	if !bind(c, &in) {
		return
	}
	d, err := h.Svc.Refresh(c.Request.Context(), id, in.BaseVersion, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, detailOf(d))
}
func (h Handler) history(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	rows, err := h.Svc.History(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]exportDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, exportOf(row))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}
func (h Handler) export(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	var in app.ExportInput
	if !bind(c, &in) {
		return
	}
	row, err := h.Svc.Export(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, exportOf(row))
}
func (h Handler) download(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	exportID, ok := path(c, "exportID")
	if !ok {
		return
	}
	row, err := h.Svc.Download(c.Request.Context(), id, exportID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+row.FileName+`"`)
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, row.Body)
}
func (h Handler) finalize(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	exportID, ok := path(c, "exportID")
	if !ok {
		return
	}
	var in struct{}
	if !bind(c, &in) {
		return
	}
	d, err := h.Svc.Finalize(c.Request.Context(), id, exportID, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, detailOf(d))
}
