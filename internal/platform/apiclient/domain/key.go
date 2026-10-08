// Package domain: quy tắc thuần của pf/apiclient — sinh khóa API, HMAC có pepper theo phiên bản
// (ADR-0022 mục 4), kiểm scope của đăng ký sản phẩm, giới hạn gói hạn mức. Không DB, không đồng hồ hệ
// thống (thời điểm truyền vào).
package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// RuleVersion là rule_version của mọi sổ pf_apiclient.
const RuleVersion = "apiclient.v1"

// prefixAlphabet là base32 thường RFC 4648 (a-z, 2-7) — 32 ký tự, nên byte & 31 không lệch.
const prefixAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

// GeneratedKey là khóa vừa sinh. Full là bí mật — chỉ trả MỘT lần cho người gọi Admin API, không
// bao giờ lưu, log hay đưa vào lỗi (R-14, R-92).
type GeneratedKey struct {
	Prefix string
	Full   string // "agk_<prefix>_<secret>"
}

// GenerateKey sinh khóa thật mới từ nguồn ngẫu nhiên r (crypto/rand.Reader ở production).
func GenerateKey(r io.Reader) (GeneratedKey, error) { return GenerateKeyFor(r, false) }

// GenerateKeyFor sinh khóa mới; sandbox → tiền tố khóa thử port.SandboxKeyTokenPrefix (W5.8, R-INT-08).
func GenerateKeyFor(r io.Reader, sandbox bool) (GeneratedKey, error) {
	if r == nil {
		r = rand.Reader
	}
	var pb [port.KeyPrefixLength]byte
	if _, err := io.ReadFull(r, pb[:]); err != nil {
		return GeneratedKey{}, fmt.Errorf("apiclient: sinh prefix khóa: %w", err)
	}
	prefix := make([]byte, len(pb))
	for i, b := range pb {
		prefix[i] = prefixAlphabet[b&31]
	}
	var secret [32]byte
	if _, err := io.ReadFull(r, secret[:]); err != nil {
		return GeneratedKey{}, fmt.Errorf("apiclient: sinh bí mật khóa: %w", err)
	}
	token := port.KeyTokenPrefix
	if sandbox {
		token = port.SandboxKeyTokenPrefix
	}
	full := token + string(prefix) + "_" + base64.RawURLEncoding.EncodeToString(secret[:])
	if p, ok := port.ParseKey(full); !ok || p != string(prefix) {
		return GeneratedKey{}, errors.New("apiclient: khóa sinh ra sai dạng")
	}
	return GeneratedKey{Prefix: string(prefix), Full: full}, nil
}

// Peppers là các pepper HMAC theo phiên bản (AG_APIKEY_PEPPER_V<n>) và phiên bản hiện hành
// (AG_APIKEY_PEPPER_CURRENT) dùng cho khóa MỚI. Khóa cũ kiểm theo pepper_version của chính nó.
type Peppers struct {
	Current  int
	versions map[int][]byte
}

// ErrNoPepper: chưa cấu hình pepper (Admin API không phát hành khóa, api-public không khởi động).
var ErrNoPepper = errors.New("apiclient: chưa cấu hình AG_APIKEY_PEPPER_V<n> / AG_APIKEY_PEPPER_CURRENT")

// NewPeppers dựng tập pepper (kiểm thử và PeppersFromEnv). Mỗi pepper ≥ port.MinPepperBytes byte.
func NewPeppers(current int, versions map[int][]byte) (*Peppers, error) {
	if len(versions) == 0 {
		return nil, ErrNoPepper
	}
	cp := map[int][]byte{}
	for v, b := range versions {
		if v < 1 {
			return nil, fmt.Errorf("apiclient: phiên bản pepper %d phải ≥ 1", v)
		}
		if len(b) < port.MinPepperBytes {
			return nil, fmt.Errorf("apiclient: %s%d phải ≥ %d byte", port.EnvPepperPrefix, v, port.MinPepperBytes)
		}
		cp[v] = append([]byte(nil), b...)
	}
	if _, ok := cp[current]; !ok {
		return nil, fmt.Errorf("apiclient: %s=%d không có pepper tương ứng", port.EnvPepperCurrent, current)
	}
	return &Peppers{Current: current, versions: cp}, nil
}

// PeppersFromEnv đọc pepper từ môi trường (trình quản lý bí mật — ADR-0021). Không đặt biến nào →
// (nil, nil): Admin API vẫn chạy nhưng phát hành khóa trả 503; cấu hình sai → lỗi khởi động.
func PeppersFromEnv(getenv func(string) string, environ []string) (*Peppers, error) {
	versions := map[int][]byte{}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, port.EnvPepperPrefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(k, port.EnvPepperPrefix))
		if err != nil {
			return nil, fmt.Errorf("apiclient: tên biến pepper không hợp lệ (%s<n>)", port.EnvPepperPrefix)
		}
		versions[n] = []byte(v)
	}
	cur := strings.TrimSpace(getenv(port.EnvPepperCurrent))
	if len(versions) == 0 && cur == "" {
		return nil, nil
	}
	if cur == "" {
		// Không đặt CURRENT → phiên bản lớn nhất.
		vs := make([]int, 0, len(versions))
		for v := range versions {
			vs = append(vs, v)
		}
		sort.Ints(vs)
		return NewPeppers(vs[len(vs)-1], versions)
	}
	n, err := strconv.Atoi(cur)
	if err != nil {
		return nil, fmt.Errorf("apiclient: %s phải là số phiên bản", port.EnvPepperCurrent)
	}
	return NewPeppers(n, versions)
}

// MAC tính HMAC-SHA-256(pepper_v, khóa đầy đủ). ok = false khi không có pepper phiên bản v.
func (p *Peppers) MAC(version int, full string) ([]byte, bool) {
	if p == nil {
		return nil, false
	}
	key, ok := p.versions[version]
	if !ok {
		return nil, false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(full))
	return m.Sum(nil), true
}

// Verify so HMAC của khóa trình ra với HMAC đã lưu bằng thời gian hằng.
func (p *Peppers) Verify(version int, full string, stored []byte) bool {
	got, ok := p.MAC(version, full)
	if !ok {
		// Vẫn tốn một lần HMAC để thời gian không lộ phiên bản pepper thiếu.
		got, _ = p.MAC(p.Current, full)
		_ = hmac.Equal(got, stored)
		return false
	}
	return hmac.Equal(got, stored)
}

// KeyUsable cho biết một khóa (bảng chiếu) còn xác thực được tại at: active và chưa hết hạn, hoặc
// rotated còn trong hạn chồng lấn (và chưa hết hạn).
func KeyUsable(status string, expiresAt time.Time, graceUntil *time.Time, at time.Time) bool {
	if !at.Before(expiresAt) {
		return false
	}
	switch status {
	case KeyStatusActive:
		return true
	case KeyStatusRotated:
		return graceUntil != nil && at.Before(*graceUntil)
	}
	return false
}

// Trạng thái khóa ở bảng chiếu api_key_current.
const (
	KeyStatusActive  = "active"
	KeyStatusRotated = "rotated"
	KeyStatusRevoked = "revoked"
	KeyStatusExpired = "expired"
)
