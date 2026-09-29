// Package port là giao diện công khai DUY NHẤT của pf/reports (mẫu biểu, xuất
// báo cáo Excel/PDF bất đồng bộ, lịch gửi định kỳ) mà module khác được import (R-02).
package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// Vai của module (tên vai trong Keycloak). Vai xuất theo nguồn dữ liệu dùng lại
// vai của module nguồn (extension/port, mechanization/port).
const (
	// RoleReportsAdmin quản lý mẫu biểu (CGH FN-10 BR-03: chỉ Admin quản lý mẫu báo cáo). Admin danh mục
	// (masterdata_admin) và platform_admin cũng được quản lý mẫu.
	RoleReportsAdmin = "reports_admin"
	// RoleMasterdataAdmin: vai Admin danh mục & cấu hình của pf/masterdata.
	RoleMasterdataAdmin = mdport.RoleMasterdataAdmin
)

// Khóa tham số pf/masterdata (tra theo thời điểm, mục 16).
const (
	// ParamDailyExportQuota: số lần xuất tối đa của một người trong một ngày (giờ Việt Nam). Mặc định 50.
	ParamDailyExportQuota = "reports.daily_export_quota"
	// ParamAnomalyThreshold: vượt số lần xuất trong ngày này → sự kiện pf.reports.export.anomaly. Mặc định 30.
	ParamAnomalyThreshold = "reports.export_anomaly_threshold"
)

// Nguồn dữ liệu của mẫu biểu.
const (
	SourceSowing          = "extension.sowing"
	SourceGrowthPest      = "extension.growth_pest"
	SourceProgramProgress = "extension.program_progress"
	SourceTasksSLA        = "extension.tasks_sla"
	SourcePeriodSummary   = "extension.period_summary"
	SourceCGHSummary      = "mechanization.summary"
	SourceCGHShortage     = "mechanization.shortage_by_coop"
	SourceCGHCompare      = "mechanization.season_compare"
	// SourceHTXProduction: sản lượng dự kiến – thực tế, diện tích, số hộ, tiến độ SOP theo
	// HTX × vụ × giống từ pf/cropcycle (HTX US-DASH-02, US-DASH-03).
	SourceHTXProduction = "cropcycle.production"
)

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventExportRequested = "pf.reports.export.requested"
	EventExportCompleted = "pf.reports.export.completed"
	EventExportAnomaly   = "pf.reports.export.anomaly"
	Producer             = "pf/reports"
)

// Consumer của worker (shared.inbox).
const (
	// ExportConsumer dựng tệp cho pf.reports.export.requested.
	ExportConsumer = "pf.reports.export"
	// DeliveryConsumer gửi email lịch báo cáo khi pf.reports.export.completed.
	DeliveryConsumer = "pf.reports.delivery"
)

// ExportRequestedV1 là payload của pf.reports.export.requested. Worker nạp bộ
// lọc và phạm vi đã giải từ bảng export_job, không từ sự kiện. Không PII.
//
//ag:event pf.reports.export.requested v1
type ExportRequestedV1 struct {
	JobID       string `json:"job_id"`
	TemplateKey string `json:"template_key"`
	Format      string `json:"format"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
}

// ExportCompletedV1 là payload của pf.reports.export.completed.
//
//ag:event pf.reports.export.completed v1
type ExportCompletedV1 struct {
	JobID       string  `json:"job_id"`
	TemplateKey string  `json:"template_key"`
	Format      string  `json:"format"`
	ObjectKey   string  `json:"object_key"`
	SHA256      string  `json:"sha256"`
	SizeBytes   int64   `json:"size_bytes"`
	RowCount    int     `json:"row_count"`
	Provisional bool    `json:"provisional"`
	ScheduleID  *string `json:"schedule_id"`
	CompletedAt string  `json:"completed_at"`
}

// ExportAnomalyV1 là payload của pf.reports.export.anomaly: một người xuất vượt
// ngưỡng bất thường trong ngày (mục 18 DLP). Consumer cảnh báo bộ phận an ninh.
// Chỉ mã tham chiếu, không PII.
//
//ag:event pf.reports.export.anomaly v1
type ExportAnomalyV1 struct {
	ActorRef     string `json:"actor_ref"`
	BusinessDate string `json:"business_date"`
	DailyCount   int    `json:"daily_count"`
	Threshold    int    `json:"threshold"`
	Quota        int    `json:"quota"`
	LastJobID    string `json:"last_job_id"`
	TemplateKey  string `json:"template_key"`
	Kind         string `json:"kind"`
	DetectedAt   string `json:"detected_at"`
}

// ErrNoEmail: tài khoản không có địa chỉ email đã xác minh.
var ErrNoEmail = errors.New("reports: tài khoản không có email")

// MailDirectory tra địa chỉ email của tài khoản nền tảng lúc gửi (US-DASH-03).
// pf/accounts hiện thực; pf/reports không lưu email. Tenant truyền tường minh
// vì job gửi chạy ở worker.
type MailDirectory interface {
	EmailOf(ctx context.Context, tenantID, actorRef uuid.UUID) (string, error)
}

// Message là một email gửi đi: không đính kèm (tệp có thể chứa dữ liệu nhạy
// cảm) — chỉ liên kết tải ngắn hạn.
type Message struct {
	To      string `pii:"true"`
	Subject string
	Text    string
}

// Mailer gửi email (hiện thực SMTP có TLS: reports.SMTPFromEnv).
type Mailer interface {
	Send(ctx context.Context, m Message) error
}
