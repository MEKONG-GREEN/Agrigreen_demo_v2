package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
)

// Số thập phân luôn là chuỗi JSON (R-10): đơn giá 4 số, tiền 2 số, kg 3 số, phần trăm 2 số.

func price(d decimal.Decimal) string  { return d.StringFixed(4) }
func amount(d decimal.Decimal) string { return d.StringFixed(2) }
func kg(d decimal.Decimal) string     { return d.StringFixed(3) }
func pct(d decimal.Decimal) string    { return d.StringFixed(2) }

func qty(unit string, d decimal.Decimal) string {
	if unit == "bale" {
		return d.StringFixed(0)
	}
	return kg(d)
}

func nullStr(d decimal.NullDecimal, f func(decimal.Decimal) string) *string {
	if !d.Valid {
		return nil
	}
	s := f(d.Decimal)
	return &s
}

func date(t time.Time) string { return t.Format("2006-01-02") }

func datePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := date(*t)
	return &s
}

type contractDTO struct {
	ContractID     uuid.UUID  `json:"contract_id"`
	Version        int        `json:"version"`
	Status         string     `json:"status"`
	Code           string     `json:"code"`
	HTXRef         uuid.UUID  `json:"htx_ref"`
	Basis          string     `json:"basis"`
	UnitPrice      string     `json:"unit_price"`
	CommittedKg    string     `json:"committed_kg"`
	MaxMoisturePct *string    `json:"max_moisture_pct"`
	ValidFrom      string     `json:"valid_from"`
	ValidTo        string     `json:"valid_to"`
	Note           *string    `json:"note"`
	ProposedBy     uuid.UUID  `json:"proposed_by"`
	ApprovedBy     *uuid.UUID `json:"approved_by"`
	Reason         *string    `json:"reason"`
	EventTime      time.Time  `json:"event_time"`
}

func contractOf(c store.Contract) contractDTO {
	return contractDTO{ContractID: c.ContractID, Version: c.Version, Status: c.Status, Code: c.Code, HTXRef: c.HTXRef, Basis: c.Basis,
		UnitPrice: price(c.UnitPrice), CommittedKg: kg(c.CommittedKg), MaxMoisturePct: nullStr(c.MaxMoisturePct, pct),
		ValidFrom: date(c.ValidFrom), ValidTo: date(c.ValidTo), Note: c.Note, ProposedBy: c.ProposerRef, ApprovedBy: c.ApproverRef,
		Reason: c.Reason, EventTime: c.EventTime}
}

func contractsOf(cs []store.Contract) []contractDTO {
	out := make([]contractDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, contractOf(c))
	}
	return out
}

type termsDTO struct {
	ContractID     uuid.UUID `json:"contract_id"`
	Code           string    `json:"code"`
	HTXRef         uuid.UUID `json:"htx_ref"`
	Basis          string    `json:"basis"`
	UnitPrice      string    `json:"unit_price"`
	CommittedKg    string    `json:"committed_kg"`
	MaxMoisturePct *string   `json:"max_moisture_pct"`
	ValidFrom      string    `json:"valid_from"`
	ValidTo        string    `json:"valid_to"`
	Version        int       `json:"version"`
}

func termsOf(t port.ContractTerms) termsDTO {
	return termsDTO{ContractID: t.ContractID, Code: t.Code, HTXRef: t.HTXRef, Basis: string(t.Basis), UnitPrice: t.UnitPrice.String(),
		CommittedKg: t.CommittedKg.String(), MaxMoisturePct: t.MaxMoisturePct, ValidFrom: date(t.ValidFrom), ValidTo: date(t.ValidTo), Version: t.Version}
}

type poDTO struct {
	POID            uuid.UUID    `json:"po_id"`
	Status          string       `json:"status"`
	Code            string       `json:"code"`
	Category        string       `json:"category"`
	SupplierRef     uuid.UUID    `json:"supplier_ref"`
	ContractRef     *uuid.UUID   `json:"contract_ref"`
	ContractVersion *int         `json:"contract_version"`
	FacilityRef     *uuid.UUID   `json:"facility_ref"`
	Unit            string       `json:"unit"`
	OrderedQty      string       `json:"ordered_qty"`
	UnitPrice       string       `json:"unit_price"`
	PriceSource     string       `json:"price_source"`
	ExpectedDate    *string      `json:"expected_date"`
	Description     *string      `json:"description"`
	CreatedBy       uuid.UUID    `json:"created_by"`
	ApprovedBy      *uuid.UUID   `json:"approved_by"`
	Reason          *string      `json:"reason"`
	EventTime       time.Time    `json:"event_time"`
	Receipts        []receiptDTO `json:"receipts,omitempty"`
}

func poOf(p store.PO) poDTO {
	return poDTO{POID: p.POID, Status: p.Status, Code: p.Code, Category: p.Category, SupplierRef: p.SupplierRef, ContractRef: p.ContractRef,
		ContractVersion: p.ContractVersion, FacilityRef: p.FacilityRef, Unit: p.Unit, OrderedQty: qty(p.Unit, p.OrderedQty),
		UnitPrice: price(p.UnitPrice), PriceSource: p.PriceSource, ExpectedDate: datePtr(p.ExpectedDate), Description: p.Description,
		CreatedBy: p.CreatorRef, ApprovedBy: p.ApproverRef, Reason: p.Reason, EventTime: p.EventTime}
}

type receiptDTO struct {
	ReceiptID           uuid.UUID   `json:"receipt_id"`
	POID                uuid.UUID   `json:"po_id"`
	Status              string      `json:"status"`
	Code                string      `json:"code"`
	Category            string      `json:"category"`
	SupplierRef         uuid.UUID   `json:"supplier_ref"`
	LotRef              *uuid.UUID  `json:"lot_ref"`
	Unit                string      `json:"unit"`
	OrderedQty          string      `json:"ordered_qty"`
	TicketRef           *uuid.UUID  `json:"ticket_ref"`
	TicketKg            *string     `json:"ticket_kg"`
	ReceivedQty         *string     `json:"received_qty"`
	QtySource           *string     `json:"qty_source"`
	VariancePct         *string     `json:"variance_pct"`
	ThresholdPct        *string     `json:"threshold_pct"`
	ParamVersion        *string     `json:"param_version"`
	NeedsVarianceReport bool        `json:"needs_variance_report"`
	AuditNote           *string     `json:"audit_note"`
	ReportNote          *string     `json:"report_note"`
	ReportFileRef       *uuid.UUID  `json:"report_file_ref"`
	CreatedBy           uuid.UUID   `json:"created_by"`
	ApprovedBy          *uuid.UUID  `json:"approved_by"`
	Reason              *string     `json:"reason"`
	EventTime           time.Time   `json:"event_time"`
	Payable             *payableDTO `json:"payable,omitempty"`
}

func receiptOf(r store.Receipt) receiptDTO {
	return receiptDTO{ReceiptID: r.ReceiptID, POID: r.POID, Status: r.Status, Code: r.Code, Category: r.Category, SupplierRef: r.SupplierRef,
		LotRef: r.LotRef, Unit: r.Unit, OrderedQty: qty(r.Unit, r.OrderedQty), TicketRef: r.TicketRef, TicketKg: nullStr(r.TicketKg, kg),
		ReceivedQty: nullStr(r.ReceivedQty, func(d decimal.Decimal) string { return qty(r.Unit, d) }), QtySource: r.QtySource,
		VariancePct: nullStr(r.VariancePct, pct), ThresholdPct: nullStr(r.ThresholdPct, pct), ParamVersion: r.ParamVersion,
		NeedsVarianceReport: r.NeedsVarianceReport, AuditNote: r.AuditNote, ReportNote: r.ReportNote, ReportFileRef: r.ReportFileRef,
		CreatedBy: r.CreatorRef, ApprovedBy: r.ApproverRef, Reason: r.Reason, EventTime: r.EventTime}
}

func receiptViewOf(v app.ReceiptView) receiptDTO {
	d := receiptOf(v.Receipt)
	if v.Payable != nil {
		p := payableOf(*v.Payable)
		d.Payable = &p
	}
	return d
}

type payableDTO struct {
	PayableID       uuid.UUID `json:"payable_id"`
	ReceiptID       uuid.UUID `json:"receipt_id"`
	POID            uuid.UUID `json:"po_id"`
	SupplierRef     uuid.UUID `json:"supplier_ref"`
	Unit            string    `json:"unit"`
	Qty             string    `json:"qty"`
	UnitPrice       string    `json:"unit_price"`
	Amount          string    `json:"amount"`
	DueDate         string    `json:"due_date"`
	PaymentTermDays int       `json:"payment_term_days"`
	ParamVersion    string    `json:"param_version"`
	RecordedAt      time.Time `json:"recorded_at"`
	// Bản bù (RB-1): phiếu HUB_IN bù sau khi phiếu nhận đã duyệt → khoản phải trả mới thay khoản cũ.
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	CorrectionReason *string    `json:"correction_reason"`
}

func payableOf(p store.Payable) payableDTO {
	return payableDTO{PayableID: p.ID, ReceiptID: p.ReceiptID, POID: p.POID, SupplierRef: p.SupplierRef, Unit: p.Unit, Qty: qty(p.Unit, p.Qty),
		UnitPrice: price(p.UnitPrice), Amount: amount(p.Amount), DueDate: date(p.DueDate), PaymentTermDays: p.PaymentTermDays,
		ParamVersion: p.ParamVersion, RecordedAt: p.RecordedAt, SupersedesID: p.SupersedesID, CorrectionReason: p.CorrectionReason}
}

type orderDTO struct {
	OrderID            uuid.UUID     `json:"order_id"`
	Status             string        `json:"status"`
	Code               string        `json:"code"`
	CustomerRef        uuid.UUID     `json:"customer_ref"`
	Product            string        `json:"product"`
	OrderedKg          string        `json:"ordered_kg"`
	ListPrice          *string       `json:"list_price"`
	UnitPrice          string        `json:"unit_price"`
	DiscountPct        string        `json:"discount_pct"`
	NetPrice           string        `json:"net_price"`
	Amount             string        `json:"amount"`
	NeedsPriceApproval bool          `json:"needs_price_approval"`
	PriceRule          string        `json:"price_rule"`
	PaymentTermDays    int           `json:"payment_term_days"`
	DeliveryFrom       *string       `json:"delivery_from"`
	DeliveryTo         *string       `json:"delivery_to"`
	CreatedBy          uuid.UUID     `json:"created_by"`
	ApprovedBy         *uuid.UUID    `json:"approved_by"`
	Reason             *string       `json:"reason"`
	EventTime          time.Time     `json:"event_time"`
	Deliveries         []deliveryDTO `json:"deliveries,omitempty"`
	// G4 O-05: kiểm tồn hub (đơn draft / confirmed) — thiếu tồn chỉ cảnh báo (warnings: orders.stock_shortage).
	StockCheck *stockCheckDTO `json:"stock_check,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
}

type stockCheckDTO struct {
	Source       string  `json:"source"` // hub | unavailable
	Product      string  `json:"product"`
	OrderedKg    string  `json:"ordered_kg"`
	AvailableKg  *string `json:"available_kg"`
	ShortageKg   *string `json:"shortage_kg"`
	Facilities   int     `json:"facilities"`
	Shortage     bool    `json:"shortage"`
	Blocking     bool    `json:"blocking"`
	ParamVersion string  `json:"param_version"`
}

func stockCheckOf(c *app.StockCheck) (*stockCheckDTO, []string) {
	if c == nil {
		return nil, nil
	}
	d := &stockCheckDTO{Source: c.Source, Product: c.Product, OrderedKg: kg(c.OrderedKg), Facilities: c.Facilities, Shortage: c.Shortage,
		Blocking: c.Blocking, ParamVersion: c.ParamVersion}
	if c.Source == app.StockSourceHub {
		a, s := kg(c.AvailableKg), kg(c.ShortageKg)
		d.AvailableKg, d.ShortageKg = &a, &s
	}
	if c.Shortage {
		return d, []string{app.WarnStockShortage}
	}
	return d, nil
}

func orderOf(o store.SalesOrder) orderDTO {
	return orderDTO{OrderID: o.OrderID, Status: o.Status, Code: o.Code, CustomerRef: o.CustomerRef, Product: o.Product, OrderedKg: kg(o.OrderedKg),
		ListPrice: nullStr(o.ListPrice, price), UnitPrice: price(o.UnitPrice), DiscountPct: pct(o.DiscountPct), NetPrice: price(o.NetPrice),
		Amount: amount(o.Amount), NeedsPriceApproval: o.NeedsPriceApproval, PriceRule: o.PriceRule, PaymentTermDays: o.PaymentTermDays,
		DeliveryFrom: datePtr(o.DeliveryFrom), DeliveryTo: datePtr(o.DeliveryTo), CreatedBy: o.CreatorRef, ApprovedBy: o.ApproverRef,
		Reason: o.Reason, EventTime: o.EventTime}
}

func soViewOf(v app.SOView) orderDTO {
	d := orderOf(v.Order)
	d.StockCheck, d.Warnings = stockCheckOf(v.Stock)
	d.Deliveries = make([]deliveryDTO, 0, len(v.Deliveries))
	for _, x := range v.Deliveries {
		d.Deliveries = append(d.Deliveries, deliveryOf(x))
	}
	return d
}

type deliveryDTO struct {
	DeliveryID      uuid.UUID  `json:"delivery_id"`
	OrderID         uuid.UUID  `json:"order_id"`
	Status          string     `json:"status"`
	Code            string     `json:"code"`
	CustomerRef     uuid.UUID  `json:"customer_ref"`
	Route           string     `json:"route"`
	LotRef          uuid.UUID  `json:"lot_ref"`
	TripRef         *uuid.UUID `json:"trip_ref"`
	PlannedDate     string     `json:"planned_date"`
	PlannedKg       string     `json:"planned_kg"`
	UnitPrice       string     `json:"unit_price"`
	TicketRef       *uuid.UUID `json:"customer_in_ticket_ref"`
	TicketKg        *string    `json:"ticket_kg"`
	AcceptedKg      *string    `json:"accepted_kg"`
	Amount          *string    `json:"amount"`
	AcceptedAt      *time.Time `json:"accepted_at"`
	AcceptanceID    *uuid.UUID `json:"acceptance_id"`
	DisputeRef      *uuid.UUID `json:"dispute_ref"`
	EvidenceFileRef *uuid.UUID `json:"evidence_file_ref"`
	EvidenceKind    *string    `json:"evidence_kind"`
	Reason          *string    `json:"reason"`
	RuleVersion     string     `json:"rule_version"`
	EventTime       time.Time  `json:"event_time"`
}

func deliveryOf(d store.Delivery) deliveryDTO {
	return deliveryDTO{DeliveryID: d.DeliveryID, OrderID: d.OrderID, Status: d.Status, Code: d.Code, CustomerRef: d.CustomerRef, Route: d.Route,
		LotRef: d.LotRef, TripRef: d.TripRef, PlannedDate: date(d.PlannedDate), PlannedKg: kg(d.PlannedKg), UnitPrice: price(d.UnitPrice),
		TicketRef: d.TicketRef, TicketKg: nullStr(d.TicketKg, kg), AcceptedKg: nullStr(d.AcceptedKg, kg), Amount: nullStr(d.Amount, amount),
		AcceptedAt: d.AcceptedAt, AcceptanceID: d.AcceptanceID, DisputeRef: d.DisputeRef, EvidenceFileRef: d.EvidenceFileRef,
		EvidenceKind: d.EvidenceKind, Reason: d.Reason, RuleVersion: d.RuleVersion, EventTime: d.EventTime}
}

type paramDTO struct {
	ParamKey       string     `json:"param_key"`
	VersionID      uuid.UUID  `json:"version_id"`
	VersionSeq     int        `json:"version_seq"`
	Value          string     `json:"value"`
	EffectiveFrom  string     `json:"effective_from"`
	Provisional    bool       `json:"provisional"`
	Status         string     `json:"status"`
	ProposedBy     uuid.UUID  `json:"proposed_by"`
	ProposalReason string     `json:"proposal_reason"`
	DecidedBy      *uuid.UUID `json:"decided_by"`
	DecidedAt      *time.Time `json:"decided_at"`
	DecisionReason *string    `json:"decision_reason"`
}

func paramOf(p store.Param) paramDTO {
	return paramDTO{ParamKey: p.Key, VersionID: p.VersionID, VersionSeq: p.VersionSeq, Value: p.Value.String(), EffectiveFrom: date(p.EffectiveFrom),
		Provisional: p.Provisional, Status: p.Status, ProposedBy: p.ProposedBy, ProposalReason: p.ProposalReason, DecidedBy: p.DecidedBy,
		DecidedAt: p.DecidedAt, DecisionReason: p.DecisionReason}
}
