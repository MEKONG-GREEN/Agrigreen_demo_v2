package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PendingReviewPeriodPage bounds native Progress reads per inbox request. The
// extra record determines the next page without projecting it or truncating rows.
const PendingReviewPeriodPage = 25

func PendingReviewPeriods(ctx context.Context, tx pgx.Tx, before uuid.UUID) ([]Period, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period p
 WHERE p.report_type <> 'adhoc'
 AND ($1::uuid='00000000-0000-0000-0000-000000000000'::uuid OR p.id<$1::uuid)
 AND NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e
   WHERE e.tenant_id=p.tenant_id AND e.period_id=p.id AND e.action IN ('closed','cancelled'))
 ORDER BY p.id DESC LIMIT $2`, before, PendingReviewPeriodPage+1)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}
