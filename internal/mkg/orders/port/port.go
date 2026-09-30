// Package port là cửa công khai DUY NHẤT của module mkg/orders (R-02): chứng từ
// thương mại của MKG — hợp đồng mua rơm với HTX (theo tấn nhà máy cân hoặc theo cuộn),
// đơn mua + phiếu nhận hàng, đơn bán + giao nhận có biên bản liên kết phiếu cân
// CUSTOMER_IN (docs/design/mkg.md §5.5; docs/dev/g4-conventions.md).
//
// ĐÓNG BĂNG ở G4-0: đổi chữ ký / ngữ nghĩa qua người tích hợp; thêm phương thức vào
// interface MỚI; thêm trường DTO chỉ khi tương thích ngược.
//
// Nguyên tắc chung:
//   - tenantID tường minh, giao dịch tenant (RB-2). Tiền money.Amount numeric(18,2),
//     đơn giá money.Price numeric(18,4), khối lượng mass.Kg — không float (R-10, agnofloat).
//   - Giá mua/bán, chiết khấu, danh tính và điều khoản khách hàng KHÔNG BAO GIỜ qua van
//     (mkg.md §3.1). Khách, HTX, nhà cung cấp là *_ref (R-06); tên hiển thị qua vault.
//   - Phê duyệt giá/chiết khấu vượt ngưỡng: sales_officer lập ≠ sales_approver duyệt (R-56,
//     app/ + CHECK DB).
package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// DBRole là vai của module ở cụm mkg (schema mkg_orders, mkg 000014/000016).
const DBRole = "orders_app"

// Vai nghiệp vụ (realm Keycloak; tài khoản tenant MKG).
const (
	// RoleSalesOfficer: lập đơn bán, đơn mua, hợp đồng mua rơm; ghi giao nhận và phiếu nhận hàng;
	// đề xuất giá/chiết khấu.
	RoleSalesOfficer = "sales_officer"
	// RoleSalesApprover: duyệt đơn / giá / chiết khấu vượt ngưỡng và hợp đồng — không duyệt đề xuất
	// của chính mình (R-56).
	RoleSalesApprover = "sales_approver"
)

// PricingBasis là cách tính giá của hợp đồng mua rơm (proto CT-03). Trùng giá trị với
// measurement/port.PricingBasis (per_tonne | per_bale); orders là nguồn của điều khoản khi có
// hợp đồng G4, measurement lot_terms giữ vai trò bảng đệm đối soát (RB-6).
type PricingBasis string

// Cách tính giá.
const (
	PricingPerTonne PricingBasis = "per_tonne" // theo tấn nhà máy cân (proto theo_tan_can)
	PricingPerBale  PricingBasis = "per_bale"  // theo cuộn (proto theo_cuon)
)

// DeliveryStatus là trạng thái giao nhận: planned → dispatched → accepted | rejected; bù sửa sai
// bằng bản ghi mới supersedes (RB-1).
type DeliveryStatus string

// Trạng thái giao nhận.
const (
	DeliveryPlanned    DeliveryStatus = "planned"
	DeliveryDispatched DeliveryStatus = "dispatched"
	DeliveryAccepted   DeliveryStatus = "accepted" // biên bản giao nhận ký, liên kết phiếu CUSTOMER_IN
	DeliveryRejected   DeliveryStatus = "rejected"
)

// Route của giao nhận (khớp measurement/port.Route): HUB (qua hub) | DIRECT (thẳng nhà máy).
const (
	RouteHub    = "HUB"
	RouteDirect = "DIRECT"
)

// ---------------------------------------------------------------------------
// Sự kiện nội bộ (ADR-0018).
// ---------------------------------------------------------------------------

// EventDeliveryAccepted: giao nhận được chấp nhận (biên bản + phiếu CUSTOMER_IN) — hoặc bản bù.
const EventDeliveryAccepted = "mkg.orders.delivery.accepted"

// Consumer của orders (hàng đợi quorum trên mkg.internal).
const (
	// WeighTicketConsumer: mkg.measurement.weigh_ticket.recorded — CUSTOMER_IN của lô đang giao →
	// gắn phiếu vào giao nhận chờ chấp nhận; HUB_IN của lô có đơn mua → kiểm phiếu nhận hàng ↔ đơn
	// mua (> 3 % chỉ cảnh báo + biên bản chênh lệch, không chặn — proto RECEIPT_VARIANCE_TOLERANCE_PCT).
	WeighTicketConsumer = "mkg.orders.weigh_ticket"
)

// DeliveryAcceptedV1 là payload khi một giao nhận được chấp nhận. Người nhận nội bộ: mkg/costing
// (doanh thu và khối lượng giao theo lô, giá vốn). Chỉ vhost mkg.internal: giá bán, khách KHÔNG
// BAO GIỜ qua van.
//
//ag:internal-event mkg.orders.delivery.accepted v1
type DeliveryAcceptedV1 struct {
	DeliveryID          uuid.UUID           `json:"delivery_id"`
	OrderRef            uuid.UUID           `json:"order_ref"`
	CustomerRef         uuid.UUID           `json:"customer_ref"`
	Route               string              `json:"route"`
	TripRef             *uuid.UUID          `json:"trip_ref"`
	CustomerInTicketRef uuid.UUID           `json:"customer_in_ticket_ref"` // phiếu cân CUSTOMER_IN (measurement) — căn cứ khối lượng
	Lots                []DeliveryLotV1     `json:"lots"`
	AcceptedKg          mass.Kg             `json:"accepted_kg"`
	UnitPrice           string              `json:"unit_price"` // VND/tấn, decimal 4 số (money.Price.String() — Price không có dạng JSON)
	Amount              money.Amount        `json:"amount"`     // VND, numeric(18,2) = accepted_kg/1000 × unit_price, làm tròn 2 số
	DisputeRef          *uuid.UUID          `json:"dispute_ref"`
	AcceptedAt          time.Time           `json:"accepted_at"`
	RuleVersion         string              `json:"rule_version"`
	SupersedesID        *uuid.UUID          `json:"supersedes_id"`
	EventTime           time.Time           `json:"event_time"`
	Evidence            *DeliveryEvidenceV1 `json:"evidence"`
}

// DeliveryLotV1 là phần của một lô trong giao nhận.
type DeliveryLotV1 struct {
	LotRef uuid.UUID `json:"lot_ref"`
	MassKg mass.Kg   `json:"mass_kg"`
}

// DeliveryEvidenceV1 là chứng từ của biên bản giao nhận (tệp ở kho đối tượng, mã hóa).
type DeliveryEvidenceV1 struct {
	FileRef uuid.UUID `json:"file_ref"`
	Kind    string    `json:"kind"` // epod | minutes | ebill
}

// ---------------------------------------------------------------------------
// Cổng cho module mkg khác (cùng tiến trình).
// ---------------------------------------------------------------------------

// DeliveryLot là phần của một lô trong giao nhận.
type DeliveryLot struct {
	LotRef uuid.UUID
	MassKg mass.Kg
}

// DeliveryView là giao nhận hiệu lực (đã tính bản bù).
type DeliveryView struct {
	DeliveryID          uuid.UUID
	OrderRef            uuid.UUID
	CustomerRef         uuid.UUID
	Status              DeliveryStatus
	Route               string
	TripRef             *uuid.UUID
	CustomerInTicketRef *uuid.UUID
	Lots                []DeliveryLot
	AcceptedKg          *mass.Kg
	UnitPrice           *money.Price
	Amount              *money.Amount
	AcceptedAt          *time.Time
	RuleVersion         string
	RecordedAt          time.Time
}

// DeliveryPage là một trang giao nhận theo con trỏ.
type DeliveryPage struct {
	Items []DeliveryView
	Next  string
}

// Deliveries cho mkg/costing đọc giao nhận. Accepted trả giao nhận accepted với accepted_at ∈
// [from, to) và recorded_at < asOf (ảnh chụp đầu vào của đóng kỳ), sắp theo (accepted_at, id).
type Deliveries interface {
	Delivery(ctx context.Context, tenantID, deliveryID uuid.UUID) (DeliveryView, error)
	Accepted(ctx context.Context, tenantID uuid.UUID, from, to, asOf time.Time, cursor string, limit int) (DeliveryPage, error)
}

// ContractTerms là điều khoản hợp đồng mua rơm có hiệu lực của một HTX tại một ngày (proto
// contractFor). Một HTX tối đa một hợp đồng hiệu lực mỗi kỳ (CT-01); giá > 0, tấn cam kết > 0,
// ngày kết thúc ≥ ngày bắt đầu (CT-02). Trần ẩm được lưu nhưng CHƯA cưỡng chế (C-4).
type ContractTerms struct {
	ContractID     uuid.UUID
	Code           string // HDR + 5 số (proto)
	HTXRef         uuid.UUID
	Basis          PricingBasis
	UnitPrice      money.Price // VND / tấn (per_tonne) hoặc VND / cuộn (per_bale)
	CommittedKg    mass.Kg
	MaxMoisturePct *string // decimal "18.00"; chưa cưỡng chế (C-4)
	ValidFrom      time.Time
	ValidTo        time.Time
	Version        int
}

// Contracts cho mkg/costing (quyết toán HTX, proto PM-02/03) và measurement (lot_terms) đọc
// điều khoản. ErrNoContract khi HTX không có hợp đồng hiệu lực tại ngày đó — quyết toán KHÔNG
// đoán giá (proto PM-03: người xác nhận nhập giá).
type Contracts interface {
	ContractFor(ctx context.Context, tenantID, htxRef uuid.UUID, on time.Time) (ContractTerms, error)
}

// ReceiptCheck là kết quả kiểm phiếu nhận hàng ↔ đơn mua.
type ReceiptCheck struct {
	PurchaseOrderRef uuid.UUID
	ReceiptRef       uuid.UUID
	OrderedKg        mass.Kg
	ReceivedKg       mass.Kg // tịnh của phiếu cân (measurement)
	VariancePct      string  // decimal 2 số, có dấu
	// NeedsVarianceReport: |chênh| > ngưỡng (mặc định 3 %, tạm chờ khách) → cần biên bản chênh
	// lệch; CHỈ cảnh báo, không chặn (prototype).
	NeedsVarianceReport bool
	ThresholdPct        string
	ParamVersion        int
}

// Lỗi ổn định của port (problem.* mã orders.* — docs/errors.md).
var (
	ErrDeliveryNotFound = errors.New("orders: không có giao nhận")
	ErrNoContract       = errors.New("orders: HTX không có hợp đồng mua rơm hiệu lực")
)

// Tham số có phiên bản (người đặt ≠ người duyệt). Mặc định TẠM, CHỜ KHÁCH.
const (
	ParamReceiptVariancePct    = "orders.receipt.po_variance_warn_pct"   // 3 (proto RECEIPT_VARIANCE_TOLERANCE_PCT)
	ParamPriceApprovalDelta    = "orders.sales.price_approval_delta_pct" // giá lệch bảng giá > x % → cần sales_approver (chờ khách)
	ParamDiscountApprovalPct   = "orders.sales.discount_approval_pct"    // chiết khấu > x % → cần sales_approver (chờ khách)
	ParamPaymentTermDays       = "orders.payment_term_days"              // 15 (proto PAYMENT_TERM_DAYS)
	ParamStockShortageBlocking = "orders.sales.stock_shortage_blocking"  // false: thiếu tồn chỉ cảnh báo (proto stockWarning)
)
