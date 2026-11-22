package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ArchivePeriod struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	ReportType string    `json:"report_type"`
}

// ArchivePeriods offers only periods with stored exports under tenant RLS, never drafts.
func ArchivePeriods(ctx context.Context, tx pgx.Tx, year int, kind, search string, offset, limit int) ([]ArchivePeriod, error) {
	rows, err := tx.Query(ctx, `SELECT p.id,p.name,p.report_type FROM pf_extension.khtc_period p
 WHERE p.plan_year=$1 AND ($2='' OR p.report_type=$2) AND ($3='' OR strpos(lower(p.name),lower($3))>0)
 AND EXISTS(SELECT 1 FROM pf_extension.khtc_report_export x WHERE x.period_id=p.id AND x.tenant_id=p.tenant_id)
 AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.period_id=p.id AND e.action='cancelled')
 ORDER BY p.event_time DESC,p.id DESC OFFSET $4 LIMIT $5`, year, kind, search, offset, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ArchivePeriod, error) {
		var x ArchivePeriod
		err := r.Scan(&x.ID, &x.Name, &x.ReportType)
		return x, err
	})
}
