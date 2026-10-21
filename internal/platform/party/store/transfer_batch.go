package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TransferBatches(ctx context.Context, tx pgx.Tx, plan uuid.UUID) ([]port.TransferBatchRequest, error) {
	rows, err := tx.Query(ctx, `SELECT document FROM pf_party.transfer_batch WHERE plan_id=$1 ORDER BY id`, plan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.TransferBatchRequest{}
	for rows.Next() {
		var raw []byte
		var b port.TransferBatchRequest
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, port.ErrTransferConflict
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func TransferBatchByID(ctx context.Context, tx pgx.Tx, plan, batch uuid.UUID) (*port.TransferBatchRequest, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT document FROM pf_party.transfer_batch WHERE plan_id=$1 AND id=$2`, plan, batch).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b port.TransferBatchRequest
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, port.ErrTransferConflict
	}
	return &b, nil
}

// TransferMappingOf reuses the original target identity across plan attempts.
// Caller holds the local plan lock and later inserts its batch association atomically.
func TransferMappingOf(ctx context.Context, tx pgx.Tx, s port.TransferScope, key port.TransferKey) (*port.TransferMapping, error) {
	var target, scope uuid.UUID
	var binding port.TransferDigest
	err := tx.QueryRow(ctx, `SELECT target_ref,scope_ref,binding_hash FROM pf_party.transfer_idmap WHERE source_tenant=$1 AND source_scope_ref=$2 AND namespace=$3 AND source_ref=$4`, s.SourceTenant, s.SourceScopeRef, key.Namespace, key.Ref).Scan(&target, &scope, &binding)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if scope != s.TargetScopeRef || binding != s.BindingHash {
		return nil, port.ErrTransferConflict
	}
	return &port.TransferMapping{Key: key, TargetRef: target}, nil
}
func ReserveTransferMapping(ctx context.Context, tx pgx.Tx, s port.TransferScope, key port.TransferKey) (port.TransferMapping, error) {
	old, err := TransferMappingOf(ctx, tx, s, key)
	if err != nil {
		return port.TransferMapping{}, err
	}
	if old != nil {
		return *old, nil
	}
	mapping := port.TransferMapping{Key: key, TargetRef: ids.New()}
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_idmap(tenant_id,scope_ref,created_plan_id,source_tenant,source_scope_ref,namespace,source_ref,target_ref,binding_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.TargetTenant, s.TargetScopeRef, s.PlanID, s.SourceTenant, s.SourceScopeRef, key.Namespace, key.Ref, mapping.TargetRef, s.BindingHash)
	return mapping, transferConstraint(err)
}
func InsertTransferBatch(ctx context.Context, tx pgx.Tx, actor uuid.UUID, b port.TransferBatchRequest, now time.Time) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	s := b.Scope
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_batch(tenant_id,scope_ref,plan_id,id,namespace,manifest_hash,map_hash,document,actor_ref,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, s.TargetTenant, s.TargetScopeRef, s.PlanID, b.BatchID, b.Namespace, b.ManifestHash, b.MapHash, string(raw), actor, now)
	if err != nil {
		return transferConstraint(err)
	}
	for _, h := range b.Headers {
		_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_batch_item(tenant_id,scope_ref,plan_id,batch_id,namespace,source_ref)VALUES($1,$2,$3,$4,$5,$6)`, s.TargetTenant, s.TargetScopeRef, s.PlanID, b.BatchID, h.Key.Namespace, h.Key.Ref)
		if err != nil {
			return transferConstraint(err)
		}
	}
	return nil
}

type TransferProof struct {
	Ref      uuid.UUID
	Hash     port.TransferDigest
	Document json.RawMessage
}

func TransferProofOf(ctx context.Context, tx pgx.Tx, plan, batch uuid.UUID, operation, namespace string) (*TransferProof, error) {
	var out TransferProof
	err := tx.QueryRow(ctx, `SELECT owner_receipt_ref,document_hash,document FROM pf_party.transfer_receipt WHERE plan_id=$1 AND batch_id=$2 AND operation=$3 AND namespace=$4`, plan, batch, operation, namespace).Scan(&out.Ref, &out.Hash, &out.Document)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
func InsertTransferProof(ctx context.Context, tx pgx.Tx, s port.TransferScope, actor, batch uuid.UUID, operation, namespace string, ref uuid.UUID, hash port.TransferDigest, document any, now time.Time) error {
	old, err := TransferProofOf(ctx, tx, s.PlanID, batch, operation, namespace)
	if err != nil {
		return err
	}
	if old != nil {
		if old.Ref != ref || old.Hash != hash {
			return port.ErrTransferConflict
		}
		return nil
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_receipt(tenant_id,scope_ref,plan_id,batch_id,operation,namespace,owner_receipt_ref,document_hash,document,actor_ref,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, s.TargetTenant, s.TargetScopeRef, s.PlanID, batch, operation, namespace, ref, hash, string(raw), actor, now)
	return transferConstraint(err)
}
