// Package engine là module mkg/engine (W8.2 — ADR-0036 §3; kiến trúc v4.1 §27.2 C, §41.4; docs/design/mkg.md §5.9):
// phía dữ liệu của engine điều phối nội bộ. W8.2 chỉ có KHO QUAN SÁT (schema mkg_engine, mkg 000037): quan sát trạng
// thái ngoài (kind, source, as_of, max_age, uncertainty) nạp từ feed của cổng tích hợp qua bộ điều hợp feed của W5.5,
// và port đọc Observations cho engine. Engine (W8.3 — dịch vụ Python dùng lib/optcore) chờ MKG v1.3.
//
// Thuộc vùng nội bộ MKG: chỉ nối ở cmd/api-internal (cụm mkg, vai r_engine). Module khác chỉ import engine/port
// (R-02); vùng nền tảng không import gì của mkg (R-03) — dữ liệu nền tảng đến qua feed như mọi đối tác (R-INT-01).
package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
	foport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Options là phụ thuộc tùy chọn của module.
type Options struct {
	// Feed: bộ điều hợp feed của cổng tích hợp (W5.5 — fieldops.NewGatewayDirectoryFeed; đọc được mọi feed theo tên,
	// khóa API cần scope feed:<tên>:read). nil → không job engine_observation_feed.
	Feed foport.DirectoryFeed
	// Feeds: feed được kéo (nil = mọi feed có bộ ánh xạ).
	Feeds []string
	// AllowProposed: cờ dev/test — kéo cả feed có hợp đồng proposed (chưa MKG ký, A7). Bỏ qua khi Production.
	AllowProposed bool
	Production    bool
}

// Module là mkg/engine đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool vai đăng nhập của đơn vị (giao dịch chạy r_engine).
func New(db tenant.Beginner, clk clock.Clock, o Options) *Module {
	return &Module{svc: &app.Service{DB: forModule(db), Clock: clk, Feed: o.Feed, Feeds: o.Feeds,
		AllowProposed: o.AllowProposed && !o.Production, Production: o.Production}}
}

// Observations cho engine (W8.3) đọc quan sát mới nhất (tenant tường minh).
func (m *Module) Observations() port.Observations { return m.svc }

// FeedSync là kết quả kéo một feed.
type FeedSync = app.FeedSync

// SyncObservationFeeds chạy một lượt nạp quan sát cho tenant MKG tid (job, kiểm thử).
func (m *Module) SyncObservationFeeds(ctx context.Context, tid uuid.UUID) ([]FeedSync, error) {
	return m.svc.SyncObservationFeeds(ctx, tid)
}

// Jobs trả job port.JobObservationFeed (mỗi interval, mặc định 15 phút): kéo feed thành quan sát, ghi cảnh báo khi feed
// quá max_age / bị thu hồi / trang lệch hợp đồng. Chưa cấu hình feed / tenant → không có job.
func (m *Module) Jobs(tid uuid.UUID, interval time.Duration, log *slog.Logger) []schedule.Job {
	if m.svc.Feed == nil || tid == uuid.Nil {
		return nil
	}
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	return []schedule.Job{{
		Name: port.JobObservationFeed, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 30 * time.Minute,
		Run: func(ctx context.Context) error {
			rs, err := m.svc.SyncObservationFeeds(ctx, tid)
			for _, r := range rs {
				outcome := "ok"
				switch {
				case r.Skipped != "":
					outcome = "skipped"
				case r.Revoked:
					outcome = "revoked"
				case r.Stale:
					outcome = "stale"
				}
				lvl := slog.LevelInfo
				if outcome == "revoked" || outcome == "stale" {
					lvl = slog.LevelWarn
				}
				log.Log(ctx, lvl, "nạp quan sát từ feed", "event", "job."+port.JobObservationFeed, "feed", r.Feed, "skipped", r.Skipped,
					"pages", r.Pages, "items", r.Items, "observed", r.Observed, "retracted", r.Retracted, "duplicates", r.Duplicates,
					"caught_up", r.CaughtUp, "as_of", r.AsOf, "max_age", r.MaxAge, "resynced", r.Resynced, "outcome", outcome)
			}
			if err != nil {
				log.Error("nạp quan sát lỗi", "event", "job."+port.JobObservationFeed, "outcome", "error", "error", err.Error())
			}
			return err
		},
	}}
}
