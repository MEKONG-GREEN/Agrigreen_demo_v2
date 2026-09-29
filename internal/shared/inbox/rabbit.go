package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DeliveryLimit: số lần giao tối đa trước khi quorum queue chuyển sang DLQ.
const DeliveryLimit = 5

// DeclareQueue khai báo quorum queue của consumer, gắn DLQ, bind các event_type.
// Tên queue: <consumer>; DLQ: <consumer>.dlq. DLQ không bao giờ tự xóa (R-62).
func DeclareQueue(ch *amqp.Channel, consumer string, eventTypes []string) error {
	if err := outbox.DeclareTopology(ch); err != nil {
		return err
	}
	dlq := consumer + ".dlq"
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return err
	}
	if err := ch.QueueBind(dlq, consumer, outbox.DeadLetterExchange, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(consumer, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-delivery-limit":          DeliveryLimit,
		"x-dead-letter-exchange":    outbox.DeadLetterExchange,
		"x-dead-letter-routing-key": consumer,
	}); err != nil {
		return err
	}
	for _, t := range eventTypes {
		if err := ch.QueueBind(consumer, t, outbox.Exchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}

// StartConsume dựng ngữ cảnh xử lý một thông điệp: đọc traceparent trong
// header (R-90) và mở span consumer là con của span phát; gắn trace_id của
// phong bì để log và outbox kế tiếp giữ cùng trace (R-63). OTel tắt → span
// no-op, ctx chỉ thêm trace_id như G0.
func StartConsume(ctx context.Context, consumer string, headers amqp.Table, env outbox.Envelope) (context.Context, trace.Span) {
	if !telemetry.Enabled() {
		if env.TraceID != "" {
			ctx = telemetry.WithTraceID(ctx, env.TraceID)
		}
		return ctx, trace.SpanFromContext(ctx)
	}
	pctx := telemetry.Extract(ctx, outbox.HeaderCarrier(headers))
	if !trace.SpanContextFromContext(pctx).IsValid() {
		pctx = telemetry.ContextWithParent(ctx, "", env.TraceID)
	}
	pctx, span := telemetry.Tracer().Start(pctx, "process "+env.EventType, trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.consumer.group.name", consumer),
			attribute.String("messaging.message.id", env.EventID.String()),
		))
	id := env.TraceID
	if id == "" {
		id = span.SpanContext().TraceID().String()
	}
	return telemetry.WithTraceID(pctx, id), span
}

// Failure là một lần xử lý sự kiện thất bại của consumer (GIS US-SYNC-01/02). Err KHÔNG
// được ghi nguyên văn ở nơi lưu (có thể chứa dữ liệu nghiệp vụ): chỉ lưu mã đã chuẩn hóa.
type Failure struct {
	Consumer string
	Envelope outbox.Envelope
	Err      error
	// DeliveryCount: số lần giao thất bại trước lần này (header x-delivery-count của quorum queue).
	DeliveryCount int64
	// DeadLettered: đây là lần giao cuối — NACK lần này đưa thông điệp sang <consumer>.dlq.
	DeadLettered bool
}

// FailureRecorder ghi nhận một lần xử lý thất bại (giao dịch riêng, sau khi giao dịch của
// handler đã rollback). Lỗi của recorder chỉ được ghi log, không đổi luồng NACK.
type FailureRecorder func(ctx context.Context, f Failure) error

// DeliveryCount đọc header x-delivery-count (quorum queue): số lần thông điệp đã được giao
// lại sau NACK/requeue; không có header = lần giao đầu.
func DeliveryCount(h amqp.Table) int64 {
	switch v := h["x-delivery-count"].(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	case int16:
		return int64(v)
	case int8:
		return int64(v)
	case uint8:
		return int64(v)
	}
	return 0
}

// FinalAttempt: với x-delivery-limit = DeliveryLimit, lần giao có DeliveryCount ≥ DeliveryLimit
// là lần cuối — thất bại thêm lần nữa thì RabbitMQ chuyển sang DLQ.
func FinalAttempt(deliveryCount int64) bool { return deliveryCount >= DeliveryLimit }

// Consume nhận thông điệp tới khi ctx hủy. ACK chỉ sau commit; lỗi → NACK
// requeue, quorum queue tự đếm lần giao và đẩy sang DLQ khi vượt giới hạn.
func Consume(ctx context.Context, ch *amqp.Channel, db tenant.Beginner, consumer string, h Handler, log *slog.Logger) error {
	return ConsumeWith(ctx, ch, db, consumer, h, log, nil)
}

// ConsumeWith như Consume, thêm rec (có thể nil) ghi nhận từng lần thất bại cho màn giám sát
// đồng bộ và cảnh báo Admin khi sự kiện vào DLQ (GIS US-SYNC-01/02).
func ConsumeWith(ctx context.Context, ch *amqp.Channel, db tenant.Beginner, consumer string, h Handler, log *slog.Logger, rec FailureRecorder) error {
	if err := ch.Qos(16, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.ConsumeWithContext(ctx, consumer, consumer, false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("inbox: kênh %s đã đóng", consumer)
			}
			var env outbox.Envelope
			if err := json.Unmarshal(d.Body, &env); err != nil {
				log.Error("phong bì hỏng, chuyển DLQ", "consumer", consumer, "message_id", d.MessageId)
				_ = d.Nack(false, false)
				continue
			}
			mctx, span := StartConsume(ctx, consumer, d.Headers, env)
			processed, err := Process(mctx, db, consumer, env, h)
			if err != nil {
				span.SetStatus(codes.Error, "handler failed")
			}
			span.End()
			if err != nil {
				n := DeliveryCount(d.Headers)
				outcome := "retry"
				if FinalAttempt(n) {
					outcome = "dead_letter"
				}
				log.Warn("xử lý sự kiện thất bại", "consumer", consumer, "event", env.EventType,
					"event_id", env.EventID, "trace_id", env.TraceID, "tenant_id", env.TenantID, "delivery_count", n, "outcome", outcome)
				if rec != nil {
					if rerr := rec(mctx, Failure{Consumer: consumer, Envelope: env, Err: err, DeliveryCount: n, DeadLettered: FinalAttempt(n)}); rerr != nil {
						log.Error("không ghi được lần thất bại", "consumer", consumer, "event_id", env.EventID, "outcome", "error")
					}
				}
				_ = d.Nack(false, true)
				continue
			}
			outcome := "processed"
			if !processed {
				outcome = "duplicate"
			}
			log.Info("sự kiện", "consumer", consumer, "event", env.EventType, "event_id", env.EventID,
				"trace_id", env.TraceID, "tenant_id", env.TenantID, "outcome", outcome)
			_ = d.Ack(false)
		}
	}
}
