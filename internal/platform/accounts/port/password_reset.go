package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Tự đặt lại mật khẩu qua email (#255 AUTH-V2-RESET-NOPIN, ADR-0057; BRD 2.0 Báo giá v2.0 FN-CH-00.01).
const (
	// EventPasswordResetRequested: một yêu cầu đặt lại đã được xếp gửi (pf/accounts tự nhận ở cmd/worker).
	EventPasswordResetRequested = "pf.accounts.password_reset.requested"
	// PasswordResetConsumer là tên consumer (hàng đợi) gửi email liên kết đặt lại.
	PasswordResetConsumer = "pf.accounts.password_reset"
)

// PasswordResetRequestedV1 là payload của pf.accounts.password_reset.requested. Không chứa email hay mã: worker sinh mã,
// chỉ ghi băm vào pf_accounts.password_reset rồi gửi SMTP ngoài giao dịch.
//
//ag:event pf.accounts.password_reset.requested v1
type PasswordResetRequestedV1 struct {
	ResetID     uuid.UUID `json:"reset_id"`
	UserRef     uuid.UUID `json:"user_ref"`
	RequestedAt string    `json:"requested_at"`
}

// ErrPasswordPolicy: Keycloak từ chối mật khẩu mới vì không đạt chính sách realm (độ dài, trùng tên đăng nhập,
// trùng mật khẩu gần đây).
var ErrPasswordPolicy = errors.New("accounts: mật khẩu không đạt chính sách của realm")

// PasswordSetter (tùy chọn, kiểm bằng type assertion) đặt mật khẩu cố định (không tạm) cho người dùng trong Keycloak.
// Mật khẩu chỉ đi qua bộ nhớ, không vào lỗi hay log (R-14). Tách khỏi Directory như ProfileUpdater.
type PasswordSetter interface {
	SetPassword(ctx context.Context, id uuid.UUID, password string) error
}
