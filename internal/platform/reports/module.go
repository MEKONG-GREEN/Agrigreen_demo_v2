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
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formimport"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/mail"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/mechanizationsrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/render"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
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
	FormSeeds      []port.FormSeedRegistration
	Builders       []port.SourceBuilderRegistration
	Catalog        []port.CatalogRegistration
	Policies       []port.Policy
	ResolveSession port.SessionResolver
	Scope          partyport.ScopeResolver
	Orgs           partyport.Orgs
	Units          partyport.AdminUnits
	Params         params.Params // reports.daily_export_quota, reports.export_anomaly_threshold
	Catalogs       mdport.Catalogs
	Store          objstore.Store // reports/<tenant>/<job>.<định dạng>
	Mechanization  mechanizationsrc.ReportData
	CropCycle      cropcyclesrc.ReportData    // pf/cropcycle: báo cáo sản lượng HTX (HTX US-DASH-02, US-DASH-03)
	Directory      accountsport.MailDirectory // pf/accounts hiện thực; nil → bỏ gửi email
	// Recipients: tài khoản của HTX để quản lý HTX chọn người nhận lịch (HTX US-DASH-03); nil → 503.
	Recipients accountsport.RecipientDirectory
	Mailer     port.Mailer
	LinkBase   string // AG_WEB_OPS_URL: liên kết trang việc xuất trong email
	// LinkDownloadBase (A4): URL gốc công khai api-platform cho liên kết tải Chi cục (AG_REPORT_LINK_BASE).
	LinkDownloadBase string
	Log              *slog.Logger
	// KN v7.7 P4 — báo cáo Chi cục: lần tổng hợp của pf/extension (nối sau bằng UseConsolidations nếu cần) và biến môi
	// trường của cờ Google Sheet (nil → TẮT; ADR-0026 "Đề xuất").
	Consolidations port.Consolidations
	Getenv         func(string) string
	// DispatchLease (P0-WORKER-REPORTS #128): hạn nhận việc gửi email Chi cục (AG_REPORT_DISPATCH_LEASE qua
	// SendLeaseFromEnv); 0 → mặc định 15 phút.
	DispatchLease time.Duration
}

// ValidateSendLease kiểm hạn nhận việc gửi Chi cục so với hạn xử lý thông điệp của consumer (lease ≥ hạn + biên) —
// worker gọi lúc khởi động với inbox.DefaultHandlerTimeout (review #128).
func ValidateSendLease(lease, handlerTimeout time.Duration) error {
	return dispatch.ValidateSendLease(lease, handlerTimeout)
}

// SendLeaseFromEnv đọc hạn nhận việc gửi báo cáo Chi cục (AG_REPORT_DISPATCH_LEASE, mặc định 15m).
func SendLeaseFromEnv(getenv func(string) string) (time.Duration, error) {
	return dispatch.SendLeaseFromEnv(getenv)
}

// Module là pf/reports đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai reports_app. Ở worker, db là pool reports_app
// (job lịch); handler inbox dùng giao dịch do inbox.Process mở.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	db = forModule(db)
	svc := &app.Service{
		ResolveSession: d.ResolveSession,
		DB:             db, Clock: clk, Log: d.Log, Scope: d.Scope, Orgs: d.Orgs, Units: d.Units, Params: d.Params, Catalogs: d.Catalogs,
		Store: d.Store, Mechanization: d.Mechanization, CropCycle: d.CropCycle, Directory: d.Directory, Recipients: d.Recipients, Mailer: d.Mailer,
		LinkBase: d.LinkBase, LinkDownloadBase: d.LinkDownloadBase, Consolidations: d.Consolidations, DispatchLease: d.DispatchLease,
	}
	if d.FormSeeds != nil {
		svc.FormSeeds = app.NewFormSeedRegistry(d.FormSeeds...)
	}
	if d.Builders != nil {
		svc.Builders = app.NewSourceBuilderRegistry(d.Builders...)
	}
	if d.Catalog != nil {
		svc.Catalog = app.NewCatalogRegistry(d.Catalog...)
	}
	if d.Policies != nil {
		svc.Policies = app.NewPolicyRegistry(d.Policies...)
	}
	if d.Getenv != nil {
		svc.GSheet = dispatch.GSheetConfigFromEnv(d.Getenv)
	}
	return &Module{svc: svc}
}

// RequireWorkerDeps (P0-WORKER-REPORTS #128) kiểm phụ thuộc bắt buộc của consumer worker (dựng tệp, gửi email lịch,
// gửi báo cáo Chi cục): thiếu → lỗi KHỞI ĐỘNG thay vì panic giữa lúc gửi email (worker cũ dựng reports.New(nil, …)).
func (m *Module) RequireWorkerDeps() error {
	var miss []string
	if m == nil || m.svc == nil {
		return errors.New("pf/reports: module chưa dựng")
	}
	if m.svc.DB == nil {
		miss = append(miss, "DB")
	}
	if m.svc.Clock == nil {
		miss = append(miss, "Clock")
	}
	if m.svc.Store == nil {
		miss = append(miss, "Store")
	}
	if m.svc.Params == nil {
		miss = append(miss, "Params")
	}
	if len(miss) > 0 {
		return fmt.Errorf("pf/reports: thiếu phụ thuộc bắt buộc của worker: %s", strings.Join(miss, ", "))
	}
	return nil
}

// UseConsolidations nối cổng lần tổng hợp của pf/extension (báo cáo Chi cục, KN v7.7 P4).
func (m *Module) UseConsolidations(c port.Consolidations) { m.svc.Consolidations = c }

// UseLinkDownloadBase đặt URL gốc công khai của api-platform cho liên kết tải Chi cục (A4; worker: AG_REPORT_LINK_BASE).
func (m *Module) UseLinkDownloadBase(base string) { m.svc.LinkDownloadBase = base }

// RegisterPublic gắn route công khai (không đăng nhập) của Application API: liên kết tải tệp Chi cục (A4).
func (m *Module) RegisterPublic(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterPublic(rg) }

// UseMailer nối bộ gửi SMTP (worker: consumer báo cáo Chi cục).
func (m *Module) UseMailer(ml port.Mailer) { m.svc.Mailer = ml }

// UseDispatchLease đặt hạn nhận việc gửi Chi cục sau khi dựng (kiểm thử hạn ngắn; vận hành dùng Deps.DispatchLease đã qua
// SendLeaseFromEnv/ValidateSendLease).
func (m *Module) UseDispatchLease(d time.Duration) { m.svc.DispatchLease = d }

// DispatchHandler là inbox.Handler cho pf.reports.dispatch.requested (consumer port.DispatchConsumer).
func (m *Module) DispatchHandler() inbox.Handler { return m.svc.HandleDispatchRequested }

// DispatchHistory cho pf/extension biết kỳ đã gửi Chi cục (cảnh báo khi mở lại kỳ).
func (m *Module) DispatchHistory() port.DispatchHistory {
	return app.DispatchHistory{Svc: m.svc}
}

// FormAvailability cho pf/party (P2): menu "Báo cáo đột xuất" chỉ hiện khi phòng ban có mẫu loại đó.
func (m *Module) FormAvailability() partyport.FormAvailability {
	return app.FormAvailability{Svc: m.svc}
}

// FormUsage cho pf/masterdata (P2): cột "biểu đang dùng" của danh mục nông nghiệp.
func (m *Module) FormUsage() mdport.FormUsage { return app.FormUsage{Svc: m.svc} }

// DepartmentUsage cho pf/party (W11.02): số mẫu báo cáo và lần gửi Chi cục theo phòng ban (FN-AD-04.03).
func (m *Module) DepartmentUsage() partyport.DepartmentUsage { return app.DepartmentUsage{Svc: m.svc} }

// UseEgress (G5, ADR-0024 mục 3) nối điểm kiểm soát xuất dùng chung (pf/metering, kênh
// report_export) sau điểm kiểm soát cục bộ ở ba lối (yêu cầu, lịch, tải). cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode) {
	m.svc.Egress, m.svc.EgressMode = cp, mode
}

// Middleware reauthorizes policy-enabled export replays before the common HTTP
// idempotency cache. The assembly orders all module middleware before that cache.
func (m *Module) Middleware() gin.HandlerFunc {
	return handler.Handler{Svc: m.svc}.ExportAuthorization()
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

// ExportHandler là inbox.Handler cho pf.reports.export.requested (consumer port.ExportConsumer).
func (m *Module) ExportHandler() inbox.Handler { return m.svc.HandleExportRequested }

// DeliveryHandler là inbox.Handler cho pf.reports.export.completed (consumer port.DeliveryConsumer).
func (m *Module) DeliveryHandler() inbox.Handler { return m.svc.HandleExportCompleted }

// ScheduleTenants dựng bộ tìm tenant có lịch đang dùng từ pool vai ag_ops_jobs.
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

// SMTPFromEnv dựng bộ gửi email từ AG_MAILER + AG_SMTP_* (SMTPFromEnvWith(os.Getenv, …)).
func SMTPFromEnv(clk clock.Clock) (port.Mailer, error) { return SMTPFromEnvWith(os.Getenv, clk) }

// SMTPFromEnvWith (P0-NOTIFY-FAILCLOSED): kênh email chưa cấu hình → nil, và app ghi lượt gửi là "Bỏ gửi"/"Gửi lỗi"
// kèm lý do (không bao giờ "Đã gửi"). AG_MAILER: ""(tự chọn theo AG_SMTP_HOST) | smtp (bắt buộc AG_SMTP_HOST) | log
// (chỉ máy phát triển; pf/reports không có bộ gửi giả → nil) | disabled (nil). Giá trị khác, hoặc log ngoài máy phát
// triển → lỗi, worker từ chối dựng module.
func SMTPFromEnvWith(getenv func(string) string, clk clock.Clock) (port.Mailer, error) {
	switch m := strings.ToLower(strings.TrimSpace(getenv("AG_MAILER"))); m {
	case "disabled":
		return nil, nil
	case "log":
		if !residency.LocalDev(getenv) {
			return nil, errors.New("reports: AG_MAILER=log chỉ dùng trên máy phát triển (AG_ENV=development|dev|local|test)")
		}
		return nil, nil
	case "", "smtp":
		cfg, err := mail.ConfigFrom(getenv)
		if errors.Is(err, mail.ErrNotConfigured) {
			if m == "smtp" {
				return nil, errors.New("reports: AG_MAILER=smtp nhưng chưa đặt AG_SMTP_HOST")
			}
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &mail.SMTP{Cfg: cfg, Clock: clk}, nil
	default:
		return nil, fmt.Errorf("reports: AG_MAILER=%q — chỉ nhận smtp, log, disabled", m)
	}
}

// LinkBasesFromEnv đọc URL gốc cho liên kết trong email báo cáo: web = AG_WEB_OPS_URL (dòng "đăng nhập để tải lại"),
// download = AG_REPORT_LINK_BASE (liên kết tải Chi cục). Ngoài máy phát triển, giá trị thiếu/không tuyệt đối/localhost
// → "" (fail closed: email lịch bỏ dòng tải lại; gửi Chi cục kiểu liên kết → "Gửi lỗi" nêu AG_REPORT_LINK_BASE) và lỗi
// trong errs để worker cảnh báo lúc khởi động.
func LinkBasesFromEnv(getenv func(string) string) (web, download string, errs []error) {
	web, err := residency.PublicBaseURL(getenv, "AG_WEB_OPS_URL")
	if err != nil {
		errs = append(errs, err)
	}
	download, err = residency.PublicBaseURL(getenv, "AG_REPORT_LINK_BASE")
	if err != nil {
		errs = append(errs, err)
	}
	return web, download, errs
}
