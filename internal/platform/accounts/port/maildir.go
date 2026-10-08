package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Danh bạ thư cho module gửi email (pf/reports US-DASH-03): khai ở pf/accounts (lõi, chủ tài khoản và email)
// để phụ thuộc chỉ đi xuống (R-MOD-05) — module gửi biết pf/accounts, pf/accounts không biết module gửi.

// ErrNoEmail: tài khoản không có địa chỉ email đã xác minh.
var ErrNoEmail = errors.New("accounts: tài khoản không có email")

// MailDirectory tra địa chỉ email của tài khoản nền tảng lúc gửi (pf/reports US-DASH-03).
// pf/accounts hiện thực; pf/reports không lưu email. Tenant truyền tường minh
// vì job gửi chạy ở worker.
type MailDirectory interface {
	EmailOf(ctx context.Context, tenantID, actorRef uuid.UUID) (string, error)
}

// Recipient là một tài khoản đã đăng ký của HTX có email đã xác minh — người nhận được
// chọn cho lịch báo cáo của quản lý HTX (HTX US-DASH-03). Email chỉ hiện đã che.
type Recipient struct {
	ActorRef    uuid.UUID
	DisplayName string `pii:"true"`
	MaskedEmail string `pii:"true"` // vd "qu•••@htx.vn"
}

// RecipientDirectory liệt kê tài khoản đang bật, email đã xác minh, thuộc tổ chức orgRef
// (thuộc tính org_ref của tài khoản). pf/accounts hiện thực; pf/reports không lưu email.
type RecipientDirectory interface {
	OrgRecipients(ctx context.Context, tenantID, orgRef uuid.UUID) ([]Recipient, error)
}
