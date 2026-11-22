package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type AdhocOpenIntent struct {
	ID, PlannedPeriodID, SessionID, ActorRef uuid.UUID
	EffectiveSessionID                       uuid.UUID
	ContextHash                              string
	SessionExpiresAt, EffectiveExpiresAt     time.Time
	LeaseGeneration                          int64
	NativeKey, RequestHash, BatchHash        string
	Input                                    json.RawMessage
	Plan                                     domain.Plan
	Batch                                    forms.BatchInput
	Sources                                  []ext.KHTCSourceRef
	Departments                              []string
	At                                       time.Time
}

const openIntentSelect = `SELECT i.id,i.planned_period_id,i.session_ref,i.actor_ref,i.native_idem_key,i.request_hash,i.batch_hash,i.input::text,i.plan::text,i.batch::text,i.source_refs::text,i.departments,i.event_time,i.context_hash,i.session_expires_at,
 coalesce(l.session_ref,i.session_ref),coalesce(l.expires_at,i.session_expires_at),coalesce(l.generation,0)
 FROM pf_extension.khtc_adhoc_open_intent i
 LEFT JOIN LATERAL(SELECT session_ref,expires_at,generation FROM pf_extension.khtc_adhoc_open_lease
 WHERE tenant_id=i.tenant_id AND intent_id=i.id ORDER BY generation DESC LIMIT 1)l ON true `

func scanOpenIntent(row pgx.Row) (*AdhocOpenIntent, error) {
	var x AdhocOpenIntent
	var input, plan, batch, sources string
	err := row.Scan(&x.ID, &x.PlannedPeriodID, &x.SessionID, &x.ActorRef, &x.NativeKey, &x.RequestHash, &x.BatchHash, &input, &plan, &batch, &sources, &x.Departments, &x.At, &x.ContextHash, &x.SessionExpiresAt, &x.EffectiveSessionID, &x.EffectiveExpiresAt, &x.LeaseGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	x.Input = json.RawMessage(input)
	if err = json.Unmarshal([]byte(plan), &x.Plan); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(batch), &x.Batch); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(sources), &x.Sources); err != nil {
		return nil, err
	}
	return &x, nil
}
func AdhocOpenByKey(ctx context.Context, tx pgx.Tx, key string) (*AdhocOpenIntent, error) {
	return scanOpenIntent(tx.QueryRow(ctx, openIntentSelect+` WHERE i.native_idem_key=$1 ORDER BY i.seq DESC LIMIT 1`, key))
}
func AdhocOpenByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*AdhocOpenIntent, error) {
	return scanOpenIntent(tx.QueryRow(ctx, openIntentSelect+` WHERE i.id=$1`, id))
}
func LockAdhocOpen(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_adhoc_open:'||$1::uuid::text||':'||$2,0))`, tid, key)
	return err
}
func InsertAdhocOpen(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x AdhocOpenIntent) error {
	plan, err := json.Marshal(x.Plan)
	if err != nil {
		return err
	}
	batch, err := json.Marshal(x.Batch)
	if err != nil {
		return err
	}
	sources, err := json.Marshal(x.Sources)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_extension.khtc_adhoc_open_intent
 (id,tenant_id,stream_key,planned_period_id,session_ref,native_idem_key,request_hash,batch_hash,input,plan,batch,source_refs,departments,event_time,actor_ref,idem_key,rule_version,context_hash,session_expires_at)
 VALUES($1,$2,'khtc_adhoc_open:'||$3::uuid::text||':'||$4,$5,$6,$4,$7,$8,$9::jsonb,$10::jsonb,$11::jsonb,$15::jsonb,$12,$13,$3,$14,'khtc-adhoc/v1',$16,$17)`,
		x.ID, tid, x.ActorRef, x.NativeKey, x.PlannedPeriodID, x.SessionID, x.RequestHash, x.BatchHash, string(x.Input), string(plan), string(batch), x.Departments, x.At, "khtc:adhoc:intent:"+x.NativeKey, string(sources), x.ContextHash, x.SessionExpiresAt)
	return err
}
func PeriodByFormsID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Period, error) {
	x, err := scanPeriod(tx.QueryRow(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period WHERE forms_period_ref=$1 ORDER BY recorded_at DESC LIMIT 1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &x, err
}

// IncompleteAdhocOpens scopes actor and context BEFORE LIMIT. Native keys and batch
// bytes remain private and never appear in the HTTP recovery projection.
func IncompleteAdhocOpens(ctx context.Context, tx pgx.Tx, actor uuid.UUID, fingerprint string) ([]AdhocOpenIntent, error) {
	rows, err := tx.Query(ctx, openIntentSelect+` WHERE i.actor_ref=$1 AND i.context_hash=$2
 AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period p WHERE p.tenant_id=i.tenant_id AND p.id=i.planned_period_id)
 ORDER BY i.event_time,i.id LIMIT 100`, actor, fingerprint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdhocOpenIntent{}
	for rows.Next() {
		x, e := scanOpenIntent(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}
func AppendAdhocOpenLease(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x AdhocOpenIntent, session uuid.UUID, expires time.Time, at time.Time, reason string, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_adhoc_open_lease
 (id,tenant_id,stream_key,intent_id,generation,previous_session_ref,session_ref,expires_at,reason,actor_ref,event_time,idem_key,rule_version)
 VALUES($1,$2,'khtc_adhoc_lease:'||$3::uuid::text,$3,$4::bigint,$5,$6,$7,$8,$9,$10,'khtc:adhoc:lease:'||$3::uuid::text||':'||($4::bigint)::text,'khtc-adhoc/v1')`,
		id, tid, x.ID, x.LeaseGeneration+1, x.EffectiveSessionID, session, expires, reason, x.ActorRef, at)
	return err
}
