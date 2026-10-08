package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// MailDirectory hiện thực reports/port.MailDirectory: tra email của tài khoản
// (id Keycloak = actor_ref) lúc gửi lịch báo cáo (KN US-DASH-03). Email chỉ đi qua
// bộ nhớ tới bộ gửi thư — không lưu, không ghi log (R-14).
//
// Chỉ trả email khi tài khoản đang bật, email đã xác minh và tài khoản thuộc đúng
// tenant; mọi trường hợp khác trả reportsport.ErrNoEmail (người nhận bị bỏ qua).
type MailDirectory struct{ Dir port.Directory }

var (
	_ port.MailDirectory      = MailDirectory{}
	_ port.RecipientDirectory = MailDirectory{}
)

// ErrNoDirectory: chưa cấu hình Keycloak Admin REST (AG_KC_*).
var ErrNoDirectory = errors.New("accounts: chưa cấu hình Keycloak Admin REST")

// EmailOf trả email đã xác minh của tài khoản.
func (m MailDirectory) EmailOf(ctx context.Context, tenantID, actorRef uuid.UUID) (string, error) {
	if m.Dir == nil {
		return "", ErrNoDirectory
	}
	u, err := m.Dir.GetUser(ctx, actorRef)
	if errors.Is(err, port.ErrUserNotFound) {
		return "", port.ErrNoEmail
	}
	if err != nil {
		return "", err
	}
	if u.TenantID != tenantID || !u.Enabled || !u.EmailVerified || strings.TrimSpace(u.Email) == "" {
		return "", port.ErrNoEmail
	}
	return u.Email, nil
}
