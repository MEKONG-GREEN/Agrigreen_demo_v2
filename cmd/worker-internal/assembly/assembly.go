// Package assembly là gốc lắp ráp của worker-internal: consumer sự kiện NỘI BỘ
// MKG trên vhost RabbitMQ mkg.internal (ADR-0018, Q-MKG-01; mục 5: đơn vị triển
// khai worker-internal của vùng mkg).
//
// Ranh giới (R-03): package này KHÔNG import internal/platform/* (tools/archlint).
// Mọi consumer xử lý idempotent qua shared.inbox của cụm mkg (R-61), trong giao
// dịch tenant của vai DB module xử lý.
package assembly

import (
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/mkgwire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
)

// OpsRole là vai DB cụm mkg của worker-internal (dbadmin: worker_internal) —
// shared.inbox qua ag_shared_user, không quyền nghiệp vụ. Consumer không khai vai
// riêng chạy bằng vai này.
const OpsRole = "worker_internal"

// Consumer là một consumer sự kiện nội bộ.
type Consumer struct {
	// Name: tên hàng đợi quorum (DLQ = Name + ".dlq"), dạng mkg.<module>.<việc>.
	Name string
	// Events: event_type nội bộ bind vào hàng đợi — phải có schema ở contracts/events-internal/.
	Events []string
	// Role: vai DB cụm mkg mà giao dịch xử lý chạy (inbox + ghi nghiệp vụ cùng giao dịch); rỗng = OpsRole.
	Role string
	// Build dựng handler lúc khởi động (nối module mkg qua port/) — handler chỉ dùng giao
	// dịch inbox. Nil khi consumer cần pool của module khác (dùng BuildWith).
	Build func(clk clock.Clock) inbox.Handler
	// BuildWith (G3-2) dựng handler cần đọc module mkg khác qua pool vai của module đó
	// (vd measurement đọc số cuộn của fieldops bằng vai fieldops_app). Ưu tiên hơn Build.
	BuildWith func(clk clock.Clock, pool PoolFor) (inbox.Handler, error)
}

// PoolFor trả pool (dùng chung trong tiến trình) của một vai DB cụm mkg.
type PoolFor func(role string) (*pgxpool.Pool, error)

// Handler dựng handler của consumer.
func (c Consumer) Handler(clk clock.Clock, pool PoolFor) (inbox.Handler, error) {
	if c.BuildWith != nil {
		return c.BuildWith(clk, pool)
	}
	return c.Build(clk), nil
}

// buildMeasurementRollCount: measurement đối soát lại khi số cuộn về — đọc số cuộn/kg
// cuộn của fieldops qua mkgwire.Rolls trên pool vai fieldops_app (chỉ đọc).
func buildMeasurementRollCount(clk clock.Clock, pool PoolFor) (inbox.Handler, error) {
	fp, err := pool(fieldops.DBRole)
	if err != nil {
		return nil, err
	}
	fo := fieldops.New(fp, clk, fieldops.Options{})
	rolls := &mkgwire.Rolls{Totals: fo.RollTotals(), KgPerRoll: fo.KgPerRoll()}
	return measurement.NewConsumers(clk, measurement.Deps{Rolls: rolls}).RollCount(), nil
}

// Consumers là sổ đăng ký consumer nội bộ. Kiểm thử của package đòi tên và sự
// kiện đúng quy ước. Mỗi consumer: quorum queue + DLQ (Name + ".dlq"), giao tối
// đa inbox.DeliveryLimit lần rồi sang DLQ, idempotent qua shared.inbox (R-61).
//
// G3-2: mkg.fieldops.weigh_ticket (fieldops_app) và mkg.measurement.roll_count (measurement_app).
//
// G3 (mkg/hub, vai hub_app):
//   - mkg.hub.weigh_ticket   ← mkg.measurement.weigh_ticket.recorded (Q-MKG-04: khối lượng lô HUB_IN, gắn HUB_OUT ↔ phiếu xuất);
//   - mkg.hub.roll_count     ← mkg.fieldops.roll_count.recorded (hàng dự kiến về);
//   - mkg.hub.reconciliation ← mkg.measurement.reconciliation.completed (quy kết hao hụt theo cơ sở).
//
// G4 (CHỖ NỐI — docs/dev/g4-conventions.md §4; tên hàng đợi và sự kiện là hằng trong port/ của
// module, đóng băng ở G4-0). Agent module thêm dòng của mình vào danh sách dưới khi module tồn tại:
//   - logistics_app:  logport.RollCountConsumer ← roll_count.recorded (FM-05), logport.WeighTicketConsumer ← weigh_ticket.recorded;
//   - orders_app:     ordport.WeighTicketConsumer ← weigh_ticket.recorded (CUSTOMER_IN → giao nhận; HUB_IN → nhận hàng ↔ đơn mua);
//   - costing_app:    costport.{WeighTicket,Reconciliation,RollCount,Trip,Delivery,JournalPushed}Consumer,
//     costport.PeriodCloseConsumer ← period_close.requested (job đóng kỳ, mẫu mục 12);
//   - acctbridge_app: abport.PeriodClosedConsumer ← period.closed, abport.JournalBuildConsumer, abport.JournalPushConsumer.
var Consumers = []Consumer{
	{Name: hubport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Role: hub.DBRole,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).WeighTicket() }},
	{Name: hubport.RollCountConsumer, Events: []string{fieldport.EventRollCountRecorded}, Role: hub.DBRole,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).RollCount() }},
	{Name: hubport.ReconciliationConsumer, Events: []string{measport.EventReconciliationCompleted}, Role: hub.DBRole,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).Reconciliation() }},
	// G3-2 (mkg/fieldops, vai fieldops_app): phiếu cân → bảng đệm học kg/cuộn.
	{Name: fieldport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Role: fieldops.DBRole,
		Build: func(clk clock.Clock) inbox.Handler { return fieldops.NewConsumers(clk).WeighTicket() }},
	// G3-2 (mkg/measurement, vai measurement_app): số cuộn mới → đối soát lại lô (RB-6).
	{Name: RollCountReconcileConsumer, Events: []string{fieldport.EventRollCountRecorded}, Role: measurement.DBRole,
		BuildWith: buildMeasurementRollCount},
}

// RollCountReconcileConsumer: mkg.fieldops.roll_count.recorded → measurement đối soát lại.
const RollCountReconcileConsumer = "mkg.measurement.roll_count"

// Queue là một hàng đợi consumer trên vhost mkg.internal.
type Queue struct {
	Name   string
	Events []string
}

// Queues liệt kê TOÀN BỘ hàng đợi của vhost mkg.internal. outbox-publisher khai
// báo chúng trước khi phát sự kiện nội bộ đầu tiên (docs/runbooks/rabbitmq-queues.md).
func Queues() []Queue {
	out := make([]Queue, 0, len(Consumers))
	for _, c := range Consumers {
		out = append(out, Queue{Name: c.Name, Events: c.Events})
	}
	return out
}

// DeclareQueues khai báo exchange + hàng đợi + DLQ trên kênh của vhost mkg.internal.
// Exchange cùng tên với vhost nền tảng (ag.events, ag.dlx) nhưng là đối tượng riêng
// của vhost mkg.internal — tài khoản nền tảng không có quyền trên vhost này.
func DeclareQueues(ch *amqp.Channel) error {
	for _, q := range Queues() {
		if err := inbox.DeclareQueue(ch, q.Name, q.Events); err != nil {
			return err
		}
	}
	return nil
}

// ValidName cho biết tên hàng đợi đúng quy ước mkg.<module>.<việc>.
func ValidName(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 3 || parts[0] != "mkg" {
		return false
	}
	for _, p := range parts[1:] {
		if p == "" || strings.Trim(p, "abcdefghijklmnopqrstuvwxyz_") != "" {
			return false
		}
	}
	return true
}
