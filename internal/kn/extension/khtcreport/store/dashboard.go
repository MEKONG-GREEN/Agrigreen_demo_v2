package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type DashboardExport struct {
	PeriodID uuid.UUID
	At       time.Time
}

func DashboardExports(ctx context.Context, tx pgx.Tx, year int, department string) ([]DashboardExport, error) {
	rows, err := tx.Query(ctx, `SELECT p.id,x.event_time FROM pf_extension.khtc_report_export x JOIN pf_extension.khtc_period p ON p.id=x.period_id AND p.tenant_id=x.tenant_id WHERE p.plan_year=$1 AND ($2='' OR $2=ANY(p.departments)) AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.period_id=p.id AND e.action='cancelled') ORDER BY x.event_time DESC`, year, department)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DashboardExport, error) {
		var x DashboardExport
		err := r.Scan(&x.PeriodID, &x.At)
		return x, err
	})
}
