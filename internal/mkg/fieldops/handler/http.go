// Package handler: HTTP của mkg/fieldops trên API nội bộ MKG (cmd/api-internal,
// contracts/openapi/mkg-internal.yaml). Mọi route ở bề mặt ứng dụng: app hiện
// trường MKG (field_collector) và giám sát (field_supervisor).
package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// DeviceHeader là header mã thiết bị của app hiện trường (như hợp đồng đồng bộ nền tảng).
const DeviceHeader = "X-Device-Id"

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm /v1 (đã có xác thực + Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/fieldops/orders", h.listOrders)
	rg.POST("/fieldops/orders", h.createOrder)
	rg.GET("/fieldops/orders/:id", h.getOrder)
	rg.POST("/fieldops/orders/:id/revisions", h.reviseOrder)
	rg.POST("/fieldops/orders/:id/decision", h.decide)
	rg.POST("/fieldops/orders/:id/assign", h.assign)
	rg.POST("/fieldops/orders/:id/close", h.close)
	rg.POST("/fieldops/orders/:id/cancel", h.cancel)
	rg.GET("/fieldops/orders/:id/roll-counts", h.counts)
	rg.POST("/fieldops/orders/:id/roll-counts", h.recordCount)
	rg.POST("/fieldops/roll-counts/:id/corrections", h.correctCount)
	rg.GET("/fieldops/orders/:id/htx-confirmations", h.confirmations)
	rg.POST("/fieldops/orders/:id/htx-confirmations", h.confirm)
	rg.GET("/fieldops/orders/:id/crew-shifts", h.shifts)
	rg.POST("/fieldops/orders/:id/crew-shifts", h.recordShift)
	rg.GET("/fieldops/orders/:id/publications", h.publications)
	rg.GET("/fieldops/reconciliation", h.reconciliation)
	rg.GET("/fieldops/htx/:id/kg-per-roll", h.kgPerRoll)
	rg.POST("/fieldops/kg-per-roll/refresh", h.refreshWeighings)
	rg.POST("/fieldops/sync/push", h.push)
	rg.GET("/fieldops/sync/pull", h.pull)
	h.registerCrews(rg)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ; khối lượng là chuỗi thập phân kg, ví dụ \"1520.500\""))
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

func (x *v) time(field, s string) time.Time {
	if s == "" {
		x.add(field, "bắt buộc, RFC 3339 có múi giờ")
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

func (x *v) kgPtr(field string, s *string) *mass.Kg {
	if s == nil {
		return nil
	}
	k, err := mass.ParseKg(*s)
	if err != nil {
		x.add(field, "khối lượng kg dạng chuỗi thập phân, tối đa 3 chữ số")
		return nil
	}
	return &k
}

func (x *v) fail(c *gin.Context) bool {
	if x.bad {
		httpx.Fail(c, x.p)
	}
	return x.bad
}

func encodeCursor(parts ...uuid.UUID) string {
	ss := make([]string, len(parts))
	for i, p := range parts {
		ss[i] = p.String()
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(ss, ",")))
}

func decodeCursor(c *gin.Context, n int) ([]uuid.UUID, bool) {
	out := make([]uuid.UUID, n)
	s := c.Query("cursor")
	if s == "" {
		return out, true
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	parts := strings.Split(string(b), ",")
	if err != nil || len(parts) != n {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return nil, false
	}
	for i, p := range parts {
		if out[i], err = uuid.Parse(p); err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return nil, false
		}
	}
	return out, true
}

func limit(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

// ---------------------------------------------------------------------------
// Lệnh gom
// ---------------------------------------------------------------------------

type planReq struct {
	HTXRef      string   `json:"htx_ref"`
	CommuneCode string   `json:"commune_code"`
	WindowFrom  string   `json:"window_from"`
	WindowTo    string   `json:"window_to"`
	PlotRefs    []string `json:"plot_refs"`
}

func (h Handler) plan(c *gin.Context) (app.PlanInput, bool) {
	var req planReq
	if !bind(c, &req) {
		return app.PlanInput{}, false
	}
	x := newV()
	in := app.PlanInput{CommuneCode: req.CommuneCode, WindowFrom: req.WindowFrom, WindowTo: req.WindowTo, IdemKey: idem(c)}
	in.HTXRef = x.uuid("htx_ref", req.HTXRef)
	for _, p := range req.PlotRefs {
		in.PlotRefs = append(in.PlotRefs, x.uuid("plot_refs", p))
	}
	return in, !x.fail(c)
}

func (h Handler) createOrder(c *gin.Context) {
	in, ok := h.plan(c)
	if !ok {
		return
	}
	r, replayed, err := h.Svc.CreateOrder(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/fieldops/orders/"+r.Order.OrderID.String(), toOrderResult(r))
}

func (h Handler) reviseOrder(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	in, ok := h.plan(c)
	if !ok {
		return
	}
	h.orderResult(c, func() (app.OrderResult, bool, error) { return h.Svc.ReviseOrder(c.Request.Context(), id, in) })
}

func (h Handler) orderResult(c *gin.Context, fn func() (app.OrderResult, bool, error)) {
	r, replayed, err := fn()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toOrderResult(r))
}

type decideReq struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	CrewRef  string `json:"crew_ref"`
}

func (h Handler) decide(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	var req decideReq
	if !bind(c, &req) {
		return
	}
	if req.Decision != "approved" && req.Decision != "rejected" {
		httpx.Fail(c, problem.Validation.WithField("decision", "approved | rejected"))
		return
	}
	h.orderResult(c, func() (app.OrderResult, bool, error) {
		return h.Svc.Decide(c.Request.Context(), id, req.Decision == "approved", req.Reason, idem(c))
	})
}

func (h Handler) close(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	h.orderResult(c, func() (app.OrderResult, bool, error) { return h.Svc.Close(c.Request.Context(), id, idem(c)) })
}

func (h Handler) cancel(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	var req decideReq
	if !bind(c, &req) {
		return
	}
	h.orderResult(c, func() (app.OrderResult, bool, error) {
		return h.Svc.Cancel(c.Request.Context(), id, req.Reason, idem(c))
	})
}

func (h Handler) getOrder(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	vw, err := h.Svc.Order(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	evs := make([]orderEventDTO, 0, len(vw.History))
	for _, e := range vw.History {
		evs = append(evs, toOrderEvent(e))
	}
	o := toOrder(vw.Order)
	o.BalingOverdue = vw.BalingAlert
	var asg *assignmentDTO
	if vw.Assignment != nil {
		asg = toAssignment(*vw.Assignment)
	}
	c.JSON(http.StatusOK, gin.H{"order": o, "history": evs, "assignment": asg, "baling": toBalings(vw.Baling),
		"baling_deadline_days": vw.BalingDays})
}

func (h Handler) listOrders(c *gin.Context) {
	cur, ok := decodeCursor(c, 1)
	if !ok {
		return
	}
	x := newV()
	var htx *uuid.UUID
	if s := c.Query("htx_ref"); s != "" {
		htx = x.uuidPtr("htx_ref", &s)
	}
	if x.fail(c) {
		return
	}
	pg, err := h.Svc.Orders(c.Request.Context(), c.Query("status"), htx, cur[0], limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]orderDTO, 0, len(pg.Items))
	for _, o := range pg.Items {
		d := toOrder(o)
		d.BalingOverdue = pg.Overdue[o.OrderID]
		items = append(items, d)
	}
	var next *string
	if pg.Next != nil {
		s := encodeCursor(*pg.Next)
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

// ---------------------------------------------------------------------------
// Đếm cuộn
// ---------------------------------------------------------------------------

type countReq struct {
	ID                 *string `json:"id"`
	PlotRef            string  `json:"plot_ref"`
	Stage              string  `json:"stage"`
	Rolls              *int    `json:"rolls"`
	LooseKg            *string `json:"loose_kg"`
	DiscrepancyReason  string  `json:"discrepancy_reason"`
	HTXConfirmationRef *string `json:"htx_confirmation_ref"`
	CorrectionReason   string  `json:"correction_reason"`
	EventTime          string  `json:"event_time"`
	DeviceID           *string `json:"device_id"`
	// GPS (G3-2, tùy chọn, không bao giờ chặn lưu): {lat, lng|lon, accuracy_m} + trạng thái máy báo.
	GPS       json.RawMessage `json:"gps"`
	GPSStatus string          `json:"gps_status"`
	// VesselCode (G4, tùy chọn): số hiệu ghe/sà lan khi xếp ghe (boat_load) — proto FM-05.
	VesselCode *string `json:"vessel_code"`
}

func (h Handler) countInput(c *gin.Context, correction bool) (app.CountInput, bool) {
	var req countReq
	if !bind(c, &req) {
		return app.CountInput{}, false
	}
	x := newV()
	in := app.CountInput{Stage: req.Stage, CorrectionReason: req.CorrectionReason, DeviceID: req.DeviceID, IdemKey: idem(c)}
	in.ID = x.uuidPtr("id", req.ID)
	if !correction {
		in.PlotRef = x.uuid("plot_ref", req.PlotRef)
	}
	if req.Rolls == nil {
		x.add("rolls", "bắt buộc số cuộn (proto FM-09); rơm rời: rolls = 0 kèm loose_kg")
	} else {
		in.Count.Rolls = *req.Rolls
	}
	in.Count.LooseKg = x.kgPtr("loose_kg", req.LooseKg)
	in.Count.DiscrepancyReason = req.DiscrepancyReason
	in.HTXConfirmationRef = x.uuidPtr("htx_confirmation_ref", req.HTXConfirmationRef)
	in.EventTime = x.time("event_time", req.EventTime)
	in.Location = domain.ParseLocation(req.GPSStatus, req.GPS)
	in.VesselCode = req.VesselCode
	return in, !x.fail(c)
}

func (h Handler) recordCount(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	in, ok := h.countInput(c, false)
	if !ok {
		return
	}
	in.OrderID = id
	r, replayed, err := h.Svc.RecordCount(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toCountResult(r))
}

func (h Handler) correctCount(c *gin.Context) {
	id, ok := pathID(c, app.ErrRollCountNotFound)
	if !ok {
		return
	}
	in, ok := h.countInput(c, true)
	if !ok {
		return
	}
	in.Supersedes = &id
	r, replayed, err := h.Svc.RecordCount(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toCountResult(r))
}

func (h Handler) counts(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	vw, err := h.Svc.Counts(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	counts := make([]rollDTO, 0, len(vw.Counts))
	for _, r := range vw.Counts {
		d := toRoll(r)
		if hc, ok := vw.Harvest[r.ID]; ok {
			d.Harvest = toHarvestCheck(&hc)
		}
		if v, ok := vw.Vessels[r.ID]; ok {
			d.VesselCode = &v
		}
		if l, ok := vw.Locations[r.ID]; ok {
			d.GPSStatus = &l.Status
			if l.Lat != nil {
				d.GPS = &gpsDTO{Lat: *l.Lat, Lng: *l.Lng, AccuracyM: l.AccuracyM}
			}
		}
		counts = append(counts, d)
	}
	totals := make([]totalDTO, 0, len(vw.Totals))
	perStage := map[string]int{}
	for _, t := range vw.Totals {
		totals = append(totals, toTotal(t))
		perStage[t.Stage] += t.Rolls
	}
	c.JSON(http.StatusOK, gin.H{"counts": counts, "totals": totals, "order_totals": perStage})
}

// ---------------------------------------------------------------------------
// Xác nhận HTX, đối soát
// ---------------------------------------------------------------------------

type confirmReq struct {
	PlotRef           string  `json:"plot_ref"`
	Outcome           string  `json:"outcome"`
	HTXRolls          *int    `json:"htx_rolls"`
	Reason            string  `json:"reason"`
	StatementRevision *int    `json:"statement_revision"`
	EvidenceRef       *string `json:"evidence_ref"`
	EventTime         string  `json:"event_time"`
}

func (h Handler) confirm(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	var req confirmReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.ConfirmInput{OrderID: id, Outcome: req.Outcome, HTXRolls: req.HTXRolls, Reason: req.Reason,
		StatementRevision: req.StatementRevision, IdemKey: idem(c)}
	in.PlotRef = x.uuid("plot_ref", req.PlotRef)
	in.EvidenceRef = x.uuidPtr("evidence_ref", req.EvidenceRef)
	in.EventTime = x.time("event_time", req.EventTime)
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.RecordConfirmation(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", gin.H{"confirmation": toConfirm(r.Confirmation), "reconciliation": toRecon(r.Recon)})
}

func (h Handler) confirmations(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	cs, err := h.Svc.Confirmations(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]confirmDTO, 0, len(cs))
	for _, x := range cs {
		items = append(items, toConfirm(x))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) reconciliation(c *gin.Context) {
	cur, ok := decodeCursor(c, 2)
	if !ok {
		return
	}
	x := newV()
	var order *uuid.UUID
	if s := c.Query("order_id"); s != "" {
		order = x.uuidPtr("order_id", &s)
	}
	if x.fail(c) {
		return
	}
	pg, err := h.Svc.Reconciliation(c.Request.Context(), order, c.Query("status"), [2]uuid.UUID{cur[0], cur[1]}, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]reconDTO, 0, len(pg.Items))
	counts := map[string]int{domain.ReconPending: 0, domain.ReconMatch: 0, domain.ReconMismatch: 0}
	for _, r := range pg.Items {
		items = append(items, toRecon(r))
		counts[r.Status]++
	}
	var next *string
	if pg.Next != nil {
		s := encodeCursor(pg.Next[0], pg.Next[1])
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page_counts": counts, "next_cursor": next})
}

// ---------------------------------------------------------------------------
// Ca tổ gom, van, kg/cuộn
// ---------------------------------------------------------------------------

type shiftReq struct {
	CrewRef       string   `json:"crew_ref"`
	Kind          string   `json:"kind"`
	Headcount     *int     `json:"headcount"`
	EquipmentRefs []string `json:"equipment_refs"`
	EventTime     string   `json:"event_time"`
	DeviceID      *string  `json:"device_id"`
}

func (h Handler) recordShift(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	var req shiftReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.ShiftInput{OrderID: id, Kind: req.Kind, Headcount: req.Headcount, EquipmentRefs: req.EquipmentRefs,
		DeviceID: req.DeviceID, IdemKey: idem(c)}
	in.CrewRef = x.uuid("crew_ref", req.CrewRef)
	in.EventTime = x.time("event_time", req.EventTime)
	if x.fail(c) {
		return
	}
	s, replayed, err := h.Svc.RecordShift(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toShift(s))
}

func (h Handler) shifts(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	ss, err := h.Svc.Shifts(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]shiftDTO, 0, len(ss))
	for _, s := range ss {
		items = append(items, toShift(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) publications(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	xs, err := h.Svc.Publications(c.Request.Context(), id, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toExports(xs)})
}

func (h Handler) kgPerRoll(c *gin.Context) {
	id, ok := pathID(c, problem.NotFound)
	if !ok {
		return
	}
	r, err := h.Svc.KgPerRoll(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"htx_ref": id, "kg_per_roll": r.Kg.StringFixed(1), "source": r.Source, "samples": r.Samples,
		"rule_version": domain.RuleKgPerRoll})
}

func (h Handler) refreshWeighings(c *gin.Context) {
	n, err := h.Svc.RefreshWeighings(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"weighed_lots_added": n})
}

// ---------------------------------------------------------------------------
// Đồng bộ ngoại tuyến (app hiện trường MKG)
// ---------------------------------------------------------------------------

func (h Handler) push(c *gin.Context) {
	var b app.SyncBatch
	if !bind(c, &b) {
		return
	}
	res, err := h.Svc.Push(c.Request.Context(), c.GetHeader(DeviceHeader), b)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": res})
}

func (h Handler) pull(c *gin.Context) {
	pg, err := h.Svc.Pull(c.Request.Context(), c.GetHeader(DeviceHeader), c.Query("cursor"), limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pg)
}
