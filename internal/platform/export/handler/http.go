// Package handler: Application API /v1/exports/* của pf/export — màn quản trị mẫu văn bản và lịch sử xuất (chỉ đọc).
// Tạo lần xuất không có route chung: bên dùng gọi port.Exporter sau khi tự kiểm quyền nghiệp vụ.
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// Handler gắn route.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn /v1/exports/*.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/exports")
	g.GET("/templates", h.templates)
	g.GET("/runs", h.runs)
	g.GET("/runs/:id/download", h.download)
}

type versionDTO struct {
	Version    int        `json:"version"`
	Hash       string     `json:"hash"`
	Registered bool       `json:"registered"`
	RecordedAt *time.Time `json:"recorded_at"`
	Drift      bool       `json:"drift"`
}

type templateDTO struct {
	Key      string        `json:"key"`
	Title    string        `json:"title"`
	Owner    string        `json:"owner"`
	Latest   int           `json:"latest"`
	Vars     []doctpl.Var  `json:"vars"`
	Lists    []doctpl.List `json:"lists"`
	Sections []string      `json:"sections"`
	Versions []versionDTO  `json:"versions"`
}

// RunDTO là lần xuất (không byte) — dùng chung cho bên dùng hiện lịch sử.
type RunDTO struct {
	ID              uuid.UUID `json:"id"`
	Seq             int64     `json:"seq"`
	Subject         string    `json:"subject"`
	Template        string    `json:"template"`
	TemplateVersion int       `json:"template_version"`
	TemplateHash    string    `json:"template_hash"`
	InputHash       string    `json:"input_hash"`
	DocNumber       string    `json:"doc_number"`
	DocNumberAuto   bool      `json:"doc_number_auto"`
	FileName        string    `json:"file_name"`
	BodyHash        string    `json:"body_hash"`
	Size            int64     `json:"size"`
	ActorRef        uuid.UUID `json:"actor_ref"`
	At              time.Time `json:"at"`
}

// RunOf chuyển port.Run sang DTO.
func RunOf(r port.Run) RunDTO {
	return RunDTO{ID: r.ID, Seq: r.Seq, Subject: r.Subject, Template: r.Template, TemplateVersion: r.TemplateVersion,
		TemplateHash: r.TemplateHash, InputHash: r.InputHash, DocNumber: r.DocNumber, DocNumberAuto: r.DocNumberAuto,
		FileName: r.FileName, BodyHash: r.BodyHash, Size: r.Size, ActorRef: r.ActorRef, At: r.At}
}

func (h Handler) templates(c *gin.Context) {
	xs, err := h.Svc.Templates(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]templateDTO, 0, len(xs))
	for _, x := range xs {
		d := templateDTO{Key: x.Key, Title: x.Title, Owner: x.Owner, Latest: x.Latest, Vars: x.Vars, Lists: x.Lists, Sections: x.Sections, Versions: []versionDTO{}}
		if d.Vars == nil {
			d.Vars = []doctpl.Var{}
		}
		if d.Lists == nil {
			d.Lists = []doctpl.List{}
		}
		if d.Sections == nil {
			d.Sections = []string{}
		}
		for _, v := range x.Versions {
			d.Versions = append(d.Versions, versionDTO(v))
		}
		items = append(items, d)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) runs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	xs, err := h.Svc.AdminRuns(c.Request.Context(), c.Query("template"), c.Query("subject"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]RunDTO, 0, len(xs))
	for _, x := range xs {
		items = append(items, RunOf(x))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) download(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, port.ErrRunNotFound)
		return
	}
	r, err := h.Svc.AdminRun(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	Attachment(c, r)
}

// Attachment trả byte .docx của lần xuất (tên tệp đã kiểm định dạng ở schema).
func Attachment(c *gin.Context, r port.Run) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+r.FileName+`"`)
	c.Header("X-Content-SHA256", r.BodyHash)
	c.Data(http.StatusOK, r.ContentType, r.Body)
}
