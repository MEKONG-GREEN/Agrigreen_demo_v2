// Package handler: HTTP của pf/content — Application API (người đọc) và Admin
// API (người soạn, người duyệt). Hợp đồng ở contracts/openapi/*.yaml, mục
// "# --- pf/content ---".
package handler

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxUploadBody là giới hạn thân multipart của đường tải tệp: 15 MB tệp + phần đầu.
const MaxUploadBody = domain.MaxAttachmentBytes + 1<<20

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API (người đọc).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/documents", h.list)
	rg.GET("/documents/:ref", h.get)
	rg.GET("/documents/:ref/versions", h.publishedVersions)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc ở router).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/documents", h.adminList)
	rg.POST("/documents", h.create)
	httpx.AllowBody(rg, http.MethodPost, "/documents/extract", MaxUploadBody)
	rg.POST("/documents/extract", h.extract)
	rg.GET("/documents/:ref", h.adminGet)
	rg.GET("/documents/:ref/versions/:no", h.adminVersion)
	rg.POST("/documents/:ref/versions", h.newVersion)
	httpx.AllowBody(rg, http.MethodPost, "/documents/:ref/attachments", MaxUploadBody)
	rg.POST("/documents/:ref/attachments", h.upload)
	rg.POST("/documents/:ref/submit", h.submit)
	rg.POST("/documents/:ref/return", h.returnVersion)
	rg.POST("/documents/:ref/publish", h.publish)
	rg.POST("/documents/:ref/retire", h.retire)
	rg.POST("/documents/:ref/duplicate", h.duplicate)
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type audienceDTO struct {
	Level        string   `json:"level"`
	Units        []string `json:"units"`
	ProvinceCode *string  `json:"province_code"`
}

type attachmentDTO struct {
	ID          string  `json:"id"`
	FileName    string  `json:"file_name"`
	ContentType string  `json:"content_type"`
	Size        int64   `json:"size"`
	SHA256      string  `json:"sha256"`
	Role        string  `json:"role"`
	URL         *string `json:"url"`
	URLExpires  *string `json:"url_expires_at"`
}

type versionDTO struct {
	ID          string          `json:"id"`
	VersionNo   int             `json:"version_no"`
	Status      string          `json:"status"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Body        string          `json:"body"`
	BodyFormat  string          `json:"body_format"`
	Category    *string         `json:"category"`
	Tags        []string        `json:"tags"`
	AuthorRef   string          `json:"author_ref"`
	RecordedAt  string          `json:"recorded_at"`
	Attachments []attachmentDTO `json:"attachments"`
}

type docDTO struct {
	Ref                string      `json:"ref"`
	Kind               string      `json:"kind"`
	Audience           audienceDTO `json:"audience"`
	ParentRef          *string     `json:"parent_ref"`
	Status             string      `json:"status"`
	Title              *string     `json:"title"`
	Summary            *string     `json:"summary"`
	Category           *string     `json:"category"`
	Tags               []string    `json:"tags"`
	PublishedVersionNo *int        `json:"published_version_no"`
	PublishedAt        *string     `json:"published_at"`
	PublisherRef       *string     `json:"publisher_ref"`
	RetiredAt          *string     `json:"retired_at,omitempty"`
	RetireReason       *string     `json:"retire_reason,omitempty"`
}

// adminDocDTO thêm thông tin soạn thảo cho bề mặt quản trị.
type adminDocDTO struct {
	docDTO
	CreatedBy        string  `json:"created_by"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	LastVersionNo    int     `json:"last_version_no"`
	WorkingVersionNo *int    `json:"working_version_no"`
	WorkingStatus    *string `json:"working_status"`
}

type supplementDTO struct {
	Ref          string  `json:"ref"`
	Title        string  `json:"title"`
	ProvinceCode *string `json:"province_code"`
	PublishedAt  *string `json:"published_at"`
}

type historyDTO struct {
	VersionNo    int     `json:"version_no"`
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	Title        string  `json:"title"`
	AuthorRef    string  `json:"author_ref"`
	RecordedAt   string  `json:"recorded_at"`
	PublishedAt  *string `json:"published_at"`
	PublisherRef *string `json:"publisher_ref"`
	ReturnReason *string `json:"return_reason,omitempty"`
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func refPtr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func toDocDTO(d domain.Document) docDTO {
	units := d.Audience.Units
	if units == nil {
		units = []string{}
	}
	return docDTO{
		Ref: d.Ref.String(), Kind: string(d.Kind),
		Audience:  audienceDTO{Level: string(d.Audience.Level), Units: units, ProvinceCode: strPtr(d.Audience.ProvinceCode)},
		ParentRef: refPtr(d.ParentRef), Status: string(d.Status), Title: strPtr(d.Title), Summary: strPtr(d.Summary),
		Category: strPtr(d.Category), Tags: d.Tags, PublishedVersionNo: d.PublishedVersionNo, PublishedAt: tsPtr(d.PublishedAt),
		PublisherRef: refPtr(d.PublisherRef), RetiredAt: tsPtr(d.RetiredAt), RetireReason: strPtr(d.RetireReason),
	}
}

func toAdminDocDTO(d domain.Document) adminDocDTO {
	out := adminDocDTO{
		docDTO: toDocDTO(d), CreatedBy: d.CreatedBy.String(), CreatedAt: ts(d.CreatedAt), UpdatedAt: ts(d.UpdatedAt),
		LastVersionNo: d.LastVersionNo, WorkingVersionNo: d.WorkingVersionNo,
	}
	if d.WorkingStatus != nil {
		s := string(*d.WorkingStatus)
		out.WorkingStatus = &s
	}
	return out
}

func toVersionDTO(v domain.Version, st domain.VersionStatus, atts []app.AttachmentView) versionDTO {
	out := versionDTO{
		ID: v.ID.String(), VersionNo: v.VersionNo, Status: string(st), Title: v.Content.Title, Summary: v.Content.Summary,
		Body: v.Content.Body, BodyFormat: string(v.Content.BodyFormat), Category: strPtr(v.Content.Category), Tags: v.Content.Tags,
		AuthorRef: v.AuthorRef.String(), RecordedAt: ts(v.RecordedAt), Attachments: []attachmentDTO{},
	}
	if atts == nil {
		for _, a := range v.Content.Attachments {
			atts = append(atts, app.AttachmentView{Attachment: a})
		}
	}
	for _, a := range atts {
		d := attachmentDTO{ID: a.ID.String(), FileName: a.FileName, ContentType: a.ContentType, Size: a.Size, SHA256: a.SHA256, Role: a.Role, URLExpires: tsPtr(a.ExpiresAt)}
		d.URL = strPtr(a.URL)
		out.Attachments = append(out.Attachments, d)
	}
	return out
}

func toHistory(hs []app.VersionInfo) []historyDTO {
	out := make([]historyDTO, 0, len(hs))
	for _, h := range hs {
		out = append(out, historyDTO{
			VersionNo: h.Version.VersionNo, ID: h.Version.ID.String(), Status: string(h.Status), Title: h.Version.Content.Title,
			AuthorRef: h.Version.AuthorRef.String(), RecordedAt: ts(h.Version.RecordedAt), PublishedAt: tsPtr(h.PublishedAt),
			PublisherRef: refPtr(h.PublisherRef), ReturnReason: strPtr(h.ReturnReason),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return ref, false
	}
	return ref, true
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func queryLimit(c *gin.Context) int {
	if n, err := strconv.Atoi(c.Query("limit")); err == nil {
		return n
	}
	return 50
}

func idemKey(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func status(replayed bool, created int) int {
	if replayed {
		return http.StatusOK
	}
	return created
}

// Cursor người đọc: 8 byte published_at (UnixMicro) + 16 byte ref; quản trị: 16 byte ref.
func encodeCursor(t *time.Time, ref uuid.UUID) any {
	if ref == uuid.Nil {
		return nil
	}
	var b []byte
	if t != nil {
		b = binary.BigEndian.AppendUint64(b, uint64(t.UnixMicro()))
	}
	b = append(b, ref[:]...)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(c *gin.Context, withTime bool) (*time.Time, uuid.UUID, bool) {
	var ref uuid.UUID
	cur := c.Query("cursor")
	if cur == "" {
		return nil, ref, true
	}
	b, err := base64.RawURLEncoding.DecodeString(cur)
	want := 16
	if withTime {
		want = 24
	}
	if err != nil || len(b) != want {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return nil, ref, false
	}
	var t *time.Time
	if withTime {
		tt := time.UnixMicro(int64(binary.BigEndian.Uint64(b[:8]))).UTC()
		t, b = &tt, b[8:]
	}
	copy(ref[:], b)
	return t, ref, true
}

// ---------------------------------------------------------------------------
// Application API
// ---------------------------------------------------------------------------

func listInput(c *gin.Context) app.ListInput {
	return app.ListInput{Kind: c.Query("kind"), Tag: c.Query("tag"), Category: c.Query("category"), Q: c.Query("q"), Limit: queryLimit(c)}
}

func (h Handler) list(c *gin.Context) {
	t, ref, ok := decodeCursor(c, true)
	if !ok {
		return
	}
	in := listInput(c)
	in.AfterTime, in.AfterRef = t, ref
	pg, err := h.Svc.List(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]docDTO, 0, len(pg.Items))
	for _, d := range pg.Items {
		items = append(items, toDocDTO(d))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.NextTime, pg.NextRef)})
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
	sups := make([]supplementDTO, 0, len(v.Supplements))
	for _, s := range v.Supplements {
		sups = append(sups, supplementDTO{Ref: s.Ref.String(), Title: s.Title, ProvinceCode: strPtr(s.Audience.ProvinceCode), PublishedAt: tsPtr(s.PublishedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"document": toDocDTO(v.Doc), "version": toVersionDTO(*v.Version, v.VersionStatus, v.Attachments), "supplements": sups})
}

func (h Handler) publishedVersions(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	d, hs, err := h.Svc.PublishedVersions(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document_ref": d.Ref.String(), "items": toHistory(hs)})
}

// ---------------------------------------------------------------------------
// Admin API
// ---------------------------------------------------------------------------

func (h Handler) adminList(c *gin.Context) {
	_, ref, ok := decodeCursor(c, false)
	if !ok {
		return
	}
	in := listInput(c)
	in.AfterRef = ref
	for _, s := range c.QueryArray("status") {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				in.Statuses = append(in.Statuses, domain.Status(p))
			}
		}
	}
	pg, err := h.Svc.AdminList(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]adminDocDTO, 0, len(pg.Items))
	for _, d := range pg.Items {
		items = append(items, toAdminDocDTO(d))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(nil, pg.NextRef)})
}

type contentReq struct {
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Body        string   `json:"body"`
	BodyFormat  string   `json:"body_format"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Attachments []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"attachments"`
}

func (r contentReq) input() app.ContentInput {
	return app.ContentInput{Title: r.Title, Summary: r.Summary, Body: r.Body, BodyFormat: domain.BodyFormat(r.BodyFormat), Category: r.Category, Tags: r.Tags}
}

type createReq struct {
	contentReq
	Kind     string `json:"kind"`
	Audience struct {
		Level string   `json:"level"`
		Units []string `json:"units"`
	} `json:"audience"`
	ParentRef *string `json:"parent_ref"`
}

func (h Handler) writeView(c *gin.Context, v app.View, replayed bool, created int) {
	c.Header("Location", "/v1/documents/"+v.Doc.Ref.String())
	c.JSON(status(replayed, created), gin.H{"document": toAdminDocDTO(v.Doc), "version": toVersionDTO(*v.Version, v.VersionStatus, nil)})
}

func (h Handler) create(c *gin.Context) {
	var req createReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.CreateInput{
		Kind: port.Kind(req.Kind), Content: req.input(), IdemKey: idemKey(c),
		Audience: domain.Audience{Level: port.AudienceLevel(req.Audience.Level), Units: req.Audience.Units},
	}
	if in.Audience.Units == nil {
		in.Audience.Units = []string{}
	}
	if len(req.Attachments) > 0 {
		httpx.Fail(c, problem.Validation.WithField("attachments", "tải tệp sau khi tạo bản nháp, rồi thêm phiên bản mới"))
		return
	}
	if req.ParentRef != nil {
		pr, err := uuid.Parse(*req.ParentRef)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("parent_ref", "phải là UUID"))
			return
		}
		in.ParentRef = &pr
	}
	v, replayed, err := h.Svc.Create(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.writeView(c, v, replayed, http.StatusCreated)
}

func (h Handler) newVersion(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req contentReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.VersionInput{Content: req.input(), IdemKey: idemKey(c)}
	for i, a := range req.Attachments {
		id, err := uuid.Parse(a.ID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("attachments["+strconv.Itoa(i)+"].id", "phải là UUID"))
			return
		}
		in.Attachments = append(in.Attachments, app.AttachmentRef{ID: id, Role: a.Role})
	}
	v, replayed, err := h.Svc.NewVersion(c.Request.Context(), ref, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.writeView(c, v, replayed, http.StatusCreated)
}

type transitionReq struct {
	VersionNo int    `json:"version_no"`
	Reason    string `json:"reason"`
}

func (h Handler) transition(c *gin.Context, fn func(ref uuid.UUID, req transitionReq) (domain.Document, bool, error)) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req transitionReq
	if !bindJSON(c, &req) {
		return
	}
	d, replayed, err := fn(ref, req)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(replayed, http.StatusOK), gin.H{"document": toAdminDocDTO(d)})
}

func (h Handler) submit(c *gin.Context) {
	h.transition(c, func(ref uuid.UUID, r transitionReq) (domain.Document, bool, error) {
		return h.Svc.Submit(c.Request.Context(), ref, r.VersionNo, idemKey(c))
	})
}

func (h Handler) returnVersion(c *gin.Context) {
	h.transition(c, func(ref uuid.UUID, r transitionReq) (domain.Document, bool, error) {
		return h.Svc.Return(c.Request.Context(), ref, r.VersionNo, r.Reason, idemKey(c))
	})
}

func (h Handler) retire(c *gin.Context) {
	h.transition(c, func(ref uuid.UUID, r transitionReq) (domain.Document, bool, error) {
		return h.Svc.Retire(c.Request.Context(), ref, r.Reason, idemKey(c))
	})
}

func (h Handler) publish(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req transitionReq
	if !bindJSON(c, &req) {
		return
	}
	v, replayed, err := h.Svc.Publish(c.Request.Context(), ref, req.VersionNo, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(replayed, http.StatusOK), gin.H{"document": toAdminDocDTO(v.Doc), "version": toVersionDTO(*v.Version, v.VersionStatus, nil)})
}

func (h Handler) duplicate(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	v, replayed, err := h.Svc.Duplicate(c.Request.Context(), ref, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.writeView(c, v, replayed, http.StatusCreated)
}

func (h Handler) adminGet(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	d, hs, err := h.Svc.AdminGet(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document": toAdminDocDTO(d), "versions": toHistory(hs)})
}

func (h Handler) adminVersion(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	no, err := strconv.Atoi(c.Param("no"))
	if err != nil || no < 1 {
		httpx.Fail(c, app.ErrVersionNotFound)
		return
	}
	v, err := h.Svc.AdminVersion(c.Request.Context(), ref, no)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document": toAdminDocDTO(v.Doc), "version": toVersionDTO(*v.Version, v.VersionStatus, v.Attachments)})
}

// readFile đọc phần "file" của multipart, tối đa 15 MB.
func readFile(c *gin.Context) ([]byte, string, bool) {
	fh, err := c.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, app.ErrFileTooLarge)
			return nil, "", false
		}
		httpx.Fail(c, problem.Validation.WithField("file", "thiếu phần multipart \"file\""))
		return nil, "", false
	}
	if fh.Size > domain.MaxAttachmentBytes {
		httpx.Fail(c, app.ErrFileTooLarge)
		return nil, "", false
	}
	f, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return nil, "", false
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, domain.MaxAttachmentBytes+1))
	if err != nil {
		httpx.Fail(c, err)
		return nil, "", false
	}
	if len(b) > domain.MaxAttachmentBytes {
		httpx.Fail(c, app.ErrFileTooLarge)
		return nil, "", false
	}
	return b, fh.Filename, true
}

type attachmentRowDTO struct {
	ID          string `json:"id"`
	DocumentRef string `json:"document_ref"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	CreatedAt   string `json:"created_at"`
}

func toAttachmentRowDTO(a store.AttachmentRow) attachmentRowDTO {
	return attachmentRowDTO{
		ID: a.ID.String(), DocumentRef: a.DocumentRef.String(), FileName: a.FileName, ContentType: a.ContentType,
		Size: a.Size, SHA256: a.SHA256, CreatedAt: ts(a.CreatedAt),
	}
}

func (h Handler) upload(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	data, name, ok := readFile(c)
	if !ok {
		return
	}
	row, replayed, err := h.Svc.Upload(c.Request.Context(), ref, app.UploadInput{Data: data, FileName: name, SHA256: c.PostForm("sha256"), IdemKey: idemKey(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(replayed, http.StatusCreated), toAttachmentRowDTO(row))
}

func (h Handler) extract(c *gin.Context) {
	data, _, ok := readFile(c)
	if !ok {
		return
	}
	x, err := h.Svc.Extract(c.Request.Context(), data)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"title": x.Title, "body": x.Body, "body_format": string(x.BodyFormat), "status": "draft_only"})
}
