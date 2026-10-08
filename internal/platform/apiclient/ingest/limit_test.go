package ingest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeTx struct {
	pgx.Tx
	open *atomic.Int64
	done atomic.Bool
}

func (t *fakeTx) Commit(context.Context) error {
	if t.done.CompareAndSwap(false, true) {
		t.open.Add(-1)
	}
	return nil
}

func (t *fakeTx) Rollback(context.Context) error {
	if t.done.CompareAndSwap(false, true) {
		t.open.Add(-1)
	}
	return pgx.ErrTxClosed
}

type fakeBeginner struct {
	open, peak atomic.Int64
	fail       bool
}

func (b *fakeBeginner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	if b.fail {
		return nil, errors.New("pool")
	}
	n := b.open.Add(1)
	for {
		p := b.peak.Load()
		if n <= p || b.peak.CompareAndSwap(p, n) {
			break
		}
	}
	return &fakeTx{open: &b.open}, nil
}

// W5.9: giao dịch inbox của bộ xử lý lô bị giới hạn TRƯỚC khi mượn kết nối (chống kẹt pool khi port chủ mở giao
// dịch thứ hai trên cùng pool); commit rồi rollback (defer) chỉ trả chỗ một lần; lỗi mở giao dịch trả chỗ.
func TestWorkerLimitsConcurrentInboxTx(t *testing.T) {
	b := &fakeBeginner{}
	s := &Service{DB: b, MaxConcurrent: 3}
	w := s.Worker()
	if w2 := s.Worker(); w2.DB.(limitedDB).sem != w.DB.(limitedDB).sem {
		t.Fatal("mọi Worker của một Service dùng chung một giới hạn")
	}
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := w.DB.BeginTx(context.Background(), pgx.TxOptions{})
			if err != nil {
				t.Error(err)
				return
			}
			time.Sleep(2 * time.Millisecond)
			_ = tx.Commit(context.Background())
			_ = tx.Rollback(context.Background())
		}()
	}
	wg.Wait()
	if p := b.peak.Load(); p < 1 || p > 3 {
		t.Fatalf("tối đa 3 giao dịch đồng thời, thấy %d", p)
	}
	if n := len(w.DB.(limitedDB).sem); n != 0 {
		t.Fatalf("chỗ phải được trả hết, còn %d", n)
	}
	b.fail = true
	if _, err := w.DB.BeginTx(context.Background(), pgx.TxOptions{}); err == nil || len(w.DB.(limitedDB).sem) != 0 {
		t.Fatal("lỗi mở giao dịch phải trả chỗ")
	}
	// Hết chỗ + ctx hủy → trả lỗi ctx, không treo.
	for range 3 {
		w.DB.(limitedDB).sem <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := w.DB.BeginTx(ctx, pgx.TxOptions{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hết chỗ → chờ tới hạn ctx: %v", err)
	}
	if (&Service{}).Worker().DB.(limitedDB).sem == nil || cap((&Service{}).Worker().DB.(limitedDB).sem) != DefaultMaxConcurrent {
		t.Fatal("mặc định DefaultMaxConcurrent")
	}
}
