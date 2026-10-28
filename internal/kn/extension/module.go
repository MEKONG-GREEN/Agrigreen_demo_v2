// Package extension là module nghiệp vụ khuyến nông pf/extension (ADR-0011):
// nhiệm vụ hỗ trợ có SLA và leo thang, chỉ đạo khẩn có xác nhận đã đọc, báo cáo
// hiện trường có phiên bản, dashboard 4 KPI theo phạm vi, danh bạ trực.
//
// Module khác chỉ được import extension/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform và cmd/worker.
package extension

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/app"
	alertapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/app"
	alerthandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/handler"
	calapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/app"
	calhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/handler"
	diaryapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/app"
	diaryhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/handler"
	zmapapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/app"
	zmaphandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/handler"
	modelapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/app"
	modelhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/handler"
	dhapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/handler"
	khtcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/app"
	khtchandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/handler"
	kaapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/app"
	matapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/material/app"
	mathandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/material/handler"
	obapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/app"
	obhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	rchandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/handler"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "extension_app"

// JobEscalation là tên job leo thang SLA trong cmd/worker (-jobs).
const JobEscalation = "extension_escalation"

// JobWorkSchedule là tên job lịch công tác (khóa tuần đã kết thúc, nhắc hạn đăng ký / hạn duyệt — KN v7.7 P5).
const JobWorkSchedule = "extension_work_schedule"

// Deps là port của module khác mà extension cần. Thiếu port nào thì thao tác cần
// nó trả 503 extension.dependency_unavailable (mặc định chặn), riêng Params thiếu
// thì dùng giá trị mặc định (SLA 24 giờ).
type Deps struct {
	Scope    partyport.ScopeResolver // phạm vi TW → Tỉnh → Xã (ADR-0009)
	Orgs     partyport.Orgs          // kiểm HTX, tìm TTKN tỉnh nhận leo thang
	Units    partyport.AdminUnits    // tỉnh của xã
	Params   params.Params           // extension.task_sla_hours, extension.duty_status_ttl_hours
	Catalogs mdport.Catalogs         // giống lúa, vụ, sâu bệnh (FR-01 BR-01)
	// Plots, Coops (tùy chọn): cổng thống kê hẹp do KN sở hữu; gốc lắp ráp nối bộ đọc publication v1.
	Plots port.PlotStatistics
	Coops port.CooperativeStatistics
}

// Module là pf/extension đã nối.
type Module struct {
	svc *app.Service
	rc  *rcapp.Service  // KN v7.7 P4: vòng đời báo cáo (extension/reportcycle, module_reportcycle.go)
	mat *matapp.Service // KN v7.7 P7 FN-XA-03.06: sổ vật tư (extension/material)
	// W11.61 (D4 KH1): Danh mục KHTC, danh sách CT/DA chủ (D20), Kế hoạch năm (extension/khtc).
	khtc *khtcapp.Service

	khtcPeriods *khtcPeriodService // W11.62: Kỳ KHTC (extension/khtcperiod, module_khtc.go)
	// PFPUB/D2: trạng thái xác nhận HTX cho số liệu đã chốt (module_closedfig.go).
	htx port.HTXConfirmations
	// W11.49 (D4 CN9): Báo cáo dịch bệnh BM-10, báo nghi dịch (extension/outbreak).
	ob *obapp.Service
	// W11.64: bộ đọc nguồn module của Kỳ KHTC (extension/khtcsource, module_khtcsource.go).
	khtcSrc *khtcSourceService
	// W11.63: phiếu Báo cáo cho KHTC (extension/khtcform, module_khtc.go).
	khtcForms   *khtcFormService
	khtcReports *khtcReportService
	// W11.43 (D4 CN3): quy tắc liên biểu + 12 cảnh báo CNTS, vòng đời cảnh báo, nhắc nộp theo biểu (module_cntsalert.go).
	alert *alertapp.Service
	// W11.46 (D4 CN6): lịch trực KSGM BM-03, lịch tiêm phòng BM-07 (extension/cntscal).
	cal *calapp.Service
	// W11.47 (D4 CN7): CT/DA, điểm trình diễn, hộ mô hình, cấp quyền CNTS (extension/cntsmodel, module_cntsmodel.go).
	model *modelapp.Service
	// W11.48 (D4 CN8): nhật ký nuôi BM-08/09/19 (extension/cntsdiary, module_cntsdiary.go).
	diary *diaryapp.Service
	// W11.52 (D4 CN12): bản đồ vùng nuôi, vùng dịch (extension/cntsmap, module_cntsmap.go).
	zmap *zmapapp.Service
	// W11.54 (D4 CN14): trang chủ phòng + kho văn bản phòng (extension/depthome, module_depthome.go).
	home *dhapp.Service
	// W11.50 (D4 CN10): chiến dịch điều tra mở rộng (extension/cntscampaign, module_cntscampaign.go).
	campaigns *campaignService
	// BF-SRV-KN-ATTRS (#119): hồ sơ KN riêng — năm sản xuất máy (TT-10), cây trồng thửa (TT-11) (module_knattrs.go).
	attrs *kaapp.Service
}

// New tạo module với pool của vai extension_app.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	pool := db
	db = forModule(db)
	m := &Module{svc: &app.Service{
		DB: db, Clock: clk, Scope: d.Scope, Orgs: d.Orgs, Units: d.Units, Params: d.Params, Catalogs: d.Catalogs,
		Plots: d.Plots, Coops: d.Coops,
	}, rc: &rcapp.Service{DB: db, Clock: clk, Orgs: d.Orgs, Units: d.Units, Params: d.Params}}
	// Tên người thao tác của sổ vật tư dùng chung danh bạ của vòng đời báo cáo (UseReportCycle nối sau).
	m.mat = &matapp.Service{DB: db, Clock: clk, Catalogs: d.Catalogs, Units: d.Units, Names: func() partyport.ActorNames { return m.rc.Names }}
	m.khtc = &khtcapp.Service{DB: db, Clock: clk, Names: func() partyport.ActorNames { return m.rc.Names }}
	m.ob = &obapp.Service{DB: db, Clock: clk, Units: d.Units, Names: func() partyport.ActorNames { return m.rc.Names }}
	m.alert = &alertapp.Service{DB: db, Clock: clk, Params: d.Params, Units: d.Units, Names: func() partyport.ActorNames { return m.rc.Names }}
	m.cal = &calapp.Service{DB: db, Clock: clk, Units: d.Units, Names: func() partyport.ActorNames { return m.rc.Names }}
	m.model = &modelapp.Service{DB: db, Clock: clk, Units: d.Units, Programs: khtcapp.Ports{Svc: m.khtc},
		Names: func() partyport.ActorNames { return m.rc.Names }}
	m.diary = &diaryapp.Service{DB: db, Clock: clk, Units: d.Units, Params: d.Params, Alerts: m.alert,
		Names: func() partyport.ActorNames { return m.rc.Names }}
	m.zmap = &zmapapp.Service{DB: db, Clock: clk, Units: d.Units, Plots: d.Plots, Names: func() partyport.ActorNames { return m.rc.Names }}
	m.rc.BM11Programs = m.bm11Programs // W11.45: kết quả CT/DA cho Báo cáo phòng BM-11
	m.home = m.newDeptHome()
	m.attrs = newKNAttrs(pool, db, clk, d.Catalogs)
	return m
}

// UseContacts nối cổng số điện thoại cán bộ (pf/accounts) sau khi module đó được dựng.
func (m *Module) UseContacts(c accport.StaffContacts) { m.svc.Contacts = c }

// UseFarmers nối cổng liên kết tài khoản nông dân ↔ nông hộ (pf/accounts) — yêu cầu
// hỗ trợ từ App HTX (HTX US-SUPPORT-01 → KN US-TASK-01). Gốc lắp ráp gọi sau khi dựng pf/accounts.
func (m *Module) UseFarmers(f accport.FarmerHouseholds) { m.svc.Farmers = f }

// UseGpsLog nối log GPS của pf/geo (consumer port.GpsLogConsumer, HTX US-GPS-03).
func (m *Module) UseGpsLog(g geoport.GpsLog) { m.svc.GpsLog = g }

// UseKNContext nối ngữ cảnh phiên KN và danh bạ gán vai trò của P1 (pf/accounts) cho lịch công tác (KN v7.7 P5).
// Chưa nối → suy từ vai realm + phạm vi đơn vị, danh bạ trực pf_extension.staff.
func (m *Module) UseKNContext(c accport.KNContextResolver, d accport.KNDirectory) {
	m.svc.KNContext, m.svc.KNDirectory = c, d
}

// UseRoleHolders (W11.46) nối người mang cờ nhiệm vụ của pf/accounts: người theo dõi lịch công tác (quy tắc duyệt
// "follower" của Chăn nuôi – Thủy sản) nhận thông báo lịch gửi duyệt và nhắc duyệt.
func (m *Module) UseRoleHolders(h accport.RoleHolders) { m.svc.Holders = h }

// UseEgress (quyết định A5, ADR-0024) nối điểm kiểm soát xuất (pf/metering) cho "Xuất Excel/PDF" lịch công tác —
// xuất nội bộ: ghi egress_event, không bao giờ bị chặn kể cả AG_EGRESS_MODE=enforce.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.svc.Egress, m.svc.EgressMode, m.svc.Log = cp, mode, log
	if m.khtcReports != nil {
		m.khtcReports.Egress, m.khtcReports.EgressMode = cp, mode
	}
	m.khtc.Egress, m.khtc.EgressMode, m.khtc.Log = cp, mode, log // W11.61: "Xuất Excel" Kế hoạch năm
}

// UseCNTSCatalogs nối danh mục CNTS theo ngày của pf/masterdata (W11.41) cho Báo cáo dịch bệnh BM-10 (W11.49: bệnh,
// vật nuôi). Chưa nối → lập/cập nhật ổ dịch, báo nghi dịch trả 503 extension.dependency_unavailable.
func (m *Module) UseCNTSCatalogs(c port.CNTSCatalogs) {
	m.ob.Catalogs, m.cal.Catalogs, m.model.Catalogs, m.zmap.Catalogs = c, c, c, c
	m.diary.Catalogs = c // BF-CN-CATALOG #170: ngưỡng mặc định theo danh mục chỉ tiêu theo dõi
}

// UseXaScopes (W11.46) nối danh sách cán bộ Cấp Xã CNTS kèm phạm vi (pf/accounts) cho lưới lịch trực KSGM BM-03.
// W11.47: cùng danh bạ cho người được cấp quyền, cán bộ theo dõi điểm, người ghi nhật ký của hộ mô hình.
func (m *Module) UseXaScopes(x accport.XaScopes) { m.cal.Officers, m.model.Officers = x, x }

// UseTablePDF nối bộ dựng PDF bảng (pf/reports) cho "Xuất PDF" lịch công tác.
func (m *Module) UseTablePDF(p port.TablePDF) { m.svc.TablePDF = p }

// GpsLogHandler là inbox.Handler của consumer port.GpsLogConsumer (vai extension_app).
func (m *Module) GpsLogHandler() inbox.Handler { return m.svc.HandleGpsLog }

// UseContactNames (W11.23, Q-072 — đề xuất của dev: Contact thay danh bạ trực): tên cán bộ đọc từ Contact dùng chung
// (hồ sơ nhân sự + kho định danh) TRƯỚC; cột display_name dạng rõ của pf_extension.staff chỉ còn là nguồn dự phòng cho
// tài khoản chưa có hồ sơ nhân sự hoặc lời gọi không có người dùng (job).
func (m *Module) UseContactNames(n partyport.ActorNames) { m.svc.ContactNames = n }

// StaffNames cho module khác tra tên cán bộ trong danh bạ (qua port, G-07/G-09).
func (m *Module) StaffNames() mdport.StaffNames { return m.svc }

// RegisterApp gắn route Application API (gồm vòng đời báo cáo KN v7.7 — cần header X-AG-Context).
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	handler.Handler{Svc: m.svc}.RegisterApp(rg)
	rchandler.Handler{Svc: m.rc}.RegisterApp(rg)
	mathandler.Handler{Svc: m.mat}.RegisterApp(rg)
	khtchandler.Handler{Svc: m.khtc}.RegisterApp(rg)
	m.registerKHTCPeriods(rg) // W11.62
	obhandler.Handler{Svc: m.ob}.RegisterApp(rg)
	m.registerKHTCReports(rg)                          // W11.66
	m.registerKHTCSources(rg)                          // W11.64
	alerthandler.Handler{Svc: m.alert}.RegisterApp(rg) // W11.43
	calhandler.Handler{Svc: m.cal}.RegisterApp(rg)
	modelhandler.Handler{Svc: m.model}.RegisterApp(rg) // W11.47
	diaryhandler.Handler{Svc: m.diary}.RegisterApp(rg) // W11.48
	zmaphandler.Handler{Svc: m.zmap}.RegisterApp(rg)   // W11.52
	m.registerDeptHome(rg)                             // W11.54
	m.registerCNTSCampaigns(rg)                        // W11.50
	m.registerKNAttrs(rg)                              // BF-SRV-KN-ATTRS
}

// KHTCPrograms là cổng đọc danh sách CT/DA chủ của KHTC (quyết định D20): CNTS (W11.47) gắn điểm/hộ, thu – chi tham
// chiếu program_ref qua cổng này.
func (m *Module) KHTCPrograms() port.KHTCPrograms { return khtcapp.Ports{Svc: m.khtc} }

// KHTCPlans là cổng đọc Kế hoạch năm theo ngày hiệu lực (W11.62 kỳ KHTC, W11.63 phiếu, W11.65 mẫu).
func (m *Module) KHTCPlans() port.KHTCPlans { return khtcapp.Ports{Svc: m.khtc} }

// RegisterAdmin: module chưa có route quản trị (tham số SLA đặt qua pf/masterdata).
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// SyncSource cho pf/sync: chiều xuống "task", "directive".
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// Staff là cổng đọc danh bạ cán bộ (chỉ mã tham chiếu + đơn vị) cho pf/notify.
func (m *Module) Staff() notifyport.StaffDirectory { return app.StaffDirectory{Svc: m.svc} }

// SyncSink cho pf/sync: chiều lên "task_update", "field_report".
func (m *Module) SyncSink() syncport.Sink { return app.SyncSink{Svc: m.svc} }

// ReportData cho pf/reports: đọc dữ liệu báo cáo FR-01..FR-05 theo phạm vi đã giải.
func (m *Module) ReportData() port.ReportData { return app.ReportData{Svc: m.svc} }

// OverdueTenants dựng bộ tìm tenant có nhiệm vụ quá hạn từ pool vai
// ag_ops_jobs (worker, nối thẳng).
func OverdueTenants(pool *pgxpool.Pool) app.TenantLister { return app.OverdueTenants(pool) }

// Jobs trả job định kỳ của module cho bộ lập lịch của worker (chỉ chạy ở leader):
// leo thang SLA mỗi 15 phút, chạy bù khi khởi động. Job idempotent.
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobEscalation, Spec: schedule.Every{Interval: 15 * time.Minute}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			n, err := m.svc.RunEscalation(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("leo thang nhiệm vụ quá SLA", "event", "job.extension_escalation", "escalated", n, "outcome", outcome)
			}
			return err
		},
	}}
}

// ScheduleTenants dựng bộ tìm tenant cho job lịch công tác từ pool vai ag_ops_jobs (worker, nối thẳng).
func ScheduleTenants(pool *pgxpool.Pool) app.TenantLister { return app.ScheduleTenants(pool) }

// ScheduleJobs trả job lịch công tác (KN v7.7 P5) cho bộ lập lịch của worker (chỉ chạy ở leader): mỗi 15 phút
// ghi sự việc khóa cho tuần đã kết thúc, nhắc cán bộ chưa gửi lịch trước hạn đăng ký, nhắc người duyệt khi quá hạn
// duyệt. Job idempotent.
func (m *Module) ScheduleJobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobWorkSchedule, Spec: schedule.Every{Interval: 15 * time.Minute}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.RunScheduleJob(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("lịch công tác: khóa tuần, nhắc hạn", "event", "job.extension_work_schedule", "locked", r.Locked,
					"reminders", r.Reminders, "checkin_points_purged", r.Purged, "outcome", outcome)
			}
			return err
		},
	}}
}

// ScheduleJobRun chạy job lịch công tác cho một tenant ngay lúc này (kiểm thử, chạy tay).
func (m *Module) ScheduleJobRun(ctx context.Context, tid uuid.UUID) (app.ScheduleJobResult, error) {
	return m.svc.ScheduleJobTenant(ctx, tid, m.svc.Clock.Now().UTC())
}

// MetricsSource cho pf/metrics (qua gốc lắp ráp): 4 KPI KN US-DASH-01 như hệ biết
// lúc hết ngày D (sổ + recorded_at < Cutoff).
func (m *Module) MetricsSource() metricsport.Source { return app.MetricsSource{Svc: m.svc} }
