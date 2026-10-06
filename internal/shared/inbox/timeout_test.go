package inbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHandlerTimeoutOption(t *testing.T) {
	cfg := consumeConfig{handlerTimeout: DefaultHandlerTimeout}
	if cfg.handlerTimeout != 2*time.Minute {
		t.Fatalf("mặc định = %v, muốn 2m", cfg.handlerTimeout)
	}
	WithHandlerTimeout(15 * time.Second)(&cfg)
	if cfg.handlerTimeout != 15*time.Second {
		t.Fatalf("WithHandlerTimeout không áp dụng: %v", cfg.handlerTimeout)
	}
}

// Handler treo: hết hạn → ctx của handler hủy với DeadlineExceeded, ctx consumer vẫn sống (không bị coi là dừng).
func TestHandlerContextDeadline(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	hctx, cancel := handlerContext(parent, 20*time.Millisecond)
	defer cancel()
	if _, ok := hctx.Deadline(); !ok {
		t.Fatal("thiếu hạn xử lý")
	}
	select {
	case <-hctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("ctx của handler không hết hạn")
	}
	if !errors.Is(hctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("hctx.Err() = %v, muốn DeadlineExceeded", hctx.Err())
	}
	if parent.Err() != nil {
		t.Fatalf("ctx consumer không được hủy theo: %v", parent.Err())
	}
}

func TestHandlerContextNoDeadline(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		hctx, cancel := handlerContext(context.Background(), d)
		if _, ok := hctx.Deadline(); ok {
			t.Fatalf("d=%v: không được đặt hạn", d)
		}
		cancel()
		if !errors.Is(hctx.Err(), context.Canceled) {
			t.Fatalf("d=%v: cancel phải hủy ctx", d)
		}
	}
}
