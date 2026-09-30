package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PlatformIdemKey là khóa chống trùng của xác nhận kênh platform: con trỏ luồng (đệm 20 chữ
// số để max(idem_key) = con trỏ lớn nhất đã ghi) + id phản hồi trên nền tảng.
func PlatformIdemKey(cursor int64, responseID uuid.UUID) string {
	return fmt.Sprintf("platform:%020d:%s", cursor, responseID)
}

// LastPlatformCursor trả con trỏ luồng lớn nhất đã ghi thành xác nhận kênh platform (0 nếu chưa có).
func LastPlatformCursor(ctx context.Context, tx pgx.Tx) (int64, error) {
	var key *string
	if err := tx.QueryRow(ctx, `SELECT max(idem_key) FROM mkg_fieldops.htx_confirmation
	    WHERE channel = 'platform' AND idem_key LIKE 'platform:%'`).Scan(&key); err != nil {
		return 0, err
	}
	if key == nil {
		return 0, nil
	}
	parts := strings.SplitN(*key, ":", 3)
	if len(parts) < 2 {
		return 0, nil
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
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
