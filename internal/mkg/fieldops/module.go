// Package fieldops là module mkg/fieldops (docs/design/mkg.md §5.1): lệnh gom có
// duyệt, ca tổ gom, đếm cuộn theo công đoạn (proto FM-03/04/09), xác nhận của
// HTX và đối soát chiều mua (RB-6), học kg/cuộn theo HTX, đồng bộ ngoại tuyến tối
// giản cho app hiện trường MKG, van collection_schedule / htx_collection_statement.
//
// Thuộc vùng nội bộ MKG: chỉ nối ở cmd/api-internal (cụm mkg, vai fieldops_app).
// Module khác chỉ import fieldops/port (R-02); vùng nền tảng không import gì của
// mkg (R-03). Thửa/HTX của nền tảng đọc qua Application API công khai (adapter).
package fieldops

import (
	"context"
	"crypto/rand"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Options là phụ thuộc tùy chọn của module.
type Options struct {
	// Directory: nguồn thửa/HTX; nil → Unconfigured (đóng: lập lệnh gom trả 503).
	Directory port.PlatformDirectory
	// Weighed: khối lượng cân của lô gom; nil → chưa học kg/cuộn (dùng cấu hình / 20).
	Weighed port.WeighedMass
	// KgPerRoll: kg/cuộn cấu hình (tạm, chờ khách); nil = không có.
	KgPerRoll *decimal.Decimal
	// CursorKey: khóa HMAC con trỏ pull; rỗng → khóa ngẫu nhiên theo tiến trình.
	CursorKey []byte
	// Feed: luồng phản hồi của HTX trên nền tảng (G3-2); nil → job kéo phản hồi không làm gì.
	Feed port.HTXResponseFeed
	// Harvest: ngày gặt theo vụ của thửa (bề mặt partner, FM-01/02); nil → mọi kiểm ngày gặt
	// gắn cờ unavailable (vẫn cho ghi) và không có job làm mới bảng đệm.
	Harvest port.HarvestCalendar
}

// Module là mkg/fieldops đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai fieldops_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, o Options) *Module {
	if o.Directory == nil {
		o.Directory = adapter.Unconfigured{}
	}
	if len(o.CursorKey) == 0 {
		o.CursorKey = make([]byte, 32)
		_, _ = rand.Read(o.CursorKey)
	}
	return &Module{svc: &app.Service{DB: db, Clock: clk, Directory: o.Directory, Weighed: o.Weighed,
		ConfiguredKgPerRoll: o.KgPerRoll, CursorKey: o.CursorKey, Feed: o.Feed, Harvest: o.Harvest}}
}

// NewPlatformFeed tạo luồng phản hồi của HTX qua Application API công khai (tài khoản dịch
// vụ tenant MKG, client agrigreen-mkg-sync, vai mkg_sync).
func NewPlatformFeed(cfg PlatformConfig, clk clock.Clock) port.HTXResponseFeed {
	return adapter.NewPlatformHTTP(cfg, func() time.Time { return clk.Now() })
}

// FeedResult là kết quả một lượt kéo phản hồi của HTX.
type FeedResult = app.FeedResult

// SyncHTXResponses chạy một lượt kéo phản hồi của HTX cho tenant MKG tid (job, kiểm thử).
func (m *Module) SyncHTXResponses(ctx context.Context, tid uuid.UUID) (FeedResult, error) {
	return m.svc.SyncHTXResponses(ctx, tid)
}

// Jobs trả job định kỳ của module cho bộ lập lịch của api-internal (chỉ leader chạy): kéo phản
// hồi của HTX mỗi interval (mặc định 5 phút) cho tenant MKG tid. Không có luồng → không có job.
func (m *Module) Jobs(tid uuid.UUID, interval time.Duration, log *slog.Logger) []schedule.Job {
	if m.svc.Feed == nil || tid == uuid.Nil {
		return nil
	}
	return m.htxJobs(tid, interval, log)
}

// HarvestJobs trả job làm mới bảng đệm ngày gặt (FM-01/02) của các lệnh gom đang mở mỗi
// interval (mặc định 1 giờ) cho tenant MKG tid. Không có nguồn ngày gặt → không có job.
func (m *Module) HarvestJobs(tid uuid.UUID, interval time.Duration, log *slog.Logger) []schedule.Job {
	if m.svc.Harvest == nil || tid == uuid.Nil {
		return nil
	}
	if interval <= 0 {
		interval = time.Hour
	}
	return []schedule.Job{{
		Name: port.JobHarvestDates, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 30 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.RefreshHarvestDates(ctx, tid)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("làm mới ngày gặt", "event", "job."+port.JobHarvestDates, "plots", r.Plots, "known", r.Known,
					"unknown", r.Unknown, "unavailable", r.Unavailable, "outcome", outcome)
			}
			return err
		},
	}}
}

// RefreshHarvestDates chạy một lượt làm mới bảng đệm ngày gặt cho tenant tid (job, kiểm thử).
func (m *Module) RefreshHarvestDates(ctx context.Context, tid uuid.UUID) (app.HarvestRefresh, error) {
	return m.svc.RefreshHarvestDates(ctx, tid)
}

func (m *Module) htxJobs(tid uuid.UUID, interval time.Duration, log *slog.Logger) []schedule.Job {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return []schedule.Job{{
		Name: port.JobHTXResponses, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.SyncHTXResponses(ctx, tid)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("kéo phản hồi của HTX", "event", "job."+port.JobHTXResponses, "recorded", r.Recorded,
					"duplicate", r.Duplicate, "skipped", r.Skipped, "cursor", r.Cursor, "outcome", outcome)
			}
			return err
		},
	}}
}

// PlatformConfig là cấu hình tài khoản dịch vụ MKG gọi Application API.
type PlatformConfig = adapter.PlatformConfig

// NewPlatformDirectory tạo PlatformDirectory qua Application API công khai (OIDC client credentials).
func NewPlatformDirectory(cfg PlatformConfig, clk clock.Clock) port.PlatformDirectory {
	return adapter.NewPlatformHTTP(cfg, func() time.Time { return clk.Now() })
}

// NewPlatformHarvestCalendar tạo nguồn ngày gặt qua bề mặt partner của nền tảng (ADR-0020,
// GET /v1/partner/season-harvest-dates, cấp quyền season_harvest_dates) cho proto FM-01/02.
func NewPlatformHarvestCalendar(cfg PlatformConfig, clk clock.Clock) port.HarvestCalendar {
	return adapter.NewPlatformHTTP(cfg, func() time.Time { return clk.Now() })
}

// WeighedFromMeasurement nối WeighedMass qua measurement/port.LotMass (cùng tiến trình).
func WeighedFromMeasurement(l mport.LotMass) port.WeighedMass { return adapter.LotMassWeighed{Lots: l} }

// RegisterApp gắn route (API nội bộ MKG, bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: fieldops không có route quản trị ở G3.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// KgPerRoll cho measurement đọc ước lượng kg/cuộn có mốc thời điểm (G3-2, tenant tường minh).
func (m *Module) KgPerRoll() port.KgPerRollEstimates { return m.svc }

// Consumers là handler sự kiện NỘI BỘ MKG của fieldops cho cmd/worker-internal (vai
// fieldops_app, giao dịch inbox — không cần pool).
type Consumers struct{ svc *app.Service }

// NewConsumers dựng handler consumer của fieldops.
func NewConsumers(clk clock.Clock) Consumers { return Consumers{svc: &app.Service{Clock: clk}} }

// WeighTicket: mkg.measurement.weigh_ticket.recorded → bảng đệm học kg/cuộn (G3-2).
func (c Consumers) WeighTicket() inbox.Handler { return c.svc.HandleWeighTicket }

// RollTotals cho module mkg khác (measurement RB-6, costing G4) đọc số cuộn của lô gom.
func (m *Module) RollTotals() port.RollTotals { return m.svc }
