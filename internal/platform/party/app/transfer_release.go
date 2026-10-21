package app

import (
	"encoding/json"
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/google/uuid"
)

func (t *Transfers) recoveryInventory(p port.TransferParticipants, source, target *store.TransferState, recovery port.TransferRecoveryRequest) (port.TransferReport, error) {
	batches, err := t.batches(p, source.Plan.ID)
	if err != nil {
		return port.TransferReport{}, err
	}
	result := []port.TransferHeader{}
	seen := map[port.TransferKey]bool{}
	for _, b := range batches {
		_, _, a, req, err := t.batch(p, port.TransferBatchRef{PlanID: source.Plan.ID, BatchID: b.BatchID}, "recovery_inventory", true)
		if err != nil {
			return port.TransferReport{}, err
		}
		apply, err := t.recoveryApplyRequest(p, a, req)
		if err != nil {
			return port.TransferReport{}, err
		}
		applied, err := t.readAppliedForRecovery(p, a, recovery, apply)
		if errors.Is(err, port.ErrTransferNotReady) {
			continue
		}
		if err != nil {
			return port.TransferReport{}, err
		}
		compensated, err := t.compensationWitness(p, source, target, a, req, applied)
		if err != nil {
			return port.TransferReport{}, err
		}
		for _, item := range compensated.Items {
			if seen[item.Key] {
				return port.TransferReport{}, port.ErrTransferConflict
			}
			seen[item.Key] = true
			result = append(result, port.TransferHeader{Key: item.Key, LogicalHash: item.LogicalHash})
		}
	}
	root, err := port.TransferLogicalRoot(result)
	return port.TransferReport{PlanID: source.Plan.ID, Count: len(result), Root: root}, err
}
func sameTransferRecoveryProof(a, b port.TransferRecoveryProof) bool {
	return a.PlanID == b.PlanID && a.OperationID == b.OperationID && a.ReasonRef == b.ReasonRef && a.IdentityRollbackReceipt == b.IdentityRollbackReceipt && a.RehearsalReceipt == b.RehearsalReceipt && a.BindingHash == b.BindingHash && a.ManifestHash == b.ManifestHash && a.CompensationRoot == b.CompensationRoot && a.ValidUntil.Equal(b.ValidUntil)
}
func (t *Transfers) recoveryReleaseProof(p port.TransferParticipants, source *store.TransferState, recovery port.TransferRecoveryRequest, report port.TransferReport) (port.TransferRecoveryProof, error) {
	if t.registry.Recovery() == nil {
		return port.TransferRecoveryProof{}, port.ErrTransferNotReady
	}
	proof, err := t.registry.Recovery()(p, recovery, report)
	if err != nil {
		return port.TransferRecoveryProof{}, transferOwnerError(err)
	}
	proof.ValidUntil = proof.ValidUntil.UTC().Round(0)
	if proof.PlanID != source.Plan.ID || proof.OperationID != recovery.OperationID || proof.ReasonRef != recovery.ReasonRef || proof.IdentityRollbackReceipt == uuid.Nil || proof.RehearsalReceipt == uuid.Nil || proof.BindingHash != source.Plan.Scope.BindingHash || proof.ManifestHash != source.Plan.ManifestHash || proof.CompensationRoot != report.Root || !proof.ValidUntil.After(t.now()) {
		return port.TransferRecoveryProof{}, port.ErrTransferNotReady
	}
	return proof, nil
}

// refreshReleaseProof uses only the immutable approved inventory. It runs again
// at each pending owner write, because a previous side's release may outlast the
// current identity/rehearsal proof. Historical receipt reads need no new proof.
func (t *Transfers) refreshReleaseProof(p port.TransferParticipants, source, target *store.TransferState, recovery port.TransferRecoveryRequest) error {
	if target.Release == nil {
		return port.ErrTransferConflict
	}
	if !target.Release.RequiresIdentity {
		return nil
	}
	report := port.TransferReport{PlanID: source.Plan.ID, Count: target.Release.Count, Root: target.Release.Root}
	proof, err := t.recoveryReleaseProof(p, source, recovery, report)
	if err != nil {
		return err
	}
	if target.Release.Proof == nil || !sameTransferRecoveryProof(*target.Release.Proof, proof) {
		target.Release.Proof = &proof
		return t.write(p.Target(), target.Intent.ID, target.Plan.Scope.TargetScopeRef, "release_proof_refreshed", target)
	}
	return nil
}

// Abort records a terminal forward state only after actual owner receipts prove
// no apply, or every applied item has a verified durable compensation. Fences stay
// closed until the separate release operation has all required owner proofs.
func (t *Transfers) Abort(p port.TransferParticipants, ref port.TransferRef, reason uuid.UUID) (port.TransferPlan, error) {
	if reason == uuid.Nil {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	source, target, adapters, err := t.recoveryPair(p, ref, "abort")
	if err != nil {
		return port.TransferPlan{}, err
	}
	if target.Plan.Status == "released" {
		return target.Plan, nil
	}
	recovery, err := t.beginRecovery(p, source, target, "abort", "plan", uuid.Nil, reason)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if target.Plan.Status == "aborted" {
		return target.Plan, nil
	}
	if err := t.ensureRecoveryGates(p, source, target, adapters, reason); err != nil {
		return port.TransferPlan{}, err
	}
	if _, err := t.recoveryInventory(p, source, target, recovery); err != nil {
		return port.TransferPlan{}, err
	}
	target.Plan.Status = "aborted"
	return target.Plan, t.write(p.Target(), target.Intent.ID, target.Plan.Scope.TargetScopeRef, "aborted", target)
}
func validateTransferRelease(receipt port.TransferFenceRelease, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, fence port.TransferFence) error {
	if receipt.Ref == uuid.Nil || receipt.OperationID != recovery.OperationID || receipt.ReasonRef != recovery.ReasonRef || receipt.IntentID != req.Scope.IntentID || receipt.PlanID != req.Scope.PlanID || receipt.BindingHash != req.Scope.BindingHash || receipt.Side != req.Side || receipt.Namespace != req.Namespace || receipt.Fence != fence {
		return port.ErrTransferConflict
	}
	return nil
}
func (t *Transfers) releaseOne(p port.TransferParticipants, source, target *store.TransferState, a port.TransferAdapter, side port.TransferSide, recovery port.TransferRecoveryRequest) error {
	ctx, scope := p.Source(), source.Plan.Scope
	read, release := a.ReadSourceFenceRelease, a.ReleaseSourceFence
	if side == port.TransferTarget {
		ctx, scope = p.Target(), target.Intent.Scope
		read, release = a.ReadTargetFenceRelease, a.ReleaseTargetFence
	}
	req := port.TransferFenceRequest{Scope: scope, Namespace: a.Namespace, Side: side}
	key := a.Namespace + ":" + string(side)
	gate, ok := target.RecoveryGates[key]
	if !ok || target.RecoveryGateRequest == nil {
		return port.ErrTransferNotReady
	}
	if err := validateTransferGate(gate, *target.RecoveryGateRequest, req, gate.Fence); err != nil {
		return err
	}
	// Gate maps and proofs checkpoint together. Re-read immutable core history
	// even after release_approved/released without checking a newer owner fence.
	gateProof, err := t.proof(p, source.Plan.ID, uuid.Nil, "gate_"+string(side), a.Namespace)
	if err != nil {
		return err
	}
	if gateProof == nil {
		return port.ErrTransferConflict
	}
	var historicalGate port.TransferRecoveryGate
	if err := json.Unmarshal(gateProof.Document, &historicalGate); err != nil {
		return port.ErrTransferConflict
	}
	gateHash, err := transferDigest(historicalGate)
	if err != nil || gateHash != gateProof.Hash || historicalGate.Ref != gateProof.Ref || historicalGate != gate {
		return port.ErrTransferConflict
	}
	fence := gate.Fence // Zero is an authoritative absent-fence tombstone, not a missing check.
	operation := "release_" + string(side)
	old, err := t.proof(p, source.Plan.ID, uuid.Nil, operation, a.Namespace)
	if err != nil {
		return err
	}
	if target.Plan.Status == "released" && old == nil {
		// Terminal state can only be replayed from its committed witnesses.
		// Missing retained evidence must never reopen the release writer.
		return port.ErrTransferConflict
	}
	if old != nil {
		var receipt port.TransferFenceRelease
		if err := json.Unmarshal(old.Document, &receipt); err != nil {
			return port.ErrTransferConflict
		}
		hash, err := transferDigest(receipt)
		if err != nil || hash != old.Hash || receipt.Ref != old.Ref {
			return port.ErrTransferConflict
		}
		if err := validateTransferRelease(receipt, recovery, req, fence); err != nil {
			return err
		}
	}
	// A committed release may already have opened this old holder. Read its
	// durable outcome BEFORE any active-fence check; never touch a new holder.
	receipt, err := read(ctx, recovery, req, fence)
	if errors.Is(err, port.ErrTransferNotReady) {
		if old != nil {
			return port.ErrTransferNotReady
		}
		if err := t.refreshReleaseProof(p, source, target, recovery); err != nil {
			return err
		}
		if err := t.authenticate(p); err != nil {
			return err
		}
		receipt, err = release(ctx, recovery, req, fence)
		if err != nil {
			return transferOwnerError(err)
		}
		if err := validateTransferRelease(receipt, recovery, req, fence); err != nil {
			return err
		}
		durable, e := read(ctx, recovery, req, fence)
		if e != nil {
			return transferOwnerError(e)
		}
		if durable != receipt {
			return port.ErrTransferConflict
		}
	} else if err != nil {
		return transferOwnerError(err)
	}
	if err := validateTransferRelease(receipt, recovery, req, fence); err != nil {
		return err
	}
	if old != nil {
		hash, err := transferDigest(receipt)
		if err != nil || hash != old.Hash || receipt.Ref != old.Ref {
			return port.ErrTransferConflict
		}
		return nil
	}
	return t.checkpoint(p, target, port.TransferBatchRequest{Scope: source.Plan.Scope, Namespace: a.Namespace}, operation, receipt.Ref, receipt)
}
func (t *Transfers) ReleaseFences(p port.TransferParticipants, ref port.TransferRef, reason uuid.UUID) (port.TransferPlan, error) {
	if reason == uuid.Nil {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	source, target, adapters, err := t.recoveryPair(p, ref, "release")
	if err != nil {
		return port.TransferPlan{}, err
	}
	// Completed release is historical state under fresh dual recovery authority;
	// it may never reacquire/check/release a later holder's active fence.
	if target.Plan.Status == "released" {
		if target.Release == nil || target.Release.ReasonRef != reason {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		recovery := port.TransferRecoveryRequest{Scope: source.Plan.Scope, OperationID: target.Release.OperationID, ReasonRef: reason}
		for _, side := range []port.TransferSide{port.TransferTarget, port.TransferSource} {
			for i := len(adapters) - 1; i >= 0; i-- {
				if err := t.releaseOne(p, source, target, adapters[i], side, recovery); err != nil {
					return port.TransferPlan{}, err
				}
			}
		}
		return target.Plan, nil
	}
	recovery, err := t.beginRecovery(p, source, target, "release", "plan", uuid.Nil, reason)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if target.Release == nil {
		if err := t.ensureRecoveryGates(p, source, target, adapters, reason); err != nil {
			return port.TransferPlan{}, err
		}
		report, err := t.recoveryInventory(p, source, target, recovery)
		if err != nil {
			return port.TransferPlan{}, err
		}
		state := &store.TransferReleaseState{OperationID: recovery.OperationID, ReasonRef: reason, Count: report.Count, Root: report.Root, RequiresIdentity: report.Count > 0 || target.CutoverProof != nil}
		if state.RequiresIdentity {
			proof, err := t.recoveryReleaseProof(p, source, recovery, report)
			if err != nil {
				return port.TransferPlan{}, err
			}
			state.Proof = &proof
		}
		target.Release = state
		if err := t.write(p.Target(), target.Intent.ID, target.Plan.Scope.TargetScopeRef, "release_approved", target); err != nil {
			return port.TransferPlan{}, err
		}
	} else {
		if target.Release.OperationID != recovery.OperationID || target.Release.ReasonRef != reason || !port.ValidTransferDigest(target.Release.Root) {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		// Once releases start, old active fences/rows may legitimately have changed.
		// Revalidate identity recovery against the immutable approved inventory,
		// then replay exact durable release receipts rather than reopening gates.
		if err := t.refreshReleaseProof(p, source, target, recovery); err != nil {
			return port.TransferPlan{}, err
		}
	}
	// Keep the source closed until every target holder has a durable release.
	for _, side := range []port.TransferSide{port.TransferTarget, port.TransferSource} {
		for i := len(adapters) - 1; i >= 0; i-- {
			if err := t.releaseOne(p, source, target, adapters[i], side, recovery); err != nil {
				return port.TransferPlan{}, err
			}
		}
	}
	if err := t.authenticate(p); err != nil {
		return port.TransferPlan{}, err
	}
	target.Plan.Status = "released"
	return target.Plan, t.write(p.Target(), target.Intent.ID, target.Plan.Scope.TargetScopeRef, "released", target)
}
