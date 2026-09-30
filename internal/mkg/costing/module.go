// Package costing là module mkg/costing (tài liệu kiến trúc mục 16; docs/design/mkg.md §5.6):
// giá thành theo lô (sổ cost_entry), quy đổi cuộn → tấn và ẩm độ, phân bổ chi phí đa chặng, quy
// kết hao hụt về lô nguồn, kỳ tháng + đóng kỳ theo mẫu mục 12, quyết toán HTX (proto PM-01..04),
// chia doanh thu carbon. Event sourcing MỨC B: sổ là nguồn, bảng chiếu dựng lại được (R-22).
//
// Vùng nội bộ MKG: nối ở cmd/api-internal (HTTP) và cmd/worker-internal (consumer + job đóng kỳ),
// vai DB costing_app. Module khác chỉ import costing/port (R-02); acctbridge đọc ClosedPeriods().
package costing

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Deps là cổng đọc module mkg khác (qua port, cùng tiến trình). Trường nil = chưa nối.
type Deps = app.Deps

// Module là mkg/costing đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai costing_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Deps: deps}}
}

// RegisterApp gắn route vận hành (bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route phê duyệt / dựng lại (bề mặt quản trị, MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// RebuildTenant dựng lại bảng chiếu của tenant từ sổ, tenant tường minh, không kiểm vai — CHỈ cho công cụ diễn
// tập R-22 trên bản sao database (cmd/rebuilddrill). Trả số dòng theo bảng chiếu.
func (m *Module) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	return m.svc.RebuildTenant(ctx, tid)
}

// ClosedPeriods cho mkg/acctbridge đọc kết quả đóng kỳ (tenant tường minh).
func (m *Module) ClosedPeriods() port.ClosedPeriods { return m.svc }

// Consumers là handler sự kiện NỘI BỘ MKG của costing cho cmd/worker-internal (vhost mkg.internal,
// ADR-0018). Handler chỉ dùng giao dịch inbox được giao (vai costing_app, tenant của phong bì).
type Consumers struct{ svc *app.Service }

// NewConsumers dựng handler consumer của costing (deps: nguồn gốc lô hub, chuyến logistics…).
func NewConsumers(clk clock.Clock, deps Deps) Consumers {
	return Consumers{svc: &app.Service{Clock: clk, Deps: deps}}
}

func handle[T any](eventType string, fn func(context.Context, pgx.Tx, outbox.Envelope, T) error) inbox.Handler {
	return func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
		if env.EventType != eventType {
			return nil
		}
		var v T
		if err := json.Unmarshal(env.Payload, &v); err != nil {
			return fmt.Errorf("costing: payload %s không hợp lệ: %w", env.EventType, err)
		}
		return fn(ctx, tx, env, v)
	}
}

// WeighTicket: mkg.measurement.weigh_ticket.recorded (hàng đợi mkg.costing.weigh_ticket).
func (c Consumers) WeighTicket() inbox.Handler {
	return handle(measport.EventWeighTicketRecorded, func(ctx context.Context, tx pgx.Tx, env outbox.Envelope, v measport.WeighTicketRecordedV1) error {
		return c.svc.OnWeighTicket(ctx, tx, env.TenantID, v)
	})
}

// Reconciliation: mkg.measurement.reconciliation.completed (mkg.costing.reconciliation).
func (c Consumers) Reconciliation() inbox.Handler {
	return handle(measport.EventReconciliationCompleted, func(ctx context.Context, tx pgx.Tx, env outbox.Envelope, v measport.ReconciliationCompletedV1) error {
		return c.svc.OnReconciliation(ctx, tx, env.TenantID, v)
	})
}

// RollCount: mkg.fieldops.roll_count.recorded (mkg.costing.roll_count).
func (c Consumers) RollCount() inbox.Handler {
	return handle(fieldport.EventRollCountRecorded, func(ctx context.Context, tx pgx.Tx, env outbox.Envelope, v fieldport.RollCountRecordedV1) error {
		return c.svc.OnRollCount(ctx, tx, env.TenantID, v)
	})
}

// Trip: mkg.logistics.trip.completed (mkg.costing.trip).
func (c Consumers) Trip() inbox.Handler {
	return handle(logport.EventTripCompleted, func(ctx context.Context, tx pgx.Tx, env outbox.Envelope, v logport.TripCompletedV1) error {
		return c.svc.OnTrip(ctx, tx, env.TenantID, v)
	})
}

// Delivery: mkg.orders.delivery.accepted (mkg.costing.delivery).
func (c Consumers) Delivery() inbox.Handler {
	return handle(ordport.EventDeliveryAccepted, func(ctx context.Context, tx pgx.Tx, env outbox.Envelope, v ordport.DeliveryAcceptedV1) error {
		return c.svc.OnDelivery(ctx, tx, env.TenantID, v)
	})
}

// JournalPushed: mkg.acctbridge.journal.pushed (mkg.costing.journal_pushed) → kỳ posted.
func (c Consumers) JournalPushed() inbox.Handler {
	return handle(abport.EventJournalPushed, func(ctx context.Context, tx pgx.Tx, env outbox.Envelope, v abport.JournalPushedV1) error {
		return c.svc.OnJournalPushed(ctx, tx, env.TenantID, v)
	})
}

// PeriodClose: mkg.costing.period_close.requested (mkg.costing.period_close) — job đóng kỳ (mục 12).
func (c Consumers) PeriodClose() inbox.Handler {
	return handle(port.EventPeriodCloseRequested, func(ctx context.Context, tx pgx.Tx, env outbox.Envelope, v port.PeriodCloseRequestedV1) error {
		return c.svc.HandleCloseRequested(ctx, tx, env.TenantID, v)
	})
}
