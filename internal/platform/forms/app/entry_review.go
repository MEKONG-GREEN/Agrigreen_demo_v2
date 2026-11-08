package app

import (
	"context"
	"slices"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// authorizeReview retains native authorization and guard calls outside DBTX.
func (s *Service) authorizeReview(ctx context.Context, prof port.Profile, a actor, id uuid.UUID, decision, note, idem string) error {
	op, err := s.reviewOperation(ctx, prof, a, id, decision, note, idem)
	if err != nil {
		return err
	}
	if _, err = needOperation(ctx, prof, a, prof.Reviewer(), "rà soát bài nộp", op); err != nil {
		return err
	}
	switch decision {
	case domain.SubAccepted:
	case domain.SubReturned:
		if note == "" {
			return problem.Validation.WithField("note", "bắt buộc khi trả lại")
		}
	default:
		return problem.Validation.WithField("decision", "accepted | returned")
	}
	if len([]rune(note)) > 1000 {
		return problem.Validation.WithField("note", "tối đa 1000 ký tự")
	}
	if g := s.ReviewGuards[prof.Name]; g != nil {
		var period uuid.UUID
		var code string
		if err := s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
			sb, err := store.SubmissionByID(ctx, tx, id)
			if err != nil {
				return err
			}
			if sb == nil {
				return port.ErrSubmissionMissing
			}
			v, err := load(ctx, tx, sb.PeriodID, s.Clock.Now().UTC())
			if err != nil {
				return err
			}
			if v == nil || v.Period.Profile != prof.Name || !a.allowsTemplate(v.Period.TemplateCode) || !a.allowsUnit(v.Spec, v.Period.Units, sb.Unit) {
				return port.ErrSubmissionMissing
			}
			period, code = sb.PeriodID, v.Period.TemplateCode
			return nil
		}); err != nil {
			return classify(err)
		}
		return g.AllowReview(ctx, period, code)
	}
	return nil
}

// reviewNative is shared by legacy Entry.Review and the additive supplier.
// Only the optional receipt expectation adds exact target/version matching.
func (s *Service) reviewNative(ctx context.Context, tx pgx.Tx, prof port.Profile, a actor, id uuid.UUID, decision, note, idem string, now time.Time, expect *port.ReviewOperationRequest, effects *bool) (SubmissionView, error) {
	sb, err := store.SubmissionByID(ctx, tx, id)
	if err != nil {
		return SubmissionView{}, err
	}
	if sb == nil {
		return SubmissionView{}, port.ErrSubmissionMissing
	}
	if err = store.Lock(ctx, tx, sb.PeriodID); err != nil {
		return SubmissionView{}, err
	}
	v, err := load(ctx, tx, sb.PeriodID, now)
	if err != nil {
		return SubmissionView{}, err
	}
	if v == nil || v.Period.Profile != prof.Name || !a.allowsTemplate(v.Period.TemplateCode) || !a.allowsUnit(v.Spec, v.Period.Units, sb.Unit) {
		return SubmissionView{}, port.ErrSubmissionMissing
	}
	if expect != nil {
		if sb.PeriodID != expect.PeriodID || sb.Unit != expect.Unit {
			return SubmissionView{}, port.ErrSubmissionMissing
		}
		if sb.Version != expect.ExpectedVersion {
			return SubmissionView{}, port.ErrVersionConflict
		}
	}
	if v.State.Status == domain.PeriodCancelled {
		return SubmissionView{}, port.ErrPeriodCancelled
	}
	if v.State.Status == domain.PeriodClosed {
		return SubmissionView{}, port.ErrPeriodClosed
	}
	i := slices.IndexFunc(v.Units, func(u UnitView) bool { return u.Unit == sb.Unit })
	cur := v.Units[i]
	if cur.Submission == nil || cur.Submission.ID != sb.ID {
		return SubmissionView{}, port.ErrSuperseded
	}
	if cur.Review != nil {
		return SubmissionView{}, port.ErrAlreadyReviewed
	}
	if decision == domain.SubReturned && v.Spec.LimitReached(sb.Version) {
		return SubmissionView{}, port.ErrSubmissionLimit.WithDetail("đã gửi đủ số lần tối đa — chỉ xác nhận được")
	}
	if sb.Submitter == a.ref {
		return SubmissionView{}, problem.SelfApproval.WithDetail("người rà soát trùng người nộp")
	}
	// Errors after here must roll back; they are never durable rejections.
	if effects != nil {
		*effects = true
	}
	var reqID *uuid.UUID
	if s.Approvals != nil {
		outcome, wnote := wfport.OutcomeApproved, ""
		if decision == domain.SubReturned {
			outcome, wnote = wfport.OutcomeRejected, note
		}
		d, err := s.Approvals.Decide(db.JoinTx(ctx, tx), wfport.DecideInput{Kind: WorkflowKind, Subject: sb.ID, Requester: sb.Submitter, Decider: a.ref, Outcome: outcome, Note: wnote, At: now})
		if err != nil {
			return SubmissionView{}, err
		}
		reqID = &d.RequestID
	}
	r := domain.Review{ID: ids.New(), Decision: decision, Reviewer: a.ref, Note: note, At: now}
	if err = store.InsertReview(ctx, tx, a.tenant, sb.ID, &r, reqID, idemOr(idem, "review")); err != nil {
		return SubmissionView{}, err
	}
	return SubmissionView{Submission: *sb, Status: decision, Review: &r}, nil
}
