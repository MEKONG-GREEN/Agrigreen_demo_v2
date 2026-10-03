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

// Rabbit là Broker RabbitMQ có publisher confirm. Kết nối/kênh đứt (broker khởi động lại,
// mạng chập chờn) được quay số lại ở lần Publish/Check kế tiếp — publisher không bị kẹt
// mãi với kết nối chết.
type Rabbit struct {
	url string
	mu  sync.Mutex
	// conn, ch, returns: nil khi đang mất kết nối; chỉ đọc/ghi khi giữ mu.
	conn    *amqp.Connection
	ch      *amqp.Channel
	returns chan amqp.Return
	// OnReturn (tùy chọn) nhận thông điệp broker trả về vì không route được (mandatory):
	// sự kiện chưa có hàng đợi nào bind — vẫn coi là đã phát, chỉ báo để quan sát.
	OnReturn func(amqp.Return)
}

// DialRabbit kết nối và khai báo exchange.
func DialRabbit(url string) (*Rabbit, error) {
	r := &Rabbit{url: url}
	if err := r.connectLocked(); err != nil {
		return nil, err
	}
	return r, nil
}

// connectLocked mở kết nối + kênh confirm mới; người gọi giữ mu (hoặc chưa chia sẻ r).
func (r *Rabbit) connectLocked() error {
	conn, err := amqp.Dial(r.url)
	if err != nil {
		return fmt.Errorf("outbox: kết nối RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err := DeclareTopology(ch); err != nil {
		_ = conn.Close()
		return err
	}
	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return fmt.Errorf("outbox: bật publisher confirm: %w", err)
	}
	// basic.return đến TRƯỚC basic.ack của cùng thông điệp và được giao đồng bộ trên
	// goroutine đọc của thư viện: kênh có đệm và được rút sau mỗi Publish để không chặn nó.
	r.returns = ch.NotifyReturn(make(chan amqp.Return, 16))
	r.conn, r.ch = conn, ch
	return nil
}

// ensureLocked quay số lại khi kết nối/kênh đã đóng; người gọi giữ mu.
func (r *Rabbit) ensureLocked() error {
	if r.ch != nil && !r.ch.IsClosed() && !r.conn.IsClosed() {
		return nil
	}
	r.dropLocked()
	return r.connectLocked()
}

// dropLocked bỏ kết nối hiện tại (đóng nếu còn mở); người gọi giữ mu.
func (r *Rabbit) dropLocked() {
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.conn, r.ch, r.returns = nil, nil, nil
}

// drainReturnsLocked rút thông điệp bị trả về; người gọi giữ mu.
func (r *Rabbit) drainReturnsLocked() {
	for {
		select {
		case ret, ok := <-r.returns:
			if !ok {
				return
			}
			if r.OnReturn != nil {
				r.OnReturn(ret)
			}
		default:
			return
		}
	}
}

// Check báo kết nối còn dùng được (probe /readyz), quay số lại khi đã đứt. Đang có
// Publish giữ khóa thì coi là sống — Publish tự xử lý lỗi kết nối của nó.
func (r *Rabbit) Check(context.Context) error {
	if !r.mu.TryLock() {
		return nil
	}
	defer r.mu.Unlock()
	return r.ensureLocked()
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
	if err := r.ensureLocked(); err != nil {
		return err
	}
	defer r.drainReturnsLocked()
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
		if errors.Is(err, amqp.ErrClosed) {
			r.dropLocked()
		}
		return err
	}
	ok, err := conf.WaitContext(ctx)
	if err != nil {
		// Không rõ broker đã nhận chưa (vd kênh đóng giữa chừng): bỏ kênh để lần sau mở
		// kênh mới; phát lại an toàn vì inbox khử trùng theo event_id.
		if ctx.Err() == nil {
			r.dropLocked()
		}
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

// Channel mở một kênh mới trên cùng kết nối (vd khai báo hàng đợi consumer lúc khởi
// động); người gọi đóng kênh. Kênh phát (confirm) không bị ảnh hưởng.
func (r *Rabbit) Channel() (*amqp.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLocked(); err != nil {
		return nil, err
	}
	return r.conn.Channel()
}

// Close đóng kết nối.
func (r *Rabbit) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return nil
	}
	err := r.conn.Close()
	r.conn, r.ch, r.returns = nil, nil, nil
	return err
}
