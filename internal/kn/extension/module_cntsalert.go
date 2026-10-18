package extension

// Nối quy tắc liên biểu + 12 cảnh báo CNTS (W11.43 — D4 CN3, extension/cntsalert): cổng đọc bài nộp cap/forms, danh bạ
// cán bộ (pf/accounts) cho người nhận, consumer port.CNTSAlertConsumer, job extension_cnts_alert.

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	alertapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/app"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// JobCNTSAlert là tên job cảnh báo CNTS trong cmd/worker (-jobs): quá hạn xử lý, nhắc nộp, trễ liên tiếp.
const JobCNTSAlert = "extension_cnts_alert"

// CNTSAlertEvents là sự kiện consumer port.CNTSAlertConsumer đăng ký.
var CNTSAlertEvents = alertapp.Events

// CNTSAlertDeps là port của module khác cho cảnh báo CNTS. Feed nil → consumer trả 503 (giao lại), job không nhắc;
// Directory/Holders nil → thông báo theo chủ đề (vai × đơn vị), không email.
type CNTSAlertDeps struct {
	Feed      formsport.SubmissionFeed
	Directory accport.KNDirectory
	Holders   accport.RoleHolders
}

// UseCNTSAlerts nối cảnh báo CNTS.
func (m *Module) UseCNTSAlerts(d CNTSAlertDeps) {
	m.alert.Feed = d.Feed
	if d.Directory != nil || d.Holders != nil {
		m.alert.Dir = alertDirectory{dir: d.Directory, holders: d.Holders}
	}
}

// CNTSAlertHandler là inbox.Handler của consumer port.CNTSAlertConsumer (vai extension_app).
func (m *Module) CNTSAlertHandler() inbox.Handler { return m.alert.HandleEvent }

// CNTSAlertTenants dựng bộ tìm tenant cho job từ pool vai ag_ops_jobs (worker, nối thẳng).
func CNTSAlertTenants(pool *pgxpool.Pool) alertapp.TenantLister { return alertapp.JobTenants(pool) }

// CNTSAlertJobs trả job cảnh báo CNTS (chỉ leader): mỗi 5 phút báo Quá hạn xử lý, nhắc nộp biểu kỳ, trễ liên tiếp.
func (m *Module) CNTSAlertJobs(tenants alertapp.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobCNTSAlert, Spec: schedule.Every{Interval: 5 * time.Minute}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.alert.RunJob(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("cảnh báo CNTS: quá hạn xử lý, nhắc nộp, trễ liên tiếp", "event", "job."+JobCNTSAlert, "overdue", r.Overdue,
					"reminders", r.Reminders, "streaks", r.Streaks, "outcome", outcome)
			}
			return err
		},
	}}
}

// CNTSAlertJobRun chạy job cảnh báo CNTS cho một tenant tại now (kiểm thử, chạy tay).
func (m *Module) CNTSAlertJobRun(ctx context.Context, tid uuid.UUID, now time.Time) (alertapp.JobResult, error) {
	return m.alert.JobTenant(ctx, tid, now)
}

// alertDirectory giải người nhận từ sổ gán vai trò của pf/accounts (phòng CNTS).
type alertDirectory struct {
	dir     accport.KNDirectory
	holders accport.RoleHolders
}

func (d alertDirectory) Officers(ctx context.Context, tid uuid.UUID, province string) ([]alertapp.Officer, error) {
	if d.dir == nil {
		return nil, nil
	}
	os, err := d.dir.Officers(ctx, tid, alertapp.DeptCNTS, province)
	if err != nil {
		return nil, err
	}
	out := make([]alertapp.Officer, 0, len(os))
	for _, o := range os {
		out = append(out, alertapp.Officer{ActorRef: o.ActorRef, Level: o.Level, Communes: slices.Clone(o.Communes),
			Manager: o.Level == accport.ScheduleLevelProvince && accport.IsManager(o.MgmtLevel, "")})
	}
	return out, nil
}

func (d alertDirectory) FormOwners(ctx context.Context, tid uuid.UUID) ([]uuid.UUID, error) {
	if d.holders == nil {
		return nil, nil
	}
	return d.holders.DutyHolders(ctx, tid, authz.KNTinh, alertapp.DeptCNTS, authz.DutyFormOwner)
}
