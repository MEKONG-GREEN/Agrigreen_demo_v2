package app

// W11.62: PeriodControl — cổng điều khiển kỳ native cho module nghiệp vụ (KN Kỳ KHTC). Dùng lại Period/Close/Cancel
// (cùng kiểm quyền, máy trạng thái, ảnh chụp đóng băng) và chỉ đổi hình dạng kết quả.

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
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
	return progressOf(&v), nil
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

// ChangeDue đổi hạn nộp của kỳ (cấp close_by của Nút phát hành; kỳ đang nhận bài).
func (c Control) ChangeDue(ctx context.Context, profile string, id uuid.UUID, due time.Time, reason, idem string) error {
	s := c.Svc
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return err
	}
	if due.IsZero() {
		return problem.Validation.WithField("due_at", "bắt buộc")
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 500 {
		return problem.Validation.WithField("reason", "tối đa 500 ký tự")
	}
	now := s.Clock.Now().UTC()
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, id); err != nil {
			return err
		}
		v, err := load(ctx, tx, id, now)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.canSee(v.Period, v.Spec) {
			return port.ErrPeriodNotFound
		}
		if !a.canClose(prof.CloseBy, v.Period, v.Spec, nil) {
			return problem.Forbidden.WithDetail("đổi hạn nộp cần cấp " + prof.CloseBy + " của Nút phát hành")
		}
		switch v.State.Status {
		case domain.PeriodCancelled:
			return port.ErrPeriodCancelled
		case domain.PeriodClosed:
			return port.ErrPeriodClosed
		}
		return store.InsertDue(ctx, tx, a.tenant, id, due, reason, a.ref, now, idemOr(idem, "due"))
	})
	return classify(err)
}
