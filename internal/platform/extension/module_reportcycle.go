package extension

// Nối vòng đời báo cáo KN v7.7 (gói P4, extension/reportcycle): phát hành biểu, công việc, bài nộp, rà soát,
// chốt/mở lại kỳ, tổng hợp các xã, dashboard Tỉnh/TW, báo cáo của xã. Gốc lắp ráp gọi UseReportCycle sau khi dựng
// pf/reports (FormEngine), pf/party (khu vực) và pf/accounts (gán vai trò).

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	rcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/app"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/extensionsrc"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// ReportCycleDeps là port của module khác cho vòng đời báo cáo. Thiếu Forms/Regions/Assignments → 503
// extension.dependency_unavailable ở thao tác cần; Names, Dispatches tùy chọn.
type ReportCycleDeps struct {
	Forms       repport.FormEngine
	Regions     partyport.Regions
	Assignments accport.Assignments
	Names       partyport.ActorNames
	Log         *slog.Logger
}

// UseReportCycle nối port cho vòng đời báo cáo.
func (m *Module) UseReportCycle(d ReportCycleDeps) {
	m.rc.Forms, m.rc.Regions, m.rc.Assignments, m.rc.Names, m.rc.Log = d.Forms, d.Regions, d.Assignments, d.Names, d.Log
}

// UseDispatchHistory nối cổng "kỳ đã gửi Chi cục" của pf/reports (cảnh báo khi mở lại kỳ).
func (m *Module) UseDispatchHistory(h extensionsrc.DispatchHistory) { m.rc.Dispatches = h }

// UseFormsFreezer (W6.2) nối cap/forms: mỗi lần chốt (thủ công, tự động, chốt lại) và mở lại kỳ KN còn ghi song song
// sự kiện + ảnh chụp đóng băng vào pf_forms, cùng giao dịch. Nil → không ghi.
func (m *Module) UseFormsFreezer(f formsport.Freezer) { m.rc.Freezer = f }

// ReportSyncSource cho pf/sync (KN v7.7 P7): chiều xuống report_task, form_layout, report_submission của các xã phụ
// trách trong ngữ cảnh Cấp Xã — field-app nhập báo cáo ngoại tuyến (N-07).
func (m *Module) ReportSyncSource() syncport.Source { return rcapp.ReportSync{Svc: m.rc} }

// ReportSyncSink cho pf/sync (KN v7.7 P7): chiều lên report_submission (lưu nháp/gửi tạo ngoại tuyến, id UUIDv7 máy sinh).
func (m *Module) ReportSyncSink() syncport.Sink { return rcapp.ReportSink{Svc: m.rc} }

// Consolidations là cổng đọc lần tổng hợp cho pf/reports (báo cáo Chi cục, FN-TI-02.06).
func (m *Module) Consolidations() extensionsrc.Consolidations { return rcapp.Consolidations{Svc: m.rc} }

// OpenPeriodsImpact là dòng "Kỳ báo cáo chưa chốt" của bảng Ảnh hưởng (P2 FN-AD-04.02).
func (m *Module) OpenPeriodsImpact() partyport.OrgImpactSource { return rcapp.OrgImpact{Svc: m.rc} }

// ReportUsage đếm công việc báo cáo theo phòng ban (P2 FN-AD-04.03: chặn "xóa" phòng ban đã có dữ liệu).
func (m *Module) ReportUsage() partyport.DepartmentUsage { return rcapp.DepartmentUsage{Svc: m.rc} }

// ReportCycleTenants dựng bộ tìm tenant có kỳ báo cáo từ pool vai ag_ops_jobs (worker, nối thẳng).
func ReportCycleTenants(pool *pgxpool.Pool) rcapp.TenantLister { return rcapp.PeriodTenants(pool) }

// ReportCycleJobs trả job vòng đời báo cáo (chỉ leader): mỗi 5 phút tự chốt kỳ quá giờ chốt (kèm tổng hợp "Hệ
// thống"), tự chốt lại xã/biểu hết hạn mở, tự nhắc trước hạn/khi trễ. Idempotent.
func (m *Module) ReportCycleJobs(tenants rcapp.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobReportCycle, Spec: schedule.Every{Interval: 5 * time.Minute}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.rc.RunJob(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("vòng đời báo cáo: tự chốt, chốt lại, nhắc", "event", "job."+JobReportCycle, "closed", r.Closed, "reclosed", r.Reclosed,
					"reminders", r.Reminders, "outcome", outcome)
			}
			return err
		},
	}}
}

// ReportCycleJobRun chạy job vòng đời báo cáo cho một tenant tại now (kiểm thử, chạy tay).
func (m *Module) ReportCycleJobRun(ctx context.Context, tid uuid.UUID, now time.Time) (rcapp.JobResult, error) {
	return m.rc.JobTenant(ctx, tid, now)
}

// JobReportCycle là tên job vòng đời báo cáo KN v7.7 trong cmd/worker (-jobs).
const JobReportCycle = "extension_report_cycle"
