package dispatch

// Liên kết tải tệp Chi cục do api-platform phục vụ (quyết định A4, 03/10/2026; FN-TI-02.06): thay URL ký sẵn 15 phút
// của kho đối tượng. Mã thông báo là chuỗi base64url KHÔNG đoán được gồm tenant (16 byte, để mở giao dịch đúng RLS),
// mã liên kết (16 byte) và bí mật ngẫu nhiên 32 byte; DB chỉ giữ SHA-256 của bí mật. Sửa bất kỳ ký tự nào → không
// khớp băm hoặc không giải mã được → 404.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
)

// DefaultLinkTTLHours: hạn liên kết tải mặc định 7 ngày (tham số reports.dispatch_link_ttl_hours).
const DefaultLinkTTLHours = 168

// MaxLinkTTLHours: trần hạn liên kết (90 ngày) — khớp CHECK ttl_hours của pf_reports.dispatch_link.
const MaxLinkTTLHours = 2160

// LinkPath là đường dẫn công khai (không cần đăng nhập) của api-platform phục vụ liên kết tải.
const LinkPath = "/v1/public/report-links/"

const (
	secretLen = 32
	tokenLen  = 16 + 16 + secretLen
)

// NewLinkToken sinh mã thông báo cho liên kết linkID của tenant tid; trả mã (đưa vào email) và băm để lưu.
func NewLinkToken(tid, linkID uuid.UUID) (token string, hash []byte, err error) {
	b := make([]byte, tokenLen)
	copy(b, tid[:])
	copy(b[16:], linkID[:])
	if _, err := rand.Read(b[32:]); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b[32:])
	return base64.RawURLEncoding.EncodeToString(b), sum[:], nil
}

// ParseLinkToken tách mã thông báo; ok = false khi sai định dạng.
func ParseLinkToken(token string) (tid, linkID uuid.UUID, secretHash []byte, ok bool) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimSpace(token)) // Strict: bit đệm khác 0 = mã bị sửa
	if err != nil || len(b) != tokenLen {
		return uuid.Nil, uuid.Nil, nil, false
	}
	copy(tid[:], b[:16])
	copy(linkID[:], b[16:32])
	sum := sha256.Sum256(b[32:])
	return tid, linkID, sum[:], tid != uuid.Nil && linkID != uuid.Nil
}

// TokenMatches so băm theo thời gian hằng.
func TokenMatches(stored, got []byte) bool {
	return len(stored) == sha256.Size && subtle.ConstantTimeCompare(stored, got) == 1
}

// IPHash băm địa chỉ IP người tải bằng HMAC-SHA256 với khóa = băm mã thông báo của liên kết: cùng liên kết → cùng
// giá trị (nhận ra truy cập lặp), khác liên kết không đối chiếu được; không lưu IP thô (R-14). IP rỗng → "".
func IPHash(tokenHash []byte, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	m := hmac.New(sha256.New, tokenHash)
	m.Write([]byte(ip))
	return hex.EncodeToString(m.Sum(nil))
}

// LinkURL dựng URL tải đầy đủ từ URL gốc api-platform.
func LinkURL(base, token string) string {
	return strings.TrimRight(base, "/") + LinkPath + token
}
