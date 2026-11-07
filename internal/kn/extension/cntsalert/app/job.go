package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// JobTenant chạy job cho một tenant tại now (kiểm thử, chạy tay).
func (s *Service) JobTenant(ctx context.Context, tid uuid.UUID, now time.Time) (JobResult, error) {
	ctx = tenant.WithID(ctx, tid) // cổng module khác (đơn vị hành chính, tham số) đọc tenant từ ngữ cảnh
	var r JobResult
	var err error
	if r.Overdue, err = s.EscalateOverdue(ctx, tid, now); err != nil {
		return r, err
	}
	if r.Reminders, r.Streaks, err = s.Remind(ctx, tid, now); err != nil {
		return r, err
	}
	if r.Vaccines, err = s.VaccineOverdue(ctx, tid, now); err != nil { // BF-CN-VACC: dòng 11 mũi tiêm quá hạn
		return r, err
	}
	r.NoCheckouts, err = s.NoCheckout(ctx, tid, now) // CNTS v4.0: dòng 13 Chưa check-out
	return r, err
}
