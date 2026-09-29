// Package metrics là module pf/metrics (tài liệu kiến trúc mục 16, cổng G3): chỉ số
// CÓ LỊCH SỬ từ định nghĩa khai báo có phiên bản (defs/*.yaml), tính "như hệ biết
// lúc hết ngày D" (recorded_at < Cutoff(D)) qua port.Source của module chủ dữ liệu,
// ghi sổ theo ngày; nhật ký lớp GIS + ảnh chụp cuối ngày + phát lại (GIS FN-18..21).
//
// Module khác chỉ được import metrics/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform và cmd/worker.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/defs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = port.DBRole

// Deps là phụ thuộc của module.
type Deps struct {
	// Sources: nguồn chỉ số theo module chủ dữ liệu ("pf/extension", "pf/mechanization",
	// "pf/cropcycle", "pf/geo") — mỗi nguồn đọc bằng pool vai của module đó (R-05).
	Sources  map[string]port.Source
	Scope    partyport.ScopeResolver
	Geometry port.GeometryResolver
	Log      *slog.Logger
}

// Module là pf/metrics đã nối.
type Module struct{ svc *app.Service }

// catalog nạp một lần: tệp định nghĩa nhúng trong nhị phân đã được kiểm ở kiểm thử.
var catalog = func() *domain.Catalog {
	c, err := domain.Load(defs.FS)
	if err != nil {
		panic(fmt.Sprintf("metrics: định nghĩa chỉ số không hợp lệ: %v", err))
	}
	return c
}()

// New tạo module với pool của vai metrics_app (nil ở worker khi chỉ chạy consumer).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Catalog: catalog, Sources: deps.Sources, Scope: deps.Scope,
		Geometry: deps.Geometry, Log: deps.Log}}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (không có).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Source là port.Source hợp nhất mọi nguồn (chọn định nghĩa, kiểm ngày đã kết thúc).
func (m *Module) Source() port.Source { return m.svc }

// LayerLog là nhật ký lớp GIS + phát lại.
func (m *Module) LayerLog() port.LayerLog { return m.svc }

// Service cho kiểm thử và lệnh worker (backfill).
func (m *Module) Service() *app.Service { return m.svc }

// LayerLogHandler là inbox.Handler của consumer port.LayerLogConsumer (vai metrics_app).
func (m *Module) LayerLogHandler() inbox.Handler { return m.svc.HandleLayerEvent }

// LayerEvents là các sự kiện consumer nhật ký lớp bind vào.
var LayerEvents = app.LayerEvents

// JobTenants liệt kê tenant cho job bằng pool ag_outbox_publisher.
func JobTenants(pool *pgxpool.Pool) app.TenantLister { return app.JobTenants(pool) }

// Jobs trả job metrics_snapshot (00:45 giờ VN, sau anchor 00:30; chạy ở leader):
// ghi sổ chỉ số và ảnh chụp lớp của hôm qua. Idempotent, chạy bù khi khởi động.
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobMetricsSnapshot, Spec: schedule.Daily{Hour: 0, Minute: 45}, RunOnStart: true, Timeout: 2 * time.Hour,
		Run: func(ctx context.Context) error {
			d := app.SnapshotDateFor(m.svc.Clock.Now())
			r, err := m.svc.RunSnapshotJob(ctx, tenants, d)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("chỉ số + ảnh chụp lớp", "event", "job.metrics_snapshot", "business_date", d.String(), "tenants", r.Tenants,
					"inserted", r.Values.Inserted, "superseded", r.Values.Superseded, "unchanged", r.Values.Unchanged,
					"no_data", r.Values.NoData, "snapshots", r.Snapshots, "outcome", outcome)
			}
			return err
		},
	}}
}

// ParseDate, Before, AddDays, Closed: tiện ích ngày nghiệp vụ cho lệnh worker.
var (
	ParseDate = domain.ParseDate
	Before    = domain.Before
	AddDays   = domain.AddDays
	Closed    = domain.Closed
)
