package keycloak

// D24 (CEO 13/10/2026): đặt lại TOTP của người dùng (quản trị viên, có lý do) — port.OTPAdmin.

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// RemoveOTP xóa mọi thông tin xác thực loại otp: GET /users/{id}/credentials rồi DELETE
// /users/{id}/credentials/{credentialId} (vai realm-management manage-users). Trả số đã xóa.
func (c *Client) RemoveOTP(ctx context.Context, id uuid.UUID) (int, error) {
	var creds []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if _, err := c.do(ctx, "đọc thông tin xác thực", http.MethodGet, userPath(id)+"/credentials", nil, nil, &creds); err != nil {
		return 0, err
	}
	n := 0
	for _, cr := range creds {
		if cr.Type != "otp" || cr.ID == "" {
			continue
		}
		if _, err := c.do(ctx, "xóa TOTP", http.MethodDelete, userPath(id)+"/credentials/"+url.PathEscape(cr.ID), nil, nil, nil); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

var (
	_ port.OTPAdmin = (*Client)(nil)
	_ port.OTPAdmin = (*Memory)(nil)
)

// RemoveOTP thực hiện port.OTPAdmin.
func (m *Memory) RemoveOTP(_ context.Context, id uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("RemoveOTP"); err != nil {
		return 0, err
	}
	u, err := m.get(id)
	if err != nil {
		return 0, err
	}
	if !u.otp {
		return 0, nil
	}
	u.otp = false
	return 1, nil
}

// SetNow đặt đồng hồ của bản trong bộ nhớ (thời điểm tạo người dùng, phiên và sự kiện đăng nhập giả lập) —
// kiểm thử job accounts_dormant.
func (m *Memory) SetNow(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}
