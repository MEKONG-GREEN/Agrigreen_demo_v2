package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PeriodOperationKey is permanent even if receipt history is unavailable.
// It never stores or infers an operation outcome.
type PeriodOperationKey struct {
	OperationID, PeriodID, ActorRef, ReceiptID uuid.UUID
	Profile, Action, Digest, Authority         string
}

func LockPeriodOperation(ctx context.Context, tx pgx.Tx, tid, operation uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_forms.operation:'||$1::uuid::text||':'||$2::uuid::text,0))`, tid, operation)
	return err
}

func PeriodOperationByKey(ctx context.Context, tx pgx.Tx, tid, operation uuid.UUID) (*PeriodOperationKey, error) {
	k := PeriodOperationKey{OperationID: operation}
	err := tx.QueryRow(ctx, `SELECT period_id,profile,action,request_digest,authority_hash,actor_ref,receipt_id FROM pf_forms.period_operation_key WHERE tenant_id=$1 AND operation_id=$2`, tid, operation).Scan(&k.PeriodID, &k.Profile, &k.Action, &k.Digest, &k.Authority, &k.ActorRef, &k.ReceiptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &k, err
}

func PeriodOperationReceiptByID(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID) (*port.PeriodOperationReceipt, error) {
	var raw []byte
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT content,recorded_at FROM pf_forms.period_operation_receipt WHERE tenant_id=$1 AND id=$2 ORDER BY seq LIMIT 1`, tid, id).Scan(&raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt port.PeriodOperationReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	receipt.RecordedAt = at.UTC()
	return &receipt, nil
}

// InsertPeriodOperation records identity and outcome in the caller's native TX.
// No INSERT may be retried after a key exists, including after ledger-idem TTL.
func InsertPeriodOperation(ctx context.Context, tx pgx.Tx, tid uuid.UUID, receipt *port.PeriodOperationReceipt, at time.Time) error {
	id := ids.New()
	_, err := tx.Exec(ctx, `INSERT INTO pf_forms.period_operation_key(tenant_id,operation_id,period_id,profile,action,request_digest,authority_hash,actor_ref,receipt_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tid, receipt.OperationID, receipt.PeriodID, receipt.Profile, receipt.Action, receipt.RequestDigest, receipt.AuthorityHash, receipt.ActorRef, id)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO pf_forms.period_operation_receipt(id,tenant_id,stream_key,operation_id,profile,period_id,action,request_digest,authority_hash,outcome,content,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,'period_operation:'||$3::uuid::text,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,'forms.period_operation/v1') RETURNING recorded_at`, id, tid, receipt.OperationID, receipt.Profile, receipt.PeriodID, receipt.Action, receipt.RequestDigest, receipt.AuthorityHash, receipt.Outcome, string(raw), at.UTC(), receipt.ActorRef, fmt.Sprintf("period_operation:%s", receipt.OperationID)).Scan(&receipt.RecordedAt)
	receipt.RecordedAt = receipt.RecordedAt.UTC()
	return err
}
