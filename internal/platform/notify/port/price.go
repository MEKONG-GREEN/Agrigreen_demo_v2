package port

// Thông báo Báo giá nông sản (gói NOTIF — G1, quyết định D5; pf/price → pf/notify, pf 20261013070025): loại riêng, người nhận
// Cấp Tỉnh, email theo từng người nhận.

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Loại thông báo của Báo giá nông sản (CHECK kind — pf 20261013070025).
const (
	// KindPriceReminder: nhắc nhập giá lần 1 / lần 2 → cán bộ Xã (FN-XA-03.04; W11.74 tạm dùng report_task_due).
	KindPriceReminder = "price_reminder"
	// KindPriceAlert: cảnh báo biến động Tạm tính mới khi Xã gửi → Cấp Tỉnh phụ trách xã + QTNV (FN-TI-02.03 B1).
	KindPriceAlert = "price_alert"
	// KindPriceVerifyRequest: yêu cầu xác minh → cán bộ Xã phụ trách (FN-TI-02.03 B3, FN-XA-03.05).
	KindPriceVerifyRequest = "price_verify_request"
	// KindPriceVerifyReply: Xã phản hồi xác minh → Cấp Tỉnh phụ trách xã + QTNV (FN-XA-03.05 B4).
	KindPriceVerifyReply = "price_verify_reply"
	// KindPriceVerifyDecision: kết quả xử lý cảnh báo / hiệu chỉnh → cán bộ Xã (FN-TI-02.03 B6, FN-TI-02.04).
	KindPriceVerifyDecision = "price_verify_decision"
	// KindPriceLockSummary: tổng hợp lúc khóa nhập (12:00) → Cấp Tỉnh + QTNV (FN-TI-02.03 B4).
	KindPriceLockSummary = "price_lock_summary"
	// KindPriceBoardApproved: bảng giá ngày đã duyệt (phiên bản mới) → Cấp Tỉnh + QTNV (FN-TI-02.05).
	KindPriceBoardApproved = "price_board_approved"
	// KindPriceBoardReopened: bảng giá ngày đang mở duyệt → Cấp Tỉnh + QTNV (FN-TI-02.06).
	KindPriceBoardReopened = "price_board_reopened"
	// KindPriceProxyEntry: Cấp Tỉnh nhập thay báo giá Nộp muộn → cán bộ Xã phụ trách (FN-TI-02.09; W11.77, pf
	// 20261013133703).
	KindPriceProxyEntry = "price_proxy_entry"
)

// PriceDesk tìm người nhận Cấp Tỉnh của Báo giá nông sản (pf/notify khai cổng; gốc lắp ráp hiện thực từ pf/accounts —
// lần gán Cấp Tỉnh của phòng ban chủ dữ liệu giá và cờ nhiệm vụ QTNV). Tenant truyền tường minh (consumer của worker
// không có Principal). Chỉ mã tham chiếu — không tên, không email. nil → thông báo chủ đề theo vai Cấp Tỉnh (không email).
type PriceDesk interface {
	// Stewards trả cán bộ Cấp Tỉnh có cờ "Quản trị nghiệp vụ báo giá" của bên dùng profile.
	Stewards(ctx context.Context, tenantID uuid.UUID, profile string) ([]uuid.UUID, error)
	// ProvinceOfficers trả mọi cán bộ Cấp Tỉnh của bên dùng profile (xã chưa phân công — mọi Cấp Tỉnh xử lý).
	ProvinceOfficers(ctx context.Context, tenantID uuid.UUID, profile string) ([]uuid.UUID, error)
}

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
