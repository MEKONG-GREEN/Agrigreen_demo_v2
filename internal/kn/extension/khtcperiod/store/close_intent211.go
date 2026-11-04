package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type CloseIntent211 struct {
	ID, OperationID, PeriodID, EventID, Actor     uuid.UUID
	RequestKey, RequestHash, AuthorityHash, Phase string
	Payload, Outcome                              json.RawMessage
	At                                            time.Time
}

const closeColumns211 = `id,operation_id,period_id,close_event_id,actor_ref,request_key,request_hash,authority_hash,phase,payload::text,outcome::text,event_time`

func scanClose211(row pgx.Row) (*CloseIntent211, error) {
	var v CloseIntent211
	var payload string
	var outcome *string
	err := row.Scan(&v.ID, &v.OperationID, &v.PeriodID, &v.EventID, &v.Actor, &v.RequestKey, &v.RequestHash, &v.AuthorityHash, &v.Phase, &payload, &outcome, &v.At)
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
func CloseByKey211(ctx context.Context, tx pgx.Tx, key string) (*CloseIntent211, error) {
	return scanClose211(tx.QueryRow(ctx, `SELECT `+closeColumns211+` FROM pf_extension.khtc_close_intent211 WHERE request_key=$1 ORDER BY seq DESC LIMIT 1`, key))
}
func CloseByOperation211(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*CloseIntent211, error) {
	return scanClose211(tx.QueryRow(ctx, `SELECT `+closeColumns211+` FROM pf_extension.khtc_close_intent211 WHERE operation_id=$1 ORDER BY seq DESC LIMIT 1`, id))
}
func LockClose211(ctx context.Context, tx pgx.Tx, tid, period uuid.UUID, key string) error {
	return LockDeadline(ctx, tx, tid, period, "close211:"+key)
}
func InsertClose211(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v CloseIntent211) error {
	var outcome any
	if len(v.Outcome) > 0 {
		outcome = string(v.Outcome)
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_close_intent211(id,tenant_id,stream_key,operation_id,period_id,close_event_id,actor_ref,request_key,request_hash,authority_hash,phase,payload,outcome,event_time,idem_key,rule_version)
 VALUES($1,$2,'khtc_close211:'||$3::uuid::text,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13,'khtc_close211:'||$1::uuid::text,'khtc.close-receipt/v1')`, v.ID, tid, v.OperationID, v.PeriodID, v.EventID, v.Actor, v.RequestKey, v.RequestHash, v.AuthorityHash, v.Phase, string(v.Payload), outcome, v.At)
	return err
}
