// Package handler: route /v1/extension/cnts/knqg-specs, /knqg-docs… (W11.53 — mẫu biểu dự án KNQG Mẫu 01, 03, 04, 05,
// 07; FN-19, UR-TINH-20…24, UR-QG-01). Mọi route cần header X-AG-Context (ngữ cảnh phiên KN); thao tác ghi (POST) cần
// Idempotency-Key. Sửa là POST lên tài nguyên kèm base_version; chuyển trạng thái là POST …/actions/{action}; ký là POST
// …/signed (multipart: meta {"base_version"}, file — PDF/JPG/PNG ≤ 10 MB).
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

const maxUpload = domain.MaxScanBytes + 64<<10

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts")
	g.GET("/knqg-specs", h.specs)
	g.GET("/knqg-docs", h.list)
	g.POST("/knqg-docs", h.create)
	g.GET("/knqg-docs/:ref", h.get)
	g.POST("/knqg-docs/:ref", h.update)
	g.POST("/knqg-docs/:ref/actions/:action", h.act)
	httpx.AllowBody(g, http.MethodPost, "/knqg-docs/:ref/signed", maxUpload)
	g.POST("/knqg-docs/:ref/signed", h.sign)
	g.GET("/knqg-docs/:ref/signed", h.scan)
	// W11.53b: ảnh thực địa Mẫu 03 (multipart: file — JPG/PNG ≤ 5 MB, caption tùy chọn).
	g.GET("/knqg-docs/:ref/photos", h.photos)
	httpx.AllowBody(g, http.MethodPost, "/knqg-docs/:ref/photos", maxPhotoUpload)
	g.POST("/knqg-docs/:ref/photos", h.addPhoto)
	g.GET("/knqg-docs/:ref/photos/:photo", h.photo)
	g.POST("/knqg-docs/:ref/photos/:photo/remove", h.removePhoto)
	g.POST("/knqg-docs/:ref/word", h.exportWord)
	g.GET("/knqg-docs/:ref/words", h.wordHistory)
	g.GET("/knqg-docs/:ref/words/:run/download", h.wordDownload)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bind(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, app.ErrDocInvalid.WithDetail("thân yêu cầu quá lớn"))
			return false
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ: "+err.Error()))
		return false
	}
	return true
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	r, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrDocNotFound)
		return uuid.Nil, false
	}
	return r, true
}

func optStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optRef(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

func nameOf(m map[uuid.UUID]string, r uuid.UUID) any {
	if n, ok := m[r]; ok && n != "" {
		return n
	}
	return nil
}

func contentOut(ct domain.Content) gin.H {
	fields := ct.Fields
	if fields == nil {
		fields = map[string]string{}
	}
	tables := map[string][]domain.Row{}
	for k, rows := range ct.Tables {
		if rows == nil {
			rows = []domain.Row{}
		}
		tables[k] = rows
	}
	return gin.H{"fields": fields, "tables": tables}
}

func scanOut(s *store.Scan) any {
	if s == nil {
		return nil
	}
	return gin.H{"name": s.Name, "content_type": s.Type, "size": s.Size, "sha256": s.SHA256}
}

func issuesOut(es []domain.FieldErr) []gin.H {
	out := []gin.H{}
	for _, e := range es {
		out = append(out, gin.H{"field": e.Field, "reason": e.Reason})
	}
	return out
}

func strs(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

func docOut(v app.DocView, names map[uuid.UUID]string, detail bool) gin.H {
	o := gin.H{"ref": v.Ref, "form": v.Form, "project_ref": v.ProjectRef, "project_code": v.ProjectCode, "project_name": v.ProjectName,
		"site_ref": optRef(v.SiteRef), "site_name": optStr(v.SiteName), "commune_code": optStr(v.CommuneCode), "period": optStr(v.Period),
		"revision": v.Revision, "event": v.Event, "status": v.Status, "status_reason": optStr(v.StatusReason),
		"signed": scanOut(v.Scan), "version": v.Seq, "updated_at": v.EventTime.UTC(), "updated_by": v.ActorRef,
		"actor_display": nameOf(names, v.ActorRef), "can_edit": v.CanEdit, "actions": strs(v.Actions), "warnings": strs(v.Warnings)}
	if !detail {
		return o
	}
	o["content"] = contentOut(v.Content)
	o["issues"] = issuesOut(v.Issues)
	o["blockers"] = strs(v.Blockers)
	vs := []gin.H{}
	for _, x := range v.Versions {
		vs = append(vs, gin.H{"version": x.Seq, "revision": x.Revision, "event": x.Event, "status": x.Status,
			"status_reason": optStr(x.StatusReason), "signed": scanOut(x.Scan), "at": x.EventTime.UTC(), "actor_ref": x.ActorRef,
			"actor_display": nameOf(names, x.ActorRef)})
	}
	o["versions"] = vs
	if v.Replayed {
		o["replayed"] = true
	}
	return o
}

func (h Handler) specs(c *gin.Context) {
	ss, err := h.Svc.Specs(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": ss})
}

func (h Handler) list(c *gin.Context) {
	q := app.ListQuery{Form: c.Query("form"), Status: c.Query("status")}
	if v := c.Query("project_ref"); v != "" {
		r, err := uuid.Parse(v)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("project_ref", "phải là UUID"))
			return
		}
		q.Project = &r
	}
	items, names, err := h.Svc.List(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, v := range items {
		out = append(out, docOut(v, names, false))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) get(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	v, err := h.Svc.Get(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, docOut(v, v.Names, true))
}

type createIn struct {
	ID         *string `json:"id"`
	Form       string  `json:"form"`
	ProjectRef string  `json:"project_ref"`
	SiteRef    *string `json:"site_ref"`
	Period     *string `json:"period"`
}

func (h Handler) create(c *gin.Context) {
	var in createIn
	if !bind(c, &in) {
		return
	}
	pr, err := uuid.Parse(in.ProjectRef)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("project_ref", "phải là UUID"))
		return
	}
	x := app.CreateInput{Form: in.Form, Project: pr, IdemKey: idem(c)}
	if in.ID != nil && *in.ID != "" {
		r, err := uuid.Parse(*in.ID)
		if err != nil || r.Version() != 7 {
			httpx.Fail(c, problem.Validation.WithField("id", "phải là UUIDv7"))
			return
		}
		x.Ref = &r
	}
	if in.SiteRef != nil && *in.SiteRef != "" {
		r, err := uuid.Parse(*in.SiteRef)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("site_ref", "phải là UUID"))
			return
		}
		x.Site = &r
	}
	if in.Period != nil {
		x.Period = *in.Period
	}
	v, err := h.Svc.Create(c.Request.Context(), x)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	code := http.StatusCreated
	if v.Replayed {
		code = http.StatusOK
	}
	c.JSON(code, docOut(v, v.Names, true))
}

type updateIn struct {
	BaseVersion *int64         `json:"base_version"`
	Content     domain.Content `json:"content"`
}

func (h Handler) update(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var in updateIn
	if !bind(c, &in) {
		return
	}
	v, err := h.Svc.Update(c.Request.Context(), app.UpdateInput{Ref: ref, BaseVersion: in.BaseVersion, Content: in.Content, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, docOut(v, v.Names, true))
}

type actIn struct {
	BaseVersion *int64 `json:"base_version"`
	Reason      string `json:"reason"`
}

func (h Handler) act(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var in actIn
	if !bind(c, &in) {
		return
	}
	v, err := h.Svc.Act(c.Request.Context(), app.ActInput{Ref: ref, Action: c.Param("action"), BaseVersion: in.BaseVersion, Reason: in.Reason,
		IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, docOut(v, v.Names, true))
}

type signMeta struct {
	BaseVersion *int64 `json:"base_version"`
}

func (h Handler) sign(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	if err := c.Request.ParseMultipartForm(maxUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, app.ErrScanInvalid)
			return
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân phải là multipart/form-data có phần meta và file"))
		return
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	var meta signMeta
	dec := json.NewDecoder(strings.NewReader(c.Request.FormValue("meta")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&meta); err != nil {
		httpx.Fail(c, problem.Validation.WithField("meta", "JSON {\"base_version\": n}"))
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "bắt buộc"))
		return
	}
	if fh.Size <= 0 || fh.Size > domain.MaxScanBytes {
		httpx.Fail(c, app.ErrScanInvalid)
		return
	}
	fr, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = fr.Close() }()
	data, err := io.ReadAll(io.LimitReader(fr, domain.MaxScanBytes+1))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, err := h.Svc.SignScan(c.Request.Context(), app.SignInput{Ref: ref, BaseVersion: meta.BaseVersion, FileName: fh.Filename, Data: data,
		IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, docOut(v, v.Names, true))
}

func (h Handler) scan(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var seq int64
	if v := c.Query("version"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			httpx.Fail(c, problem.Validation.WithField("version", "số nguyên dương"))
			return
		}
		seq = n
	}
	f, err := h.Svc.Scan(c.Request.Context(), ref, seq)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = f.Body.Close() }()
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename*=UTF-8''`+urlEscape(f.Name))
	c.Header("X-Content-SHA256", f.SHA256)
	c.DataFromReader(http.StatusOK, f.Size, f.Type, f.Body, nil)
}

// urlEscape mã hóa tên tệp cho filename* (RFC 5987).
func urlEscape(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.IndexByte("-._~", r) >= 0:
			b.WriteByte(r)
		default:
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(r)>>4, 16)) + strings.ToUpper(strconv.FormatInt(int64(r)&15, 16)))
		}
	}
	return b.String()
}

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
	run, err := h.Svc.ExportWord(c.Request.Context(), ref, idem(c))
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
