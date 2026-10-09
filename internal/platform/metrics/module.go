// Package metrics là module pf/metrics (tài liệu kiến trúc mục 16, cổng G3): chỉ số
// CÓ LỊCH SỬ từ định nghĩa khai báo có phiên bản (defs/*.yaml), tính "như hệ biết
// lúc hết ngày D" (recorded_at < Cutoff(D)) trên view công bố pf_<module>_pub của module chủ dữ liệu (W6.3, R-DB-10),
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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/defs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = port.DBRole

// Deps là phụ thuộc của module.
type Deps struct {
	// Sources: nguồn chỉ số theo module chủ dữ liệu ("pf/extension", "pf/mechanization", "pf/cropcycle",
	// "pf/geo"). Nil → nguồn trên view công bố _pub (R-DB-10, W6.3): module đọc pf_<module>_pub.v<N>_* bằng vai
	// của chính nó (r_metrics ∈ r_<module>_pub), không đọc bảng nội bộ, không qua port của module chủ.
	Sources map[string]port.Source
	Scope   partyport.ScopeResolver
	// Geometry: hình học as-of cho phát lại lớp GIS. Nil → pf_geo_pub.v1_layer_geometry (W6.3).
	Geometry port.GeometryResolver
	// Replica: pool vai đăng nhập đơn vị trên bản sao chỉ đọc (AG_DB_REPLICA_URL) cho truy vấn phân tích; nil →
	// primary. Được bọc db.ForModule như pool chính.
	Replica tenant.Beginner
	Log     *slog.Logger
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
	db = forModule(db)
	svc := &app.Service{DB: db, Replica: forModule(deps.Replica), Clock: clk, Catalog: catalog, Sources: deps.Sources,
		Scope: deps.Scope, Geometry: deps.Geometry, Log: deps.Log}
	if svc.Sources == nil {
		svc.Sources = svc.PubSources()
	}
	if svc.Geometry == nil {
		svc.Geometry = app.PubGeometry{S: svc}
	}
	return &Module{svc: svc}
}

// PubSource trả nguồn chỉ số trên view _pub của module chủ dữ liệu (W6.3) — kiểm thử đối chứng của module chủ.
func (m *Module) PubSource(module string) app.PubSource { return m.svc.PubSource(module) }

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

// JobTenants liệt kê tenant cho job bằng pool ag_ops_jobs.
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

// AggDeps là phụ thuộc của bộ chạy job tổng hợp (W6.4, ADR-0031): pool vai đăng nhập của đơn vị chạy job
// (app_worker — thành viên r_metrics; danh mục, kiểm duyệt, nhật ký), pool vai đăng nhập riêng app_agg (chỉ thành
// viên vai tổng hợp — ADR-0031 §3, gói FC; nil → chỉ Propose/Decide), cap/workflow cho bốn mắt (nil → chỉ Run),
// tenant quản trị (danh mục, quyết định, nhật ký chạy), nguồn k sàn publicdata.k_min (nil → 10 tạm, ADR-0031 §7).
type AggDeps struct {
	DB         tenant.Beginner
	AggDB      tenant.Beginner
	Approvals  wfport.Approvals
	Governance uuid.UUID
	KFloor     app.KFloorSource
}

// Agg dựng bộ quản lý danh mục + bộ chạy job tổng hợp (cmd/worker -agg-run; lịch production chờ C-4).
func Agg(clk clock.Clock, d AggDeps) *app.Agg {
	return &app.Agg{DB: forModule(d.DB), AggDB: d.AggDB, Clock: clk, Approvals: d.Approvals, Governance: d.Governance, KFloor: d.KFloor}
}

// AggJobs là mọi job tổng hợp đã khai (đăng ký lúc biên dịch).
var AggJobs = app.AggJobs

// ParseDate, Before, AddDays, Closed: tiện ích ngày nghiệp vụ cho lệnh worker.
var (
	ParseDate = domain.ParseDate
	Before    = domain.Before
	AddDays   = domain.AddDays
	Closed    = domain.Closed
)
