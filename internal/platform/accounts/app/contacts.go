package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// StaffContacts hiện thực extension/port.StaffContacts: tra số điện thoại của tài
// khoản cán bộ (id Keycloak = actor_ref) lúc bấm "gọi/nhắn" trong danh bạ trực
// (KN US-DIR-02). Số chỉ đi qua bộ nhớ — không lưu ở pf, không ghi log (R-14).
type StaffContacts struct{ Dir port.Directory }

var _ port.StaffContacts = StaffContacts{}

// PhoneOf trả số điện thoại của tài khoản đang bật, đúng tenant.
func (m StaffContacts) PhoneOf(ctx context.Context, tenantID, actorRef uuid.UUID) (string, error) {
	if m.Dir == nil {
		return "", ErrNoDirectory
	}
	u, err := m.Dir.GetUser(ctx, actorRef)
	if errors.Is(err, port.ErrUserNotFound) {
		return "", port.ErrNoPhone
	}
	if err != nil {
		return "", err
	}
	if u.TenantID != tenantID || !u.Enabled || strings.TrimSpace(u.Phone) == "" {
		return "", port.ErrNoPhone
	}
	return u.Phone, nil
}
