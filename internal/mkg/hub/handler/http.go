// Package handler: HTTP của mkg/hub trên API nội bộ MKG (cmd/api-internal,
// contracts/openapi/mkg-internal.yaml). Dữ liệu chủ cơ sở và phê duyệt ở bề mặt
// quản trị (MFA); vận hành kho ở bề mặt ứng dụng.
package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vận hành vào nhóm /v1 (đã có xác thực + Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/hub/facilities", h.listFacilities)
	rg.GET("/hub/facilities/:id", h.getFacility)
	rg.GET("/hub/facilities/:id/inventory", h.inventory)
	rg.GET("/hub/facilities/:id/dispatch-suggestion", h.suggest)
	rg.GET("/hub/facilities/:id/publications", h.publications)
	rg.POST("/hub/facilities/:id/receipts", h.receive)
	rg.POST("/hub/facilities/:id/batches", h.transform)
	rg.POST("/hub/facilities/:id/dispatches", h.dispatch)
	rg.POST("/hub/facilities/:id/capacity-snapshots", h.capacitySnapshot)
	rg.GET("/hub/lots/:id", h.getLot)
	rg.POST("/hub/movements/:id/corrections", h.correct)
	rg.GET("/hub/arrivals", h.arrivals)
	h.registerG3App(rg)
}

// RegisterAdmin gắn route dữ liệu chủ và phê duyệt vào nhóm /v1 của bề mặt quản trị (MFA).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/hub/facilities", h.proposeNew)
	rg.POST("/hub/facilities/:id/versions", h.proposeChange)
	rg.GET("/hub/facilities/:id/versions", h.versions)
	rg.POST("/hub/facilities/:id/versions/:version/decision", h.decide)
	rg.POST("/hub/projections/rebuild", h.rebuild)
	h.registerG3Admin(rg)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ; khối lượng là chuỗi thập phân, ví dụ \"1520.500\""))
		return false
	}
	return true
}

func pathID(c *gin.Context, notFound *problem.Problem) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func created(c *gin.Context, replayed bool, location string, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	if location != "" {
		c.Header("Location", location)
	}
	c.JSON(status, body)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// v gom lỗi theo trường.
type v struct {
	p   *problem.Problem
	bad bool
}

func newV() *v { return &v{p: problem.Validation} }

func (x *v) add(field, reason string) { x.p, x.bad = x.p.WithField(field, reason), true }

func (x *v) time(field, s string, required bool) time.Time {
	if s == "" {
		if required {
			x.add(field, "bắt buộc, RFC 3339 có múi giờ")
		}
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		x.add(field, "phải theo RFC 3339 có múi giờ")
	}
	return t
}

func (x *v) uuid(field, s string) uuid.UUID {
	id, err := ids.ParseRef(s)
	if err != nil {
		x.add(field, "phải là UUID")
	}
	return id
}

func (x *v) uuidPtr(field string, s *string) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	id := x.uuid(field, *s)
	return &id
}

func (x *v) kg(field, s string) mass.Kg {
	k, err := mass.ParseKg(s)
	if err != nil {
		x.add(field, "khối lượng kg dạng chuỗi thập phân, tối đa 3 chữ số")
	}
	return k
}

func (x *v) pct(field string, s *string) *decimal.Decimal {
	if s == nil {
		return nil
	}
	d, err := domain.ParsePct(field, *s)
	if err != nil {
		x.add(field, "0..100, tối đa 2 chữ số thập phân, dạng chuỗi")
	}
	return d
}

func (x *v) fail(c *gin.Context) bool {
	if x.bad {
		httpx.Fail(c, x.p)
	}
	return x.bad
}

// ---------------------------------------------------------------------------
// Dữ liệu chủ (Admin)
// ---------------------------------------------------------------------------

type specReq struct {
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Kind             string          `json:"kind"`
	ParentFacilityID *string         `json:"parent_facility_id"`
	Location         pointDTO        `json:"location"`
	Boundary         json.RawMessage `json:"boundary"`
	ProvinceCode     string          `json:"province_code"`
	Status           string          `json:"status"`
	CapacityKg       string          `json:"capacity_kg"`
	EffectiveFrom    string          `json:"effective_from"`
	Reason           string          `json:"reason"`
	EventTime        string          `json:"event_time"`
	DeviceID         *string         `json:"device_id"`
}

func (h Handler) spec(c *gin.Context, facility *uuid.UUID) (app.ProposeInput, bool) {
	var req specReq
	if !bind(c, &req) {
		return app.ProposeInput{}, false
	}
	x := newV()
	in := app.ProposeInput{
		FacilityID: facility, Reason: req.Reason, DeviceID: req.DeviceID, IdemKey: idem(c),
		Spec: domain.FacilitySpec{
			Code: req.Code, Name: req.Name, Kind: domain.Kind(req.Kind), ProvinceCode: req.ProvinceCode,
			Status: domain.Status(req.Status), Boundary: req.Boundary,
		},
	}
	if in.Spec.Status == "" {
		in.Spec.Status = domain.StatusActive
	}
	in.Spec.ParentFacilityID = x.uuidPtr("parent_facility_id", req.ParentFacilityID)
	pt, err := domain.ParsePoint(req.Location.Lat.String(), req.Location.Lng.String())
	var fe domain.FieldError
	if errors.As(err, &fe) {
		x.add(fe.Field, fe.Msg)
	}
	in.Spec.Location = pt
	in.Spec.Capacity = x.kg("capacity_kg", req.CapacityKg)
	in.Spec.EffectiveFrom = x.time("effective_from", req.EffectiveFrom, true)
	in.EventTime = x.time("event_time", req.EventTime, false)
	return in, !x.fail(c)
}

func (h Handler) proposeNew(c *gin.Context) {
	in, ok := h.spec(c, nil)
	if !ok {
		return
	}
	out, replayed, err := h.Svc.Propose(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/hub/facilities/"+out.FacilityID.String()+"/versions", toVersion(out))
}

func (h Handler) proposeChange(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	in, ok := h.spec(c, &id)
	if !ok {
		return
	}
	out, replayed, err := h.Svc.Propose(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/hub/facilities/"+id.String()+"/versions", toVersion(out))
}

func (h Handler) versions(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	vs, err := h.Svc.Versions(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]versionDTO, 0, len(vs))
	for _, v := range vs {
		items = append(items, toVersion(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type decideReq struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (h Handler) decide(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	seq, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || seq < 1 {
		httpx.Fail(c, app.ErrVersionNotFound)
		return
	}
	var req decideReq
	if !bind(c, &req) {
		return
	}
	res, replayed, err := h.Svc.Decide(c.Request.Context(), app.DecideInput{
		FacilityID: id, VersionSeq: seq, Decision: domain.Decision(req.Decision), Reason: req.Reason, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	body := gin.H{"version": toVersion(res.Version), "facility": nil}
	if res.Facility != nil {
		body["facility"] = toFacility(*res.Facility)
	}
	exports := make([]exportDTO, 0, len(res.Exports))
	for _, x := range res.Exports {
		exports = append(exports, toExport(x))
	}
	body["publications"] = exports
	status := http.StatusOK
	if !replayed {
		status = http.StatusCreated
	}
	c.JSON(status, body)
}

func (h Handler) rebuild(c *gin.Context) {
	n, err := h.Svc.RebuildProjections(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"lot_balances": n["lot_balance"], "arrivals": n["arrival_current"]})
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

func (h Handler) listFacilities(c *gin.Context) {
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		copy(after[:], b)
	}
	limit := 50
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			limit = n
		}
	}
	kind := c.Query("kind")
	if kind != "" && kind != string(domain.KindHub) && kind != string(domain.KindWarehouse) && kind != string(domain.KindYard) {
		httpx.Fail(c, problem.Validation.WithField("kind", "hub, warehouse hoặc yard"))
		return
	}
	pg, err := h.Svc.Facilities(c.Request.Context(), kind, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]facilityDTO, 0, len(pg.Items))
	for _, f := range pg.Items {
		items = append(items, toFacility(f))
	}
	resp := gin.H{"items": items, "next_cursor": nil}
	if pg.Next != nil {
		resp["next_cursor"] = base64.RawURLEncoding.EncodeToString(pg.Next[:])
	}
	c.JSON(http.StatusOK, resp)
}

func (h Handler) getFacility(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	f, err := h.Svc.Facility(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toFacility(f))
}

func (h Handler) inventory(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	inv, err := h.Svc.Inventory(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, inventoryDTO(inv))
}

func (h Handler) suggest(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var qty *mass.Kg
	if s := c.Query("mass_kg"); s != "" {
		k, err := mass.ParseKg(s)
		if err != nil || k.Decimal().Sign() <= 0 {
			httpx.Fail(c, problem.Validation.WithField("mass_kg", "khối lượng kg > 0"))
			return
		}
		qty = &k
	}
	sg, err := h.Svc.SuggestDispatch(c.Request.Context(), id, qty)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"risk_rule": sg.Rule, "lots": toRanked(sg.Lots), "allocations": toAllocations(sg.Allocations), "shortfall_kg": kgp(sg.Shortfall),
	})
}

func (h Handler) publications(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	contract := c.DefaultQuery("contract", "hub_capacity")
	if contract != "hub_capacity" && contract != "hub_facility" {
		httpx.Fail(c, problem.Validation.WithField("contract", "hub_facility hoặc hub_capacity"))
		return
	}
	xs, err := h.Svc.Publications(c.Request.Context(), id, contract, 50)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]exportDTO, 0, len(xs))
	for _, x := range xs {
		items = append(items, toExport(x))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) getLot(c *gin.Context) {
	id, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	lv, err := h.Svc.Lot(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"lot": toLot(lv.Lot), "remaining_kg": lv.Remaining.String(), "movements": toMovements(lv.Movements)})
}

// ---------------------------------------------------------------------------
// Vận hành
// ---------------------------------------------------------------------------

type sourceReq struct {
	Kind   string  `json:"kind"`
	Ref    string  `json:"ref"`
	MassKg *string `json:"mass_kg"`
	Ratio  *string `json:"ratio"`
}

type receiveReq struct {
	ID          *string     `json:"id"`
	Code        string      `json:"code"`
	Product     string      `json:"product"`
	MassKg      string      `json:"mass_kg"`
	MoisturePct *string     `json:"moisture_pct"`
	Sources     []sourceReq `json:"sources"`
	EventTime   string      `json:"event_time"`
	DeviceID    *string     `json:"device_id"`
	// G3 (Q-MKG-04)
	WeighTicketRef *string `json:"weigh_ticket_ref"`
	ManualReason   string  `json:"manual_reason"`
}

func (h Handler) receive(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var req receiveReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.ReceiveInput{
		FacilityID: fid, Code: strings.TrimSpace(req.Code), Product: domain.Product(req.Product), DeviceID: req.DeviceID, IdemKey: idem(c),
	}
	if req.ID != nil {
		id, err := ids.Parse(*req.ID)
		if err != nil {
			x.add("id", "phải là UUIDv7")
		} else {
			in.LotID = &id
		}
	}
	in.WeighTicketRef = x.uuidPtr("weigh_ticket_ref", req.WeighTicketRef)
	in.ManualReason = req.ManualReason
	if in.WeighTicketRef == nil || req.MassKg != "" {
		in.Mass = x.kg("mass_kg", req.MassKg)
	}
	in.MoisturePct = x.pct("moisture_pct", req.MoisturePct)
	in.EventTime = x.time("event_time", req.EventTime, false)
	for i, s := range req.Sources {
		f := "sources[" + strconv.Itoa(i) + "]"
		si := domain.SourceInput{Kind: domain.SourceKind(s.Kind), Ref: x.uuid(f+".ref", s.Ref)}
		if s.MassKg != nil {
			k := x.kg(f+".mass_kg", *s.MassKg)
			si.MassKg = &k
		}
		if s.Ratio != nil {
			r, err := decimal.NewFromString(*s.Ratio)
			if err != nil {
				x.add(f+".ratio", "tỷ lệ decimal dạng chuỗi, ví dụ \"0.250000\"")
			}
			si.Ratio = &r
		}
		in.Sources = append(in.Sources, si)
	}
	if x.fail(c) {
		return
	}
	res, replayed, err := h.Svc.Receive(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/hub/lots/"+res.Lot.ID.String(), gin.H{
		"lot": toLot(res.Lot), "movement": toMovement(res.Movement), "over_capacity": res.OverCapacity,
	})
}

type batchReq struct {
	Kind   string `json:"kind"`
	Inputs []struct {
		LotID  string `json:"lot_id"`
		MassKg string `json:"mass_kg"`
	} `json:"inputs"`
	Outputs []struct {
		Code        string  `json:"code"`
		Product     string  `json:"product"`
		MassKg      string  `json:"mass_kg"`
		MoisturePct *string `json:"moisture_pct"`
	} `json:"outputs"`
	EventTime string  `json:"event_time"`
	DeviceID  *string `json:"device_id"`
}

func (h Handler) transform(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var req batchReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.TransformInput{FacilityID: fid, Kind: domain.BatchKind(req.Kind), DeviceID: req.DeviceID, IdemKey: idem(c)}
	for i, it := range req.Inputs {
		f := "inputs[" + strconv.Itoa(i) + "]"
		in.Inputs = append(in.Inputs, app.TransformIn{LotID: x.uuid(f+".lot_id", it.LotID), Mass: x.kg(f+".mass_kg", it.MassKg)})
	}
	for i, it := range req.Outputs {
		f := "outputs[" + strconv.Itoa(i) + "]"
		in.Outputs = append(in.Outputs, app.TransformOut{
			Code: strings.TrimSpace(it.Code), Product: domain.Product(it.Product), Mass: x.kg(f+".mass_kg", it.MassKg),
			MoisturePct: x.pct(f+".moisture_pct", it.MoisturePct),
		})
	}
	in.EventTime = x.time("event_time", req.EventTime, false)
	if x.fail(c) {
		return
	}
	res, replayed, err := h.Svc.Transform(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	outs := make([]lotDTO, 0, len(res.Outputs))
	for _, l := range res.Outputs {
		outs = append(outs, toLot(l))
	}
	b := res.Batch
	created(c, replayed, "", gin.H{
		"batch": gin.H{
			"id": b.ID.String(), "facility_id": b.FacilityID.String(), "kind": string(b.Kind), "input_kg": b.Input.String(),
			"output_kg": b.Output.String(), "loss_kg": b.Loss.String(), "inputs": b.Inputs, "event_time": ts(b.EventTime), "seq": b.Seq,
		},
		"outputs": outs,
	})
}

type dispatchReq struct {
	MassKg         string  `json:"mass_kg"`
	DestinationRef *string `json:"destination_ref"`
	Allocations    []struct {
		LotID  string `json:"lot_id"`
		MassKg string `json:"mass_kg"`
	} `json:"allocations"`
	EventTime string  `json:"event_time"`
	DeviceID  *string `json:"device_id"`
}

func (h Handler) dispatch(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var req dispatchReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.DispatchInput{FacilityID: fid, DeviceID: req.DeviceID, IdemKey: idem(c)}
	in.Mass = x.kg("mass_kg", req.MassKg)
	in.DestinationRef = x.uuidPtr("destination_ref", req.DestinationRef)
	for i, a := range req.Allocations {
		f := "allocations[" + strconv.Itoa(i) + "]"
		in.Allocations = append(in.Allocations, domain.Allocation{LotID: x.uuid(f+".lot_id", a.LotID), MassKg: x.kg(f+".mass_kg", a.MassKg)})
	}
	in.EventTime = x.time("event_time", req.EventTime, false)
	if x.fail(c) {
		return
	}
	res, replayed, err := h.Svc.Dispatch(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d := res.Dispatch
	var dest *string
	if d.DestinationRef != nil {
		s := d.DestinationRef.String()
		dest = &s
	}
	created(c, replayed, "", gin.H{
		"dispatch": gin.H{
			"id": d.ID.String(), "facility_id": d.FacilityID.String(), "mass_kg": d.Mass.String(), "destination_ref": dest,
			"strategy": d.Strategy, "risk_rule": d.RuleVersion, "allocations": toAllocations(d.Allocations), "event_time": ts(d.EventTime),
		},
		"movements": toMovements(res.Movements),
	})
}

func (h Handler) capacitySnapshot(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	x, replayed, err := h.Svc.PublishCapacity(c.Request.Context(), fid, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toExport(x))
}

type correctReq struct {
	Reason    string  `json:"reason"`
	EventTime string  `json:"event_time"`
	DeviceID  *string `json:"device_id"`
}

func (h Handler) correct(c *gin.Context) {
	id, ok := pathID(c, app.ErrMovementNotFound)
	if !ok {
		return
	}
	var req correctReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	t := x.time("event_time", req.EventTime, false)
	if x.fail(c) {
		return
	}
	m, replayed, err := h.Svc.Correct(c.Request.Context(), app.CorrectInput{
		MovementID: id, Reason: req.Reason, EventTime: t, DeviceID: req.DeviceID, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toMovement(m))
}
