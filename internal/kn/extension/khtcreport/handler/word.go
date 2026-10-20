package handler

import (
	"net/http"
	"time"

	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type wordDTO struct {
	ID              uuid.UUID `json:"id"`
	Seq             int64     `json:"seq"`
	Template        string    `json:"template"`
	TemplateVersion int       `json:"template_version"`
	TemplateHash    string    `json:"template_hash"`
	InputHash       string    `json:"input_hash"`
	DocNumber       string    `json:"doc_number"`
	DocNumberAuto   bool      `json:"doc_number_auto"`
	FileName        string    `json:"file_name"`
	BodyHash        string    `json:"body_hash"`
	Size            int64     `json:"size"`
	Actor           uuid.UUID `json:"actor_ref"`
	At              time.Time `json:"at"`
}

func wordOf(r exportport.Run) wordDTO {
	return wordDTO{ID: r.ID, Seq: r.Seq, Template: r.Template, TemplateVersion: r.TemplateVersion, TemplateHash: r.TemplateHash, InputHash: r.InputHash,
		DocNumber: r.DocNumber, DocNumberAuto: r.DocNumberAuto, FileName: r.FileName, BodyHash: r.BodyHash, Size: r.Size, Actor: r.ActorRef, At: r.At}
}

func (h Handler) exportWord(c *gin.Context) {
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
	run, err := h.Svc.ExportWord(c.Request.Context(), id, exportID, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, wordOf(run))
}

func (h Handler) wordHistory(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	rows, err := h.Svc.WordHistory(c.Request.Context(), id)
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
	id, ok := path(c, "id")
	if !ok {
		return
	}
	runID, ok := path(c, "runID")
	if !ok {
		return
	}
	run, err := h.Svc.WordDownload(c.Request.Context(), id, runID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+run.FileName+`"`)
	c.Header("X-Content-SHA256", run.BodyHash)
	c.Data(http.StatusOK, run.ContentType, run.Body)
}
