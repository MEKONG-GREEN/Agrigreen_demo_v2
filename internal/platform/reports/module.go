// Package reports là module pf/reports: mẫu biểu có phiên bản (Admin quản lý),
// xuất báo cáo Excel/PDF bất đồng bộ (mục 12, R-52) từ cổng đọc của pf/extension
// (KN FR-01..FR-05, US-DASH-02), pf/mechanization (CGH FN-10, từ kết quả cân đối
// ĐÃ LƯU) và pf/cropcycle (sản lượng HTX — HTX US-DASH-02/03), điểm kiểm soát xuất dữ liệu duy nhất (mục 18 DLP) và lịch gửi báo cáo
// định kỳ qua email (US-DASH-03).
//
// Module khác chỉ được import reports/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform (assembly) và cmd/worker.
package reports

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/mail"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "reports_app"

// JobSchedules là tên job lịch gửi báo cáo trong cmd/worker (-jobs).
const JobSchedules = "reports_schedules"

// Deps là phụ thuộc của module. Thiếu cổng nào thì thao tác cần nó trả 503
// reports.dependency_unavailable; thiếu Directory/Mailer thì bỏ gửi email kèm cảnh báo.
type Deps struct {
	Scope         partyport.ScopeResolver
	Orgs          partyport.Orgs
	Units         mdport.AdminUnits
	Params        mdport.Params // reports.daily_export_quota, reports.export_anomaly_threshold
	Catalogs      mdport.Catalogs
	Store         objstore.Store // reports/<tenant>/<job>.<định dạng>
	Extension     extport.ReportData
	Mechanization mechport.ReportData
	CropCycle     ccport.ReportData  // pf/cropcycle: báo cáo sản lượng HTX (HTX US-DASH-02, US-DASH-03)
	Directory     port.MailDirectory // pf/accounts hiện thực; nil → bỏ gửi email
	// Recipients: tài khoản của HTX để quản lý HTX chọn người nhận lịch (HTX US-DASH-03); nil → 503.
	Recipients port.RecipientDirectory
	Mailer     port.Mailer
	LinkBase      string // AG_WEB_OPS_URL: liên kết trang việc xuất trong email
	Log           *slog.Logger
}

// Module là pf/reports đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai reports_app. Ở worker, db là pool reports_app
// (job lịch); handler inbox dùng giao dịch do inbox.Process mở.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	return &Module{svc: &app.Service{
		DB: db, Clock: clk, Log: d.Log, Scope: d.Scope, Orgs: d.Orgs, Units: d.Units, Params: d.Params, Catalogs: d.Catalogs,
		Store: d.Store, Extension: d.Extension, Mechanization: d.Mechanization, CropCycle: d.CropCycle, Directory: d.Directory, Recipients: d.Recipients, Mailer: d.Mailer,
		LinkBase: d.LinkBase,
	}}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (mẫu biểu, lịch gửi).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// ExportHandler là inbox.Handler cho pf.reports.export.requested (consumer port.ExportConsumer).
func (m *Module) ExportHandler() inbox.Handler { return m.svc.HandleExportRequested }

// DeliveryHandler là inbox.Handler cho pf.reports.export.completed (consumer port.DeliveryConsumer).
func (m *Module) DeliveryHandler() inbox.Handler { return m.svc.HandleExportCompleted }

// ScheduleTenants dựng bộ tìm tenant có lịch đang dùng từ pool vai ag_outbox_publisher.
func ScheduleTenants(pool *pgxpool.Pool) app.TenantLister { return app.ScheduleTenants(pool) }

// Jobs trả job lịch gửi cho bộ lập lịch của worker (chỉ leader): mỗi 15 phút,
// chạy bù khi khởi động. Idempotent theo (lịch, mốc).
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobSchedules, Spec: schedule.Every{Interval: 15 * time.Minute}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			n, err := m.svc.RunSchedules(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("chạy lịch gửi báo cáo", "event", "job."+JobSchedules, "fired", n, "outcome", outcome)
			}
			return err
		},
	}}
}

// SMTPFromEnv dựng bộ gửi email từ AG_SMTP_*; chưa cấu hình → nil (bỏ gửi kèm cảnh báo).
func SMTPFromEnv(clk clock.Clock) (port.Mailer, error) {
	cfg, err := mail.ConfigFromEnv()
	if errors.Is(err, mail.ErrNotConfigured) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &mail.SMTP{Cfg: cfg, Clock: clk}, nil
}
