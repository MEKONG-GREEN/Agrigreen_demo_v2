package store

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type DraftEvent225 struct {
	Meta
	Year                                    int
	DraftID                                 uuid.UUID
	Action                                  string
	ExpectedRevision                        int64
	EntriesCount                            int
	EntriesHash, RequestHash, AuthorityHash []byte
	Note                                    string
}

const draftColumns225 = `id,seq,plan_year,draft_id,action,expected_revision,entries_count,entries_hash,request_hash,authority_hash,note,event_time,recorded_at,actor_ref,on_behalf_of,support_reason,idem_key`

func scanDraft225(row pgx.Row) (*DraftEvent225, error) {
	var e DraftEvent225
	err := row.Scan(&e.ID, &e.Seq, &e.Year, &e.DraftID, &e.Action, &e.ExpectedRevision, &e.EntriesCount, &e.EntriesHash, &e.RequestHash, &e.AuthorityHash, &e.Note, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.OnBehalfOf, &e.SupportReason, &e.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}
func CurrentDraft225(ctx context.Context, tx pgx.Tx, year int) (*DraftEvent225, error) {
	return scanDraft225(tx.QueryRow(ctx, `SELECT `+draftColumns225+` FROM pf_extension.khtc_plan_draft225_event WHERE plan_year=$1 ORDER BY seq DESC LIMIT 1`, year))
}
func DraftByIdem225(ctx context.Context, tx pgx.Tx, key string) (*DraftEvent225, error) {
	return scanDraft225(tx.QueryRow(ctx, `SELECT `+draftColumns225+` FROM pf_extension.khtc_plan_draft225_event WHERE idem_key=$1 ORDER BY seq DESC LIMIT 1`, key))
}
func HasPublished225(ctx context.Context, tx pgx.Tx, year int) (bool, error) {
	var yes bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_extension.khtc_plan_version WHERE plan_year=$1)`, year).Scan(&yes)
	return yes, err
}
func DraftHistory225(ctx context.Context, tx pgx.Tx, year int, after int64, limit int) ([]DraftEvent225, error) {
	rows, err := tx.Query(ctx, `SELECT `+draftColumns225+` FROM pf_extension.khtc_plan_draft225_event WHERE plan_year=$1 AND ($2::bigint=0 OR seq<$2) ORDER BY seq DESC LIMIT $3`, year, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DraftEvent225{}
	for rows.Next() {
		e, err := scanDraft225(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}
func DraftEntries225(ctx context.Context, tx pgx.Tx, e DraftEvent225) ([]domain.Entry, error) {
	return readPlanEntryRows225(ctx, tx, `SELECT entry_ref, tab, kind, parent_ref, program_ref, sort_order, coalesce(code, ''), name,
	    coalesce(unit_code, ''), planned_qty::text, coalesce(scale_note, ''), budget::text, coalesce(funding_code, ''),
	    coalesce(field_code, ''), coalesce(department_code, ''), communes, points_count, coalesce(data_source, ''),
	    coalesce(source_key, ''), tracking, stopped_from, coalesce(stop_decision, ''), coalesce(note, '')
	  FROM pf_extension.khtc_plan_draft225_entry WHERE draft_event_id = $1::uuid AND draft_recorded_at=$2::timestamptz ORDER BY position`, e.ID, e.RecordedAt)
}

// Deferred FK + header-count guard bind a complete immutable tree to this event.
func InsertDraft225(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *DraftEvent225, es []domain.Entry) error {
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&e.RecordedAt); err != nil {
		return err
	}
	b := &pgx.Batch{}
	for i, x := range es {
		var stopped any
		if x.StoppedFrom != nil {
			stopped = x.StoppedFrom.Format(time.DateOnly)
		}
		communes := x.Communes
		if communes == nil {
			communes = []string{}
		}
		b.Queue(`INSERT INTO pf_extension.khtc_plan_draft225_entry (tenant_id, draft_event_id, plan_year, entry_ref, position, tab, kind,
		    parent_ref, program_ref, sort_order, code, name, unit_code, planned_qty, scale_note, budget, funding_code, field_code,
		    department_code, communes, points_count, data_source, source_key, tracking, stopped_from, stop_decision, note, draft_recorded_at)
		  VALUES ($1::uuid, $2::uuid, $3::smallint, $4::uuid, $5::int, $6::text, $7::text, $8::uuid, $9::uuid, $10::int, $11::text,
		    $12::text, $13::text, $14::numeric, $15::text, $16::numeric, $17::text, $18::text, $19::text, $20::text[], $21::int,
		    $22::text, $23::text, $24::text, $25::date, $26::text, $27::text, $28::timestamptz)`, tid, e.ID, e.Year, x.Ref, i, x.Tab, x.Kind, x.ParentRef, x.ProgramRef, x.SortOrder, nz(x.Code), x.Name, nz(x.UnitCode), decArg(x.PlannedQty), nz(x.ScaleNote), decArg(x.Budget), nz(x.FundingCode), nz(x.FieldCode), nz(x.Department), communes, x.PointsCount, nz(x.DataSource), nz(x.SourceKey), x.Tracking, stopped, nz(x.StopDecision), nz(x.Note), e.RecordedAt)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_plan_draft225_event
 (id,tenant_id,stream_key,plan_year,draft_id,action,expected_revision,base_version,entries_count,entries_hash,request_hash,authority_hash,note,event_time,recorded_at,actor_ref,on_behalf_of,support_reason,idem_key,rule_version)
 VALUES($1,$2,$3,$4,$5,$6,$7,0,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,'kn.khtc.draft225/v1') RETURNING seq,recorded_at`, e.ID, tid, "plan-draft:"+strconv.Itoa(e.Year), e.Year, e.DraftID, e.Action, e.ExpectedRevision, e.EntriesCount, e.EntriesHash, e.RequestHash, e.AuthorityHash, e.Note, e.EventTime, e.RecordedAt, e.ActorRef, e.OnBehalfOf, e.SupportReason, e.IdemKey).Scan(&e.Seq, &e.RecordedAt)
}
