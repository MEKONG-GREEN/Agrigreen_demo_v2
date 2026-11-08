package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SubmitInput's stable v1 wire shape is the request binding, including nil base
// versus explicit zero and the offline identity. It is never inferred from latest.
func submissionRequestOf(in SubmitInput) (json.RawMessage, error) {
	// Keep the persisted v1 request stable when SubmitInput gains unrelated fields.
	request := struct {
		ID                *uuid.UUID
		Unit              string
		Values            map[string]string
		BaseVersion       *int64
		NilReport         bool
		Note, Explanation string
		CapturedAt        *time.Time
		DeviceID          string
	}{in.ID, in.Unit, in.Values, in.BaseVersion, in.NilReport, in.Note, in.Explanation, in.CapturedAt, in.DeviceID}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(raw) > 1048576 {
		return nil, problem.Validation.WithDetail("bài nộp quá lớn")
	}
	return raw, nil
}

func recordSubmissionReceipt(ctx context.Context, tx pgx.Tx, a actor, profile string, request json.RawMessage, key string, out SubmissionView) error {
	if key == "" {
		return nil
	}
	return store.InsertSubmissionReceipt(ctx, tx, a.tenant, profile, request, key, out.Submission, out.Status)
}

func replaySubmissionReceipt(ctx context.Context, tx pgx.Tx, a actor, v *PeriodView, unit, key string, request json.RawMessage, out *SubmissionView) (bool, error) {
	r, err := store.SubmissionReceiptByKey(ctx, tx, key, request)
	if err != nil || r == nil {
		return false, err
	}
	if !r.SameRequest || r.Profile != v.Period.Profile || r.PeriodID != v.Period.ID || r.Unit != unit || r.Actor != a.ref {
		return false, problem.Conflict.WithDetail("khóa gửi đã dùng cho nội dung, người gửi hoặc phiếu khác")
	}
	if r.Status == domain.SubAwaitingReopen {
		h, err := store.HeldByID(ctx, tx, r.SubmissionID)
		if err != nil {
			return false, err
		}
		if h == nil || h.PeriodID != r.PeriodID || h.Unit != r.Unit || h.Submitter != a.ref {
			return false, problem.Conflict
		}
		*out = SubmissionView{Submission: store.Submission{ID: h.ID, PeriodID: h.PeriodID, Unit: h.Unit, Values: h.Values, Submitter: h.Submitter, EventTime: h.EventTime}, Status: r.Status, Replayed: true}
		return true, nil
	}
	sb, err := store.SubmissionByID(ctx, tx, r.SubmissionID)
	if err != nil {
		return false, err
	}
	if sb == nil || sb.PeriodID != r.PeriodID || sb.Unit != r.Unit || sb.Submitter != a.ref {
		return false, problem.Conflict
	}
	meta, err := store.MetaOf(ctx, tx, []uuid.UUID{sb.ID})
	if err != nil {
		return false, err
	}
	m, ok := meta[sb.ID]
	if !ok {
		return false, problem.Conflict
	}
	*out = SubmissionView{Submission: *sb, Status: r.Status, Meta: &m, Replayed: true}
	return true, nil
}
