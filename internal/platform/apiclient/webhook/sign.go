package webhook

// Chữ ký HMAC của webhook (R-INT-07, ADR-0033 §4) và bí mật dẫn xuất.
//
// Header gửi đối tác:
//
//	AG-Webhook-Id:        mã lần giao (UUIDv7) — khóa idempotent phía nhận
//	AG-Webhook-Timestamp: giây Unix lúc ký
//	AG-Webhook-Signature: t=<giây Unix>,v1=<hex HMAC-SHA256(bí mật, "<t>.<thân>")>[,v1=<hex bí mật cũ>]
//
// Trong khoảng chồng lấn sau khi xoay bí mật, header có HAI giá trị v1 (bí mật mới trước) — đối tác chấp nhận nếu
// một giá trị khớp. Đối tác từ chối khi |now − t| vượt dung sai (mặc định 5 phút) để chống phát lại.
//
// Bí mật không lưu ở DB: Secret = HMAC-SHA256(khóa chủ AG_WEBHOOK_KEY_V<k>, "agrigreen.webhook.v1:<webhook_id>:
// <phiên bản bí mật>:<nonce>"); hiện cho quản trị viên MỘT lần dạng "whsec_" + base64url.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Header của lời gọi webhook.
const (
	HeaderID        = "AG-Webhook-Id"
	HeaderTimestamp = "AG-Webhook-Timestamp"
	HeaderSignature = "AG-Webhook-Signature"
	SecretPrefix    = "whsec_"
	// DefaultTolerance là dung sai thời gian khi xác minh.
	DefaultTolerance = 5 * time.Minute
)

// Biến môi trường của khóa chủ (trình quản lý bí mật — ADR-0021).
const (
	EnvKeyPrefix  = "AG_WEBHOOK_KEY_V"
	EnvKeyCurrent = "AG_WEBHOOK_KEY_CURRENT"
	// MinKeyBytes là độ dài tối thiểu của một khóa chủ.
	MinKeyBytes = 32
)

// Lỗi xác minh chữ ký.
var (
	ErrSignatureMissing = errors.New("webhook: thiếu hoặc sai dạng header chữ ký")
	ErrSignatureExpired = errors.New("webhook: dấu thời gian ngoài dung sai")
	ErrSignatureInvalid = errors.New("webhook: chữ ký không khớp")
	ErrNoKeyring        = errors.New("webhook: chưa cấu hình " + EnvKeyPrefix + "<n> / " + EnvKeyCurrent)
)

// Mac tính hex HMAC-SHA256(secret, "<t>.<body>").
func Mac(secret []byte, t int64, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(strconv.FormatInt(t, 10)))
	m.Write([]byte{'.'})
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// Sign dựng giá trị header AG-Webhook-Signature với mọi bí mật đang hiệu lực (mới trước).
func Sign(secrets [][]byte, at time.Time, body []byte) string {
	t := at.Unix()
	var b strings.Builder
	b.WriteString("t=")
	b.WriteString(strconv.FormatInt(t, 10))
	for _, s := range secrets {
		b.WriteString(",v1=")
		b.WriteString(Mac(s, t, body))
	}
	return b.String()
}

// Verify xác minh header chữ ký với bí mật của đối tác (thư viện mẫu cho đối tác và kiểm thử).
func Verify(header string, secret, body []byte, now time.Time, tolerance time.Duration) error {
	if tolerance <= 0 {
		tolerance = DefaultTolerance
	}
	var (
		t    int64
		sigs []string
		okT  bool
	)
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return ErrSignatureMissing
		}
		switch k {
		case "t":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return ErrSignatureMissing
			}
			t, okT = n, true
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if !okT || len(sigs) == 0 {
		return ErrSignatureMissing
	}
	if d := now.Sub(time.Unix(t, 0)); d > tolerance || d < -tolerance {
		return ErrSignatureExpired
	}
	want := Mac(secret, t, body)
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return ErrSignatureInvalid
}

// FormatSecret mã hóa bí mật để hiện cho quản trị viên (một lần).
func FormatSecret(raw []byte) string { return SecretPrefix + base64.RawURLEncoding.EncodeToString(raw) }

// ParseSecret giải "whsec_…" thành byte bí mật.
func ParseSecret(s string) ([]byte, error) {
	if !strings.HasPrefix(s, SecretPrefix) {
		return nil, errors.New("webhook: bí mật phải bắt đầu bằng " + SecretPrefix)
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, SecretPrefix))
}

// Keyring là các khóa chủ theo phiên bản dẫn xuất bí mật webhook.
type Keyring struct {
	Current int
	keys    map[int][]byte
}

// NewKeyring dựng tập khóa chủ (mỗi khóa ≥ MinKeyBytes byte).
func NewKeyring(current int, keys map[int][]byte) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, ErrNoKeyring
	}
	cp := make(map[int][]byte, len(keys))
	for v, k := range keys {
		if v < 1 || len(k) < MinKeyBytes {
			return nil, fmt.Errorf("webhook: %s%d phải ≥ %d byte", EnvKeyPrefix, v, MinKeyBytes)
		}
		cp[v] = append([]byte(nil), k...)
	}
	if _, ok := cp[current]; !ok {
		return nil, fmt.Errorf("webhook: %s=%d không có khóa tương ứng", EnvKeyCurrent, current)
	}
	return &Keyring{Current: current, keys: cp}, nil
}

// KeyringFromEnv đọc AG_WEBHOOK_KEY_V<n> / AG_WEBHOOK_KEY_CURRENT (không CURRENT → phiên bản lớn nhất). Không đặt
// biến nào → (nil, nil): đăng ký / xoay bí mật trả 503, job giao không chạy.
func KeyringFromEnv(getenv func(string) string, environ []string) (*Keyring, error) {
	keys := map[int][]byte{}
	for _, kv := range environ {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, EnvKeyPrefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(k, EnvKeyPrefix))
		if err != nil || n < 1 {
			return nil, fmt.Errorf("webhook: tên biến khóa không hợp lệ (%s<n>)", EnvKeyPrefix)
		}
		keys[n] = []byte(getenv(k))
	}
	if len(keys) == 0 {
		return nil, nil
	}
	cur := strings.TrimSpace(getenv(EnvKeyCurrent))
	if cur == "" {
		vs := make([]int, 0, len(keys))
		for v := range keys {
			vs = append(vs, v)
		}
		sort.Ints(vs)
		return NewKeyring(vs[len(vs)-1], keys)
	}
	n, err := strconv.Atoi(cur)
	if err != nil {
		return nil, fmt.Errorf("webhook: %s phải là số phiên bản", EnvKeyCurrent)
	}
	return NewKeyring(n, keys)
}

// Secret dẫn xuất bí mật của (webhook, phiên bản bí mật, nonce) từ khóa chủ keyVersion. ok = false khi thiếu khóa.
func (k *Keyring) Secret(keyVersion int, webhookID uuid.UUID, secretVersion int, nonce string) ([]byte, bool) {
	if k == nil {
		return nil, false
	}
	key, ok := k.keys[keyVersion]
	if !ok {
		return nil, false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte("agrigreen.webhook.v1:" + webhookID.String() + ":" + strconv.Itoa(secretVersion) + ":" + nonce))
	return m.Sum(nil), true
}
