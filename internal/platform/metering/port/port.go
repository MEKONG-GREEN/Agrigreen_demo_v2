// Package port là cửa công khai DUY NHẤT của module pf/metering (R-02):
//
//   - sổ đo lường sử dụng `usage_event` — CHỨNG TỪ theo mẫu sổ mục 8, một dòng mỗi lời gọi Public
//     API đã xác thực (tài liệu kiến trúc mục 11, 16; ADR-0023);
//   - bộ đếm hạn mức thẩm quyền (cập nhật cùng giao dịch với sổ);
//   - ĐIỂM KIỂM SOÁT XUẤT DỮ LIỆU duy nhất của vùng nền tảng (mục 18 DLP; ADR-0024): van công bố
//     (phía nhận), Public API, xuất gói MRV, xuất báo cáo, bề mặt /v1/partner/*;
//   - bảng kê sử dụng theo kỳ tháng (mẫu mục 12; người lập ≠ người duyệt, R-56).
//
// Tiền: shared/money (Price numeric(18,4), Amount numeric(18,2)) — agnofloat cấm float trong mọi
// package internal/platform/metering/** (R-10).
//
// ĐÓNG BĂNG ở G5-0: đổi chữ ký / ngữ nghĩa qua người tích hợp; thêm phương thức vào interface MỚI;
// thêm trường DTO chỉ khi tương thích ngược.
package port

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

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

// ---------------------------------------------------------------------------
// Sổ sử dụng (ADR-0023)
// ---------------------------------------------------------------------------

// RuleUnitsV1 là phiên bản quy tắc tính đơn vị ghim vào rule_version của dòng sổ.
const RuleUnitsV1 = "metering.units.v1"

// CallKind phân loại lời gọi để tính đơn vị (không suy từ URL).
type CallKind string

// Loại lời gọi.
const (
	CallRead          CallKind = "read"           // đọc sản phẩm dữ liệu
	CallExportRequest CallKind = "export_request" // POST yêu cầu xuất (202)
	CallJobStatus     CallKind = "job_status"     // GET /v1/jobs/{id}
	CallFileDownload  CallKind = "file_download"  // GET /v1/jobs/{id}/file
	CallMeta          CallKind = "meta"           // /v1/data-products, /v1/usage
)

// Call là một lời gọi Public API đã xác thực, ghi MỘT dòng usage_event. Không có bí mật khóa,
// không có giá trị truy vấn, không dữ liệu cá nhân.
type Call struct {
	RequestID       uuid.UUID // idem_key của dòng sổ (UUIDv7 sinh ở api-public)
	ReceivedAt      time.Time // event_time
	ClientID        uuid.UUID
	ClientTenantID  uuid.UUID
	KeyPrefix       string
	AuthMethod      string
	Kind            CallKind
	Product         string // rỗng với CallMeta
	ContractVersion int    // 0 khi không phải sản phẩm
	Scope           string
	RouteTemplate   string    // "GET /v1/data/collection-volumes" — MẪU route
	Status          int       // mã HTTP trả về
	ProblemCode     string    // mã RFC 9457 nếu lỗi
	DataTenantID    uuid.UUID // bên cấp đã phục vụ; uuid.Nil khi không phục vụ dữ liệu
	Rows            int64
	Bytes           int64
	Latency         time.Duration
	Replayed        bool // gọi lặp idempotent (R-51) → 0 đơn vị
	JobID           uuid.UUID
	TraceID         string
}

// UnitsV1 là quy tắc tính đơn vị metering.units.v1 (ADR-0023 mục 2): đọc 2xx = 1; yêu cầu xuất
// được nhận = 0; tải tệp = ceil(rows/1000), tối thiểu 1; lỗi và gọi lặp = 0.
func UnitsV1(c Call) int64 {
	if c.Replayed || c.Status < 200 || c.Status > 299 {
		return 0
	}
	switch c.Kind {
	case CallRead:
		return 1
	case CallFileDownload:
		if c.Rows <= 0 {
			return 1
		}
		return (c.Rows + 999) / 1000
	}
	return 0
}

// Recorded là kết quả ghi sổ.
type Recorded struct {
	EventID          uuid.UUID
	Units            int64
	RuleVersion      string
	DailyRemaining   int64
	MonthlyRemaining int64
	WindowReset      time.Time // đầu ngày kế tiếp (giờ VN) — cho RateLimit-Reset / Retry-After
}

// Ledger ghi sổ sử dụng. Record chạy trong MỘT giao dịch của metering_app: dòng usage_event (tenant
// đơn vị vận hành), cập nhật bộ đếm, và — khi DataTenantID ≠ Nil — dòng egress_event kênh
// ChannelPublicAPI ở tenant chủ dữ liệu (ADR-0024). api-public gọi SAU khi dựng phản hồi, TRƯỚC khi
// gửi byte; lỗi → 503 ErrUnavailable, không gửi dữ liệu. Vượt hạn mức → dòng vẫn được ghi (status 429,
// units 0) và trả ErrQuotaExceeded. Idempotent theo RequestID.
type Ledger interface {
	Record(ctx context.Context, operatorTenant uuid.UUID, c Call) (Recorded, error)
}

// Remaining là hạn mức còn lại (đọc bộ đếm đã commit — không giữ chỗ).
type Remaining struct {
	DailyUnits   int64
	MonthlyUnits int64
	WindowReset  time.Time
}

// Quotas cho api-public kiểm SỚM trước khi dựng phản hồi (tránh tốn công); thẩm quyền cuối vẫn là
// Ledger.Record trong cùng giao dịch với sổ.
type Quotas interface {
	Remaining(ctx context.Context, operatorTenant, clientID uuid.UUID, product string, at time.Time) (Remaining, error)
}

// UsageDay là tổng sử dụng một ngày × sản phẩm của một khách hàng (GET /v1/usage).
type UsageDay struct {
	Date    string // YYYY-MM-DD giờ VN
	Product string
	Calls   int64
	Units   int64
}

// Usage đọc tổng sử dụng của một khách hàng (chỉ của chính nó — client_id lấy từ khóa).
type Usage interface {
	UsageOf(ctx context.Context, operatorTenant, clientID uuid.UUID, from, to time.Time) ([]UsageDay, error)
}

// ---------------------------------------------------------------------------
// Điểm kiểm soát xuất dữ liệu — DLP (ADR-0024)
// ---------------------------------------------------------------------------

// Channel là đường xuất dữ liệu.
type Channel string

// Kênh xuất (mục 18 + bề mặt /v1/partner/* của ADR-0020).
const (
	ChannelPublicAPI    Channel = "public_api"
	ChannelMRVPackage   Channel = "mrv_package"
	ChannelReportExport Channel = "report_export"
	ChannelValve        Channel = "valve"
	ChannelPartnerShare Channel = "partner_share"
)

// Channels là mọi kênh hợp lệ (khớp CHECK của pf_metering.egress_event).
var Channels = []Channel{ChannelPublicAPI, ChannelMRVPackage, ChannelReportExport, ChannelValve, ChannelPartnerShare}

// CounterpartyKind là loại bên nhận dữ liệu.
type CounterpartyKind string

// Loại bên nhận.
const (
	CounterpartyAPIClient CounterpartyKind = "api_client" // khách hàng Public API
	CounterpartyTenant    CounterpartyKind = "tenant"     // tenant bên cấp (van) / bên nhận (/v1/partner/*)
	CounterpartyActor     CounterpartyKind = "actor"      // người dùng nội bộ xuất báo cáo / gói MRV (actor_ref)
)

// Egress là MỘT lần dữ liệu rời đi qua điểm kiểm soát. Chỉ mã tham chiếu và số lượng — không dữ liệu
// cá nhân, không giá trị truy vấn.
type Egress struct {
	// SourceID: mã thao tác nguồn để idempotent — request_id (Public API), job_id (xuất), event_id
	// của phong bì (van). Một (Channel, SourceID, CounterpartyRef) ghi đúng một dòng.
	SourceID         uuid.UUID
	Channel          Channel
	DataTenantID     uuid.UUID // chủ dữ liệu rời đi = tenant của dòng egress_event
	CounterpartyKind CounterpartyKind
	CounterpartyRef  uuid.UUID
	// Product: sản phẩm dữ liệu / tên hợp đồng van / template báo cáo / "mrv_package".
	Product         string
	ContractVersion int
	Records         int64
	Bytes           int64
	OccurredAt      time.Time
	ActorRef        uuid.UUID // người/khách hàng gây ra (Nil với van)
	TraceID         string
	// Internal (quyết định A5, 03/10/2026): xuất NỘI BỘ của cán bộ trong phạm vi được giao (xuất lịch công tác,
	// tải tệp thửa KN). Điểm kiểm soát vẫn ghi egress_event, đếm và phát hiện bất thường như mọi lần xuất,
	// nhưng LUÔN xử lý ở chế độ observe — không bao giờ từ chối, kể cả AG_EGRESS_MODE=enforce (Guard và
	// Checkpoint.Pass đều tôn trọng). Mặc định false: hành vi cũ, kênh ra ngoài vẫn bị chặn ở enforce.
	Internal bool
}

// EffectiveMode là chế độ áp cho một lần xuất: Internal → luôn ModeObserve.
func (e Egress) EffectiveMode(mode Mode) Mode {
	if e.Internal {
		return ModeObserve
	}
	return mode
}

// Decision là quyết định của điểm kiểm soát.
type Decision struct {
	Allowed      bool
	Reason       string // "", "quota_exceeded"
	Anomaly      bool   // vượt ngưỡng bất thường (lần đầu trong ngày phát EventEgressAnomaly)
	DailyRecords int64  // sau lần này
	DailyQuota   int64  // 0 = không giới hạn
	Mode         Mode
}

// Checkpoint là ĐIỂM KIỂM SOÁT DUY NHẤT: ghi egress_event (chuỗi băm), đếm, so hạn mức + ngưỡng
// bất thường. Pass KHÔNG tự từ chối ở ModeObserve (Allowed luôn true); ở ModeEnforce, vượt hạn mức →
// Allowed = false. Lỗi trả về là lỗi hạ tầng (DB) — dùng Guard để áp ngữ nghĩa observe/enforce thống
// nhất. Idempotent theo (Channel, SourceID, CounterpartyRef).
type Checkpoint interface {
	Pass(ctx context.Context, e Egress) (Decision, error)
}

// Mode là chế độ của điểm kiểm soát (AG_EGRESS_MODE).
type Mode string

// Chế độ.
const (
	ModeObserve Mode = "observe" // mặc định ĐTK 1: ghi, đếm, cảnh báo, không bao giờ từ chối
	ModeEnforce Mode = "enforce" // Phase K: vượt hạn mức → từ chối; checkpoint lỗi → thao tác thất bại
)

// EnvEgressMode là biến môi trường chọn chế độ.
const EnvEgressMode = "AG_EGRESS_MODE"

// ParseMode đọc AG_EGRESS_MODE; rỗng → ModeObserve.
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.TrimSpace(s)) {
	case "", ModeObserve:
		return ModeObserve, nil
	case ModeEnforce:
		return ModeEnforce, nil
	}
	return "", fmt.Errorf("metering: %s=%q không hợp lệ (observe | enforce)", EnvEgressMode, s)
}

// Guard áp ngữ nghĩa thống nhất của ADR-0024 cho mọi đường xuất (trừ Public API, luôn đóng khi lỗi
// qua Ledger.Record):
//   - cp == nil → nil (chưa nối — hành vi cũ, kiểm thử hiện có không đổi);
//   - lỗi hạ tầng: observe → log event=egress outcome=checkpoint_error, trả nil; enforce →
//     ErrEgressUnavailable;
//   - !Allowed: enforce → ErrEgressQuotaExceeded; observe → nil.
//
// Van: lỗi trả về làm consumer giao lại → DLQ (R-62).
//
// e.Internal (A5): xuất nội bộ → áp như observe (ghi, đếm, cảnh báo, không bao giờ từ chối).
func Guard(ctx context.Context, cp Checkpoint, mode Mode, e Egress, log *slog.Logger) error {
	if cp == nil {
		return nil
	}
	mode = e.EffectiveMode(mode)
	d, err := cp.Pass(ctx, e)
	if err != nil {
		if mode == ModeEnforce {
			return ErrEgressUnavailable.WithCause(err)
		}
		if log == nil {
			log = slog.Default()
		}
		log.WarnContext(ctx, "điểm kiểm soát xuất lỗi — chế độ observe, tiếp tục", "event", "egress", "channel", string(e.Channel),
			"source_id", e.SourceID, "outcome", "checkpoint_error", "error", err.Error())
		return nil
	}
	if !d.Allowed && mode == ModeEnforce {
		return ErrEgressQuotaExceeded
	}
	return nil
}

// Khóa tham số DLP (pf/masterdata, có phiên bản, setter ≠ approver; "tạm, chờ khách").
const (
	ParamEgressDailyQuotaFmt     = "metering.egress.%s.daily_records_quota" // 0 = không giới hạn
	ParamEgressAnomalyFactorFmt  = "metering.egress.%s.anomaly_factor"      // × trung vị 28 ngày
	ParamEgressAnomalyRecordsFmt = "metering.egress.%s.anomaly_records"     // ngưỡng tuyệt đối khi < 7 ngày mẫu
	AnomalyBaselineDays          = 28
	AnomalyMinSampleDays         = 7
)

// EgressParam trả khóa tham số của một kênh, vd EgressParam(ParamEgressDailyQuotaFmt, ChannelValve).
func EgressParam(format string, ch Channel) string { return fmt.Sprintf(format, string(ch)) }

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
	// ErrUnavailable: không ghi được sổ sử dụng — Public API KHÔNG trả dữ liệu (đóng khi lỗi).
	ErrUnavailable = problem.New(http.StatusServiceUnavailable, "metering.unavailable", "Không ghi được sổ đo lường sử dụng — không trả dữ liệu")
	// ErrQuotaExceeded: vượt hạn mức đơn vị ngày/tháng của gói (Retry-After tới đầu cửa sổ kế tiếp).
	ErrQuotaExceeded = problem.New(http.StatusTooManyRequests, "metering.quota_exceeded", "Vượt hạn mức sử dụng của gói")
	// ErrEgressQuotaExceeded: điểm kiểm soát xuất (enforce) từ chối — vượt hạn mức kênh.
	ErrEgressQuotaExceeded = problem.New(http.StatusTooManyRequests, "metering.egress_quota_exceeded", "Vượt hạn mức xuất dữ liệu của kênh")
	// ErrEgressUnavailable: điểm kiểm soát xuất lỗi ở chế độ enforce — không xuất.
	ErrEgressUnavailable = problem.New(http.StatusServiceUnavailable, "metering.egress_unavailable", "Điểm kiểm soát xuất dữ liệu không sẵn sàng — không xuất")
	// ErrSelfApproval: người duyệt bảng kê là người yêu cầu lập (R-56).
	ErrSelfApproval = problem.New(http.StatusForbidden, "metering.self_approval", "Không được duyệt bảng kê do chính mình yêu cầu")
	// ErrStatementFrozen: bảng kê đã duyệt không sửa — lập bảng kê bù.
	ErrStatementFrozen = problem.New(http.StatusConflict, "metering.statement_frozen", "Bảng kê đã duyệt — lập bảng kê bù")
	// ErrPeriodNotEnded: kỳ chưa kết thúc (giờ VN).
	ErrPeriodNotEnded = problem.New(http.StatusConflict, "metering.period_not_ended", "Kỳ chưa kết thúc")
)
