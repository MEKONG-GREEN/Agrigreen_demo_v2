package app

import (
	"encoding/json"
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// recoveryPair also accepts a target IntentID. An orphan target fence must remain
// recoverable after normal approval revocation and before normal Propose ever ran.
func (t *Transfers) recoveryPair(p port.TransferParticipants, ref port.TransferRef, action string) (*store.TransferState, *store.TransferState, []port.TransferAdapter, error) {
	if err := t.authenticate(p); err != nil {
		return nil, nil, nil, err
	}
	if ref.ID == uuid.Nil {
		return nil, nil, nil, port.ErrTransferConflict
	}
	source, err := t.read(p.Source(), ref.ID)
	if err == nil {
		if source.Plan.ID != ref.ID {
			return nil, nil, nil, port.ErrTransferConflict
		}
		return t.pair(p, ref, action, true)
	}
	if !errors.Is(err, port.ErrTransferNotReady) {
		return nil, nil, nil, err
	}
	target, err := t.read(p.Target(), ref.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	if target.Intent.ID != ref.ID {
		return nil, nil, nil, port.ErrTransferConflict
	}
	adapters, err := t.adapters(target.Intent.Adapters)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, _, err := t.authority(p, target.Intent.Scope, adapters, nil, nil, action, true); err != nil {
		return nil, nil, nil, err
	}
	var planID uuid.UUID
	err = t.local(p.Source(), func(tx pgx.Tx) error {
		if err := store.LockTransfer(p.Source(), tx, transferTenant(p.Source()), target.Intent.ID); err != nil {
			return err
		}
		var err error
		planID, err = store.TransferPlanByIntent(p.Source(), tx, target.Intent.ID)
		if err != nil {
			return err
		}
		if planID != uuid.Nil {
			return nil
		}
		if target.Plan.ID != uuid.Nil {
			return port.ErrTransferConflict
		}
		state, err := transferClone(*target)
		if err != nil {
			return err
		}
		scope := target.Intent.Scope
		scope.PlanID = ids.New()
		planID = scope.PlanID
		state.Plan = port.TransferPlan{ID: planID, Scope: scope, Status: "recovery_only", Adapters: target.Intent.Adapters}
		state.Sequence = 0
		if err := store.InsertTransferPlan(p.Source(), tx, scope.SourceTenant, transferActor(p.Source()), port.TransferSource, state.Plan, t.now()); err != nil {
			return err
		}
		return store.AppendTransferState(p.Source(), tx, scope.SourceTenant, scope.SourceScopeRef, transferActor(p.Source()), planID, 0, "recovery_associated", &state, t.now())
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return t.pair(p, port.TransferRef{ID: planID}, action, true)
}
func (t *Transfers) gateOperation(p port.TransferParticipants, source, target *store.TransferState, firstReason uuid.UUID) (port.TransferRecoveryRequest, error) {
	var request port.TransferRecoveryRequest
	err := t.local(p.Target(), func(tx pgx.Tx) error {
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
		op, err := store.TransferOperationOf(p.Target(), tx, target.Intent.ID, "recovery_gate", "plan")
		if err != nil {
			return err
		}
		if op == nil {
			input := transferRecoveryInput{Scope: source.Plan.Scope, ManifestHash: source.Plan.ManifestHash, ReasonRef: firstReason}
			hash, err := transferDigest(input)
			if err != nil {
				return err
			}
			created, err := store.ReserveTransferOperation(p.Target(), tx, source.Plan.Scope.TargetTenant, source.Plan.Scope.TargetScopeRef, target.Intent.ID, "recovery_gate", "plan", hash, input)
			if err != nil {
				return err
			}
			op = &created
		}
		var input transferRecoveryInput
		if err := json.Unmarshal(op.Document, &input); err != nil {
			return port.ErrTransferConflict
		}
		hash, err := transferDigest(input)
		if err != nil || hash != op.Hash || input.Scope != source.Plan.Scope || input.ManifestHash != source.Plan.ManifestHash || input.BatchID != uuid.Nil || input.ReasonRef == uuid.Nil {
			return port.ErrTransferConflict
		}
		request = port.TransferRecoveryRequest{Scope: source.Plan.Scope, OperationID: op.ID, ReasonRef: input.ReasonRef}
		if target.RecoveryGateRequest != nil {
			if *target.RecoveryGateRequest != request {
				return port.ErrTransferConflict
			}
			witness, ok := target.RecoveryOperations["recovery_gate:plan"]
			if !ok || witness.ID != op.ID || witness.Hash != op.Hash {
				return port.ErrTransferConflict
			}
			return nil
		}
		if target.RecoveryOperations == nil {
			target.RecoveryOperations = map[string]store.TransferOperationWitness{}
		}
		target.RecoveryOperations["recovery_gate:plan"] = store.TransferOperationWitness{ID: op.ID, Hash: op.Hash}
		target.RecoveryGateRequest = &request
		return store.AppendTransferState(p.Target(), tx, source.Plan.Scope.TargetTenant, source.Plan.Scope.TargetScopeRef, transferActor(p.Target()), target.Intent.ID, target.Sequence, "recovery_gate_reserved", target, t.now())
	})
	return request, err
}
func validateTransferGate(g port.TransferRecoveryGate, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, f port.TransferFence) error {
	if f != (port.TransferFence{}) && !validTransferFence(f) {
		return port.ErrTransferConflict
	}
	if g.Ref == uuid.Nil || g.OperationID != recovery.OperationID || g.ReasonRef != recovery.ReasonRef || g.IntentID != req.Scope.IntentID || g.PlanID != req.Scope.PlanID || g.BindingHash != req.Scope.BindingHash || g.Side != req.Side || g.Namespace != req.Namespace || g.Fence != f {
		return port.ErrTransferConflict
	}
	return nil
}
func (t *Transfers) ensureRecoveryGates(p port.TransferParticipants, source, target *store.TransferState, adapters []port.TransferAdapter, reason uuid.UUID) error {
	recovery, err := t.gateOperation(p, source, target, reason)
	if err != nil {
		return err
	}
	if target.RecoveryGates == nil {
		target.RecoveryGates = map[string]port.TransferRecoveryGate{}
	}
	for _, side := range []port.TransferSide{port.TransferSource, port.TransferTarget} {
		ctx, scope, fences := p.Source(), source.Plan.Scope, source.SourceFences
		if side == port.TransferTarget {
			ctx, scope, fences = p.Target(), target.Intent.Scope, target.Intent.TargetFences
		}
		for _, a := range adapters {
			fence, present := fences[a.Namespace]
			req := port.TransferFenceRequest{Scope: scope, Namespace: a.Namespace, Side: side}
			actual, readErr := a.ReadFenceForRecovery(ctx, recovery, req)
			if errors.Is(readErr, port.ErrTransferNotReady) {
				if present {
					return port.ErrTransferConflict
				}
				fence = port.TransferFence{}
			} else if readErr != nil {
				return transferOwnerError(readErr)
			} else {
				if !validTransferFence(actual) {
					return port.ErrTransferConflict
				}
				if present && actual != fence {
					return port.ErrTransferConflict
				}
				if !present {
					// Persist a discovered owner-committed fence in the correct local tenant.
					fence = actual
					if side == port.TransferSource {
						if source.SourceFences == nil {
							source.SourceFences = map[string]port.TransferFence{}
						}
						source.SourceFences[a.Namespace] = actual
						fences = source.SourceFences
						if err := t.write(p.Source(), source.Plan.ID, source.Plan.Scope.SourceScopeRef, "source_fence_recovered", source); err != nil {
							return err
						}
					} else {
						if target.Intent.TargetFences == nil {
							target.Intent.TargetFences = map[string]port.TransferFence{}
						}
						target.Intent.TargetFences[a.Namespace] = actual
						fences = target.Intent.TargetFences
						if err := t.write(p.Target(), target.Intent.ID, target.Intent.Scope.TargetScopeRef, "target_fence_recovered", target); err != nil {
							return err
						}
					}
				}
			}
			operation := "gate_" + string(side)
			key := a.Namespace + ":" + string(side)
			old, err := t.proof(p, source.Plan.ID, uuid.Nil, operation, a.Namespace)
			if err != nil {
				return err
			}
			if _, recorded := target.RecoveryGates[key]; recorded && old == nil {
				// The map and witness commit together; retaining only the map is
				// missing evidence, never permission to enter the owner gate again.
				return port.ErrTransferConflict
			}
			if old != nil {
				var gate port.TransferRecoveryGate
				if err := json.Unmarshal(old.Document, &gate); err != nil {
					return port.ErrTransferConflict
				}
				hash, err := transferDigest(gate)
				if err != nil || hash != old.Hash || gate.Ref != old.Ref {
					return port.ErrTransferConflict
				}
				if err := validateTransferGate(gate, recovery, req, fence); err != nil {
					return err
				}
				recorded, ok := target.RecoveryGates[key]
				if !ok || recorded != gate {
					return port.ErrTransferConflict
				}
			}
			gate, err := a.ReadRecoveryGate(ctx, recovery, req, fence)
			if errors.Is(err, port.ErrTransferNotReady) {
				if old != nil {
					return port.ErrTransferNotReady
				}
				gate, err = a.EnterRecoveryGate(ctx, recovery, req, fence)
				if err != nil {
					return transferOwnerError(err)
				}
				if err := validateTransferGate(gate, recovery, req, fence); err != nil {
					return err
				}
				durable, e := a.ReadRecoveryGate(ctx, recovery, req, fence)
				if e != nil {
					return transferOwnerError(e)
				}
				if durable != gate {
					return port.ErrTransferConflict
				}
			} else if err != nil {
				return transferOwnerError(err)
			}
			if err := validateTransferGate(gate, recovery, req, fence); err != nil {
				return err
			}
			if old != nil {
				hash, err := transferDigest(gate)
				if err != nil || hash != old.Hash || gate.Ref != old.Ref {
					return port.ErrTransferConflict
				}
				continue
			}
			target.RecoveryGates[key] = gate
			metadata := port.TransferBatchRequest{Scope: source.Plan.Scope, Namespace: a.Namespace}
			if err := t.checkpoint(p, target, metadata, operation, gate.Ref, gate); err != nil {
				return err
			}
		}
	}
	return nil
}
