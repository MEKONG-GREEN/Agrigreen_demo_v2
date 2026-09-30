// Package acctbridge là module mkg/acctbridge (tài liệu kiến trúc mục 16; docs/design/mkg.md §5.7;
// docs/dev/g4-conventions.md): bút toán TỔNG HỢP theo kỳ từ kết quả đóng kỳ của mkg/costing, đẩy MỘT
// CHIỀU, tần suất tháng, tới phần mềm kế toán pháp định qua port.LedgerAdapter. Không đẩy giao dịch chi
// tiết; bút toán không bao giờ qua van.
//
// Vùng nội bộ MKG: nối ở cmd/api-internal (HTTP + job thử lại, vai acctbridge_app) và cmd/worker-internal
// (consumer period_closed, journal_build, journal_push). Module khác chỉ import acctbridge/port (R-02).
package acctbridge

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	costingport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// JobPushRetry là tên job định kỳ (api-internal, AG_MKG_JOBS) tạo job đẩy lại cho bút toán có hạn thử lại.
const JobPushRetry = "acctbridge_push_retry"

// Deps là phụ thuộc của module.
type Deps struct {
	// Periods: kết quả đóng kỳ của mkg/costing (cùng tiến trình). nil → job dựng lỗi tạm thời (chờ nối).
	Periods costingport.ClosedPeriods
	// Adapter: bộ điều hợp phần mềm kế toán (AdapterFromEnv). nil → adapter.Unconfigured.
	Adapter port.LedgerAdapter
	// RetryMax: số lần đẩy tối đa khi đích tạm không nhận (0 = mặc định 8 — tạm, chờ khách).
	RetryMax int
}

// Module là mkg/acctbridge đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool vai acctbridge_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: newService(db, clk, deps)}
}

func newService(db tenant.Beginner, clk clock.Clock, deps Deps) *app.Service {
	ad := deps.Adapter
	if ad == nil {
		ad = unconfigured()
	}
	return &app.Service{DB: db, Clock: clk, Periods: deps.Periods, Adapter: ad, RetryMax: deps.RetryMax,
		Metrics: app.NewMetrics(telemetry.Meter())}
}

// RegisterApp gắn route bề mặt ứng dụng.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route bề mặt quản trị (MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// RebuildTenant dựng lại bảng chiếu của tenant từ sổ, tenant tường minh, không kiểm vai — CHỈ cho công cụ diễn
// tập R-22 trên bản sao database (cmd/rebuilddrill). Trả số dòng theo bảng chiếu.
func (m *Module) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	r, err := m.svc.RebuildTenant(ctx, tid)
	return map[string]int64{"mapping_current": int64(r.MappingEvents), "journal_current": int64(r.Journals)}, err
}

// PendingRetryTenants liệt kê tenant có bút toán đến hạn thử lại — pool vai ag_outbox_publisher của cụm mkg
// (chỉ đọc tenant_id, status, next_retry_at; mkg 000019).
func PendingRetryTenants(pool *pgxpool.Pool) app.TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM mkg_acctbridge.journal_current
		  WHERE status = 'approved' AND next_retry_at IS NOT NULL AND next_retry_at <= now()`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// Jobs trả job định kỳ của module (api-internal, chỉ leader): mỗi phút tạo job đẩy lại cho bút toán có hạn
// thử lại đã tới (giãn cách mũ, R-62).
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: JobPushRetry, Spec: schedule.Every{Interval: time.Minute}, RunOnStart: true, Timeout: 5 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := m.svc.RetryDue(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("thử lại đẩy bút toán", "event", "job."+JobPushRetry, "enqueued", r.Enqueued, "outcome", outcome)
			}
			return err
		},
	}}
}

// RetryDue chạy một lượt thử lại (kiểm thử, công cụ vận hành).
func (m *Module) RetryDue(ctx context.Context, tenants app.TenantLister) (app.RetryResult, error) {
	return m.svc.RetryDue(ctx, tenants)
}

// Consumers là handler sự kiện NỘI BỘ MKG của acctbridge cho cmd/worker-internal (vhost mkg.internal,
// ADR-0018). Handler chạy trong giao dịch inbox của vai acctbridge_app; PeriodSummary đọc qua
// deps.Periods (pool vai costing_app), bút toán đẩy qua deps.Adapter.
type Consumers struct{ svc *app.Service }

// NewConsumers dựng handler consumer của acctbridge.
func NewConsumers(db tenant.Beginner, clk clock.Clock, deps Deps) Consumers {
	return Consumers{svc: newService(db, clk, deps)}
}

// PeriodClosed: mkg.costing.period.closed → job dựng bút toán (B-02).
func (c Consumers) PeriodClosed() inbox.Handler { return c.svc.HandlePeriodClosed }

// JournalBuild: mkg.acctbridge.journal_build.requested → dựng bút toán tổng hợp.
func (c Consumers) JournalBuild() inbox.Handler { return c.svc.HandleBuildRequested }

// JournalPush: mkg.acctbridge.journal_push.requested → đẩy qua LedgerAdapter.
func (c Consumers) JournalPush() inbox.Handler { return c.svc.HandlePushRequested }
