package handler

import (
	"context"
	"encoding/base64"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Registry gói app.Registry cho HTTP: sổ tổ chức, HTX, nông hộ.
type Registry struct{ Reg *app.Registry }

// RegisterApp gắn route Application API.
func (h Registry) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/orgs", h.listOrgs)
	rg.GET("/cooperatives", h.listCooperatives)
	rg.GET("/cooperatives/:ref", h.getCooperative)
	rg.GET("/cooperatives/:ref/households", h.listHouseholds)
	rg.POST("/households", h.createHousehold)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc ở router).
func (h Registry) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/orgs", h.adminListOrgs)
	rg.POST("/orgs", h.createOrg)
	rg.GET("/orgs/export.xlsx", h.exportOrgTree)
	httpx.AllowBody(rg, http.MethodPost, "/orgs/import", app.MaxOrgTreeBytes)
	rg.POST("/orgs/import", h.importOrgTree)
	rg.POST("/orgs/:ref/move", h.moveOrg)
	rg.POST("/orgs/:ref/rename", h.renameOrg)
	rg.GET("/orgs/:ref/events", h.orgEvents)
	rg.POST("/cooperatives/:ref/deactivate", h.deactivate)
	rg.POST("/cooperatives/:ref/reactivate", h.reactivate)
	httpx.AllowBody(rg, http.MethodPost, "/households/import", app.MaxImportBytes)
	rg.POST("/households/import", h.importHouseholds)
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type orgDTO struct {
	Ref               string  `json:"ref"`
	Kind              string  `json:"kind"`
	Code              string  `json:"code"`
	Name              string  `json:"name"`
	AdminUnitCode     *string `json:"admin_unit_code"`
	ParentRef         *string `json:"parent_ref"`
	RepresentativeRef *string `json:"representative_ref"`
	Active            bool    `json:"active"`
}

func refPtr(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toOrgDTO(o port.Org) orgDTO {
	return orgDTO{
		Ref: o.Ref.String(), Kind: string(o.Kind), Code: o.Code, Name: o.Name, AdminUnitCode: strPtr(o.AdminUnitCode),
		ParentRef: refPtr(o.ParentRef), RepresentativeRef: refPtr(o.RepresentativeRef), Active: o.Active,
	}
}

type pointDTO struct {
	Lat decimal.Decimal `json:"lat"`
	Lon decimal.Decimal `json:"lon"`
}

type coopDTO struct {
	orgDTO
	StatusReason  *string          `json:"status_reason"`
	TaxCode       *string          `json:"tax_code"`
	AreaHa        *decimal.Decimal `json:"area_ha"`
	EstablishedOn *string          `json:"established_on"`
	Location      *pointDTO        `json:"location"`
	// LocationProvisional: chưa có toạ độ — bản đồ hiển thị tâm xã kèm nhãn "vị trí tạm" (CGH FN-03 AC-03).
	LocationProvisional bool `json:"location_provisional"`
}

func toCoopDTO(c store.Cooperative) coopDTO {
	d := coopDTO{
		orgDTO: toOrgDTO(c.Org), StatusReason: strPtr(c.StatusReason), TaxCode: strPtr(c.Profile.TaxCode),
		AreaHa: c.Profile.AreaHa, LocationProvisional: c.Profile.Location == nil,
	}
	if c.Profile.EstablishedOn != nil {
		s := c.Profile.EstablishedOn.Format(time.DateOnly)
		d.EstablishedOn = &s
	}
	if c.Profile.Location != nil {
		d.Location = &pointDTO{Lat: c.Profile.Location.Lat, Lon: c.Profile.Location.Lon}
	}
	return d
}

type eventDTO struct {
	Seq           int64          `json:"seq"`
	Action        string         `json:"action"`
	EventTime     string         `json:"event_time"`
	RecordedAt    string         `json:"recorded_at"`
	ActorRef      string         `json:"actor_ref"`
	Reason        *string        `json:"reason"`
	Name          string         `json:"name"`
	AdminUnitCode *string        `json:"admin_unit_code"`
	ParentRef     *string        `json:"parent_ref"`
	PrevParentRef *string        `json:"prev_parent_ref"`
	PrevAdminUnit *string        `json:"prev_admin_unit_code"`
	Active        bool           `json:"active"`
	Details       map[string]any `json:"details,omitempty"`
}

func toEventDTO(e domain.OrgEvent) eventDTO {
	return eventDTO{
		Seq: e.Seq, Action: string(e.Action), EventTime: e.EventTime.UTC().Format(time.RFC3339Nano),
		RecordedAt: e.RecordedAt.UTC().Format(time.RFC3339Nano), ActorRef: e.ActorRef.String(), Reason: strPtr(e.Reason),
		Name: e.Org.Name, AdminUnitCode: strPtr(e.Org.AdminUnitCode), ParentRef: refPtr(e.Org.ParentRef),
		PrevParentRef: refPtr(e.PrevParentRef), PrevAdminUnit: strPtr(e.PrevAdminUnit), Active: e.Org.Active, Details: e.Details,
	}
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

func parseCursor(c *gin.Context) (uuid.UUID, bool) {
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return after, false
		}
		copy(after[:], b)
	}
	return after, true
}

func encodeCursor(next *uuid.UUID) any {
	if next == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(next[:])
}

func queryLimit(c *gin.Context) int {
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 50
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrOrgNotFound)
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

// fieldErrs gom lỗi theo trường khi đọc thân request.
type fieldErrs struct{ p *problem.Problem }

func (f *fieldErrs) add(field, reason string) {
	if f.p == nil {
		f.p = problem.Validation
	}
	f.p = f.p.WithField(field, reason)
}

// fail ghi lỗi nếu có; trả true khi đã ghi.
func (f *fieldErrs) fail(c *gin.Context) bool {
	if f.p == nil {
		return false
	}
	httpx.Fail(c, f.p)
	return true
}

func (f *fieldErrs) optRef(field string, s *string) uuid.UUID {
	if s == nil || *s == "" {
		return uuid.Nil
	}
	u, err := ids.ParseRef(*s)
	if err != nil {
		f.add(field, "phải là UUID")
	}
	return u
}

func (f *fieldErrs) date(field string, s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, *s)
	if err != nil {
		f.add(field, "phải có dạng YYYY-MM-DD")
		return nil
	}
	return &t
}

// ---------------------------------------------------------------------------
// Application API
// ---------------------------------------------------------------------------

func (h Registry) listOrgs(c *gin.Context) {
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	page, err := h.Reg.ListOrgs(c.Request.Context(), app.ListQuery{
		Kind: c.Query("kind"), IncludeInactive: c.Query("include_inactive") == "true", After: after, Limit: queryLimit(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]orgDTO, 0, len(page.Items))
	for _, o := range page.Items {
		items = append(items, toOrgDTO(o))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(page.Next)})
}

func (h Registry) listCooperatives(c *gin.Context) {
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	page, err := h.Reg.ListCooperatives(c.Request.Context(), app.ListQuery{
		Q: c.Query("q"), IncludeInactive: c.Query("include_inactive") == "true", After: after, Limit: queryLimit(c),
		AdminUnit: c.Query("admin_unit"),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, err := h.coopItems(c.Request.Context(), page.Items)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(page.Next)})
}

func (h Registry) getCooperative(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	out, err := h.Reg.GetCooperative(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, err := h.coopItems(c.Request.Context(), []store.Cooperative{out})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, items[0])
}

type householdDTO struct {
	HouseholdRef     string           `json:"household_ref"`
	CooperativeRef   *string          `json:"cooperative_ref"`
	CommuneCode      string           `json:"commune_code"`
	CultivatedAreaHa *decimal.Decimal `json:"cultivated_area_ha"`
	MemberSince      *string          `json:"member_since"`
	Active           bool             `json:"active"`
}

func toHouseholdDTO(hh domain.Household) householdDTO {
	d := householdDTO{
		HouseholdRef: hh.Ref.String(), CooperativeRef: refPtr(hh.CooperativeRef), CommuneCode: hh.CommuneCode,
		CultivatedAreaHa: hh.CultivatedAreaHa, Active: hh.Active,
	}
	if hh.MemberSince != nil {
		s := hh.MemberSince.Format(time.DateOnly)
		d.MemberSince = &s
	}
	return d
}

type householdListItem struct {
	householdDTO
	Label            string `json:"label" pii:"true"`
	MaskedPhone      string `json:"masked_phone" pii:"true"`
	LabelUnavailable bool   `json:"label_unavailable"`
	// Consents: trạng thái đồng ý hiện tại theo mục đích (G-04, R-41/R-42).
	Consents []consentStateDTO `json:"consents"`
}

func (h Registry) listHouseholds(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	page, err := h.Reg.ListHouseholds(c.Request.Context(), ref, after, queryLimit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	refs := make([]uuid.UUID, 0, len(page.Items))
	for _, it := range page.Items {
		refs = append(refs, it.Ref)
	}
	consents, err := h.consentsOf(c.Request.Context(), refs)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]householdListItem, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, householdListItem{
			householdDTO: toHouseholdDTO(it.Household), Label: it.Label, MaskedPhone: it.MaskedPhone, LabelUnavailable: it.LabelUnavailable,
			Consents: consents[it.Ref],
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(page.Next)})
}

type identityReq struct {
	FullName   string `json:"full_name" pii:"true"`
	Phone      string `json:"phone" pii:"true"`
	NationalID string `json:"national_id" pii:"true"`
	Address    string `json:"address" pii:"true"`
}

type createHouseholdReq struct {
	CooperativeRef   *string          `json:"cooperative_ref"`
	CommuneCode      string           `json:"commune_code"`
	CultivatedAreaHa *decimal.Decimal `json:"cultivated_area_ha"`
	MemberSince      *string          `json:"member_since"`
	Identity         identityReq      `json:"identity"`
}

func (h Registry) createHousehold(c *gin.Context) {
	var req createHouseholdReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	in := app.CreateHouseholdInput{
		Identity: app.FarmerIdentity{
			FullName: req.Identity.FullName, Phone: req.Identity.Phone, NationalID: req.Identity.NationalID, Address: req.Identity.Address,
		},
		CommuneCode: req.CommuneCode, CultivatedAreaHa: req.CultivatedAreaHa,
	}
	in.CooperativeRef = fe.optRef("cooperative_ref", req.CooperativeRef)
	in.MemberSince = fe.date("member_since", req.MemberSince)
	if fe.fail(c) {
		return
	}
	out, err := h.Reg.CreateHousehold(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	body := gin.H{"household": toHouseholdDTO(out.Household), "duplicate": out.Duplicate}
	if out.Duplicate {
		body["duplicate_label"] = app.DuplicateLabel
	}
	if !out.Created {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

// ---------------------------------------------------------------------------
// Admin API
// ---------------------------------------------------------------------------

type coopProfileReq struct {
	TaxCode       string           `json:"tax_code"`
	AreaHa        *decimal.Decimal `json:"area_ha"`
	EstablishedOn *string          `json:"established_on"`
	Location      *pointDTO        `json:"location"`
}

type representativeReq struct {
	FullName string `json:"full_name" pii:"true"`
	Phone    string `json:"phone" pii:"true"`
}

type createOrgReq struct {
	Kind              string             `json:"kind"`
	Code              string             `json:"code"`
	Name              string             `json:"name"`
	AdminUnitCode     string             `json:"admin_unit_code"`
	ParentRef         *string            `json:"parent_ref"`
	RepresentativeRef *string            `json:"representative_ref"`
	Representative    *representativeReq `json:"representative"`
	Cooperative       *coopProfileReq    `json:"cooperative"`
}

func (h Registry) createOrg(c *gin.Context) {
	var req createOrgReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	in := app.CreateOrgInput{
		Kind: port.OrgKind(req.Kind), Code: req.Code, Name: req.Name, AdminUnitCode: req.AdminUnitCode,
		IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	}
	in.ParentRef = fe.optRef("parent_ref", req.ParentRef)
	in.RepresentativeRef = fe.optRef("representative_ref", req.RepresentativeRef)
	if req.Representative != nil {
		in.Representative = &app.Representative{FullName: req.Representative.FullName, Phone: req.Representative.Phone}
	}
	if req.Cooperative != nil {
		p := domain.CooperativeProfile{TaxCode: req.Cooperative.TaxCode, AreaHa: req.Cooperative.AreaHa}
		p.EstablishedOn = fe.date("cooperative.established_on", req.Cooperative.EstablishedOn)
		if req.Cooperative.Location != nil {
			p.Location = &domain.Point{Lat: req.Cooperative.Location.Lat, Lon: req.Cooperative.Location.Lon}
		}
		in.Cooperative = &p
	}
	if fe.fail(c) {
		return
	}
	out, err := h.Reg.CreateOrg(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if out.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/orgs/"+out.Org.Ref.String())
	c.JSON(status, gin.H{"org": toOrgDTO(out.Org), "seq": out.Event.Seq, "representative_duplicate": out.RepresentativeDuplicate})
}

type moveReq struct {
	NewParentRef     *string `json:"new_parent_ref"`
	NewAdminUnitCode string  `json:"new_admin_unit_code"`
	Reason           string  `json:"reason"`
}

func (h Registry) moveOrg(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req moveReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	parent := fe.optRef("new_parent_ref", req.NewParentRef)
	if fe.fail(c) {
		return
	}
	out, err := h.Reg.MoveOrg(c.Request.Context(), app.MoveOrgInput{
		Ref: ref, NewParentRef: parent, NewAdminUnitCode: req.NewAdminUnitCode, Reason: req.Reason,
		IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	})
	writeOrgResult(c, out, err)
}

type statusReq struct {
	Reason string `json:"reason"`
	Name   string `json:"name"`
}

type statusFn func(ctx context.Context, in app.StatusInput) (app.OrgResult, error)

func status(c *gin.Context, kind port.OrgKind, fn statusFn) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req statusReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	out, err := fn(ctx, app.StatusInput{
		Ref: ref, ExpectKind: kind, Reason: req.Reason, Name: req.Name, IdemKey: idempotency.KeyFromContext(ctx),
	})
	writeOrgResult(c, out, err)
}

func writeOrgResult(c *gin.Context, out app.OrgResult, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"org": toOrgDTO(out.Org), "event": toEventDTO(out.Event), "active_households": out.ActiveHouseholds,
	})
}

func (h Registry) renameOrg(c *gin.Context)  { status(c, "", h.Reg.RenameOrg) }
func (h Registry) deactivate(c *gin.Context) { status(c, port.OrgCooperative, h.Reg.Deactivate) }
func (h Registry) reactivate(c *gin.Context) { status(c, port.OrgCooperative, h.Reg.Reactivate) }

func (h Registry) orgEvents(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	evs, err := h.Reg.OrgHistory(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]eventDTO, 0, len(evs))
	for _, e := range evs {
		items = append(items, toEventDTO(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type importRowDTO struct {
	Row          int     `json:"row"`
	Status       string  `json:"status"`
	Reason       *string `json:"reason"`
	HouseholdRef *string `json:"household_ref"`
}

func (h Registry) importHouseholds(c *gin.Context) {
	out, err := h.Reg.ImportHouseholds(c.Request.Context(), c.Request.Body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]importRowDTO, 0, len(out.Rows))
	for _, r := range out.Rows {
		rows = append(rows, importRowDTO{Row: r.Row, Status: r.Status, Reason: strPtr(r.Reason), HouseholdRef: refPtr(r.HouseholdRef)})
	}
	c.JSON(http.StatusOK, gin.H{"accepted": out.Accepted, "duplicate": out.Duplicate, "rejected": out.Rejected, "rows": rows})
}

func (h Registry) exportOrgTree(c *gin.Context) {
	data, err := h.Reg.ExportOrgTree(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="cay-to-chuc.xlsx"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, data)
}

type orgImportRowDTO struct {
	Row     int      `json:"row"`
	Status  string   `json:"status"`
	Code    *string  `json:"code"`
	OrgRef  *string  `json:"org_ref"`
	Changes []string `json:"changes"`
	Reason  *string  `json:"reason"`
}

func (h Registry) importOrgTree(c *gin.Context) {
	ctx := c.Request.Context()
	out, err := h.Reg.ImportOrgTree(ctx, app.OrgImportInput{Body: c.Request.Body, Reason: c.Query("reason"), IdemKey: idempotency.KeyFromContext(ctx)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]orgImportRowDTO, 0, len(out.Rows))
	for _, r := range out.Rows {
		changes := r.Changes
		if changes == nil {
			changes = []string{}
		}
		rows = append(rows, orgImportRowDTO{Row: r.Row, Status: r.Status, Code: strPtr(r.Code), OrgRef: refPtr(r.OrgRef), Changes: changes, Reason: strPtr(r.Reason)})
	}
	c.JSON(http.StatusOK, gin.H{"created": out.Created, "updated": out.Updated, "unchanged": out.Unchanged, "rejected": out.Rejected, "rows": rows})
}
