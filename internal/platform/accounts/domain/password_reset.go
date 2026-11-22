package domain

// Mã liên kết tự đặt lại mật khẩu (#255, ADR-0057): tenant_id ‖ reset_id ‖ 32 byte ngẫu nhiên, base64url 86 ký tự.
// Tenant đi trong mã để mở đúng giao dịch RLS; DB chỉ giữ SHA-256 của phần bí mật.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Chính sách BRD 2.0 FN-CH-00.01: liên kết hiệu lực 30 phút, tối đa 5 yêu cầu mỗi giờ cho một tài khoản.
const (
	ResetLinkTTL      = 30 * time.Minute
	ResetPerHour      = 5
	ResetWindow       = time.Hour
	ResetClaimLease   = time.Minute // giữ mã trong lúc gọi Keycloak
	resetTokenLen     = 16 + 16 + 32
	MaxResetEmailLen  = 254
	MaxResetPasswdLen = 256
)

// NewResetToken sinh mã và băm phần bí mật.
func NewResetToken(tid, resetID uuid.UUID) (token string, hash []byte, err error) {
	b := make([]byte, resetTokenLen)
	copy(b, tid[:])
	copy(b[16:], resetID[:])
	if _, err := rand.Read(b[32:]); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b[32:])
	return base64.RawURLEncoding.EncodeToString(b), sum[:], nil
}

// ParseResetToken tách mã; ok = false khi sai định dạng (gồm bit đệm khác 0 — mã bị sửa).
func ParseResetToken(token string) (tid, resetID uuid.UUID, secretHash []byte, ok bool) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimSpace(token))
	if err != nil || len(b) != resetTokenLen {
		return uuid.Nil, uuid.Nil, nil, false
	}
	copy(tid[:], b[:16])
	copy(resetID[:], b[16:32])
	sum := sha256.Sum256(b[32:])
	return tid, resetID, sum[:], tid != uuid.Nil && resetID != uuid.Nil
}

// ResetTokenMatches so băm theo thời gian hằng.
func ResetTokenMatches(stored, got []byte) bool {
	return len(stored) == sha256.Size && subtle.ConstantTimeCompare(stored, got) == 1
}

// NormalizeResetEmail chuẩn hóa email người dùng nhập (cắt khoảng trắng, chữ thường); ok = false khi không giống một
// địa chỉ email (rỗng, quá dài, không có đúng một '@', có khoảng trắng/ký tự điều khiển).
func NormalizeResetEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > MaxResetEmailLen || strings.Count(s, "@") != 1 {
		return "", false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return "", false
		}
	}
	local, host, _ := strings.Cut(s, "@")
	if local == "" || host == "" || !strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", false
	}
	return s, true
}
