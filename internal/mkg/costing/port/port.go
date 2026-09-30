// Package port là cửa công khai DUY NHẤT của module mkg/costing (R-02): giá thành theo
// lô, quy đổi cuộn → tấn và ẩm độ, phân bổ chi phí đa chặng, quy kết hao hụt về lô nguồn,
// kỳ + đóng kỳ, quyết toán HTX (docs/design/mkg.md §5.6; tài liệu kiến trúc mục 16;
// docs/dev/g4-conventions.md).
//
// EVENT SOURCING MỨC B (ADR-0003): sổ là nguồn — cost_entry, allocation_result,
// settlement_event, period_event; mọi bảng *_current dựng lại được từ sổ
// (POST /v1/costing/projections/rebuild, R-22). Mỗi phép tính ghim rule_version vào bản ghi
// kết quả; tính lại quá khứ dùng recorded_at (R-13) và ghi kết quả MỚI trỏ supersedes_id —
// không bao giờ sửa kết quả cũ. Định mức/tham số theo thời điểm SỰ VIỆC.
//
// ĐÓNG BĂNG ở G4-0: đổi chữ ký / ngữ nghĩa qua người tích hợp; thêm phương thức vào interface
// MỚI; thêm trường DTO chỉ khi tương thích ngược.
//
// Đầu vào costing đọc qua port của module khác (cùng tiến trình, pool vai của module đó —
// nối ở cmd/api-internal/assembly và cmd/worker-internal/assembly):
//   - hub/port.Lots, hub/port.LotOrigins — nguồn của lô kèm tỷ lệ (Σ = 1) để quy kết hao hụt;
//   - measurement/port.LotMass — khối lượng chuẩn HUB_IN / HUB_OUT / CUSTOMER_IN;
//   - measurement/port.RollTotals (hoặc fieldops) — số cuộn, kg/cuộn học theo HTX;
//   - logistics/port.Trips — chuyến hoàn tất, chặng, cước thực tế;
//   - orders/port.Deliveries, orders/port.Contracts — giao nhận chấp nhận, điều khoản mua rơm.
//
// Cộng với sự kiện nội bộ (consumer dưới đây) làm tín hiệu "có dữ liệu mới"; ảnh chụp đầu vào
// của đóng kỳ luôn đọc lại qua port với asOf (bất biến, băm SHA-256).
//
// Giá thành, giá mua/bán, biên lợi nhuận, quyết toán, bút toán KHÔNG BAO GIỜ qua van.
package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// DBRole là vai của module ở cụm mkg (schema mkg_costing, mkg 000014/000017/000018).
const DBRole = "costing_app"

// Vai nghiệp vụ (realm Keycloak; tài khoản tenant MKG).
const (
	// RoleCostAccountant: kế toán giá thành — ghi bút toán chi phí, chạy phân bổ, lập quyết toán HTX,
	// lập bút toán điều chỉnh, đề xuất tham số giá thành.
	RoleCostAccountant = "cost_accountant"
	// RolePeriodCloser: yêu cầu đóng kỳ (202 + job, mẫu mục 12) — không được là người lập bút toán
	// điều chỉnh của kỳ (R-56, app/ + CHECK DB).
	RolePeriodCloser = "period_closer"
	// RoleChiefAccountant: kế toán trưởng — duyệt quyết toán HTX (≠ người lập), duyệt tham số giá
	// thành (≠ người đặt), duyệt bút toán tổng hợp của mkg/acctbridge (≠ người dựng). R-56.
	RoleChiefAccountant = "chief_accountant"
)

// CostCategory là nhóm chi phí / doanh thu của sổ giá thành (cũng là khóa ánh xạ tài khoản của
// mkg/acctbridge). Thêm nhóm = thêm hằng + ánh xạ tài khoản có phiên bản.
type CostCategory string

// Nhóm chi phí / doanh thu.
const (
	CatStrawPurchase     CostCategory = "straw_purchase"     // mua rơm HTX (quyết toán, proto PM-01..04)
	CatCollectionLabour  CostCategory = "collection_labour"  // nhân công gom (#20 180.000 đ/t — chỉ mô phỏng)
	CatConsumables       CostCategory = "consumables"        // vật tư (#21 45.000 đ/t)
	CatTransportRoad     CostCategory = "transport_road"     // cước đường bộ (logistics)
	CatTransportWaterway CostCategory = "transport_waterway" // cước đường thủy (logistics)
	CatHubProcessing     CostCategory = "hub_processing"     // sấy/nén tại hub (#55 120.000 đ/t)
	CatStorage           CostCategory = "storage"            // lưu kho
	CatHandling          CostCategory = "handling"           // bốc xếp
	CatLoss              CostCategory = "loss"               // hao hụt quy kết (bất thường; dự kiến nằm trong giá thành)
	CatSalesRevenue      CostCategory = "sales_revenue"      // doanh thu giao nhận (orders)
	CatCarbonRevenue     CostCategory = "carbon_revenue"     // doanh thu carbon — phần MKG giữ lại
	CatCarbonShare       CostCategory = "carbon_share"       // phần chia carbon phải trả (nông hộ/HTX, cơ quan, VVB)
)

// PeriodStatus là trạng thái kỳ: open → closing (job đang chạy) → closed. Kỳ đã đóng KHÔNG mở
// lại; sai sót sau đóng = bút toán điều chỉnh ở kỳ đang mở, trỏ supersedes về kết quả cũ.
// Kỳ đã có bút toán tổng hợp đẩy thành công (mkg.acctbridge.journal.pushed) → posted.
type PeriodStatus string

// Trạng thái kỳ.
const (
	PeriodOpen    PeriodStatus = "open"
	PeriodClosing PeriodStatus = "closing"
	PeriodClosed  PeriodStatus = "closed"
	PeriodPosted  PeriodStatus = "posted"
)

// SettlementStatus là trạng thái quyết toán HTX (proto phiếu mua rơm straw_tickets):
// awaiting_weighing (theo tấn: chờ cân đủ mọi ghe) → pending_confirmation → approved (ghi đúng
// MỘT khoản phải trả, hạn 15 ngày, đóng băng) → paid; cancelled có lý do (đã trả không hủy được —
// dùng quyết toán điều chỉnh; đã duyệt chỉ hoàn bằng bút toán đảo).
type SettlementStatus string

// Trạng thái quyết toán.
const (
	SettlementAwaitingWeighing    SettlementStatus = "awaiting_weighing"
	SettlementPendingConfirmation SettlementStatus = "pending_confirmation"
	SettlementApproved            SettlementStatus = "approved"
	SettlementPaid                SettlementStatus = "paid"
	SettlementCancelled           SettlementStatus = "cancelled"
)

// ---------------------------------------------------------------------------
// Sự kiện nội bộ (ADR-0018).
// ---------------------------------------------------------------------------

// Sự kiện costing phát.
const (
	EventPeriodClosed       = "mkg.costing.period.closed"
	EventSettlementApproved = "mkg.costing.settlement.approved"
)

// Consumer của costing (hàng đợi quorum trên mkg.internal, DLQ = tên + ".dlq"). Mọi consumer
// idempotent (R-61) và chỉ ghi sổ đầu vào / đánh dấu lô cần tính lại — không tự đóng kỳ.
const (
	WeighTicketConsumer    = "mkg.costing.weigh_ticket"   // ← mkg.measurement.weigh_ticket.recorded
	ReconciliationConsumer = "mkg.costing.reconciliation" // ← mkg.measurement.reconciliation.completed (quy kết hao hụt)
	RollCountConsumer      = "mkg.costing.roll_count"     // ← mkg.fieldops.roll_count.recorded (quyết toán theo cuộn, PM-02)
	TripConsumer           = "mkg.costing.trip"           // ← mkg.logistics.trip.completed (cước đa chặng)
	DeliveryConsumer       = "mkg.costing.delivery"       // ← mkg.orders.delivery.accepted (doanh thu, giá vốn)
	JournalPushedConsumer  = "mkg.costing.journal_pushed" // ← mkg.acctbridge.journal.pushed (kỳ → posted)
)

// JobPeriodClose là loại job đóng kỳ (mẫu mục 12: 202 + job, chạy ở cmd/worker-internal,
// ảnh chụp đầu vào bất biến).
const JobPeriodClose = "costing.period_close"

// Job đóng kỳ theo mẫu mục 12: POST …/periods/{id}/close ghi (cùng giao dịch) ảnh chụp đầu vào
// bất biến + job + outbox mkg.costing.period_close.requested (KindInternal) → 202 + job_id;
// consumer PeriodCloseConsumer ở cmd/worker-internal nạp ảnh chụp, tính, ghi kết quả + phát
// mkg.costing.period.closed trong một giao dịch.
const (
	EventPeriodCloseRequested = "mkg.costing.period_close.requested"
	PeriodCloseConsumer       = "mkg.costing.period_close"
)

// PeriodCloseRequestedV1 là payload yêu cầu đóng kỳ. Worker nạp tham số và ảnh chụp từ bảng job,
// không từ sự kiện (như pf.waterway.network_build.requested).
//
//ag:internal-event mkg.costing.period_close.requested v1
type PeriodCloseRequestedV1 struct {
	JobID       uuid.UUID `json:"job_id"`
	PeriodID    uuid.UUID `json:"period_id"`
	RequestedBy uuid.UUID `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
}

// PeriodClosedV1 là payload khi một kỳ đóng xong. Người nhận nội bộ: mkg/acctbridge (dựng bút toán
// tổng hợp — đọc chi tiết qua ClosedPeriods, không qua sự kiện).
//
//ag:internal-event mkg.costing.period.closed v1
type PeriodClosedV1 struct {
	PeriodID            uuid.UUID `json:"period_id"`
	PeriodCode          string    `json:"period_code"` // YYYY-MM (tần suất tháng, mục 16)
	From                time.Time `json:"from"`
	To                  time.Time `json:"to"` // độc quyền
	CloseRunID          uuid.UUID `json:"close_run_id"`
	InputSnapshotSHA256 string    `json:"input_snapshot_sha256"`
	ResultSetSHA256     string    `json:"result_set_sha256"` // băm tập kết quả đã tổng hợp = source_hash của acctbridge
	LineCount           int       `json:"line_count"`
	ClosedBy            uuid.UUID `json:"closed_by"`
	RuleVersion         string    `json:"rule_version"`
	EventTime           time.Time `json:"event_time"`
}

// SettlementApprovedV1 là payload khi một quyết toán HTX được duyệt (hoặc quyết toán điều chỉnh).
// Người nhận nội bộ: chưa có consumer bắt buộc ở G4-0 (acctbridge đọc khoản phải trả qua
// ClosedPeriods; hàng đợi có thể thêm sau). Số tiền, giá KHÔNG BAO GIỜ qua van.
//
//ag:internal-event mkg.costing.settlement.approved v1
type SettlementApprovedV1 struct {
	SettlementID       uuid.UUID    `json:"settlement_id"`
	PeriodID           *uuid.UUID   `json:"period_id"`
	HTXRef             uuid.UUID    `json:"htx_ref"`
	CollectionOrderRef *uuid.UUID   `json:"collection_order_ref"`
	ContractRef        *uuid.UUID   `json:"contract_ref"`
	Basis              string       `json:"basis"` // per_tonne | per_bale
	Rolls              *int         `json:"rolls"`
	MassKg             *mass.Kg     `json:"mass_kg"`
	UnitPrice          string       `json:"unit_price"` // decimal 4 số
	Amount             money.Amount `json:"amount"`
	DueOn              string       `json:"due_on"` // YYYY-MM-DD (hạn 15 ngày — tạm)
	PreparedBy         uuid.UUID    `json:"prepared_by"`
	ApprovedBy         uuid.UUID    `json:"approved_by"`
	RuleVersion        string       `json:"rule_version"`
	SupersedesID       *uuid.UUID   `json:"supersedes_id"`
	EventTime          time.Time    `json:"event_time"`
}

// ---------------------------------------------------------------------------
// Cổng cho mkg/acctbridge (cùng tiến trình).
// ---------------------------------------------------------------------------

// SummaryLine là một dòng tổng hợp của kỳ đã đóng: tổng theo nhóm × chiều (không giao dịch chi tiết).
type SummaryLine struct {
	Category CostCategory
	// DimensionKind + DimensionRef: chiều tổng hợp (facility | htx | customer | mode | none); acctbridge
	// ánh xạ (Category, DimensionKind) → tài khoản theo bảng ánh xạ có phiên bản.
	DimensionKind string
	DimensionRef  *uuid.UUID
	Amount        money.Amount // dương = chi phí/doanh thu theo nhóm; điều chỉnh có thể âm
	MassKg        *mass.Kg
	Entries       int // số bản ghi nguồn đã gộp
}

// PeriodSummary là kết quả đóng kỳ bất biến.
type PeriodSummary struct {
	PeriodID            uuid.UUID
	PeriodCode          string
	From, To            time.Time
	Status              PeriodStatus
	CloseRunID          uuid.UUID
	ClosedAt            time.Time
	ClosedBy            uuid.UUID
	RuleVersion         string
	InputSnapshotSHA256 string
	ResultSetSHA256     string
	Lines               []SummaryLine
}

// ClosedPeriods cho mkg/acctbridge đọc kết quả đóng kỳ. ErrPeriodNotClosed khi kỳ chưa đóng
// (open/closing); ErrPeriodNotFound khi không có. Kết quả tất định: cùng CloseRunID → cùng
// Lines (sắp theo Category, DimensionKind, DimensionRef) và cùng ResultSetSHA256.
type ClosedPeriods interface {
	ClosedPeriod(ctx context.Context, tenantID, periodID uuid.UUID) (PeriodSummary, error)
}

// Lỗi ổn định của port (problem.* mã costing.* — docs/errors.md).
var (
	ErrPeriodNotFound  = errors.New("costing: không có kỳ")
	ErrPeriodNotClosed = errors.New("costing: kỳ chưa đóng")
)

// Tham số có phiên bản (param_version, người đặt cost_accountant ≠ người duyệt chief_accountant).
// Mặc định lấy theo prototype, TẠM, CHỜ KHÁCH (docs/dev/g4-conventions.md §1.2).
const (
	ParamAllocationKey        = "costing.allocation.key"                // mass: phân bổ chi phí chặng theo khối lượng lô (chờ khách)
	ParamMoistureBasePct      = "costing.moisture.base_pct"             // quy đổi ẩm độ — CHƯA có công thức (C-4); mặc định: không quy đổi
	ParamLossRatioPct         = "costing.loss.expected_pct"             // 5 (#19)
	ParamHubProcessingLossPct = "costing.loss.hub_processing_pct"       // 2 (#56)
	ParamHubProcessingRate    = "costing.rate.hub_processing_vnd_per_t" // 120000 (#55)
	ParamSettlementTermDays   = "costing.settlement.payment_term_days"  // 15 (proto PAYMENT_TERM_DAYS)
	ParamCarbonRetainedPct    = "costing.carbon.retained_pct"           // 45 (proto CARBON-SHARE)
	ParamCarbonSplitFarmers   = "costing.carbon.split.farmers_pct"      // 70 của phần chia 55 %
	ParamCarbonSplitAuthority = "costing.carbon.split.authority_pct"    // 15
	ParamCarbonSplitVerifier  = "costing.carbon.split.verifier_pct"     // 15 (nhận phần dư làm tròn)
)
