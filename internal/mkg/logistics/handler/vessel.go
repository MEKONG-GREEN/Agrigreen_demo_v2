package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type vesselReq struct {
	Code               string  `json:"code"`
	Kind               string  `json:"kind"`
	Name               string  `json:"name"`
	OwnerRef           *string `json:"owner_ref"`
	RegisteredTons     *string `json:"registered_tons"`
	StrawPayloadTons   *string `json:"straw_payload_tons"`
	RegistrationExpiry *string `json:"registration_expiry"`
	RateType           string  `json:"rate_type"`
	RateVND            *string `json:"rate_vnd"`
	Status             string  `json:"status"`
	Reason             string  `json:"reason"`
	ExpectedVersion    int     `json:"expected_version"`
}

type vesselDTO struct {
	VesselID           string  `json:"vessel_id"`
	Version            int     `json:"version"`
	Code               string  `json:"code"`
	Kind               string  `json:"kind"`
	Name               *string `json:"name"`
	OwnerRef           *string `json:"owner_ref"`
	RegisteredTons     *string `json:"registered_tons"`
	StrawPayloadTons   *string `json:"straw_payload_tons"`
	RegistrationExpiry *string `json:"registration_expiry"`
	RateType           *string `json:"rate_type"`
	RateVND            *string `json:"rate_vnd"`
	Status             string  `json:"status"`
	Expiry             string  `json:"registration_status"`
	DaysLeft           *int    `json:"registration_days_left"`
	Warning            *string `json:"warning"`
	WarnVersion        string  `json:"warn_param_version"`
	UpdatedAt          string  `json:"updated_at"`
}

func toVessel(v app.VesselView) vesselDTO {
	d := vesselDTO{VesselID: v.Vessel.VesselID.String(), Version: v.Vessel.Version, Code: v.Vessel.Code, Kind: v.Vessel.VehicleKind,
		Name: v.Vessel.Name, OwnerRef: ref(v.Vessel.OwnerRef), RegisteredTons: decp(v.Vessel.RegisteredTons),
		StrawPayloadTons: decp(v.Vessel.StrawPayloadTons), RateType: v.Vessel.RateType, RateVND: decp(v.Vessel.RateVND),
		Status: v.Vessel.Status, Expiry: v.Expiry, DaysLeft: v.DaysLeft, WarnVersion: v.WarnVersion,
		UpdatedAt: v.Vessel.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if v.Vessel.RegistrationExpiry != nil {
		s := v.Vessel.RegistrationExpiry.Format(time.DateOnly)
		d.RegistrationExpiry = &s
	}
	if v.Warning != "" {
		w := v.Warning
		d.Warning = &w
	}
	return d
}

func (h Handler) vesselInput(c *gin.Context) (app.VesselInput, bool) {
	var req vesselReq
	if !bind(c, &req) {
		return app.VesselInput{}, false
	}
	x := newV()
	in := app.VesselInput{Code: req.Code, Kind: req.Kind, Name: req.Name, OwnerRef: x.uuidPtr("owner_ref", req.OwnerRef),
		RegisteredTons: x.dec("registered_tons", req.RegisteredTons), StrawPayloadTons: x.dec("straw_payload_tons", req.StrawPayloadTons),
		RegistrationExpiry: x.date("registration_expiry", req.RegistrationExpiry), RateType: req.RateType, RateVND: x.dec("rate_vnd", req.RateVND),
		Active: req.Status != "inactive", Reason: req.Reason, ExpectedVersion: req.ExpectedVersion, IdemKey: idem(c)}
	if req.Status != "" && req.Status != "active" && req.Status != "inactive" {
		x.add("status", "active | inactive")
	}
	return in, !x.fail(c)
}

func (h Handler) createVessel(c *gin.Context) {
	in, ok := h.vesselInput(c)
	if !ok {
		return
	}
	v, replayed, err := h.Svc.CreateVessel(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/logistics/vessels/"+v.Vessel.VesselID.String())
	created(c, replayed, gin.H{"vessel": toVessel(v)})
}

func (h Handler) reviseVessel(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrVesselNotFound)
	if !ok {
		return
	}
	in, ok := h.vesselInput(c)
	if !ok {
		return
	}
	v, replayed, err := h.Svc.ReviseVessel(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"vessel": toVessel(v)})
}

func (h Handler) getVessel(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrVesselNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetVessel(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"vessel": toVessel(v)})
}

func (h Handler) listVessels(c *gin.Context) {
	status := c.Query("status")
	if status != "" && status != "active" && status != "inactive" {
		httpx.Fail(c, problem.Validation.WithField("status", "active | inactive"))
		return
	}
	vs, err := h.Svc.ListVessels(c.Request.Context(), status)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]vesselDTO, 0, len(vs))
	for _, v := range vs {
		out = append(out, toVessel(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) alerts(c *gin.Context) {
	a, err := h.Svc.Alerts(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	trips := make([]tripDTO, 0, len(a.Unweighed))
	for _, t := range a.Unweighed {
		trips = append(trips, toTrip(t, nil))
	}
	vs := make([]vesselDTO, 0, len(a.Vessels))
	for _, v := range a.Vessels {
		vs = append(vs, toVessel(v))
	}
	c.JSON(http.StatusOK, gin.H{"unweighed_hours": a.UnweighedHours, "unweighed_param_version": a.UnweighedVersion,
		"unweighed_trips": trips, "vessels": vs})
}

// ---------------------------------------------------------------------------
// Tham số
// ---------------------------------------------------------------------------

type paramDTO struct {
	Key            string  `json:"key"`
	VersionID      string  `json:"version_id"`
	VersionSeq     int     `json:"version_seq"`
	Value          string  `json:"value"`
	EffectiveFrom  string  `json:"effective_from"`
	Provisional    bool    `json:"provisional"`
	Status         string  `json:"status"`
	ProposedBy     string  `json:"proposed_by"`
	ProposalReason string  `json:"proposal_reason"`
	DecidedBy      *string `json:"decided_by"`
	DecidedAt      *string `json:"decided_at"`
	DecisionReason *string `json:"decision_reason"`
}

func toParam(p store.Param) paramDTO {
	return paramDTO{Key: p.Key, VersionID: p.VersionID.String(), VersionSeq: p.VersionSeq, Value: p.Value.String(),
		EffectiveFrom: p.EffectiveFrom.Format(time.DateOnly), Provisional: p.Provisional, Status: p.Status, ProposedBy: p.ProposedBy.String(),
		ProposalReason: p.ProposalReason, DecidedBy: ref(p.DecidedBy), DecidedAt: ts(p.DecidedAt), DecisionReason: p.DecisionReason}
}

func (h Handler) params(c *gin.Context) {
	v, err := h.Svc.Params(c.Request.Context(), c.Param("key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]paramDTO, 0, len(v.Versions))
	for _, p := range v.Versions {
		out = append(out, toParam(p))
	}
	c.JSON(http.StatusOK, gin.H{"key": v.Key, "versions": out, "effective": gin.H{"value": v.Effective.Value.String(),
		"param_version": v.Effective.Version, "provisional": v.Effective.Provisional}, "default": v.Default.String()})
}

func (h Handler) propose(c *gin.Context) {
	var req struct {
		Value         string `json:"value"`
		EffectiveFrom string `json:"effective_from"`
		Provisional   bool   `json:"provisional"`
		Reason        string `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	val, err := decimal.NewFromString(req.Value)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("value", "số thập phân (chuỗi)"))
		return
	}
	r, replayed, err := h.Svc.ProposeParam(c.Request.Context(), app.ParamInput{Key: c.Param("key"), Value: val,
		EffectiveFrom: req.EffectiveFrom, Provisional: req.Provisional, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"param": toParam(r.Param)})
}

func (h Handler) decide(c *gin.Context) {
	version, err := uuid.Parse(c.Param("version"))
	if err != nil {
		httpx.Fail(c, app.ErrParamNotFound)
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Decision != "approved" && req.Decision != "rejected" {
		httpx.Fail(c, problem.Validation.WithField("decision", "approved | rejected"))
		return
	}
	r, replayed, err := h.Svc.DecideParam(c.Request.Context(), c.Param("key"), version, req.Decision == "approved", req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"param": toParam(r.Param)})
}
