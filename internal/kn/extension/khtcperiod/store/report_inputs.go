package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// ReportInputPeriods filters the actual weekly/current source set before the
// defensive bound. Unrelated year records cannot displace submitted weeks.
// A current program period is kept even when its quantity cutoff precedes To,
// matching the existing capture rule; no other future period is included.
func ReportInputPeriods(ctx context.Context, tx pgx.Tx, year int, current uuid.UUID, end time.Time, currentProgram bool) ([]Period, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period p
 WHERE p.plan_year=$1 AND (p.report_type='week' OR p.id=$2)
 AND (p.period_to<=$3::date OR ($4 AND p.id=$2))
 AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id=p.tenant_id AND e.period_id=p.id AND e.action='cancelled')
 ORDER BY p.id LIMIT 1001`, year, current, end.Format(time.DateOnly), currentProgram)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
	if err == nil && len(out) > 1000 {
		return nil, errors.New("quá 1000 kỳ nguồn báo cáo trong một năm")
	}
	return out, err
}
