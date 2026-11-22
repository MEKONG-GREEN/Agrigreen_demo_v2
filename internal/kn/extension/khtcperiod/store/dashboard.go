package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SourcesForReader reads attached sources in one tenant-scoped transaction.
func SourcesForReader(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, includeAdhoc bool) ([]Source, error) {
	rows, err := tx.Query(ctx, `SELECT s.id, s.period_id, s.department, s.source, s.calendar_id, s.calendar_version, s.native_key, s.label
 FROM pf_extension.khtc_period_source s JOIN pf_extension.khtc_period p ON p.id=s.period_id AND p.tenant_id=s.tenant_id
 WHERE s.period_id=ANY($1::uuid[]) AND ($2 OR p.report_type<>'adhoc') ORDER BY s.period_id, s.seq`, uuidStrings(ids), includeAdhoc)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Source, error) {
		var s Source
		err := r.Scan(&s.ID, &s.PeriodID, &s.Department, &s.Source, &s.CalendarID, &s.CalendarVersion, &s.NativeKey, &s.Label)
		return s, err
	})
}
