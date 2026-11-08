package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReviewOperationKey is a permanent identity anchor without an inferred outcome.
type ReviewOperationKey struct {
	Actor, ReceiptID  uuid.UUID
	Digest, Authority string
}

func LockReviewOperation(ctx context.Context, tx pgx.Tx, tenant, operation uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_forms.review_operation:'||$1::uuid::text||':'||$2::uuid::text,0))`, tenant, operation)
	return err
}

func ReviewOperationByKey(ctx context.Context, tx pgx.Tx, tenant, operation uuid.UUID) (*ReviewOperationKey, error) {
	var k ReviewOperationKey
	err := tx.QueryRow(ctx, `SELECT actor_ref,receipt_id,request_digest,authority_hash FROM pf_forms.review_operation_key WHERE tenant_id=$1 AND operation_id=$2`, tenant, operation).Scan(&k.Actor, &k.ReceiptID, &k.Digest, &k.Authority)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &k, err
}

func ReviewOperationReceiptByID(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (*port.ReviewOperationReceipt, error) {
	var raw []byte
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT content,recorded_at FROM pf_forms.review_operation_receipt WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r port.ReviewOperationReceipt
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	r.RecordedAt = at.UTC()
	return &r, nil
}

// ReviewResult reads only the exact native identity just inserted by this TX.
func ReviewResult(ctx context.Context, tx pgx.Tx, id uuid.UUID) (port.ReviewOperationResult, error) {
	var r port.ReviewOperationResult
	err := tx.QueryRow(ctx, `SELECT id,seq,reviewer_ref,event_time,recorded_at,workflow_request_id FROM pf_forms.review WHERE id=$1`, id).Scan(&r.ReviewID, &r.ReviewSeq, &r.Reviewer, &r.ReviewedAt, &r.ReviewRecordedAt, &r.WorkflowRequestID)
	r.ReviewedAt = r.ReviewedAt.UTC()
	r.ReviewRecordedAt = r.ReviewRecordedAt.UTC()
	return r, err
}

// InsertReviewOperation writes the receipt/audit and its permanent anchor in
// the same native review/workflow transaction. There is no TTL replay window.
func InsertReviewOperation(ctx context.Context, tx pgx.Tx, receipt *port.ReviewOperationReceipt, at time.Time) error {
	id := ids.New()
	_, err := tx.Exec(ctx, `INSERT INTO pf_forms.review_operation_key(tenant_id,operation_id,actor_ref,request_digest,authority_hash,receipt_id) VALUES($1,$2,$3,$4,$5,$6)`, receipt.TenantID, receipt.OperationID, receipt.ActorRef, receipt.RequestDigest, receipt.AuthorityHash, id)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.review_operation_receipt(id,tenant_id,stream_key,operation_id,profile,period_id,unit_code,submission_id,expected_version,decision,request_digest,authority_hash,outcome,content,event_time,actor_ref,idem_key,rule_version)
VALUES($1,$2,'review_operation:'||$3::uuid::text,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15,'review_operation:'||$3::uuid::text,'forms.review_operation/v1') RETURNING recorded_at`, id, receipt.TenantID, receipt.OperationID, receipt.Profile, receipt.PeriodID, receipt.Unit, receipt.SubmissionID, receipt.ExpectedVersion, receipt.Decision, receipt.RequestDigest, receipt.AuthorityHash, receipt.Outcome, string(raw), at.UTC(), receipt.ActorRef).Scan(&receipt.RecordedAt)
}
