package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// OpenPeriodCount counts the four live KHTC states without loading a page or
// projecting Forms. A terminal event permanently removes its period from this
// ledger's open set, matching terminalOf. Tenant RLS applies to both ledgers.
func OpenPeriodCount(ctx context.Context, tx pgx.Tx, includeAdhoc bool) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_period p
 WHERE ($1 OR p.report_type <> 'adhoc')
 AND NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e
   WHERE e.tenant_id=p.tenant_id AND e.period_id=p.id AND e.action IN ('closed','cancelled'))`, includeAdhoc).Scan(&count)
	return count, err
}
