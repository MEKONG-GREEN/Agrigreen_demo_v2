package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeSession là phiên RabbitMQ giả: serve trả lỗi "đứt kết nối" ngay (dropped) hoặc chặn tới khi ctx hủy.
type fakeSession struct {
	id      int
	dropped bool
	closed  bool
}

type fakeBroker struct {
	mu        sync.Mutex
	dialFails int // số lần quay số lỗi liên tiếp còn lại
	drops     int // số phiên tiếp theo sẽ đứt ngay khi phục vụ
	sessions  []*fakeSession
	backoffs  []int
	served    chan int
}

func (b *fakeBroker) connect(context.Context) (*fakeSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dialFails > 0 {
		b.dialFails--
		return nil, errors.New("dial: connection refused")
	}
	s := &fakeSession{id: len(b.sessions)}
	if b.drops > 0 {
		b.drops--
		s.dropped = true
	}
	b.sessions = append(b.sessions, s)
	return s, nil
}

func (b *fakeBroker) serve(ctx context.Context, s *fakeSession) error {
	b.served <- s.id
	if s.dropped {
		return errors.New("inbox: kênh đã đóng")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (b *fakeBroker) close(s *fakeSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s.closed = true
}

func (b *fakeBroker) loop() amqpLoop[*fakeSession] {
	return amqpLoop[*fakeSession]{
		connect: b.connect, serve: b.serve, close: b.close,
		backoff: func(n int) time.Duration {
			b.mu.Lock()
			b.backoffs = append(b.backoffs, n)
			b.mu.Unlock()
			return time.Millisecond
		},
		sleep: sleepCtx,
		log:   slog.New(slog.DiscardHandler),
	}
}

// TestAMQPLoopReconnects — #128: mất kết nối RabbitMQ → worker không thoát: đóng phiên cũ, lùi tăng dần qua các lần quay
// số lỗi, kết nối lại và phục vụ (khai báo + đăng ký lại consumer nằm trong connect); SIGTERM → dừng êm, đóng phiên.
func TestAMQPLoopReconnects(t *testing.T) {
	b := &fakeBroker{served: make(chan int, 16)}
	first, err := b.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.dropped = true // phiên đầu đứt
	b.mu.Lock()
	b.dialFails, b.drops = 2, 1 // hai lần quay số lỗi, phiên kế đứt tiếp, phiên sau đó ổn định
	b.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.loop().run(ctx, first) }()
	for want := 0; want <= 2; want++ {
		select {
		case got := <-b.served:
			if got != want {
				t.Fatalf("phục vụ phiên %d, muốn %d", got, want)
			}
		case err := <-done:
			t.Fatalf("vòng giám sát thoát khi mất kết nối: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatalf("không kết nối lại tới phiên %d", want)
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dừng êm phải trả context.Canceled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("không dừng khi ctx hủy")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.sessions {
		if !s.closed {
			t.Fatalf("phiên %d chưa đóng", s.id)
		}
	}
	// Đứt phiên 0 → lùi 1 (lỗi), 2 (lỗi), 3 (được); đứt phiên 1 → lùi lại từ 1.
	if want := []int{1, 2, 3, 1}; !slices.Equal(b.backoffs, want) {
		t.Fatalf("độ lùi %v, muốn %v", b.backoffs, want)
	}
}

// TestAMQPLoopStopsDuringBackoff: SIGTERM trong lúc chờ kết nối lại → trả ngay, không quay số thêm.
func TestAMQPLoopStopsDuringBackoff(t *testing.T) {
	b := &fakeBroker{served: make(chan int, 4), dialFails: 1 << 30}
	first := &fakeSession{dropped: true}
	l := b.loop()
	l.backoff = func(int) time.Duration { return time.Hour }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.run(ctx, first) }()
	<-b.served
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("không dừng trong lúc lùi")
	}
	if !first.closed {
		t.Fatal("phiên đứt chưa đóng")
	}
}

func TestAMQPBackoff(t *testing.T) {
	for n, want := range map[int]time.Duration{0: time.Second, 1: time.Second, 2: 2 * time.Second, 5: 16 * time.Second, 6: 30 * time.Second, 50: 30 * time.Second} {
		if got := amqpBackoff(n); got != want {
			t.Errorf("amqpBackoff(%d) = %v, muốn %v", n, got, want)
		}
	}
}
