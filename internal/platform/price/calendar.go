package price

// W11.74: lịch báo giá, giờ khóa, nhắc, ngưỡng — port cho W11.75 và job price_calendar của cmd/worker.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// Calendar cho module khác (và W11.75): kế hoạch ngày báo giá, giờ khóa, hạn phản hồi.
func (m *Module) Calendar() port.Calendar { return m.svc }

// Thresholds cho W11.75: ngưỡng cảnh báo đang hiệu lực, tham số chung.
func (m *Module) Thresholds() port.Thresholds { return m.svc }

// UseReminderTargets thay bộ chọn người nhận nhắc nhập giá. Mặc định (New, W11.72): cán bộ Xã phụ trách từng xã báo
// giá; lần 2 bỏ xã đã gửi khi W11.75 nối PointHooks.Submitted. nil → thông báo chủ đề tới cán bộ xã của tenant.
func (m *Module) UseReminderTargets(t port.ReminderTargets) { m.svc.Reminders = t }

// CalendarTenants dựng bộ tìm tenant cho job lịch từ pool vai ag_ops_jobs (worker, nối thẳng).
func CalendarTenants(pool *pgxpool.Pool) app.CalendarTenantLister { return app.CalendarTenants(pool) }

// CalendarJobs trả job lịch báo giá (chỉ chạy ở leader): mỗi phút, mốc khóa (±1 phút — FN-AD-04.04 AC-01) và nhắc
// nhập giá (FN-XA-03.04 BR-03). RunOnStart bắt kịp lần lỡ khi đổi leader. Job idempotent.
func (m *Module) CalendarJobs(tenants app.CalendarTenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobCalendar, Spec: schedule.Every{Interval: time.Minute}, RunOnStart: true, Timeout: 50 * time.Second,
		Run: func(ctx context.Context) error {
			r, err := m.svc.RunCalendarJob(ctx, tenants)
			if log != nil && (err != nil || r.Locked+r.Reminders > 0) {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("lịch báo giá: khóa, nhắc", "event", "job."+port.JobCalendar, "locked", r.Locked, "reminders", r.Reminders,
					"events", r.Events, "outcome", outcome)
			}
			return err
		},
	}}
}
