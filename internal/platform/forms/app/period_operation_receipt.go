package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReceiptControl is dormant until an owner installs a current-authority policy.
// It calls the same native CAS implementation; it never falls back to legacy writes.
type ReceiptControl struct{ Svc *Service }

var _ port.PeriodOperations = ReceiptControl{}

type periodOperationContext struct{}
type periodOperation struct {
	request                        port.PeriodOperationRequest
	digest, authority              string
	actor                          actor
	profile                        port.Profile
	grant                          bool
	now                            time.Time
	receipt                        port.PeriodOperationReceipt
	effects, durable, acknowledged bool
}

// receiptStop is private and can only finish a proven pre-effect branch.
var receiptStop = errors.New("forms: receipt replay or durable rejection")

func unknownOperation() port.PeriodOperationReceipt {
	return port.PeriodOperationReceipt{Outcome: port.PeriodOperationUnknown}
}

func (c ReceiptControl) prepare(ctx context.Context, in port.PeriodOperationRequest) (*periodOperation, error) {
	if c.Svc == nil || c.Svc.DB == nil {
		return nil, port.ErrPeriodOperationUnavailable
	}
	if _, joined := db.JoinedTx(ctx); joined {
		return nil, port.ErrPeriodOperationUnavailable
	}
	req, err := port.NormalizePeriodOperation(in)
	if err != nil {
		return nil, err
	}
	policy := c.Svc.PeriodOperationPolicies[req.Profile]
	if policy == nil {
		return nil, port.ErrPeriodOperationUnavailable
	}
	principal, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	if !principal.MFA || principal.ActorRef == uuid.Nil || principal.TenantID == uuid.Nil || principal.TenantID != req.Expected.TenantID {
		return nil, problem.Forbidden
	}
	prof, a, err := c.Svc.resolve(ctx, req.Profile)
	if err != nil {
		return nil, err
	}
	if req.Action == port.PeriodOperationReopen && prof.NoReopen {
		return nil, problem.Forbidden
	}
	permission, err := policy(ctx, principal, req)
	if err != nil {
		return nil, err
	}
	if !permission.Allowed || strings.TrimSpace(permission.Binding) == "" || len(permission.Binding) > 512 || strings.ContainsRune(permission.Binding, 0) {
		return nil, problem.Forbidden
	}
	digest, err := port.PeriodOperationDigest(req)
	if err != nil {
		return nil, err
	}
	// Bind trusted facts and the owner's current authority version, not client headers.
	roles := slices.Clone(principal.Roles)
	slices.Sort(roles)
	units := slices.Clone(a.scope.Units)
	slices.Sort(units)
	raw, err := json.Marshal(struct {
		Actor, Tenant, Org, Node         uuid.UUID
		Roles, Units                     []string
		Role, Level, Department, Binding string
		All, MFA                         bool
	}{a.ref, a.tenant, principal.OrgRef, a.node, roles, units, a.role, a.level, a.dept, permission.Binding, a.scope.All, principal.MFA})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	op := &periodOperation{request: req, digest: digest, authority: hex.EncodeToString(sum[:]), actor: a, profile: prof, now: c.Svc.Clock.Now().UTC()}
	if a.level != prof.CloseBy && req.Action != port.PeriodOperationReopen {
		native := port.Operation{Profile: req.Profile, PeriodID: req.PeriodID, IdemKey: operationIdem(req)}
		switch req.Action {
		case port.PeriodOperationChangeDue:
			native.Action = port.OperationChangeDue
			native.PayloadHash, err = port.OperationHash(struct {
				Due    time.Time
				Reason string
			}{req.DueAt, req.Reason})
		case port.PeriodOperationClose:
			native.Action = port.OperationClose
		case port.PeriodOperationCancel:
			native.Action = port.OperationCancel
			native.PayloadHash, err = port.OperationHash(struct{ Reason string }{req.Reason})
		}
		if err != nil {
			return nil, err
		}
		allowed, callErr := operationAllowed(ctx, prof, native)
		if callErr != nil {
			return nil, callErr
		}
		op.grant = allowed.Allowed
		// The owner receipt policy must version delegated operation authority too.
	}
	return op, nil
}
func operationIdem(req port.PeriodOperationRequest) string {
	return "forms.operation:" + req.OperationID.String()
}
func (c ReceiptControl) ExecutePeriodOperation(ctx context.Context, in port.PeriodOperationRequest) (port.PeriodOperationReceipt, error) {
	op, err := c.prepare(ctx, in)
	if err != nil {
		return unknownOperation(), err
	}
	req := op.request
	ctx = context.WithValue(ctx, periodOperationContext{}, op)
	guard := controlCAS{expected: req.Expected, receipt: op}
	control := Control{Svc: c.Svc}
	switch req.Action {
	case port.PeriodOperationChangeDue:
		err = control.changeDue(ctx, req.Profile, req.PeriodID, req.DueAt, req.Reason, operationIdem(req), &guard)
	case port.PeriodOperationClose:
		_, err = c.Svc.closeWithRevision(ctx, req.Profile, req.PeriodID, operationIdem(req), &guard)
	case port.PeriodOperationReopen:
		_, err = c.Svc.reopenWithRevision(ctx, req.Profile, req.PeriodID, ReopenInput{Reason: req.Reason}, operationIdem(req), &guard)
	case port.PeriodOperationCancel:
		_, err = c.Svc.cancelWithRevision(ctx, req.Profile, req.PeriodID, req.Reason, operationIdem(req), &guard)
	}
	if op.acknowledged {
		return op.receipt, nil
	}
	// No commit acknowledgement means unknown, even if a server committed before losing ACK.
	return unknownOperation(), err
}
func (c ReceiptControl) ReadPeriodOperation(ctx context.Context, in port.PeriodOperationRequest) (port.PeriodOperationReceipt, error) {
	op, err := c.prepare(ctx, in)
	if err != nil {
		return unknownOperation(), err
	}
	out := unknownOperation()
	err = c.Svc.tx(ctx, op.actor.tenant, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, op.request.PeriodID); err != nil {
			return err
		}
		v, err := load(ctx, tx, op.request.PeriodID, c.Svc.Clock.Now().UTC())
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != op.request.Profile || !op.actor.canSee(v.Period, v.Spec) {
			return port.ErrPeriodNotFound
		}
		permitted := op.actor.canControlGranted(op.profile.CloseBy, v.Period, v.Spec, op.grant)
		if op.request.Action == port.PeriodOperationReopen {
			permitted = op.actor.canClose(op.profile.CloseBy, v.Period, v.Spec, nil)
		}
		if !permitted {
			return problem.Forbidden
		}
		receipt, _, err := op.lookup(ctx, tx)
		if err != nil {
			return err
		}
		if receipt != nil {
			out = *receipt
		}
		return nil
	})
	if err != nil {
		return unknownOperation(), err
	}
	return out, nil
}

// lookup intentionally reveals no existence metadata to another actor/body/authority.
func (op *periodOperation) lookup(ctx context.Context, tx pgx.Tx) (*port.PeriodOperationReceipt, bool, error) {
	key, err := store.PeriodOperationByKey(ctx, tx, op.actor.tenant, op.request.OperationID)
	if err != nil {
		return nil, false, err
	}
	if key == nil {
		return nil, false, nil
	}
	if key.ActorRef != op.actor.ref || key.PeriodID != op.request.PeriodID || key.Profile != op.request.Profile || key.Action != op.request.Action || key.Digest != op.digest || key.Authority != op.authority {
		return nil, true, nil
	}
	receipt, err := store.PeriodOperationReceiptByID(ctx, tx, op.actor.tenant, key.ReceiptID)
	if err != nil {
		return nil, true, err
	}
	if receipt == nil {
		return nil, true, nil
	}
	if receipt.OperationID != key.OperationID || receipt.Profile != key.Profile || receipt.PeriodID != key.PeriodID || receipt.Action != key.Action || receipt.RequestDigest != key.Digest || receipt.ActorRef != key.ActorRef || receipt.AuthorityHash != key.Authority || receipt.Expected != op.request.Expected {
		return nil, true, nil
	}
	if receipt.Outcome != port.PeriodOperationApplied && receipt.Outcome != port.PeriodOperationRejected {
		return nil, true, nil
	}
	if receipt.Outcome == port.PeriodOperationApplied && (receipt.Applied.TenantID != op.actor.tenant || receipt.Applied.Profile != key.Profile || receipt.Applied.PeriodID != key.PeriodID || receipt.Applied.EventSeq < 1 || receipt.Applied.DueSeq < 0) {
		return nil, true, nil
	}
	return receipt, true, nil
}
func (op *periodOperation) before(ctx context.Context, tx pgx.Tx) error {
	if err := store.LockPeriodOperation(ctx, tx, op.actor.tenant, op.request.OperationID); err != nil {
		return err
	}
	receipt, exists, err := op.lookup(ctx, tx)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	op.receipt = unknownOperation()
	if receipt != nil {
		op.receipt = *receipt
	}
	op.durable = true
	return receiptStop
}
func (op *periodOperation) base(outcome string) port.PeriodOperationReceipt {
	req := op.request
	return port.PeriodOperationReceipt{OperationID: req.OperationID, Profile: req.Profile, PeriodID: req.PeriodID, Action: req.Action, RequestDigest: op.digest, ActorRef: op.actor.ref, AuthorityHash: op.authority, Expected: req.Expected, Outcome: outcome}
}
func rejectControl(ctx context.Context, tx pgx.Tx, guard *controlCAS, cause error) error {
	if guard == nil || guard.receipt == nil {
		return cause
	}
	op := guard.receipt
	if op.effects {
		return cause
	}
	receipt := op.base(port.PeriodOperationRejected)
	var p *problem.Problem
	if errors.As(cause, &p) {
		receipt.RejectionCode = p.Code
	} else {
		return cause
	}
	if err := store.InsertPeriodOperation(ctx, tx, op.actor.tenant, &receipt, op.now); err != nil {
		return err
	}
	op.receipt, op.durable = receipt, true
	return receiptStop
}
func beginControlEffects(guard *controlCAS) {
	if guard != nil && guard.receipt != nil {
		guard.receipt.effects = true
	}
}
func appliedOperation(ctx context.Context, tx pgx.Tx, guard *controlCAS) error {
	if guard.receipt == nil {
		return nil
	}
	op := guard.receipt
	v, err := load(ctx, tx, op.request.PeriodID, op.now)
	if err != nil {
		return err
	}
	if v == nil {
		return port.ErrPeriodNotFound
	}
	receipt := op.base(port.PeriodOperationApplied)
	receipt.Applied = guard.applied
	receipt.Result = port.PeriodOperationResult{Status: v.State.Status, DueAt: v.Period.DueAt.UTC()}
	if op.request.Action == port.PeriodOperationClose {
		if len(v.Snapshots) == 0 {
			return errors.New("forms: committed close snapshot missing")
		}
		snap := v.Snapshots[len(v.Snapshots)-1]
		receipt.Result.SnapshotID, receipt.Result.SnapshotNo, receipt.Result.SnapshotHash = snap.ID, snap.No, hex.EncodeToString(snap.Hash)
	}
	if err = store.InsertPeriodOperation(ctx, tx, op.actor.tenant, &receipt, op.now); err != nil {
		return err
	}
	op.receipt, op.durable = receipt, true
	return nil
}
