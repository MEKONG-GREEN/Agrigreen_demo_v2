package handler

// HTTP của KN v7.7 gói P2: phòng ban, khu vực, cây 3 cấp, bảng ảnh hưởng, menu theo cấu hình.
// Admin API (MFA): /v1/departments*, /v1/regions*, /v1/orgs/{ref}/region|impact|communes/ensure|departments/seed.
// Application API: GET /v1/departments, /v1/departments/{code}/modules, /v1/menu, /v1/regions, /v1/orgs/tree.

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// KN gói app.KN cho HTTP.
type KN struct{ Svc *app.KN }

// RegisterAdmin gắn route Admin API.
func (h KN) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/departments", h.adminListDepartments)
	rg.POST("/departments", h.createDepartment)
	rg.GET("/departments/:id", h.getDepartment)
	rg.PUT("/departments/:id", h.updateDepartment)
	rg.POST("/departments/:id/status", h.departmentStatus)
	rg.POST("/departments/:id/retire", h.retireDepartment)
	rg.GET("/departments/:id/log", h.departmentLog)
	rg.GET("/regions", h.listRegions)
	rg.POST("/regions", h.createRegion)
	rg.POST("/regions/seed", h.seedRegions)
	rg.GET("/regions/:id", h.getRegion)
	rg.PUT("/regions/:id", h.updateRegion)
	rg.POST("/regions/:id/deactivate", h.setRegionActive(false))
	rg.POST("/regions/:id/reactivate", h.setRegionActive(true))
	rg.GET("/regions/:id/log", h.regionLog)
	rg.GET("/orgs/tree", h.tree)
	rg.POST("/orgs/:ref/region", h.assignRegion)
	rg.GET("/orgs/:ref/region-log", h.orgRegionLog)
	rg.POST("/orgs/:ref/communes/ensure", h.ensureCommunes)
	rg.GET("/orgs/:ref/impact", h.impact)
	rg.POST("/orgs/:ref/departments/seed", h.seedDepartments)
}

// RegisterApp gắn route Application API (đọc).
func (h KN) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/departments", h.appListDepartments)
	rg.GET("/departments/:code/modules", h.modules)
	rg.GET("/menu", h.menu)
	rg.GET("/regions", h.listRegions)
	rg.GET("/orgs/tree", h.tree)
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type departmentDTO struct {
	ID           string  `json:"id"`
	OrgRef       string  `json:"org_ref"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	SortOrder    int     `json:"sort_order"`
	Status       string  `json:"status"`
	StatusLabel  string  `json:"status_label"`
	MenuProfile  string  `json:"menu_profile"`
	Retired      bool    `json:"retired"`
	Selectable   bool    `json:"selectable"`
	AccountCount *int    `json:"account_count"`
	RecordCount  *int    `json:"record_count"`
	UpdatedBy    string  `json:"updated_by"`
	UpdatedAt    string  `json:"updated_at"`
}

func toDepartmentDTO(d port.Department, u *port.Usage) departmentDTO {
	out := departmentDTO{ID: d.ID.String(), OrgRef: d.OrgRef.String(), Code: d.Code, Name: d.Name, Description: strPtr(d.Description),
		SortOrder: d.SortOrder, Status: d.Status, StatusLabel: domain.StatusLabel(d.Status), MenuProfile: d.MenuProfile, Retired: d.Retired,
		Selectable: d.Selectable(), UpdatedBy: d.UpdatedBy.String(), UpdatedAt: d.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if u != nil {
		a, r := u.Accounts, u.Records
		out.AccountCount, out.RecordCount = &a, &r
	}
	return out
}

type logEntryDTO struct {
	Seq          int64                `json:"seq"`
	Event        string               `json:"event"`
	ActorRef     string               `json:"actor_ref"`
	ActorDisplay *string              `json:"actor_display"`
	At           string               `json:"at"`
	RecordedAt   string               `json:"recorded_at"`
	Changes      []domain.FieldChange `json:"changes"`
	Reason       *string              `json:"reason"`
}

func writeLog(c *gin.Context, es []app.LogEntry, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]logEntryDTO, 0, len(es))
	var next *int64
	for _, e := range es {
		items = append(items, logEntryDTO{Seq: e.Seq, Event: e.Event, ActorRef: e.ActorRef.String(), ActorDisplay: strPtr(e.ActorDisplay),
			At: e.At.UTC().Format(time.RFC3339Nano), RecordedAt: e.RecordedAt.UTC().Format(time.RFC3339Nano), Changes: e.Changes, Reason: e.Reason})
	}
	if n := len(es); n > 0 && n == clampLimit(limitOf(c)) {
		s := es[n-1].Seq
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func limitOf(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

func afterSeq(c *gin.Context) (int64, bool) {
	s := c.Query("cursor")
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		httpx.Fail(c, problem.Validation.WithField("cursor", "phải là số seq ≥ 0"))
		return 0, false
	}
	return n, true
}

func pathID(c *gin.Context, name string, nf error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, nf)
		return uuid.Nil, false
	}
	return id, true
}

func optRefQuery(c *gin.Context, name string) (uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải là UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func idemKey(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// ---------------------------------------------------------------------------
// Phòng ban
// ---------------------------------------------------------------------------

func (h KN) listDepartments(c *gin.Context, admin bool) {
	unit, ok := optRefQuery(c, "org_ref")
	if !ok {
		return
	}
	vs, resolved, err := h.Svc.ListDepartments(c.Request.Context(), unit, c.Query("include_retired") == "true", admin)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]departmentDTO, 0, len(vs))
	for _, v := range vs {
		items = append(items, toDepartmentDTO(v.Department, v.Usage))
	}
	c.JSON(http.StatusOK, gin.H{"org_ref": resolved.String(), "items": items})
}

func (h KN) adminListDepartments(c *gin.Context) { h.listDepartments(c, true) }
func (h KN) appListDepartments(c *gin.Context)   { h.listDepartments(c, false) }

type departmentReq struct {
	OrgRef      string `json:"org_ref"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
}

func (h KN) createDepartment(c *gin.Context) {
	var req departmentReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.DepartmentInput{Code: req.Code, Name: req.Name, Description: req.Description, SortOrder: req.SortOrder, Status: req.Status,
		MenuProfile: req.MenuProfile}
	if ref, err := uuid.Parse(req.OrgRef); err == nil {
		in.OrgRef = ref
	}
	d, replayed, err := h.Svc.CreateDepartment(c.Request.Context(), in, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/departments/"+d.ID.String())
	written(c, replayed, toDepartmentDTO(d, nil))
}

func (h KN) getDepartment(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrDepartmentNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetDepartment(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDepartmentDTO(v.Department, v.Usage))
}

func (h KN) updateDepartment(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrDepartmentNotFound)
	if !ok {
		return
	}
	var req departmentReq
	if !bindJSON(c, &req) {
		return
	}
	d, err := h.Svc.UpdateDepartment(c.Request.Context(), id, app.DepartmentInput{Name: req.Name, Description: req.Description,
		SortOrder: req.SortOrder, MenuProfile: req.MenuProfile})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDepartmentDTO(d, nil))
}

type deptStatusReq struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (h KN) departmentStatus(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrDepartmentNotFound)
	if !ok {
		return
	}
	var req deptStatusReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.ChangeDepartmentStatus(c.Request.Context(), id, req.Status, req.Reason, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	written(c, res.Replayed, gin.H{"department": toDepartmentDTO(res.Department, res.Usage), "warnings": warnings})
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h KN) retireDepartment(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrDepartmentNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bindJSON(c, &req) {
		return
	}
	d, replayed, err := h.Svc.RetireDepartment(c.Request.Context(), id, req.Reason, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	written(c, replayed, toDepartmentDTO(d, nil))
}

func (h KN) departmentLog(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrDepartmentNotFound)
	if !ok {
		return
	}
	after, ok := afterSeq(c)
	if !ok {
		return
	}
	es, err := h.Svc.DepartmentLog(c.Request.Context(), id, after, limitOf(c))
	writeLog(c, es, err)
}

func (h KN) seedDepartments(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrOrgNotFound)
	if !ok {
		return
	}
	res, err := h.Svc.SeedDepartments(c.Request.Context(), ref, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]departmentDTO, 0, len(res.Created))
	for _, d := range res.Created {
		items = append(items, toDepartmentDTO(d, nil))
	}
	c.JSON(http.StatusOK, gin.H{"created": items, "existing": res.Existing, "status": res.Status,
		"legacy_retired": res.LegacyRetired, "legacy_kept": res.LegacyKept})
}

// ---------------------------------------------------------------------------
// Khu vực
// ---------------------------------------------------------------------------

type regionDTO struct {
	ID                 string  `json:"id"`
	ProvinceCode       string  `json:"province_code"`
	Code               string  `json:"code"`
	Name               string  `json:"name"`
	SortOrder          int     `json:"sort_order"`
	Active             bool    `json:"active"`
	DeactivationReason *string `json:"deactivation_reason"`
	CommuneCount       int     `json:"commune_count"`
	UpdatedBy          string  `json:"updated_by"`
	UpdatedAt          string  `json:"updated_at"`
}

func toRegionDTO(r store.RegionRow, communes int) regionDTO {
	return regionDTO{ID: r.ID.String(), ProvinceCode: r.ProvinceCode, Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Active: r.Active,
		DeactivationReason: strPtr(r.DeactivationReason), CommuneCount: communes, UpdatedBy: r.UpdatedBy.String(),
		UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func (h KN) listRegions(c *gin.Context) {
	vs, err := h.Svc.ListRegions(c.Request.Context(), c.Query("province_code"), c.Query("include_inactive") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]regionDTO, 0, len(vs))
	total := 0
	for _, v := range vs {
		items = append(items, toRegionDTO(v.RegionRow, v.Communes))
		total += v.Communes
	}
	c.JSON(http.StatusOK, gin.H{"province_code": c.Query("province_code"), "items": items, "assigned_communes": total})
}

type regionReq struct {
	ProvinceCode string `json:"province_code"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	SortOrder    int    `json:"sort_order"`
}

func (h KN) createRegion(c *gin.Context) {
	var req regionReq
	if !bindJSON(c, &req) {
		return
	}
	r, replayed, err := h.Svc.CreateRegion(c.Request.Context(), port.Region{ProvinceCode: req.ProvinceCode, Code: req.Code, Name: req.Name,
		SortOrder: req.SortOrder}, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/regions/"+r.ID.String())
	written(c, replayed, toRegionDTO(r, 0))
}

func (h KN) getRegion(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrRegionNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetRegion(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toRegionDTO(v.RegionRow, v.Communes))
}

func (h KN) updateRegion(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrRegionNotFound)
	if !ok {
		return
	}
	var req regionReq
	if !bindJSON(c, &req) {
		return
	}
	r, err := h.Svc.UpdateRegion(c.Request.Context(), id, req.Name, req.SortOrder)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toRegionDTO(r, 0))
}

func (h KN) setRegionActive(active bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, "id", app.ErrRegionNotFound)
		if !ok {
			return
		}
		var req reasonReq
		if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
			return
		}
		r, replayed, err := h.Svc.SetRegionActive(c.Request.Context(), id, active, req.Reason, idemKey(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		written(c, replayed, toRegionDTO(r, 0))
	}
}

func (h KN) regionLog(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrRegionNotFound)
	if !ok {
		return
	}
	after, ok := afterSeq(c)
	if !ok {
		return
	}
	es, err := h.Svc.RegionLog(c.Request.Context(), id, after, limitOf(c))
	writeLog(c, es, err)
}

func (h KN) seedRegions(c *gin.Context) {
	var req struct {
		ProvinceCode string `json:"province_code"`
	}
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.SeedRegions(c.Request.Context(), req.ProvinceCode, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"province_code": res.ProvinceCode, "regions_created": res.RegionsCreated, "assigned": res.Assigned,
		"already_assigned": res.AlreadyAssigned, "missing_node": res.MissingNode, "status": res.Status})
}

type assignReq struct {
	RegionCode *string `json:"region_code"`
	Reason     string  `json:"reason"`
}

func (h KN) assignRegion(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrOrgNotFound)
	if !ok {
		return
	}
	var req assignReq
	if !bindJSON(c, &req) {
		return
	}
	code := ""
	if req.RegionCode != nil {
		code = *req.RegionCode
	}
	res, err := h.Svc.AssignRegion(c.Request.Context(), ref, code, req.Reason, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	written(c, res.Replayed, gin.H{"org_ref": res.OrgRef.String(), "commune_code": res.CommuneCode, "province_code": res.ProvinceCode,
		"region_code": strPtr(res.RegionCode), "action": res.Action})
}

func (h KN) orgRegionLog(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrOrgNotFound)
	if !ok {
		return
	}
	after, ok := afterSeq(c)
	if !ok {
		return
	}
	es, err := h.Svc.OrgRegionLog(c.Request.Context(), ref, after, limitOf(c))
	writeLog(c, es, err)
}

// ---------------------------------------------------------------------------
// Cây 3 cấp, bảo đảm Nút xã, ảnh hưởng
// ---------------------------------------------------------------------------

type regionRefDTO struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type treeNodeDTO struct {
	Ref           *string       `json:"ref"`
	Kind          string        `json:"kind"`
	Code          string        `json:"code"`
	Name          string        `json:"name"`
	AdminUnitCode *string       `json:"admin_unit_code"`
	Level         int           `json:"level"`
	Active        bool          `json:"active"`
	Region        *regionRefDTO `json:"region"`
	CommuneCount  int           `json:"commune_count"`
	Children      []treeNodeDTO `json:"children"`
}

func toTreeDTO(n app.TreeNode) treeNodeDTO {
	d := treeNodeDTO{Ref: refPtr(n.Ref), Kind: n.Kind, Code: n.Code, Name: n.Name, AdminUnitCode: strPtr(n.AdminUnitCode), Level: n.Level,
		Active: n.Active, CommuneCount: n.CommuneCount, Children: make([]treeNodeDTO, 0, len(n.Children))}
	if n.Region != nil {
		d.Region = &regionRefDTO{Code: n.Region.RegionCode, Name: n.Region.RegionName}
	}
	for _, ch := range n.Children {
		d.Children = append(d.Children, toTreeDTO(ch))
	}
	return d
}

func (h KN) tree(c *gin.Context) {
	ns, err := h.Svc.OrgTree(c.Request.Context(), c.Query("include_inactive") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]treeNodeDTO, 0, len(ns))
	for _, n := range ns {
		items = append(items, toTreeDTO(n))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h KN) ensureCommunes(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrOrgNotFound)
	if !ok {
		return
	}
	var req struct {
		CodePrefix string `json:"code_prefix"`
	}
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.EnsureCommuneNodes(c.Request.Context(), ref, req.CodePrefix, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created := make([]orgDTO, 0, len(res.Created))
	for _, o := range res.Created {
		created = append(created, toOrgDTO(o))
	}
	c.JSON(http.StatusOK, gin.H{"province_code": res.ProvinceCode, "communes": res.Communes, "existing": res.Existing, "created": created})
}

type impactRowDTO struct {
	Kind      string           `json:"kind"`
	Label     string           `json:"label"`
	Count     *int             `json:"count"`
	AreaHa    *decimal.Decimal `json:"area_ha,omitempty"`
	Available bool             `json:"available"`
}

func (h KN) impact(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrOrgNotFound)
	if !ok {
		return
	}
	im, err := h.Svc.OrgImpact(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]impactRowDTO, 0, len(im.Rows))
	for _, r := range im.Rows {
		d := impactRowDTO{Kind: r.Kind, Label: r.Label, AreaHa: r.AreaHa, Available: r.Available}
		if r.Available {
			n := r.Count
			d.Count = &n
		}
		rows = append(rows, d)
	}
	warnings := im.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"org": toOrgDTO(im.Org), "communes": im.Communes, "rows": rows, "warnings": warnings, "movable": im.Movable})
}

// withRegions gắn khu vực vào Nút xã của danh sách /v1/orgs (FN-AD-04.02: Khu vực trả kèm trong /v1/orgs).
func withRegions(ctx context.Context, svc *app.KN, items []orgDTO, orgs []port.Org) {
	if svc == nil {
		return
	}
	var refs []uuid.UUID
	for _, o := range orgs {
		if o.Kind == port.OrgExtensionTeam {
			refs = append(refs, o.Ref)
		}
	}
	if len(refs) == 0 {
		return
	}
	regions, err := svc.RegionsOf(ctx, refs)
	if err != nil {
		return // trang trí: lỗi không chặn danh sách
	}
	for i, o := range orgs {
		if r, ok := regions[o.Ref]; ok {
			items[i].Region = &regionRefDTO{Code: r.RegionCode, Name: r.RegionName}
		}
	}
}

// ---------------------------------------------------------------------------
// Menu theo cấu hình (UR-KT-02)
// ---------------------------------------------------------------------------

type menuItemDTO struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Route       *string       `json:"route"`
	Icon        string        `json:"icon"`
	BadgeSource *string       `json:"badge_source"`
	Enabled     bool          `json:"enabled"`
	Status      *string       `json:"status,omitempty"`
	StatusLabel *string       `json:"status_label,omitempty"`
	Children    []menuItemDTO `json:"children"`
}

type menuGroupDTO struct {
	Key   string        `json:"key"`
	Label string        `json:"label"`
	Items []menuItemDTO `json:"items"`
}

type menuDeptDTO struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
}

func toMenuItems(items []app.MenuItemView) []menuItemDTO {
	out := make([]menuItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, menuItemDTO{Key: it.Key, Label: it.Label, Route: strPtr(it.Route), Icon: it.Icon, BadgeSource: strPtr(it.BadgeSource),
			Enabled: it.Enabled, Status: strPtr(it.Status), StatusLabel: strPtr(it.StatusLabel), Children: toMenuItems(it.Children)})
	}
	return out
}

func writeMenu(c *gin.Context, v app.MenuView, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	// Menu đổi ngay khi Admin đổi trạng thái phòng ban — không lưu đệm phía trung gian.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, menuBody(v))
}

// menuBody là thân JSON của KNMenu (contracts/openapi/application.yaml) — dùng chung với ca vàng web.
func menuBody(v app.MenuView) gin.H {
	groups := make([]menuGroupDTO, 0, len(v.Groups))
	for _, g := range v.Groups {
		groups = append(groups, menuGroupDTO{Key: g.Key, Label: g.Label, Items: toMenuItems(g.Items)})
	}
	var dept *menuDeptDTO
	if v.Department != nil {
		dept = &menuDeptDTO{Code: v.Department.Code, Name: v.Department.Name, Status: v.Department.Status, MenuProfile: v.Department.MenuProfile}
	}
	return gin.H{"menu_version": v.Version, "audience": v.Audience, "org_ref": refPtr(v.UnitRef), "department": dept,
		"default_route": v.DefaultRoute, "groups": groups}
}

func menuQuery(c *gin.Context) (app.MenuQuery, bool) {
	unit, ok := optRefQuery(c, "org_ref")
	if !ok {
		return app.MenuQuery{}, false
	}
	// "role" là tên cũ của "audience" (hợp đồng tạm P6 trước KN-fix) — nhận thêm một đợt, "audience" thắng.
	aud := c.Query("audience")
	if aud == "" {
		aud = c.Query("role")
	}
	return app.MenuQuery{Audience: aud, Department: c.Query("department"), UnitRef: unit}, true
}

func (h KN) menu(c *gin.Context) {
	q, ok := menuQuery(c)
	if !ok {
		return
	}
	v, err := h.Svc.Menu(c.Request.Context(), q)
	writeMenu(c, v, err)
}

func (h KN) modules(c *gin.Context) {
	q, ok := menuQuery(c)
	if !ok {
		return
	}
	v, err := h.Svc.DepartmentModules(c.Request.Context(), c.Param("code"), q)
	writeMenu(c, v, err)
}
