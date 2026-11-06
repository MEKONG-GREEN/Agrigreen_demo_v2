package app

// Tự đặt lại mật khẩu qua email (#255 AUTH-V2-RESET-NOPIN, ADR-0056; BRD 2.0 Báo giá v2.0 FN-CH-00.01 luồng phụ
// "Quên mật khẩu?": nhập email, nhận liên kết hiệu lực 30 phút, dùng một lần, tối đa 5 yêu cầu mỗi giờ cho một tài
// khoản, tự đặt mật khẩu mới không qua Admin; tài khoản bắt MFA vẫn phải nhập OTP ở lần đăng nhập sau).
//
//   - RequestPasswordReset (API công khai): luôn trả nil cho email đúng cú pháp — không lộ email có tồn tại. Tài khoản
//     hợp lệ và chưa quá 5 yêu cầu/giờ → ghi pf_accounts.password_reset + sự kiện outbox trong MỘT giao dịch.
//   - HandlePasswordResetRequested (cmd/worker): sinh mã, chỉ ghi băm (giao dịch ngắn), gửi SMTP NGOÀI giao dịch, ghi
//     sent_at.
//   - ConfirmPasswordReset (API công khai): nhận lease trên mã, thu hồi phiên, đặt mật khẩu ở Keycloak, thu hồi phiên
//     lần nữa, đánh dấu đã dùng. Không có mã/mật khẩu trong log hay lỗi (R-14).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi đặt lại mật khẩu (docs/errors.md, mục pf/accounts).
var (
	// ErrResetLinkInvalid: một mã chung cho mã sai, đã dùng, hết hạn, đang được dùng hoặc tài khoản bị khóa.
	ErrResetLinkInvalid = problem.New(http.StatusGone, "accounts.reset_link_invalid",
		"Liên kết đặt lại mật khẩu không hợp lệ, đã dùng hoặc đã hết hạn. Hãy yêu cầu liên kết mới.")
	ErrPasswordPolicy = problem.New(http.StatusUnprocessableEntity, "accounts.password_policy",
		"Mật khẩu chưa đạt yêu cầu: tối thiểu 12 ký tự, khác tên đăng nhập và không trùng 5 mật khẩu gần nhất.")
)

// ResetMailer gửi một email văn bản thuần (pf/notify channel.SMTP thỏa mãn — gốc lắp ráp nối ở cmd/worker).
type ResetMailer interface {
	Send(ctx context.Context, to []string, subject, body string) error
}

// resetSubject là tiêu đề email liên kết đặt lại.
const resetSubject = "Đặt lại mật khẩu Hệ thống Khuyến nông"

// RequestPasswordReset xử lý POST /v1/public/password-reset. Email sai cú pháp → 400; còn lại luôn nil (202) dù email
// không có, tài khoản bị khóa hay đã quá 5 yêu cầu/giờ. Chỉ lỗi hạ tầng (Keycloak/DB) mới trả lỗi.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	addr, ok := domain.NormalizeResetEmail(email)
	if !ok {
		return problem.Validation.WithField("email", "email không hợp lệ")
	}
	if s.Dir == nil {
		return ErrDependency
	}
	users, err := s.Dir.FindUsers(ctx, port.Query{Email: addr, Max: 2})
	if err != nil {
		return ErrDependency.WithCause(err)
	}
	if len(users) != 1 {
		s.log().Info("yêu cầu đặt lại mật khẩu", "event", "accounts.password_reset", "outcome", "no_account")
		return nil
	}
	u := users[0]
	if !u.Enabled || !u.EmailVerified || u.TenantID == uuid.Nil || !strings.EqualFold(strings.TrimSpace(u.Email), addr) {
		s.log().Info("yêu cầu đặt lại mật khẩu", "event", "accounts.password_reset", "outcome", "not_eligible")
		return nil
	}
	now := s.now()
	id := ids.New()
	queued := false
	err = tenant.WithTenantTx(ctx, s.DB, u.TenantID, func(tx pgx.Tx) error {
		if err := store.LockPasswordResetUser(ctx, tx, u.ID); err != nil {
			return err
		}
		n, err := store.CountPasswordResets(ctx, tx, u.ID, now.Add(-domain.ResetWindow))
		if err != nil || n >= domain.ResetPerHour {
			return err
		}
		if err := store.InsertPasswordReset(ctx, tx, u.TenantID, store.PasswordReset{ID: id, UserRef: u.ID, RequestedAt: now}); err != nil {
			return err
		}
		queued = true
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: u.TenantID, Type: port.EventPasswordResetRequested, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: Producer,
			Payload: port.PasswordResetRequestedV1{ResetID: id, UserRef: u.ID, RequestedAt: now.Format(time.RFC3339Nano)},
		})
	})
	if err != nil {
		return err
	}
	outcome := "rate_limited"
	if queued {
		outcome = "queued"
	}
	s.log().Info("yêu cầu đặt lại mật khẩu", "event", "accounts.password_reset", "tenant_id", u.TenantID, "user_ref", u.ID,
		"reset_id", id, "outcome", outcome)
	return nil
}

// resetLink dựng URL trang đặt lại: mã nằm sau '#' nên không tới máy chủ web, không vào log hay Referer.
func resetLink(base, token string) string {
	return strings.TrimRight(base, "#") + "#token=" + token
}

func resetBody(link string, expires time.Time) string {
	return "Xin chào,\n\nHệ thống Khuyến nông nhận được yêu cầu đặt lại mật khẩu cho tài khoản của bạn. Mở liên kết sau để " +
		"đặt mật khẩu mới (dùng một lần, hết hạn lúc " + expires.In(clock.Zone()).Format("15:04 02/01/2006") + "):\n\n" + link +
		"\n\nNếu bạn không yêu cầu, hãy bỏ qua email này; mật khẩu hiện tại không đổi.\n"
}

// HandlePasswordResetRequested là consumer port.PasswordResetConsumer (cmd/worker). Bỏ giao dịch của inbox: mỗi bước DB
// là giao dịch ngắn riêng, Keycloak và SMTP gọi ngoài giao dịch. Gửi lỗi → trả lỗi để giao lại; lần sau sinh mã mới thay
// mã chưa gửi.
func (s *Service) HandlePasswordResetRequested(ctx context.Context, _ pgx.Tx, env outbox.Envelope) error {
	var p port.PasswordResetRequestedV1
	if err := json.Unmarshal(env.Payload, &p); err != nil || p.ResetID == uuid.Nil || p.UserRef == uuid.Nil || env.TenantID == uuid.Nil {
		return fmt.Errorf("accounts: payload %s không hợp lệ", env.EventType)
	}
	if s.Dir == nil || s.ResetMailer == nil || s.ResetURL == "" {
		return errors.New("accounts: chưa cấu hình Keycloak, kênh email hoặc AG_WEB_OPS_URL cho đặt lại mật khẩu")
	}
	tid := env.TenantID
	var r *store.PasswordReset
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var err error
		r, err = store.PasswordResetByID(ctx, tx, p.ResetID, false)
		return err
	}); err != nil {
		return err
	}
	if r == nil || r.SentAt != nil || r.UsedAt != nil || r.UserRef != p.UserRef {
		return nil // đã gửi (giao lại) hoặc không còn việc
	}
	u, err := s.Dir.GetUser(ctx, r.UserRef)
	if errors.Is(err, port.ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !u.Enabled || !u.EmailVerified || u.Email == "" || u.TenantID != tid {
		s.log().Info("bỏ gửi liên kết đặt lại mật khẩu", "event", "accounts.password_reset", "tenant_id", tid, "reset_id", r.ID, "outcome", "not_eligible")
		return nil
	}
	now := s.now()
	expires := now.Add(domain.ResetLinkTTL)
	tok, hash, err := domain.NewResetToken(tid, r.ID)
	if err != nil {
		return err
	}
	set := false
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		set, err = store.SetPasswordResetToken(ctx, tx, r.ID, hash, expires)
		return err
	}); err != nil || !set {
		return err
	}
	if err := s.ResetMailer.Send(ctx, []string{u.Email}, resetSubject, resetBody(resetLink(s.ResetURL, tok), expires)); err != nil {
		s.log().Warn("gửi liên kết đặt lại mật khẩu thất bại", "event", "accounts.password_reset", "tenant_id", tid, "reset_id", r.ID, "outcome", "send_failed")
		return fmt.Errorf("accounts: gửi email đặt lại mật khẩu: %w", err)
	}
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return store.MarkPasswordResetSent(ctx, tx, r.ID, s.now()) }); err != nil {
		return err
	}
	s.log().Info("đã gửi liên kết đặt lại mật khẩu", "event", "accounts.password_reset", "tenant_id", tid, "user_ref", r.UserRef,
		"reset_id", r.ID, "outcome", "sent")
	return nil
}

// ConfirmPasswordReset xử lý POST /v1/public/password-reset/confirm.
func (s *Service) ConfirmPasswordReset(ctx context.Context, token, password string) error {
	if password == "" || len(password) > domain.MaxResetPasswdLen {
		return problem.Validation.WithField("password", "mật khẩu từ 1 đến 256 byte")
	}
	tid, id, got, ok := domain.ParseResetToken(token)
	if !ok {
		return ErrResetLinkInvalid
	}
	setter, ok := s.Dir.(port.PasswordSetter)
	if s.Dir == nil || !ok {
		return ErrDependency
	}
	now := s.now()
	var r *store.PasswordReset
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var err error
		if r, err = store.PasswordResetByID(ctx, tx, id, true); err != nil || r == nil {
			return err
		}
		switch {
		case !domain.ResetTokenMatches(r.TokenHash, got), r.UsedAt != nil, r.ExpiresAt == nil, !now.Before(*r.ExpiresAt),
			r.ClaimedUntil != nil && now.Before(*r.ClaimedUntil):
			r = nil
			return nil
		}
		return store.ClaimPasswordReset(ctx, tx, id, now.Add(domain.ResetClaimLease))
	})
	if err != nil {
		return err
	}
	if r == nil {
		s.log().Info("xác nhận đặt lại mật khẩu bị từ chối", "event", "accounts.password_reset_confirm", "tenant_id", tid, "reset_id", id, "outcome", "invalid")
		return ErrResetLinkInvalid
	}
	release := func(cause error) error {
		if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return store.ReleasePasswordReset(ctx, tx, id) }); err != nil {
			return err
		}
		return cause
	}
	u, err := s.Dir.GetUser(ctx, r.UserRef)
	switch {
	case errors.Is(err, port.ErrUserNotFound):
		return release(ErrResetLinkInvalid)
	case err != nil:
		return release(ErrDependency.WithCause(err))
	case !u.Enabled || u.TenantID != tid:
		return release(ErrResetLinkInvalid)
	}
	// Thu hồi phiên trước và sau khi đặt mật khẩu: không còn phiên nào mở bằng mật khẩu cũ.
	if err := s.Dir.Logout(ctx, r.UserRef); err != nil {
		return release(ErrDependency.WithCause(err))
	}
	if err := setter.SetPassword(ctx, r.UserRef, password); err != nil {
		if errors.Is(err, port.ErrPasswordPolicy) {
			return release(ErrPasswordPolicy)
		}
		return release(ErrDependency.WithCause(err))
	}
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return store.UsePasswordResets(ctx, tx, r.UserRef, s.now()) }); err != nil {
		return err
	}
	outcome := "reset"
	if err := s.Dir.Logout(ctx, r.UserRef); err != nil {
		outcome = "reset_logout_failed"
	}
	s.log().Info("đã đặt lại mật khẩu qua liên kết email", "event", "accounts.password_reset_confirm", "tenant_id", tid, "user_ref", r.UserRef,
		"reset_id", id, "outcome", outcome)
	return nil
}
