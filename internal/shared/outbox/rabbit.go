package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Exchange là topic exchange của sự kiện nghiệp vụ; routing key = event_type.
const Exchange = "ag.events"

// DeadLetterExchange nhận thông điệp vượt giới hạn giao (R-62).
const DeadLetterExchange = "ag.dlx"

// Rabbit là Broker RabbitMQ có publisher confirm.
type Rabbit struct {
	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// DialRabbit kết nối và khai báo exchange.
func DialRabbit(url string) (*Rabbit, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("outbox: kết nối RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := DeclareTopology(ch); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("outbox: bật publisher confirm: %w", err)
	}
	return &Rabbit{conn: conn, ch: ch}, nil
}

// DeclareTopology khai báo exchange sự kiện và exchange thư chết.
func DeclareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(Exchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	return ch.ExchangeDeclare(DeadLetterExchange, "topic", true, false, false, false, nil)
}

// Publish phát bền vững và chờ broker xác nhận.
func (r *Rabbit) Publish(ctx context.Context, env Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	conf, err := r.ch.PublishWithDeferredConfirmWithContext(ctx, Exchange, env.EventType, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    env.EventID.String(),
		Timestamp:    env.RecordedAt,
		Type:         env.EventType,
		Headers:      MessageHeaders(ctx, env), // R-63
		Body:         body,
	})
	if err != nil {
		return err
	}
	ok, err := conf.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("outbox: broker từ chối (nack)")
	}
	return nil
}

// MessageHeaders dựng header thông điệp: trace_id và schema_version (R-63),
// cộng traceparent W3C của ctx khi OpenTelemetry bật (R-90).
func MessageHeaders(ctx context.Context, env Envelope) amqp.Table {
	h := amqp.Table{"trace_id": env.TraceID, "schema_version": int32(env.SchemaVersion)}
	telemetry.Inject(ctx, HeaderCarrier(h))
	return h
}

// HeaderCarrier cho propagator OTel đọc/ghi header AMQP.
type HeaderCarrier amqp.Table

// Get trả giá trị chuỗi của khóa.
func (c HeaderCarrier) Get(key string) string {
	v, _ := c[key].(string)
	return v
}

// Set ghi khóa.
func (c HeaderCarrier) Set(key, value string) { c[key] = value }

// Keys liệt kê khóa.
func (c HeaderCarrier) Keys() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

// Close đóng kết nối.
func (r *Rabbit) Close() error { return r.conn.Close() }
