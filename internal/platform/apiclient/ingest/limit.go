package ingest

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DefaultMaxConcurrent là số lô xử lý đồng thời tối đa (mọi đối tác cộng lại) của một bản sao cổng.
//
// Bộ xử lý giữ giao dịch inbox (một kết nối) trong khi port module chủ ghi từng bản ghi bằng giao dịch RIÊNG trên
// CÙNG pool của đơn vị (sổ đệm Staging, port W5.6). Không giới hạn, mỗi đối tác một consumer (Memory.Run,
// Rabbit.Run): khi số đối tác có lô đang xử lý ≥ cỡ pool (AG_DB_MAX_CONNS, mặc định 20), mọi kết nối bị giao
// dịch inbox giữ, mọi port chủ chờ kết nối thứ hai → kẹt vĩnh viễn, kéo theo mọi lời gọi của cổng (xác thực,
// sổ đo lường) — phát hiện bởi kiểm thử tải W5.9 (20 đối tác, pool 16). Giữ giá trị này nhỏ hơn hẳn cỡ pool.
const DefaultMaxConcurrent = 4

// limitedDB giới hạn số giao dịch inbox đang mở: lấy chỗ TRƯỚC khi mượn kết nối, trả chỗ khi commit/rollback.
type limitedDB struct {
	inner tenant.Beginner
	sem   chan struct{}
}

func (l limitedDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	tx, err := l.inner.BeginTx(ctx, opts)
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitedTx{Tx: tx, release: sync.OnceFunc(func() { <-l.sem })}, nil
}

type limitedTx struct {
	pgx.Tx
	release func()
}

func (t *limitedTx) Commit(ctx context.Context) error {
	defer t.release()
	return t.Tx.Commit(ctx)
}

func (t *limitedTx) Rollback(ctx context.Context) error {
	defer t.release()
	return t.Tx.Rollback(ctx)
}
