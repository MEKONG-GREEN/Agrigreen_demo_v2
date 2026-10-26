package tenant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// blockingBeginner giả pool cạn: BeginTx chờ tới khi ctx hết.
type blockingBeginner struct{}

func (blockingBeginner) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// P0-POOL-SHARED (#126): pool cạn → lỗi ErrAcquireTimeout sau AcquireTimeout, không treo theo ctx không hạn.
func TestWithTenantTxAcquireTimeout(t *testing.T) {
	prev := AcquireTimeout()
	t.Cleanup(func() { SetAcquireTimeout(prev) })
	SetAcquireTimeout(50 * time.Millisecond)
	start := time.Now()
	err := WithTenantTx(context.Background(), blockingBeginner{}, uuid.New(), func(pgx.Tx) error { return nil })
	if !errors.Is(err, ErrAcquireTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lỗi = %v; muốn ErrAcquireTimeout", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("chờ quá hạn AcquireTimeout")
	}
	// ctx của người gọi hết trước: trả lỗi của ctx, không gắn ErrAcquireTimeout.
	SetAcquireTimeout(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = WithTenantTx(ctx, blockingBeginner{}, uuid.New(), func(pgx.Tx) error { return nil })
	if errors.Is(err, ErrAcquireTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lỗi ctx người gọi = %v", err)
	}
	// ≤ 0 = không giới hạn (chỉ theo ctx).
	SetAcquireTimeout(-1)
	if AcquireTimeout() != 0 {
		t.Fatal("SetAcquireTimeout(-1) phải tắt giới hạn")
	}
}
