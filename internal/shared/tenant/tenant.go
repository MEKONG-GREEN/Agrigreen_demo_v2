// Package tenant là cửa duy nhất vào DB nghiệp vụ (RB-2, mục 9).
//
// Mọi truy cập DB đi qua WithTenantTx. Ngữ cảnh tenant đặt bằng
// set_config('app.tenant_id', $1, true) — tức SET LOCAL — trong từng giao dịch
// (R-30). Không bao giờ đặt ở mức phiên: PgBouncer transaction mode sẽ làm
// rò rỉ sang request khác.
package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrMissing báo thiếu tenant trong ngữ cảnh.
var ErrMissing = errors.New("tenant: thiếu tenant_id")

type ctxKey struct{}

// WithID gắn tenant vào context. Chỉ middleware xác thực được gọi — tenant
// lấy từ token, không bao giờ từ path, query hay body (R-16).
func WithID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext lấy tenant đã xác thực.
func FromContext(ctx context.Context) (uuid.UUID, error) {
	id, ok := ctx.Value(ctxKey{}).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return uuid.Nil, ErrMissing
	}
	return id, nil
}

// Beginner là thứ mở được giao dịch — *pgxpool.Pool hoặc pgx.Tx (savepoint).
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

var _ Beginner = (*pgxpool.Pool)(nil)

// WithTenantTx mở giao dịch, đặt app.tenant_id bằng SET LOCAL, chạy fn.
// fn trả lỗi → rollback. Không có đường nào khác để chạm bảng nghiệp vụ.
func WithTenantTx(ctx context.Context, db Beginner, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return WithTenantTxOptions(ctx, db, tid, pgx.TxOptions{}, fn)
}

// WithTenantTxOptions như WithTenantTx, cho phép chọn mức cô lập.
func WithTenantTxOptions(ctx context.Context, db Beginner, tid uuid.UUID, opts pgx.TxOptions, fn func(pgx.Tx) error) (err error) {
	if tid == uuid.Nil {
		return ErrMissing
	}
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("tenant: mở giao dịch: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	// is_local = true: giá trị tự hết khi giao dịch kết thúc.
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tid.String()); err != nil {
		return fmt.Errorf("tenant: đặt ngữ cảnh: %w", err)
	}
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("tenant: commit: %w", err)
	}
	return nil
}

// FromCtxTx lấy tenant từ context rồi gọi WithTenantTx.
func FromCtxTx(ctx context.Context, db Beginner, fn func(pgx.Tx) error) error {
	tid, err := FromContext(ctx)
	if err != nil {
		return err
	}
	return WithTenantTx(ctx, db, tid, fn)
}
