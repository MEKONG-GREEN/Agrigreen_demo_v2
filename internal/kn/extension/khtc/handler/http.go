// Package handler: route /v1/kn/khtc/… (W11.61 — Danh mục KHTC, danh sách CT/DA, Kế hoạch năm). Mọi route cần header
// X-AG-Context (ngữ cảnh phiên KN).
package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/kn/khtc/catalogs", h.listCatalog)
	rg.POST("/kn/khtc/catalogs/:kind", h.createCatalog)
	rg.POST("/kn/khtc/catalogs/:kind/:code/revise", h.reviseCatalog)
	rg.GET("/kn/khtc/catalogs/:kind/:code/history", h.catalogHistory)
	rg.GET("/kn/khtc/programs", h.listPrograms)
	rg.POST("/kn/khtc/programs", h.createProgram)
	rg.POST("/kn/khtc/programs/:ref/revise", h.reviseProgram)
	rg.GET("/kn/khtc/programs/:ref/history", h.programHistory)
	rg.GET("/kn/khtc/plans/:year", h.getPlan)
	rg.GET("/kn/khtc/plans/:year/versions", h.versions)
	rg.POST("/kn/khtc/plans/:year/versions", h.saveVersion)
	rg.GET("/kn/khtc/plans/:year/copy-draft", h.copyDraft)
	rg.GET("/kn/khtc/plans/:year/export", h.export)
	rg.POST("/kn/khtc/plans/:year/import-draft", h.importDraft)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func day(t *time.Time) any {
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

func optDec(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func optRef(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

func bind(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func created(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

func actor(a app.Actor) gin.H { return gin.H{"ref": a.Ref.String(), "display": a.Display} }

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// ---------------------------------------------------------------------------
// Danh mục KHTC
// ---------------------------------------------------------------------------

func catDTO(v app.CatalogView) gin.H {
	attrs := v.Attrs
	if attrs == nil {
		attrs = map[string]string{}
	}
	df := v.DefaultFor
	if df == nil {
		df = []string{}
	}
	return gin.H{"kind": v.Kind, "code": v.Code, "name": v.Name, "sort_order": v.SortOrder, "active": v.Status == domain.StatusActive,
		"default_for": df, "attrs": attrs, "version": v.Seq, "version_id": v.ID.String(), "recorded_at": ts(v.RecordedAt),
		"actor": actor(v.Actor)}
}

func (h Handler) listCatalog(c *gin.Context) {
	res, err := h.Svc.ListCatalog(c.Request.Context(), c.Query("kind"), c.Query("include_retired") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	counts := make([]gin.H, 0, len(res.Counts))
	for _, k := range res.Counts {
		counts = append(counts, gin.H{"kind": k.Kind, "label": domain.KindLabel[k.Kind], "active": k.Active, "total": k.Total})
	}
	items := make([]gin.H, 0, len(res.Items))
	for _, v := range res.Items {
		items = append(items, catDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"counts": counts, "items": items})
}

type catalogReq struct {
	Code       string            `json:"code"`
	Name       string            `json:"name"`
	SortOrder  int               `json:"sort_order"`
	Active     *bool             `json:"active"`
	DefaultFor []string          `json:"default_for"`
	Attrs      map[string]string `json:"attrs"`
}

func (r catalogReq) item(kind, code string) domain.CatalogItem {
	st := domain.StatusActive
	if r.Active != nil && !*r.Active {
		st = domain.StatusRetired
	}
	return domain.CatalogItem{Kind: kind, Code: code, Name: r.Name, SortOrder: r.SortOrder, Status: st, DefaultFor: r.DefaultFor, Attrs: r.Attrs}
}

func (h Handler) createCatalog(c *gin.Context) {
	var req catalogReq
	if !bind(c, &req) {
		return
	}
	v, rep, err := h.Svc.CreateCatalog(c.Request.Context(), app.CatalogInput{Item: req.item(c.Param("kind"), req.Code), IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, catDTO(v))
}

func (h Handler) reviseCatalog(c *gin.Context) {
	var req catalogReq
	if !bind(c, &req) {
		return
	}
	v, rep, err := h.Svc.ReviseCatalog(c.Request.Context(), app.CatalogInput{Item: req.item(c.Param("kind"), c.Param("code")), IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, catDTO(v))
}

func (h Handler) catalogHistory(c *gin.Context) {
	vs, err := h.Svc.CatalogHistory(c.Request.Context(), c.Param("kind"), c.Param("code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		items = append(items, catDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Danh sách CT/DA
// ---------------------------------------------------------------------------

func progDTO(v app.ProgramView) gin.H {
	return gin.H{"ref": v.Ref.String(), "code": v.Code, "name": v.Name, "kind": v.Kind, "field_code": optStr(v.FieldCode),
		"owner_department": optStr(v.OwnerDepartment), "start_year": optInt(v.StartYear), "end_year": optInt(v.EndYear),
		"active": v.Status == domain.StatusActive, "note": optStr(v.Note), "version": v.Seq, "recorded_at": ts(v.RecordedAt),
		"actor": actor(v.Actor)}
}

func optInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func (h Handler) listPrograms(c *gin.Context) {
	vs, err := h.Svc.ListPrograms(c.Request.Context(), c.Query("kind"), c.Query("include_retired") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		items = append(items, progDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type programReq struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	FieldCode       string `json:"field_code"`
	OwnerDepartment string `json:"owner_department"`
	StartYear       int    `json:"start_year"`
	EndYear         int    `json:"end_year"`
	Active          *bool  `json:"active"`
	Note            string `json:"note"`
}

func (r programReq) program() domain.Program {
	st := domain.StatusActive
	if r.Active != nil && !*r.Active {
		st = domain.StatusRetired
	}
	return domain.Program{Code: r.Code, Name: r.Name, Kind: r.Kind, FieldCode: r.FieldCode, OwnerDepartment: r.OwnerDepartment,
		StartYear: r.StartYear, EndYear: r.EndYear, Status: st, Note: r.Note}
}

func (h Handler) createProgram(c *gin.Context) {
	var req programReq
	if !bind(c, &req) {
		return
	}
	v, rep, err := h.Svc.CreateProgram(c.Request.Context(), app.ProgramInput{Program: req.program(), IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, progDTO(v))
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrProgramNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) reviseProgram(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req programReq
	if !bind(c, &req) {
		return
	}
	v, rep, err := h.Svc.ReviseProgram(c.Request.Context(), app.ProgramInput{Ref: ref, Program: req.program(), IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, progDTO(v))
}

func (h Handler) programHistory(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	vs, err := h.Svc.ProgramHistory(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		items = append(items, progDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Kế hoạch năm
// ---------------------------------------------------------------------------

func pathYear(c *gin.Context) (int, bool) {
	y, err := strconv.Atoi(c.Param("year"))
	if err != nil || y < 2000 || y > 2100 {
		httpx.Fail(c, problem.Validation.WithField("year", "năm 2000–2100"))
		return 0, false
	}
	return y, true
}

func verDTO(v app.VersionView) gin.H {
	return gin.H{"id": v.ID.String(), "year": v.Year, "version": v.Seq, "basis": v.Basis, "decision_no": optStr(v.DecisionNo),
		"decision_date": day(v.DecisionDate), "effective_from": v.EffectiveFrom.Format(time.DateOnly),
		"copied_from_year": optInt(v.CopiedFromYear), "note": optStr(v.Note), "entries_count": v.EntriesCount,
		"recorded_at": ts(v.RecordedAt), "actor": actor(v.Actor)}
}

func entryDTO(e app.EntryView) gin.H {
	communes := e.Communes
	if communes == nil {
		communes = []string{}
	}
	return gin.H{"ref": e.Ref.String(), "tab": e.Tab, "kind": e.Kind, "parent_ref": optRef(e.ParentRef), "program_ref": optRef(e.ProgramRef),
		"program_code": optStr(e.ProgramCode), "program_name": optStr(e.ProgramName), "roman": optStr(e.Roman),
		"sort_order": e.SortOrder, "code": optStr(e.Code), "name": e.Name, "unit_code": optStr(e.UnitCode),
		"planned_qty": optDec(e.PlannedQty), "scale_note": optStr(e.ScaleNote), "budget": optDec(e.Budget),
		"funding_code": optStr(e.FundingCode), "field_code": optStr(e.FieldCode), "department_code": optStr(e.Department),
		"communes": communes, "points_count": e.PointsCount, "data_source": optStr(e.DataSource), "source_key": optStr(e.SourceKey),
		"tracking": e.Tracking, "stopped_from": day(e.StoppedFrom), "stop_decision": optStr(e.StopDecision), "note": optStr(e.Note)}
}

func entryDTOs(es []app.EntryView) []gin.H {
	out := make([]gin.H, 0, len(es))
	for _, e := range es {
		out = append(out, entryDTO(e))
	}
	return out
}

func planDTO(p app.PlanView) gin.H {
	return gin.H{"year": p.Version.Year, "version": verDTO(p.Version), "latest_version": p.Latest, "entries": entryDTOs(p.Entries)}
}

func (h Handler) getPlan(c *gin.Context) {
	y, ok := pathYear(c)
	if !ok {
		return
	}
	ver := 0
	if v := c.Query("version"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.Fail(c, problem.Validation.WithField("version", "số phiên bản ≥ 1"))
			return
		}
		ver = n
	}
	p, err := h.Svc.GetPlan(c.Request.Context(), y, ver, c.Query("at"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, planDTO(p))
}

func (h Handler) versions(c *gin.Context) {
	y, ok := pathYear(c)
	if !ok {
		return
	}
	vs, err := h.Svc.Versions(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		items = append(items, verDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type entryReq struct {
	Ref            string           `json:"ref"`
	Tab            string           `json:"tab"`
	Kind           string           `json:"kind"`
	ParentRef      *string          `json:"parent_ref"`
	ProgramRef     *string          `json:"program_ref"`
	SortOrder      int              `json:"sort_order"`
	Code           string           `json:"code"`
	Name           string           `json:"name"`
	UnitCode       string           `json:"unit_code"`
	PlannedQty     *decimal.Decimal `json:"planned_qty"`
	ScaleNote      string           `json:"scale_note"`
	Budget         *decimal.Decimal `json:"budget"`
	FundingCode    string           `json:"funding_code"`
	FieldCode      string           `json:"field_code"`
	DepartmentCode string           `json:"department_code"`
	Communes       []string         `json:"communes"`
	PointsCount    *int             `json:"points_count"`
	DataSource     string           `json:"data_source"`
	SourceKey      string           `json:"source_key"`
	Tracking       string           `json:"tracking"`
	StoppedFrom    *string          `json:"stopped_from"`
	StopDecision   string           `json:"stop_decision"`
	Note           string           `json:"note"`
}

type saveReq struct {
	BaseVersion    int        `json:"base_version"`
	DecisionNo     string     `json:"decision_no"`
	DecisionDate   string     `json:"decision_date"`
	EffectiveFrom  string     `json:"effective_from"`
	CopiedFromYear int        `json:"copied_from_year"`
	Note           string     `json:"note"`
	Entries        []entryReq `json:"entries"`
}

func optUUID(p **problem.Problem, bad *bool, field string, s *string) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	u, err := uuid.Parse(*s)
	if err != nil {
		*p, *bad = (*p).WithField(field, "phải là UUID"), true
		return nil
	}
	return &u
}

func (r saveReq) entries() ([]domain.Entry, error) {
	p, bad := problem.Validation, false
	out := make([]domain.Entry, 0, len(r.Entries))
	for i, e := range r.Entries {
		f := func(n string) string { return "entries." + strconv.Itoa(i) + "." + n }
		ref, err := uuid.Parse(e.Ref)
		if err != nil {
			p, bad = p.WithField(f("ref"), "phải là UUID"), true
		}
		d := domain.Entry{Ref: ref, Tab: e.Tab, Kind: e.Kind, SortOrder: e.SortOrder, Code: e.Code, Name: e.Name, UnitCode: e.UnitCode,
			PlannedQty: e.PlannedQty, ScaleNote: e.ScaleNote, Budget: e.Budget, FundingCode: e.FundingCode, FieldCode: e.FieldCode,
			Department: e.DepartmentCode, Communes: e.Communes, PointsCount: e.PointsCount, DataSource: e.DataSource,
			SourceKey: e.SourceKey, Tracking: e.Tracking, StopDecision: e.StopDecision, Note: e.Note}
		d.ParentRef = optUUID(&p, &bad, f("parent_ref"), e.ParentRef)
		d.ProgramRef = optUUID(&p, &bad, f("program_ref"), e.ProgramRef)
		if e.StoppedFrom != nil && *e.StoppedFrom != "" {
			t, err := domain.ParseDate(*e.StoppedFrom)
			if err != nil {
				p, bad = p.WithField(f("stopped_from"), "phải là ngày YYYY-MM-DD"), true
			} else {
				d.StoppedFrom = &t
			}
		}
		out = append(out, d)
	}
	if bad {
		return nil, p
	}
	return out, nil
}

func (h Handler) saveVersion(c *gin.Context) {
	y, ok := pathYear(c)
	if !ok {
		return
	}
	var req saveReq
	if !bind(c, &req) {
		return
	}
	es, err := req.entries()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	p, rep, err := h.Svc.SaveVersion(c.Request.Context(), app.SaveInput{Year: y, BaseVersion: req.BaseVersion, DecisionNo: req.DecisionNo,
		DecisionDate: req.DecisionDate, EffectiveFrom: req.EffectiveFrom, CopiedFromYear: req.CopiedFromYear, Note: req.Note,
		Entries: es, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, planDTO(p))
}

func draftDTO(d app.Draft) gin.H {
	probs := make([]gin.H, 0, len(d.Problems))
	for _, p := range d.Problems {
		probs = append(probs, gin.H{"row": optInt(p.Row), "field": p.Field, "reason": p.Reason})
	}
	return gin.H{"from_year": optInt(d.FromYear), "from_version": optInt(d.FromVersion), "entries": entryDTOs(d.Entries), "problems": probs}
}

func (h Handler) copyDraft(c *gin.Context) {
	y, ok := pathYear(c)
	if !ok {
		return
	}
	d, err := h.Svc.CopyDraft(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, draftDTO(d))
}

func (h Handler) export(c *gin.Context) {
	y, ok := pathYear(c)
	if !ok {
		return
	}
	ver := 0
	if v := c.Query("version"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.Fail(c, problem.Validation.WithField("version", "số phiên bản ≥ 1"))
			return
		}
		ver = n
	}
	ex, err := h.Svc.ExportPlan(c.Request.Context(), y, ver)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\""+ex.FileName+"\"; filename*=UTF-8''"+url.PathEscape(ex.FileName))
	c.Data(http.StatusOK, ex.ContentType, ex.Body)
}

func (h Handler) importDraft(c *gin.Context) {
	y, ok := pathYear(c)
	if !ok {
		return
	}
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Fail(c, app.ErrImport.WithDetail("không đọc được thân request"))
		return
	}
	d, err := h.Svc.ImportDraft(c.Request.Context(), y, raw)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, draftDTO(d))
}
