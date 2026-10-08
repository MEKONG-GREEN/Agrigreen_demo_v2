package app

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"

	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ErrRecipientNotInCoop: người nhận không phải tài khoản đã đăng ký (email đã xác minh)
// của HTX người lập (HTX US-DASH-03 — giả định: chỉ chọn từ danh sách tài khoản của HTX).
var ErrRecipientNotInCoop = problem.New(http.StatusUnprocessableEntity, "reports.recipient_not_in_cooperative",
	"Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại.")

// coopManager: lịch trên Application API chỉ dành cho quản lý HTX thuần (coopOnly) có
// org_ref — cán bộ KN, Admin CGH, quản trị dùng Admin API (MFA).
func coopManager(ctx context.Context) (authz.Principal, error) {
	p, err := authz.Require(ctx, ccport.RoleCoopManager)
	if err != nil {
		return p, err
	}
	if !coopOnly(p) || p.OrgRef == uuid.Nil {
		return p, problem.Forbidden.WithDetail("lập lịch trên Application API chỉ dành cho quản lý HTX; cán bộ và quản trị dùng Admin API")
	}
	return p, nil
}

// CoopRecipients liệt kê người nhận chọn được cho lịch của quản lý HTX: tài khoản đang
// bật, email đã xác minh, thuộc HTX của người gọi (HTX US-DASH-03).
func (s *Service) CoopRecipients(ctx context.Context) ([]accountsport.Recipient, error) {
	p, err := coopManager(ctx)
	if err != nil {
		return nil, err
	}
	if s.Recipients == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh bạ tài khoản (pf/accounts)")
	}
	list, err := s.Recipients.OrgRecipients(ctx, p.TenantID, p.OrgRef)
	if err != nil {
		return nil, ErrDependency.WithDetail("không đọc được danh bạ tài khoản").WithCause(err)
	}
	return list, nil
}

// CreateCoopSchedule: quản lý HTX lập lịch gửi báo cáo HTX (HTX US-DASH-03) trên
// Application API. Người nhận phải là tài khoản của HTX (CoopRecipients); phạm vi của
// lịch luôn là HTX của người lập (CreateSchedule ghi cooperative_ref).
func (s *Service) CreateCoopSchedule(ctx context.Context, in ScheduleInput) (domain.Schedule, bool, error) {
	if _, err := coopManager(ctx); err != nil {
		return domain.Schedule{}, false, err
	}
	if len(in.Schedule.Recipients) > 0 {
		list, err := s.CoopRecipients(ctx)
		if err != nil {
			return domain.Schedule{}, false, err
		}
		for _, r := range in.Schedule.Recipients {
			if !slices.ContainsFunc(list, func(x accountsport.Recipient) bool { return x.ActorRef == r }) {
				return domain.Schedule{}, false, ErrRecipientNotInCoop.WithField("recipients", "chỉ chọn tài khoản đã đăng ký, có email đã xác minh của HTX")
			}
		}
	}
	return s.CreateSchedule(ctx, in)
}

// CoopSchedules liệt kê lịch HTX của quản lý HTX (Application API).
func (s *Service) CoopSchedules(ctx context.Context) ([]domain.Schedule, error) {
	if _, err := coopManager(ctx); err != nil {
		return nil, err
	}
	return s.ListSchedules(ctx)
}

// DeactivateCoopSchedule ngừng lịch HTX của quản lý HTX, kèm lý do (không xóa).
func (s *Service) DeactivateCoopSchedule(ctx context.Context, id uuid.UUID, reason string) (domain.Schedule, error) {
	if _, err := coopManager(ctx); err != nil {
		return domain.Schedule{}, err
	}
	return s.DeactivateSchedule(ctx, id, reason)
}

// CoopScheduleRuns trả các lần chạy (kèm trạng thái gửi email) của lịch HTX.
func (s *Service) CoopScheduleRuns(ctx context.Context, id uuid.UUID) ([]store.Run, error) {
	if _, err := coopManager(ctx); err != nil {
		return nil, err
	}
	return s.ScheduleRuns(ctx, id)
}
