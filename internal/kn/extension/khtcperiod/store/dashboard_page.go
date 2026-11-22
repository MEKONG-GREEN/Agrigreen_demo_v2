package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DashboardPage uses a bounded keyset scan. Filtering is applied before LIMIT,
// so unrelated departments or restricted adhoc records cannot starve later pages.
func DashboardPage(ctx context.Context, tx pgx.Tx, year int, department string, before uuid.UUID, includeAdhoc bool) ([]Period, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period
 WHERE plan_year=$1 AND ($2='' OR $2=ANY(departments))
 AND ($3::uuid='00000000-0000-0000-0000-000000000000'::uuid OR id<$3::uuid)
 AND ($4 OR report_type<>'adhoc') ORDER BY id DESC LIMIT 100`, year, department, before, includeAdhoc)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}
