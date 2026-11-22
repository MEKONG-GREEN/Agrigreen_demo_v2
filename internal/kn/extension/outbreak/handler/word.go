package handler

// BF-CN-OUTBREAK (PRD UP-08): Word BM-10 của ổ dịch qua pf/export — tạo (cấp số "{n}/BC-TTKN"), lịch sử, tải lại.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type wordDTO struct {
	ID              uuid.UUID `json:"id"`
	Seq             int64     `json:"seq"`
	Template        string    `json:"template"`
	TemplateVersion int       `json:"template_version"`
	DocNumber       string    `json:"doc_number"`
	DocNumberAuto   bool      `json:"doc_number_auto"`
	FileName        string    `json:"file_name"`
	BodyHash        string    `json:"body_hash"`
	Size            int64     `json:"size"`
	Actor           uuid.UUID `json:"actor_ref"`
	At              time.Time `json:"at"`
}

func wordOf(r exportport.Run) wordDTO {
	return wordDTO{ID: r.ID, Seq: r.Seq, Template: r.Template, TemplateVersion: r.TemplateVersion, DocNumber: r.DocNumber,
		DocNumberAuto: r.DocNumberAuto, FileName: r.FileName, BodyHash: r.BodyHash, Size: r.Size, Actor: r.ActorRef, At: r.At.UTC()}
}

func (h Handler) exportWord(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	run, err := h.Svc.ExportWord(c.Request.Context(), ref, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, wordOf(run))
}

func (h Handler) wordHistory(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	rows, err := h.Svc.WordHistory(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]wordDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, wordOf(r))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) wordDownload(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	run, err := uuid.Parse(c.Param("run"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("run", "phải là UUID"))
		return
	}
	r, err := h.Svc.WordDownload(c.Request.Context(), ref, run)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+r.FileName+`"`)
	c.Header("X-Content-SHA256", r.BodyHash)
	c.Data(http.StatusOK, r.ContentType, r.Body)
}
