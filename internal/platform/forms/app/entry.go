package app

// W11.63: Entry — cổng nhập/rà soát bài nộp kỳ native cho module nghiệp vụ (KN phiếu "Báo cáo cho KHTC"). Dùng lại
// SubmitWith/Submissions/Review (cùng kiểm quyền, giới hạn lần gửi, khóa lạc quan, bốn mắt) và chỉ đổi hình dạng kết quả.

import (
	"context"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// Entry cài port.Entry trên Service.
type Entry struct{ Svc *Service }

var _ port.Entry = Entry{}

func refOfView(v SubmissionView) port.SubmissionRef {
	r := port.SubmissionRef{ID: v.Submission.ID, Unit: v.Submission.Unit, Version: v.Submission.Version, Values: v.Submission.Values,
		Submitter: v.Submission.Submitter, SubmittedAt: v.Submission.EventTime}
	if v.Meta != nil {
		r.Late = v.Meta.Late
	}
	if v.Review != nil {
		at, by := v.Review.At, v.Review.Reviewer
		r.Decision, r.ReviewNote, r.ReviewedAt, r.Reviewer = v.Review.Decision, v.Review.Note, &at, &by
	}
	return r
}

// Submit nộp phiên bản mới (khóa lạc quan theo baseVersion).
func (e Entry) Submit(ctx context.Context, profile string, periodID uuid.UUID, unit string, values map[string]string, baseVersion int64,
	idem string) (port.SubmissionRef, error) {
	base := baseVersion
	v, err := e.Svc.SubmitWith(ctx, profile, periodID, SubmitInput{Unit: unit, Values: values, BaseVersion: &base}, idem)
	if err != nil {
		return port.SubmissionRef{}, err
	}
	return refOfView(v), nil
}

// Submissions trả lịch sử phiên bản của đơn vị (mới trước).
func (e Entry) Submissions(ctx context.Context, profile string, periodID uuid.UUID, unit string) ([]port.SubmissionRef, error) {
	vs, err := e.Svc.Submissions(ctx, profile, periodID, unit)
	if err != nil {
		return nil, err
	}
	out := make([]port.SubmissionRef, 0, len(vs))
	for _, v := range vs {
		out = append(out, refOfView(v))
	}
	return out, nil
}

// Review rà soát phiên bản cuối.
func (e Entry) Review(ctx context.Context, profile string, submissionID uuid.UUID, decision, note, idem string) (port.SubmissionRef, error) {
	v, err := e.Svc.Review(ctx, profile, submissionID, decision, note, idem)
	if err != nil {
		return port.SubmissionRef{}, err
	}
	return refOfView(v), nil
}
