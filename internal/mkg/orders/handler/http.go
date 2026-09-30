// Package handler: HTTP của mkg/orders trên API nội bộ MKG (cmd/api-internal,
// contracts/openapi/mkg-internal.yaml, tag orders). Bề mặt ứng dụng: sales_officer lập, sales_approver
// duyệt (R-56). Bề mặt quản trị (MFA): dựng lại bảng chiếu (R-22).
package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm /v1 (đã có xác thực + Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/orders/contracts", h.listContracts)
	rg.POST("/orders/contracts", h.proposeContract)
	rg.GET("/orders/contract-terms", h.contractTerms)
	rg.GET("/orders/contracts/:id", h.getContract)
	rg.POST("/orders/contracts/:id/revisions", h.reviseContract)
	rg.POST("/orders/contracts/:id/versions/:version/decision", h.decideContract)
	rg.POST("/orders/contracts/:id/terminate", h.terminateContract)

	rg.GET("/orders/purchase-orders", h.listPOs)
	rg.POST("/orders/purchase-orders", h.createPO)
	rg.GET("/orders/purchase-orders/:id", h.getPO)
	rg.POST("/orders/purchase-orders/:id/decision", h.decidePO)
	rg.POST("/orders/purchase-orders/:id/cancel", h.cancelPO)
	rg.POST("/orders/purchase-orders/:id/receipts", h.createReceipt)
	rg.GET("/orders/receipts/:id", h.getReceipt)
	rg.POST("/orders/receipts/:id/variance-report", h.varianceReport)
	rg.POST("/orders/receipts/:id/corrections", h.correctReceipt)
	rg.POST("/orders/receipts/:id/decision", h.decideReceipt)
	rg.GET("/orders/payables", h.payables)

	rg.GET("/orders/sales-orders", h.listSOs)
	rg.POST("/orders/sales-orders", h.createSO)
	rg.GET("/orders/sales-orders/:id", h.getSO)
	rg.POST("/orders/sales-orders/:id/confirm", h.confirmSO)
	rg.POST("/orders/sales-orders/:id/advance", h.advanceSO)
	rg.POST("/orders/sales-orders/:id/deliveries", h.planDelivery)
	rg.GET("/orders/deliveries/:id", h.getDelivery)
	rg.POST("/orders/deliveries/:id/dispatch", h.dispatch)
	rg.POST("/orders/deliveries/:id/accept", h.accept)
	rg.POST("/orders/deliveries/:id/reject", h.reject)
	rg.POST("/orders/deliveries/:id/cancel", h.cancelDelivery)
	rg.POST("/orders/deliveries/:id/corrections", h.correctDelivery)

	rg.GET("/orders/params/:key", h.params)
	rg.POST("/orders/params/:key/versions", h.proposeParam)
	rg.POST("/orders/params/:key/versions/:version/decision", h.decideParam)
	h.registerFiles(rg)
}

// RegisterAdmin gắn route quản trị (MFA).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/orders/projections/rebuild", h.rebuild)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ; tiền, đơn giá, khối lượng là chuỗi thập phân"))
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

func created(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func limit(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

// v gom lỗi theo trường.
type v struct {
	p   *problem.Problem
	bad bool
}

func newV() *v { return &v{p: problem.Validation} }

func (x *v) add(field, reason string) { x.p, x.bad = x.p.WithField(field, reason), true }

func (x *v) fail(c *gin.Context) bool {
	if x.bad {
		httpx.Fail(c, x.p)
	}
	return x.bad
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

func (x *v) price(field, s string) money.Price {
	p, err := money.ParsePrice(s)
	if err != nil {
		x.add(field, "đơn giá dạng chuỗi thập phân, tối đa 4 chữ số")
	}
	return p
}

func (x *v) pricePtr(field string, s *string) *money.Price {
	if s == nil || *s == "" {
		return nil
	}
	p := x.price(field, *s)
	return &p
}

func (x *v) kg(field, s string) mass.Kg {
	k, err := mass.ParseKg(s)
	if err != nil {
		x.add(field, "khối lượng kg dạng chuỗi thập phân, tối đa 3 chữ số")
	}
	return k
}

func (x *v) dec(field, s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		x.add(field, "chuỗi thập phân")
	}
	return d
}

func (x *v) decPtr(field string, s *string) *decimal.Decimal {
	if s == nil || *s == "" {
		return nil
	}
	d := x.dec(field, *s)
	return &d
}

func queryUUID(c *gin.Context, name string) (*uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải là UUID"))
		return nil, false
	}
	return &id, true
}

type decisionReq struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func decision(c *gin.Context) (bool, string, bool) {
	var req decisionReq
	if !bind(c, &req) {
		return false, "", false
	}
	switch req.Decision {
	case "approved":
		return true, req.Reason, true
	case "rejected":
		return false, req.Reason, true
	}
	httpx.Fail(c, problem.Validation.WithField("decision", "approved | rejected"))
	return false, "", false
}

type reasonReq struct {
	Reason string `json:"reason"`
}

// ---------------------------------------------------------------------------
// Hợp đồng mua rơm
// ---------------------------------------------------------------------------

type contractReq struct {
	HTXRef         string  `json:"htx_ref"`
	Basis          string  `json:"basis"`
	UnitPrice      string  `json:"unit_price"`
	CommittedKg    string  `json:"committed_kg"`
	MaxMoisturePct *string `json:"max_moisture_pct"`
	ValidFrom      string  `json:"valid_from"`
	ValidTo        string  `json:"valid_to"`
	Note           string  `json:"note"`
	Reason         string  `json:"reason"`
}

func (h Handler) contractInput(c *gin.Context, needHTX bool) (app.ContractInput, bool) {
	var req contractReq
	if !bind(c, &req) {
		return app.ContractInput{}, false
	}
	x := newV()
	in := app.ContractInput{Basis: port.PricingBasis(req.Basis), ValidFrom: req.ValidFrom, ValidTo: req.ValidTo, Note: req.Note,
		Reason: req.Reason, IdemKey: idem(c)}
	if needHTX {
		in.HTXRef = x.uuid("htx_ref", req.HTXRef)
	}
	in.UnitPrice = x.price("unit_price", req.UnitPrice)
	in.CommittedKg = x.kg("committed_kg", req.CommittedKg)
	in.MaxMoisturePct = x.decPtr("max_moisture_pct", req.MaxMoisturePct)
	return in, !x.fail(c)
}

func (h Handler) proposeContract(c *gin.Context) {
	in, ok := h.contractInput(c, true)
	if !ok {
		return
	}
	cs, replayed, err := h.Svc.ProposeContract(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"contract_id": cs[0].ContractID, "versions": contractsOf(cs)})
}

func (h Handler) reviseContract(c *gin.Context) {
	id, ok := pathID(c, app.ErrContractNotFound)
	if !ok {
		return
	}
	in, ok := h.contractInput(c, false)
	if !ok {
		return
	}
	cs, replayed, err := h.Svc.ReviseContract(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"contract_id": id, "versions": contractsOf(cs)})
}

func (h Handler) decideContract(c *gin.Context) {
	id, ok := pathID(c, app.ErrContractNotFound)
	if !ok {
		return
	}
	ver, err := strconv.Atoi(c.Param("version"))
	if err != nil || ver < 1 {
		httpx.Fail(c, app.ErrContractNotFound)
		return
	}
	approve, reason, ok := decision(c)
	if !ok {
		return
	}
	cs, replayed, err := h.Svc.DecideContract(c.Request.Context(), id, ver, approve, reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"contract_id": id, "versions": contractsOf(cs)})
}

func (h Handler) terminateContract(c *gin.Context) {
	id, ok := pathID(c, app.ErrContractNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	cs, replayed, err := h.Svc.TerminateContract(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"contract_id": id, "versions": contractsOf(cs)})
}

func (h Handler) getContract(c *gin.Context) {
	id, ok := pathID(c, app.ErrContractNotFound)
	if !ok {
		return
	}
	cs, err := h.Svc.Contract(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"contract_id": id, "versions": contractsOf(cs)})
}

func (h Handler) listContracts(c *gin.Context) {
	htx, ok := queryUUID(c, "htx_ref")
	if !ok {
		return
	}
	cs, err := h.Svc.ListContracts(c.Request.Context(), htx, c.Query("status"), limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": contractsOf(cs)})
}

func (h Handler) contractTerms(c *gin.Context) {
	htx, ok := queryUUID(c, "htx_ref")
	if !ok {
		return
	}
	if htx == nil {
		httpx.Fail(c, problem.Validation.WithField("htx_ref", "bắt buộc"))
		return
	}
	on := h.Svc.Clock.Now()
	if s := c.Query("on"); s != "" {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("on", "ngày YYYY-MM-DD"))
			return
		}
		on = d.Add(12 * time.Hour) // giữa ngày: ngày nghiệp vụ giờ VN trùng ngày yêu cầu
	}
	t, err := h.Svc.EffectiveContract(c.Request.Context(), *htx, on)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, termsOf(t))
}

// ---------------------------------------------------------------------------
// Đơn mua, phiếu nhận hàng, phải trả
// ---------------------------------------------------------------------------

type poReq struct {
	Category     string  `json:"category"`
	SupplierRef  string  `json:"supplier_ref"`
	FacilityRef  *string `json:"facility_ref"`
	Unit         string  `json:"unit"`
	OrderedQty   string  `json:"ordered_qty"`
	UnitPrice    *string `json:"unit_price"`
	ExpectedDate string  `json:"expected_date"`
	Description  string  `json:"description"`
}

func (h Handler) createPO(c *gin.Context) {
	var req poReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.POInput{Category: req.Category, Unit: req.Unit, ExpectedDate: req.ExpectedDate, Description: req.Description, IdemKey: idem(c)}
	in.SupplierRef = x.uuid("supplier_ref", req.SupplierRef)
	in.FacilityRef = x.uuidPtr("facility_ref", req.FacilityRef)
	in.OrderedQty = x.dec("ordered_qty", req.OrderedQty)
	in.UnitPrice = x.pricePtr("unit_price", req.UnitPrice)
	if x.fail(c) {
		return
	}
	po, replayed, err := h.Svc.CreatePO(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"purchase_order": poOf(po)})
}

func (h Handler) decidePO(c *gin.Context) {
	id, ok := pathID(c, app.ErrPONotFound)
	if !ok {
		return
	}
	approve, reason, ok := decision(c)
	if !ok {
		return
	}
	po, replayed, err := h.Svc.DecidePO(c.Request.Context(), id, approve, reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"purchase_order": poOf(po)})
}

func (h Handler) cancelPO(c *gin.Context) {
	id, ok := pathID(c, app.ErrPONotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	po, replayed, err := h.Svc.CancelPO(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"purchase_order": poOf(po)})
}

func (h Handler) getPO(c *gin.Context) {
	id, ok := pathID(c, app.ErrPONotFound)
	if !ok {
		return
	}
	v, err := h.Svc.PO(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d := poOf(v.PO)
	d.Receipts = make([]receiptDTO, 0, len(v.Receipts))
	for _, r := range v.Receipts {
		d.Receipts = append(d.Receipts, receiptOf(r))
	}
	c.JSON(http.StatusOK, gin.H{"purchase_order": d})
}

func (h Handler) listPOs(c *gin.Context) {
	sup, ok := queryUUID(c, "supplier_ref")
	if !ok {
		return
	}
	ps, err := h.Svc.ListPOs(c.Request.Context(), sup, c.Query("status"), limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]poDTO, 0, len(ps))
	for _, p := range ps {
		out = append(out, poOf(p))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type receiptReq struct {
	LotRef      *string `json:"lot_ref"`
	ReceivedQty *string `json:"received_qty"`
}

func (h Handler) createReceipt(c *gin.Context) {
	id, ok := pathID(c, app.ErrPONotFound)
	if !ok {
		return
	}
	var req receiptReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.ReceiptInput{POID: id, IdemKey: idem(c)}
	in.LotRef = x.uuidPtr("lot_ref", req.LotRef)
	in.ReceivedQty = x.decPtr("received_qty", req.ReceivedQty)
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.CreateReceipt(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"receipt": receiptViewOf(r)})
}

func (h Handler) getReceipt(c *gin.Context) {
	id, ok := pathID(c, app.ErrReceiptNotFound)
	if !ok {
		return
	}
	r, err := h.Svc.Receipt(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"receipt": receiptViewOf(r)})
}

type reportReq struct {
	Note    string  `json:"note"`
	FileRef *string `json:"file_ref"`
}

func (h Handler) varianceReport(c *gin.Context) {
	id, ok := pathID(c, app.ErrReceiptNotFound)
	if !ok {
		return
	}
	var req reportReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	file := x.uuidPtr("file_ref", req.FileRef)
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.FileVarianceReport(c.Request.Context(), id, req.Note, file, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"receipt": receiptViewOf(r)})
}

type receiptCorrectionReq struct {
	ReceivedQty string `json:"received_qty"`
	Reason      string `json:"reason"`
}

func (h Handler) correctReceipt(c *gin.Context) {
	id, ok := pathID(c, app.ErrReceiptNotFound)
	if !ok {
		return
	}
	var req receiptCorrectionReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	q := x.dec("received_qty", req.ReceivedQty)
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.CorrectReceipt(c.Request.Context(), id, q, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"receipt": receiptViewOf(r)})
}

func (h Handler) decideReceipt(c *gin.Context) {
	id, ok := pathID(c, app.ErrReceiptNotFound)
	if !ok {
		return
	}
	approve, reason, ok := decision(c)
	if !ok {
		return
	}
	r, replayed, err := h.Svc.DecideReceipt(c.Request.Context(), id, approve, reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"receipt": receiptViewOf(r)})
}

func (h Handler) payables(c *gin.Context) {
	po, ok := queryUUID(c, "po_id")
	if !ok {
		return
	}
	var after *uuid.UUID
	if s := c.Query("cursor"); s != "" {
		b, err := base64.RawURLEncoding.DecodeString(s)
		id, perr := uuid.ParseBytes(b)
		if err != nil || perr != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = &id
	}
	n := limit(c)
	if n <= 0 || n > 200 {
		n = 50
	}
	ps, err := h.Svc.Payables(c.Request.Context(), po, after, n+1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	next := ""
	if len(ps) > n {
		ps = ps[:n]
		next = base64.RawURLEncoding.EncodeToString([]byte(ps[n-1].ID.String()))
	}
	out := make([]payableDTO, 0, len(ps))
	for _, p := range ps {
		out = append(out, payableOf(p))
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": next})
}

// ---------------------------------------------------------------------------
// Đơn bán, giao nhận
// ---------------------------------------------------------------------------

type soReq struct {
	CustomerRef  string  `json:"customer_ref"`
	Product      string  `json:"product"`
	OrderedKg    string  `json:"ordered_kg"`
	ListPrice    *string `json:"list_price"`
	UnitPrice    string  `json:"unit_price"`
	DiscountPct  *string `json:"discount_pct"`
	DeliveryFrom string  `json:"delivery_from"`
	DeliveryTo   string  `json:"delivery_to"`
}

func (h Handler) createSO(c *gin.Context) {
	var req soReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.SOInput{Product: req.Product, DeliveryFrom: req.DeliveryFrom, DeliveryTo: req.DeliveryTo, IdemKey: idem(c)}
	in.CustomerRef = x.uuid("customer_ref", req.CustomerRef)
	in.OrderedKg = x.kg("ordered_kg", req.OrderedKg)
	in.ListPrice = x.pricePtr("list_price", req.ListPrice)
	in.UnitPrice = x.price("unit_price", req.UnitPrice)
	if d := x.decPtr("discount_pct", req.DiscountPct); d != nil {
		in.DiscountPct = *d
	}
	if x.fail(c) {
		return
	}
	o, replayed, err := h.Svc.CreateSO(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"sales_order": soViewOf(o)})
}

func (h Handler) confirmSO(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	o, replayed, err := h.Svc.ConfirmSO(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"sales_order": soViewOf(o)})
}

type advanceReq struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (h Handler) advanceSO(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	var req advanceReq
	if !bind(c, &req) {
		return
	}
	o, replayed, err := h.Svc.AdvanceSO(c.Request.Context(), id, req.Status, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"sales_order": soViewOf(o)})
}

func (h Handler) getSO(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	o, err := h.Svc.SO(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sales_order": soViewOf(o)})
}

func (h Handler) listSOs(c *gin.Context) {
	cust, ok := queryUUID(c, "customer_ref")
	if !ok {
		return
	}
	os, err := h.Svc.ListSOs(c.Request.Context(), cust, c.Query("status"), limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]orderDTO, 0, len(os))
	for _, o := range os {
		out = append(out, orderOf(o))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type deliveryReq struct {
	LotRef      string  `json:"lot_ref"`
	Route       string  `json:"route"`
	PlannedDate string  `json:"planned_date"`
	PlannedKg   string  `json:"planned_kg"`
	TripRef     *string `json:"trip_ref"`
}

func (h Handler) planDelivery(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	var req deliveryReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.DeliveryInput{Route: req.Route, PlannedDate: req.PlannedDate, IdemKey: idem(c)}
	in.LotRef = x.uuid("lot_ref", req.LotRef)
	in.PlannedKg = x.kg("planned_kg", req.PlannedKg)
	in.TripRef = x.uuidPtr("trip_ref", req.TripRef)
	if x.fail(c) {
		return
	}
	d, replayed, err := h.Svc.PlanDelivery(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"delivery": deliveryOf(d)})
}

func (h Handler) getDelivery(c *gin.Context) {
	id, ok := pathID(c, app.ErrDeliveryNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetDelivery(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"delivery": deliveryOf(d)})
}

type dispatchReq struct {
	TripRef *string `json:"trip_ref"`
}

func (h Handler) dispatch(c *gin.Context) {
	id, ok := pathID(c, app.ErrDeliveryNotFound)
	if !ok {
		return
	}
	var req dispatchReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	trip := x.uuidPtr("trip_ref", req.TripRef)
	if x.fail(c) {
		return
	}
	d, replayed, err := h.Svc.DispatchDelivery(c.Request.Context(), id, trip, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"delivery": deliveryOf(d)})
}

type acceptReq struct {
	Evidence *struct {
		FileRef string `json:"file_ref"`
		Kind    string `json:"kind"`
	} `json:"evidence"`
	DisputeRef *string `json:"dispute_ref"`
	AcceptedAt *string `json:"accepted_at"`
}

func (h Handler) accept(c *gin.Context) {
	id, ok := pathID(c, app.ErrDeliveryNotFound)
	if !ok {
		return
	}
	var req acceptReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.AcceptInput{IdemKey: idem(c)}
	if req.Evidence == nil {
		x.add("evidence", "bắt buộc: biên bản giao nhận (file_ref + kind)")
	} else {
		in.EvidenceFileRef = x.uuid("evidence.file_ref", req.Evidence.FileRef)
		in.EvidenceKind = req.Evidence.Kind
	}
	in.DisputeRef = x.uuidPtr("dispute_ref", req.DisputeRef)
	if req.AcceptedAt != nil && *req.AcceptedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, *req.AcceptedAt)
		if err != nil {
			x.add("accepted_at", "RFC 3339 có múi giờ")
		}
		in.AcceptedAt = &t
	}
	if x.fail(c) {
		return
	}
	d, replayed, err := h.Svc.AcceptDelivery(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"delivery": deliveryOf(d)})
}

func (h Handler) reject(c *gin.Context) { h.endDelivery(c, false) }

func (h Handler) cancelDelivery(c *gin.Context) { h.endDelivery(c, true) }

func (h Handler) endDelivery(c *gin.Context, cancel bool) {
	id, ok := pathID(c, app.ErrDeliveryNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	d, replayed, err := h.Svc.RejectDelivery(c.Request.Context(), id, cancel, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"delivery": deliveryOf(d)})
}

type deliveryCorrectionReq struct {
	TicketRef  *string `json:"customer_in_ticket_ref"`
	DisputeRef *string `json:"dispute_ref"`
	Reason     string  `json:"reason"`
}

func (h Handler) correctDelivery(c *gin.Context) {
	id, ok := pathID(c, app.ErrDeliveryNotFound)
	if !ok {
		return
	}
	var req deliveryCorrectionReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.CorrectionInput{Reason: req.Reason, IdemKey: idem(c)}
	in.TicketRef = x.uuidPtr("customer_in_ticket_ref", req.TicketRef)
	in.DisputeRef = x.uuidPtr("dispute_ref", req.DisputeRef)
	if x.fail(c) {
		return
	}
	d, replayed, err := h.Svc.CorrectDelivery(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"delivery": deliveryOf(d)})
}

// ---------------------------------------------------------------------------
// Tham số, dựng lại
// ---------------------------------------------------------------------------

func (h Handler) params(c *gin.Context) {
	pv, err := h.Svc.Params(c.Request.Context(), c.Param("key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	vs := make([]paramDTO, 0, len(pv.Versions))
	for _, p := range pv.Versions {
		vs = append(vs, paramOf(p))
	}
	c.JSON(http.StatusOK, gin.H{"param_key": pv.Def.Key, "effective_value": pv.Effective.String(), "param_version": pv.ParamVersion,
		"provisional": pv.Provisional, "default_value": pv.Def.Default.String(), "note": pv.Def.Note, "versions": vs})
}

type paramReq struct {
	Value         string `json:"value"`
	EffectiveFrom string `json:"effective_from"`
	Provisional   *bool  `json:"provisional"`
	Reason        string `json:"reason"`
}

func (h Handler) proposeParam(c *gin.Context) {
	var req paramReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	val := x.dec("value", req.Value)
	if x.fail(c) {
		return
	}
	prov := true
	if req.Provisional != nil {
		prov = *req.Provisional
	}
	p, replayed, err := h.Svc.ProposeParam(c.Request.Context(), app.ParamInput{Key: c.Param("key"), Value: val, EffectiveFrom: req.EffectiveFrom,
		Provisional: prov, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"param": paramOf(p)})
}

func (h Handler) decideParam(c *gin.Context) {
	ver, err := uuid.Parse(c.Param("version"))
	if err != nil {
		httpx.Fail(c, app.ErrParamNotFound)
		return
	}
	approve, reason, ok := decision(c)
	if !ok {
		return
	}
	p, replayed, err := h.Svc.DecideParam(c.Request.Context(), c.Param("key"), ver, approve, reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, gin.H{"param": paramOf(p)})
}

func (h Handler) rebuild(c *gin.Context) {
	n, err := h.Svc.RebuildProjections(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, n)
}
