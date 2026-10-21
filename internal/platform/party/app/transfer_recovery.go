package app

import (
	"encoding/json"
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type transferRecoveryInput struct {
	Scope        port.TransferScope
	ManifestHash port.TransferDigest
	BatchID      uuid.UUID
	ReasonRef    uuid.UUID
}

// beginRecovery commits the permanent operation and forward-write stop BEFORE any
// owner compensation/probe. It also creates a target metadata association for an
// intent aborted before Accept, without reserving a global Centre binding.
func (t *Transfers) beginRecovery(p port.TransferParticipants, source, target *store.TransferState, kind, key string, batch, reason uuid.UUID) (port.TransferRecoveryRequest, error) {
	if reason == uuid.Nil {
		return port.TransferRecoveryRequest{}, port.ErrTransferConflict
	}
	input := transferRecoveryInput{Scope: source.Plan.Scope, ManifestHash: source.Plan.ManifestHash, BatchID: batch, ReasonRef: reason}
	hash, err := transferDigest(input)
	if err != nil {
		return port.TransferRecoveryRequest{}, err
	}
	var op store.TransferOperation
	err = t.local(p.Target(), func(tx pgx.Tx) error {
		if err := store.LockTransfer(p.Target(), tx, source.Plan.Scope.TargetTenant, target.Intent.ID); err != nil {
			return err
		}
		current, err := store.TransferLatest(p.Target(), tx, target.Intent.ID)
		if err != nil {
			return err
		}
		if current == nil || current.Sequence != target.Sequence {
			return port.ErrTransferConflict
		}
		if target.Plan.Status == "released" {
			return port.ErrTransferConflict
		}
		if target.Plan.ID == uuid.Nil {
			target.Plan = source.Plan
			target.Plan.Status = "recovering"
			if err := store.InsertTransferPlan(p.Target(), tx, source.Plan.Scope.TargetTenant, transferActor(p.Target()), port.TransferTarget, target.Plan, t.now()); err != nil {
				return err
			}
		}
		oldOp, err := store.TransferOperationOf(p.Target(), tx, target.Intent.ID, kind, key)
		if err != nil {
			return err
		}
		op, err = store.ReserveTransferOperation(p.Target(), tx, source.Plan.Scope.TargetTenant, source.Plan.Scope.TargetScopeRef, target.Intent.ID, kind, key, hash, input)
		if err != nil {
			return err
		}
		if target.RecoveryOperations == nil {
			target.RecoveryOperations = map[string]store.TransferOperationWitness{}
		}
		witnessKey := kind + ":" + key
		witness, exists := target.RecoveryOperations[witnessKey]
		if exists && (witness.ID != op.ID || witness.Hash != op.Hash) {
			return port.ErrTransferConflict
		}
		needsEvent := oldOp == nil || !exists || current.Plan.ID == uuid.Nil || target.Plan.Status != "recovering" && target.Plan.Status != "aborted"
		target.RecoveryOperations[witnessKey] = store.TransferOperationWitness{ID: op.ID, Hash: op.Hash}
		if target.Plan.Status != "aborted" {
			target.Plan.Status = "recovering"
		}
		if needsEvent {
			return store.AppendTransferState(p.Target(), tx, source.Plan.Scope.TargetTenant, source.Plan.Scope.TargetScopeRef, transferActor(p.Target()), target.Intent.ID, target.Sequence, "recovery_reserved", target, t.now())
		}
		return nil
	})
	return port.TransferRecoveryRequest{Scope: source.Plan.Scope, OperationID: op.ID, ReasonRef: reason}, err
}
func (t *Transfers) recoveryApplyRequest(p port.TransferParticipants, adapter port.TransferAdapter, req port.TransferBatchRequest) (port.TransferApplyRequest, error) {
	proof, err := t.proof(p, req.Scope.PlanID, req.BatchID, "export", req.Namespace)
	if err != nil {
		return port.TransferApplyRequest{}, err
	}
	apply := port.TransferApplyRequest{TransferBatchRequest: req}
	// An unexported reservation must still be probed. The owner must return
	// NotReady only when no apply receipt exists; an unexpected receipt is conflict.
	if proof == nil {
		return apply, nil
	}
	apply.Artifact, err = transferDecodeArtifact(proof, req, adapter.Version)
	return apply, err
}
func (t *Transfers) readAppliedForRecovery(p port.TransferParticipants, a port.TransferAdapter, recovery port.TransferRecoveryRequest, req port.TransferApplyRequest) (port.TransferReceipt, error) {
	expected, err := t.proof(p, req.Scope.PlanID, req.BatchID, "apply", req.Namespace)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if expected != nil {
		old, err := transferDecodeReceipt(expected)
		if err != nil {
			return port.TransferReceipt{}, err
		}
		if err := validateTransferReceipt(req.TransferBatchRequest, old, false); err != nil {
			return port.TransferReceipt{}, err
		}
	}
	copyReq, err := transferClone(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	receipt, err := a.ReadAppliedForRecovery(p.Target(), recovery, copyReq)
	if err != nil {
		if expected != nil && errors.Is(err, port.ErrTransferNotReady) {
			return port.TransferReceipt{}, port.ErrTransferConflict
		}
		return port.TransferReceipt{}, transferOwnerError(err)
	}
	receipt, err = transferCanonicalReceipt(receipt)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if receipt.OwnerVersion != a.Version || receipt.ArtifactHash != req.Artifact.Hash {
		return port.TransferReceipt{}, port.ErrTransferConflict
	}
	if err := validateTransferReceipt(req.TransferBatchRequest, receipt, false); err != nil {
		return port.TransferReceipt{}, err
	}
	proof, err := t.proof(p, req.Scope.PlanID, req.BatchID, "apply", req.Namespace)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if proof != nil {
		hash, err := transferDigest(receipt)
		if err != nil || hash != proof.Hash || receipt.Ref != proof.Ref {
			return port.TransferReceipt{}, port.ErrTransferConflict
		}
	}
	return receipt, nil
}
func (t *Transfers) ownerCompensation(p port.TransferParticipants, a port.TransferAdapter, req port.TransferCompensationRequest) (port.TransferReceipt, bool, error) {
	copyReq, err := transferClone(req)
	if err != nil {
		return port.TransferReceipt{}, false, err
	}
	receipt, err := a.ReadCompensation(p.Target(), copyReq)
	if err != nil {
		return port.TransferReceipt{}, false, transferOwnerError(err)
	}
	receipt, err = transferCanonicalReceipt(receipt)
	if err != nil {
		return port.TransferReceipt{}, true, err
	}
	if receipt.OwnerVersion != a.Version || receipt.ArtifactHash != req.Artifact.Hash || receipt.OperationID != req.OperationID || receipt.ReasonRef != req.ReasonRef || receipt.CompensatesRef != req.Applied.Ref || receipt.SourceLogicalRoot != req.Applied.LogicalRoot {
		return port.TransferReceipt{}, true, port.ErrTransferConflict
	}
	if err := validateTransferReceipt(req.TransferBatchRequest, receipt, true); err != nil {
		return port.TransferReceipt{}, true, err
	}
	resulting := make([]port.TransferHeader, 0, len(receipt.Items))
	for _, item := range receipt.Items {
		resulting = append(resulting, port.TransferHeader{Key: item.Key, LogicalHash: item.LogicalHash})
	}
	root, err := port.TransferLogicalRoot(resulting)
	if err != nil || root != receipt.LogicalRoot {
		return port.TransferReceipt{}, true, port.ErrTransferConflict
	}
	copyReq, err = transferClone(req)
	if err != nil {
		return port.TransferReceipt{}, true, err
	}
	copyReceipt, err := transferClone(receipt)
	if err != nil {
		return port.TransferReceipt{}, true, err
	}
	if err := a.VerifyCompensation(p.Target(), copyReq, copyReceipt); err != nil {
		return port.TransferReceipt{}, true, transferOwnerError(err)
	}
	return receipt, true, nil
}
func (t *Transfers) Compensate(p port.TransferParticipants, ref port.TransferBatchRef, reason uuid.UUID) (port.TransferReceipt, error) {
	source, target, a, batch, err := t.batch(p, ref, "compensate", true)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	recovery, err := t.beginRecovery(p, source, target, "compensate", ref.BatchID.String(), ref.BatchID, reason)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	adapters, err := t.adapters(source.Plan.Adapters)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if err := t.ensureRecoveryGates(p, source, target, adapters, reason); err != nil {
		return port.TransferReceipt{}, err
	}
	if err := t.reverseCompensationDependencies(p, source, target, batch, recovery); err != nil {
		return port.TransferReceipt{}, err
	}
	apply, err := t.recoveryApplyRequest(p, a, batch)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	applied, err := t.readAppliedForRecovery(p, a, recovery, apply)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	req := port.TransferCompensationRequest{TransferApplyRequest: apply, OperationID: recovery.OperationID, ReasonRef: reason, Applied: applied}
	oldProof, err := t.proof(p, ref.PlanID, ref.BatchID, "compensate", batch.Namespace)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if oldProof != nil {
		old, err := transferDecodeReceipt(oldProof)
		if err != nil {
			return port.TransferReceipt{}, err
		}
		if old.OperationID != req.OperationID || old.ReasonRef != req.ReasonRef || old.CompensatesRef != applied.Ref || old.SourceLogicalRoot != applied.LogicalRoot {
			return port.TransferReceipt{}, port.ErrTransferConflict
		}
	}
	receipt, found, err := t.ownerCompensation(p, a, req)
	if !found && errors.Is(err, port.ErrTransferNotReady) {
		if oldProof != nil {
			return port.TransferReceipt{}, port.ErrTransferNotReady
		}
		copyReq, e := transferClone(req)
		if e != nil {
			return port.TransferReceipt{}, e
		}
		returned, e := a.Compensate(p.Target(), copyReq)
		if e != nil {
			return port.TransferReceipt{}, transferOwnerError(e)
		}
		returned, e = transferCanonicalReceipt(returned)
		if e != nil {
			return port.TransferReceipt{}, e
		}
		receipt, _, err = t.ownerCompensation(p, a, req)
		if err != nil {
			return port.TransferReceipt{}, err
		}
		want, e := transferDigest(returned)
		if e != nil {
			return port.TransferReceipt{}, e
		}
		actual, e := transferDigest(receipt)
		if e != nil || actual != want {
			return port.TransferReceipt{}, port.ErrTransferConflict
		}
	}
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if err := t.authenticate(p); err != nil {
		return port.TransferReceipt{}, err
	}
	return receipt, t.checkpoint(p, target, batch, "compensate", receipt.Ref, receipt)
}

// compensationWitness requires a completed core witness AND a current durable
// owner compensation receipt. Pending owner commits are resumed through Compensate,
// never counted as completed by a predecessor or release operation.
func (t *Transfers) compensationWitness(p port.TransferParticipants, source, target *store.TransferState, a port.TransferAdapter, b port.TransferBatchRequest, applied port.TransferReceipt) (port.TransferReceipt, error) {
	var op *store.TransferOperation
	err := t.local(p.Target(), func(tx pgx.Tx) error {
		var err error
		op, err = store.TransferOperationOf(p.Target(), tx, target.Intent.ID, "compensate", b.BatchID.String())
		return err
	})
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if op == nil {
		return port.TransferReceipt{}, port.ErrTransferNotReady
	}
	var input transferRecoveryInput
	if err := json.Unmarshal(op.Document, &input); err != nil {
		return port.TransferReceipt{}, port.ErrTransferConflict
	}
	hash, err := transferDigest(input)
	witness, ok := target.RecoveryOperations["compensate:"+b.BatchID.String()]
	if err != nil || hash != op.Hash || input.Scope != source.Plan.Scope || input.ManifestHash != source.Plan.ManifestHash || input.BatchID != b.BatchID || input.ReasonRef == uuid.Nil || !ok || witness.ID != op.ID || witness.Hash != op.Hash {
		return port.TransferReceipt{}, port.ErrTransferConflict
	}
	core, err := t.proof(p, b.Scope.PlanID, b.BatchID, "compensate", b.Namespace)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if core == nil {
		return port.TransferReceipt{}, port.ErrTransferNotReady
	}
	if _, err := transferDecodeReceipt(core); err != nil {
		return port.TransferReceipt{}, err
	}
	apply, err := t.recoveryApplyRequest(p, a, b)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	req := port.TransferCompensationRequest{TransferApplyRequest: apply, OperationID: op.ID, ReasonRef: input.ReasonRef, Applied: applied}
	receipt, _, err := t.ownerCompensation(p, a, req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	actual, err := transferDigest(receipt)
	if err != nil || actual != core.Hash || receipt.Ref != core.Ref {
		return port.TransferReceipt{}, port.ErrTransferConflict
	}
	return receipt, nil
}
func (t *Transfers) reverseCompensationDependencies(p port.TransferParticipants, source, target *store.TransferState, batch port.TransferBatchRequest, recovery port.TransferRecoveryRequest) error {
	adapters, err := t.adapters(source.Plan.Adapters)
	if err != nil {
		return err
	}
	dependencies := map[string][]string{}
	for _, a := range adapters {
		dependencies[a.Namespace] = a.DependsOn
	}
	var depends func(string, string) bool
	depends = func(child, parent string) bool {
		for _, dep := range dependencies[child] {
			if dep == parent || depends(dep, parent) {
				return true
			}
		}
		return false
	}
	members := map[port.TransferKey]bool{}
	for _, m := range batch.Mappings {
		members[m.Key] = true
	}
	batches, err := t.batches(p, source.Plan.ID)
	if err != nil {
		return err
	}
	for _, candidate := range batches {
		if candidate.BatchID == batch.BatchID {
			continue
		}
		dependent := candidate.Namespace != batch.Namespace && depends(candidate.Namespace, batch.Namespace)
		for _, m := range candidate.DependencyMappings {
			if members[m.Key] {
				dependent = true
			}
		}
		if !dependent {
			continue
		}
		_, _, a, req, err := t.batch(p, port.TransferBatchRef{PlanID: source.Plan.ID, BatchID: candidate.BatchID}, "compensation_dependency", true)
		if err != nil {
			return err
		}
		apply, err := t.recoveryApplyRequest(p, a, req)
		if err != nil {
			return err
		}
		applied, err := t.readAppliedForRecovery(p, a, recovery, apply)
		if errors.Is(err, port.ErrTransferNotReady) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := t.compensationWitness(p, source, target, a, req, applied); err != nil {
			return err
		}
	}
	return nil
}
