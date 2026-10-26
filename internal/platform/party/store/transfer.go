package store

// Generic transfer metadata only. Owner rows/receipts stay in their own modules.
// Callers pass a transaction opened with the actual local participant context.
import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TransferScanState struct {
	Cursor   string
	Complete bool
}

// TransferState is an immutable ledger snapshot, never an authorization cache.
// Sequence is DB-derived and excluded from the hashed document to avoid recursion.
type TransferOperationWitness struct {
	ID   uuid.UUID
	Hash port.TransferDigest
}

type TransferReleaseState struct {
	OperationID, ReasonRef uuid.UUID
	Count                  int
	Root                   port.TransferDigest
	RequiresIdentity       bool
	Proof                  *port.TransferRecoveryProof
}

type TransferState struct {
	Release                 *TransferReleaseState                `json:"release,omitempty"`
	SourceRecoveryStopped   bool                                 `json:"source_recovery_stopped"`
	SourceRecoveryReasonRef uuid.UUID                            `json:"source_recovery_reason_ref"`
	RecoveryGateRequest     *port.TransferRecoveryRequest        `json:"recovery_gate_request,omitempty"`
	RecoveryGates           map[string]port.TransferRecoveryGate `json:"recovery_gates"`
	RecoveryOperations      map[string]TransferOperationWitness  `json:"recovery_operations"`
	VerifiedRoot            port.TransferDigest                  `json:"verified_root"`
	CutoverProof            *port.TransferCutoverProof           `json:"cutover_proof,omitempty"`
	Intent                  port.TransferIntent                  `json:"intent"`
	Plan                    port.TransferPlan                    `json:"plan"`
	SourceApprovals         map[string]port.TransferApproval     `json:"source_approvals"`
	TargetApprovals         map[string]port.TransferApproval     `json:"target_approvals"`
	SourceFences            map[string]port.TransferFence        `json:"source_fences"`
	Scans                   map[string]TransferScanState         `json:"scans"`
	Sequence                int64                                `json:"-"`
}

func LockTransfer(ctx context.Context, tx pgx.Tx, tid, entity uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "pf_party.transfer:"+tid.String()+":"+entity.String())
	return err
}
func TransferLatest(ctx context.Context, tx pgx.Tx, entity uuid.UUID) (*TransferState, error) {
	var raw []byte
	var seq int64
	err := tx.QueryRow(ctx, `SELECT document, seq FROM pf_party.transfer_event WHERE entity_id=$1 ORDER BY seq DESC LIMIT 1`, entity).Scan(&raw, &seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s TransferState
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, port.ErrTransferConflict
	}
	s.Sequence = seq
	return &s, nil
}
func TransferIntentByOperation(ctx context.Context, tx pgx.Tx, operation uuid.UUID) (uuid.UUID, port.TransferDigest, error) {
	var id uuid.UUID
	var hash port.TransferDigest
	err := tx.QueryRow(ctx, `SELECT id,input_hash FROM pf_party.transfer_intent WHERE operation_id=$1`, operation).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", nil
	}
	return id, hash, err
}
func TransferPlanByIntent(ctx context.Context, tx pgx.Tx, intent uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM pf_party.transfer_plan WHERE intent_id=$1`, intent).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}
func InsertTransferIntent(ctx context.Context, tx pgx.Tx, tid, actor, operation uuid.UUID, hash port.TransferDigest, intent port.TransferIntent, now time.Time) error {
	if err := insertTransferEntity(ctx, tx, tid, intent.Scope.TargetScopeRef, intent.ID, "intent"); err != nil {
		return err
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_intent(tenant_id,scope_ref,id,operation_id,input_hash,document,actor_ref,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tid, intent.Scope.TargetScopeRef, intent.ID, operation, hash, string(raw), actor, now)
	return transferConstraint(err)
}
func InsertTransferPlan(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, side port.TransferSide, plan port.TransferPlan, now time.Time) error {
	scopeRef := plan.Scope.SourceScopeRef
	if side == port.TransferTarget {
		scopeRef = plan.Scope.TargetScopeRef
	}
	if err := insertTransferEntity(ctx, tx, tid, scopeRef, plan.ID, "plan"); err != nil {
		return err
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_plan(tenant_id,scope_ref,id,intent_id,side,document,actor_ref,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tid, scopeRef, plan.ID, plan.Scope.IntentID, side, string(raw), actor, now)
	return transferConstraint(err)
}

// AppendTransferState checks the expected ledger sequence under the local lock.
// No mutable projection is used as authority; the new snapshot and event are one row.
func AppendTransferState(ctx context.Context, tx pgx.Tx, tid, scopeRef, actor, entity uuid.UUID, expected int64, kind string, s *TransferState, now time.Time) error {
	if err := LockTransfer(ctx, tx, tid, entity); err != nil {
		return err
	}
	prev, err := TransferLatest(ctx, tx, entity)
	if err != nil {
		return err
	}
	actual := int64(0)
	if prev != nil {
		actual = prev.Sequence
	}
	if actual != expected {
		return port.ErrTransferConflict
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	eventID := ids.New()
	return tx.QueryRow(ctx, `INSERT INTO pf_party.transfer_event(id,tenant_id,scope_ref,stream_key,entity_id,kind,document,event_time,actor_ref,idem_key,rule_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING seq`, eventID, tid, scopeRef, "transfer:"+entity.String(), entity, kind, string(raw), now, actor, "transfer:"+eventID.String(), "party.transfer/v1").Scan(&s.Sequence)
}
func TransferHeaders(ctx context.Context, tx pgx.Tx, plan uuid.UUID) ([]port.TransferHeader, error) {
	rows, err := tx.Query(ctx, `SELECT document FROM pf_party.transfer_item WHERE plan_id=$1 ORDER BY namespace COLLATE "C",source_ref`, plan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.TransferHeader{}
	for rows.Next() {
		var raw []byte
		var h port.TransferHeader
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, port.ErrTransferConflict
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
func InsertTransferHeaders(ctx context.Context, tx pgx.Tx, tid, scopeRef, plan uuid.UUID, headers []port.TransferHeader) error {
	for _, h := range headers {
		raw, err := json.Marshal(h)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_item(tenant_id,scope_ref,plan_id,namespace,source_ref,document)VALUES($1,$2,$3,$4,$5,$6)`, tid, scopeRef, plan, h.Key.Namespace, h.Key.Ref, string(raw))
		if err != nil {
			return transferConstraint(err)
		}
	}
	return nil
}

// InsertTransferBinding fixes a permanent Centre/tenant pair, not a single attempt.
// A later plan for exactly this pair reuses it; owner fences/compensation must first
// release the old holder. Global uniqueness still rejects any different pair under RLS.
func InsertTransferBinding(ctx context.Context, tx pgx.Tx, actor uuid.UUID, plan port.TransferPlan, now time.Time) error {
	s := plan.Scope
	var oldSource, oldScope, oldTargetScope uuid.UUID
	var oldHash port.TransferDigest
	err := tx.QueryRow(ctx, `SELECT source_tenant,source_scope_ref,target_scope_ref,binding_hash FROM pf_party.transfer_binding WHERE target_tenant=$1`, s.TargetTenant).Scan(&oldSource, &oldScope, &oldTargetScope, &oldHash)
	if err == nil {
		if oldSource != s.SourceTenant || oldScope != s.SourceScopeRef || oldTargetScope != s.TargetScopeRef || oldHash != s.BindingHash {
			return port.ErrTransferConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_binding(tenant_id,scope_ref,first_plan_id,source_tenant,source_scope_ref,target_tenant,target_scope_ref,binding_hash,actor_ref,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, s.TargetTenant, s.TargetScopeRef, plan.ID, s.SourceTenant, s.SourceScopeRef, s.TargetTenant, s.TargetScopeRef, s.BindingHash, actor, now)
	return transferConstraint(err)
}

// TransferOperation is a permanent reservation, not an expiring ledger-idem entry.
type TransferOperation struct {
	ID       uuid.UUID
	Hash     port.TransferDigest
	Document json.RawMessage
}

func ReserveTransferOperation(ctx context.Context, tx pgx.Tx, tid, scopeRef, entity uuid.UUID, kind, key string, hash port.TransferDigest, document any) (TransferOperation, error) {
	var old TransferOperation
	err := tx.QueryRow(ctx, `SELECT operation_id,input_hash,document FROM pf_party.transfer_operation WHERE entity_id=$1 AND kind=$2 AND semantic_key=$3`, entity, kind, key).Scan(&old.ID, &old.Hash, &old.Document)
	if err == nil {
		if old.Hash != hash {
			return TransferOperation{}, port.ErrTransferConflict
		}
		return old, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TransferOperation{}, err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return TransferOperation{}, err
	}
	out := TransferOperation{ID: ids.New(), Hash: hash, Document: raw}
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.transfer_operation(tenant_id,scope_ref,entity_id,kind,semantic_key,operation_id,input_hash,document)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tid, scopeRef, entity, kind, key, out.ID, hash, string(raw))
	return out, transferConstraint(err)
}
func transferConstraint(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23503" || pg.Code == "23514") {
		return port.ErrTransferConflict
	}
	return err
}

func insertTransferEntity(ctx context.Context, tx pgx.Tx, tid, scopeRef, id uuid.UUID, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.transfer_entity(tenant_id,scope_ref,id,kind)VALUES($1,$2,$3,$4)`, tid, scopeRef, id, kind)
	return transferConstraint(err)
}
