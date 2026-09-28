package domain

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// PushMessage là thông điệp DỮ LIỆU gửi tới một thiết bị. Chỉ mang mã thông báo
// và loại — không tiêu đề, không nội dung, không dữ liệu cá nhân (RB-7, ADR-0015):
// dịch vụ đẩy nằm ngoài lãnh thổ; app nhận xong gọi API của ta để lấy nội dung.
type PushMessage struct {
	Token          string `pii:"true"` // token của nhà cung cấp đẩy: định danh thiết bị, không ghi log
	Platform       string
	Kind           string
	NotificationID *uuid.UUID // nil với sync_now
	Urgent         bool
}

// Data là cặp khóa–giá trị của thông điệp dữ liệu. Đây là TOÀN BỘ những gì rời
// khỏi lãnh thổ qua kênh đẩy (ngoài token).
func (m PushMessage) Data() map[string]string {
	d := map[string]string{"kind": m.Kind}
	if m.NotificationID != nil {
		d["notification_id"] = m.NotificationID.String()
	}
	return d
}

// Nhà cung cấp token đẩy.
const ProviderFCM = "fcm"

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_:.\-]+$`)

// ValidToken kiểm dạng token đẩy (FCM: 20–4096 ký tự chữ, số, _ : . -).
func ValidToken(provider, token string) bool {
	token = strings.TrimSpace(token)
	return provider == ProviderFCM && len(token) >= 20 && len(token) <= 4096 && tokenRe.MatchString(token)
}
