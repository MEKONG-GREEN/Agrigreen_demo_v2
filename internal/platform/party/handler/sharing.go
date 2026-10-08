package handler

// HTTP của cấp quyền chia sẻ (ADR-0020, v2 ADR-0030/W4.1): Admin API /v1/share-grants, rà soát định kỳ
// /v1/share-grant-reviews/due + /v1/share-grants/{id}/reviews (platform_admin bên cấp, MFA)
// và bề mặt đối tác /v1/partner/grants, /v1/partner/cooperatives (tài khoản dịch vụ bên nhận).

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Sharing gói app.Sharing cho HTTP.
type Sharing struct{ Svc *app.Sharing }

// RegisterAdmin gắn route Admin API.
func (h Sharing) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/share-grants", h.propose)
	rg.GET("/share-grants", h.list)
	rg.GET("/share-grants/:id", h.get)
	rg.POST("/share-grants/:id/approve", h.approve)
	rg.POST("/share-grants/:id/revoke", h.revoke)
	rg.GET("/share-grants/:id/reads", h.reads)
	// Rà soát định kỳ (W4.1, R-SEC-04).
	rg.GET("/share-grant-reviews/due", h.due)
	rg.GET("/share-grants/:id/reviews", h.reviews)
	rg.POST("/share-grants/:id/reviews", h.review)
}

// RegisterApp gắn bề mặt đối tác của pf/party.
func (h Sharing) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/partner/grants", h.partnerGrants)
	rg.GET("/partner/cooperatives", h.partnerCooperatives)
	rg.GET("/partner/cooperatives/:ref", h.partnerCooperative)
}

type grantDTO struct {
	ID           string   `json:"id"`
	Grantor      string   `json:"grantor_tenant_id"`
	Grantee      string   `json:"grantee_tenant_id"`
	Kinds        []string `json:"kinds"`
	Purpose      string   `json:"purpose"`
	ValidFrom    string   `json:"valid_from"`
	ValidTo      string   `json:"valid_to"`
	Status       string   `json:"status"`
	ProposedBy   string   `json:"proposed_by"`
	ProposedAt   string   `json:"proposed_at"`
	ApprovedBy   *string  `json:"approved_by"`
	ApprovedAt   *string  `json:"approved_at"`
	RevokedBy    *string  `json:"revoked_by"`
	RevokedAt    *string  `json:"revoked_at"`
	RevokeReason *string  `json:"revoke_reason"`
	// Điều khoản v2 (W4.1). grant_class luôn có (cấp phép trước v2 → partner, legacy_terms = true).
	GrantClass     string  `json:"grant_class"`
	LegacyTerms    bool    `json:"legacy_terms"`
	ModuleTable    *string `json:"module_table"`
	ScopeNodeRef   *string `json:"scope_node_ref"`
	PurposeCode    *string `json:"purpose_code"`
	BasisRef       *string `json:"basis_ref"`
	LastReviewedAt *string `json:"last_reviewed_at"`
	LastReviewedBy *string `json:"last_reviewed_by"`
	ReviewDueAt    *string `json:"review_due_at"`
}

func refStr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func timeStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func toGrantDTO(g store.Grant) grantDTO {
	d := grantDTO{ID: g.GrantID.String(), Grantor: g.TenantID.String(), Grantee: g.Grantee.String(), Kinds: g.Kinds, Purpose: g.Purpose,
		ValidFrom: g.ValidFrom.UTC().Format(time.RFC3339), ValidTo: g.ValidTo.UTC().Format(time.RFC3339), Status: g.Status,
		ProposedBy: g.ProposedBy.String(), ProposedAt: g.ProposedAt.UTC().Format(time.RFC3339Nano), ApprovedBy: refStr(g.ApprovedBy),
		ApprovedAt: timeStr(g.ApprovedAt), RevokedBy: refStr(g.RevokedBy), RevokedAt: timeStr(g.RevokedAt), RevokeReason: g.RevokeReason,
		GrantClass: g.Class(), LegacyTerms: g.Terms == nil, LastReviewedAt: timeStr(g.LastReviewedAt), LastReviewedBy: refStr(g.LastReviewedBy),
		ReviewDueAt: timeStr(app.ReviewDue(g))}
	if d.Kinds == nil {
		d.Kinds = []string{}
	}
	if t := g.Terms; t != nil {
		d.ModuleTable, d.ScopeNodeRef, d.PurposeCode, d.BasisRef = t.ModuleTable, refStr(t.ScopeNodeRef), t.PurposeCode, t.BasisRef
	}
	return d
}

func shareID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrShareGrantNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func written(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

type proposeReq struct {
	Grantee      string   `json:"grantee_tenant_id"`
	Kinds        []string `json:"kinds"`
	Purpose      string   `json:"purpose"`
	ValidFrom    string   `json:"valid_from"`
	ValidTo      string   `json:"valid_to"`
	GrantClass   string   `json:"grant_class"`
	ModuleTable  string   `json:"module_table"`
	ScopeNodeRef string   `json:"scope_node_ref"`
	PurposeCode  string   `json:"purpose_code"`
	BasisRef     string   `json:"basis_ref"`
}

func (h Sharing) propose(c *gin.Context) {
	var req proposeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	in := app.ProposeShareInput{Kinds: req.Kinds, Purpose: req.Purpose, IdemKey: idempotency.KeyFromContext(c.Request.Context()),
		GrantClass: req.GrantClass, ModuleTable: req.ModuleTable, PurposeCode: req.PurposeCode, BasisRef: req.BasisRef}
	if g, err := uuid.Parse(req.Grantee); err == nil {
		in.Grantee = g
	}
	if req.ScopeNodeRef != "" {
		n, err := uuid.Parse(req.ScopeNodeRef)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("scope_node_ref", "phải là UUID"))
			return
		}
		in.ScopeNodeRef = n
	}
	if req.ValidFrom != "" {
		t, err := time.Parse(time.RFC3339Nano, req.ValidFrom)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("valid_from", "phải theo RFC 3339 có múi giờ"))
			return
		}
		in.ValidFrom = t
	}
	if t, err := time.Parse(time.RFC3339Nano, req.ValidTo); err == nil {
		in.ValidTo = t
	}
	g, replayed, err := h.Svc.Propose(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/share-grants/"+g.GrantID.String())
	written(c, replayed, toGrantDTO(g))
}

func (h Sharing) approve(c *gin.Context) {
	id, ok := shareID(c)
	if !ok {
		return
	}
	g, replayed, err := h.Svc.Approve(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	written(c, replayed, toGrantDTO(g))
}

func (h Sharing) revoke(c *gin.Context) {
	id, ok := shareID(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	g, replayed, err := h.Svc.Revoke(c.Request.Context(), id, req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	written(c, replayed, toGrantDTO(g))
}

func limitQ(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func (h Sharing) list(c *gin.Context) {
	var after uuid.UUID
	if s := c.Query("cursor"); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = u
	}
	gs, err := h.Svc.ListShareGrants(c.Request.Context(), c.Query("status"), after, limitQ(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]grantDTO, 0, len(gs))
	for _, g := range gs {
		items = append(items, toGrantDTO(g))
	}
	var next *string
	l := limitQ(c)
	if l <= 0 || l > 200 {
		l = 50
	}
	if n := len(gs); n > 0 && n == l {
		s := gs[n-1].GrantID.String()
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Sharing) get(c *gin.Context) {
	id, ok := shareID(c)
	if !ok {
		return
	}
	g, err := h.Svc.GetShareGrant(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toGrantDTO(g))
}

type readDTO struct {
	ID         string  `json:"id"`
	Seq        int64   `json:"seq"`
	Grantee    string  `json:"grantee_tenant_id"`
	Kind       string  `json:"kind"`
	Endpoint   string  `json:"endpoint"`
	Query      *string `json:"query"`
	Items      int     `json:"item_count"`
	ActorRef   string  `json:"actor_ref"`
	RecordedAt string  `json:"recorded_at"`
}

func (h Sharing) reads(c *gin.Context) {
	id, ok := shareID(c)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(c.Query("after"), 10, 64)
	rs, err := h.Svc.ShareGrantReads(c.Request.Context(), id, after, limitQ(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]readDTO, 0, len(rs))
	next := after
	for _, r := range rs {
		items = append(items, readDTO{ID: r.ID.String(), Seq: r.Seq, Grantee: r.Grantee.String(), Kind: r.Kind, Endpoint: r.Endpoint, Query: r.Query,
			Items: r.Items, ActorRef: r.ActorRef.String(), RecordedAt: r.RecordedAt.UTC().Format(time.RFC3339Nano)})
		next = r.Seq
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

type reviewDTO struct {
	ID         string  `json:"id"`
	GrantID    string  `json:"grant_id"`
	Seq        int64   `json:"seq"`
	Decision   string  `json:"decision"`
	Note       *string `json:"note"`
	ActorRef   string  `json:"actor_ref"`
	EventTime  string  `json:"event_time"`
	RecordedAt string  `json:"recorded_at"`
}

func toReviewDTO(r store.Review) reviewDTO {
	return reviewDTO{ID: r.ID.String(), GrantID: r.GrantID.String(), Seq: r.Seq, Decision: r.Decision, Note: r.Note, ActorRef: r.ActorRef.String(),
		EventTime: r.EventTime.UTC().Format(time.RFC3339Nano), RecordedAt: r.RecordedAt.UTC().Format(time.RFC3339Nano)}
}

// due: cấp phép đang hiệu lực đến hạn rà soát định kỳ (R-SEC-04) trong within_days tới.
func (h Sharing) due(c *gin.Context) {
	var after uuid.UUID
	if s := c.Query("cursor"); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = u
	}
	days := 0
	if s := c.Query("within_days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("within_days", "số nguyên 0–366"))
			return
		}
		days = n
	}
	l := limitQ(c)
	if l <= 0 || l > 200 {
		l = 50
	}
	gs, err := h.Svc.ShareGrantsDue(c.Request.Context(), time.Duration(days)*24*time.Hour, after, l)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]grantDTO, 0, len(gs))
	for _, g := range gs {
		items = append(items, toGrantDTO(g))
	}
	var next *string
	if n := len(gs); n > 0 && n == l {
		s := gs[n-1].GrantID.String()
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next, "review_interval_days": int(app.ShareReviewInterval / (24 * time.Hour))})
}

func (h Sharing) reviews(c *gin.Context) {
	id, ok := shareID(c)
	if !ok {
		return
	}
	rs, err := h.Svc.ShareGrantReviews(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]reviewDTO, 0, len(rs))
	for _, r := range rs {
		items = append(items, toReviewDTO(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Sharing) review(c *gin.Context) {
	id, ok := shareID(c)
	if !ok {
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	r, g, replayed, err := h.Svc.ReviewShareGrant(c.Request.Context(), id,
		app.ReviewInput{Decision: req.Decision, Note: req.Note, IdemKey: idempotency.KeyFromContext(c.Request.Context())})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	written(c, replayed, gin.H{"review": toReviewDTO(r), "grant": toGrantDTO(g)})
}

// ---------------------------------------------------------------------------
// Bề mặt đối tác
// ---------------------------------------------------------------------------

// grantorQ đọc tham số grantor (tenant bên cấp) — xem port.ParseGrantor. Sai dạng → đã ghi lỗi.
func grantorQ(c *gin.Context) (uuid.UUID, bool) {
	u, err := port.ParseGrantor(c.Query("grantor"))
	if err != nil {
		httpx.Fail(c, err)
		return uuid.Nil, false
	}
	return u, true
}

type partnerGrantDTO struct {
	ID        string   `json:"id"`
	Grantor   string   `json:"grantor_tenant_id"`
	Kinds     []string `json:"kinds"`
	ValidFrom string   `json:"valid_from"`
	ValidTo   string   `json:"valid_to"`
}

func (h Sharing) partnerGrants(c *gin.Context) {
	gs, err := h.Svc.PartnerGrants(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]partnerGrantDTO, 0, len(gs))
	for _, g := range gs {
		items = append(items, partnerGrantDTO{ID: g.ID.String(), Grantor: g.Grantor.String(), Kinds: g.Kinds,
			ValidFrom: g.ValidFrom.UTC().Format(time.RFC3339), ValidTo: g.ValidTo.UTC().Format(time.RFC3339)})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// partnerCoopDTO là HTX trong danh bạ bên nhận — không người đại diện, không mã số thuế, không nông hộ.
type partnerCoopDTO struct {
	Ref         string  `json:"ref"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	CommuneCode *string `json:"commune_code"`
	Active      bool    `json:"active"`
	Lon         *string `json:"lon"`
	Lat         *string `json:"lat"`
	UpdatedAt   string  `json:"updated_at"`
}

func toPartnerCoop(e store.CooperativeEntry) partnerCoopDTO {
	return partnerCoopDTO{Ref: e.Ref.String(), Code: e.Code, Name: e.Name, CommuneCode: e.CommuneCode, Active: e.Active, Lon: e.Lon, Lat: e.Lat,
		UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func (h Sharing) partnerCooperatives(c *gin.Context) {
	grantor, ok := grantorQ(c)
	if !ok {
		return
	}
	var after uuid.UUID
	if s := c.Query("cursor"); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = u
	}
	pg, err := h.Svc.PartnerCooperatives(c.Request.Context(), grantor, after, limitQ(c), c.Request.URL.RawQuery)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]partnerCoopDTO, 0, len(pg.Items))
	for _, e := range pg.Items {
		items = append(items, toPartnerCoop(e))
	}
	var next *string
	if pg.Next != nil {
		s := pg.Next.String()
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Sharing) partnerCooperative(c *gin.Context) {
	grantor, ok := grantorQ(c)
	if !ok {
		return
	}
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrPartnerNotFound)
		return
	}
	e, err := h.Svc.PartnerCooperative(c.Request.Context(), grantor, ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toPartnerCoop(e))
}
