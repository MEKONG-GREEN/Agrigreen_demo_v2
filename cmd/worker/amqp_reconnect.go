package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// P0-WORKER-REPORTS (#128): worker KHÔNG thoát khi RabbitMQ mất kết nối (trước đây: kênh đóng → run trả lỗi → tiến trình
// thoát, 20 lần khởi động lại quan sát được). Vòng giám sát kết nối lại với độ lùi tăng dần, khai báo lại hàng đợi và
// đăng ký lại mọi consumer; ctx hủy (SIGTERM) → dừng êm như cũ (consumer không NACK, broker trả thông điệp chưa ACK).

// Độ lùi kết nối lại: amqpBackoffBase·2^(n-1), trần amqpBackoffMax.
const (
	amqpBackoffBase = time.Second
	amqpBackoffMax  = 30 * time.Second
)

// amqpBackoff trả độ lùi trước lần kết nối lại thứ n (n ≥ 1).
func amqpBackoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > 6 { // 1s·2^5 = 32s > trần
		return amqpBackoffMax
	}
	return min(amqpBackoffBase<<(n-1), amqpBackoffMax)
}

// sleepCtx chờ d hoặc tới khi ctx hủy (trả ctx.Err()).
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// amqpLoop giữ một phiên (kết nối + kênh đã khai báo hàng đợi) chạy qua các lần đứt. S là phiên — kiểm thử dùng phiên
// giả, không cần broker thật.
type amqpLoop[S any] struct {
	connect func(ctx context.Context) (S, error) // quay số + mở kênh + khai báo hàng đợi
	serve   func(ctx context.Context, s S) error // tiêu thụ tới khi lỗi (kênh/kết nối đóng) hoặc ctx hủy
	close   func(s S)
	backoff func(n int) time.Duration
	sleep   func(ctx context.Context, d time.Duration) error
	log     *slog.Logger
}

// run phục vụ phiên first, rồi mỗi lần serve trả về (khi ctx còn sống) thì đóng phiên, lùi và kết nối lại tới khi được.
// Chỉ trả về khi ctx hủy (trả ctx.Err()).
func (l amqpLoop[S]) run(ctx context.Context, first S) error {
	s := first
	for {
		err := l.serve(ctx, s)
		l.close(s)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			err = errors.New("phiên RabbitMQ kết thúc")
		}
		l.log.Warn("mất kết nối RabbitMQ — kết nối lại", "event", "amqp.reconnect", "outcome", "retry", "error", err.Error())
		for n := 1; ; n++ {
			if err := l.sleep(ctx, l.backoff(n)); err != nil {
				return err
			}
			next, err := l.connect(ctx)
			if err == nil {
				s = next
				l.log.Info("đã kết nối lại RabbitMQ", "event", "amqp.reconnect", "attempt", n, "outcome", "ok")
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			l.log.Warn("kết nối lại RabbitMQ lỗi", "event", "amqp.reconnect", "attempt", n, "outcome", "retry", "error", err.Error())
		}
	}
}

// amqpSub là một consumer đã có kênh + hàng đợi trên phiên hiện tại.
type amqpSub struct {
	c  consumer
	ch *amqp.Channel
	db tenant.Beginner
}

// amqpSession là kết nối RabbitMQ cùng các kênh consumer đã khai báo hàng đợi.
type amqpSession struct {
	conn *amqp.Connection
	subs []amqpSub
}

// dialSession quay số, mở mỗi consumer một kênh và khai báo (lại) hàng đợi + DLQ + bind (idempotent).
func dialSession(amqpURL string, cs []consumer, dbFor func(consumer) tenant.Beginner) (*amqpSession, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}
	s := &amqpSession{conn: conn}
	for _, c := range cs {
		ch, err := conn.Channel()
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := inbox.DeclareQueue(ch, c.name, c.events); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("khai báo hàng đợi %s: %w", c.name, err)
		}
		s.subs = append(s.subs, amqpSub{c: c, ch: ch, db: dbFor(c)})
	}
	return s, nil
}

// serveSession chạy mọi consumer của phiên; consumer đầu tiên dừng (kênh đóng) hoặc kết nối đóng → hủy các consumer
// còn lại, chờ chúng dừng hẳn rồi trả lỗi để vòng giám sát kết nối lại.
func serveSession(ctx context.Context, s *amqpSession, log *slog.Logger, rec inbox.FailureRecorder) error {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	closed := s.conn.NotifyClose(make(chan *amqp.Error, 1))
	errs := make(chan error, len(s.subs))
	var wg sync.WaitGroup
	for _, sub := range s.subs {
		wg.Add(1)
		go func(sub amqpSub) {
			defer wg.Done()
			errs <- inbox.ConsumeWith(sctx, sub.ch, sub.db, sub.c.name, sub.c.handler, log, rec)
		}(sub)
	}
	var err error
	select {
	case err = <-errs:
	case e := <-closed:
		err = errors.New("kết nối RabbitMQ đã đóng")
		if e != nil {
			err = fmt.Errorf("kết nối RabbitMQ đã đóng: %w", e)
		}
	case <-ctx.Done():
		err = ctx.Err()
	}
	cancel()
	wg.Wait()
	return err
}

// currentConn giữ kết nối đang dùng cho /readyz và dọn dẹp khi dừng.
type currentConn struct {
	p atomic.Pointer[amqp.Connection]
}

func (c *currentConn) set(conn *amqp.Connection) { c.p.Store(conn) }

func (c *currentConn) isClosed() bool {
	conn := c.p.Load()
	return conn == nil || conn.IsClosed()
}

func (c *currentConn) close() {
	if conn := c.p.Load(); conn != nil {
		_ = conn.Close()
	}
}
