package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UnitManagerCandidate is one effective province assignment at a node whose internal
// unit (or, without one, department) is among the asked units. Leadership is decided
// by the caller from the attribute catalog.
type UnitManagerCandidate struct {
	UserRef       uuid.UUID
	MgmtLevel     string
	ProvinceGroup string
	Duties        []string
	InternalUnit  string
	Departments   []string
}

// UnitManagerCandidatesAt preserves UnitRef precedence (legacy null/zero falls back
// to OrgRef) and validity at `at`, as ScopedDutyHoldersAt.
func UnitManagerCandidatesAt(ctx context.Context, tx pgx.Tx, node uuid.UUID, role string, units []string, at time.Time) ([]UnitManagerCandidate, error) {
	rows, err := tx.Query(ctx, `SELECT a.user_ref, coalesce(a.mgmt_level,''), coalesce(x.province_group,''), coalesce(x.duties,'{}'::text[]),
        coalesce(x.internal_unit_code,''), a.department_codes
      FROM pf_accounts.role_assignment_current a
      LEFT JOIN pf_accounts.assignment_attr_current x ON x.tenant_id=a.tenant_id AND x.assignment_id=a.assignment_id
      WHERE a.active AND a.valid_from <= $4 AND (a.valid_to IS NULL OR a.valid_to > $4) AND a.role=$2
        AND COALESCE(NULLIF(a.unit_ref,'00000000-0000-0000-0000-000000000000'::uuid),a.org_ref)=$1
        AND (x.internal_unit_code = ANY($3::text[]) OR (coalesce(x.internal_unit_code,'')='' AND a.department_codes && $3::text[]))
      ORDER BY a.user_ref, a.assignment_id`, node, role, units, at)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (UnitManagerCandidate, error) {
		var c UnitManagerCandidate
		err := r.Scan(&c.UserRef, &c.MgmtLevel, &c.ProvinceGroup, &c.Duties, &c.InternalUnit, &c.Departments)
		return c, err
	})
}
