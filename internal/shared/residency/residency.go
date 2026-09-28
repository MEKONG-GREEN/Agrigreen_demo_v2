// Package residency cưỡng chế RB-7 lúc khởi động: ở production mọi đích mạng
// mà tiến trình được cấu hình để gửi dữ liệu tới (DB, S3, OTLP, SMTP, FCM,
// Keycloak, kho định danh, RabbitMQ…) phải thuộc danh mục nhà cung cấp trong
// lãnh thổ (mục 21 tài liệu kiến trúc). Sai → dừng ngay (fail fast), không chạy
// "tạm" với cấu hình lệch.
//
// Biến môi trường:
//
//	AG_ENV                  production (hoặc prod) mới kiểm; môi trường khác bỏ qua
//	AG_RESIDENCY_ALLOWLIST  danh mục trong lãnh thổ, phân tách bằng dấu phẩy — BẮT BUỘC ở production
//	AG_NONPII_ALLOWLIST     (tùy chọn) đích ngoài lãnh thổ CHỈ nhận dữ liệu không định danh
//	                        (vd FCM data-only: fcm.googleapis.com) — chỉ áp cho biến đẩy thông báo
//
// Mỗi mục của danh mục là một trong:
//
//	agrigreen.vn     hậu tố tên miền: khớp agrigreen.vn và mọi *.agrigreen.vn (".vn" cũng được)
//	203.0.113.10     một địa chỉ IP
//	10.0.0.0/8       một dải CIDR
//	single-label     tên dịch vụ nội cụm một nhãn (vd "pgbouncer") — cụm chạy trong lãnh thổ
//
// Biến được kiểm: mọi biến AG_* có tên kết thúc bằng _URL, _URLS, _ENDPOINT,
// _HOST, _ISSUER (trừ AG_TEST_*), nên module mới (SMTP, FCM…) chỉ cần đặt tên
// biến theo quy ước là được kiểm tự động. Thông báo lỗi chỉ nêu TÊN BIẾN và máy
// chủ — không bao giờ in cả URL (có thể chứa mật khẩu).
package residency

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Tên biến môi trường.
const (
	EnvName          = "AG_ENV"
	EnvAllowlist     = "AG_RESIDENCY_ALLOWLIST"
	EnvNonPIIAllow   = "AG_NONPII_ALLOWLIST"
	keywordSingleLbl = "single-label"
)

// endpointSuffixes là hậu tố tên biến chứa đích mạng.
var endpointSuffixes = []string{"_URL", "_URLS", "_ENDPOINT", "_HOST", "_ISSUER"}

// nonPIIPrefixes là biến được phép trỏ ra ngoài lãnh thổ nếu đích thuộc
// AG_NONPII_ALLOWLIST: kênh đẩy thông báo chỉ mang dữ liệu không định danh
// (data-only, mã tham chiếu) — ADR bắt buộc theo mục 21.
var nonPIIPrefixes = []string{"AG_FCM_", "AG_APNS_", "AG_PUSH_"}

// ErrViolation là lỗi gốc của mọi vi phạm RB-7.
var ErrViolation = errors.New("RB-7: đích ngoài danh mục nhà cung cấp trong lãnh thổ")

// Production cho biết AG_ENV là production.
func Production(getenv func(string) string) bool {
	e := strings.ToLower(strings.TrimSpace(getenv(EnvName)))
	return e == "production" || e == "prod"
}

// CheckEnv kiểm môi trường của tiến trình hiện tại. Gọi đầu tiên trong main.
func CheckEnv() error { return Check(os.Getenv, os.Environ()) }

// Check kiểm các biến trong environ (dạng K=V) với getenv. Không phải production → nil.
func Check(getenv func(string) string, environ []string) error {
	if !Production(getenv) {
		return nil
	}
	allow, err := ParseList(getenv(EnvAllowlist))
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrViolation, EnvAllowlist, err)
	}
	if allow.Empty() {
		return fmt.Errorf("%w: AG_ENV=production bắt buộc %s", ErrViolation, EnvAllowlist)
	}
	nonPII, err := ParseList(getenv(EnvNonPIIAllow))
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrViolation, EnvNonPIIAllow, err)
	}
	names := endpointVars(environ)
	var bad []string
	for _, k := range names {
		v := strings.TrimSpace(getenv(k))
		if v == "" {
			continue
		}
		hosts, err := Hosts(v)
		if err != nil {
			bad = append(bad, k+": không đọc được máy chủ")
			continue
		}
		for _, h := range hosts {
			if allow.Match(h) {
				continue
			}
			if eligibleNonPII(k) && nonPII.Match(h) {
				continue
			}
			bad = append(bad, fmt.Sprintf("%s → %s", k, h))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%w: %s", ErrViolation, strings.Join(bad, "; "))
	}
	return nil
}

func endpointVars(environ []string) []string {
	seen := map[string]bool{}
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(k, "AG_") || strings.HasPrefix(k, "AG_TEST_") || seen[k] {
			continue
		}
		for _, s := range endpointSuffixes {
			if strings.HasSuffix(k, s) {
				seen[k] = true
				break
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func eligibleNonPII(k string) bool {
	for _, p := range nonPIIPrefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// Hosts trích các máy chủ từ một giá trị cấu hình: URL có scheme (kể cả
// postgres://h1:5432,h2:5432/db), host:port, host, hoặc danh sách phân tách
// bằng dấu phẩy. Socket Unix (host rỗng) không có máy chủ mạng → bỏ qua.
func Hosts(v string) ([]string, error) {
	var out []string
	for _, part := range splitTop(v) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "://") {
			u, err := url.Parse(part)
			if err != nil {
				// postgres nhiều máy chủ: url.Parse từ chối "h1:1,h2:2" ở một số dạng — tách tay.
				hs, herr := multiHostURL(part)
				if herr != nil {
					return nil, err
				}
				out = append(out, hs...)
				continue
			}
			for _, hp := range strings.Split(u.Host, ",") {
				if h := hostOnly(hp); h != "" {
					out = append(out, h)
				}
			}
			continue
		}
		if h := hostOnly(part); h != "" {
			out = append(out, h)
		}
	}
	return out, nil
}

// splitTop tách danh sách theo dấu phẩy nhưng giữ nguyên dấu phẩy trong phần
// authority của URL postgres nhiều máy chủ.
func splitTop(v string) []string {
	if strings.Contains(v, "://") && strings.Count(v, "://") == 1 {
		return []string{v}
	}
	return strings.Split(v, ",")
}

func multiHostURL(s string) ([]string, error) {
	_, rest, ok := strings.Cut(s, "://")
	if !ok {
		return nil, errors.New("không phải URL")
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	var out []string
	for _, hp := range strings.Split(rest, ",") {
		if h := hostOnly(hp); h != "" {
			out = append(out, h)
		}
	}
	return out, nil
}

func hostOnly(hp string) string {
	hp = strings.TrimSpace(hp)
	if i := strings.LastIndex(hp, "@"); i >= 0 {
		hp = hp[i+1:]
	}
	if i := strings.IndexAny(hp, "/?#"); i >= 0 {
		hp = hp[:i]
	}
	if h, _, err := net.SplitHostPort(hp); err == nil {
		hp = h
	}
	hp = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSuffix(hp, "]"), "["), ".")
	return strings.ToLower(hp)
}

// List là một danh mục đã phân tích.
type List struct {
	suffixes    []string
	addrs       []netip.Addr
	prefixes    []netip.Prefix
	singleLabel bool
}

// ParseList đọc danh mục phân tách bằng dấu phẩy. "*" hay mục rỗng như "." bị từ chối.
func ParseList(s string) (List, error) {
	var l List
	for _, raw := range strings.Split(s, ",") {
		e := strings.ToLower(strings.TrimSpace(raw))
		if e == "" {
			continue
		}
		switch {
		case e == keywordSingleLbl:
			l.singleLabel = true
		case strings.Contains(e, "*"):
			return List{}, fmt.Errorf("mục %q: không dùng ký tự đại diện", e)
		case strings.Contains(e, "/"):
			p, err := netip.ParsePrefix(e)
			if err != nil {
				return List{}, fmt.Errorf("mục %q: CIDR không hợp lệ", e)
			}
			l.prefixes = append(l.prefixes, p.Masked())
		default:
			if a, err := netip.ParseAddr(e); err == nil {
				l.addrs = append(l.addrs, a)
				continue
			}
			d := strings.Trim(e, ".")
			if d == "" || strings.ContainsAny(d, ":@ ") {
				return List{}, fmt.Errorf("mục %q: không phải tên miền, IP hay CIDR", e)
			}
			l.suffixes = append(l.suffixes, d)
		}
	}
	return l, nil
}

// Empty cho biết danh mục rỗng.
func (l List) Empty() bool {
	return len(l.suffixes) == 0 && len(l.addrs) == 0 && len(l.prefixes) == 0 && !l.singleLabel
}

// Match cho biết máy chủ h (đã chữ thường, không cổng) thuộc danh mục.
func (l List) Match(h string) bool {
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	if a, err := netip.ParseAddr(h); err == nil {
		a = a.Unmap()
		for _, x := range l.addrs {
			if x.Unmap() == a {
				return true
			}
		}
		for _, p := range l.prefixes {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	if l.singleLabel && h != "" && !strings.Contains(h, ".") {
		return true
	}
	for _, s := range l.suffixes {
		if h == s || strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}
