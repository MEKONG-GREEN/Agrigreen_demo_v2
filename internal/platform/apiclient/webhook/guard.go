package webhook

// Chặn SSRF cho URL webhook của đối tác (R-INT-07, ADR-0033 §4). Hai lớp:
//
//  1. lúc đăng ký (ValidateURL): https, không userinfo/fragment, cổng cho phép, tên máy không phải tên nội bộ, IP
//     literal và MỌI địa chỉ phân giải được phải ngoài dải cấm;
//  2. lúc kết nối (Client): net.Dialer.Control kiểm ĐỊA CHỈ THẬT sắp nối (sau phân giải DNS) — tên máy đổi bản ghi DNS
//     sang địa chỉ nội bộ sau khi đăng ký (DNS rebinding) vẫn bị chặn; không proxy, không theo chuyển hướng.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MaxURLLength là độ dài tối đa của URL webhook.
const MaxURLLength = 2048

// ErrBlockedAddress: địa chỉ đích thuộc dải cấm (loopback, nội bộ, link-local, metadata đám mây, …).
var ErrBlockedAddress = errors.New("webhook: địa chỉ đích thuộc dải cấm")

// blockedPrefixes là các dải không bao giờ được làm đích webhook (IPv4-mapped IPv6 được bỏ ánh xạ trước khi so).
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24",
	"192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"224.0.0.0/4", "240.0.0.0/4",
	"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16",
	"fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// Hậu tố tên máy nội bộ — không phân giải công khai, không bao giờ là đích.
var internalSuffixes = []string{".localhost", ".local", ".internal", ".lan", ".home.arpa", ".intranet", ".corp", ".svc", ".cluster.local"}

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// Resolver phân giải tên máy (net.DefaultResolver; kiểm thử thay).
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Guard là chính sách đích webhook.
type Guard struct {
	// AllowHosts (tùy chọn, AG_WEBHOOK_ALLOW_HOSTS): nếu có, tên máy phải bằng một mục hoặc là tên con của nó.
	AllowHosts []string
	// AllowNets: dải được MIỄN chặn — CHỈ dev/test (httptest trên 127.0.0.1); FromEnv từ chối ở production.
	AllowNets []netip.Prefix
	// AllowAnyPort: cho mọi cổng (dev/test). Mặc định chỉ 443 và 8443.
	AllowAnyPort bool
	// Resolver: nil → net.DefaultResolver.
	Resolver Resolver
}

func (g Guard) resolver() Resolver {
	if g.Resolver == nil {
		return net.DefaultResolver
	}
	return g.Resolver
}

// CheckIP trả ErrBlockedAddress khi ip thuộc dải cấm và không thuộc AllowNets.
func (g Guard) CheckIP(ip netip.Addr) error {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" {
		return ErrBlockedAddress
	}
	for _, p := range g.AllowNets {
		if p.Contains(ip) {
			return nil
		}
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return ErrBlockedAddress
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return ErrBlockedAddress
		}
	}
	if ip.Is4() && ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return ErrBlockedAddress
	}
	return nil
}

// RejectedError là lý do URL bị từ chối (detail cho problem — không chứa giá trị nhạy cảm).
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return "webhook: URL bị từ chối: " + e.Reason }

func rejected(format string, a ...any) error {
	return &RejectedError{Reason: fmt.Sprintf(format, a...)}
}

// CheckStatic kiểm URL không cần mạng: https, không userinfo/fragment, cổng, tên máy hoặc IP literal.
func (g Guard) CheckStatic(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > MaxURLLength || strings.TrimSpace(raw) != raw {
		return nil, rejected("URL rỗng, có khoảng trắng hoặc dài quá %d ký tự", MaxURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, rejected("URL sai cú pháp")
	}
	switch {
	case u.Scheme != "https":
		return nil, rejected("chỉ nhận https")
	case u.Opaque != "" || u.Host == "":
		return nil, rejected("thiếu tên máy")
	case u.User != nil:
		return nil, rejected("không được có thông tin đăng nhập trong URL")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return nil, rejected("không được có fragment")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 || (!g.AllowAnyPort && n != 443 && n != 8443) {
			return nil, rejected("cổng chỉ 443 hoặc 8443")
		}
	}
	host := strings.ToLower(u.Hostname())
	if ip, err := netip.ParseAddr(host); err == nil {
		if g.CheckIP(ip) != nil {
			return nil, rejected("địa chỉ IP thuộc dải cấm (nội bộ, loopback, link-local, metadata)")
		}
		if len(g.AllowHosts) > 0 {
			return nil, rejected("chỉ nhận tên máy trong danh sách cho phép")
		}
		return u, nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, rejected("tên máy nội bộ")
	}
	if !hostRe.MatchString(host) || len(host) > 253 {
		return nil, rejected("tên máy không hợp lệ (cần tên miền đầy đủ)")
	}
	for _, s := range internalSuffixes {
		if strings.HasSuffix(host, s) {
			return nil, rejected("tên máy nội bộ (%s)", s)
		}
	}
	if len(g.AllowHosts) > 0 && !hostAllowed(host, g.AllowHosts) {
		return nil, rejected("tên máy ngoài danh sách cho phép")
	}
	return u, nil
}

func hostAllowed(host string, allow []string) bool {
	for _, a := range allow {
		a = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a), "."))
		if a != "" && (host == a || strings.HasSuffix(host, "."+a)) {
			return true
		}
	}
	return false
}

// ValidateURL kiểm tĩnh rồi phân giải tên máy: mọi địa chỉ phải ngoài dải cấm (đăng ký). Không thay thế kiểm lúc
// kết nối — bản ghi DNS có thể đổi sau đó.
func (g Guard) ValidateURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := g.CheckStatic(raw)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	if _, err := netip.ParseAddr(host); err == nil {
		return u, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := g.resolver().LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return nil, rejected("không phân giải được tên máy")
	}
	for _, a := range addrs {
		if g.CheckIP(a) != nil {
			return nil, rejected("tên máy phân giải tới địa chỉ thuộc dải cấm")
		}
	}
	return u, nil
}

// control kiểm địa chỉ THẬT sắp nối (sau phân giải) — chống DNS rebinding.
func (g Guard) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return ErrBlockedAddress
	}
	return g.CheckIP(ap.Addr())
}

// ClientOptions là tham số client giao webhook.
type ClientOptions struct {
	Timeout time.Duration // toàn lời gọi (mặc định 10 giây)
	TLS     *tls.Config   // nil → hệ thống; kiểm thử đặt RootCAs của httptest
}

// Client dựng http.Client giao webhook: Dialer.Control kiểm từng địa chỉ nối, không proxy, không chuyển hướng,
// TLS ≥ 1.2, giới hạn thời gian và kích thước header.
func (g Guard) Client(o ClientOptions) *http.Client {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.TLS != nil {
		tc = o.TLS.Clone()
		if tc.MinVersion < tls.VersionTLS12 {
			tc.MinVersion = tls.VersionTLS12
		}
	}
	d := &net.Dialer{Timeout: 5 * time.Second, Control: g.control}
	tr := &http.Transport{
		Proxy:                  nil, // không bao giờ qua proxy: Control phải thấy địa chỉ đích thật
		DialContext:            d.DialContext,
		TLSClientConfig:        tc,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  o.Timeout,
		MaxResponseHeaderBytes: 16 << 10,
		MaxIdleConnsPerHost:    2,
		IdleConnTimeout:        30 * time.Second,
		ForceAttemptHTTP2:      true,
	}
	return &http.Client{Transport: tr, Timeout: o.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
