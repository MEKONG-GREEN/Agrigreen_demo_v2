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
	"io"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formimport"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/mail"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/render"
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
	LinkBase   string // AG_WEB_OPS_URL: liên kết trang việc xuất trong email
	Log        *slog.Logger
	// KN v7.7 P4 — báo cáo Chi cục: lần tổng hợp của pf/extension (nối sau bằng UseConsolidations nếu cần) và biến môi
	// trường của cờ Google Sheet (nil → TẮT; ADR-0026 "Đề xuất").
	Consolidations extport.Consolidations
	Getenv         func(string) string
}

// Module là pf/reports đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai reports_app. Ở worker, db là pool reports_app
// (job lịch); handler inbox dùng giao dịch do inbox.Process mở.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	svc := &app.Service{
		DB: db, Clock: clk, Log: d.Log, Scope: d.Scope, Orgs: d.Orgs, Units: d.Units, Params: d.Params, Catalogs: d.Catalogs,
		Store: d.Store, Extension: d.Extension, Mechanization: d.Mechanization, CropCycle: d.CropCycle, Directory: d.Directory, Recipients: d.Recipients, Mailer: d.Mailer,
		LinkBase: d.LinkBase, Consolidations: d.Consolidations,
	}
	if d.Getenv != nil {
		svc.GSheet = dispatch.GSheetConfigFromEnv(d.Getenv)
	}
	return &Module{svc: svc}
}

// UseConsolidations nối cổng lần tổng hợp của pf/extension (báo cáo Chi cục, KN v7.7 P4).
func (m *Module) UseConsolidations(c extport.Consolidations) { m.svc.Consolidations = c }

// UseMailer nối bộ gửi SMTP (worker: consumer báo cáo Chi cục).
func (m *Module) UseMailer(ml port.Mailer) { m.svc.Mailer = ml }

// DispatchHandler là inbox.Handler cho pf.reports.dispatch.requested (consumer port.DispatchConsumer).
func (m *Module) DispatchHandler() inbox.Handler { return m.svc.HandleDispatchRequested }

// DispatchHistory cho pf/extension biết kỳ đã gửi Chi cục (cảnh báo khi mở lại kỳ).
func (m *Module) DispatchHistory() extport.DispatchHistory { return app.DispatchHistory{Svc: m.svc} }

// FormAvailability cho pf/party (P2): menu "Báo cáo đột xuất" chỉ hiện khi phòng ban có mẫu loại đó.
func (m *Module) FormAvailability() partyport.FormAvailability {
	return app.FormAvailability{Svc: m.svc}
}

// FormUsage cho pf/masterdata (P2): cột "biểu đang dùng" của danh mục nông nghiệp.
func (m *Module) FormUsage() mdport.FormUsage { return app.FormUsage{Svc: m.svc} }

// UseEgress (G5, ADR-0024 mục 3) nối điểm kiểm soát xuất dùng chung (pf/metering, kênh
// report_export) sau điểm kiểm soát cục bộ ở ba lối (yêu cầu, lịch, tải). cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meteringport.Checkpoint, mode meteringport.Mode) {
	m.svc.Egress, m.svc.EgressMode = cp, mode
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (mẫu biểu, lịch gửi).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Forms trả cổng mẫu báo cáo KN v7.7 cho P4 (pf/extension: phát hành, bài nộp, tổng hợp):
// tra phiên bản, kiểm + tính lưới bài nộp, dòng tổng. Nối ở cmd/api-platform/assembly và cmd/worker.
func (m *Module) Forms() port.FormEngine { return app.Forms{Svc: m.svc} }

// TablePDF là bộ dựng PDF bảng chữ cho module khác (pf/extension: "Xuất PDF" lịch công tác, KN v7.7 P5) —
// gốc lắp ráp nối qua extension.Module.UseTablePDF. Không đụng DB.
func (m *Module) TablePDF() TablePDFRenderer { return TablePDFRenderer{} }

// TablePDFRenderer hiện thực interface TablePDF của module gọi (chữ ký chỉ dùng kiểu cơ bản).
type TablePDFRenderer struct{}

// RenderTablePDF dựng PDF A4 ngang từ tiêu đề, các dòng "nhãn: giá trị", tiêu đề cột và các dòng.
func (TablePDFRenderer) RenderTablePDF(title string, meta [][2]string, header []string, rows [][]string, at time.Time) ([]byte, error) {
	return render.TablePDF(title, meta, header, rows, at)
}

// ImportFormXLSX nạp một sheet Excel thành định nghĩa lưới nháp (không DB) — dùng cho
// cmd/formimport khi khách gửi tệp gốc (Q-01).
func ImportFormXLSX(r io.Reader, o port.FormImportOptions) (port.FormDefinition, port.FormImportReport, error) {
	return formimport.Import(r, o)
}

// ExportFormXLSX dựng tệp mẫu Excel đúng bố cục của một định nghĩa.
func ExportFormXLSX(def port.FormDefinition) ([]byte, error) { return formimport.Export(def) }

// TrongTrotSeedDefinitions trả 14 định nghĩa mẫu Phòng Trồng trọt khởi tạo (T01–T07, M01, V01–V06).
func TrongTrotSeedDefinitions() []port.FormDefinition {
	seeds := form.TrongTrotSeeds()
	out := make([]port.FormDefinition, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, s.Def)
	}
	return out
}

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
