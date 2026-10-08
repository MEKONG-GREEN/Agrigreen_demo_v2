// Package port là cửa công khai DUY NHẤT của module pf/metering (R-02):
//
//   - sổ đo lường sử dụng `usage_event` — CHỨNG TỪ theo mẫu sổ mục 8, một dòng mỗi lời gọi Public
//     API đã xác thực (tài liệu kiến trúc mục 11, 16; ADR-0023);
//   - bộ đếm hạn mức thẩm quyền (cập nhật cùng giao dịch với sổ);
//   - ĐIỂM KIỂM SOÁT XUẤT DỮ LIỆU duy nhất của vùng nền tảng (mục 18 DLP; ADR-0024): van công bố
//     (phía nhận), Public API, xuất gói MRV, xuất báo cáo, bề mặt /v1/partner/*;
//   - bảng kê sử dụng theo kỳ tháng (mẫu mục 12; người lập ≠ người duyệt, R-56).
//
// Hợp đồng sổ sử dụng (Call, Ledger, Quotas, Usage) và điểm kiểm soát xuất (Egress, Checkpoint, Guard) nằm ở
// internal/shared/meter để module ở mọi tầng gọi mà phụ thuộc chỉ đi xuống (R-MOD-05); pf/metering hiện thực
// chúng. Gói này giữ phần riêng của module: vai, khóa tham số DLP, sự kiện, bảng kê.
//
// Tiền: shared/money (Price numeric(18,4), Amount numeric(18,2)) — agnofloat cấm float trong mọi
// package internal/platform/metering/** (R-10).
//
// ĐÓNG BĂNG ở G5-0: đổi chữ ký / ngữ nghĩa qua người tích hợp; thêm phương thức vào interface MỚI;
// thêm trường DTO chỉ khi tương thích ngược.
package port

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// DBRole là vai DB của module ở cụm pf (schema pf_metering, pf 000080/000082).
const DBRole = "metering_app"

// Producer là giá trị producer của phong bì sự kiện.
const Producer = "pf/metering"

// Vai nghiệp vụ (realm Keycloak; bề mặt Admin API — MFA).
const (
	// RoleMeteringViewer: xem sổ sử dụng (tenant đơn vị vận hành), sổ xuất dữ liệu và cảnh báo bất
	// thường (tenant chủ dữ liệu — RLS), bảng kê.
	RoleMeteringViewer = "metering_viewer"
	// RoleBillingOfficer: yêu cầu lập bảng kê kỳ (202 + job), đề xuất tham số giá (pf/masterdata).
	RoleBillingOfficer = "billing_officer"
	// RoleBillingApprover: duyệt bảng kê (≠ người yêu cầu — R-56, app/ + CHECK DB), duyệt tham số giá.
	RoleBillingApprover = "billing_approver"
)

// Khóa tham số DLP (pf/masterdata, có phiên bản, setter ≠ approver; "tạm, chờ khách").
const (
	ParamEgressDailyQuotaFmt     = "metering.egress.%s.daily_records_quota" // 0 = không giới hạn
	ParamEgressAnomalyFactorFmt  = "metering.egress.%s.anomaly_factor"      // × trung vị 28 ngày
	ParamEgressAnomalyRecordsFmt = "metering.egress.%s.anomaly_records"     // ngưỡng tuyệt đối khi < 7 ngày mẫu
	AnomalyBaselineDays          = 28
	AnomalyMinSampleDays         = 7
)

// EgressParam trả khóa tham số của một kênh, vd EgressParam(ParamEgressDailyQuotaFmt, ChannelValve).
func EgressParam(format string, ch meter.Channel) string { return fmt.Sprintf(format, string(ch)) }

// Sự kiện DLP.
const (
	// EventEgressAnomaly: lần ĐẦU khối lượng ngày (tenant dữ liệu × kênh × bên nhận) vượt ngưỡng.
	EventEgressAnomaly = "pf.metering.egress.anomaly_detected"
	// NotifyEgressAnomalyQueue: consumer của pf/notify báo quản trị nền tảng tenant chủ dữ liệu.
	NotifyEgressAnomalyQueue = "pf.notify.egress_anomaly"
)

// EgressAnomalyV1 là payload cảnh báo khối lượng xuất bất thường (tenant phong bì = chủ dữ liệu).
// Chỉ mã tham chiếu.
//
//ag:event pf.metering.egress.anomaly_detected v1
type EgressAnomalyV1 struct {
	EgressEventID    string `json:"egress_event_id"`
	Channel          string `json:"channel"`
	Product          string `json:"product"`
	CounterpartyKind string `json:"counterparty_kind"`
	CounterpartyRef  string `json:"counterparty_ref"`
	BusinessDate     string `json:"business_date"`
	DailyRecords     int64  `json:"daily_records"`
	Threshold        int64  `json:"threshold"`
	Basis            string `json:"basis"` // median | absolute
	Mode             string `json:"mode"`
	DetectedAt       string `json:"detected_at"`
}

// ---------------------------------------------------------------------------
// Bảng kê theo kỳ (ADR-0023 mục 4)
// ---------------------------------------------------------------------------

// StatementStatus: draft → approved (đóng băng); sai = bảng kê mới supersedes (RB-1).
type StatementStatus string

// Trạng thái bảng kê.
const (
	StatementDraft    StatementStatus = "draft"
	StatementApproved StatementStatus = "approved"
)

// Khóa tham số giá (pf/masterdata; TÙY CHỌN — chưa đặt → bảng kê chỉ có đơn vị; chờ khách).
const (
	ParamPricePerUnitFmt      = "metering.price.%s.%s.per_unit_vnd"            // plan, product → Price
	ParamPriceSubscriptionFmt = "metering.price.%s.subscription_vnd_per_month" // plan → Amount
)

// StatementLine là một dòng bảng kê: (sản phẩm × lớp trạng thái) của một khách hàng trong kỳ.
type StatementLine struct {
	Product      string
	StatusClass  string // 2xx | 4xx | 5xx
	Calls        int64
	Units        int64
	UnitPrice    *money.Price  // nil khi chưa có tham số giá
	Amount       *money.Amount // nil khi chưa có tham số giá
	PriceVersion string
}

// Statement là bảng kê của một khách hàng một kỳ tháng — chứng từ lập hóa đơn ngoài hệ thống.
type Statement struct {
	ID             uuid.UUID
	ClientID       uuid.UUID
	ClientTenantID uuid.UUID
	Period         string // YYYY-MM giờ VN
	Status         StatementStatus
	Lines          []StatementLine
	Subscription   *money.Amount
	Total          *money.Amount
	FirstSeq       int64
	LastSeq        int64
	ChainHeadHash  string // hex row_hash dòng cuối của stream kỳ
	SnapshotSHA256 string
	RuleVersion    string
	RequestedBy    uuid.UUID
	ApprovedBy     uuid.UUID // Nil khi draft
	SupersedesID   uuid.UUID // Nil khi không bù
}

// Sự kiện và consumer bảng kê (mẫu mục 12).
const (
	EventStatementRequested = "pf.metering.statement.requested"
	StatementConsumer       = "pf.metering.statement"
)

// StatementRequestedV1 là yêu cầu lập bảng kê. Worker nạp phạm vi từ bảng job. ClientID rỗng = mọi
// khách hàng của tenant đơn vị vận hành.
//
//ag:event pf.metering.statement.requested v1
type StatementRequestedV1 struct {
	JobID       string  `json:"job_id"`
	Period      string  `json:"period"`
	ClientID    *string `json:"client_id"`
	RequestedBy string  `json:"requested_by"`
	RequestedAt string  `json:"requested_at"`
}

// ---------------------------------------------------------------------------
// Lỗi (docs/errors.md, mục pf/metering)
// ---------------------------------------------------------------------------

var (
	// ErrSelfApproval: người duyệt bảng kê là người yêu cầu lập (R-56).
	ErrSelfApproval = problem.New(http.StatusForbidden, "metering.self_approval", "Không được duyệt bảng kê do chính mình yêu cầu")
	// ErrStatementFrozen: bảng kê đã duyệt không sửa — lập bảng kê bù.
	ErrStatementFrozen = problem.New(http.StatusConflict, "metering.statement_frozen", "Bảng kê đã duyệt — lập bảng kê bù")
	// ErrPeriodNotEnded: kỳ chưa kết thúc (giờ VN).
	ErrPeriodNotEnded = problem.New(http.StatusConflict, "metering.period_not_ended", "Kỳ chưa kết thúc")
)
