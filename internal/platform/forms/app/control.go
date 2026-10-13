package app

// W11.62: PeriodControl — cổng điều khiển kỳ native cho module nghiệp vụ (KN Kỳ KHTC). Dùng lại Period/Close/Cancel
// (cùng kiểm quyền, máy trạng thái, ảnh chụp đóng băng) và chỉ đổi hình dạng kết quả.

import (
	"context"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// Control cài port.PeriodControl trên Service.
type Control struct{ Svc *Service }

var _ port.PeriodControl = Control{}

// Progress đọc kỳ và tiến độ từng đơn vị trong phạm vi người gọi.
func (c Control) Progress(ctx context.Context, profile string, id uuid.UUID) (port.PeriodProgress, error) {
	v, err := c.Svc.Period(ctx, profile, id)
	if err != nil {
		return port.PeriodProgress{}, err
	}
	out := port.PeriodProgress{ID: v.Period.ID, Profile: v.Period.Profile, Label: v.Period.Label, TemplateCode: v.Period.TemplateCode,
		DueAt: v.Period.DueAt, Status: v.State.Status, MaxSubmissions: v.Spec.MaxSubmissions, Units: make([]port.UnitProgress, 0, len(v.Units))}
	if out.Status == "" {
		out.Status = "open"
	}
	for _, u := range v.Units {
		up := port.UnitProgress{Unit: u.Unit, Status: u.Status}
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
	return out, nil
}

// ClosePeriod chốt kỳ (close_by của Nút phát hành) kèm ảnh chụp đóng băng.
func (c Control) ClosePeriod(ctx context.Context, profile string, id uuid.UUID, idem string) error {
	_, err := c.Svc.Close(ctx, profile, id, idem)
	return err
}

// CancelPeriod hủy kỳ đang mở (lý do bắt buộc).
func (c Control) CancelPeriod(ctx context.Context, profile string, id uuid.UUID, reason, idem string) error {
	_, err := c.Svc.Cancel(ctx, profile, id, reason, idem)
	return err
}
