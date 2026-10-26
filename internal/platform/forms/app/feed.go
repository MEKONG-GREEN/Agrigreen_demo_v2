package app

// W11.43: port.SubmissionFeed — đọc của hệ thống (consumer, job) cho quy tắc liên biểu + cảnh báo của bên dùng. Không
// kiểm người gọi: tenant truyền tường minh, giao dịch riêng của module (r_forms, RLS theo tenant).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
)

// Feed cài port.SubmissionFeed trên Service.
type Feed struct{ Svc *Service }

var _ port.SubmissionFeed = Feed{}

// duePeriodsMax giới hạn số kỳ một lần đọc (job nhắc chạy theo cửa sổ hạn nộp).
const duePeriodsMax = 2000

func recorded(f store.FeedSubmission) port.RecordedSubmission {
	return port.RecordedSubmission{ID: f.ID, PeriodID: f.PeriodID, Profile: f.Profile, TemplateCode: f.TemplateCode, PeriodLabel: f.PeriodLabel,
		PeriodKind: f.PeriodKind, PeriodKey: f.PeriodKey, From: f.From, To: f.To, DueAt: f.DueAt, UnitKind: f.UnitKind, Unit: f.Unit,
		Parent: f.Parent, Version: f.Version, Values: f.Values, NilReport: f.NilReport, Submitter: f.Submitter, EventTime: f.EventTime}
}

// Recorded đọc một phiên bản bài nộp.
func (f Feed) Recorded(ctx context.Context, tid, id uuid.UUID) (*port.RecordedSubmission, error) {
	var out *port.RecordedSubmission
	err := f.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		r, err := store.FeedByID(ctx, tx, id)
		if err != nil || r == nil {
			return err
		}
		v := recorded(*r)
		out = &v
		return nil
	})
	return out, err
}

// Latest trả bài nộp cuối của từng (kỳ, đơn vị) khớp q.
func (f Feed) Latest(ctx context.Context, tid uuid.UUID, q port.SubmissionQuery) ([]port.RecordedSubmission, error) {
	limit := q.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []port.RecordedSubmission
	err := f.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		rs, err := store.FeedLatest(ctx, tx, q.Profile, q.TemplateCode, q.Units, q.Parents, q.From, q.To, limit, q.ExcludePeriodKinds)
		for _, r := range rs {
			out = append(out, recorded(r))
		}
		return err
	})
	return out, err
}

// DuePeriods trả kỳ (không hủy) có hạn trong [dueFrom, dueTo] kèm tiến độ mọi đơn vị.
func (f Feed) DuePeriods(ctx context.Context, tid uuid.UUID, profile string, dueFrom, dueTo time.Time) ([]port.PeriodProgress, error) {
	now := f.Svc.Clock.Now().UTC()
	var out []port.PeriodProgress
	err := f.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		ids, err := store.DuePeriodIDs(ctx, tx, profile, dueFrom, dueTo, duePeriodsMax)
		if err != nil {
			return err
		}
		for _, id := range ids {
			v, err := load(ctx, tx, id, now)
			if err != nil {
				return err
			}
			if v == nil || v.State.Status == domain.PeriodCancelled {
				continue
			}
			out = append(out, progressOf(v))
		}
		return nil
	})
	return out, err
}

// progressOf chiếu PeriodView sang port.PeriodProgress (dùng chung Control.Progress).
func progressOf(v *PeriodView) port.PeriodProgress {
	out := port.PeriodProgress{ID: v.Period.ID, Profile: v.Period.Profile, Label: v.Period.Label, TemplateCode: v.Period.TemplateCode,
		DueAt: v.Period.DueAt, Status: v.State.Status, MaxSubmissions: v.Spec.MaxSubmissions, Units: make([]port.UnitProgress, 0, len(v.Units)),
		PeriodKey: v.Spec.PeriodKey, From: v.Spec.From, To: v.Spec.To}
	if out.Status == "" {
		out.Status = "open"
	}
	for _, u := range v.Units {
		up := port.UnitProgress{Unit: u.Unit, Status: u.Status, Parent: u.Parent}
		if u.Submission != nil {
			at := u.Submission.EventTime
			up.Versions, up.LastSubmittedAt = u.Submission.Version, &at
		}
		if u.Review != nil {
			at := u.Review.At
			up.Decision, up.ReviewNote, up.ReviewedAt = u.Review.Decision, u.Review.Note, &at
		}
		if u.Meta != nil {
			up.Late = u.Meta.Late
		}
		out.Units = append(out.Units, up)
	}
	return out
}
