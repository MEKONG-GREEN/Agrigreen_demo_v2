package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docfile"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerFiles gắn route tệp chứng từ vùng mkg (G4): POST multipart (file + sha256 + purpose) → file_ref.
func (h Handler) registerFiles(rg *gin.RouterGroup) {
	httpx.AllowBody(rg, http.MethodPost, "/orders/files", docfile.MaxBytes+64<<10)
	rg.POST("/orders/files", h.uploadFile)
	rg.GET("/orders/files/:id", h.getFile)
	rg.GET("/orders/files/:id/content", h.fileContent)
}

type fileDTO struct {
	ID          string `json:"id"`
	Purpose     string `json:"purpose"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	UploadedBy  string `json:"uploaded_by"`
	RuleVersion string `json:"rule_version"`
	RecordedAt  string `json:"recorded_at"`
}

func toFile(f store.File) fileDTO {
	return fileDTO{ID: f.ID.String(), Purpose: f.Purpose, FileName: f.FileName, ContentType: f.ContentType, Size: f.Size, SHA256: f.SHA256,
		UploadedBy: f.UploadedBy.String(), RuleVersion: f.RuleVersion, RecordedAt: f.RecordedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00")}
}

func (h Handler) uploadFile(c *gin.Context) {
	data, name, err := docfile.ReadMultipart(c.Request)
	if err != nil {
		httpx.Fail(c, app.FileProblem(err))
		return
	}
	f, replayed, err := h.Svc.UploadFile(c.Request.Context(), app.UploadInput{Data: data, FileName: name, SHA256: c.PostForm("sha256"),
		Purpose: c.PostForm("purpose"), IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"file": toFile(f)})
}

func (h Handler) getFile(c *gin.Context) {
	id, ok := pathID(c, app.ErrFileNotFound)
	if !ok {
		return
	}
	f, err := h.Svc.File(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"file": toFile(f)})
}

func (h Handler) fileContent(c *gin.Context) {
	id, ok := pathID(c, app.ErrFileNotFound)
	if !ok {
		return
	}
	rc, f, err := h.Svc.OpenFile(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = rc.Close() }()
	docfile.WriteContent(c.Writer, rc, f.ContentType, f.Size, f.SHA256, f.FileName)
}
