package ingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Queue là hàng đợi RIÊNG của từng đối tác (R-INT-04): mỗi khách hàng API một hàng đợi + một DLQ, consumer
// riêng — đối tác gửi dồn hay gửi bản ghi độc không làm chậm/chặn đối tác khác.
type Queue interface {
	// Publish đưa thông điệp lô vào hàng đợi của đối tác client. Hàng đợi đầy → ErrQueueSaturated.
	Publish(ctx context.Context, client uuid.UUID, env outbox.Envelope) error
	// Depth là số thông điệp đang chờ trong hàng đợi của đối tác (không tính DLQ).
	Depth(ctx context.Context, client uuid.UUID) (int, error)
	// Run tiêu thụ mọi hàng đợi đối tác tới khi ctx hủy.
	Run(ctx context.Context, w Worker) error
}

// Worker là phần xử lý mà Queue gọi: inbox.Process(DB, <tên hàng đợi>, env, Handler); lỗi → giao lại, lần cuối
// (inbox.FinalAttempt) → DLQ và Failure ghi kết cục dead_lettered.
type Worker struct {
	DB      tenant.Beginner
	Handler inbox.Handler
	Failure inbox.FailureRecorder
	// Partners liệt kê đối tác cần mở consumer khi khởi động (Rabbit); hàng đợi mới mở khi Publish lần đầu.
	Partners func(ctx context.Context) ([]uuid.UUID, error)
	Log      *slog.Logger

	// process thay inbox.Process (kiểm thử đơn vị không DB).
	process func(ctx context.Context, queue string, env outbox.Envelope) error
}

// QueueName là tên hàng đợi của đối tác (DLQ: + ".dlq").
func QueueName(client uuid.UUID) string { return QueuePrefix + client.String() }

// ---------------------------------------------------------------------------
// Memory: hàng đợi trong tiến trình (dev/test, một bản sao) — cùng ngữ nghĩa với quorum queue: giới hạn độ
// sâu, đếm lần giao, quá inbox.DeliveryLimit → DLQ; mỗi đối tác một goroutine tiêu thụ.
// ---------------------------------------------------------------------------

type memMsg struct {
	env       outbox.Envelope
	delivered int64 // số lần giao thất bại trước đó (x-delivery-count)
}

type memQueue struct {
	pending []memMsg
	dlq     []outbox.Envelope
	running bool
}

// Memory là Queue trong tiến trình. Không bền: chỉ cho dev/test (production cần RabbitMQ — gateway từ chối).
type Memory struct {
	// MaxLen: độ sâu tối đa mỗi đối tác (x-max-length, overflow reject-publish); ≤ 0 → DefaultMaxQueued.
	MaxLen int
	// Backoff trước khi trả lại thông điệp lỗi (mặc định inbox.RetryBackoff; kiểm thử đặt 0).
	Backoff func(deliveryCount int64) time.Duration

	mu     sync.Mutex
	queues map[uuid.UUID]*memQueue
	wake   chan uuid.UUID
}

// NewMemory tạo hàng đợi trong tiến trình.
func NewMemory(maxLen int) *Memory {
	return &Memory{MaxLen: maxLen, Backoff: inbox.RetryBackoff, queues: map[uuid.UUID]*memQueue{}, wake: make(chan uuid.UUID, 1024)}
}

func (m *Memory) q(client uuid.UUID) *memQueue {
	q, ok := m.queues[client]
	if !ok {
		q = &memQueue{}
		m.queues[client] = q
	}
	return q
}

// Publish thêm vào cuối hàng đợi của đối tác; đầy → ErrQueueSaturated.
func (m *Memory) Publish(_ context.Context, client uuid.UUID, env outbox.Envelope) error {
	m.mu.Lock()
	q := m.q(client)
	if Saturation(len(q.pending), m.MaxLen) {
		m.mu.Unlock()
		return ErrQueueSaturated
	}
	q.pending = append(q.pending, memMsg{env: env})
	m.mu.Unlock()
	select {
	case m.wake <- client:
	default:
	}
	return nil
}

// Depth trả số thông điệp đang chờ của đối tác.
func (m *Memory) Depth(_ context.Context, client uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.q(client).pending), nil
}

// DeadLettered trả các thông điệp đã vào DLQ của đối tác (kiểm thử, vận hành dev).
func (m *Memory) DeadLettered(client uuid.UUID) []outbox.Envelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]outbox.Envelope(nil), m.q(client).dlq...)
}

// pop lấy thông điệp đầu hàng đợi của đối tác.
func (m *Memory) pop(client uuid.UUID) (memMsg, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.q(client)
	if len(q.pending) == 0 {
		return memMsg{}, false
	}
	msg := q.pending[0]
	q.pending = q.pending[1:]
	return msg, true
}

// deliver xử lý một thông điệp như inbox.ConsumeWith: lỗi → ghi thất bại, trả lại cuối hàng đợi (tăng đếm) hoặc
// DLQ ở lần cuối.
func (m *Memory) deliver(ctx context.Context, w Worker, client uuid.UUID, msg memMsg) (requeued bool) {
	queue := QueueName(client)
	var err error
	if w.process != nil {
		err = w.process(ctx, queue, msg.env)
	} else {
		_, err = inbox.Process(ctx, w.DB, queue, msg.env, w.Handler)
	}
	if err == nil {
		return false
	}
	if ctx.Err() != nil { // đang dừng: trả lại nguyên trạng
		m.mu.Lock()
		q := m.q(client)
		q.pending = append([]memMsg{msg}, q.pending...)
		m.mu.Unlock()
		return false
	}
	final := inbox.FinalAttempt(msg.delivered)
	if w.Log != nil {
		w.Log.Warn("xử lý lô nạp thất bại", "event", "ingest.batch_failed", "queue", queue, "batch_id", msg.env.EventID,
			"delivery_count", msg.delivered, "dead_lettered", final, "outcome", "error")
	}
	if w.Failure != nil {
		if rerr := w.Failure(ctx, inbox.Failure{Consumer: queue, Envelope: msg.env, Err: err, DeliveryCount: msg.delivered, DeadLettered: final}); rerr != nil && w.Log != nil {
			w.Log.Error("không ghi được kết cục lô", "event", "ingest.failure_record", "batch_id", msg.env.EventID, "outcome", "error")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.q(client)
	if final {
		q.dlq = append(q.dlq, msg.env)
		return false
	}
	msg.delivered++
	q.pending = append(q.pending, msg)
	return true
}

// Drain xử lý đồng bộ mọi thông điệp đang chờ (kể cả lần giao lại) tới khi mọi hàng đợi rỗng — kiểm thử và lệnh
// vận hành dev. Không chờ Backoff.
func (m *Memory) Drain(ctx context.Context, w Worker) {
	for {
		m.mu.Lock()
		var next []uuid.UUID
		for c, q := range m.queues {
			if len(q.pending) > 0 {
				next = append(next, c)
			}
		}
		m.mu.Unlock()
		if len(next) == 0 || ctx.Err() != nil {
			return
		}
		for _, c := range next {
			if msg, ok := m.pop(c); ok {
				m.deliver(ctx, w, c, msg)
			}
		}
	}
}

// Run tiêu thụ tới khi ctx hủy: mỗi đối tác một goroutine (vách ngăn), xử lý tuần tự trong hàng đợi của nó.
func (m *Memory) Run(ctx context.Context, w Worker) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	start := func(client uuid.UUID) {
		m.mu.Lock()
		q := m.q(client)
		if q.running {
			m.mu.Unlock()
			return
		}
		q.running = true
		m.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { m.mu.Lock(); m.q(client).running = false; m.mu.Unlock() }()
			for ctx.Err() == nil {
				msg, ok := m.pop(client)
				if !ok {
					return
				}
				if m.deliver(ctx, w, client, msg) && m.Backoff != nil {
					select {
					case <-ctx.Done():
					case <-time.After(m.Backoff(msg.delivered)):
					}
				}
			}
		}()
	}
	waiting := func() []uuid.UUID {
		m.mu.Lock()
		defer m.mu.Unlock()
		var out []uuid.UUID
		for c, q := range m.queues {
			if len(q.pending) > 0 && !q.running {
				out = append(out, c)
			}
		}
		return out
	}
	for _, c := range waiting() {
		start(c)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case c := <-m.wake:
			start(c)
		case <-tick.C: // thông điệp đến đúng lúc goroutine của đối tác vừa thoát
			for _, c := range waiting() {
				start(c)
			}
		}
	}
}

// errNoQueue: cổng chưa cấu hình hàng đợi.
var errNoQueue = errors.New("ingest: chưa cấu hình hàng đợi")
