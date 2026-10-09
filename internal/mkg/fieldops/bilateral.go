package fieldops

// Luồng song phương MKG ↔ HTX qua cổng tích hợp (W5.6 — ADR-0034, ADR-0033 §3; app/bilateral.go).

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// BilateralSync là kết quả một lượt của job luồng song phương.
type BilateralSync = app.BilateralSync

// BilateralComparison là kết quả đối soát tổng kiểm ngày.
type BilateralComparison = app.BilateralComparison

// NewGatewayBilateral tạo port.BilateralGateway qua cổng tích hợp (khóa API của khách hàng API tenant MKG có scope
// ingest:collection_schedule:write, ingest:roll_count_statement:write, feed:htx_roll_confirmation:read).
func NewGatewayBilateral(cfg GatewayConfig, clk clock.Clock) port.BilateralGateway {
	return adapter.NewGatewayBilateral(cfg, func() time.Time { return clk.Now() })
}

// SyncBilateral chạy một lượt chiếu → gửi lô → nhận kết quả → kéo feed của cặp cho tenant MKG tid (job, kiểm thử).
func (m *Module) SyncBilateral(ctx context.Context, tid uuid.UUID) (BilateralSync, error) {
	return m.svc.SyncBilateral(ctx, tid)
}

// CompareBilateral đối soát tổng kiểm ngày [from, to] với cổng tích hợp (job, kiểm thử).
func (m *Module) CompareBilateral(ctx context.Context, tid uuid.UUID, from, to clock.Date) (BilateralComparison, error) {
	return m.svc.CompareBilateral(ctx, tid, from, to)
}

// BilateralJobs trả job port.JobBilateral (mỗi interval, mặc định 5 phút): một lượt SyncBilateral rồi đối soát tổng
// kiểm 2 ngày gần nhất, ghi từng ngày lệch (fieldops.bilateral_checksum_mismatch). Chưa cấu hình → không có job.
func (m *Module) BilateralJobs(tid uuid.UUID, interval time.Duration, log *slog.Logger) []schedule.Job {
	if m.svc.Bilateral == nil || tid == uuid.Nil {
		return nil
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	return []schedule.Job{{
		Name: port.JobBilateral, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 30 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.SyncBilateral(ctx, tid)
			outcome := "ok"
			if err != nil {
				outcome = "error"
			}
			log.Info("luồng song phương: gửi lô + kéo feed của cặp", "event", "job."+port.JobBilateral, "staged", r.Staged, "skipped", r.Skipped,
				"requeued", r.Requeued, "batches", r.Batches, "accepted", r.Accepted, "rejected", r.Rejected, "pairs", r.Pairs, "items", r.Items,
				"terminated", r.Terminated, "pending", r.Pending, "submitted", r.Submitted, "outcome", outcome)
			if err != nil {
				log.Error("luồng song phương lỗi", "event", "job."+port.JobBilateral, "outcome", "error", "error", err.Error())
				return err
			}
			today := clock.BusinessDate(m.svc.Clock.Now())
			from := clock.BusinessDate(clock.StartOfBusinessDate(today).Add(-24 * time.Hour))
			c, err := m.svc.CompareBilateral(ctx, tid, from, today)
			if err != nil {
				return err
			}
			for _, d := range c.Mismatches {
				log.Warn("đối soát song phương: tổng kiểm ngày lệch", "event", "fieldops.bilateral_checksum_mismatch", "contract", d.Contract,
					"htx_ref", d.HTXRef, "date", d.Date, "local_count", d.LocalCount, "remote_count", d.RemoteCount)
			}
			outcome = "match"
			if len(c.Mismatches) > 0 || c.NotInFeed > 0 {
				outcome = "diff"
			}
			log.Info("đối soát song phương: tổng kiểm ngày", "event", "fieldops.bilateral_checksum_compare", "days", c.Days, "matched", c.Matched,
				"mismatched", len(c.Mismatches), "not_in_feed", c.NotInFeed, "outcome", outcome)
			return nil
		},
	}}
}
