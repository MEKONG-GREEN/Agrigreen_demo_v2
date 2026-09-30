// Package store: truy cập DB của mkg/orders (schema mkg_orders, vai orders_app). Mọi hàm chạy trong
// giao dịch tenant đã mở ở app/ (RLS là lớp chặn cuối, RB-2).
//
// Mỗi dòng sổ là ảnh chụp đầy đủ của aggregate sau sự kiện; bảng chiếu = dòng mới nhất theo seq của
// chuỗi — cùng một câu SQL dùng cho ghi thường (R-21, cùng giao dịch) và dựng lại (R-22).
// Số thập phân đọc qua ::text vào decimal.Decimal (R-10, không float).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Meta là các cột chung của dòng sổ do app/ đặt (seq, recorded_at do DB tính).
type Meta struct {
	ID               uuid.UUID
	Seq              int64
	RecordedAt       time.Time
	EventTime        time.Time
	ActorRef         uuid.UUID
	IdemKey          string
	RuleVersion      string
	SupersedesID     *uuid.UUID
	CorrectionReason *string
}

// Lock tuần tự hóa ghi theo một khóa logic trong tenant (vd chuỗi của aggregate, HTX của hợp đồng).
func Lock(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "mkg_orders:"+tid.String()+":"+key)
	return err
}

// NextCode trả số thứ tự kế tiếp của mã chứng từ (đếm thực thể trong bảng chiếu; gọi sau Lock "code:<bảng>").
func NextCode(ctx context.Context, tx pgx.Tx, table, idCol string) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, "SELECT count(DISTINCT "+idCol+") + 1 FROM mkg_orders."+table).Scan(&n)
	return n, err
}

// EntityByIdem tìm thực thể đã ghi với idem_key (gọi lặp — R-51, lớp hai sau middleware).
func EntityByIdem(ctx context.Context, tx pgx.Tx, table, idCol, idem string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT "+idCol+" FROM mkg_orders."+table+" WHERE idem_key = $1 LIMIT 1", idem).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// EventIDByIdem trả id dòng sổ ghi với idem_key.
func EventIDByIdem(ctx context.Context, tx pgx.Tx, table, idem string) (*uuid.UUID, error) {
	return EntityByIdem(ctx, tx, table, "id", idem)
}

// rebuildProjection dựng lại bảng chiếu của một thực thể (where = "col = $1") hoặc toàn tenant (where rỗng).
func rebuildProjection(ctx context.Context, tx pgx.Tx, current, event, distinct, cols, where string, args ...any) (int64, error) {
	del := "DELETE FROM mkg_orders." + current
	sel := "SELECT DISTINCT ON (" + distinct + ") tenant_id, " + cols + ", id, seq FROM mkg_orders." + event
	if where != "" {
		del += " WHERE " + where
		sel += " WHERE " + where
	}
	sel += " ORDER BY " + distinct + ", seq DESC"
	if _, err := tx.Exec(ctx, del, args...); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, "INSERT INTO mkg_orders."+current+" (tenant_id, "+cols+", last_event_id, last_seq) "+sel, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func noRows[T any](v T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
