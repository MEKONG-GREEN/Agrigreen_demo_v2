package handler

// W11.07 (Q-068): HTTP danh mục đơn vị nội bộ.
//
//	Admin API (MFA): GET|POST /v1/accounts/internal-units, PUT /v1/accounts/internal-units/{id},
//	                 POST /v1/accounts/internal-units/{id}/deactivate|reactivate, GET /v1/accounts/internal-units/{id}/log,
//	                 POST /v1/accounts/internal-units/seed
//	Application API: GET /v1/internal-units

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerInternalUnitsAdmin(rg *gin.RouterGroup) {
	rg.GET("/accounts/internal-units", h.listInternalUnits)
	rg.POST("/accounts/internal-units", h.createInternalUnit)
	rg.POST("/accounts/internal-units/seed", h.seedInternalUnits)
	rg.PUT("/accounts/internal-units/:id", h.updateInternalUnit)
	rg.POST("/accounts/internal-units/:id/deactivate", h.setInternalUnitActive(false))
	rg.POST("/accounts/internal-units/:id/reactivate", h.setInternalUnitActive(true))
	rg.GET("/accounts/internal-units/:id/log", h.internalUnitLog)
}

func (h Handler) registerInternalUnitsApp(rg *gin.RouterGroup) {
	rg.GET("/internal-units", h.listInternalUnits)
}

type internalUnitDTO struct {
	ID             string  `json:"id"`
	OrgRef         string  `json:"org_ref"`
	Code           string  `json:"code"`
	Kind           string  `json:"kind"`
	KindLabel      string  `json:"kind_label"`
	Name           string  `json:"name"`
	DepartmentCode *string `json:"department_code"`
	SortOrder      int     `json:"sort_order"`
	Active         bool    `json:"active"`
	UpdatedAt      string  `json:"updated_at"`
}

func toInternalUnitDTO(u port.InternalUnit) internalUnitDTO {
	return internalUnitDTO{ID: u.ID.String(), OrgRef: u.OrgRef.String(), Code: u.Code, Kind: u.Kind,
		KindLabel: port.CatalogLabel(port.InternalUnitKinds, u.Kind), Name: u.Name, DepartmentCode: strPtr(u.DepartmentCode),
		SortOrder: u.SortOrder, Active: u.Active, UpdatedAt: rfc(u.UpdatedAt)}
}

func optQueryRef(c *gin.Context, name string) (uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return uuid.Nil, true
	}
	r, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "UUID"))
		return uuid.Nil, false
	}
	return r, true
}

func (h Handler) listInternalUnits(c *gin.Context) {
	unit, ok := optQueryRef(c, "org_ref")
	if !ok {
		return
	}
	us, resolved, err := h.Svc.ListInternalUnits(c.Request.Context(), unit, c.Query("include_inactive") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]internalUnitDTO, 0, len(us))
	for _, u := range us {
		items = append(items, toInternalUnitDTO(u))
	}
	c.JSON(http.StatusOK, gin.H{"org_ref": resolved.String(), "items": items, "kinds": catalogDTO(port.InternalUnitKinds)})
}

type internalUnitReq struct {
	OrgRef         string `json:"org_ref"`
	Code           string `json:"code"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	DepartmentCode string `json:"department_code"`
	SortOrder      int    `json:"sort_order"`
}

func (r internalUnitReq) input() app.InternalUnitInput {
	in := app.InternalUnitInput{Code: r.Code, Kind: r.Kind, Name: r.Name, DepartmentCode: r.DepartmentCode, SortOrder: r.SortOrder}
	if ref, err := uuid.Parse(r.OrgRef); err == nil {
		in.OrgRef = ref
	}
	return in
}

func (h Handler) createInternalUnit(c *gin.Context) {
	var req internalUnitReq
	if !bindJSON(c, &req) {
		return
	}
	u, replayed, err := h.Svc.CreateInternalUnit(c.Request.Context(), req.input(), key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/accounts/internal-units/"+u.ID.String())
	c.JSON(status, toInternalUnitDTO(u))
}

func (h Handler) updateInternalUnit(c *gin.Context) {
	id, ok := pathRef(c, "id", app.ErrInternalUnitNotFound)
	if !ok {
		return
	}
	var req internalUnitReq
	if !bindJSON(c, &req) {
		return
	}
	u, err := h.Svc.UpdateInternalUnit(c.Request.Context(), id, req.input())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toInternalUnitDTO(u))
}

func (h Handler) setInternalUnitActive(active bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathRef(c, "id", app.ErrInternalUnitNotFound)
		if !ok {
			return
		}
		var req revokeDelegationReq
		if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
			return
		}
		u, err := h.Svc.SetInternalUnitActive(c.Request.Context(), id, active, req.Reason, key(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, toInternalUnitDTO(u))
	}
}

type unitLogDTO struct {
	Seq      int64          `json:"seq"`
	Event    string         `json:"event"`
	ActorRef string         `json:"actor_ref"`
	At       string         `json:"at"`
	Changes  []logChangeDTO `json:"changes"`
	Reason   *string        `json:"reason"`
}

func (h Handler) internalUnitLog(c *gin.Context) {
	id, ok := pathRef(c, "id", app.ErrInternalUnitNotFound)
	if !ok {
		return
	}
	var after int64
	if s := c.Query("cursor"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = n
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	es, err := h.Svc.InternalUnitLog(c.Request.Context(), id, after, clamp(limit))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]unitLogDTO, 0, len(es))
	for _, e := range es {
		d := unitLogDTO{Seq: e.Seq, Event: e.Event, ActorRef: e.ActorRef.String(), At: rfc(e.At), Changes: []logChangeDTO{}, Reason: e.Reason}
		for _, ch := range e.Changes {
			d.Changes = append(d.Changes, logChangeDTO(ch))
		}
		items = append(items, d)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type unitSeedReq struct {
	OrgRef string `json:"org_ref"`
}

func (h Handler) seedInternalUnits(c *gin.Context) {
	var req unitSeedReq
	if !bindJSON(c, &req) {
		return
	}
	ref, err := uuid.Parse(req.OrgRef)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("org_ref", "UUID"))
		return
	}
	res, err := h.Svc.SeedInternalUnits(c.Request.Context(), ref, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]internalUnitDTO, 0, len(res.Created))
	for _, u := range res.Created {
		items = append(items, toInternalUnitDTO(u))
	}
	c.JSON(http.StatusOK, gin.H{"created": items, "existing": res.Existing, "status": res.Status})
}
