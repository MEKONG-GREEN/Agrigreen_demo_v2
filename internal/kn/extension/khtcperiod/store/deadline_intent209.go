package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// DeadlineIntent records one immutable operation and its append-only phases.
// Payload is owned data: base snapshot, original/applied public revisions only.
type DeadlineIntent struct {
	ID, OperationID, PeriodID, Actor              uuid.UUID
	RequestKey, RequestHash, AuthorityHash, Phase string
	Payload, Outcome                              json.RawMessage
	At                                            time.Time
}

const deadlineColumns = `id,operation_id,period_id,actor_ref,request_key,request_hash,authority_hash,phase,payload::text,outcome::text,event_time`

func scanDeadline(r pgx.Row) (*DeadlineIntent, error) {
	var v DeadlineIntent
	var payload string
	var outcome *string
	err := r.Scan(&v.ID, &v.OperationID, &v.PeriodID, &v.Actor, &v.RequestKey, &v.RequestHash, &v.AuthorityHash, &v.Phase, &payload, &outcome, &v.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.Payload = json.RawMessage(payload)
	if outcome != nil {
		v.Outcome = json.RawMessage(*outcome)
	}
	return &v, nil
}
func DeadlineByKey(ctx context.Context, tx pgx.Tx, key string) (*DeadlineIntent, error) {
	return scanDeadline(tx.QueryRow(ctx, `SELECT `+deadlineColumns+` FROM pf_extension.khtc_deadline_intent WHERE request_key=$1 ORDER BY seq DESC LIMIT 1`, key))
}
func DeadlineByOperation(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*DeadlineIntent, error) {
	return scanDeadline(tx.QueryRow(ctx, `SELECT `+deadlineColumns+` FROM pf_extension.khtc_deadline_intent WHERE operation_id=$1 ORDER BY seq DESC LIMIT 1`, id))
}
func LockDeadline(ctx context.Context, tx pgx.Tx, tid, period uuid.UUID, key string) error {
	// Key first, resource second, consistently in application and SQL trigger.
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_deadline_key:'||$1::uuid::text||':'||$2,0)),pg_advisory_xact_lock(hashtextextended('khtc_deadline_period:'||$1::uuid::text||':'||$3::uuid::text,0))`, tid, key, period)
	return err
}
func PendingDeadline(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var pending bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM(SELECT DISTINCT ON(operation_id)phase FROM pf_extension.khtc_deadline_intent WHERE period_id=$1 ORDER BY operation_id,seq DESC)x WHERE phase NOT IN('completed','undone')) OR EXISTS(SELECT 1 FROM(SELECT DISTINCT ON(operation_id)phase FROM pf_extension.khtc_close_intent211 WHERE period_id=$1 ORDER BY operation_id,seq DESC)y WHERE phase NOT IN('completed','rejected'))`, id).Scan(&pending)
	return pending, err
}
func InsertDeadline(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v DeadlineIntent) error {
	var outcome any
	if len(v.Outcome) > 0 {
		outcome = string(v.Outcome)
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_deadline_intent(id,tenant_id,stream_key,operation_id,period_id,request_key,request_hash,authority_hash,phase,payload,outcome,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,'khtc_deadline:'||$3::uuid::text,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,'khtc_deadline:'||$3::uuid::text||':'||$8,'khtc-deadline-cas/v1')`, v.ID, tid, v.OperationID, v.PeriodID, v.RequestKey, v.RequestHash, v.AuthorityHash, v.Phase, string(v.Payload), outcome, v.At, v.Actor)
	return err
}
