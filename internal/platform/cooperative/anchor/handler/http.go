// Package handler: HTTP của neo HTX (D2 — ADR-0052; pf/cooperative). Admin API (MFA):
//
//	GET  /v1/htx-anchors                       danh sách (?status=provisional|claimed&q=<MST|mã tạm>&after=&limit=)
//	POST /v1/htx-anchors                       lập HTX tạm {org_ref, created_by: kn|mkg, mst?, agreement_ref?}
//	POST /v1/htx-anchors/backfill              neo HTX có trước D2 {dry_run, limit?}
//	GET  /v1/htx-anchors/{anchor_id}           chi tiết + sổ (vết kiểm toán)
//	POST /v1/htx-anchors/{anchor_id}/mst       gắn MST {mst}
//	POST /v1/htx-anchors/{anchor_id}/claim     nhận quyền {account_ref, verified_mst, verification_ref, target_org_ref?, dry_run}
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói app.Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA, có middleware Idempotency-Key).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/htx-anchors", h.list)
	rg.POST("/htx-anchors", h.create)
	rg.POST("/htx-anchors/backfill", h.backfill)
	rg.GET("/htx-anchors/:anchor_id", h.get)
	rg.POST("/htx-anchors/:anchor_id/mst", h.attachMST)
	rg.POST("/htx-anchors/:anchor_id/claim", h.claim)
}

func ts(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func refs(us []uuid.UUID) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.String()
	}
	return out
}

func anchorDTO(a app.Anchor) gin.H {
	var account *string
	if a.HTXAccountRef != nil {
		s := a.HTXAccountRef.String()
		account = &s
	}
	return gin.H{
		"anchor_id": a.AnchorID.String(), "anchor_key": a.AnchorKey, "mst": a.MST, "temporary": a.MST == nil,
		"status": a.Status, "created_by": a.CreatedByKind, "provisional_org_ref": a.ProvisionalOrgRef.String(),
		"org_ref": a.OrgRef.String(), "aliases": refs(a.Aliases), "htx_account_ref": account, "agreement_ref": a.AgreementRef,
		"co_owners":           []string{"mkg", "htx"},
		"confirmation_status": a.Confirmation, "confirmation_label": domain.ConfirmationLabel(a.Confirmation),
		"claimed_at": ts(a.ClaimedAt), "created_at": a.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func eventDTO(e store.Event) gin.H {
	var details any
	if len(e.Details) > 0 {
		details = json.RawMessage(e.Details)
	}
	return gin.H{"id": e.ID.String(), "seq": e.Seq, "action": e.Action, "anchor_key": e.AnchorKey, "mst": e.MST,
		"org_ref": e.OrgRef.String(), "verification_ref": e.VerificationRef, "details": details, "actor_ref": e.ActorRef.String(),
		"event_time": e.EventTime.UTC().Format(time.RFC3339Nano), "recorded_at": e.RecordedAt.UTC().Format(time.RFC3339Nano)}
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("anchor_id"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("anchor_id", "phải là UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) list(c *gin.Context) {
	f := store.Filter{Status: c.Query("status"), Q: c.Query("q")}
	if s := c.Query("after"); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("after", "phải là UUID"))
			return
		}
		f.After = u
	}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("limit", "phải là số"))
			return
		}
		f.Limit = n
	}
	as, next, err := h.Svc.List(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(as))
	for _, a := range as {
		items = append(items, anchorDTO(a))
	}
	var nc *string
	if next != nil {
		s := next.String()
		nc = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nc})
}

func (h Handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	a, evs, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := anchorDTO(a)
	items := make([]gin.H, 0, len(evs))
	for _, e := range evs {
		items = append(items, eventDTO(e))
	}
	out["events"] = items
	c.JSON(http.StatusOK, out)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ"))
		return false
	}
	return true
}

func (h Handler) create(c *gin.Context) {
	var req struct {
		OrgRef         string `json:"org_ref"`
		NewCooperative *struct {
			Name              string `json:"name"`
			CommuneCode       string `json:"admin_unit_code"`
			ParentRef         string `json:"parent_ref"`
			RepresentativeRef string `json:"representative_ref"`
			Representative    *struct {
				FullName string `json:"full_name" pii:"true"`
				Phone    string `json:"phone" pii:"true"`
			} `json:"representative"`
			Profile *struct {
				AreaHa        *decimal.Decimal `json:"area_ha"`
				EstablishedOn string           `json:"established_on"`
				Address       string           `json:"address"`
				Location      *struct {
					Lat decimal.Decimal `json:"lat"`
					Lon decimal.Decimal `json:"lon"`
				} `json:"location"`
			} `json:"profile"`
		} `json:"new_cooperative"`
		CreatedBy    string `json:"created_by"`
		MST          string `json:"mst"`
		AgreementRef string `json:"agreement_ref"`
	}
	if !bind(c, &req) {
		return
	}
	org := uuid.Nil
	if req.OrgRef != "" || req.NewCooperative == nil {
		var err error
		org, err = uuid.Parse(req.OrgRef)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("org_ref", "phải là UUID nếu không tạo HTX mới"))
			return
		}
	}
	var registration *partyport.CooperativeRegistration
	if req.NewCooperative != nil {
		registration = &partyport.CooperativeRegistration{Name: req.NewCooperative.Name, CommuneCode: req.NewCooperative.CommuneCode}
		n := req.NewCooperative
		if n.RepresentativeRef != "" {
			ref, err := uuid.Parse(n.RepresentativeRef)
			if err != nil {
				httpx.Fail(c, problem.Validation.WithField("new_cooperative.representative_ref", "phải là UUID"))
				return
			}
			registration.RepresentativeRef = ref
		}
		if n.Representative != nil {
			registration.Representative = &partyport.RegistrationRepresentative{FullName: n.Representative.FullName, Phone: n.Representative.Phone}
		}
		if n.Profile != nil {
			registration.AreaHa, registration.Address = n.Profile.AreaHa, n.Profile.Address
			if n.Profile.EstablishedOn != "" {
				day, err := time.Parse(time.DateOnly, n.Profile.EstablishedOn)
				if err != nil {
					httpx.Fail(c, problem.Validation.WithField("new_cooperative.profile.established_on", "phải là ngày YYYY-MM-DD"))
					return
				}
				registration.EstablishedOn = &day
			}
			if n.Profile.Location != nil {
				registration.Lat, registration.Lon = &n.Profile.Location.Lat, &n.Profile.Location.Lon
			}
		}
		if req.NewCooperative.ParentRef != "" {
			var err error
			registration.ParentRef, err = uuid.Parse(req.NewCooperative.ParentRef)
			if err != nil {
				httpx.Fail(c, problem.Validation.WithField("new_cooperative.parent_ref", "phải là UUID"))
				return
			}
		}
	}
	a, replayed, err := h.Svc.Create(c.Request.Context(), app.CreateInput{OrgRef: org, NewCooperative: registration, CreatedBy: req.CreatedBy, MST: req.MST,
		AgreementRef: req.AgreementRef, IdemKey: idempotency.KeyFromContext(c.Request.Context())})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	st := http.StatusCreated
	if replayed {
		st = http.StatusOK
	}
	c.JSON(st, anchorDTO(a))
}

func (h Handler) attachMST(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		MST string `json:"mst"`
	}
	if !bind(c, &req) {
		return
	}
	a, err := h.Svc.AttachMST(c.Request.Context(), id, req.MST, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, anchorDTO(a))
}

func (h Handler) claim(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		TargetOrgRef    string `json:"target_org_ref"`
		AccountRef      string `json:"account_ref"`
		VerifiedMST     string `json:"verified_mst"`
		VerificationRef string `json:"verification_ref"`
		DryRun          bool   `json:"dry_run"`
	}
	if !bind(c, &req) {
		return
	}
	in := app.ClaimInput{VerifiedMST: req.VerifiedMST, VerificationRef: req.VerificationRef, DryRun: req.DryRun,
		IdemKey: idempotency.KeyFromContext(c.Request.Context())}
	var err error
	if in.AccountRef, err = uuid.Parse(req.AccountRef); err != nil {
		httpx.Fail(c, problem.Validation.WithField("account_ref", "phải là UUID"))
		return
	}
	if req.TargetOrgRef != "" {
		if in.TargetOrgRef, err = uuid.Parse(req.TargetOrgRef); err != nil {
			httpx.Fail(c, problem.Validation.WithField("target_org_ref", "phải là UUID"))
			return
		}
	}
	rep, err := h.Svc.Claim(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, rep)
}

func (h Handler) backfill(c *gin.Context) {
	var req struct {
		DryRun bool `json:"dry_run"`
		Limit  int  `json:"limit"`
	}
	if !bind(c, &req) {
		return
	}
	res, err := h.Svc.Backfill(c.Request.Context(), req.DryRun, req.Limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
