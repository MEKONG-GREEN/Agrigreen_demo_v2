// Package handler: route /v1/extension/dept-home…, /v1/extension/dept-docs… (W11.54 — Trang chủ phòng + kho văn bản
// phòng, D4 CN14). Mọi route cần header X-AG-Context (ngữ cảnh phiên KN); Admin chọn phòng bằng tham số department.
// Thao tác ghi cần Idempotency-Key. Tệp tải lên là multipart (phần meta JSON + phần file), ≤ 20 MB.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// maxUpload: thân multipart tối đa (tệp 20 MB + thuộc tính + biên).
const maxUpload = domain.MaxFileBytes + 64<<10

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension")
	g.GET("/dept-home", h.home)
	g.POST("/dept-home/profile", h.saveProfile)
	g.GET("/dept-home/profile/history", h.profileHistory)
	g.GET("/dept-docs/folders", h.folders)
	g.POST("/dept-docs/folders", h.createFolder)
	g.POST("/dept-docs/folders/:ref/rename", h.changeFolder(domain.FolderRenamed))
	g.POST("/dept-docs/folders/:ref/hide", h.changeFolder(domain.FolderHidden))
	g.POST("/dept-docs/folders/:ref/restore", h.changeFolder(domain.FolderRestored))
	g.GET("/dept-docs", h.listDocs)
	httpx.AllowBody(g, http.MethodPost, "/dept-docs", maxUpload)
	g.POST("/dept-docs", h.createDoc)
	g.GET("/dept-docs/:ref", h.getDoc)
	g.POST("/dept-docs/:ref/update", h.updateDoc)
	httpx.AllowBody(g, http.MethodPost, "/dept-docs/:ref/file", maxUpload)
	g.POST("/dept-docs/:ref/file", h.replaceFile)
	g.POST("/dept-docs/:ref/hide", h.hideDoc(true))
	g.POST("/dept-docs/:ref/restore", h.hideDoc(false))
	g.GET("/dept-docs/:ref/content", h.content)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func dept(c *gin.Context) string { return c.Query("department") }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func optTS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func optDay(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.DateOnly)
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

func decode(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ: "+err.Error()))
		return false
	}
	return true
}

func refParam(c *gin.Context, notFound *problem.Problem) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("ref"))
	if err != nil || id == uuid.Nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func actor(ref uuid.UUID, name string) gin.H {
	return gin.H{"ref": ref.String(), "display": optStr(name)}
}

// --- Trang chủ. ---

func profileOut(p app.ProfileView) gin.H {
	return gin.H{"department_code": p.Department, "intro": p.Intro, "duties": p.Duties, "version": p.Seq, "recorded_at": ts(p.RecordedAt),
		"actor": actor(p.ActorRef, p.ActorName)}
}

func (h Handler) home(c *gin.Context) {
	r, err := h.Svc.GetHome(c.Request.Context(), dept(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	kpis := make([]gin.H, 0, len(r.KPIs))
	for _, k := range r.KPIs {
		kpis = append(kpis, gin.H{"key": k.Key, "label": k.Label, "value": k.Value, "tone": k.Tone})
	}
	closed := make([]gin.H, 0, len(r.Closed))
	for _, p := range r.Closed {
		closed = append(closed, gin.H{"form_code": p.FormCode, "period_type": p.PeriodType, "period_key": p.PeriodKey, "period_label": p.PeriodLabel,
			"period_to": optDay(p.PeriodTo), "units_total": p.UnitsTotal, "units_with_data": p.UnitsWithData, "closed": p.Closed,
			"closed_at": optTS(p.ClosedAt)})
	}
	officers := make([]gin.H, 0, len(r.Officers))
	for _, o := range r.Officers {
		officers = append(officers, gin.H{"actor": actor(o.ActorRef, o.Name), "mgmt_level": optStr(o.MgmtLevel), "leader": o.Leader})
	}
	docs := make([]gin.H, 0, len(r.LatestDocs))
	for _, d := range r.LatestDocs {
		docs = append(docs, docOut(d))
	}
	var profile, price any
	if r.Profile != nil {
		profile = profileOut(*r.Profile)
	}
	if r.Price != nil {
		price = gin.H{"board_date": optDay(r.Price.BoardDate), "items": r.Price.Items, "week_key": optStr(r.Price.WeekKey),
			"tickets": r.Price.Tickets, "week_days_approved": r.Price.WeekDays}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"department_code": r.Department, "as_of": ts(r.AsOf), "manager": r.Manager, "read_only": r.ReadOnly,
		"can_edit_profile": r.CanEditProfile, "profile": profile, "kpis": kpis, "closed_periods": closed, "price": price,
		"officers": officers, "library": gin.H{"total": r.Library.Total, "expired": r.Library.Expired, "new_30d": r.Library.New30},
		"latest_docs": docs})
}

type profileIn struct {
	Intro       string `json:"intro"`
	Duties      string `json:"duties"`
	BaseVersion int64  `json:"base_version"`
}

func (h Handler) saveProfile(c *gin.Context) {
	var in profileIn
	if !decode(c, &in) {
		return
	}
	r, err := h.Svc.SaveProfile(c.Request.Context(), app.ProfileInput{Department: dept(c), Intro: in.Intro, Duties: in.Duties, BaseVersion: in.BaseVersion}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, profileOut(r))
}

func (h Handler) profileHistory(c *gin.Context) {
	rs, err := h.Svc.ProfileHistory(c.Request.Context(), dept(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(rs))
	for _, r := range rs {
		items = append(items, profileOut(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// --- Thư mục. ---

func folderOut(f store.FolderRow, path []string, docs int, hiddenChain bool) gin.H {
	if path == nil {
		path = []string{f.Name}
	}
	return gin.H{"ref": f.Ref.String(), "department_code": f.Department, "parent_ref": optRef(f.ParentRef), "name": f.Name, "path": path,
		"hidden": f.Hidden || hiddenChain, "status_reason": f.StatusReason, "docs": docs, "version": f.Seq, "event": f.Event,
		"recorded_at": ts(f.RecordedAt)}
}

func (h Handler) folders(c *gin.Context) {
	r, err := h.Svc.Folders(c.Request.Context(), dept(c), c.Query("include_hidden") == "1")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(r.Items))
	for _, f := range r.Items {
		items = append(items, folderOut(f.FolderRow, f.Path, f.Docs, f.HiddenChain))
	}
	c.JSON(http.StatusOK, gin.H{"department_code": r.Department, "can_write": r.CanWrite, "can_manage": r.CanManage, "items": items})
}

type folderIn struct {
	ParentRef *uuid.UUID `json:"parent_ref"`
	Name      string     `json:"name"`
}

func (h Handler) createFolder(c *gin.Context) {
	var in folderIn
	if !decode(c, &in) {
		return
	}
	f, err := h.Svc.CreateFolder(c.Request.Context(), app.FolderInput{Department: dept(c), ParentRef: in.ParentRef, Name: in.Name}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, folderOut(f, nil, 0, false))
}

type folderChangeIn struct {
	Name        string `json:"name"`
	Reason      string `json:"reason"`
	BaseVersion int64  `json:"base_version"`
}

func (h Handler) changeFolder(event string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := refParam(c, app.ErrFolderNotFound)
		if !ok {
			return
		}
		var in folderChangeIn
		if !decode(c, &in) {
			return
		}
		f, err := h.Svc.ChangeFolder(c.Request.Context(), app.FolderChange{Department: dept(c), Ref: ref, Event: event, Name: in.Name,
			Reason: in.Reason, BaseVersion: in.BaseVersion}, idem(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, folderOut(f, nil, 0, false))
	}
}

// --- Văn bản. ---

func fileOut(f store.File) gin.H {
	return gin.H{"version": f.Version, "name": f.Name, "content_type": f.ContentType, "size_bytes": f.Size, "sha256": f.SHA256}
}

func docRowOut(d store.DocRow) gin.H {
	return gin.H{"ref": d.Ref.String(), "version": d.Seq, "event": d.Event, "department_code": d.Department, "folder_ref": optRef(d.FolderRef),
		"title": d.Title, "kind": d.Kind, "field": optStr(d.Field), "number": optStr(d.Number), "issuer": optStr(d.Issuer),
		"summary": optStr(d.Summary), "issued_on": optDay(d.IssuedOn), "effective_from": optDay(d.EffectiveFrom),
		"effective_to": optDay(d.EffectiveTo), "audience": d.Audience, "file": fileOut(d.File), "hidden": d.Hidden,
		"status_reason": d.StatusReason, "recorded_at": ts(d.RecordedAt), "actor": actor(d.ActorRef, "")}
}

func docOut(d app.DocView) gin.H {
	m := docRowOut(d.DocRow)
	m["validity"] = d.Validity
	m["folder_path"] = d.FolderPath
	m["actor"] = actor(d.ActorRef, d.ActorName)
	m["can_edit"] = d.CanEdit
	m["can_manage"] = d.CanManage
	if d.Versions != nil {
		vs := make([]gin.H, 0, len(d.Versions))
		for _, v := range d.Versions {
			vs = append(vs, docRowOut(v))
		}
		m["versions"] = vs
	}
	return m
}

func (h Handler) listDocs(c *gin.Context) {
	f := app.DocFilter{Department: dept(c), Query: c.Query("q"), Kind: c.Query("kind"), Validity: c.Query("validity"),
		IncludeHidden: c.Query("include_hidden") == "1"}
	if s := c.Query("folder"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("folder", "phải là UUID"))
			return
		}
		f.Folder = &id
	}
	r, err := h.Svc.ListDocs(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(r.Items))
	for _, d := range r.Items {
		items = append(items, docOut(d))
	}
	c.JSON(http.StatusOK, gin.H{"department_code": r.Department, "can_write": r.CanWrite, "can_manage": r.CanManage, "total": r.Total,
		"kinds": domain.Kinds, "extensions": domain.FileExtensions, "max_bytes": domain.MaxFileBytes, "items": items})
}

func (h Handler) getDoc(c *gin.Context) {
	ref, ok := refParam(c, app.ErrDocNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetDoc(c.Request.Context(), dept(c), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, docOut(d))
}

// docIn là thuộc tính văn bản (phần meta của multipart hoặc thân JSON của /update).
type docIn struct {
	FolderRef     *uuid.UUID `json:"folder_ref"`
	Title         string     `json:"title"`
	Kind          string     `json:"kind"`
	Field         string     `json:"field"`
	Number        string     `json:"number"`
	Issuer        string     `json:"issuer"`
	Summary       string     `json:"summary"`
	IssuedOn      string     `json:"issued_on"`
	EffectiveFrom string     `json:"effective_from"`
	EffectiveTo   string     `json:"effective_to"`
	Audience      []string   `json:"audience"`
	BaseVersion   int64      `json:"base_version"`
}

func dateIn(s, field string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, app.ErrDocInvalid.WithField(field, "dạng YYYY-MM-DD")
	}
	return &t, nil
}

func (in docIn) app(department string) (app.DocInput, error) {
	out := app.DocInput{Department: department, FolderRef: in.FolderRef, BaseVersion: in.BaseVersion, Meta: domain.DocMeta{Title: in.Title,
		Kind: in.Kind, Field: in.Field, Number: in.Number, Issuer: in.Issuer, Summary: in.Summary, Audience: in.Audience}}
	var err error
	if out.Meta.IssuedOn, err = dateIn(in.IssuedOn, "issued_on"); err != nil {
		return out, err
	}
	if out.Meta.EffectiveFrom, err = dateIn(in.EffectiveFrom, "effective_from"); err != nil {
		return out, err
	}
	out.Meta.EffectiveTo, err = dateIn(in.EffectiveTo, "effective_to")
	return out, err
}

// multipartFile đọc phần "file" (và phần "meta" vào meta nếu khác nil) của thân multipart.
func multipartFile(c *gin.Context, meta any) (app.FileInput, bool) {
	if err := c.Request.ParseMultipartForm(maxUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, app.ErrFileTooLarge.WithDetail("tối đa 20 MB mỗi tệp (FN-20 AC-02)"))
			return app.FileInput{}, false
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân phải là multipart/form-data có phần meta và file"))
		return app.FileInput{}, false
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	if meta != nil {
		dec := json.NewDecoder(strings.NewReader(c.Request.FormValue("meta")))
		dec.DisallowUnknownFields()
		if err := dec.Decode(meta); err != nil {
			httpx.Fail(c, problem.Validation.WithField("meta", "JSON thuộc tính không hợp lệ: "+err.Error()))
			return app.FileInput{}, false
		}
	}
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "bắt buộc"))
		return app.FileInput{}, false
	}
	if fh.Size > domain.MaxFileBytes {
		httpx.Fail(c, app.ErrFileTooLarge.WithDetail("tối đa 20 MB mỗi tệp (FN-20 AC-02)"))
		return app.FileInput{}, false
	}
	fr, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return app.FileInput{}, false
	}
	data, err := io.ReadAll(io.LimitReader(fr, domain.MaxFileBytes+1))
	_ = fr.Close()
	if err != nil {
		httpx.Fail(c, err)
		return app.FileInput{}, false
	}
	return app.FileInput{Name: fh.Filename, Data: data}, true
}

func (h Handler) createDoc(c *gin.Context) {
	var in docIn
	file, ok := multipartFile(c, &in)
	if !ok {
		return
	}
	ai, err := in.app(dept(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d, err := h.Svc.CreateDoc(c.Request.Context(), ai, file, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, docOut(d))
}

func (h Handler) updateDoc(c *gin.Context) {
	ref, ok := refParam(c, app.ErrDocNotFound)
	if !ok {
		return
	}
	var in docIn
	if !decode(c, &in) {
		return
	}
	ai, err := in.app(dept(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d, err := h.Svc.UpdateDoc(c.Request.Context(), ref, ai, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, docOut(d))
}

func (h Handler) replaceFile(c *gin.Context) {
	ref, ok := refParam(c, app.ErrDocNotFound)
	if !ok {
		return
	}
	file, ok := multipartFile(c, nil)
	if !ok {
		return
	}
	base, err := strconv.ParseInt(c.Request.FormValue("base_version"), 10, 64)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)"))
		return
	}
	d, err := h.Svc.ReplaceFile(c.Request.Context(), dept(c), ref, base, file, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, docOut(d))
}

type hideIn struct {
	Reason      string `json:"reason"`
	BaseVersion int64  `json:"base_version"`
}

func (h Handler) hideDoc(hidden bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := refParam(c, app.ErrDocNotFound)
		if !ok {
			return
		}
		var in hideIn
		if !decode(c, &in) {
			return
		}
		d, err := h.Svc.SetDocHidden(c.Request.Context(), dept(c), ref, hidden, in.BaseVersion, in.Reason, idem(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, docOut(d))
	}
}

// content trả nội dung tệp (download=1 → tải xuống; mặc định xem trong trình duyệt). Cache-Control: no-store.
func (h Handler) content(c *gin.Context) {
	ref, ok := refParam(c, app.ErrDocNotFound)
	if !ok {
		return
	}
	var version int64
	if s := c.Query("version"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v < 1 {
			httpx.Fail(c, problem.Validation.WithField("version", "số nguyên ≥ 1"))
			return
		}
		version = v
	}
	o, err := h.Svc.OpenFile(c.Request.Context(), dept(c), ref, version)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = o.Reader.Close() }()
	disp := "inline"
	if c.Query("download") == "1" {
		disp = "attachment"
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(o.File.Name))
	c.DataFromReader(http.StatusOK, o.File.Size, o.File.ContentType, o.Reader, nil)
}
