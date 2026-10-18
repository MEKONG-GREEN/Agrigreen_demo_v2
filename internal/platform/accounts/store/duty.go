package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DutyHoldersAt trả user_ref (không trùng, sắp) của lần gán vai role còn hiệu lực tại now, thuộc phòng ban dept (rỗng =
// mọi), có cờ nhiệm vụ duty trên thuộc tính lần gán (pf 000142, chỉ mục GIN duties).
func DutyHoldersAt(ctx context.Context, tx pgx.Tx, role, dept, duty string, now time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT a.user_ref FROM pf_accounts.role_assignment_current a
	    JOIN pf_accounts.assignment_attr_current x ON x.tenant_id = a.tenant_id AND x.assignment_id = a.assignment_id
	  WHERE a.active AND a.valid_from <= $4 AND (a.valid_to IS NULL OR a.valid_to > $4)
	    AND ($1 = '' OR a.role = $1) AND ($2 = '' OR $2 = ANY (a.department_codes)) AND x.duties @> ARRAY[$3]::text[]
	  ORDER BY a.user_ref`, role, dept, duty, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
