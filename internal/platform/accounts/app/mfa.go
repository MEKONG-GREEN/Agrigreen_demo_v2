package app

// D24 (CEO 13/10/2026 — W10.03): trạng thái MFA của tài khoản và đặt lại TOTP có lý do.

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// MFAStatus là trạng thái TOTP của tài khoản cho màn quản trị.
type MFAStatus struct {
	// Required: vai của tài khoản bắt TOTP lúc đăng nhập (port.LoginTOTPRoles — D24).
	Required bool
	// Configured: đã có thông tin xác thực otp trong Keycloak.
	Configured bool
	// EnrollmentPending: đang chờ người dùng đăng ký (required action CONFIGURE_TOTP).
	EnrollmentPending bool
}

// mfaStatus đọc trạng thái MFA từ Keycloak (vai realm trực tiếp + thông tin xác thực).
func (s *Service) mfaStatus(ctx context.Context, u port.User, realm []string) (MFAStatus, error) {
	has, err := s.Dir.HasOTP(ctx, u.ID)
	if err != nil {
		return MFAStatus{}, dirErr(err)
	}
	return MFAStatus{
		Required:          port.NeedsLoginTOTP(realm),
		Configured:        has,
		EnrollmentPending: slices.Contains(u.RequiredActions, port.ActionConfigureTOTP),
	}, nil
}

// TOTPResetInput là dữ liệu đặt lại TOTP.
type TOTPResetInput struct {
	UserID  uuid.UUID
	Reason  string
	IdemKey string
}

// ResetTOTP gỡ TOTP của tài khoản (mất/đổi điện thoại): xóa thông tin xác thực otp trong Keycloak, đặt required
// action CONFIGURE_TOTP (đăng ký lại ở lần đăng nhập sau), thu hồi mọi phiên, ghi sổ 'totp_reset' kèm lý do
// ≥ 20 ký tự. Cùng ma trận cấp vai/phạm vi như khóa; không tự đặt lại TOTP của chính mình.
func (s *Service) ResetTOTP(ctx context.Context, in TOTPResetInput) (Result, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := requireKey(in.IdemKey); err != nil {
		return Result{}, err
	}
	if err := domain.CheckReason(in.Reason); err != nil {
		return Result{}, fieldProblem(err)
	}
	if prev, err := s.replay(ctx, a.p.TenantID, in.IdemKey); err != nil || prev != nil {
		return Result{Account: derefAcc(prev), Event: derefEv(prev), Replayed: prev != nil}, err
	}
	if in.UserID == a.p.ActorRef {
		return Result{}, ErrSelfAction.WithDetail("Không được tự đặt lại TOTP của chính mình.")
	}
	otp, ok := s.Dir.(port.OTPAdmin)
	if !ok {
		return Result{}, ErrDependency.WithDetail("Keycloak Admin chưa hỗ trợ đặt lại TOTP.")
	}
	t, err := s.target(ctx, a, in.UserID)
	if err != nil {
		return Result{}, err
	}
	cur := t.state()
	if !cur.Enabled {
		return Result{}, ErrState.WithDetail("Tài khoản đang bị khóa; mở khóa trước khi đặt lại TOTP.")
	}
	removed, err := otp.RemoveOTP(ctx, t.user.ID)
	if err != nil {
		return Result{}, dirErr(err)
	}
	pending := slices.Contains(t.user.RequiredActions, port.ActionConfigureTOTP)
	if removed == 0 && !pending && !port.NeedsLoginTOTP(t.roles) {
		return Result{}, ErrState.WithDetail("Tài khoản chưa cấu hình TOTP.")
	}
	if !pending {
		if err := s.Dir.AddRequiredActions(ctx, t.user.ID, []string{port.ActionConfigureTOTP}); err != nil {
			return Result{}, dirErr(err)
		}
	}
	revoked := s.Dir.Logout(ctx, t.user.ID) == nil
	ev := newEvent(a.p.TenantID, a.p.ActorRef, cur, domain.ActionTOTPReset, s.now(), in.IdemKey, in.Reason)
	ev.Details = map[string]any{"otp_removed": removed, "sessions_revoked": revoked}
	replayed, err := s.write(ctx, a.p.TenantID, &ev)
	return Result{Account: ev.Account, Event: ev, User: t.user, Replayed: replayed}, err
}
