// Package handler: HTTP của mkg/costing trên API nội bộ MKG (cmd/api-internal,
// contracts/openapi/mkg-internal.yaml, tag costing). Duyệt tham số, duyệt quyết toán, dựng lại
// bảng chiếu ở bề mặt quản trị (MFA); còn lại ở bề mặt ứng dụng. Tiền / khối lượng là chuỗi (R-10).
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

const base = "/costing"

// RegisterApp gắn route vận hành.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET(base+"/lots/:id/cost", h.lotCost)
	rg.GET(base+"/lots/:id/entries", h.lotEntries)
	rg.POST(base+"/entries", h.manualEntry)
	rg.POST(base+"/entries/:id/adjust", h.adjustEntry)
	rg.GET(base+"/allocations/:id", h.allocation)
	rg.POST(base+"/allocations/:id/recompute", h.recompute)
	rg.GET(base+"/sources/:id/allocations", h.sourceAllocations)
	rg.GET(base+"/collections/:id/conversion", h.conversion)
	rg.GET(base+"/periods", h.periods)
	rg.POST(base+"/periods", h.openPeriod)
	rg.GET(base+"/periods/:id", h.period)
	rg.POST(base+"/periods/:id/close", h.closePeriod)
	rg.GET(base+"/period-close-jobs/:id", h.job)
	rg.GET(base+"/settlements", h.settlements)
	rg.POST(base+"/settlements", h.createSettlement)
	rg.GET(base+"/settlements/:id", h.settlement)
	rg.POST(base+"/settlements/:id/price", h.priceSettlement)
	rg.POST(base+"/settlements/:id/pay", h.paySettlement)
	rg.POST(base+"/settlements/:id/cancel", h.cancelSettlement)
	rg.POST(base+"/settlements/:id/adjust", h.adjustSettlement)
	rg.POST(base+"/carbon-revenue", h.carbon)
	rg.GET(base+"/params", h.params)
}

// RegisterAdmin gắn route phê duyệt / quản trị (MFA).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET(base+"/params", h.params)
	rg.POST(base+"/params", h.proposeParam)
	rg.POST(base+"/params/:key/versions/:seq/decision", h.decideParam)
	rg.POST(base+"/settlements/:id/approve", h.approveSettlement)
	rg.GET(base+"/settlements/:id", h.settlement)
	rg.POST(base+"/projections/rebuild", h.rebuild)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ; tiền / khối lượng là chuỗi thập phân, ví dụ \"1250000.50\""))
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

func parseTime(field, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return t, problem.Validation.WithField(field, "RFC 3339 có múi giờ")
	}
	return t, nil
}

func parseAmount(field, s string) (money.Amount, error) {
	a, err := money.ParseAmount(s)
	if err != nil {
		return a, problem.Validation.WithField(field, "chuỗi thập phân tối đa 2 chữ số")
	}
	return a, nil
}

func parseKgPtr(field string, s *string) (*mass.Kg, error) {
	if s == nil {
		return nil, nil
	}
	k, err := mass.ParseKg(*s)
	if err != nil || k.NonNegative() != nil {
		return nil, problem.Validation.WithField(field, "chuỗi kg ≥ 0, tối đa 3 chữ số")
	}
	return &k, nil
}

func parseUUIDPtr(field string, s *string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "uuid")
	}
	return &id, nil
}

// ---------------------------------------------------------------------------
// Giá thành lô, bút toán
// ---------------------------------------------------------------------------

func (h Handler) lotCost(c *gin.Context) {
	id, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.LotCost(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	cats := make([]gin.H, 0, len(v.Categories))
	for _, x := range v.Categories {
		cats = append(cats, gin.H{"category": x.Category, "amount": x.Amount, "mass_kg": x.MassKg, "entries": x.Entries})
	}
	body := gin.H{"lot_ref": v.LotRef, "categories": cats, "total_cost": v.TotalCost, "revenue": v.Revenue, "delivered_kg": v.DeliveredKg,
		"cost_per_tonne": v.CostPerTonne, "cost_per_tonne_undetermined": v.CostPerTonne == nil,
		"note": "Giá thành không gồm giá mua rơm (quyết toán HTX, tính riêng) và khấu hao (proto buildCostChain)"}
	c.JSON(http.StatusOK, body)
}

func entryDTO(e store.Entry) gin.H {
	return gin.H{"id": e.ID, "lot_ref": e.LotRef, "period_code": e.PeriodCode, "late_from": e.LateFrom, "category": e.Category,
		"amount": e.Amount, "delta_amount": e.DeltaAmount, "mass_kg": e.MassKg, "delta_mass_kg": e.DeltaMassKg,
		"dimension_kind": e.DimensionKind, "dimension_ref": e.DimensionRef, "source_kind": e.SourceKind, "source_ref": e.SourceRef,
		"allocation_ref": e.AllocationRef, "entry_kind": e.EntryKind, "note": e.Note, "event_time": e.EventTime, "recorded_at": e.RecordedAt,
		"supersedes_id": e.SupersedesID, "correction_reason": e.CorrectionReason, "actor_ref": e.ActorRef, "rule_version": e.RuleVersion,
		"seq": e.Seq}
}

func (h Handler) lotEntries(c *gin.Context) {
	id, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(c.Query("after_seq"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	es, err := h.Svc.LotEntries(c.Request.Context(), id, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(es))
	for _, e := range es {
		items = append(items, entryDTO(e))
	}
	var next *int64
	if len(es) > 0 && (limit <= 0 || len(es) >= limit) {
		n := es[len(es)-1].Seq
		next = &n
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_after_seq": next})
}

type manualEntryReq struct {
	LotRef        string  `json:"lot_ref"`
	Category      string  `json:"category"`
	Amount        string  `json:"amount"`
	MassKg        *string `json:"mass_kg"`
	DimensionKind string  `json:"dimension_kind"`
	DimensionRef  *string `json:"dimension_ref"`
	EventTime     string  `json:"event_time"`
	Note          string  `json:"note"`
}

func (h Handler) manualEntry(c *gin.Context) {
	var r manualEntryReq
	if !bind(c, &r) {
		return
	}
	lot, err := uuid.Parse(r.LotRef)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("lot_ref", "uuid"))
		return
	}
	amt, err := parseAmount("amount", r.Amount)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	kg, err := parseKgPtr("mass_kg", r.MassKg)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	dim, err := parseUUIDPtr("dimension_ref", r.DimensionRef)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	at, err := parseTime("event_time", r.EventTime)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	e, replayed, err := h.Svc.ManualEntry(c.Request.Context(), app.ManualEntryInput{LotRef: lot, Category: port.CostCategory(r.Category),
		Amount: amt, MassKg: kg, DimensionKind: r.DimensionKind, DimensionRef: dim, EventTime: at, Note: r.Note, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, entryDTO(e))
}

type adjustReq struct {
	Amount string  `json:"amount"`
	MassKg *string `json:"mass_kg"`
	Reason string  `json:"reason"`
}

func (h Handler) adjustEntry(c *gin.Context) {
	id, ok := pathID(c, app.ErrEntryNotFound)
	if !ok {
		return
	}
	var r adjustReq
	if !bind(c, &r) {
		return
	}
	amt, err := parseAmount("amount", r.Amount)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	kg, err := parseKgPtr("mass_kg", r.MassKg)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	e, replayed, err := h.Svc.AdjustEntry(c.Request.Context(), id, amt, kg, r.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, entryDTO(e))
}

// ---------------------------------------------------------------------------
// Kết quả phân bổ, quy đổi
// ---------------------------------------------------------------------------

func allocDTO(a store.Allocation) gin.H {
	return gin.H{"id": a.ID, "period_ref": a.PeriodRef, "kind": a.Kind, "source_ref": a.SourceRef, "source_event_time": a.SourceEventTime,
		"basis": a.Basis, "total_amount": a.TotalAmount, "total_kg": a.TotalKg, "lines": json.RawMessage(a.Lines),
		"inputs": json.RawMessage(a.Inputs), "inputs_sha256": a.InputsSHA256, "result_sha256": a.ResultSHA256, "param_pins": a.Pins,
		"params_as_of": a.ParamsAsOf, "rule_version": a.RuleVersion, "supersedes_id": a.SupersedesID, "superseded_by": a.SupersededBy,
		"correction_reason": a.CorrectionReason, "recorded_at": a.RecordedAt}
}

func (h Handler) allocation(c *gin.Context) {
	id, ok := pathID(c, app.ErrAllocationNotFound)
	if !ok {
		return
	}
	a, err := h.Svc.Allocation(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, allocDTO(a))
}

func (h Handler) sourceAllocations(c *gin.Context) {
	id, ok := pathID(c, app.ErrAllocationNotFound)
	if !ok {
		return
	}
	as, err := h.Svc.AllocationsForSource(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(as))
	for _, a := range as {
		items = append(items, allocDTO(a))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type recomputeReq struct {
	AsOf   string `json:"as_of"`
	Reason string `json:"reason"`
}

func (h Handler) recompute(c *gin.Context) {
	id, ok := pathID(c, app.ErrAllocationNotFound)
	if !ok {
		return
	}
	var r recomputeReq
	if !bind(c, &r) {
		return
	}
	var asOf *time.Time
	if r.AsOf != "" {
		t, err := parseTime("as_of", r.AsOf)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		asOf = &t
	}
	a, changed, replayed, err := h.Svc.Recompute(c.Request.Context(), id, asOf, r.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	body := allocDTO(a)
	body["changed"] = changed
	if !changed {
		c.JSON(http.StatusOK, body)
		return
	}
	created(c, replayed, body)
}

func (h Handler) conversion(c *gin.Context) {
	id, ok := pathID(c, app.ErrConversionNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Conversion(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": v.ID, "collection_order_ref": v.OrderRef, "rolls": v.Rolls, "weighed_kg": v.WeighedKg,
		"weigh_point": v.WeighPoint, "yield_kg_per_roll": v.YieldKgPerRoll, "moisture_rule": v.MoistureRule, "converted_kg": v.ConvertedKg,
		"inputs_sha256": v.InputsSHA256, "param_pins": v.Pins, "rule_version": v.RuleVersion, "supersedes_id": v.SupersedesID,
		"event_time": v.EventTime, "recorded_at": v.RecordedAt})
}

// ---------------------------------------------------------------------------
// Kỳ, đóng kỳ
// ---------------------------------------------------------------------------

func periodDTO(p store.Period) gin.H {
	return gin.H{"id": p.ID, "code": p.Code, "from": p.From, "to": p.To, "status": p.Status, "opened_at": p.OpenedAt,
		"close_run_id": p.CloseRunID, "closed_by": p.ClosedBy, "closed_at": p.ClosedAt, "journal_ref": p.JournalRef, "posted_at": p.PostedAt}
}

func (h Handler) periods(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	ps, err := h.Svc.Periods(c.Request.Context(), c.Query("before"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(ps))
	for _, p := range ps {
		items = append(items, periodDTO(p))
	}
	var next *string
	if len(ps) > 0 && (limit <= 0 || len(ps) >= limit) {
		n := ps[len(ps)-1].Code
		next = &n
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_before": next})
}

func (h Handler) openPeriod(c *gin.Context) {
	var r struct {
		Code string `json:"code"`
	}
	if !bind(c, &r) {
		return
	}
	p, err := h.Svc.OpenPeriod(c.Request.Context(), r.Code)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, periodDTO(p))
}

func summaryDTO(s *store.Summary) gin.H {
	if s == nil {
		return nil
	}
	return gin.H{"close_run_id": s.CloseRunID, "lines": s.Lines, "line_count": len(s.Lines), "input_snapshot_sha256": s.InputSnapshotSHA256,
		"result_set_sha256": s.ResultSetSHA256, "closed_by": s.ClosedBy, "closed_at": s.EventTime, "rule_version": s.RuleVersion}
}

func (h Handler) period(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	p, sum, err := h.Svc.Period(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	body := periodDTO(p)
	body["summary"] = summaryDTO(sum)
	c.JSON(http.StatusOK, body)
}

func jobDTO(j store.CloseJob) gin.H {
	return gin.H{"job_id": j.ID, "kind": port.JobPeriodClose, "period_id": j.PeriodID, "period_code": j.PeriodCode, "status": j.Status,
		"requested_by": j.RequestedBy, "requested_at": j.RequestedAt, "as_of": j.AsOf, "input_snapshot_sha256": j.SnapshotSHA256,
		"snapshot_counts": gin.H{"entries": len(j.Snapshot.Entries), "settlements": len(j.Snapshot.Settlements), "carbon": len(j.Snapshot.Carbon)},
		"error_code":      j.ErrorCode, "completed_at": j.CompletedAt}
}

func (h Handler) closePeriod(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	j, _, err := h.Svc.RequestClose(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/costing/period-close-jobs/"+j.ID.String())
	c.JSON(http.StatusAccepted, jobDTO(j))
}

func (h Handler) job(c *gin.Context) {
	id, ok := pathID(c, app.ErrJobNotFound)
	if !ok {
		return
	}
	j, err := h.Svc.Job(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, jobDTO(j))
}

// ---------------------------------------------------------------------------
// Quyết toán HTX
// ---------------------------------------------------------------------------

func settlementDTO(s store.Settlement) gin.H {
	var price *string
	if s.UnitPrice != nil {
		p := s.UnitPrice.String()
		price = &p
	}
	return gin.H{"id": s.ID, "kind": s.Kind, "status": s.Status, "collection_order_ref": s.CollectionOrderRef, "htx_ref": s.HTXRef,
		"harvest_date": s.HarvestDate, "contract_ref": s.ContractRef, "contract_version": s.ContractVersion, "basis": s.Basis,
		"price_source": s.PriceSource, "unit_price": price, "rolls": s.Rolls, "mass_kg": s.MassKg, "gross_amount": s.GrossAmount,
		"deductions": s.Deductions, "amount": s.Amount, "blocked_reason": s.BlockedReason, "due_on": s.DueOn, "period_ref": s.PeriodRef,
		"prepared_by": s.PreparedBy, "approved_by": s.ApprovedBy, "approved_at": s.ApprovedAt, "supersedes_id": s.SupersedesID,
		"rule_version": s.RuleVersion}
}

func (h Handler) settlements(c *gin.Context) {
	order, err := parseUUIDPtr("collection_order_ref", strPtrQ(c, "collection_order_ref"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	before, err := parseUUIDPtr("before", strPtrQ(c, "before"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	ss, err := h.Svc.Settlements(c.Request.Context(), c.Query("status"), order, before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(ss))
	for _, s := range ss {
		items = append(items, settlementDTO(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func strPtrQ(c *gin.Context, k string) *string {
	if v := c.Query(k); v != "" {
		return &v
	}
	return nil
}

type createSettlementReq struct {
	CollectionOrderRef string `json:"collection_order_ref"`
	HTXRef             string `json:"htx_ref"`
	HarvestDate        string `json:"harvest_date"`
	Deductions         string `json:"deductions"`
	Reason             string `json:"reason"`
}

func (h Handler) createSettlement(c *gin.Context) {
	var r createSettlementReq
	if !bind(c, &r) {
		return
	}
	order, err1 := uuid.Parse(r.CollectionOrderRef)
	htx, err2 := uuid.Parse(r.HTXRef)
	if err1 != nil || err2 != nil {
		httpx.Fail(c, problem.Validation.WithField("collection_order_ref", "uuid").WithField("htx_ref", "uuid"))
		return
	}
	if r.Deductions == "" {
		r.Deductions = "0"
	}
	ded, err := parseAmount("deductions", r.Deductions)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	s, replayed, err := h.Svc.CreateSettlement(c.Request.Context(), app.CreateSettlementInput{CollectionOrderRef: order, HTXRef: htx,
		HarvestDate: r.HarvestDate, Deductions: ded, Reason: r.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, settlementDTO(s))
}

func (h Handler) settlement(c *gin.Context) {
	id, ok := pathID(c, app.ErrSettlementNotFound)
	if !ok {
		return
	}
	s, err := h.Svc.Settlement(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, settlementDTO(s))
}

func (h Handler) priceSettlement(c *gin.Context) {
	id, ok := pathID(c, app.ErrSettlementNotFound)
	if !ok {
		return
	}
	var r struct {
		Basis     string `json:"basis"`
		UnitPrice string `json:"unit_price"`
		Reason    string `json:"reason"`
	}
	if !bind(c, &r) {
		return
	}
	price, err := money.ParsePrice(r.UnitPrice)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("unit_price", "chuỗi thập phân tối đa 4 chữ số"))
		return
	}
	s, replayed, err := h.Svc.PriceSettlement(c.Request.Context(), app.PriceSettlementInput{ID: id, Basis: r.Basis, UnitPrice: price,
		Reason: r.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, settlementDTO(s))
}

func (h Handler) approveSettlement(c *gin.Context) {
	id, ok := pathID(c, app.ErrSettlementNotFound)
	if !ok {
		return
	}
	s, replayed, err := h.Svc.ApproveSettlement(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, settlementDTO(s))
}

func (h Handler) paySettlement(c *gin.Context) {
	id, ok := pathID(c, app.ErrSettlementNotFound)
	if !ok {
		return
	}
	s, replayed, err := h.Svc.PaySettlement(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, settlementDTO(s))
}

func (h Handler) cancelSettlement(c *gin.Context) {
	id, ok := pathID(c, app.ErrSettlementNotFound)
	if !ok {
		return
	}
	var r struct {
		Reason string `json:"reason"`
	}
	if !bind(c, &r) {
		return
	}
	s, replayed, err := h.Svc.CancelSettlement(c.Request.Context(), id, r.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, settlementDTO(s))
}

func (h Handler) adjustSettlement(c *gin.Context) {
	id, ok := pathID(c, app.ErrSettlementNotFound)
	if !ok {
		return
	}
	var r struct {
		Amount string `json:"amount"`
		Reason string `json:"reason"`
	}
	if !bind(c, &r) {
		return
	}
	amt, err := parseAmount("amount", r.Amount)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	s, replayed, err := h.Svc.AdjustSettlement(c.Request.Context(), id, amt, r.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, settlementDTO(s))
}

// ---------------------------------------------------------------------------
// Doanh thu carbon, tham số, dựng lại
// ---------------------------------------------------------------------------

func (h Handler) carbon(c *gin.Context) {
	var r struct {
		Amount    string `json:"amount"`
		EventTime string `json:"event_time"`
		Reference string `json:"reference"`
	}
	if !bind(c, &r) {
		return
	}
	amt, err := parseAmount("amount", r.Amount)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	at, err := parseTime("event_time", r.EventTime)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	x, replayed, err := h.Svc.RecordCarbonRevenue(c.Request.Context(), app.CarbonInput{Amount: amt, EventTime: at, Reference: r.Reference,
		IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"id": x.ID, "period_ref": x.PeriodRef, "period_code": x.PeriodCode, "revenue_amount": x.Revenue,
		"retained_amount": x.Split.Retained, "farmers_amount": x.Split.Farmers, "authority_amount": x.Split.Authority,
		"verifier_amount": x.Split.Verifier, "retained_pct": x.Split.RetainedPct.StringFixed(2), "farmers_pct": x.Split.FarmersPct.StringFixed(2),
		"authority_pct": x.Split.AuthorityPct.StringFixed(2), "verifier_pct": x.Split.VerifierPct.StringFixed(2), "param_pins": x.Pins,
		"label": domain.ProvisionalLabel, "rule_version": domain.RuleCarbonSplit, "event_time": x.EventTime,
		"note": "Chỉ chia doanh thu carbon đã ghi; không tính tCO2e (pf/mrv, chờ C-1)"})
}

func paramVersionDTO(v store.ParamVersion) gin.H {
	return gin.H{"key": v.Key, "seq": v.Seq, "value": v.Value, "reason": v.Reason, "effective_from": v.EffectiveFrom,
		"proposed_by": v.ProposedBy, "recorded_at": v.RecordedAt, "decision": v.Decision, "decided_by": v.DecidedBy,
		"decided_at": v.DecidedAt, "decision_reason": v.DecisionNote}
}

func (h Handler) params(c *gin.Context) {
	ps, err := h.Svc.ParamsView(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(ps))
	for _, p := range ps {
		vs := make([]gin.H, 0, len(p.Versions))
		for _, v := range p.Versions {
			vs = append(vs, paramVersionDTO(v))
		}
		active := gin.H{"value": p.Active.Value, "seq": p.Active.Seq, "pinned": p.Active.Pinned()}
		if p.Active.Seq == 0 {
			active["label"] = domain.ProvisionalLabel
		}
		items = append(items, gin.H{"key": p.Def.Key, "default": p.Def.Default, "default_source": p.Def.Source,
			"default_label": domain.ProvisionalLabel, "active": active, "versions": vs})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) proposeParam(c *gin.Context) {
	var r struct {
		Key           string `json:"key"`
		Value         string `json:"value"`
		Reason        string `json:"reason"`
		EffectiveFrom string `json:"effective_from"`
	}
	if !bind(c, &r) {
		return
	}
	eff, err := parseTime("effective_from", r.EffectiveFrom)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, replayed, err := h.Svc.ProposeParam(c.Request.Context(), app.ProposeParamInput{Key: r.Key, Value: r.Value, Reason: r.Reason,
		EffectiveFrom: eff, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, paramVersionDTO(v))
}

func (h Handler) decideParam(c *gin.Context) {
	seq, err := strconv.ParseInt(c.Param("seq"), 10, 64)
	if err != nil || seq < 1 {
		httpx.Fail(c, app.ErrParamVersionNotFound)
		return
	}
	var r struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !bind(c, &r) {
		return
	}
	v, replayed, err := h.Svc.DecideParam(c.Request.Context(), c.Param("key"), seq, r.Decision, r.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, paramVersionDTO(v))
}

func (h Handler) rebuild(c *gin.Context) {
	n, err := h.Svc.RebuildProjections(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, n)
}
