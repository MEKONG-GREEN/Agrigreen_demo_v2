// Package store persists append-only KHTC reports using the extension role and tenant RLS.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type Draft struct {
	ID        uuid.UUID
	PeriodID  uuid.UUID
	Version   int64
	Action    string
	Content   domain.Draft
	InputHash string
	ActorRef  uuid.UUID
	At        time.Time
}
type Export struct {
	ID          uuid.UUID
	PeriodID    uuid.UUID
	Version     int64
	DraftID     uuid.UUID
	Content     domain.Draft
	InputHash   string
	Filter      domain.Filter
	FileName    string
	Body        []byte
	BodyHash    string
	Provisional bool
	ActorRef    uuid.UUID
	At          time.Time
}
type Final struct {
	ID       uuid.UUID
	PeriodID uuid.UUID
	ExportID uuid.UUID
	ActorRef uuid.UUID
	At       time.Time
}

func Lock(ctx context.Context, tx pgx.Tx, tid, period uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || $1::uuid::text || ':' || $2::uuid::text, 0))`, tid, period)
	return err
}

const draftCols = `id, period_id, seq, action, payload, input_hash, actor_ref, event_time`

func scanDraft(row pgx.Row) (*Draft, error) {
	var d Draft
	var raw []byte
	err := row.Scan(&d.ID, &d.PeriodID, &d.Version, &d.Action, &raw, &d.InputHash, &d.ActorRef, &d.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &d.Content)
	return &d, err
}
func LatestDraft(ctx context.Context, tx pgx.Tx, period uuid.UUID) (*Draft, error) {
	return scanDraft(tx.QueryRow(ctx, `SELECT `+draftCols+` FROM pf_extension.khtc_report_draft WHERE period_id=$1 ORDER BY seq DESC LIMIT 1`, period))
}

// PreviousProgramDraft uses only this module's RLS-protected ledgers, including prior years.
func PreviousProgramDraft(ctx context.Context, tx pgx.Tx, before time.Time) (*Draft, error) {
	return scanDraft(tx.QueryRow(ctx, `SELECT `+draftCols+` FROM pf_extension.khtc_report_draft
 WHERE period_id=(SELECT p.id FROM pf_extension.khtc_period p
 WHERE p.report_type='program' AND p.period_to < $1::date
 AND EXISTS (SELECT 1 FROM pf_extension.khtc_report_draft d WHERE d.period_id=p.id)
 AND NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.period_id=p.id AND e.action='cancelled')
 ORDER BY p.period_to DESC, p.id DESC LIMIT 1) ORDER BY seq DESC LIMIT 1`, before.Format(time.DateOnly)))
}
func DraftByIdem(ctx context.Context, tx pgx.Tx, key string) (*Draft, error) {
	return scanDraft(tx.QueryRow(ctx, `SELECT `+draftCols+` FROM pf_extension.khtc_report_draft WHERE idem_key=$1`, key))
}
func InsertDraft(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d *Draft, idem string) error {
	raw, err := json.Marshal(d.Content)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_report_draft
  (id,tenant_id,stream_key,period_id,action,payload,input_hash,actor_ref,event_time,idem_key,rule_version)
  VALUES($1,$2,'khtc_report_draft:'||$3::uuid::text,$3,$4,$5::jsonb,$6,$7,$8,$9,$10) RETURNING seq`,
		d.ID, tid, d.PeriodID, d.Action, string(raw), d.InputHash, d.ActorRef, d.At, idem, domain.RuleVersion).Scan(&d.Version)
}

const exportCols = `id, period_id, seq, draft_id, payload, input_hash, export_filter, file_name, body, body_hash, provisional, actor_ref, event_time`

func scanExport(row pgx.Row) (*Export, error) {
	var x Export
	var payload, filter []byte
	err := row.Scan(&x.ID, &x.PeriodID, &x.Version, &x.DraftID, &payload, &x.InputHash, &filter, &x.FileName, &x.Body, &x.BodyHash, &x.Provisional, &x.ActorRef, &x.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(payload, &x.Content); err != nil {
		return nil, err
	}
	err = json.Unmarshal(filter, &x.Filter)
	return &x, err
}
func ExportByID(ctx context.Context, tx pgx.Tx, period, id uuid.UUID) (*Export, error) {
	return scanExport(tx.QueryRow(ctx, `SELECT `+exportCols+` FROM pf_extension.khtc_report_export WHERE period_id=$1 AND id=$2`, period, id))
}
func ExportByIdem(ctx context.Context, tx pgx.Tx, key string) (*Export, error) {
	return scanExport(tx.QueryRow(ctx, `SELECT `+exportCols+` FROM pf_extension.khtc_report_export WHERE idem_key=$1`, key))
}
func LatestExport(ctx context.Context, tx pgx.Tx, period uuid.UUID) (*Export, error) {
	return scanExport(tx.QueryRow(ctx, `SELECT `+exportCols+` FROM pf_extension.khtc_report_export WHERE period_id=$1 ORDER BY seq DESC LIMIT 1`, period))
}

// ExportHeaders omits immutable payloads and file bytes from the history listing.
func ExportHeaders(ctx context.Context, tx pgx.Tx, period uuid.UUID, limit int) ([]Export, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := tx.Query(ctx, `SELECT id,period_id,seq,draft_id,input_hash,export_filter,file_name,body_hash,provisional,actor_ref,event_time
  FROM pf_extension.khtc_report_export WHERE period_id=$1 ORDER BY seq DESC LIMIT $2`, period, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Export, error) {
		var x Export
		var filter []byte
		err := row.Scan(&x.ID, &x.PeriodID, &x.Version, &x.DraftID, &x.InputHash, &filter, &x.FileName, &x.BodyHash, &x.Provisional, &x.ActorRef, &x.At)
		if err != nil {
			return x, err
		}
		err = json.Unmarshal(filter, &x.Filter)
		return x, err
	})
}

func InsertExport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x *Export, idem string) error {
	raw, err := json.Marshal(x.Content)
	if err != nil {
		return err
	}
	filter, err := json.Marshal(x.Filter)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_report_export
  (id,tenant_id,stream_key,period_id,draft_id,payload,input_hash,export_filter,file_name,body,body_hash,provisional,actor_ref,event_time,idem_key,rule_version)
  VALUES($1,$2,'khtc_report_export:'||$3::uuid::text,$3,$4,$5::jsonb,$6,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING seq`,
		x.ID, tid, x.PeriodID, x.DraftID, string(raw), x.InputHash, string(filter), x.FileName, x.Body, x.BodyHash, x.Provisional, x.ActorRef, x.At, idem, domain.RuleVersion).Scan(&x.Version)
}
func GetFinal(ctx context.Context, tx pgx.Tx, period uuid.UUID) (*Final, error) {
	var f Final
	err := tx.QueryRow(ctx, `SELECT id,period_id,export_id,actor_ref,event_time FROM pf_extension.khtc_report_final WHERE period_id=$1 ORDER BY seq DESC LIMIT 1`, period).Scan(&f.ID, &f.PeriodID, &f.ExportID, &f.ActorRef, &f.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &f, err
}
func InsertFinal(ctx context.Context, tx pgx.Tx, tid uuid.UUID, f Final, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_report_final
  (id,tenant_id,stream_key,period_id,export_id,actor_ref,event_time,idem_key,rule_version)
  VALUES($1,$2,'khtc_report_final:'||$3::uuid::text,$3,$4,$5,$6,$7,$8)`, f.ID, tid, f.PeriodID, f.ExportID, f.ActorRef, f.At, idem, domain.RuleVersion)
	return err
}
