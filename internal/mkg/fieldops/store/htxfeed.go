package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PlatformIdemKey là khóa chống trùng của xác nhận kênh platform: con trỏ luồng (đệm 20 chữ
// số để max(idem_key) = con trỏ lớn nhất đã ghi) + id phản hồi trên nền tảng.
func PlatformIdemKey(cursor int64, responseID uuid.UUID) string {
	return fmt.Sprintf("platform:%020d:%s", cursor, responseID)
}

// LastPlatformCursor trả con trỏ luồng lớn nhất đã ghi thành xác nhận kênh platform hoặc đã gác
// (htx_feed_parked — mục lỗi vĩnh viễn không được đọc lại sau khởi động); 0 nếu chưa có.
func LastPlatformCursor(ctx context.Context, tx pgx.Tx) (int64, error) {
	var key *string
	var parked int64
	if err := tx.QueryRow(ctx, `SELECT (SELECT max(idem_key) FROM mkg_fieldops.htx_confirmation
	    WHERE channel = 'platform' AND idem_key LIKE 'platform:%'),
	    coalesce((SELECT max(feed_cursor) FROM mkg_fieldops.htx_feed_parked), 0)`).Scan(&key, &parked); err != nil {
		return 0, err
	}
	if key == nil {
		return parked, nil
	}
	parts := strings.SplitN(*key, ":", 3)
	if len(parts) < 2 {
		return parked, nil
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return parked, nil
	}
	return max(n, parked), nil
}

// ParkedResponse là một phản hồi HTX bị gác vì lỗi vĩnh viễn (htx_feed_parked, mkg 000033).
type ParkedResponse struct {
	Cursor       int64
	ResponseID   uuid.UUID
	StatementRef uuid.UUID
	Code         string // mã lỗi ổn định (mã problem / SQLSTATE)
	Detail       string
	Payload      []byte // JSON phản hồi như bộ điều hợp đọc
	ParkedAt     time.Time
}

// ParkResponse gác một phản hồi (gọi lặp cùng (con trỏ, id phản hồi) không ghi thêm).
func ParkResponse(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p ParkedResponse) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.htx_feed_parked
	    (tenant_id, feed_cursor, response_id, statement_ref, error_code, error_detail, payload, parked_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
	  ON CONFLICT (tenant_id, feed_cursor, response_id) DO NOTHING`,
		tid, p.Cursor, p.ResponseID, p.StatementRef, p.Code, p.Detail, string(p.Payload), p.ParkedAt.UTC())
	return err
}

// ParkedResponses đọc các phản hồi bị gác theo con trỏ tăng dần (vận hành, kiểm thử).
func ParkedResponses(ctx context.Context, tx pgx.Tx, after int64, limit int) ([]ParkedResponse, error) {
	rows, err := tx.Query(ctx, `SELECT feed_cursor, response_id, statement_ref, error_code, error_detail, payload, parked_at
	    FROM mkg_fieldops.htx_feed_parked WHERE feed_cursor > $1 ORDER BY feed_cursor, response_id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParkedResponse
	for rows.Next() {
		var p ParkedResponse
		if err := rows.Scan(&p.Cursor, &p.ResponseID, &p.StatementRef, &p.Code, &p.Detail, &p.Payload, &p.ParkedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReconByStatement đọc đối soát theo statement_ref (khóa dòng); nil nếu không có.
func ReconByStatement(ctx context.Context, tx pgx.Tx, statementRef uuid.UUID) (*Recon, error) {
	r, err := scanRecon(tx.QueryRow(ctx, "SELECT "+reconCols+` FROM mkg_fieldops.buyside_reconciliation_current
	    WHERE statement_ref = $1 FOR UPDATE`, statementRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
