package hrm

// W11.25 — nối nâng bậc lương (FN-09, FN-10): cổng ghi Word (W10.17), job hrm_promotion_scan cho cmd/worker (thông báo kết
// quả qua outbox — module_notify.go).

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// JobPromotionScan là tên job tính mốc nâng bậc hằng ngày trong cmd/worker (-jobs).
const JobPromotionScan = app.JobPromotionScan

// UsePromotion nối cổng ghi Word Mẫu số 1/2/4 (W10.17 — nil: chỉ Excel). Thông báo kết quả job cho Phòng HCTH đi qua outbox
// (sự kiện pf.hrm.promotion_due.detected cùng giao dịch với sổ — NOTIFY-HCTH-KN #50), không qua cổng gọi trực tiếp.
func (m *Module) UsePromotion(w port.PromotionWriter) {
	m.svc.Promo = app.PromotionConfig{Writer: w}
}

// PromotionTenants trả bộ tìm tenant cho job từ pool vai ag_ops_jobs (worker, nối thẳng — chỉ đọc cột tenant_id).
func PromotionTenants(pool *pgxpool.Pool) func(context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) { return store.ScanTenants(ctx, pool) }
}

// PromotionJobs trả job tính mốc (chỉ leader): mỗi giờ một lượt, mỗi tenant ghi tối đa một dòng promotion_scan mỗi ngày.
func (m *Module) PromotionJobs(tenants func(context.Context) ([]uuid.UUID, error), log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobPromotionScan, Spec: schedule.Every{Interval: time.Hour}, RunOnStart: true, Timeout: 15 * time.Minute,
		Run: func(ctx context.Context) error {
			ts, err := tenants(ctx)
			if err != nil {
				return err
			}
			n, err := m.svc.PromotionScanAll(ctx, ts, log)
			if log != nil {
				log.Info("tính mốc nâng bậc NLTX/TNVK", "event", "job."+JobPromotionScan, "tenants", len(ts), "scanned", n)
			}
			return err
		},
	}}
}

// PromotionScanRun chạy job cho một tenant tại now (kiểm thử, chạy tay).
func (m *Module) PromotionScanRun(ctx context.Context, tid uuid.UUID, now time.Time) (app.ScanResult, error) {
	return m.svc.PromotionScan(ctx, tid, now)
}
