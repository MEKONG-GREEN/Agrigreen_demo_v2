package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Email theo từng người nhận (gói NOTIF — Báo giá nông sản FN-XA-03.04, FN-XA-03.05, NFR "Thông báo"): một thông báo
// có RecipientRef và cờ Email → một lần gửi ở pf_notify.email_delivery. Địa chỉ KHÔNG lưu ở pf_notify: consumer
// pf.notify.email tra lúc gửi qua pf/accounts (Keycloak) và chỉ ghi log dạng đã che (MaskEmail).

// EmailJob là một lần gửi email chưa xong, kèm nội dung thông báo (tiêu đề/nội dung đã Redact, không dữ liệu cá nhân).
type EmailJob struct {
	ID             uuid.UUID
	BatchID        uuid.UUID
	NotificationID uuid.UUID
	RecipientRef   uuid.UUID
	Kind           string
	Status         string
	Attempts       int
	Title          string
	Body           string
	DeepLink       string
	CreatedAt      time.Time
}

// Mã lỗi ghi ở email_delivery.last_error (chỉ mã, không địa chỉ).
const (
	EmailErrNoAddress = "no_email"  // tài khoản không có email đã xác minh → chỉ gửi trong ứng dụng (FN-XA-03.04 ngoại lệ)
	EmailErrLookup    = "lookup"    // tra địa chỉ lỗi tạm thời
	EmailErrSend      = "temporary" // máy chủ thư lỗi tạm thời
)

// MaskEmail che phần tên hộp thư để ghi log: "nguyen.van.a@mekong.vn" → "ng•••@mekong.vn"; không phải địa chỉ → "•••".
func MaskEmail(e string) string {
	e = strings.TrimSpace(e)
	at := strings.LastIndexByte(e, '@')
	if at <= 0 || at == len(e)-1 {
		return "•••"
	}
	local := []rune(e[:at])
	keep := 2
	if len(local) <= 2 {
		keep = 1
	}
	return string(local[:keep]) + "•••" + e[at:]
}

// EmailMessage dựng tiêu đề và nội dung email (văn bản thuần) của một thông báo. linkBase (vd https://kn.example.vn) rỗng
// → chỉ ghi đường dẫn trong ứng dụng.
func EmailMessage(j EmailJob, linkBase string) (subject, body string) {
	subject = "[AgriGreen] " + j.Title
	var b strings.Builder
	b.WriteString(j.Title)
	b.WriteString("\n\n")
	if j.Body != "" {
		b.WriteString(j.Body)
		b.WriteString("\n\n")
	}
	if j.DeepLink != "" {
		b.WriteString("Mở trong ứng dụng: " + strings.TrimRight(linkBase, "/") + j.DeepLink + "\n\n")
	}
	b.WriteString("Email tự động từ hệ thống AgriGreen, không trả lời thư này.\n")
	return subject, b.String()
}
