// Package handler: HTTP của mkg/measurement trên API nội bộ MKG (cmd/api-internal,
// contracts/openapi/mkg-internal.yaml). Duyệt thiết bị, dung sai, định mức hao
// hụt ở bề mặt quản trị (MFA); vận hành cân, đối soát, tranh chấp ở bề mặt ứng dụng.
package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

const base = "/measurement"

// RegisterApp gắn route vận hành.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET(base+"/devices", h.listDevices)
	rg.GET(base+"/devices/:device_id", h.getDevice)
	rg.POST(base+"/sessions", h.openSession)
	rg.GET(base+"/sessions/:id", h.getSession)
	rg.POST(base+"/sessions/:id/close", h.closeSession)
	rg.GET(base+"/tickets/:id", h.getTicket)
	rg.GET(base+"/lots/:id/tickets", h.lotTickets)
	rg.POST(base+"/lots/:id/tickets", h.manualTicket)
	rg.POST(base+"/lots/:id/terms", h.setTerms)
	rg.POST(base+"/lots/:id/reconciliations", h.reconcile)
	rg.GET(base+"/lots/:id/reconciliations", h.reconciliations)
	rg.GET(base+"/lots/:id/yield", h.yield)
	rg.POST(base+"/lots/:id/mrv-evidence", h.mrvEvidence)
	rg.GET(base+"/lots/:id/publications", h.publications)
	rg.POST(base+"/customer-weight-imports", h.importWeights)
	rg.GET(base+"/customer-weight-imports/:id", h.getImport)
	rg.GET(base+"/disputes", h.listDisputes)
	rg.GET(base+"/disputes/:id", h.getDispute)
	rg.POST(base+"/disputes/:id/actions", h.disputeAction)
	rg.GET(base+"/tolerance-versions", h.listParams(store.ParamTolerance))
	rg.GET(base+"/loss-norm-versions", h.listParams(store.ParamLossNorm))
}

// RegisterAdmin gắn route đăng ký / duyệt (bề mặt quản trị, MFA).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST(base+"/devices", h.proposeDevice)
	rg.GET(base+"/devices", h.listDevices)
	rg.POST(base+"/devices/:device_id/epochs/:epoch/decision", h.decideDevice)
	rg.POST(base+"/devices/:device_id/epochs/:epoch/revoke", h.revokeDevice)
	rg.POST(base+"/tolerance-versions", h.proposeTolerance)
	rg.GET(base+"/tolerance-versions", h.listParams(store.ParamTolerance))
	rg.POST(base+"/tolerance-versions/:seq/decision", h.decideParam(store.ParamTolerance))
	rg.POST(base+"/loss-norm-versions", h.proposeLossNorm)
	rg.GET(base+"/loss-norm-versions", h.listParams(store.ParamLossNorm))
	rg.POST(base+"/loss-norm-versions/:seq/decision", h.decideParam(store.ParamLossNorm))
	rg.POST(base+"/projections/rebuild", h.rebuild)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ; khối lượng là chuỗi thập phân, ví dụ \"1520.500\""))
		return false
	}
	return true
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func pathID(c *gin.Context, nf *problem.Problem) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, nf)
		return uuid.Nil, false
	}
	return id, true
}

func created(c *gin.Context, replayed bool, body any) {
	if replayed {
		c.JSON(http.StatusOK, body)
		return
	}
	c.JSON(http.StatusCreated, body)
}

// v gom lỗi theo trường.
type v struct {
	p   *problem.Problem
	bad bool
}

func newV() *v { return &v{p: problem.Validation} }

func (x *v) add(f, r string) { x.p, x.bad = x.p.WithField(f, r), true }

func (x *v) time(f, s string, required bool) time.Time {
	if s == "" {
		if required {
			x.add(f, "bắt buộc, RFC 3339 có múi giờ")
		}
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		x.add(f, "phải theo RFC 3339 có múi giờ")
	}
	return t
}

func (x *v) ref(f string, s *string) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	id, err := ids.ParseRef(*s)
	if err != nil {
		x.add(f, "phải là UUID")
		return nil
	}
	return &id
}

func (x *v) v7(f string, s *string) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	id, err := ids.Parse(*s)
	if err != nil {
		x.add(f, "phải là UUIDv7")
		return nil
	}
	return &id
}

func (x *v) kg(f, s string) mass.Kg {
	k, err := mass.ParseKg(s)
	if err != nil || k.NonNegative() != nil {
		x.add(f, "khối lượng kg ≥ 0 dạng chuỗi thập phân, tối đa 3 chữ số")
	}
	return k
}

func (x *v) pct(f, s string) decimal.Decimal {
	d, err := domain.ParsePct(f, s)
	if err != nil {
		x.add(f, "phần trăm 0..100 dạng chuỗi, tối đa 2 chữ số")
	}
	return d
}

func (x *v) fail(c *gin.Context) bool {
	if x.bad {
		httpx.Fail(c, x.p)
	}
	return x.bad
}

func limitOf(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

// ---------------------------------------------------------------------------
// Thiết bị
// ---------------------------------------------------------------------------

type registerReq struct {
	DeviceID    string `json:"device_id"`
	Epoch       int    `json:"epoch"`
	FacilityRef string `json:"facility_ref"`
	Point       string `json:"point"`
	PublicKey   string `json:"public_key"` // base64 32 byte
	CertSHA256  string `json:"cert_sha256"`
	ScaleModel  string `json:"scale_model"`
}

func (h Handler) proposeDevice(c *gin.Context) {
	var req registerReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	fac := x.ref("facility_ref", &req.FacilityRef)
	pub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		x.add("public_key", "base64 chuẩn")
	}
	if x.fail(c) {
		return
	}
	in := app.RegisterInput{DeviceID: req.DeviceID, Epoch: req.Epoch, Point: port.WeighPoint(req.Point), PublicKey: pub,
		CertSHA256: strings.ToLower(req.CertSHA256), ScaleModel: req.ScaleModel, IdemKey: idem(c)}
	if fac != nil {
		in.FacilityRef = *fac
	}
	d, rep, err := h.Svc.ProposeDevice(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toDevice(d))
}

type decisionReq struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func epochParam(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("epoch"))
	if err != nil || n < 1 {
		httpx.Fail(c, app.ErrDeviceNotFound)
		return 0, false
	}
	return n, true
}

func (h Handler) decideDevice(c *gin.Context) {
	epoch, ok := epochParam(c)
	if !ok {
		return
	}
	var req decisionReq
	if !bind(c, &req) {
		return
	}
	d, rep, err := h.Svc.DecideDevice(c.Request.Context(), c.Param("device_id"), epoch, req.Decision, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toDevice(d))
}

func (h Handler) revokeDevice(c *gin.Context) {
	epoch, ok := epochParam(c)
	if !ok {
		return
	}
	var req decisionReq
	if !bind(c, &req) {
		return
	}
	d, rep, err := h.Svc.RevokeDevice(c.Request.Context(), c.Param("device_id"), epoch, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toDevice(d))
}

func (h Handler) listDevices(c *gin.Context) {
	afterDev, afterEpoch := "", 0
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		i := strings.LastIndexByte(string(b), '|')
		if err != nil || i < 0 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		afterDev = string(b[:i])
		afterEpoch, _ = strconv.Atoi(string(b[i+1:]))
	}
	limit := limitOf(c)
	ds, err := h.Svc.Devices(c.Request.Context(), afterDev, afterEpoch, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]deviceDTO, 0, len(ds))
	for _, d := range ds {
		items = append(items, toDevice(d))
	}
	var next *string
	if limit > 0 && len(ds) == limit {
		last := ds[len(ds)-1]
		s := base64.RawURLEncoding.EncodeToString([]byte(last.DeviceID + "|" + strconv.Itoa(last.Epoch)))
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) getDevice(c *gin.Context) {
	ds, err := h.Svc.Device(c.Request.Context(), c.Param("device_id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]deviceDTO, 0, len(ds))
	for _, d := range ds {
		items = append(items, toDevice(d))
	}
	c.JSON(http.StatusOK, gin.H{"device_id": c.Param("device_id"), "epochs": items})
}

// ---------------------------------------------------------------------------
// Phiên cân
// ---------------------------------------------------------------------------

type openReq struct {
	ID          *string `json:"id"`
	LotRef      string  `json:"lot_ref"`
	DeviceID    string  `json:"device_id"`
	Epoch       int     `json:"epoch"`
	CounterFrom int64   `json:"counter_from"`
	EventTime   string  `json:"event_time"`
}

func (h Handler) openSession(c *gin.Context) {
	var req openReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	id := x.v7("id", req.ID)
	lot := x.ref("lot_ref", &req.LotRef)
	at := x.time("event_time", req.EventTime, false)
	if lot == nil && !x.bad {
		x.add("lot_ref", "bắt buộc")
	}
	if x.fail(c) {
		return
	}
	s, rep, err := h.Svc.OpenSession(c.Request.Context(), app.OpenSessionInput{ID: id, LotRef: *lot, DeviceID: req.DeviceID, Epoch: req.Epoch,
		CounterFrom: req.CounterFrom, EventTime: at, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toSession(s))
}

type closeReq struct {
	CounterTo *int64 `json:"counter_to"`
	EventTime string `json:"event_time"`
}

func (h Handler) closeSession(c *gin.Context) {
	id, ok := pathID(c, app.ErrSessionNotFound)
	if !ok {
		return
	}
	var req closeReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	at := x.time("event_time", req.EventTime, false)
	if req.CounterTo == nil {
		x.add("counter_to", "bắt buộc")
	}
	if x.fail(c) {
		return
	}
	s, rep, err := h.Svc.CloseSession(c.Request.Context(), id, *req.CounterTo, at, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toSession(s))
}

func (h Handler) getSession(c *gin.Context) {
	id, ok := pathID(c, app.ErrSessionNotFound)
	if !ok {
		return
	}
	s, err := h.Svc.GetSession(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toSession(s))
}

// ---------------------------------------------------------------------------
// Phiếu cân
// ---------------------------------------------------------------------------

func (h Handler) getTicket(c *gin.Context) {
	id, ok := pathID(c, app.ErrTicketNotFound)
	if !ok {
		return
	}
	t, err := h.Svc.Ticket(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toTicket(t))
}

func (h Handler) lotTickets(c *gin.Context) {
	id, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	ts, lm, err := h.Svc.LotTickets(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	eff := map[uuid.UUID]bool{}
	for _, pm := range lm.Points {
		eff[pm.TicketID] = true
	}
	items := make([]ticketDTO, 0, len(ts))
	for _, t := range ts {
		d := toTicket(t)
		e := eff[t.ID]
		d.Effective = &e
		items = append(items, d)
	}
	c.JSON(http.StatusOK, gin.H{"mass": toLotMass(*lm), "tickets": items})
}

type manualReq struct {
	ID               *string `json:"id"`
	Point            string  `json:"point"`
	Route            string  `json:"route"`
	MassKg           string  `json:"mass_kg"`
	MoisturePct      *string `json:"moisture_pct"`
	MoistureState    string  `json:"moisture_state"`
	WeighedAt        string  `json:"weighed_at"`
	EvidenceRef      *string `json:"evidence_ref"`
	CustomerRef      *string `json:"customer_ref"`
	Reason           string  `json:"reason"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason string  `json:"correction_reason"`
}

func (h Handler) manualTicket(c *gin.Context) {
	lot, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	var req manualReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.ManualTicketInput{ID: x.v7("id", req.ID), LotRef: lot, Point: port.WeighPoint(req.Point), Route: port.Route(req.Route),
		MassKg: x.kg("mass_kg", req.MassKg), MoistureState: req.MoistureState, WeighedAt: x.time("weighed_at", req.WeighedAt, true),
		EvidenceRef: x.ref("evidence_ref", req.EvidenceRef), CustomerRef: x.ref("customer_ref", req.CustomerRef), Reason: req.Reason,
		Supersedes: x.ref("supersedes_id", req.SupersedesID), CorrectionReason: req.CorrectionReason, IdemKey: idem(c)}
	if in.Point == "" {
		in.Point = port.PointCustomerIn
	}
	if req.MoisturePct != nil {
		in.MoisturePct = x.pct("moisture_pct", *req.MoisturePct).StringFixed(2)
	}
	if in.EvidenceRef == nil && !x.bad {
		x.add("evidence_ref", "nhập tay bắt buộc tệp bằng chứng")
	}
	if x.fail(c) {
		return
	}
	res, rep, err := h.Svc.RecordManual(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	body := gin.H{"ticket": toTicket(res.Ticket), "reconciliation": nil}
	if res.Reconciliation != nil {
		body["reconciliation"] = toRecon(*res.Reconciliation)
	}
	created(c, rep, body)
}

type termsReq struct {
	PricingBasis string  `json:"pricing_basis"`
	ContractRef  *string `json:"contract_ref"`
	CustomerRef  *string `json:"customer_ref"`
	Reason       string  `json:"reason"`
}

func (h Handler) setTerms(c *gin.Context) {
	lot, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	var req termsReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	contract, customer := x.ref("contract_ref", req.ContractRef), x.ref("customer_ref", req.CustomerRef)
	if x.fail(c) {
		return
	}
	t, rep, err := h.Svc.SetTerms(c.Request.Context(), lot, port.PricingBasis(req.PricingBasis), contract, customer, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, gin.H{"id": t.ID, "lot_ref": t.LotRef, "pricing_basis": t.Basis, "contract_ref": t.ContractRef,
		"customer_ref": t.CustomerRef, "seq": t.Seq})
}

// ---------------------------------------------------------------------------
// Đối soát, sản lượng, van
// ---------------------------------------------------------------------------

func (h Handler) reconcile(c *gin.Context) {
	lot, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	r, rep, err := h.Svc.Reconcile(c.Request.Context(), lot, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toRecon(*r))
}

func (h Handler) reconciliations(c *gin.Context) {
	lot, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	rs, err := h.Svc.Reconciliations(c.Request.Context(), lot)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]reconDTO, 0, len(rs))
	for _, r := range rs {
		items = append(items, toRecon(r))
	}
	c.JSON(http.StatusOK, gin.H{"latest": items[0], "items": items})
}

func (h Handler) yield(c *gin.Context) {
	lot, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	y, err := h.Svc.Yield(c.Request.Context(), lot)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"lot_ref": y.LotRef, "route": y.Route, "basis_point": y.BasisPoint, "basis_kg": y.BasisKg,
		"rolls": y.Rolls, "yield_kg_per_roll": y.YieldKgPerRoll, "aggregated": y.Aggregated, "reconciliation_id": y.ReconciliationID})
}

type mrvReq struct {
	SeasonRef *string `json:"season_ref"`
}

func (h Handler) mrvEvidence(c *gin.Context) {
	lot, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	var req mrvReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	season := x.ref("season_ref", req.SeasonRef)
	if x.fail(c) {
		return
	}
	xs, rep, err := h.Svc.PublishMRVEvidence(c.Request.Context(), lot, season, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]exportDTO, 0, len(xs))
	for _, v := range xs {
		items = append(items, toExport(v))
	}
	created(c, rep, gin.H{"publications": items})
}

func (h Handler) publications(c *gin.Context) {
	lot, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	xs, err := h.Svc.Publications(c.Request.Context(), lot)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]exportDTO, 0, len(xs))
	for _, v := range xs {
		items = append(items, toExport(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Nhập số cân của khách
// ---------------------------------------------------------------------------

type importRowReq struct {
	LotRef           string  `json:"lot_ref"`
	Route            string  `json:"route"`
	MassKg           string  `json:"mass_kg"`
	MoisturePct      *string `json:"moisture_pct"`
	WeighedAt        string  `json:"weighed_at"`
	CustomerTicketNo *string `json:"customer_ticket_no"`
}

type importReq struct {
	Channel     string         `json:"channel"`
	CustomerRef *string        `json:"customer_ref"`
	EvidenceRef *string        `json:"evidence_ref"`
	FileSHA256  string         `json:"file_sha256"`
	Reason      string         `json:"reason"`
	Rows        []importRowReq `json:"rows"`
}

func (h Handler) importWeights(c *gin.Context) {
	var req importReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.ImportInput{Channel: req.Channel, CustomerRef: x.ref("customer_ref", req.CustomerRef), EvidenceRef: x.ref("evidence_ref", req.EvidenceRef),
		FileSHA256: strings.ToLower(req.FileSHA256), Reason: req.Reason, IdemKey: idem(c)}
	for i, r := range req.Rows {
		f := "rows[" + strconv.Itoa(i) + "]."
		row := store.ImportRow{MassKg: x.kg(f+"mass_kg", r.MassKg), WeighedAt: x.time(f+"weighed_at", r.WeighedAt, true),
			CustomerTicketNo: r.CustomerTicketNo, Route: r.Route}
		if l := x.ref(f+"lot_ref", &r.LotRef); l != nil {
			row.LotRef = *l
		} else if !x.bad {
			x.add(f+"lot_ref", "bắt buộc")
		}
		if r.MoisturePct != nil {
			s := x.pct(f+"moisture_pct", *r.MoisturePct).StringFixed(2)
			row.MoisturePct = &s
		}
		in.Rows = append(in.Rows, row)
	}
	if x.fail(c) {
		return
	}
	res, rep, err := h.Svc.ImportCustomerWeights(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	recs := make([]reconDTO, 0, len(res.Reconciliations))
	for _, r := range res.Reconciliations {
		recs = append(recs, toRecon(r))
	}
	created(c, rep, gin.H{"import": toImport(res.Import), "reconciliations": recs})
}

func toImport(im store.Import) gin.H {
	return gin.H{"id": im.ID, "channel": im.Channel, "customer_ref": im.CustomerRef, "evidence_ref": im.EvidenceRef,
		"file_sha256": im.FileSHA256, "rows": im.Rows, "ticket_ids": im.TicketIDs, "provisional": im.Provisional, "reason": im.Reason,
		"recorded_at": im.RecordedAt}
}

func (h Handler) getImport(c *gin.Context) {
	id, ok := pathID(c, app.ErrImportNotFound)
	if !ok {
		return
	}
	im, err := h.Svc.Import(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toImport(im))
}

// ---------------------------------------------------------------------------
// Tranh chấp
// ---------------------------------------------------------------------------

func (h Handler) listDisputes(c *gin.Context) {
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		copy(after[:], b)
	}
	status := c.Query("status")
	switch status {
	case "", domain.DisputeOpen, domain.DisputeUnderReview, domain.DisputeResolved, domain.DisputeCancelled:
	default:
		httpx.Fail(c, problem.Validation.WithField("status", "open, under_review, resolved hoặc cancelled"))
		return
	}
	limit := limitOf(c)
	ds, err := h.Svc.Disputes(c.Request.Context(), status, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]disputeDTO, 0, len(ds))
	for _, d := range ds {
		items = append(items, toDispute(d, nil))
	}
	var next *string
	if limit > 0 && len(ds) == limit {
		last := ds[len(ds)-1].ID
		s := base64.RawURLEncoding.EncodeToString(last[:])
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) getDispute(c *gin.Context) {
	id, ok := pathID(c, app.ErrDisputeNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.Dispute(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDispute(d.Dispute, d.Events))
}

type disputeReq struct {
	Action       string   `json:"action"`
	EvidenceRefs []string `json:"evidence_refs"`
	Resolution   string   `json:"resolution"`
	AgreedKg     *string  `json:"agreed_kg"`
	Note         string   `json:"note"`
}

func (h Handler) disputeAction(c *gin.Context) {
	id, ok := pathID(c, app.ErrDisputeNotFound)
	if !ok {
		return
	}
	var req disputeReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.DisputeActionInput{DisputeID: id, Action: req.Action, Resolution: req.Resolution, Note: req.Note, IdemKey: idem(c)}
	for i := range req.EvidenceRefs {
		if r := x.ref("evidence_refs["+strconv.Itoa(i)+"]", &req.EvidenceRefs[i]); r != nil {
			in.EvidenceRefs = append(in.EvidenceRefs, *r)
		}
	}
	if req.AgreedKg != nil {
		k := x.kg("agreed_kg", *req.AgreedKg)
		in.AgreedKg = &k
	}
	if x.fail(c) {
		return
	}
	d, rep, err := h.Svc.ActOnDispute(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toDispute(d.Dispute, d.Events))
}

// ---------------------------------------------------------------------------
// Tham số (Admin)
// ---------------------------------------------------------------------------

type toleranceReq struct {
	CommercialFlagPct     string `json:"commercial_flag_pct"`
	CommercialCriticalPct string `json:"commercial_critical_pct"`
	RollFlagPct           string `json:"roll_flag_pct"`
	RollCriticalPct       string `json:"roll_critical_pct"`
	EffectiveFrom         string `json:"effective_from"`
	Provisional           *bool  `json:"provisional"`
	Reason                string `json:"reason"`
}

func provisional(b *bool) bool { return b == nil || *b }

func (h Handler) proposeTolerance(c *gin.Context) {
	var req toleranceReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	t := domain.Tolerance{CommercialFlag: x.pct("commercial_flag_pct", req.CommercialFlagPct),
		CommercialCrit: x.pct("commercial_critical_pct", req.CommercialCriticalPct), RollFlag: x.pct("roll_flag_pct", req.RollFlagPct),
		RollCrit: x.pct("roll_critical_pct", req.RollCriticalPct), EffectiveFrom: x.time("effective_from", req.EffectiveFrom, true),
		Provisional: provisional(req.Provisional)}
	if x.fail(c) {
		return
	}
	p, rep, err := h.Svc.ProposeTolerance(c.Request.Context(), t, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toParam(p))
}

type lossNormReq struct {
	Bands            []domain.Band `json:"bands"`
	DefaultLossPct   string        `json:"default_loss_pct"`
	AbnormalAlertPct string        `json:"abnormal_alert_pct"`
	EffectiveFrom    string        `json:"effective_from"`
	Provisional      *bool         `json:"provisional"`
	Reason           string        `json:"reason"`
}

func (h Handler) proposeLossNorm(c *gin.Context) {
	var req lossNormReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	n := domain.LossNorm{Bands: req.Bands, DefaultLoss: x.pct("default_loss_pct", req.DefaultLossPct),
		AlertPct: x.pct("abnormal_alert_pct", req.AbnormalAlertPct), EffectiveFrom: x.time("effective_from", req.EffectiveFrom, true),
		Provisional: provisional(req.Provisional)}
	if x.fail(c) {
		return
	}
	p, rep, err := h.Svc.ProposeLossNorm(c.Request.Context(), n, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, rep, toParam(p))
}

func (h Handler) decideParam(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		seq, err := strconv.ParseInt(c.Param("seq"), 10, 64)
		if err != nil || seq < 1 {
			httpx.Fail(c, app.ErrVersionNotFound)
			return
		}
		var req decisionReq
		if !bind(c, &req) {
			return
		}
		p, rep, err := h.Svc.DecideParam(c.Request.Context(), kind, seq, req.Decision, req.Reason, idem(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		created(c, rep, toParam(p))
	}
}

func (h Handler) listParams(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ps, err := h.Svc.Params(c.Request.Context(), kind)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		items := make([]paramDTO, 0, len(ps))
		for _, p := range ps {
			items = append(items, toParam(p))
		}
		def := domain.DefaultTolerance()
		c.JSON(http.StatusOK, gin.H{"items": items, "provisional_default": gin.H{"label": domain.ProvisionalLabel,
			"note": "Chưa có phiên bản duyệt có hiệu lực → dùng mặc định tạm (prototype, chờ khách C-4)",
			"commercial_flag_pct": def.CommercialFlag.StringFixed(2), "commercial_critical_pct": def.CommercialCrit.StringFixed(2)}})
	}
}

func (h Handler) rebuild(c *gin.Context) {
	n, err := h.Svc.RebuildProjections(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, n)
}
