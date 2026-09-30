// Package logistics là module mkg/logistics (docs/design/mkg.md §5.4): chuyến xe / ghe / sà lan với
// trạng thái chỉ tiến, chặng đường thủy / đường bộ có ảnh chụp định tuyến nguyên văn + SHA-256, danh mục
// phương tiện có phiên bản (proto GH-01..03), tham số cước có phiên bản (người đặt ≠ người duyệt), chuyến
// sinh từ lượt xuống ghe (proto FM-05) và đóng bằng phiếu cân; phát mkg.logistics.trip.completed (nội bộ).
//
// Thuộc vùng nội bộ MKG: chỉ nối ở cmd/api-internal và cmd/worker-internal (cụm mkg, vai logistics_app).
// Module khác chỉ import logistics/port (R-02); không import vùng nền tảng (R-03) — mạng đường thủy tới
// qua bề mặt đối tác /v1/partner/waterway/route (ADR-0020).
package logistics

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Options là phụ thuộc tùy chọn của module.
type Options struct {
	// Waterway: định tuyến đường thủy (bề mặt đối tác); nil → Unconfigured (mọi chặng thủy dự phòng Haversine).
	Waterway port.WaterwayRoutes
	// Road: định tuyến đường bộ; nil → Haversine × tham số #10 (chưa có OSRM trong lãnh thổ, RB-7).
	Road port.RoadRoutes
	// Files (G4, L-11): kho đối tượng trong lãnh thổ (shared/objstore) cho tệp chứng từ chuyến — POST /v1/logistics/files.
	Files objstore.Store
}

// Module là mkg/logistics đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai logistics_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, o Options) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Waterway: o.Waterway, Road: o.Road, Files: o.Files, Metrics: app.NewMetrics(telemetry.Meter())}}
}

// RebuildTenant dựng lại bảng chiếu của tenant từ sổ, tenant tường minh, không kiểm vai — CHỈ cho công cụ diễn
// tập R-22 trên bản sao database (cmd/rebuilddrill). Trả số dòng theo bảng chiếu.
func (m *Module) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	n, err := m.svc.RebuildTenant(ctx, tid)
	return map[string]int64{"trip_current": n}, err
}

// JobAlerts là tên job cảnh báo định kỳ L-08 của api-internal (AG_MKG_JOBS).
const JobAlerts = port.JobAlerts

// ActiveTenants liệt kê tenant có chuyến mở hoặc phương tiện đang dùng — pool vai ag_outbox_publisher của cụm mkg
// (chỉ đọc tenant_id, status; mkg 000020).
func ActiveTenants(pool *pgxpool.Pool) app.TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT tenant_id FROM mkg_logistics.trip_current WHERE status IN ('planned', 'departed', 'arrived')
		  UNION SELECT tenant_id FROM mkg_logistics.vessel_current WHERE status = 'active'`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// Jobs trả job định kỳ của module (api-internal, chỉ leader): mỗi interval (mặc định 15 phút) quét ghe chưa cân
// quá N giờ và đăng kiểm hết / sắp hết hạn → thông báo alert_notice (chống lặp) + metric cho Alertmanager (L-08).
func (m *Module) Jobs(tenants app.TenantLister, interval time.Duration, log *slog.Logger) []schedule.Job {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return []schedule.Job{{
		Name: JobAlerts, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 5 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.ScanAlerts(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("quét cảnh báo logistics", "event", "job."+JobAlerts, "tenants", r.Tenants,
					"unweighed", r.Open[app.NoticeTripUnweighed], "expired", r.Open[app.NoticeRegistrationExpired],
					"expiring", r.Open[app.NoticeRegistrationExpiring], "outcome", outcome)
			}
			return err
		},
	}}
}

// ScanAlerts chạy một lượt quét cảnh báo (kiểm thử, công cụ vận hành).
func (m *Module) ScanAlerts(ctx context.Context, tenants app.TenantLister) (app.AlertScan, error) {
	return m.svc.ScanAlerts(ctx, tenants)
}

// PlatformConfig là cấu hình tài khoản dịch vụ MKG gọi Application API (cùng biến với fieldops).
type PlatformConfig = adapter.PlatformConfig

// NewPlatformWaterway tạo WaterwayRoutes qua GET/POST /v1/partner/waterway/route (cấp quyền waterway_routes).
func NewPlatformWaterway(cfg PlatformConfig, clk clock.Clock) port.WaterwayRoutes {
	return adapter.NewWaterwayHTTP(cfg, func() time.Time { return clk.Now() })
}

// RegisterApp gắn route (API nội bộ MKG, bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route quản trị (MFA): dựng lại bảng chiếu.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Trips cho mkg/costing, mkg/orders đọc chuyến (tenant tường minh).
func (m *Module) Trips() port.Trips { return m.svc }

// Consumers là handler sự kiện NỘI BỘ MKG của logistics cho cmd/worker-internal (vai logistics_app,
// giao dịch inbox — không cần pool).
type Consumers struct{ c app.Consumer }

// NewConsumers dựng handler consumer; kg nil → 20 kg/cuộn cho tấn ước FM-05.
func NewConsumers(clk clock.Clock, kg fieldport.KgPerRollEstimates) Consumers {
	return Consumers{c: app.Consumer{Clock: clk, KgPerRoll: kg}}
}

// RollCount: mkg.fieldops.roll_count.recorded (boat_load) → chuyến đường thủy planned (proto FM-05).
func (c Consumers) RollCount() inbox.Handler { return c.c.HandleRollCount }

// WeighTicket: mkg.measurement.weigh_ticket.recorded → đóng chuyến với tấn thật.
func (c Consumers) WeighTicket() inbox.Handler { return c.c.HandleWeighTicket }
