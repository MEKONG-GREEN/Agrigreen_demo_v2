package handler

// W11.07 — HTTP phân quyền trong vai:
//
//	Admin API (MFA): PUT /v1/accounts/users/{id}/assignments/{aid}/attributes; GET /v1/accounts/users/{id}/delegations;
//	                 POST /v1/accounts/delegations/{id}/revoke
//	Application API: GET|POST /v1/me/delegations; POST /v1/me/delegations/{id}/revoke

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerAttrsAdmin(rg *gin.RouterGroup) {
	rg.PUT("/accounts/users/:id/assignments/:aid/attributes", h.setAttrs)
	rg.GET("/accounts/users/:id/delegations", h.userDelegations)
	rg.POST("/accounts/delegations/:id/revoke", h.revokeDelegation(true))
}

func (h Handler) registerDelegationsApp(rg *gin.RouterGroup) {
	rg.GET("/me/delegations", h.myDelegations)
	rg.POST("/me/delegations", h.createDelegation)
	rg.POST("/me/delegations/:id/revoke", h.revokeDelegation(false))
}

func nonNilStr(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func refsDTO(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func catalogDTO(items []port.CatalogItem) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{"code": it.Code, "label": it.Label})
	}
	return out
}

type attrsDTO struct {
	ProvinceGroup      *string  `json:"province_group"`
	ProvinceGroupLabel *string  `json:"province_group_label"`
	Duties             []string `json:"duties"`
	InternalUnitCode   *string  `json:"internal_unit_code"`
	FacilityRefs       []string `json:"facility_refs"`
	HouseholdRefs      []string `json:"household_refs"`
}

func toAttrsDTO(a port.AssignmentAttrs) attrsDTO {
	return attrsDTO{ProvinceGroup: strPtr(a.ProvinceGroup), ProvinceGroupLabel: strPtr(port.CatalogLabel(port.ProvinceGroups, a.ProvinceGroup)),
		Duties: nonNilStr(a.Duties), InternalUnitCode: strPtr(a.InternalUnit), FacilityRefs: refsDTO(a.Facilities),
		HouseholdRefs: refsDTO(a.Households)}
}

type attrsReq struct {
	ProvinceGroup    string   `json:"province_group"`
	Duties           []string `json:"duties"`
	InternalUnitCode string   `json:"internal_unit_code"`
	FacilityRefs     []string `json:"facility_refs"`
	HouseholdRefs    []string `json:"household_refs"`
	Reason           string   `json:"reason"`
}

func parseRefs(in []string, field string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(in))
	for _, s := range in {
		r, err := uuid.Parse(s)
		if err != nil {
			return nil, problem.Validation.WithField(field, "UUID")
		}
		out = append(out, r)
	}
	return out, nil
}

func (h Handler) setAttrs(c *gin.Context) {
	user, ok := pathID(c)
	if !ok {
		return
	}
	aid, ok := pathRef(c, "aid", app.ErrAssignmentNotFound)
	if !ok {
		return
	}
	var req attrsReq
	if !bindJSON(c, &req) {
		return
	}
	in := domain.AttrsInput{ProvinceGroup: req.ProvinceGroup, Duties: req.Duties, InternalUnit: req.InternalUnitCode}
	var err error
	if in.Facilities, err = parseRefs(req.FacilityRefs, "facility_refs"); err != nil {
		httpx.Fail(c, err)
		return
	}
	if in.Households, err = parseRefs(req.HouseholdRefs, "household_refs"); err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.SetAssignmentAttrs(c.Request.Context(), user, aid, in, req.Reason, headerKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"assignment_id": aid.String(), "attributes": toAttrsDTO(res.Attrs), "seq": res.Attrs.Seq})
}

type delegationDTO struct {
	DelegationID   string `json:"delegation_id"`
	GrantorRef     string `json:"grantor_ref"`
	GranteeRef     string `json:"grantee_ref"`
	DepartmentCode string `json:"department_code"`
	Power          string `json:"power"`
	PowerLabel     string `json:"power_label"`
	ValidFrom      string `json:"valid_from"`
	ValidTo        string `json:"valid_to"`
	Revoked        bool   `json:"revoked"`
	Status         string `json:"status"` // active | scheduled | expired | revoked
	Reason         string `json:"reason"`
	CreatedAt      string `json:"created_at"`
}

func toDelegationDTO(d port.Delegation, now time.Time) delegationDTO {
	status := "active"
	switch {
	case d.Revoked:
		status = "revoked"
	case !now.Before(d.ValidTo):
		status = "expired"
	case now.Before(d.ValidFrom):
		status = "scheduled"
	}
	return delegationDTO{DelegationID: d.ID.String(), GrantorRef: d.GrantorRef.String(), GranteeRef: d.GranteeRef.String(),
		DepartmentCode: d.Department, Power: d.Power, PowerLabel: port.CatalogLabel(port.Powers, d.Power), ValidFrom: rfc(d.ValidFrom),
		ValidTo: rfc(d.ValidTo), Revoked: d.Revoked, Status: status, Reason: d.Reason, CreatedAt: rfc(d.CreatedAt)}
}

func (h Handler) delegationList(c *gin.Context, ds []port.Delegation, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	now := h.Svc.Clock.Now()
	items := make([]delegationDTO, 0, len(ds))
	for _, d := range ds {
		items = append(items, toDelegationDTO(d, now))
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) myDelegations(c *gin.Context) {
	ds, err := h.Svc.MyDelegations(c.Request.Context())
	h.delegationList(c, ds, err)
}

func (h Handler) userDelegations(c *gin.Context) {
	user, ok := pathID(c)
	if !ok {
		return
	}
	ds, err := h.Svc.UserDelegations(c.Request.Context(), user)
	h.delegationList(c, ds, err)
}

type delegationReq struct {
	GranteeRef     string  `json:"grantee_ref"`
	DepartmentCode string  `json:"department_code"`
	Power          string  `json:"power"`
	ValidFrom      *string `json:"valid_from"`
	ValidTo        string  `json:"valid_to"`
	Reason         string  `json:"reason"`
}

func (h Handler) createDelegation(c *gin.Context) {
	var req delegationReq
	if !bindJSON(c, &req) {
		return
	}
	in := domain.DelegationInput{Department: req.DepartmentCode, Power: req.Power, Reason: req.Reason}
	var err error
	if in.GranteeRef, err = uuid.Parse(req.GranteeRef); err != nil {
		httpx.Fail(c, problem.Validation.WithField("grantee_ref", "UUID"))
		return
	}
	if in.ValidTo, err = time.Parse(time.RFC3339, req.ValidTo); err != nil {
		httpx.Fail(c, problem.Validation.WithField("valid_to", "RFC 3339"))
		return
	}
	if req.ValidFrom != nil && *req.ValidFrom != "" {
		if in.ValidFrom, err = time.Parse(time.RFC3339, *req.ValidFrom); err != nil {
			httpx.Fail(c, problem.Validation.WithField("valid_from", "RFC 3339"))
			return
		}
	}
	res, err := h.Svc.CreateDelegation(c.Request.Context(), in, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, toDelegationDTO(res.Delegation, h.Svc.Clock.Now()))
}

type revokeDelegationReq struct {
	Reason string `json:"reason"`
}

func (h Handler) revokeDelegation(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathRef(c, "id", app.ErrDelegationNotFound)
		if !ok {
			return
		}
		var req revokeDelegationReq
		if !bindJSON(c, &req) {
			return
		}
		res, err := h.Svc.RevokeDelegation(c.Request.Context(), id, req.Reason, key(c), admin)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, toDelegationDTO(res.Delegation, h.Svc.Clock.Now()))
	}
}
