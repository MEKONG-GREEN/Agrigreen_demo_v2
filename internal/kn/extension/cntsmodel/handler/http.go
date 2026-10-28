// Package handler: route /v1/extension/cnts/projects…, /demo-sites…, /model-households…, /grants…, /grant-candidates,
// /khtc-programs (W11.47 — CT/DA, điểm trình diễn, hộ mô hình, cấp quyền CNTS). Mọi route cần header X-AG-Context (ngữ
// cảnh phiên KN); thao tác ghi (POST) cần Idempotency-Key. Sửa là POST lên tài nguyên kèm base_version.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts")
	g.GET("/projects", h.listProjects)
	g.POST("/projects", h.createProject)
	g.GET("/projects/:ref", h.getProject)
	g.POST("/projects/:ref", h.updateProject)
	g.POST("/projects/:ref/close", h.projectStatus(true))
	g.POST("/projects/:ref/reopen", h.projectStatus(false))
	g.GET("/khtc-programs", h.programs)
	g.GET("/demo-sites", h.listSites)
	g.POST("/demo-sites", h.createSite)
	g.GET("/demo-sites/:ref", h.getSite)
	g.POST("/demo-sites/:ref", h.updateSite)
	g.POST("/demo-sites/:ref/close", h.siteStatus(true))
	g.POST("/demo-sites/:ref/reopen", h.siteStatus(false))
	g.GET("/model-households", h.listHouseholds)
	g.POST("/model-households", h.createHousehold)
	g.GET("/model-households/:ref", h.getHousehold)
	g.POST("/model-households/:ref", h.updateHousehold)
	g.POST("/model-households/:ref/withdraw", h.householdStatus(true))
	g.POST("/model-households/:ref/reinstate", h.householdStatus(false))
	g.GET("/grants", h.listGrants)
	g.POST("/grants", h.createGrant)
	g.GET("/grants/:ref", h.getGrant)
	g.POST("/grants/:ref", h.updateGrant)
	g.POST("/grants/:ref/revoke", h.revokeGrant)
	g.GET("/grant-candidates", h.candidates)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bind(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ: "+err.Error()))
		return false
	}
	return true
}

// form gom lỗi phân tích trường.
type form struct {
	p   problem.Problem
	bad bool
}

func newForm() *form { return &form{p: *problem.Validation} }

func (f *form) fail(field, reason string) { f.p, f.bad = *f.p.WithField(field, reason), true }

func (f *form) ref(field string, s *string, v7 bool) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	r, err := uuid.Parse(*s)
	if err != nil || (v7 && r.Version() != 7) {
		f.fail(field, "phải là UUID"+map[bool]string{true: "v7", false: ""}[v7])
		return nil
	}
	return &r
}

func (f *form) refs(field string, ss []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		r, err := uuid.Parse(s)
		if err != nil {
			f.fail(field, "phải là UUID")
			return nil
		}
		out = append(out, r)
	}
	return out
}

func (f *form) date(field string, s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	d, err := time.Parse(time.DateOnly, *s)
	if err != nil {
		f.fail(field, "dạng YYYY-MM-DD")
		return nil
	}
	return &d
}

func (f *form) dec(field string, s *string) *decimal.Decimal {
	if s == nil || *s == "" {
		return nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		f.fail(field, "số thập phân")
		return nil
	}
	return &d
}

func (f *form) done(c *gin.Context) bool {
	if f.bad {
		httpx.Fail(c, &f.p)
		return false
	}
	return true
}

func pathRef(c *gin.Context, nf error) (uuid.UUID, bool) {
	r, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, nf)
		return uuid.Nil, false
	}
	return r, true
}

func queryRef(c *gin.Context, f *form, key string) *uuid.UUID {
	v := c.Query(key)
	return f.ref(key, &v, false)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nameOf(m map[uuid.UUID]string, r *uuid.UUID) any {
	if r == nil {
		return nil
	}
	if n, ok := m[*r]; ok && n != "" {
		return n
	}
	return nil
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

func optDate(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.DateOnly)
}

func optDec(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func refsOut(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func pointOut(p *domain.Point) any {
	if p == nil {
		return nil
	}
	return gin.H{"lat": p.Lat, "lng": p.Lng, "source": p.Source}
}

type pointReq struct {
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Source string  `json:"source"`
}

func (p *pointReq) domain() *domain.Point {
	if p == nil {
		return nil
	}
	return &domain.Point{Lat: p.Lat, Lng: p.Lng, Source: p.Source}
}

func meta(m store.Meta) gin.H {
	return gin.H{"version": m.Seq, "updated_at": m.RecordedAt.UTC().Format(time.RFC3339), "updated_by": m.ActorRef.String()}
}

func merge(a gin.H, b gin.H) gin.H {
	for k, v := range b {
		a[k] = v
	}
	return a
}

func status(replayed bool, created int) int {
	if replayed {
		return http.StatusOK
	}
	return created
}

type statusReq struct {
	BaseVersion *int64 `json:"base_version"`
	Reason      string `json:"reason"`
}

// ---------------------------------------------------------------------------
// CT/DA
// ---------------------------------------------------------------------------

func projectCore(p store.Project) gin.H {
	return merge(gin.H{"ref": p.Ref.String(), "event": p.Event, "province_code": p.Province, "code": p.Code, "name": p.Name,
		"kind": p.Kind, "basis": optStr(p.Basis), "scale_note": optStr(p.ScaleNote), "start_date": p.Start.Format(time.DateOnly),
		"end_date": optDate(p.End), "budget": optDec(p.Budget), "manager_ref": p.ManagerRef.String(), "program_ref": optRef(p.ProgramRef),
		"training_note": optStr(p.TrainingNote), "supplies_note": optStr(p.SuppliesNote), "attachment_refs": refsOut(p.Attachments),
		"status_reason": optStr(p.StatusReason)}, meta(p.Meta))
}

func projectOut(v app.ProjectView, names map[uuid.UUID]string) gin.H {
	m := projectCore(v.Project)
	m["status"], m["site_count"], m["household_count"], m["can_edit"] = v.Status, v.SiteCount, v.HouseholdCount, v.CanEdit
	m["manager_display"] = nameOf(names, &v.ManagerRef)
	return m
}

func (h Handler) listProjects(c *gin.Context) {
	list, names, canCreate, err := h.Svc.ListProjects(c.Request.Context(), app.ProjectQuery{Status: c.Query("status"),
		Kind: c.Query("kind"), Q: c.Query("q")})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, v := range list {
		items = append(items, projectOut(v, names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "can_create": canCreate})
}

func (h Handler) writeProject(c *gin.Context, v app.ProjectView, err error, created int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m := projectOut(v, v.Names)
	sites := make([]gin.H, 0, len(v.Sites))
	for _, s := range v.Sites {
		sites = append(sites, siteOut(s, v.Names, nil))
	}
	vers := make([]gin.H, 0, len(v.Versions))
	for i := len(v.Versions) - 1; i >= 0; i-- {
		x := projectCore(v.Versions[i])
		x["actor_display"] = nameOf(v.Names, &v.Versions[i].ActorRef)
		vers = append(vers, x)
	}
	var program any
	if v.Program != nil {
		program = gin.H{"ref": v.Program.Ref.String(), "code": v.Program.Code, "name": v.Program.Name, "kind": v.Program.Kind, "active": v.Program.Active}
	}
	m["sites"], m["versions"], m["program"] = sites, vers, program
	c.JSON(status(v.Replayed, created), m)
}

type projectReq struct {
	ID             *string  `json:"id"`
	BaseVersion    *int64   `json:"base_version"`
	ProvinceCode   *string  `json:"province_code"`
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Basis          *string  `json:"basis"`
	ScaleNote      *string  `json:"scale_note"`
	StartDate      string   `json:"start_date"`
	EndDate        *string  `json:"end_date"`
	Budget         *string  `json:"budget"`
	ManagerRef     *string  `json:"manager_ref"`
	ProgramRef     *string  `json:"program_ref"`
	TrainingNote   *string  `json:"training_note"`
	SuppliesNote   *string  `json:"supplies_note"`
	AttachmentRefs []string `json:"attachment_refs"`
}

func (r projectReq) input(c *gin.Context, f *form) app.ProjectInput {
	in := app.ProjectInput{BaseVersion: r.BaseVersion, Province: deref(r.ProvinceCode), IdemKey: idem(c), Project: domain.Project{
		Code: r.Code, Name: r.Name, Kind: r.Kind, Basis: deref(r.Basis), ScaleNote: deref(r.ScaleNote), End: f.date("end_date", r.EndDate),
		Budget: f.dec("budget", r.Budget), ProgramRef: f.ref("program_ref", r.ProgramRef, false), TrainingNote: deref(r.TrainingNote),
		SuppliesNote: deref(r.SuppliesNote), Attachments: f.refs("attachment_refs", r.AttachmentRefs)}}
	if d := f.date("start_date", &r.StartDate); d != nil {
		in.Start = *d
	}
	if m := f.ref("manager_ref", r.ManagerRef, false); m != nil {
		in.ManagerRef = *m
	}
	in.Ref = f.ref("id", r.ID, true)
	return in
}

func (h Handler) createProject(c *gin.Context) {
	var req projectReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := req.input(c, f)
	if !f.done(c) {
		return
	}
	v, err := h.Svc.CreateProject(c.Request.Context(), in)
	h.writeProject(c, v, err, http.StatusCreated)
}

func (h Handler) getProject(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrProjectNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetProject(c.Request.Context(), ref)
	h.writeProject(c, v, err, http.StatusOK)
}

func (h Handler) updateProject(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrProjectNotFound)
	if !ok {
		return
	}
	var req projectReq
	if !bind(c, &req) {
		return
	}
	req.ID = nil
	f := newForm()
	in := req.input(c, f)
	if !f.done(c) {
		return
	}
	in.Ref = &ref
	v, err := h.Svc.UpdateProject(c.Request.Context(), in)
	h.writeProject(c, v, err, http.StatusOK)
}

func (h Handler) projectStatus(closeIt bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathRef(c, app.ErrProjectNotFound)
		if !ok {
			return
		}
		var req statusReq
		if !bind(c, &req) {
			return
		}
		v, err := h.Svc.SetProjectStatus(c.Request.Context(), ref, closeIt, req.BaseVersion, req.Reason, idem(c))
		h.writeProject(c, v, err, http.StatusOK)
	}
}

func (h Handler) programs(c *gin.Context) {
	list, err := h.Svc.KHTCPrograms(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, p := range list {
		items = append(items, gin.H{"ref": p.Ref.String(), "code": p.Code, "name": p.Name, "kind": p.Kind, "active": p.Active})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Điểm trình diễn
// ---------------------------------------------------------------------------

func siteCore(s store.Site) gin.H {
	return merge(gin.H{"ref": s.Ref.String(), "event": s.Event, "project_ref": s.ProjectRef.String(), "commune_code": s.CommuneCode,
		"name": s.Name, "model_type_code": s.ModelType, "scale": optDec(s.Scale.Value), "scale_unit": optStr(s.Scale.Unit),
		"officer_ref": optRef(s.OfficerRef), "diary_form": optStr(s.DiaryForm), "cooperative_ref": optRef(s.CooperativeRef),
		"note": optStr(s.Note), "status_reason": optStr(s.StatusReason)}, meta(s.Meta))
}

func siteOut(v app.SiteView, names map[uuid.UUID]string, labels map[string]string) gin.H {
	m := siteCore(v.Site)
	m["project_code"], m["project_name"], m["status"], m["location"] = v.ProjectCode, v.ProjectName, v.Status, pointOut(v.Point)
	m["household_count"], m["can_edit"], m["can_add_households"] = v.HouseholdCount, v.CanEdit, v.CanAddHouseholds
	m["officer_display"] = nameOf(names, v.OfficerRef)
	name := v.ModelTypeName
	if labels != nil {
		name = labels[v.ModelType]
	}
	m["model_type_name"] = optStr(name)
	return m
}

func (h Handler) listSites(c *gin.Context) {
	f := newForm()
	q := app.SiteQuery{Project: queryRef(c, f, "project_ref"), Commune: c.Query("commune_code"), Status: c.Query("status")}
	if !f.done(c) {
		return
	}
	list, names, labels, err := h.Svc.ListSites(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, v := range list {
		items = append(items, siteOut(v, names, labels))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) writeSite(c *gin.Context, v app.SiteView, err error, created int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m := siteOut(v, v.Names, nil)
	hhs := make([]gin.H, 0, len(v.Households))
	for _, x := range v.Households {
		hhs = append(hhs, householdOut(x, v.Names))
	}
	vers := make([]gin.H, 0, len(v.Versions))
	for i := len(v.Versions) - 1; i >= 0; i-- {
		x := siteCore(v.Versions[i])
		x["actor_display"] = nameOf(v.Names, &v.Versions[i].ActorRef)
		vers = append(vers, x)
	}
	m["households"], m["versions"] = hhs, vers
	c.JSON(status(v.Replayed, created), m)
}

type siteReq struct {
	ID             *string   `json:"id"`
	BaseVersion    *int64    `json:"base_version"`
	ProjectRef     *string   `json:"project_ref"`
	CommuneCode    string    `json:"commune_code"`
	Name           string    `json:"name"`
	ModelTypeCode  string    `json:"model_type_code"`
	Scale          *string   `json:"scale"`
	ScaleUnit      *string   `json:"scale_unit"`
	OfficerRef     *string   `json:"officer_ref"`
	DiaryForm      *string   `json:"diary_form"`
	CooperativeRef *string   `json:"cooperative_ref"`
	Note           *string   `json:"note"`
	Location       *pointReq `json:"location"`
	RemoveLocation bool      `json:"remove_location"`
}

func (r siteReq) input(c *gin.Context, f *form) app.SiteInput {
	in := app.SiteInput{BaseVersion: r.BaseVersion, Point: r.Location.domain(), RemovePoint: r.RemoveLocation, IdemKey: idem(c),
		Site: domain.Site{CommuneCode: r.CommuneCode, Name: r.Name, ModelType: r.ModelTypeCode,
			Scale: domain.Scale{Value: f.dec("scale", r.Scale), Unit: deref(r.ScaleUnit)}, OfficerRef: f.ref("officer_ref", r.OfficerRef, false),
			DiaryForm: deref(r.DiaryForm), CooperativeRef: f.ref("cooperative_ref", r.CooperativeRef, false), Note: deref(r.Note)}}
	in.Ref = f.ref("id", r.ID, true)
	return in
}

func (h Handler) createSite(c *gin.Context) {
	var req siteReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := req.input(c, f)
	if p := f.ref("project_ref", req.ProjectRef, false); p != nil {
		in.ProjectRef = *p
	} else if !f.bad {
		f.fail("project_ref", "bắt buộc")
	}
	if !f.done(c) {
		return
	}
	v, err := h.Svc.CreateSite(c.Request.Context(), in)
	h.writeSite(c, v, err, http.StatusCreated)
}

func (h Handler) getSite(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrSiteNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetSite(c.Request.Context(), ref)
	h.writeSite(c, v, err, http.StatusOK)
}

func (h Handler) updateSite(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrSiteNotFound)
	if !ok {
		return
	}
	var req siteReq
	if !bind(c, &req) {
		return
	}
	req.ID = nil
	f := newForm()
	in := req.input(c, f)
	if !f.done(c) {
		return
	}
	in.Ref = &ref
	v, err := h.Svc.UpdateSite(c.Request.Context(), in)
	h.writeSite(c, v, err, http.StatusOK)
}

func (h Handler) siteStatus(closeIt bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathRef(c, app.ErrSiteNotFound)
		if !ok {
			return
		}
		var req statusReq
		if !bind(c, &req) {
			return
		}
		v, err := h.Svc.SetSiteStatus(c.Request.Context(), ref, closeIt, req.BaseVersion, req.Reason, idem(c))
		h.writeSite(c, v, err, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Hộ mô hình
// ---------------------------------------------------------------------------

func householdCore(x store.Household) gin.H {
	return merge(gin.H{"ref": x.Ref.String(), "event": x.Event, "site_ref": x.SiteRef.String(), "project_ref": x.ProjectRef.String(),
		"commune_code": x.CommuneCode, "person_ref": x.PersonRef.String(), "scale": optDec(x.Scale.Value), "scale_unit": optStr(x.Scale.Unit),
		"keeper_kind": x.KeeperKind, "keeper_ref": optRef(x.KeeperRef), "self_entry": x.SelfEntry, "joined_on": optDate(x.JoinedOn),
		"note": optStr(x.Note), "status_reason": optStr(x.StatusReason)}, meta(x.Meta))
}

func householdOut(v app.HouseholdView, names map[uuid.UUID]string) gin.H {
	m := householdCore(v.Household)
	m["site_name"], m["project_code"], m["status"], m["location"], m["can_edit"] = v.SiteName, v.ProjectCode, v.Status, pointOut(v.Point), v.CanEdit
	m["owner_label"], m["owner_phone_masked"], m["keeper_display"] = optStr(v.OwnerLabel), optStr(v.OwnerPhone), nameOf(names, v.KeeperRef)
	return m
}

func (h Handler) listHouseholds(c *gin.Context) {
	f := newForm()
	q := app.HouseholdQuery{Site: queryRef(c, f, "site_ref"), Project: queryRef(c, f, "project_ref"), Commune: c.Query("commune_code"),
		Status: c.Query("status")}
	if !f.done(c) {
		return
	}
	list, names, err := h.Svc.ListHouseholds(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, v := range list {
		items = append(items, householdOut(v, names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) writeHousehold(c *gin.Context, v app.HouseholdView, err error, created int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m := householdOut(v, v.Names)
	vers := make([]gin.H, 0, len(v.Versions))
	for i := len(v.Versions) - 1; i >= 0; i-- {
		x := householdCore(v.Versions[i])
		x["actor_display"] = nameOf(v.Names, &v.Versions[i].ActorRef)
		vers = append(vers, x)
	}
	m["versions"] = vers
	c.JSON(status(v.Replayed, created), m)
}

type householdReq struct {
	ID             *string   `json:"id"`
	BaseVersion    *int64    `json:"base_version"`
	SiteRef        *string   `json:"site_ref"`
	PersonRef      *string   `json:"person_ref"`
	OwnerName      *string   `json:"owner_name"`
	OwnerPhone     *string   `json:"owner_phone"`
	Scale          *string   `json:"scale"`
	ScaleUnit      *string   `json:"scale_unit"`
	KeeperKind     *string   `json:"keeper_kind"`
	KeeperRef      *string   `json:"keeper_ref"`
	SelfEntry      bool      `json:"self_entry"`
	JoinedOn       *string   `json:"joined_on"`
	Note           *string   `json:"note"`
	Location       *pointReq `json:"location"`
	RemoveLocation bool      `json:"remove_location"`
}

func (r householdReq) input(c *gin.Context, f *form) app.HouseholdInput {
	in := app.HouseholdInput{BaseVersion: r.BaseVersion, PersonRef: f.ref("person_ref", r.PersonRef, false), Point: r.Location.domain(),
		RemovePoint: r.RemoveLocation, IdemKey: idem(c), Household: domain.Household{
			Scale: domain.Scale{Value: f.dec("scale", r.Scale), Unit: deref(r.ScaleUnit)}, KeeperKind: deref(r.KeeperKind),
			KeeperRef: f.ref("keeper_ref", r.KeeperRef, false), SelfEntry: r.SelfEntry, JoinedOn: f.date("joined_on", r.JoinedOn),
			Note: deref(r.Note)}}
	if r.OwnerName != nil || r.OwnerPhone != nil {
		in.Owner = &domain.Owner{FullName: deref(r.OwnerName), Phone: deref(r.OwnerPhone)}
	}
	in.Ref = f.ref("id", r.ID, true)
	return in
}

func (h Handler) createHousehold(c *gin.Context) {
	var req householdReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := req.input(c, f)
	if s := f.ref("site_ref", req.SiteRef, false); s != nil {
		in.SiteRef = *s
	} else if !f.bad {
		f.fail("site_ref", "bắt buộc")
	}
	if !f.done(c) {
		return
	}
	v, err := h.Svc.CreateHousehold(c.Request.Context(), in)
	h.writeHousehold(c, v, err, http.StatusCreated)
}

func (h Handler) getHousehold(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrHouseholdNF)
	if !ok {
		return
	}
	v, err := h.Svc.GetHousehold(c.Request.Context(), ref)
	h.writeHousehold(c, v, err, http.StatusOK)
}

func (h Handler) updateHousehold(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrHouseholdNF)
	if !ok {
		return
	}
	var req householdReq
	if !bind(c, &req) {
		return
	}
	req.ID = nil
	f := newForm()
	in := req.input(c, f)
	if !f.done(c) {
		return
	}
	in.Ref = &ref
	v, err := h.Svc.UpdateHousehold(c.Request.Context(), in)
	h.writeHousehold(c, v, err, http.StatusOK)
}

func (h Handler) householdStatus(withdraw bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathRef(c, app.ErrHouseholdNF)
		if !ok {
			return
		}
		var req statusReq
		if !bind(c, &req) {
			return
		}
		v, err := h.Svc.SetHouseholdStatus(c.Request.Context(), ref, withdraw, req.BaseVersion, req.Reason, idem(c))
		h.writeHousehold(c, v, err, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Cấp quyền
// ---------------------------------------------------------------------------

func grantCore(g store.Grant) gin.H {
	return merge(gin.H{"ref": g.Ref.String(), "event": g.Event, "grantee_ref": g.GranteeRef.String(), "role": g.Role,
		"province_code": g.Province, "commune_codes": strs(g.Communes), "facility_refs": refsOut(g.Facilities), "site_refs": refsOut(g.Sites),
		"household_refs": refsOut(g.Households), "forms": strs(g.Forms), "valid_from": g.ValidFrom.Format(time.DateOnly),
		"valid_to": optDate(g.ValidTo), "successor_ref": optRef(g.SuccessorRef), "successor_grant_ref": optRef(g.SuccessorGrantRef),
		"reason": optStr(g.Reason)}, meta(g.Meta))
}

func grantOut(v app.GrantView, names map[uuid.UUID]string) gin.H {
	m := grantCore(v.Grant)
	m["status"], m["can_edit"] = v.Status, v.CanEdit
	m["grantee_display"], m["updated_by_display"] = nameOf(names, &v.GranteeRef), nameOf(names, &v.ActorRef)
	m["successor_display"] = nameOf(names, v.SuccessorRef)
	return m
}

func (h Handler) listGrants(c *gin.Context) {
	f := newForm()
	q := app.GrantQuery{Mine: c.Query("mine") == "true", Grantee: queryRef(c, f, "grantee_ref"), Role: c.Query("role"), Status: c.Query("status")}
	if !f.done(c) {
		return
	}
	list, names, canGrant, err := h.Svc.ListGrants(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, v := range list {
		items = append(items, grantOut(v, names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "can_grant": canGrant})
}

func (h Handler) writeGrant(c *gin.Context, v app.GrantView, err error, created int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m := grantOut(v, v.Names)
	vers := make([]gin.H, 0, len(v.Versions))
	for i := len(v.Versions) - 1; i >= 0; i-- {
		x := grantCore(v.Versions[i])
		x["actor_display"] = nameOf(v.Names, &v.Versions[i].ActorRef)
		vers = append(vers, x)
	}
	m["versions"] = vers
	c.JSON(status(v.Replayed, created), m)
}

type grantReq struct {
	BaseVersion   *int64   `json:"base_version"`
	GranteeRef    *string  `json:"grantee_ref"`
	ProvinceCode  *string  `json:"province_code"`
	Role          string   `json:"role"`
	CommuneCodes  []string `json:"commune_codes"`
	FacilityRefs  []string `json:"facility_refs"`
	SiteRefs      []string `json:"site_refs"`
	HouseholdRefs []string `json:"household_refs"`
	Forms         []string `json:"forms"`
	ValidFrom     *string  `json:"valid_from"`
	ValidTo       *string  `json:"valid_to"`
}

func (r grantReq) input(c *gin.Context, f *form) app.GrantInput {
	in := app.GrantInput{BaseVersion: r.BaseVersion, Province: deref(r.ProvinceCode), IdemKey: idem(c), Grant: domain.Grant{
		Role: r.Role, Communes: r.CommuneCodes, Facilities: f.refs("facility_refs", r.FacilityRefs), Sites: f.refs("site_refs", r.SiteRefs),
		Households: f.refs("household_refs", r.HouseholdRefs), Forms: r.Forms, ValidTo: f.date("valid_to", r.ValidTo)}}
	if d := f.date("valid_from", r.ValidFrom); d != nil {
		in.ValidFrom = *d
	}
	if g := f.ref("grantee_ref", r.GranteeRef, false); g != nil {
		in.GranteeRef = *g
	}
	return in
}

func (h Handler) createGrant(c *gin.Context) {
	var req grantReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := req.input(c, f)
	if !f.done(c) {
		return
	}
	v, err := h.Svc.CreateGrant(c.Request.Context(), in)
	h.writeGrant(c, v, err, http.StatusCreated)
}

func (h Handler) getGrant(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrGrantNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetGrant(c.Request.Context(), ref)
	h.writeGrant(c, v, err, http.StatusOK)
}

func (h Handler) updateGrant(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrGrantNotFound)
	if !ok {
		return
	}
	var req grantReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := req.input(c, f)
	if !f.done(c) {
		return
	}
	in.Ref = &ref
	v, err := h.Svc.UpdateGrant(c.Request.Context(), in)
	h.writeGrant(c, v, err, http.StatusOK)
}

type revokeReq struct {
	BaseVersion  *int64  `json:"base_version"`
	Reason       string  `json:"reason"`
	SuccessorRef *string `json:"successor_ref"`
}

func (h Handler) revokeGrant(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrGrantNotFound)
	if !ok {
		return
	}
	var req revokeReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := app.RevokeInput{Ref: ref, BaseVersion: req.BaseVersion, Reason: req.Reason, SuccessorRef: f.ref("successor_ref", req.SuccessorRef, false), IdemKey: idem(c)}
	if !f.done(c) {
		return
	}
	v, err := h.Svc.RevokeGrant(c.Request.Context(), in)
	h.writeGrant(c, v, err, http.StatusOK)
}

func (h Handler) candidates(c *gin.Context) {
	list, names, err := h.Svc.Candidates(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, x := range list {
		items = append(items, gin.H{"ref": x.Ref.String(), "display": nameOf(names, &x.Ref), "commune_codes": strs(x.Communes)})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
