package fieldops

// Bộ điều hợp feed danh bạ HTX/thửa qua cổng tích hợp (W5.5 — ADR-0033 §2, §5; app/directoryfeed.go).

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// GatewayConfig là cấu hình feed danh bạ qua cổng tích hợp (khóa API của khách hàng API tenant MKG).
type GatewayConfig = adapter.GatewayConfig

// NewGatewayDirectoryFeed tạo port.DirectoryFeed qua GET /v1/feeds/{feed} của cổng tích hợp.
func NewGatewayDirectoryFeed(cfg GatewayConfig) port.DirectoryFeed {
	return adapter.NewGatewayFeed(cfg)
}

// SyncDirectoryFeeds chạy một lượt kéo feed danh bạ cho tenant MKG tid (job, kiểm thử).
func (m *Module) SyncDirectoryFeeds(ctx context.Context, tid uuid.UUID) ([]app.DirectoryFeedSync, error) {
	return m.svc.SyncDirectoryFeeds(ctx, tid)
}

// CompareDirectorySources đối soát bản sao cũ (/v1/partner/*) với bản sao feed cho tenant tid (job, kiểm thử).
func (m *Module) CompareDirectorySources(ctx context.Context, tid uuid.UUID) (app.DirectoryComparison, error) {
	return m.svc.CompareDirectorySources(ctx, tid)
}

// DirectoryFeedJobs trả job port.JobDirectoryFeed (mỗi interval, mặc định 15 phút): kéo hai feed danh bạ, ghi cảnh báo
// khi dữ liệu quá max_age / bị thu hồi, rồi đối soát với bản sao cũ và ghi từng khác biệt
// (fieldops.directory_source_diff — chạy song song một kỳ, ADR-0033 §7). Chưa cấu hình feed / tenant → không có job.
func (m *Module) DirectoryFeedJobs(tid uuid.UUID, interval time.Duration, log *slog.Logger) []schedule.Job {
	if m.svc.DirectoryFeed == nil || tid == uuid.Nil {
		return nil
	}
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	return []schedule.Job{{
		Name: port.JobDirectoryFeed, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 30 * time.Minute,
		Run: func(ctx context.Context) error {
			rs, err := m.svc.SyncDirectoryFeeds(ctx, tid)
			for _, r := range rs {
				outcome := "ok"
				switch {
				case r.Revoked:
					outcome = "revoked"
				case r.Stale:
					outcome = "stale"
				}
				lvl := slog.LevelInfo
				if outcome != "ok" {
					lvl = slog.LevelWarn
				}
				log.Log(ctx, lvl, "kéo feed danh bạ HTX/thửa", "event", "job."+port.JobDirectoryFeed, "feed", r.Feed, "pages", r.Pages,
					"upserts", r.Upserts, "retracts", r.Retracts, "caught_up", r.CaughtUp, "as_of", r.AsOf, "max_age", r.MaxAge,
					"resynced", r.Resynced, "cleared", r.Cleared, "source", m.svc.DirectorySource, "outcome", outcome)
			}
			if err != nil {
				log.Error("kéo feed danh bạ lỗi", "event", "job."+port.JobDirectoryFeed, "outcome", "error", "error", err.Error())
				return err
			}
			c, cerr := m.svc.CompareDirectorySources(ctx, tid)
			if cerr != nil {
				return cerr
			}
			for _, d := range c.Diffs {
				log.Warn("bản sao danh bạ: hai nguồn khác nhau", "event", "fieldops.directory_source_diff", "feed", d.Feed, "ref", d.Ref,
					"kind", d.Kind, "field", d.Field)
			}
			outcome := "match"
			if c.Skipped {
				outcome = "skipped"
			} else if c.FeedMissing+c.LegacyAbsent+c.Mismatch > 0 {
				outcome = "diff"
			}
			log.Info("đối soát bản sao danh bạ: /v1/partner/* ↔ feed", "event", "fieldops.directory_source_compare", "compared", c.Compared,
				"matched", c.Matched, "feed_missing", c.FeedMissing, "legacy_absent", c.LegacyAbsent, "mismatch", c.Mismatch, "outcome", outcome)
			return nil
		},
	}}
}
