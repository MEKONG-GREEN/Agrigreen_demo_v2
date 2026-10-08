package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// Rabbit là Queue trên RabbitMQ (vhost của cụm pf): mỗi đối tác một quorum queue QueueName(client) với
// x-delivery-limit = inbox.DeliveryLimit, x-max-length = MaxLen + x-overflow = reject-publish (đầy → nack →
// ErrQueueSaturated → 429), thư chết sang DLQ riêng QueueName(client)+".dlq" qua ag.dlx. Không bind vào exchange
// sự kiện ag.events: phát thẳng qua exchange mặc định (routing key = tên hàng đợi) — thông điệp lô không phải sự
// kiện công bố. Tiêu thụ bằng inbox.ConsumeWith (ACK sau commit, NACK + lùi, DLQ khi vượt giới hạn).
type Rabbit struct {
	URL    string
	MaxLen int
	Log    *slog.Logger

	mu       sync.Mutex
	conn     *amqp.Connection
	pub      *amqp.Channel
	declared map[uuid.UUID]bool
	newQueue chan uuid.UUID

	cmu   sync.Mutex
	cconn *amqp.Connection
}

// NewRabbit tạo hàng đợi RabbitMQ (kết nối khi dùng lần đầu).
func NewRabbit(url string, maxLen int, log *slog.Logger) *Rabbit {
	if log == nil {
		log = slog.Default()
	}
	return &Rabbit{URL: url, MaxLen: maxLen, Log: log, declared: map[uuid.UUID]bool{}, newQueue: make(chan uuid.UUID, 256)}
}

// QueueArgs là tham số quorum queue của đối tác (tách để kiểm thử đơn vị).
func QueueArgs(client uuid.UUID, maxLen int) amqp.Table {
	if maxLen <= 0 {
		maxLen = DefaultMaxQueued
	}
	return amqp.Table{
		"x-queue-type":              "quorum",
		"x-delivery-limit":          inbox.DeliveryLimit,
		"x-max-length":              maxLen,
		"x-overflow":                "reject-publish",
		"x-dead-letter-exchange":    outbox.DeadLetterExchange,
		"x-dead-letter-routing-key": QueueName(client),
	}
}

// declare khai báo DLQ + hàng đợi của đối tác trên ch (idempotent), trả số thông điệp đang chờ.
func declare(ch *amqp.Channel, client uuid.UUID, maxLen int) (int, error) {
	if err := outbox.DeclareTopology(ch); err != nil {
		return 0, err
	}
	name := QueueName(client)
	dlq := name + ".dlq"
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return 0, err
	}
	if err := ch.QueueBind(dlq, name, outbox.DeadLetterExchange, false, nil); err != nil {
		return 0, err
	}
	q, err := ch.QueueDeclare(name, true, false, false, false, QueueArgs(client, maxLen))
	if err != nil {
		return 0, err
	}
	return q.Messages, nil
}

func (r *Rabbit) ensureLocked() error {
	if r.conn != nil && !r.conn.IsClosed() && r.pub != nil && !r.pub.IsClosed() {
		return nil
	}
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.conn, r.pub, r.declared = nil, nil, map[uuid.UUID]bool{}
	conn, err := amqp.Dial(r.URL)
	if err != nil {
		return fmt.Errorf("ingest: kết nối RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return err
	}
	r.conn, r.pub = conn, ch
	return nil
}

func (r *Rabbit) declareLocked(client uuid.UUID) (int, error) {
	n, err := declare(r.pub, client, r.MaxLen)
	if err != nil {
		_ = r.conn.Close() // lỗi khai báo đóng kênh — lần sau quay số lại
		return 0, err
	}
	if !r.declared[client] {
		r.declared[client] = true
		select {
		case r.newQueue <- client:
		default:
		}
	}
	return n, nil
}

// Publish phát bền vững, chờ confirm. Nack (hàng đợi đầy, reject-publish) → ErrQueueSaturated.
func (r *Rabbit) Publish(ctx context.Context, client uuid.UUID, env outbox.Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLocked(); err != nil {
		return err
	}
	if !r.declared[client] {
		if _, err := r.declareLocked(client); err != nil {
			return err
		}
	}
	conf, err := r.pub.PublishWithDeferredConfirmWithContext(ctx, "", QueueName(client), true, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: env.EventID.String(), Timestamp: env.RecordedAt,
		Type: env.EventType, Headers: outbox.MessageHeaders(ctx, env), Body: body,
	})
	if err != nil {
		_ = r.conn.Close()
		return err
	}
	ok, err := conf.WaitContext(ctx)
	if err != nil {
		_ = r.conn.Close()
		return err
	}
	if !ok {
		return ErrQueueSaturated
	}
	return nil
}

// Depth đọc số thông điệp đang chờ (khai báo lại idempotent trả message count).
func (r *Rabbit) Depth(_ context.Context, client uuid.UUID) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLocked(); err != nil {
		return 0, err
	}
	return r.declareLocked(client)
}

// Run mở một consumer (kênh riêng) cho mỗi đối tác: đối tác đã biết lúc khởi động (w.Partners) và đối tác có hàng
// đợi mới khai ở Publish/Depth. Consumer lỗi (kênh đứt) được mở lại sau 5 giây.
func (r *Rabbit) Run(ctx context.Context, w Worker) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	started := map[uuid.UUID]bool{}
	start := func(client uuid.UUID) {
		if started[client] {
			return
		}
		started[client] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				if err := r.consume(ctx, w, client); err != nil && ctx.Err() == nil {
					r.Log.Warn("consumer hàng đợi nạp dừng — mở lại", "event", "ingest.consumer_restart", "queue", QueueName(client),
						"outcome", "error", "error", err.Error())
				}
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
				}
			}
		}()
	}
	if w.Partners != nil {
		ids, err := w.Partners(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			start(id)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case c := <-r.newQueue:
			start(c)
		}
	}
}

// consumerChannel mở một kênh trên kết nối tiêu thụ dùng chung (một kết nối, mỗi đối tác một kênh).
func (r *Rabbit) consumerChannel() (*amqp.Channel, error) {
	r.cmu.Lock()
	defer r.cmu.Unlock()
	if r.cconn == nil || r.cconn.IsClosed() {
		conn, err := amqp.Dial(r.URL)
		if err != nil {
			return nil, fmt.Errorf("ingest: kết nối RabbitMQ (consumer): %w", err)
		}
		r.cconn = conn
	}
	return r.cconn.Channel()
}

func (r *Rabbit) consume(ctx context.Context, w Worker, client uuid.UUID) error {
	ch, err := r.consumerChannel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if _, err := declare(ch, client, r.MaxLen); err != nil {
		return err
	}
	err = inbox.ConsumeWith(ctx, ch, w.DB, QueueName(client), w.Handler, w.Log, w.Failure)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Close đóng kết nối phát và kết nối tiêu thụ.
func (r *Rabbit) Close() {
	r.mu.Lock()
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.mu.Unlock()
	r.cmu.Lock()
	if r.cconn != nil {
		_ = r.cconn.Close()
	}
	r.cmu.Unlock()
}
