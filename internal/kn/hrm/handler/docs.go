package handler

// W11.24: HTTP kho tài liệu nhân sự (FN-14) — /v1/hrm/docs*. Nội dung tệp (xem trước/tải xuống) đặt Cache-Control: no-store.

import (
	"bytes"
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// maxDocUpload: thân multipart tối đa (tệp + phân loại + biên).
const maxDocUpload = domain.MaxDocBytes + 64<<10

func (h Handler) registerDocs(rg *gin.RouterGroup) {
	rg.GET("/hrm/docs/folders", h.docFolders)
	rg.POST("/hrm/docs/folders", h.docFolderCreate)
	rg.POST("/hrm/docs/folders/defaults", h.docFolderDefaults)
	rg.GET("/hrm/docs/folders/:id/history", h.docFolderHistory)
	rg.POST("/hrm/docs/folders/:id/rename", h.docFolderRename)
	rg.POST("/hrm/docs/folders/:id/acl", h.docFolderACL)
	rg.POST("/hrm/docs/folders/:id/hide", h.docFolderHide(true))
	rg.POST("/hrm/docs/folders/:id/restore", h.docFolderHide(false))
	rg.GET("/hrm/docs", h.docList)
	rg.POST("/hrm/docs", h.docUpload)
	rg.GET("/hrm/docs/:id", h.docGet)
	rg.POST("/hrm/docs/:id/classify", h.docClassify)
	rg.POST("/hrm/docs/:id/hide", h.docHide(true))
	rg.POST("/hrm/docs/:id/restore", h.docHide(false))
	rg.GET("/hrm/docs/:id/content", h.docContent)
}

func docUUID(s, field string) (*uuid.UUID, error) {
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return nil, problem.Validation.WithField(field, "phải là UUID")
	}
	return &id, nil
}

// --- Thư mục. ---

type folderDTO struct {
	FolderID   uuid.UUID                    `json:"folder_id"`
	ParentID   *uuid.UUID                   `json:"parent_id"`
	Name       string                       `json:"name"`
	Path       []string                     `json:"path"`
	SystemKey  *string                      `json:"system_key"`
	DocType    *string                      `json:"doc_type"`
	SortOrder  int                          `json:"sort_order"`
	Status     string                       `json:"status"`
	Hidden     bool                         `json:"hidden"`
	Docs       int                          `json:"docs"`
	CanView    bool                         `json:"can_view"`
	CanManage  bool                         `json:"can_manage"`
	CanAdmin   bool                         `json:"can_admin"`
	ACL        map[string]map[string]string `json:"acl,omitempty"`
	Version    int64                        `json:"version"`
	RecordedAt time.Time                    `json:"recorded_at"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func aclOut(a domain.FolderACL) map[string]map[string]string {
	if a == nil {
		return nil
	}
	out := map[string]map[string]string{}
	for k, m := range a {
		out[k] = matrixOut(m)
	}
	return out
}

func (h Handler) docFolders(c *gin.Context) {
	meta, fs, err := h.Svc.Folders(c.Request.Context(), c.Query("include_hidden") == "1")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]folderDTO, 0, len(fs))
	for _, f := range fs {
		items = append(items, folderDTO{FolderID: f.FolderID, ParentID: f.ParentID, Name: f.Name, Path: f.Path, SystemKey: strPtr(f.SystemKey),
			DocType: strPtr(f.DocType), SortOrder: f.SortOrder, Status: f.Status, Hidden: f.Hidden, Docs: f.Docs, CanView: f.CanView,
			CanManage: f.CanManage, CanAdmin: f.CanAdmin, ACL: aclOut(f.ACL), Version: f.Seq, RecordedAt: f.RecordedAt})
	}
	c.JSON(http.StatusOK, gin.H{"records_enabled": meta.RecordsEnabled, "ready": meta.Ready, "group": meta.Group, "unit": meta.Unit,
		"perms": matrixOut(meta.Perms), "can_create_root": meta.CanCreateRoot, "doc_types": domain.DocTypes,
		"extensions": domain.DocExtensions, "max_bytes": domain.MaxDocBytes, "items": items})
}

type folderIn struct {
	ParentID *uuid.UUID `json:"parent_id"`
	Name     string     `json:"name"`
	DocType  string     `json:"doc_type"`
}

func folderWritten(c *gin.Context, r app.FolderResult, err error, code int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(code, gin.H{"folder_id": r.FolderID, "version": r.Version})
}

func (h Handler) docFolderCreate(c *gin.Context) {
	var in folderIn
	if !decode(c, &in) {
		return
	}
	r, err := h.Svc.CreateFolder(c.Request.Context(), app.FolderInput{Parent: in.ParentID, Name: in.Name, DocType: in.DocType}, idem(c))
	folderWritten(c, r, err, http.StatusCreated)
}

func (h Handler) docFolderDefaults(c *gin.Context) {
	n, err := h.Svc.InstallDocFolders(c.Request.Context(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"created": n})
}

func (h Handler) docFolderRename(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrFolderNotFound)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !decode(c, &in) {
		return
	}
	r, err := h.Svc.RenameFolder(c.Request.Context(), id, in.Name, idem(c))
	folderWritten(c, r, err, http.StatusOK)
}

func (h Handler) docFolderACL(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrFolderNotFound)
	if !ok {
		return
	}
	var in struct {
		ACL  json.RawMessage `json:"acl"`
		Note string          `json:"note"`
	}
	if !decode(c, &in) {
		return
	}
	r, err := h.Svc.SetFolderACL(c.Request.Context(), id, in.ACL, in.Note, idem(c))
	folderWritten(c, r, err, http.StatusOK)
}

type reasonIn struct {
	Reason string `json:"reason"`
}

func (h Handler) docFolderHide(hidden bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := uuidParam(c, "id", port.ErrFolderNotFound)
		if !ok {
			return
		}
		var in reasonIn
		if !decode(c, &in) {
			return
		}
		r, err := h.Svc.SetFolderHidden(c.Request.Context(), id, hidden, in.Reason, idem(c))
		folderWritten(c, r, err, http.StatusOK)
	}
}

func (h Handler) docFolderHistory(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrFolderNotFound)
	if !ok {
		return
	}
	rows, err := h.Svc.FolderHistory(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		a, _, _ := domain.ParseFolderACL(r.ACL)
		items = append(items, gin.H{"version": r.Seq, "change": r.Change, "name": r.Name, "status": r.Status, "note": strPtr(r.Note),
			"acl": aclOut(a), "actor_ref": r.Actor, "recorded_at": r.RecordedAt})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// --- Tài liệu. ---

type docIn struct {
	FolderID uuid.UUID   `json:"folder_id"`
	Title    string      `json:"title"`
	DocType  string      `json:"doc_type"`
	Year     *int        `json:"year"`
	UnitCode string      `json:"unit_code"`
	StaffIDs []uuid.UUID `json:"staff_ids"`
	DocNo    string      `json:"doc_no"`
	IssuedOn string      `json:"issued_on"`
	Note     string      `json:"note"`
}

func (in docIn) app() (app.DocInput, error) {
	out := app.DocInput{FolderID: in.FolderID, Title: in.Title, DocType: in.DocType, Year: in.Year, UnitCode: in.UnitCode,
		StaffIDs: in.StaffIDs, DocNo: in.DocNo, Note: in.Note}
	if in.IssuedOn != "" {
		d, err := dateOf(in.IssuedOn, "issued_on")
		if err != nil {
			return out, err
		}
		out.IssuedOn = &d
	}
	return out, nil
}

type docDTO struct {
	DocID       uuid.UUID   `json:"doc_id"`
	FolderID    uuid.UUID   `json:"folder_id"`
	FolderPath  []string    `json:"folder_path"`
	Title       string      `json:"title"`
	DocType     string      `json:"doc_type"`
	Year        *int        `json:"year"`
	UnitCode    *string     `json:"unit_code"`
	StaffIDs    []uuid.UUID `json:"staff_ids"`
	StaffCodes  []string    `json:"staff_codes"`
	DocNo       *string     `json:"doc_no"`
	IssuedOn    *string     `json:"issued_on"`
	Note        *string     `json:"note"`
	FileName    string      `json:"file_name"`
	ContentType string      `json:"content_type"`
	SizeBytes   int64       `json:"size_bytes"`
	Status      string      `json:"status"`
	Version     int64       `json:"version"`
	CreatedAt   time.Time   `json:"created_at"`
	UploadedBy  uuid.UUID   `json:"uploaded_by"`
	UpdatedAt   time.Time   `json:"updated_at"`
	CanManage   bool        `json:"can_manage"`
}

func dateOut(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func docOut(d app.DocItem) docDTO {
	staff, codes := d.StaffIDs, d.StaffCodes
	if staff == nil {
		staff = []uuid.UUID{}
	}
	if codes == nil {
		codes = []string{}
	}
	return docDTO{DocID: d.DocID, FolderID: d.FolderID, FolderPath: d.FolderPath, Title: d.Title, DocType: d.DocType, Year: d.Year,
		UnitCode: strPtr(d.UnitCode), StaffIDs: staff, StaffCodes: codes, DocNo: strPtr(d.DocNo), IssuedOn: dateOut(d.IssuedOn),
		Note: strPtr(d.Note), FileName: d.FileName, ContentType: d.ContentType, SizeBytes: d.Size, Status: d.Status, Version: d.Seq,
		CreatedAt: d.Created, UploadedBy: d.Uploader, UpdatedAt: d.RecordedAt, CanManage: d.CanManage}
}

func (h Handler) docList(c *gin.Context) {
	f := app.DocFilter{Q: c.Query("q"), DocType: c.Query("doc_type"), Unit: c.Query("unit"), Recursive: c.Query("recursive") == "1",
		IncludeHidden: c.Query("include_hidden") == "1"}
	var err error
	if f.Folder, err = docUUID(c.Query("folder"), "folder"); err != nil {
		httpx.Fail(c, err)
		return
	}
	if f.Staff, err = docUUID(c.Query("staff"), "staff"); err != nil {
		httpx.Fail(c, err)
		return
	}
	if y := c.Query("year"); y != "" {
		n, err := strconv.Atoi(y)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("year", "phải là số"))
			return
		}
		f.Year = &n
	}
	f.Offset, _ = strconv.Atoi(c.Query("offset"))
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	r, err := h.Svc.ListDocs(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]docDTO, 0, len(r.Items))
	for _, d := range r.Items {
		items = append(items, docOut(d))
	}
	c.JSON(http.StatusOK, gin.H{"total": r.Total, "items": items})
}

// docUpload: multipart — phần "meta" (JSON docIn) và phần "file".
func (h Handler) docUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDocUpload)
	if err := c.Request.ParseMultipartForm(maxDocUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, port.ErrDocTooLarge.WithDetail("tối đa 15 MB mỗi tệp"))
			return
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân phải là multipart/form-data có phần meta và file"))
		return
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	var in docIn
	dec := json.NewDecoder(strings.NewReader(c.Request.FormValue("meta")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.Fail(c, problem.Validation.WithField("meta", "JSON phân loại không hợp lệ: "+err.Error()))
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "bắt buộc"))
		return
	}
	if fh.Size > domain.MaxDocBytes {
		httpx.Fail(c, port.ErrDocTooLarge.WithDetail("tối đa 15 MB mỗi tệp"))
		return
	}
	fr, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, io.LimitReader(fr, domain.MaxDocBytes+1))
	_ = fr.Close()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ai, err := in.app()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	r, err := h.Svc.UploadDoc(c.Request.Context(), ai, fh.Filename, buf.Bytes(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"doc_id": r.DocID, "version": r.Version})
}

func docWritten(c *gin.Context, r app.DocResult, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"doc_id": r.DocID, "version": r.Version})
}

func (h Handler) docClassify(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrDocNotFound)
	if !ok {
		return
	}
	var in docIn
	if !decode(c, &in) {
		return
	}
	ai, err := in.app()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	r, err := h.Svc.UpdateDoc(c.Request.Context(), id, ai, idem(c))
	docWritten(c, r, err)
}

func (h Handler) docHide(hidden bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := uuidParam(c, "id", port.ErrDocNotFound)
		if !ok {
			return
		}
		var in reasonIn
		if !decode(c, &in) {
			return
		}
		r, err := h.Svc.SetDocHidden(c.Request.Context(), id, hidden, in.Reason, idem(c))
		docWritten(c, r, err)
	}
}

func versionOut(r store.DocRow) gin.H {
	fields := r.ChangedFields
	if fields == nil {
		fields = []string{}
	}
	return gin.H{"version": r.Seq, "change": r.Change, "changed_fields": fields, "status": r.Status, "reason": strPtr(r.Reason),
		"actor_ref": r.Actor, "recorded_at": r.RecordedAt}
}

func (h Handler) docGet(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrDocNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetDoc(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	hist := make([]gin.H, 0, len(d.History))
	for _, r := range d.History {
		hist = append(hist, versionOut(r))
	}
	var access []gin.H
	if d.CanAccess {
		access = make([]gin.H, 0, len(d.Access))
		for _, a := range d.Access {
			access = append(access, gin.H{"action": a.Action, "scope": a.Scope, "actor_ref": a.Actor, "recorded_at": a.RecordedAt})
		}
	}
	c.JSON(http.StatusOK, gin.H{"doc": docOut(d.DocItem), "history": hist, "access": access})
}

func (h Handler) docContent(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrDocNotFound)
	if !ok {
		return
	}
	download := c.Query("download") == "1"
	o, err := h.Svc.OpenDoc(c.Request.Context(), id, download)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	disp := "inline"
	if download {
		disp = "attachment"
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(o.FileName))
	c.Data(http.StatusOK, o.ContentType, o.Content)
}
