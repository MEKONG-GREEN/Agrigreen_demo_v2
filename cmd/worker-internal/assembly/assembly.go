// Package assembly là gốc lắp ráp của worker-internal: consumer sự kiện NỘI BỘ
// MKG trên vhost RabbitMQ mkg.internal (ADR-0018, Q-MKG-01; mục 5: đơn vị triển
// khai worker-internal của vùng mkg).
//
// Ranh giới (R-03): package này KHÔNG import internal/platform/* (tools/archlint).
// Mọi consumer xử lý idempotent qua shared.inbox của cụm mkg (R-61), trong giao
// dịch tenant của vai DB module xử lý.
package assembly

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/mkgwire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge"
	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	costport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
)

// OpsRole là vai đăng nhập cụm mkg của worker-internal (W2.6, ADR-0028): app_worker_internal NOINHERIT, MỘT
// pool cho mọi consumer; giao dịch inbox chạy SET LOCAL ROLE r_<module> của consumer (Consumer.Module), module
// dựng thêm (fieldops, costing, …) tự bọc db.ForModule. Thay worker_internal + bảy vai <module>_app.
const OpsRole = dbadmin.AppWorkerInternal

// Consumer là một consumer sự kiện nội bộ.
type Consumer struct {
	// Name: tên hàng đợi quorum (DLQ = Name + ".dlq"), dạng mkg.<module>.<việc>.
	Name string
	// Events: event_type nội bộ bind vào hàng đợi — phải có schema ở contracts/events-internal/.
	Events []string
	// Module: vai nhóm mà giao dịch xử lý chạy (inbox + ghi nghiệp vụ cùng giao dịch, SET LOCAL ROLE r_<module>).
	Module db.ModuleID
	// Build dựng handler lúc khởi động (nối module mkg qua port/) — handler chỉ dùng giao
	// dịch inbox. Nil khi consumer cần pool của module khác (dùng BuildWith).
	Build func(clk clock.Clock) inbox.Handler
	// BuildWith (G3-2) dựng handler cần đọc module mkg khác qua pool vai của module đó
	// (vd measurement đọc số cuộn của fieldops bằng r_fieldops). Ưu tiên hơn Build.
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
// cuộn của fieldops qua mkgwire.Rolls trên pool r_fieldops (chỉ đọc).
func buildMeasurementRollCount(clk clock.Clock, pool PoolFor) (inbox.Handler, error) {
	fp, err := pool(OpsRole)
	if err != nil {
		return nil, err
	}
	fo := fieldops.New(fp, clk, fieldops.Options{})
	rolls := &mkgwire.Rolls{Totals: fo.RollTotals(), KgPerRoll: fo.KgPerRoll()}
	return measurement.NewConsumers(clk, measurement.Deps{Rolls: rolls}).RollCount(), nil
}

// buildLogisticsRollCount: tấn ước FM-05 = cuộn × kg/cuộn học theo HTX của fieldops (pool r_fieldops, chỉ đọc).
func buildLogisticsRollCount(clk clock.Clock, pool PoolFor) (inbox.Handler, error) {
	fp, err := pool(OpsRole)
	if err != nil {
		return nil, err
	}
	return logistics.NewConsumers(clk, fieldops.New(fp, clk, fieldops.Options{}).KgPerRoll()).RollCount(), nil
}

// buildAcctbridge dựng consumer của mkg/acctbridge (r_acctbridge): bộ điều hợp kế toán theo
// AG_ACCTBRIDGE_ADAPTER (mặc định xuất tệp vào kho đối tượng trong lãnh thổ). Kỳ đã đóng đọc qua
// costing/port.ClosedPeriods trên pool r_costing (chỉ đọc).
func buildAcctbridge(pick func(acctbridge.Consumers) inbox.Handler) func(clock.Clock, PoolFor) (inbox.Handler, error) {
	return func(clk clock.Clock, pool PoolFor) (inbox.Handler, error) {
		p, err := pool(OpsRole)
		if err != nil {
			return nil, err
		}
		ad, _ := acctbridge.AdapterFromEnv(context.Background(), clk)
		cp, err := pool(OpsRole)
		if err != nil {
			return nil, err
		}
		periods := costing.New(cp, clk, costing.Deps{}).ClosedPeriods()
		return pick(acctbridge.NewConsumers(p, clk, acctbridge.Deps{Periods: periods, Adapter: ad, RetryMax: acctbridge.RetryMaxFromEnv()})), nil
	}
}

// Consumers là sổ đăng ký consumer nội bộ. Kiểm thử của package đòi tên và sự
// kiện đúng quy ước. Mỗi consumer: quorum queue + DLQ (Name + ".dlq"), giao tối
// đa inbox.DeliveryLimit lần rồi sang DLQ, idempotent qua shared.inbox (R-61).
//
// G3-2: mkg.fieldops.weigh_ticket (fieldops_app) và mkg.measurement.roll_count (measurement_app).
//
// G3 (mkg/hub, r_hub):
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
	{Name: hubport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Module: hub.DBModule,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).WeighTicket() }},
	{Name: hubport.RollCountConsumer, Events: []string{fieldport.EventRollCountRecorded}, Module: hub.DBModule,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).RollCount() }},
	{Name: hubport.ReconciliationConsumer, Events: []string{measport.EventReconciliationCompleted}, Module: hub.DBModule,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).Reconciliation() }},
	// G4 (proto FM-05): mốc chuyến có đích là cơ sở → thông báo hàng đến mang mã chuyến (cho_den → da_den).
	{Name: hubport.TripMilestoneConsumer, Events: []string{logport.EventTripMilestoneRecorded}, Module: hub.DBModule,
		Build: func(clk clock.Clock) inbox.Handler { return hub.NewConsumers(clk).TripMilestone() }},
	// G3-2 (mkg/fieldops, r_fieldops): phiếu cân → bảng đệm học kg/cuộn.
	{Name: fieldport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Module: fieldops.DBModule,
		Build: func(clk clock.Clock) inbox.Handler { return fieldops.NewConsumers(clk).WeighTicket() }},
	// G3-2 (mkg/measurement, r_measurement): số cuộn mới → đối soát lại lô (RB-6).
	{Name: RollCountReconcileConsumer, Events: []string{fieldport.EventRollCountRecorded}, Module: measurement.DBModule,
		BuildWith: buildMeasurementRollCount},
	// G4 (mkg/logistics, r_logistics): xuống ghe → chuyến đường thủy planned (FM-05); phiếu cân → đóng chuyến.
	{Name: logport.RollCountConsumer, Events: []string{fieldport.EventRollCountRecorded}, Module: logistics.DBModule,
		BuildWith: buildLogisticsRollCount},
	{Name: logport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Module: logistics.DBModule,
		Build: func(clk clock.Clock) inbox.Handler { return logistics.NewConsumers(clk, nil).WeighTicket() }},
	// G4 (mkg/orders, r_orders): HUB_IN → phiếu nhận hàng ↔ đơn mua; CUSTOMER_IN → giao nhận.
	{Name: ordport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Module: orders.DBModule,
		Build: func(clk clock.Clock) inbox.Handler { return orders.NewConsumers(clk).WeighTicket() }},
	// G4 (mkg/costing, r_costing): đầu vào giá thành + job đóng kỳ (mẫu mục 12).
	{Name: costport.WeighTicketConsumer, Events: []string{measport.EventWeighTicketRecorded}, Module: costing.DBModule,
		BuildWith: buildCosting(costing.Consumers.WeighTicket)},
	{Name: costport.ReconciliationConsumer, Events: []string{measport.EventReconciliationCompleted}, Module: costing.DBModule,
		BuildWith: buildCosting(costing.Consumers.Reconciliation)},
	{Name: costport.RollCountConsumer, Events: []string{fieldport.EventRollCountRecorded}, Module: costing.DBModule,
		BuildWith: buildCosting(costing.Consumers.RollCount)},
	{Name: costport.TripConsumer, Events: []string{logport.EventTripCompleted}, Module: costing.DBModule,
		BuildWith: buildCosting(costing.Consumers.Trip)},
	{Name: costport.DeliveryConsumer, Events: []string{ordport.EventDeliveryAccepted}, Module: costing.DBModule,
		BuildWith: buildCosting(costing.Consumers.Delivery)},
	{Name: costport.JournalPushedConsumer, Events: []string{abport.EventJournalPushed}, Module: costing.DBModule,
		BuildWith: buildCosting(costing.Consumers.JournalPushed)},
	{Name: costport.PeriodCloseConsumer, Events: []string{costport.EventPeriodCloseRequested}, Module: costing.DBModule,
		BuildWith: buildCosting(costing.Consumers.PeriodClose)},
	// G4 (mkg/acctbridge, r_acctbridge): đóng kỳ → job dựng → dựng; duyệt → job đẩy → đẩy (R-62).
	{Name: abport.PeriodClosedConsumer, Events: []string{costport.EventPeriodClosed}, Module: acctbridge.DBModule,
		BuildWith: buildAcctbridge(acctbridge.Consumers.PeriodClosed)},
	{Name: abport.JournalBuildConsumer, Events: []string{abport.EventJournalBuildRequested}, Module: acctbridge.DBModule,
		BuildWith: buildAcctbridge(acctbridge.Consumers.JournalBuild)},
	{Name: abport.JournalPushConsumer, Events: []string{abport.EventJournalPushRequested}, Module: acctbridge.DBModule,
		BuildWith: buildAcctbridge(acctbridge.Consumers.JournalPush)},
}

// CostingDeps dựng cổng đọc của mkg/costing ở worker-internal, mỗi cổng trên pool vai của module CHỦ dữ
// liệu (chỉ đọc, tenant tường minh — không GRANT chéo schema, g4-conventions §3.1):
//   - Origins (hub_app): nguồn gốc lô → quy kết hao hụt về lô nguồn (C-07);
//   - Contracts (orders_app): hợp đồng mua rơm → làm mới quyết toán theo tấn/cuộn sau phiếu cân / đếm cuộn;
//   - Trips (logistics_app): ghe chưa cân của lệnh gom (PM-02) + đọc lại chuyến khi đóng kỳ (asOf);
//   - Deliveries (orders_app): đọc lại giao nhận đã chấp nhận khi đóng kỳ (asOf).
func CostingDeps(clk clock.Clock, pool PoolFor) (costing.Deps, error) {
	hp, err := pool(OpsRole)
	if err != nil {
		return costing.Deps{}, err
	}
	op, err := pool(OpsRole)
	if err != nil {
		return costing.Deps{}, err
	}
	lp, err := pool(OpsRole)
	if err != nil {
		return costing.Deps{}, err
	}
	om := orders.New(op, clk)
	return costing.Deps{
		Origins:    hub.New(hp, clk).LotOrigins(),
		Contracts:  om.Contracts(),
		Trips:      logistics.New(lp, clk, logistics.Options{}).Trips(),
		Deliveries: om.Deliveries(),
	}, nil
}

// buildCosting dựng một consumer của mkg/costing (r_costing) với đủ cổng đọc CostingDeps.
func buildCosting(pick func(costing.Consumers) inbox.Handler) func(clock.Clock, PoolFor) (inbox.Handler, error) {
	return func(clk clock.Clock, pool PoolFor) (inbox.Handler, error) {
		deps, err := CostingDeps(clk, pool)
		if err != nil {
			return nil, err
		}
		return pick(costing.NewConsumers(clk, deps)), nil
	}
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
