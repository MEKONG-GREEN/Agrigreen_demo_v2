package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// StaffContacts tra số điện thoại công vụ của cán bộ để "gọi/nhắn 1 chạm" từ danh
// bạ trực (KN US-DIR-02). Số nằm ở tài khoản Keycloak (pf/accounts), được đọc lúc
// người dùng bấm liên hệ — pf/extension không lưu, không ghi log (R-04, R-14).
type StaffContacts interface {
	// PhoneOf trả số điện thoại của tài khoản actorRef thuộc tenantID. Tài khoản
	// không có, đã khóa, khác tenant hoặc chưa khai số → ErrNoPhone.
	PhoneOf(ctx context.Context, tenantID, actorRef uuid.UUID) (string, error)
}

// ErrNoPhone: cán bộ chưa có số điện thoại dùng được.
var ErrNoPhone = errors.New("extension: cán bộ chưa có số điện thoại")
