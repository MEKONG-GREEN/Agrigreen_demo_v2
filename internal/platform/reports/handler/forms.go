package handler

// HTTP của mẫu báo cáo dạng lưới KN v7.7 (contracts/openapi/admin.yaml và application.yaml,
// mục "# --- pf/reports: mẫu báo cáo KN v7.7 ---").

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formimport"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxFormBody: thân request của định nghĩa lưới (T04 226 cột ~ 200 KB).
const MaxFormBody = 4 << 20

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// registerFormsAdmin gắn route Admin của mẫu báo cáo vào nhóm /reports.
func (h Handler) registerFormsAdmin(g *gin.RouterGroup) {
	for _, p := range []string{"/forms", "/forms/:id/versions/:v/draft", "/forms/:id/versions/:v/ops", "/form-versions/:id/evaluate"} {
		httpx.AllowBody(g, http.MethodPost, p, MaxFormBody)
	}
	httpx.AllowBody(g, http.MethodPost, "/forms/import", formimport.MaxBytes+64<<10)
	g.GET("/forms", h.listForms)
	g.POST("/forms", h.createForm)
	g.POST("/forms/import", h.importForm)
	g.POST("/forms/seed", h.seedForms)
	g.GET("/forms/:id", h.getForm)
	g.POST("/forms/:id/retire", h.retireForm)
	g.GET("/forms/:id/log", h.formLog)
	g.POST("/forms/:id/versions", h.newVersion)
	g.GET("/forms/:id/versions/:v", h.getVersion)
	g.POST("/forms/:id/versions/:v/draft", h.saveDraft)
	g.POST("/forms/:id/versions/:v/ops", h.applyOps)
	g.POST("/forms/:id/versions/:v/propose", h.propose)
	g.POST("/forms/:id/versions/:v/withdraw", h.withdraw)
	g.POST("/forms/:id/versions/:v/publish", h.publish)
	g.POST("/forms/:id/versions/:v/retire", h.retireVersion)
	g.POST("/form-versions/:id/evaluate", h.evaluate)
	g.GET("/form-assignments", h.listAssignments)
	g.POST("/form-assignments", h.assign)
	g.POST("/form-assignments/:id/revoke", h.revokeAssignment)
}

// registerFormsApp gắn route Application của mẫu báo cáo vào nhóm /reports.
func (h Handler) registerFormsApp(g *gin.RouterGroup) {
	httpx.AllowBody(g, http.MethodPost, "/form-versions/:id/evaluate", MaxFormBody)
	g.GET("/forms/available", h.availableForms)
	g.GET("/forms/:id/versions/:v/layout", h.layout)
	g.GET("/forms/:id/versions/:v/template.xlsx", h.templateXLSX)
	g.POST("/form-versions/:id/evaluate", h.evaluate)
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type formTemplateDTO struct {
	ID             string  `json:"id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	DepartmentCode string  `json:"department_code"`
	EntryLevel     string  `json:"entry_level"`
	Status         string  `json:"status"`
	RetiredReason  *string `json:"retired_reason"`
	CreatedBy      string  `json:"created_by"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func toFormTemplate(t store.FormTemplate) formTemplateDTO {
	return formTemplateDTO{ID: t.ID.String(), Code: t.Code, Name: t.Name, DepartmentCode: t.DepartmentCode, EntryLevel: t.EntryLevel, Status: t.Status,
		RetiredReason: t.RetiredReason, CreatedBy: t.CreatedBy.String(), CreatedAt: ts(t.CreatedAt), UpdatedAt: ts(t.UpdatedAt)}
}

type formVersionDTO struct {
	ID               string               `json:"id"`
	TemplateID       string               `json:"template_id"`
	Version          int                  `json:"version"`
	Status           string               `json:"status"`
	EffectiveFrom    *string              `json:"effective_from"`
	EffectiveLabel   *string              `json:"effective_label"`
	Origin           string               `json:"origin"`
	OriginVersionID  *string              `json:"origin_version_id"`
	ImportID         *string              `json:"import_id"`
	DefinitionSHA256 string               `json:"definition_sha256"`
	Completeness     string               `json:"completeness"`
	PendingCount     int                  `json:"pending_count"`
	ColumnCount      int                  `json:"column_count"`
	CreatedBy        string               `json:"created_by"`
	CreatedAt        string               `json:"created_at"`
	UpdatedBy        string               `json:"updated_by"`
	UpdatedAt        string               `json:"updated_at"`
	ProposedBy       *string              `json:"proposed_by"`
	ProposedAt       *string              `json:"proposed_at"`
	PublishedBy      *string              `json:"published_by"`
	PublishedAt      *string              `json:"published_at"`
	RetiredBy        *string              `json:"retired_by"`
	RetiredAt        *string              `json:"retired_at"`
	RetireReason     *string              `json:"retire_reason"`
	Definition       *port.FormDefinition `json:"definition,omitempty"`
}

func refStr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

func toFormVersion(v store.FormVersion, withDef bool) formVersionDTO {
	d := formVersionDTO{ID: v.ID.String(), TemplateID: v.TemplateID.String(), Version: v.Version, Status: v.Status, EffectiveFrom: dateStr(v.EffectiveFrom),
		EffectiveLabel: v.EffectiveLabel, Origin: v.Origin, OriginVersionID: refStr(v.OriginVersionID), ImportID: refStr(v.ImportID), DefinitionSHA256: v.SHA256,
		Completeness: v.Def.Completeness, PendingCount: len(v.Def.Pending), ColumnCount: len(v.Def.Columns),
		CreatedBy: v.CreatedBy.String(), CreatedAt: ts(v.CreatedAt), UpdatedBy: v.UpdatedBy.String(), UpdatedAt: ts(v.UpdatedAt),
		ProposedBy: refStr(v.ProposedBy), ProposedAt: tsp(v.ProposedAt), PublishedBy: refStr(v.PublishedBy), PublishedAt: tsp(v.PublishedAt),
		RetiredBy: refStr(v.RetiredBy), RetiredAt: tsp(v.RetiredAt), RetireReason: v.RetireReason}
	if withDef {
		def := v.Def
		d.Definition = &def
	}
	return d
}

func optVersion(v *store.FormVersion) *formVersionDTO {
	if v == nil {
		return nil
	}
	d := toFormVersion(*v, false)
	return &d
}

type assignmentDTO struct {
	ID             string  `json:"id"`
	TemplateID     string  `json:"template_id"`
	DepartmentCode string  `json:"department_code"`
	EntryLevel     string  `json:"entry_level"`
	ReportType     string  `json:"report_type"`
	Status         string  `json:"status"`
	AssignedBy     string  `json:"assigned_by"`
	AssignedAt     string  `json:"assigned_at"`
	RevokedBy      *string `json:"revoked_by"`
	RevokedAt      *string `json:"revoked_at"`
	RevokeReason   *string `json:"revoke_reason"`
}

func toAssignment(a store.Assignment) assignmentDTO {
	return assignmentDTO{ID: a.ID.String(), TemplateID: a.TemplateID.String(), DepartmentCode: a.DepartmentCode, EntryLevel: a.EntryLevel, ReportType: a.ReportType,
		Status: a.Status, AssignedBy: a.AssignedBy.String(), AssignedAt: ts(a.AssignedAt), RevokedBy: refStr(a.RevokedBy), RevokedAt: tsp(a.RevokedAt), RevokeReason: a.RevokeReason}
}

func assignments(as []store.Assignment) []assignmentDTO {
	out := make([]assignmentDTO, 0, len(as))
	for _, a := range as {
		out = append(out, toAssignment(a))
	}
	return out
}

// ---------------------------------------------------------------------------
// Tham số đường dẫn
// ---------------------------------------------------------------------------

func formPath(c *gin.Context) (uuid.UUID, int, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrFormNotFound)
		return uuid.Nil, 0, false
	}
	if c.Param("v") == "" {
		return id, 0, true
	}
	n, err := strconv.Atoi(c.Param("v"))
	if err != nil || n < 1 {
		httpx.Fail(c, app.ErrFormVersionNotFound)
		return uuid.Nil, 0, false
	}
	return id, n, true
}

func parseDate(field, s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "ngày dạng YYYY-MM-DD")
	}
	return &t, nil
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

func (h Handler) listForms(c *gin.Context) {
	out, err := h.Svc.ListForms(c.Request.Context(), store.FormTemplateFilter{Department: c.Query("department"), Level: c.Query("level"), Status: c.Query("status")}, c.Query("report_type"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(out))
	for _, s := range out {
		items = append(items, gin.H{"template": toFormTemplate(s.Template), "open_version": optVersion(s.Open), "published_version": optVersion(s.Published), "report_types": s.ReportTypes})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type createFormReq struct {
	Code             string               `json:"code"`
	Name             string               `json:"name"`
	DepartmentCode   string               `json:"department_code"`
	EntryLevel       string               `json:"entry_level"`
	Definition       *port.FormDefinition `json:"definition"`
	CloneFromVersion *string              `json:"clone_from_version_id"`
}

func (h Handler) createForm(c *gin.Context) {
	var req createFormReq
	if !bind(c, &req) {
		return
	}
	in := app.CreateFormInput{Code: req.Code, Name: req.Name, DepartmentCode: req.DepartmentCode, EntryLevel: req.EntryLevel, Definition: req.Definition}
	if req.CloneFromVersion != nil {
		id, err := uuid.Parse(*req.CloneFromVersion)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("clone_from_version_id", "UUID"))
			return
		}
		in.CloneVersionID = &id
	}
	t, v, err := h.Svc.CreateForm(c.Request.Context(), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"template": toFormTemplate(t), "version": toFormVersion(v, true)})
}

func (h Handler) getForm(c *gin.Context) {
	id, _, ok := formPath(c)
	if !ok {
		return
	}
	d, err := h.Svc.GetForm(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	vs := make([]formVersionDTO, 0, len(d.Versions))
	for _, v := range d.Versions {
		vs = append(vs, toFormVersion(v, false))
	}
	c.JSON(http.StatusOK, gin.H{"template": toFormTemplate(d.Template), "versions": vs, "assignments": assignments(d.Assignments)})
}

func (h Handler) retireForm(c *gin.Context) {
	id, _, ok := formPath(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	t, err := h.Svc.RetireForm(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toFormTemplate(t))
}

func (h Handler) formLog(c *gin.Context) {
	id, _, ok := formPath(c)
	if !ok {
		return
	}
	var before *uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		u, uerr := uuid.FromBytes(b)
		if err != nil || uerr != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		before = &u
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	evs, next, err := h.Svc.FormLog(c.Request.Context(), id, before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(evs))
	for _, e := range evs {
		var ch any = []any{}
		if len(e.Changes) > 0 {
			_ = json.Unmarshal(e.Changes, &ch)
		}
		items = append(items, gin.H{"id": e.ID.String(), "event": e.Action, "actor_ref": e.ActorRef.String(), "actor_display": nil, "at": ts(e.EventTime),
			"version_id": refStr(e.VersionID), "version": e.Version, "changes": ch, "reason": e.Reason, "seq": e.Seq})
	}
	var nc any
	if next != nil {
		nc = base64.RawURLEncoding.EncodeToString(next[:])
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nc})
}

type newVersionReq struct {
	FromVersion *int `json:"from_version"`
	Restore     bool `json:"restore"`
}

func (h Handler) newVersion(c *gin.Context) {
	id, _, ok := formPath(c)
	if !ok {
		return
	}
	var req newVersionReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.NewFormVersion(c.Request.Context(), id, req.FromVersion, req.Restore, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toFormVersion(v, true))
}

func (h Handler) getVersion(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	t, v, err := h.Svc.GetFormVersion(c.Request.Context(), id, n)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d := toFormVersion(v, true)
	c.JSON(http.StatusOK, gin.H{"template": toFormTemplate(t), "version": d})
}

type draftReq struct {
	Definition     *port.FormDefinition `json:"definition"`
	EffectiveFrom  string               `json:"effective_from"`
	EffectiveLabel *string              `json:"effective_label"`
}

func (h Handler) saveDraft(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	var req draftReq
	if !bind(c, &req) {
		return
	}
	eff, err := parseDate("effective_from", req.EffectiveFrom)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, err := h.Svc.SaveFormDraft(c.Request.Context(), id, n, app.DraftInput{Definition: req.Definition, EffectiveFrom: eff, EffectiveLabel: req.EffectiveLabel}, idem(c))
	versionResp(c, v, err)
}

type opsReq struct {
	Ops []form.Op `json:"ops"`
}

func (h Handler) applyOps(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	var req opsReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.ApplyFormOps(c.Request.Context(), id, n, req.Ops, idem(c))
	versionResp(c, v, err)
}

func versionResp(c *gin.Context, v store.FormVersion, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toFormVersion(v, true))
}

type proposeReq struct {
	EffectiveFrom  string  `json:"effective_from"`
	EffectiveLabel *string `json:"effective_label"`
}

func (h Handler) propose(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	var req proposeReq
	if !bind(c, &req) {
		return
	}
	eff, err := parseDate("effective_from", req.EffectiveFrom)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, err := h.Svc.ProposeFormVersion(c.Request.Context(), id, n, app.ProposeInput{EffectiveFrom: eff, EffectiveLabel: req.EffectiveLabel}, idem(c))
	versionResp(c, v, err)
}

func (h Handler) withdraw(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.WithdrawFormProposal(c.Request.Context(), id, n, req.Reason, idem(c))
	versionResp(c, v, err)
}

func (h Handler) publish(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	v, err := h.Svc.PublishFormVersion(c.Request.Context(), id, n, idem(c))
	versionResp(c, v, err)
}

func (h Handler) retireVersion(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.RetireFormVersion(c.Request.Context(), id, n, req.Reason, idem(c))
	versionResp(c, v, err)
}

func (h Handler) importForm(c *gin.Context) {
	if mt := strings.TrimSpace(strings.Split(c.GetHeader("Content-Type"), ";")[0]); mt != xlsxType {
		httpx.Fail(c, app.ErrImportInvalid.WithDetail("Content-Type phải là "+xlsxType))
		return
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, formimport.MaxBytes+1))
	if err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("không đọc được tệp"))
		return
	}
	in := app.ImportInput{Code: c.Query("code"), Name: c.Query("name"), DepartmentCode: c.Query("department_code"), EntryLevel: c.DefaultQuery("entry_level", port.LevelCommune),
		FileName: c.Query("file_name"), Options: formimport.Options{Sheet: c.Query("sheet")}}
	if s := c.Query("template_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, app.ErrFormNotFound)
			return
		}
		in.TemplateID = &id
	}
	for name, dst := range map[string]*int{"header_start": &in.Options.HeaderStart, "data_row": &in.Options.DataRow} {
		if s := c.Query(name); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > 1000 {
				httpx.Fail(c, problem.Validation.WithField(name, "số dòng 1–1000"))
				return
			}
			*dst = n
		}
	}
	res, err := h.Svc.ImportForm(c.Request.Context(), data, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"import_id": res.ImportID.String(), "template": toFormTemplate(res.Template), "version": toFormVersion(res.Version, true), "report": res.Report})
}

type seedReq struct {
	DepartmentCode string `json:"department_code"`
}

func (h Handler) seedForms(c *gin.Context) {
	var req seedReq
	if !bind(c, &req) {
		return
	}
	res, err := h.Svc.SeedTrongTrot(c.Request.Context(), req.DepartmentCode, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"created": res.Created, "skipped": res.Skipped})
}

func (h Handler) listAssignments(c *gin.Context) {
	f := store.AssignmentFilter{Department: c.Query("department"), Level: c.Query("level"), ReportType: c.Query("report_type"), ActiveOnly: c.Query("active") != "false"}
	if s := c.Query("template_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("template_id", "UUID"))
			return
		}
		f.TemplateID = &id
	}
	out, err := h.Svc.ListFormAssignments(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": assignments(out)})
}

type assignReq struct {
	TemplateID string `json:"template_id"`
	ReportType string `json:"report_type"`
}

func (h Handler) assign(c *gin.Context) {
	var req assignReq
	if !bind(c, &req) {
		return
	}
	id, err := uuid.Parse(req.TemplateID)
	if err != nil {
		httpx.Fail(c, app.ErrFormNotFound)
		return
	}
	a, err := h.Svc.AssignForm(c.Request.Context(), id, req.ReportType, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toAssignment(a))
}

func (h Handler) revokeAssignment(c *gin.Context) {
	id, ok := pathID(c, app.ErrAssignmentNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	a, err := h.Svc.RevokeAssignment(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toAssignment(a))
}

// ---------------------------------------------------------------------------
// Application
// ---------------------------------------------------------------------------

func (h Handler) availableForms(c *gin.Context) {
	at := h.Svc.Clock.Now().In(vnZone()) // ngày Việt Nam hiện tại khi không truyền at
	if s := c.Query("at"); s != "" {
		t, err := parseDate("at", s)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		at = *t
	}
	out, err := h.Svc.AvailableForms(c.Request.Context(), c.Query("department"), c.Query("level"), c.Query("report_type"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(out))
	for _, a := range out {
		items = append(items, gin.H{"template": toFormTemplate(a.Template), "report_type": a.ReportType, "version": toFormVersion(a.Version, false)})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func vnZone() *time.Location { return time.FixedZone("ICT", 7*3600) }

func (h Handler) layout(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	o := form.LayoutOptions{Season: c.Query("season")}
	for _, l := range c.QueryArray("period_label") {
		if l = strings.TrimSpace(l); l != "" {
			o.PeriodLabels = append(o.PeriodLabels, l)
		}
	}
	t, v, err := h.Svc.FormLayout(c.Request.Context(), id, n, o)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"template": toFormTemplate(t), "version": toFormVersion(v, false), "definition": v.Def})
}

func (h Handler) templateXLSX(c *gin.Context) {
	id, n, ok := formPath(c)
	if !ok {
		return
	}
	t, v, err := h.Svc.FormLayout(c.Request.Context(), id, n, form.LayoutOptions{Season: c.Query("season")})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	b, err := formimport.Export(v.Def)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+t.Code+`-v`+strconv.Itoa(v.Version)+`.xlsx"`)
	c.Data(http.StatusOK, xlsxType, b)
}

func (h Handler) evaluate(c *gin.Context) {
	id, ok := pathID(c, app.ErrFormVersionNotFound)
	if !ok {
		return
	}
	var in port.EvalInput
	if !bind(c, &in) {
		return
	}
	r, totals, err := h.Svc.EvaluateFormVersion(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if totals == nil {
		totals = []port.TotalRowResult{}
	}
	c.JSON(http.StatusOK, gin.H{"valid": r.Valid, "rows": r.Rows, "issues": r.Issues, "totals": totals})
}
