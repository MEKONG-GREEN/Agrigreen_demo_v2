package residency

// Phân loại môi trường cho kênh gửi ra ngoài (P0-NOTIFY-FAILCLOSED, #127): email/đẩy/liên kết trong email chỉ được
// "giả gửi" (ghi log) hay dùng liên kết tương đối/localhost ở máy phát triển. Mọi nơi khác — staging, máy demo
// (AG_DEPLOY_ENV=demo chạy AG_ENV=staging), production, hoặc AG_ENV chưa đặt — kênh thiếu cấu hình phải báo đúng
// "chưa gửi", không bao giờ "đã gửi".

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// LocalDev cho biết AG_ENV là máy phát triển/kiểm thử: development|dev|local|test. AG_ENV thiếu hay giá trị khác
// (staging, production…) → false: guard không "mở cửa" vì quên đặt biến.
func LocalDev(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvName))) {
	case "development", "dev", "local", "test":
		return true
	}
	return false
}

// ErrLinkBase: URL gốc dùng cho liên kết trong email thiếu, không tuyệt đối hoặc trỏ về máy cục bộ ngoài máy phát
// triển. Bên gọi tắt kênh (ghi bản gửi là bị bỏ, kèm lý do) thay vì gửi liên kết hỏng.
var ErrLinkBase = errors.New("URL gốc của liên kết trong email chưa cấu hình hợp lệ")

// PublicBaseURL đọc URL gốc công khai (vd AG_WEB_OPS_URL, AG_REPORT_LINK_BASE) cho liên kết trong email. Máy phát
// triển (LocalDev): trả nguyên giá trị (rỗng → liên kết tương đối, localhost được phép). Nơi khác: bắt buộc URL
// http(s) tuyệt đối, máy chủ không phải localhost/loopback; thiếu hay sai → ("", ErrLinkBase). Lỗi chỉ nêu tên biến.
func PublicBaseURL(getenv func(string) string, key string) (string, error) {
	v := strings.TrimSpace(getenv(key))
	if LocalDev(getenv) {
		return v, nil
	}
	if v == "" {
		return "", fmt.Errorf("%w: chưa đặt %s", ErrLinkBase, key)
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%w: %s phải là URL http(s) tuyệt đối", ErrLinkBase, key)
	}
	if loopbackHost(u.Hostname()) {
		return "", fmt.Errorf("%w: %s trỏ về máy cục bộ", ErrLinkBase, key)
	}
	return v, nil
}

func loopbackHost(h string) bool {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
