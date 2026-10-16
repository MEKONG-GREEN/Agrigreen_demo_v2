package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// StaffNames trả tên hiển thị của cán bộ có trong danh bạ (mọi trạng thái).
func StaffNames(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := tx.Query(ctx, `SELECT actor_ref, display_name FROM pf_extension.staff WHERE actor_ref = ANY($1::uuid[])`,
		uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var a uuid.UUID
		var n string
		if err := rows.Scan(&a, &n); err != nil {
			return nil, err
		}
		out[a] = n
	}
	return out, rows.Err()
}
