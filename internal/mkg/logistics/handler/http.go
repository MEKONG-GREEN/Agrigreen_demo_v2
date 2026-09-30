// Package handler: HTTP của mkg/logistics trên API nội bộ MKG (cmd/api-internal,
// contracts/openapi/mkg-internal.yaml, tag logistics). Điều phối (dispatcher) lập / hủy chuyến, gán
// phương tiện, lập chặng, xác nhận hoàn tất; tài xế (driver) ghi mốc; kế toán trưởng duyệt tham số.
// Dựng lại bảng chiếu ở bề mặt quản trị (MFA).
package handler

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// DeviceHeader là header mã thiết bị của app tài xế.
const DeviceHeader = "X-Device-Id"

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vận hành vào nhóm /v1 (đã có xác thực + Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/logistics/trips", h.listTrips)
	rg.POST("/logistics/trips", h.createTrip)
	rg.GET("/logistics/trips/:id", h.getTrip)
	rg.GET("/logistics/trips/:id/events", h.tripEvents)
	rg.POST("/logistics/trips/:id/vehicle", h.assign)
	rg.POST("/logistics/trips/:id/legs", h.planLegs)
	rg.GET("/logistics/trips/:id/legs/:leg/route-snapshot", h.snapshot)
	rg.POST("/logistics/trips/:id/milestones", h.milestone)
	rg.POST("/logistics/trips/:id/completion", h.complete)
	rg.POST("/logistics/trips/:id/cancel", h.cancel)
	rg.GET("/logistics/vessels", h.listVessels)
	rg.POST("/logistics/vessels", h.createVessel)
	rg.GET("/logistics/vessels/:id", h.getVessel)
	rg.POST("/logistics/vessels/:id/versions", h.reviseVessel)
	rg.GET("/logistics/alerts", h.alerts)
	rg.GET("/logistics/alert-notices", h.notices)
	rg.GET("/logistics/params/:key", h.params)
	rg.POST("/logistics/params/:key/versions", h.propose)
	rg.POST("/logistics/params/:key/versions/:version/decision", h.decide)
}

// RegisterAdmin gắn route quản trị (MFA): dựng lại bảng chiếu (R-22).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/logistics/projections/rebuild", h.rebuild)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ; khối lượng, km, tiền, tọa độ là chuỗi thập phân"))
		return false
	}
	return true
}

func pathID(c *gin.Context, name string, notFound *problem.Problem) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func created(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
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
		x.add(field, "RFC 3339 có múi giờ")
	}
	return t
}

func (x *v) uuidPtr(field string, s *string) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	id, err := ids.ParseRef(*s)
	if err != nil {
		x.add(field, "phải là UUID")
		return nil
	}
	return &id
}

func (x *v) uuid(field, s string) uuid.UUID {
	id, err := ids.ParseRef(s)
	if err != nil {
		x.add(field, "phải là UUID")
	}
	return id
}

func (x *v) kg(field, s string) mass.Kg {
	k, err := mass.ParseKg(s)
	if err != nil || k.Decimal().Sign() < 0 {
		x.add(field, "kg thập phân không âm, tối đa 3 chữ số thập phân")
	}
	return k
}

func (x *v) dec(field string, s *string) *decimal.Decimal {
	if s == nil || *s == "" {
		return nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		x.add(field, "số thập phân (chuỗi)")
		return nil
	}
	return &d
}

func (x *v) date(field string, s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, *s)
	if err != nil {
		x.add(field, "YYYY-MM-DD")
		return nil
	}
	return &t
}

func (x *v) fail(c *gin.Context) bool {
	if x.bad {
		httpx.Fail(c, x.p)
	}
	return x.bad
}

func limit(c *gin.Context) int {
	n, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type cargoDTO struct {
	LotRef    string  `json:"lot_ref"`
	MassKg    string  `json:"mass_kg"`
	TicketRef *string `json:"ticket_ref"`
}

type endpointDTO struct {
	Kind string  `json:"kind"`
	Ref  *string `json:"ref"`
	Lat  string  `json:"lat"`
	Lng  string  `json:"lng"`
}

type legDTO struct {
	LegID            string      `json:"leg_id"`
	Seq              int         `json:"seq"`
	PlanRevision     int         `json:"plan_revision"`
	Mode             string      `json:"mode"`
	From             endpointDTO `json:"from"`
	To               endpointDTO `json:"to"`
	DistanceKm       string      `json:"distance_km"`
	RouteSource      string      `json:"route_source"`
	RouteSnapshotRef *string     `json:"route_snapshot_ref"`
	NetworkVersion   *int        `json:"network_version"`
	ManualReason     *string     `json:"manual_reason"`
	MassKg           *string     `json:"mass_kg"`
	RateVNDPerTKm    string      `json:"rate_vnd_per_t_km,omitempty"` // bỏ với người chỉ có vai driver (G4 mục 10)
	PlannedCost      string      `json:"planned_cost,omitempty"`
	ParamVersion     string      `json:"param_version"`
	Flags            []string    `json:"flags"`
	RuleVersion      string      `json:"rule_version"`
}

type tripDTO struct {
	TripID             string     `json:"trip_id"`
	Code               string     `json:"code"`
	Status             string     `json:"status"`
	Mode               string     `json:"mode"`
	VehicleRef         *string    `json:"vehicle_ref"`
	DriverRef          *string    `json:"driver_ref"`
	CollectionOrderRef *string    `json:"collection_order_ref"`
	DeliveryRef        *string    `json:"delivery_ref"`
	Cargo              []cargoDTO `json:"cargo"`
	PlannedKg          *string    `json:"planned_kg"`
	ActualKg           *string    `json:"actual_kg"`
	PlannedCost        *string    `json:"planned_cost,omitempty"` // cước: bỏ với người chỉ có vai driver (G4 mục 10)
	ActualCost         *string    `json:"actual_cost,omitempty"`
	CostSource         *string    `json:"cost_source,omitempty"`
	PlanRevision       int        `json:"plan_revision"`
	PlannedAt          string     `json:"planned_at"`
	DepartedAt         *string    `json:"departed_at"`
	ArrivedAt          *string    `json:"arrived_at"`
	CompletedAt        *string    `json:"completed_at"`
	CancelledAt        *string    `json:"cancelled_at"`
	CancelReason       *string    `json:"cancel_reason"`
	TicketRef          *string    `json:"ticket_ref"`
	SourceRef          *string    `json:"source_ref"`
	Flags              []string   `json:"flags"`
	RuleVersion        string     `json:"rule_version"`
	RecordedAt         string     `json:"recorded_at"`
	Legs               []legDTO   `json:"legs,omitempty"`
}

func ref(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func ts(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func kgp(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}

func amt(a *money.Amount) *string {
	if a == nil {
		return nil
	}
	s := a.String()
	return &s
}

func flagsOrEmpty(f []string) []string {
	if f == nil {
		return []string{}
	}
	return f
}

func toLeg(l store.Leg) legDTO {
	return legDTO{LegID: l.LegID.String(), Seq: l.LegSeq, PlanRevision: l.PlanRevision, Mode: string(l.Mode),
		From:       endpointDTO{Kind: l.FromKind, Ref: ref(l.FromRef), Lat: l.From.Lat.String(), Lng: l.From.Lng.String()},
		To:         endpointDTO{Kind: l.ToKind, Ref: ref(l.ToRef), Lat: l.To.Lat.String(), Lng: l.To.Lng.String()},
		DistanceKm: l.DistanceKm.StringFixed(3), RouteSource: string(l.RouteSource), RouteSnapshotRef: ref(l.SnapshotID),
		NetworkVersion: l.NetworkVersion, ManualReason: l.ManualReason, MassKg: kgp(l.MassKg), RateVNDPerTKm: l.RateVNDPerTKm.StringFixed(4),
		PlannedCost: l.PlannedCost.String(), ParamVersion: l.ParamVersion, Flags: flagsOrEmpty(l.Flags), RuleVersion: l.RuleVersion}
}

func toTrip(t store.Trip, legs []store.Leg) tripDTO {
	d := tripDTO{TripID: t.TripID.String(), Code: t.Code, Status: string(t.Status), Mode: string(t.Mode), VehicleRef: ref(t.VehicleRef),
		DriverRef: ref(t.DriverRef), CollectionOrderRef: ref(t.CollectionOrderRef), DeliveryRef: ref(t.DeliveryRef), Cargo: []cargoDTO{},
		PlannedKg: kgp(t.PlannedKg), ActualKg: kgp(t.ActualKg), PlannedCost: amt(t.PlannedCost), ActualCost: amt(t.ActualCost),
		PlanRevision: t.PlanRevision, PlannedAt: t.PlannedAt.UTC().Format(time.RFC3339Nano), DepartedAt: ts(t.DepartedAt),
		ArrivedAt: ts(t.ArrivedAt), CompletedAt: ts(t.CompletedAt), CancelledAt: ts(t.CancelledAt), CancelReason: t.CancelReason,
		TicketRef: ref(t.TicketRef), SourceRef: ref(t.SourceRef), Flags: flagsOrEmpty(t.Flags), RuleVersion: t.RuleVersion,
		RecordedAt: t.RecordedAt.UTC().Format(time.RFC3339Nano)}
	if t.CostSource != nil {
		s := string(*t.CostSource)
		d.CostSource = &s
	}
	for _, c := range t.Cargo {
		d.Cargo = append(d.Cargo, cargoDTO{LotRef: c.LotRef.String(), MassKg: c.MassKg.String(), TicketRef: ref(c.TicketRef)})
	}
	for _, l := range legs {
		d.Legs = append(d.Legs, toLeg(l))
	}
	return d
}

type eventDTO struct {
	EventID          string   `json:"event_id"`
	Kind             string   `json:"kind"`
	Status           string   `json:"status"`
	Seq              int64    `json:"seq"`
	EventTime        string   `json:"event_time"`
	RecordedAt       string   `json:"recorded_at"`
	ActorRef         string   `json:"actor_ref"`
	SupersedesID     *string  `json:"supersedes_id"`
	CorrectionReason *string  `json:"correction_reason"`
	Reason           *string  `json:"reason"`
	ActualKg         *string  `json:"actual_kg"`
	ActualCost       *string  `json:"actual_cost"`
	CostSource       *string  `json:"cost_source"`
	ParamVersion     *string  `json:"param_version"`
	Lat              *string  `json:"lat"`
	Lng              *string  `json:"lng"`
	AccuracyM        *string  `json:"accuracy_m,omitempty"`
	SourceRef        *string  `json:"source_ref"`
	Flags            []string `json:"flags"`
	RuleVersion      string   `json:"rule_version"`
}

func decp(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func toEvent(e store.TripEvent) eventDTO {
	d := eventDTO{EventID: e.ID.String(), Kind: e.Kind, Status: string(e.Status), Seq: e.Seq, EventTime: e.EventTime.UTC().Format(time.RFC3339Nano),
		RecordedAt: e.RecordedAt.UTC().Format(time.RFC3339Nano), ActorRef: e.ActorRef.String(), SupersedesID: ref(e.SupersedesID),
		CorrectionReason: e.CorrectionReason, Reason: e.Reason, ActualKg: kgp(e.ActualKg), ActualCost: amt(e.ActualCost),
		ParamVersion: e.ParamVersion, Lat: decp(e.Lat), Lng: decp(e.Lng), AccuracyM: decp(e.AccuracyM), SourceRef: ref(e.SourceRef),
		Flags: flagsOrEmpty(e.Flags), RuleVersion: e.RuleVersion}
	if e.CostSource != nil {
		s := string(*e.CostSource)
		d.CostSource = &s
	}
	return d
}

func tripBody(c *gin.Context, r app.TripResult) gin.H {
	t, ev := toTrip(r.Trip, r.Legs), toEvent(r.Event)
	if driverOnly(c) {
		t, ev = hideCosts(t), hideEventCosts(ev)
	}
	body := gin.H{"trip": t, "warnings": flagsOrEmpty(r.Trip.Flags)}
	if r.Event.ID != uuid.Nil {
		body["event"] = ev
	}
	return body
}

// driverOnly: người gọi chỉ có vai driver (không điều phối) — không thấy cước (G4 tích hợp mục 10).
func driverOnly(c *gin.Context) bool {
	p, ok := authz.FromContext(c.Request.Context())
	return ok && app.DriverOnly(p)
}

// hideCosts bỏ cước kế hoạch / thực, nguồn cước, đơn giá và cước của chặng.
func hideCosts(d tripDTO) tripDTO {
	d.PlannedCost, d.ActualCost, d.CostSource = nil, nil, nil
	for i := range d.Legs {
		d.Legs[i].RateVNDPerTKm, d.Legs[i].PlannedCost = "", ""
	}
	return d
}

func hideEventCosts(e eventDTO) eventDTO {
	e.ActualCost, e.CostSource = nil, nil
	return e
}

// ---------------------------------------------------------------------------
// Chuyến
// ---------------------------------------------------------------------------

type createTripReq struct {
	Mode               string  `json:"mode"`
	VehicleRef         *string `json:"vehicle_ref"`
	DriverRef          *string `json:"driver_ref"`
	CollectionOrderRef *string `json:"collection_order_ref"`
	DeliveryRef        *string `json:"delivery_ref"`
	Cargo              []struct {
		LotRef string `json:"lot_ref"`
		MassKg string `json:"mass_kg"`
	} `json:"cargo"`
	PlannedKg *string `json:"planned_kg"`
	EventTime string  `json:"event_time"`
}

func (h Handler) createTrip(c *gin.Context) {
	var req createTripReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.CreateTripInput{Mode: port.Mode(req.Mode), VehicleRef: x.uuidPtr("vehicle_ref", req.VehicleRef),
		DriverRef: x.uuidPtr("driver_ref", req.DriverRef), CollectionOrderRef: x.uuidPtr("collection_order_ref", req.CollectionOrderRef),
		DeliveryRef: x.uuidPtr("delivery_ref", req.DeliveryRef), EventTime: x.time("event_time", req.EventTime, false), IdemKey: idem(c)}
	if len(req.Cargo) > 50 {
		x.add("cargo", "tối đa 50 lô")
	}
	for i, cg := range req.Cargo {
		f := "cargo[" + strconv.Itoa(i) + "]"
		in.Cargo = append(in.Cargo, app.CargoInput{LotRef: x.uuid(f+".lot_ref", cg.LotRef), MassKg: x.kg(f+".mass_kg", cg.MassKg)})
	}
	if req.PlannedKg != nil {
		k := x.kg("planned_kg", *req.PlannedKg)
		in.PlannedKg = &k
	}
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.CreateTrip(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/logistics/trips/"+r.Trip.TripID.String())
	created(c, replayed, tripBody(c, r))
}

func (h Handler) getTrip(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	r, err := h.Svc.GetTrip(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d := toTrip(r.Trip, r.Legs)
	if driverOnly(c) {
		d = hideCosts(d)
	}
	c.JSON(http.StatusOK, gin.H{"trip": d})
}

func (h Handler) tripEvents(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	evs, err := h.Svc.TripEvents(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]eventDTO, 0, len(evs))
	for _, e := range evs {
		out = append(out, toEvent(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// listTrips: GET /v1/logistics/trips?status=&assigned_to_me=true&updated_since=RFC3339&cursor=&limit=
//   - assigned_to_me=true → chỉ chuyến có driver_ref = sub của token; người chỉ có vai driver luôn bị lọc như vậy;
//   - updated_since → chế độ delta: chuyến có sự kiện ghi sau mốc (kể cả hủy / hoàn tất), sắp recorded_at tăng dần,
//     con trỏ theo recorded_at; không có → sắp planned_at giảm dần như trước (tương thích).
func (h Handler) listTrips(c *gin.Context) {
	at, id, ok := app.DecodeCursor(c.Query("cursor"))
	if !ok {
		httpx.Fail(c, problem.Validation.WithField("cursor", "con trỏ không hợp lệ"))
		return
	}
	status := c.Query("status")
	switch port.TripStatus(status) {
	case "", port.TripPlanned, port.TripDeparted, port.TripArrived, port.TripCompleted, port.TripCancelled:
	default:
		httpx.Fail(c, problem.Validation.WithField("status", "planned | departed | arrived | completed | cancelled"))
		return
	}
	q := app.TripQuery{Status: status, AfterAt: at, AfterID: id, Limit: limit(c)}
	switch c.Query("assigned_to_me") {
	case "", "false":
	case "true":
		q.AssignedToMe = true
	default:
		httpx.Fail(c, problem.Validation.WithField("assigned_to_me", "true | false"))
		return
	}
	if s := c.Query("updated_since"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("updated_since", "RFC 3339 có múi giờ"))
			return
		}
		q.UpdatedSince = &t
	}
	ts, hide, err := h.Svc.QueryTrips(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]tripDTO, 0, len(ts))
	for _, t := range ts {
		d := toTrip(t, nil)
		if hide {
			d = hideCosts(d)
		}
		items = append(items, d)
	}
	var next *string
	if len(ts) > 0 && len(ts) == q.Limit {
		last := ts[len(ts)-1]
		cur := last.PlannedAt
		if q.UpdatedSince != nil {
			cur = last.RecordedAt
		}
		s := app.EncodeCursor(cur, last.TripID)
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

// notices: GET /v1/logistics/alert-notices?kind=&cursor=&limit= — thông báo cảnh báo L-08 đã ghi (điều phối).
func (h Handler) notices(c *gin.Context) {
	at, id, ok := app.DecodeCursor(c.Query("cursor"))
	if !ok {
		httpx.Fail(c, problem.Validation.WithField("cursor", "con trỏ không hợp lệ"))
		return
	}
	kind := c.Query("kind")
	switch kind {
	case "", app.NoticeTripUnweighed, app.NoticeRegistrationExpired, app.NoticeRegistrationExpiring:
	default:
		httpx.Fail(c, problem.Validation.WithField("kind", "trip_unweighed | registration_expired | registration_expiring"))
		return
	}
	n := limit(c)
	ns, err := h.Svc.Notices(c.Request.Context(), kind, at, id, n)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(ns))
	for _, x := range ns {
		items = append(items, gin.H{"notice_id": x.ID.String(), "kind": x.Kind, "subject_ref": x.SubjectRef.String(), "subject_code": x.SubjectCode,
			"audience": x.Audience, "detail": x.Detail, "param_version": x.ParamVersion, "rule_version": x.RuleVersion,
			"noticed_at": x.NoticedAt.UTC().Format(time.RFC3339Nano)})
	}
	var next *string
	if len(ns) == n {
		last := ns[len(ns)-1]
		s := app.EncodeCursor(last.NoticedAt, last.ID)
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

type assignReq struct {
	VehicleRef string  `json:"vehicle_ref"`
	DriverRef  *string `json:"driver_ref"`
}

func (h Handler) assign(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	var req assignReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	vehicle := x.uuid("vehicle_ref", req.VehicleRef)
	driver := x.uuidPtr("driver_ref", req.DriverRef)
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.AssignVehicle(c.Request.Context(), id, vehicle, driver, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, tripBody(c, r))
}

type legReq struct {
	Mode         string      `json:"mode"`
	From         endpointReq `json:"from"`
	To           endpointReq `json:"to"`
	ManualKm     *string     `json:"manual_km"`
	ManualReason string      `json:"manual_reason"`
}

type endpointReq struct {
	Kind string          `json:"kind"`
	Ref  *string         `json:"ref"`
	Lat  decimal.Decimal `json:"lat"`
	Lng  decimal.Decimal `json:"lng"`
}

func (h Handler) planLegs(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	var req struct {
		Legs []legReq `json:"legs"`
	}
	if !bind(c, &req) {
		return
	}
	x := newV()
	legs := make([]app.LegInput, 0, len(req.Legs))
	for i, l := range req.Legs {
		f := "legs[" + strconv.Itoa(i) + "]"
		legs = append(legs, app.LegInput{Mode: port.Mode(l.Mode),
			From:     app.Endpoint{Kind: l.From.Kind, Ref: x.uuidPtr(f+".from.ref", l.From.Ref), Point: port.GeoPoint{Lat: l.From.Lat, Lng: l.From.Lng}},
			To:       app.Endpoint{Kind: l.To.Kind, Ref: x.uuidPtr(f+".to.ref", l.To.Ref), Point: port.GeoPoint{Lat: l.To.Lat, Lng: l.To.Lng}},
			ManualKm: x.dec(f+".manual_km", l.ManualKm), ManualReason: l.ManualReason})
	}
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.PlanLegs(c.Request.Context(), id, legs, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, tripBody(c, r))
}

func (h Handler) snapshot(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	leg, ok := pathID(c, "leg", app.ErrSnapshotNotFound)
	if !ok {
		return
	}
	s, err := h.Svc.RouteSnapshot(c.Request.Context(), id, leg)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshot_id": s.ID.String(), "trip_id": s.TripID.String(), "leg_id": s.LegID.String(),
		"provider": s.Provider, "request": json.RawMessage(s.Request), "raw_base64": base64.StdEncoding.EncodeToString(s.Raw),
		"raw_sha256": hex.EncodeToString(s.RawSHA256), "grantor_tenant": ref(s.GrantorTenant), "network_version": s.NetworkVersion,
		"fetched_at": s.FetchedAt.UTC().Format(time.RFC3339Nano), "recorded_at": s.RecordedAt.UTC().Format(time.RFC3339Nano),
		"rule_version": s.RuleVersion})
}

// milestoneReq: lat/lng tùy chọn (null / vắng = máy không có GPS → lưu NULL, không bao giờ 0,0); accuracy_m tùy
// chọn. App cũ gửi {kind, event_time, lat, lng} vẫn hợp lệ. (0, 0) — không bao giờ là vị trí ở VN — coi như không
// có GPS (app cũ gửi 0,0 khi thiếu tọa độ).
type milestoneReq struct {
	Kind      string           `json:"kind"`
	EventTime string           `json:"event_time"`
	Lat       *decimal.Decimal `json:"lat"`
	Lng       *decimal.Decimal `json:"lng"`
	AccuracyM *decimal.Decimal `json:"accuracy_m"`
}

func (h Handler) milestone(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	var req milestoneReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	at := x.time("event_time", req.EventTime, true)
	if x.fail(c) {
		return
	}
	var dev *string
	if d := c.GetHeader(DeviceHeader); d != "" {
		dev = &d
	}
	if req.Lat != nil && req.Lng != nil && req.Lat.IsZero() && req.Lng.IsZero() {
		req.Lat, req.Lng, req.AccuracyM = nil, nil, nil
	}
	r, replayed, err := h.Svc.RecordMilestone(c.Request.Context(), id, app.MilestoneInput{Kind: req.Kind, EventTime: at, Lat: req.Lat,
		Lng: req.Lng, AccuracyM: req.AccuracyM, DeviceID: dev, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, tripBody(c, r))
}

type completeReq struct {
	ActualKg         string  `json:"actual_kg"`
	TicketRef        *string `json:"ticket_ref"`
	ActualCost       *string `json:"actual_cost"`
	Reason           string  `json:"reason"`
	CorrectionReason string  `json:"correction_reason"`
	EventTime        string  `json:"event_time"`
}

func (h Handler) complete(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	var req completeReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.CompleteInput{ActualKg: x.kg("actual_kg", req.ActualKg), TicketRef: x.uuidPtr("ticket_ref", req.TicketRef), Reason: req.Reason,
		CorrectionReason: req.CorrectionReason, EventTime: x.time("event_time", req.EventTime, false), IdemKey: idem(c)}
	if req.ActualCost != nil {
		a, err := money.ParseAmount(*req.ActualCost)
		if err != nil {
			x.add("actual_cost", "VND, tối đa 2 chữ số thập phân")
		} else {
			in.ActualCost = &a
		}
	}
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.CompleteTrip(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, tripBody(c, r))
}

func (h Handler) cancel(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrTripNotFound)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	r, replayed, err := h.Svc.CancelTrip(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, tripBody(c, r))
}

func (h Handler) rebuild(c *gin.Context) {
	n, err := h.Svc.RebuildProjections(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"trip_current": n})
}
