// Package extension là module nghiệp vụ khuyến nông pf/extension (ADR-0011):
// nhiệm vụ hỗ trợ có SLA và leo thang, chỉ đạo khẩn có xác nhận đã đọc, báo cáo
// hiện trường có phiên bản, dashboard 4 KPI theo phạm vi, danh bạ trực.
//
// Module khác chỉ được import extension/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform và cmd/worker.
package extension

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "extension_app"

// JobEscalation là tên job leo thang SLA trong cmd/worker (-jobs).
const JobEscalation = "extension_escalation"

// Deps là port của module khác mà extension cần. Thiếu port nào thì thao tác cần
// nó trả 503 extension.dependency_unavailable (mặc định chặn), riêng Params thiếu
// thì dùng giá trị mặc định (SLA 24 giờ).
type Deps struct {
	Scope    partyport.ScopeResolver // phạm vi TW → Tỉnh → Xã (ADR-0009)
	Orgs     partyport.Orgs          // kiểm HTX, tìm TTKN tỉnh nhận leo thang
	Units    mdport.AdminUnits       // tỉnh của xã
	Params   mdport.Params           // extension.task_sla_hours, extension.duty_status_ttl_hours
	Catalogs mdport.Catalogs         // giống lúa, vụ, sâu bệnh (FR-01 BR-01)
}

// Module là pf/extension đã nối.
type Module struct {
	svc *app.Service
}

// New tạo module với pool của vai extension_app.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	return &Module{svc: &app.Service{
		DB: db, Clock: clk, Scope: d.Scope, Orgs: d.Orgs, Units: d.Units, Params: d.Params, Catalogs: d.Catalogs,
	}}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: module chưa có route quản trị (tham số SLA đặt qua pf/masterdata).
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// SyncSource cho pf/sync: chiều xuống "task", "directive".
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// Staff là cổng đọc danh bạ cán bộ (chỉ mã tham chiếu + đơn vị) cho pf/notify.
func (m *Module) Staff() port.StaffDirectory { return app.StaffDirectory{Svc: m.svc} }

// SyncSink cho pf/sync: chiều lên "task_update", "field_report".
func (m *Module) SyncSink() syncport.Sink { return app.SyncSink{Svc: m.svc} }

// OverdueTenants dựng bộ tìm tenant có nhiệm vụ quá hạn từ pool vai
// ag_outbox_publisher (worker, nối thẳng).
func OverdueTenants(pool *pgxpool.Pool) app.TenantLister { return app.OverdueTenants(pool) }

// Jobs trả job định kỳ của module cho bộ lập lịch của worker (chỉ chạy ở leader):
// leo thang SLA mỗi 15 phút, chạy bù khi khởi động. Job idempotent.
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobEscalation, Spec: schedule.Every{Interval: 15 * time.Minute}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			n, err := m.svc.RunEscalation(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("leo thang nhiệm vụ quá SLA", "event", "job.extension_escalation", "escalated", n, "outcome", outcome)
			}
			return err
		},
	}}
}
