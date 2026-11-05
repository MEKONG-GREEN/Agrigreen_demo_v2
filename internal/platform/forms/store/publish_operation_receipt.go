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

type PublishOperationKey224 struct {
	ActorRef, ReceiptID        uuid.UUID
	Profile, Digest, Authority string
	Command                    []byte
	ReceiptAt                  time.Time
}

func LockPublishOperation224(ctx context.Context, tx pgx.Tx, tid, operation uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_forms.publish_operation:'||$1::uuid::text||':'||$2::uuid::text,0))`, tid, operation)
	return err
}
func PublishOperationByKey224(ctx context.Context, tx pgx.Tx, tid, operation uuid.UUID) (*PublishOperationKey224, error) {
	var key PublishOperationKey224
	err := tx.QueryRow(ctx, `SELECT actor_ref,profile,request_digest,authority_hash,request_content,receipt_id,receipt_recorded_at FROM pf_forms.publish_operation_key WHERE tenant_id=$1 AND operation_id=$2`, tid, operation).Scan(&key.ActorRef, &key.Profile, &key.Digest, &key.Authority, &key.Command, &key.ReceiptID, &key.ReceiptAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &key, err
}
func PublishOperationReceipt224(ctx context.Context, tx pgx.Tx, request port.PublishOperationRequest, key PublishOperationKey224) (*port.PublishOperationReceipt, bool, error) {
	var raw []byte
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT r.content,r.recorded_at FROM pf_forms.publish_operation_receipt r
 JOIN pf_forms.publish_operation_key k ON k.tenant_id=r.tenant_id AND k.operation_id=r.operation_id
 AND k.receipt_id=r.id AND k.receipt_recorded_at=r.recorded_at AND k.actor_ref=r.actor_ref AND k.profile=r.profile
 AND k.request_digest=r.request_digest AND k.authority_hash=r.authority_hash AND k.request_content=r.request_content
 WHERE r.tenant_id=$1 AND r.id=$2 AND r.recorded_at=$3`, request.TenantID, key.ReceiptID, key.ReceiptAt).Scan(&raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var receipt port.PublishOperationReceipt
	if json.Unmarshal(raw, &receipt) != nil {
		return nil, false, nil
	}
	// Validate the exact returned bytes against the original command and native
	// tuples, not a boolean computed over a different/partially decoded manifest.
	var valid bool
	err = tx.QueryRow(ctx, `SELECT pf_forms.publish_operation_proof224($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,false)`, request.TenantID, request.OperationID, key.ActorRef, key.Profile, key.Digest, key.Authority, string(key.Command), string(raw)).Scan(&valid)
	if err != nil {
		return nil, false, err
	}
	receipt.RecordedAt = at.UTC()
	return &receipt, valid, nil
}

// Receipt/header/native data share the caller's native TX. Header stores the
// exact partitioned receipt identity after INSERT RETURNING, without mutable ACK.
func InsertPublishOperation224(ctx context.Context, tx pgx.Tx, request port.PublishOperationRequest, receipt *port.PublishOperationReceipt, at time.Time) error {
	command, err := json.Marshal(request)
	if err != nil {
		return err
	}
	content, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	id := ids.New()
	err = tx.QueryRow(ctx, `INSERT INTO pf_forms.publish_operation_receipt(id,tenant_id,stream_key,operation_id,profile,request_digest,authority_hash,request_content,content,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,'publish_operation:'||$3::uuid::text,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,'publish_operation:'||$3::uuid::text,'forms.publish_operation/v1') RETURNING recorded_at`, id, request.TenantID, request.OperationID, request.Input.Profile, receipt.RequestDigest, receipt.AuthorityHash, string(command), string(content), at.UTC(), receipt.ActorRef).Scan(&receipt.RecordedAt)
	if err != nil {
		return err
	}
	receipt.RecordedAt = receipt.RecordedAt.UTC()
	_, err = tx.Exec(ctx, `INSERT INTO pf_forms.publish_operation_key(tenant_id,operation_id,profile,request_digest,authority_hash,request_content,actor_ref,receipt_id,receipt_recorded_at)
 VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9)`, request.TenantID, request.OperationID, request.Input.Profile, receipt.RequestDigest, receipt.AuthorityHash, string(command), receipt.ActorRef, id, receipt.RecordedAt)
	return err
}
