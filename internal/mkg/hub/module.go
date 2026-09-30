// Package hub là module mkg/hub (docs/design/mkg.md mục 4): dữ liệu chủ hub /
// kho / bãi có phiên bản và phê duyệt, lô có N nguồn kèm tỷ lệ, mẻ biến đổi
// (sấy, nén), dịch chuyển tồn bất biến, tồn theo lô, xuất theo rủi ro xuống
// cấp, và van công bố hub_facility / hub_capacity sang nền tảng.
//
// Thuộc vùng nội bộ MKG: chỉ được nối ở gốc lắp ráp cmd/api-internal (cụm mkg,
// vai hub_app). Module khác chỉ được import hub/port (R-02); vùng nền tảng
// không được import gì của mkg (R-03).
package hub

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module ở cụm mkg (R-05).
const DBRole = "hub_app"

// Module là mkg/hub đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai hub_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk}}
}

// RegisterApp gắn route vận hành (API nội bộ MKG, bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route dữ liệu chủ và phê duyệt (bề mặt quản trị, MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Lots cho module mkg khác đọc nguồn gốc và tồn của lô (qua port).
func (m *Module) Lots() port.Lots { return m.svc }

// LotOrigins cho module mkg khác đọc nguồn gốc lô với tenant tường minh, không cần vai hub (G3-2).
func (m *Module) LotOrigins() port.LotOrigins { return m.svc }

// ActiveHubTenants dựng bộ tìm tenant có cơ sở đang dùng từ pool vai ag_outbox_publisher.
func ActiveHubTenants(pool *pgxpool.Pool) app.TenantLister { return app.ActiveHubTenants(pool) }

// Jobs trả job định kỳ của module cho bộ lập lịch của api-internal (chỉ chạy ở leader): công
// bố sức chứa mọi cơ sở đang dùng qua van hub_capacity mỗi ngày lúc 01:15 giờ Việt Nam, chạy
// bù khi khởi động (idempotent theo cơ sở × ngày) — GIS FN-06 / US-WH-01 AC-2, Q-MKG-02.
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobDailyCapacity, Spec: schedule.Daily{Hour: app.DailyCapacityHour, Minute: app.DailyCapacityMinute},
		RunOnStart: true, Timeout: 30 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.PublishDailyCapacity(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("công bố sức chứa định kỳ", "event", "job."+port.JobDailyCapacity, "published", r.Published,
					"skipped", r.Skipped, "outcome", outcome)
			}
			return err
		},
	}}
}

// PublishDailyCapacity chạy một lượt công bố định kỳ (kiểm thử, công cụ vận hành).
func (m *Module) PublishDailyCapacity(ctx context.Context, tenants app.TenantLister) (app.DailyResult, error) {
	return m.svc.PublishDailyCapacity(ctx, tenants)
}

// WithEnv gắn nguồn số đo cảm biến kho cho hub.risk.v2 (mặc định port.NoEnvReadings).
func (m *Module) WithEnv(env port.EnvReadings) *Module {
	m.svc.Env = env
	return m
}

// Consumers là handler sự kiện NỘI BỘ MKG của hub cho cmd/worker-internal
// (vhost mkg.internal, ADR-0018). Handler chỉ dùng giao dịch inbox được giao
// (vai hub_app, tenant của phong bì) — không cần pool.
type Consumers struct{ c app.Consumer }

// NewConsumers dựng handler consumer của hub.
func NewConsumers(clk clock.Clock) Consumers { return Consumers{c: app.Consumer{Clock: clk}} }

// WeighTicket: mkg.measurement.weigh_ticket.recorded → khối lượng lô (Q-MKG-04).
func (c Consumers) WeighTicket() inbox.Handler { return c.c.HandleWeighTicket }

// RollCount: mkg.fieldops.roll_count.recorded → hàng dự kiến về.
func (c Consumers) RollCount() inbox.Handler { return c.c.HandleRollCount }

// Reconciliation: mkg.measurement.reconciliation.completed → quy kết hao hụt.
func (c Consumers) Reconciliation() inbox.Handler { return c.c.HandleReconciliation }
