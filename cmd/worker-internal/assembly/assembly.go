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

	amqp "github.com/rabbitmq/amqp091-go"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
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
	// Build dựng handler lúc khởi động (nối module mkg qua port/).
	Build func(clk clock.Clock) inbox.Handler
}

// Consumers là sổ đăng ký consumer nội bộ. Kiểm thử của package đòi tên và sự
// kiện đúng quy ước. Mỗi consumer: quorum queue + DLQ (Name + ".dlq"), giao tối
// đa inbox.DeliveryLimit lần rồi sang DLQ, idempotent qua shared.inbox (R-61).
//
// G3 (mkg/hub, vai hub_app):
//   - mkg.hub.weigh_ticket   ← mkg.measurement.weigh_ticket.recorded (Q-MKG-04: khối lượng lô HUB_IN, gắn HUB_OUT ↔ phiếu xuất);
//   - mkg.hub.roll_count     ← mkg.fieldops.roll_count.recorded (hàng dự kiến về);
//   - mkg.hub.reconciliation ← mkg.measurement.reconciliation.completed (quy kết hao hụt theo cơ sở).
var Consumers = []Consumer{
	{Name: hubport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Role: hub.DBRole,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).WeighTicket() }},
	{Name: hubport.RollCountConsumer, Events: []string{fieldport.EventRollCountRecorded}, Role: hub.DBRole,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).RollCount() }},
	{Name: hubport.ReconciliationConsumer, Events: []string{measport.EventReconciliationCompleted}, Role: hub.DBRole,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).Reconciliation() }},
}

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
