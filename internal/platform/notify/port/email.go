package port

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

// EmailDirectory tra địa chỉ email đã xác minh của tài khoản lúc gửi (pf/accounts — Keycloak; gốc lắp ráp bọc
// accounts/port.MailDirectory). pf/notify không lưu địa chỉ. Tài khoản không có email → ErrNoEmail (chỉ gửi trong ứng
// dụng — BRD FN-XA-03.04 luồng ngoại lệ); lỗi khác là lỗi tạm thời (thử lại).
type EmailDirectory interface {
	EmailOf(ctx context.Context, tenantID, actorRef uuid.UUID) (string, error)
}

// ErrNoEmail: tài khoản không có email đã xác minh (bên hiện thực EmailDirectory trả lỗi bọc lỗi này).
var ErrNoEmail = errors.New("notify: tài khoản không có email")

// Sự kiện email theo người nhận.
const EventEmailRequested = "pf.notify.email.requested"

// Tên consumer / job của gói NOTIF.
const (
	// ConsumerEmail: gửi email theo từng người nhận (pf.notify.email.requested).
	ConsumerEmail = "pf.notify.email"
)

// EmailRequestedV1 là payload của pf.notify.email.requested: một lô email theo người nhận đã xếp trong
// pf_notify.email_delivery. Không có địa chỉ, không dữ liệu cá nhân.
//
//ag:event pf.notify.email.requested v1
type EmailRequestedV1 struct {
	BatchID    uuid.UUID `json:"batch_id"`
	Kind       string    `json:"kind"`
	Deliveries int       `json:"deliveries"`
}
