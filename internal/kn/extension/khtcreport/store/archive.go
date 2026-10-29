package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// ArchiveRow contains immutable metadata, never draft payloads or file bytes.
type ArchiveRow struct {
	ID          uuid.UUID `json:"id"`
	PeriodID    uuid.UUID `json:"period_id"`
	PeriodName  string    `json:"period_name"`
	ReportType  string    `json:"report_type"`
	DocumentNo  string    `json:"document_no"`
	Version     int64     `json:"version"`
	Final       bool      `json:"final"`
	Provisional bool      `json:"provisional"`
	ActorRef    uuid.UUID `json:"actor_ref"`
	At          time.Time `json:"at"`
}

func Archive(ctx context.Context, tx pgx.Tx, year int, kind, search string, versions bool, offset, limit int) ([]ArchiveRow, error) {
	rows, err := tx.Query(ctx, `SELECT x.id,x.period_id,p.name,p.report_type,
 COALESCE(x.payload->'admin'->'document_no'->>'override',x.payload->'admin'->'document_no'->>'system',''),
 x.seq,EXISTS(SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.period_id=p.id AND f.export_id=x.id),x.provisional,x.actor_ref,x.event_time
 FROM pf_extension.khtc_report_export x JOIN pf_extension.khtc_period p ON p.id=x.period_id AND p.tenant_id=x.tenant_id
 WHERE p.plan_year=$1 AND ($2='' OR p.report_type=$2)
 AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.period_id=p.id AND e.action='cancelled')
 AND ($4 OR x.id=COALESCE((SELECT f.export_id FROM pf_extension.khtc_report_final f WHERE f.period_id=p.id ORDER BY f.seq DESC LIMIT 1),
 (SELECT e.id FROM pf_extension.khtc_report_export e WHERE e.period_id=p.id ORDER BY e.seq DESC LIMIT 1)))
 AND ($3='' OR strpos(lower(p.name || ' ' || COALESCE(x.payload->'admin'->'document_no'->>'override',x.payload->'admin'->'document_no'->>'system','')),lower($3))>0)
 ORDER BY x.event_time DESC,x.id DESC OFFSET $5 LIMIT $6`, year, kind, search, versions, offset, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ArchiveRow, error) {
		var x ArchiveRow
		err := r.Scan(&x.ID, &x.PeriodID, &x.PeriodName, &x.ReportType, &x.DocumentNo, &x.Version, &x.Final, &x.Provisional, &x.ActorRef, &x.At)
		return x, err
	})
}
