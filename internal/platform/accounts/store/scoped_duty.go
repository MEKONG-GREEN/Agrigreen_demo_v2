package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ScopedDutyHoldersAt preserves UnitRef precedence (including legacy null/zero
// fallback to OrgRef) while matching every restriction on the same assignment.
func ScopedDutyHoldersAt(ctx context.Context, tx pgx.Tx, issuer uuid.UUID, role, dept, duty string, at time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT a.user_ref FROM pf_accounts.role_assignment_current a
        JOIN pf_accounts.assignment_attr_current x ON x.tenant_id=a.tenant_id AND x.assignment_id=a.assignment_id
      WHERE a.active AND a.valid_from <= $5 AND (a.valid_to IS NULL OR a.valid_to > $5)
        AND a.role=$2 AND $3=ANY(a.department_codes) AND x.duties @> ARRAY[$4]::text[]
        AND COALESCE(NULLIF(a.unit_ref,'00000000-0000-0000-0000-000000000000'::uuid),a.org_ref)=$1
      ORDER BY a.user_ref`, issuer, role, dept, duty, at)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
