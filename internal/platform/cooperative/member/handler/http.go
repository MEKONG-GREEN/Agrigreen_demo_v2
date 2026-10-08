// Package handler: HTTP của nông hộ và xã viên HTX (pf/cooperative, W3.3 — chuyển từ pf/party; đường dẫn, JSON
// và mã lỗi giữ nguyên). Application API:
//
//	GET  /v1/cooperatives/{ref}/households, POST /v1/households, POST /v1/households/copy,
//	GET  /v1/households/{ref}/contact, POST /v1/households/{ref}/membership,
//	GET|POST /v1/kn/cooperatives/{ref}/households, PUT /v1/kn/households/{ref}, GET /v1/kn/households/{ref}/log.
//
// Admin API: POST /v1/households/import. Các route HTX hướng Nút (/v1/cooperatives, /v1/cooperatives/{ref},
// /v1/kn/cooperatives*, …) ở lại pf/party.
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói app.Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API (nhóm /v1 đã xác thực, có middleware Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/cooperatives/:ref/households", h.listHouseholds)
	rg.POST("/households", h.createHousehold)
	rg.POST("/households/copy", h.copyHouseholds)
	rg.GET("/households/:ref/contact", h.householdContact)
	rg.POST("/households/:ref/membership", h.changeMembership)
	h.registerKN(rg)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc ở router).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	httpx.AllowBody(rg, http.MethodPost, "/households/import", app.MaxImportBytes)
	rg.POST("/households/import", h.importHouseholds)
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

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

func limitOf(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

// pathRef đọc {ref} của HTX; không phải UUID → 404 party.org_not_found.
func pathRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrOrgNotFound)
		return ref, false
	}
	return ref, true
}

// householdRef đọc {ref} của nông hộ; không phải UUID → 404 party.household_not_found.
func householdRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrHouseholdNotFound)
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
// Nông hộ (Application API)
// ---------------------------------------------------------------------------

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

// consentStateDTO là trạng thái đồng ý hiện tại theo mục đích (G-04).
type consentStateDTO struct {
	Purpose   string `json:"purpose"`
	Status    string `json:"status"`
	EventTime string `json:"event_time"`
}

type householdListItem struct {
	householdDTO
	Label            string `json:"label" pii:"true"`
	MaskedPhone      string `json:"masked_phone" pii:"true"`
	LabelUnavailable bool   `json:"label_unavailable"`
	// Consents: trạng thái đồng ý hiện tại theo mục đích (G-04, R-41/R-42).
	Consents []consentStateDTO `json:"consents"`
}

func (h Handler) consentsOf(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID][]consentStateDTO, error) {
	st, err := h.Svc.ConsentStates(ctx, refs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]consentStateDTO, len(refs))
	for _, r := range refs {
		list := make([]consentStateDTO, 0, len(st[r]))
		for _, c := range st[r] {
			list = append(list, consentStateDTO{Purpose: c.Purpose, Status: c.Status, EventTime: c.EventTime.UTC().Format(time.RFC3339Nano)})
		}
		out[r] = list
	}
	return out, nil
}

func (h Handler) listHouseholds(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	page, err := h.Svc.ListHouseholds(c.Request.Context(), ref, after, queryLimit(c), c.Query("phone"))
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

func (h Handler) createHousehold(c *gin.Context) {
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
	out, err := h.Svc.CreateHousehold(c.Request.Context(), in)
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

type membershipReq struct {
	CooperativeRef *string `json:"cooperative_ref"` // null = rời HTX
	EffectiveDate  *string `json:"effective_date"`
	Reason         string  `json:"reason"`
}

// changeMembership: nông hộ vào / chuyển / rời HTX (KN US-HTX-02, US-HTX-05).
func (h Handler) changeMembership(c *gin.Context) {
	ref, ok := householdRef(c)
	if !ok {
		return
	}
	var req membershipReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	in := app.ChangeMembershipInput{HouseholdRef: ref, Reason: req.Reason}
	in.CooperativeRef = fe.optRef("cooperative_ref", req.CooperativeRef)
	in.EffectiveDate = fe.date("effective_date", req.EffectiveDate)
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.ChangeMembership(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"household": toHouseholdDTO(out.Household), "change": out.Change, "prev_cooperative_ref": refPtr(out.Prev)})
}

type copyHouseholdItemReq struct {
	SourceRef  string `json:"source_ref"`
	FullName   string `json:"full_name" pii:"true"`
	Phone      string `json:"phone" pii:"true"`
	NationalID string `json:"national_id" pii:"true"`
	Address    string `json:"address" pii:"true"`
}

type copyHouseholdsReq struct {
	TargetCooperativeRef *string                `json:"target_cooperative_ref"`
	Items                []copyHouseholdItemReq `json:"items"`
}

type copyRowDTO struct {
	Index        int     `json:"index"`
	SourceRef    string  `json:"source_ref"`
	Status       string  `json:"status"`
	Reason       *string `json:"reason"`
	HouseholdRef *string `json:"household_ref"`
}

// copyHouseholds: sao chép hàng loạt nông hộ, sửa riêng họ tên/SĐT từng hồ sơ (HTX US-HH-02).
func (h Handler) copyHouseholds(c *gin.Context) {
	var req copyHouseholdsReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	in := app.CopyHouseholdsInput{TargetCooperativeRef: fe.optRef("target_cooperative_ref", req.TargetCooperativeRef)}
	for i, it := range req.Items {
		src, err := ids.ParseRef(it.SourceRef)
		if err != nil {
			fe.add("items["+strconv.Itoa(i)+"].source_ref", "phải là UUID")
			continue
		}
		in.Items = append(in.Items, app.CopyHouseholdItem{SourceRef: src, Identity: app.FarmerIdentity{
			FullName: it.FullName, Phone: it.Phone, NationalID: it.NationalID, Address: it.Address,
		}})
	}
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.CopyHouseholds(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]copyRowDTO, 0, len(out.Rows))
	for _, r := range out.Rows {
		rows = append(rows, copyRowDTO{Index: r.Index, SourceRef: r.SourceRef.String(), Status: r.Status, Reason: strPtr(r.Reason), HouseholdRef: refPtr(r.HouseholdRef)})
	}
	c.JSON(http.StatusOK, gin.H{"created": out.Created, "rejected": out.Rejected, "rows": rows})
}

// householdContact: SĐT chủ hộ lúc bấm gọi nhanh (HTX US-HH-03). Không lưu đệm.
func (h Handler) householdContact(c *gin.Context) {
	ref, ok := householdRef(c)
	if !ok {
		return
	}
	phone, err := h.Svc.HouseholdPhone(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"household_ref": ref.String(), "phone": phone})
}

// ---------------------------------------------------------------------------
// Admin API
// ---------------------------------------------------------------------------

type importRowDTO struct {
	Row          int     `json:"row"`
	Status       string  `json:"status"`
	Reason       *string `json:"reason"`
	HouseholdRef *string `json:"household_ref"`
}

func (h Handler) importHouseholds(c *gin.Context) {
	out, err := h.Svc.ImportHouseholds(c.Request.Context(), c.Request.Body)
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
