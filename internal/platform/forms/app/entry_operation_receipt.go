package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type reviewOperation struct {
	request           port.ReviewOperationRequest
	digest, authority string
	actor             actor
	profile           port.Profile
}

func unknownReviewOperation() port.ReviewOperationReceipt {
	return port.ReviewOperationReceipt{Outcome: port.ReviewOperationUnknown}
}

func (e Entry) prepareReviewOperation(ctx context.Context, in port.ReviewOperationRequest) (*reviewOperation, error) {
	if e.Svc == nil || e.Svc.DB == nil {
		return nil, port.ErrReviewOperationUnavailable
	}
	if _, joined := db.JoinedTx(ctx); joined {
		return nil, port.ErrReviewOperationUnavailable
	}
	principal, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	if !principal.MFA || principal.ActorRef == uuid.Nil || principal.TenantID == uuid.Nil || principal.SelectedSession == nil || !principal.SelectedSession.Valid() {
		return nil, problem.Forbidden
	}
	req, err := port.NormalizeReviewOperation(in)
	if err != nil {
		return nil, err
	}
	policy := e.Svc.ReviewOperationPolicies[req.Profile]
	if policy == nil {
		return nil, port.ErrReviewOperationUnavailable
	}
	prof, a, err := e.Svc.resolve(ctx, req.Profile)
	if err != nil {
		return nil, err
	}
	grant, err := policy(ctx, principal, req)
	if err != nil {
		return nil, err
	}
	if !grant.Allowed || !utf8.ValidString(grant.Binding) || strings.TrimSpace(grant.Binding) == "" || len(grant.Binding) > 512 || strings.ContainsRune(grant.Binding, 0) {
		return nil, problem.Forbidden
	}
	// The selected application context is NOT a unique OIDC login session.
	// Include its entire immutable authority projection and the owner's current
	// revocation/version binding. No client digest or session getter is trusted.
	stamp, err := principal.SelectedSession.AuthorizationStamp()
	if err != nil {
		return nil, problem.Forbidden.WithCause(err)
	}
	roles := slices.Clone(principal.Roles)
	slices.Sort(roles)
	units := slices.Clone(a.scope.Units)
	slices.Sort(units)
	raw, err := json.Marshal(struct {
		Actor, Tenant, Org, Node         uuid.UUID
		Roles, Units                     []string
		Role, Level, Department, Binding string
		All, MFA                         bool
		SelectedAuthority                []byte
	}{a.ref, a.tenant, principal.OrgRef, a.node, roles, units, a.role, a.level, a.dept, grant.Binding, a.scope.All, principal.MFA, stamp})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	digest, err := port.ReviewOperationDigest(req)
	if err != nil {
		return nil, err
	}
	// Native delegated authority and owner guard run before all receipt DB I/O.
	if err = e.Svc.authorizeReview(ctx, prof, a, req.SubmissionID, req.Decision, req.Note, reviewOperationIdem(req)); err != nil {
		return nil, err
	}
	return &reviewOperation{req, digest, hex.EncodeToString(sum[:]), a, prof}, nil
}

func reviewOperationIdem(req port.ReviewOperationRequest) string {
	return "forms.review_operation:" + req.OperationID.String()
}

func (e Entry) ExecuteReviewOperation(ctx context.Context, in port.ReviewOperationRequest) (port.ReviewOperationReceipt, error) {
	return e.reviewOperation(ctx, in, false)
}
func (e Entry) ReadReviewOperation(ctx context.Context, in port.ReviewOperationRequest) (port.ReviewOperationReceipt, error) {
	return e.reviewOperation(ctx, in, true)
}

func (e Entry) reviewOperation(ctx context.Context, in port.ReviewOperationRequest, read bool) (port.ReviewOperationReceipt, error) {
	op, err := e.prepareReviewOperation(ctx, in)
	if err != nil {
		return unknownReviewOperation(), err
	}
	req, a := op.request, op.actor
	out := unknownReviewOperation()
	err = e.Svc.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		// Check the current native template/target scope before lookup. Terminal
		// state and successor changes do not replace an earlier operation proof.
		sb, err := store.SubmissionByID(ctx, tx, req.SubmissionID)
		if err != nil {
			return err
		}
		if sb == nil || sb.PeriodID != req.PeriodID || sb.Unit != req.Unit {
			return port.ErrSubmissionMissing
		}
		v, err := load(ctx, tx, req.PeriodID, e.Svc.Clock.Now().UTC())
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != req.Profile || !a.allowsTemplate(v.Period.TemplateCode) || !a.allowsUnit(v.Spec, v.Period.Units, req.Unit) {
			return port.ErrSubmissionMissing
		}
		if err = store.LockReviewOperation(ctx, tx, a.tenant, req.OperationID); err != nil {
			return err
		}
		receipt, exists, err := op.lookup(ctx, tx)
		if err != nil {
			return err
		}
		if exists || read {
			if receipt != nil {
				out = *receipt
			}
			return nil
		}
		effects := false
		view, err := e.Svc.reviewNative(ctx, tx, op.profile, a, req.SubmissionID, req.Decision, req.Note, reviewOperationIdem(req), e.Svc.Clock.Now().UTC(), &req, &effects)
		if err != nil {
			if effects || !reviewRejection(err) {
				return err
			}
			out = op.base(port.ReviewOperationRejected)
			var p *problem.Problem
			if !errors.As(err, &p) {
				return err
			}
			out.RejectionCode = p.Code
		} else {
			out = op.base(port.ReviewOperationApplied)
			out.Result, err = store.ReviewResult(ctx, tx, view.Review.ID)
			if err != nil {
				return err
			}
		}
		return store.InsertReviewOperation(ctx, tx, &out, e.Svc.Clock.Now().UTC())
	})
	if err != nil {
		return unknownReviewOperation(), classify(err)
	}
	return out, nil
}

func reviewRejection(err error) bool {
	var p *problem.Problem
	if !errors.As(err, &p) {
		return false
	}
	for _, known := range []*problem.Problem{port.ErrVersionConflict, port.ErrSuperseded, port.ErrAlreadyReviewed, port.ErrPeriodCancelled, port.ErrPeriodClosed, port.ErrSubmissionLimit, problem.SelfApproval} {
		if p.Code == known.Code {
			return true
		}
	}
	return false
}

func (op *reviewOperation) base(outcome string) port.ReviewOperationReceipt {
	r := op.request
	return port.ReviewOperationReceipt{OperationID: r.OperationID, TenantID: op.actor.tenant, Profile: r.Profile, PeriodID: r.PeriodID, Unit: r.Unit, SubmissionID: r.SubmissionID, ExpectedVersion: r.ExpectedVersion, Decision: r.Decision, RequestDigest: op.digest, ActorRef: op.actor.ref, AuthorityHash: op.authority, Outcome: outcome}
}

// A permanent key stops all re-execution even when a receipt is absent/purged
// or mismatched. Reads reveal only the opaque unknown value in those cases.
func (op *reviewOperation) lookup(ctx context.Context, tx pgx.Tx) (*port.ReviewOperationReceipt, bool, error) {
	key, err := store.ReviewOperationByKey(ctx, tx, op.actor.tenant, op.request.OperationID)
	if err != nil {
		return nil, false, err
	}
	if key == nil {
		return nil, false, nil
	}
	if key.Actor != op.actor.ref || key.Digest != op.digest || key.Authority != op.authority {
		return nil, true, nil
	}
	r, err := store.ReviewOperationReceiptByID(ctx, tx, op.actor.tenant, key.ReceiptID)
	if err != nil || r == nil {
		return nil, true, err
	}
	expected := op.base(r.Outcome)
	expected.Result, expected.RecordedAt, expected.RejectionCode = r.Result, r.RecordedAt, r.RejectionCode
	// The immutable row and permanent anchor must agree on every original fact.
	if *r != expected || r.RecordedAt.IsZero() {
		return nil, true, nil
	}
	switch r.Outcome {
	case port.ReviewOperationApplied:
		if r.RejectionCode != "" || r.Result.ReviewID == uuid.Nil || r.Result.ReviewSeq < 1 || r.Result.Reviewer != op.actor.ref || r.Result.ReviewedAt.IsZero() || r.Result.ReviewRecordedAt.IsZero() {
			return nil, true, nil
		}
	case port.ReviewOperationRejected:
		if r.RejectionCode == "" || r.Result != (port.ReviewOperationResult{}) {
			return nil, true, nil
		}
	default:
		return nil, true, nil
	}
	return r, true, nil
}
