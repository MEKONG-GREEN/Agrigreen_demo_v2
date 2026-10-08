package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeResolver map[string][]string

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	ss, ok := f[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	out := make([]netip.Addr, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

// W5.7 (R-INT-07): chặn SSRF — https, không userinfo/fragment, cổng 443/8443, không tên nội bộ, không IP nội bộ (kể
// cả dạng IPv4-mapped/NAT64/6to4), mọi địa chỉ phân giải phải công khai; danh sách tên miền cho phép.
func TestGuardRejectsSSRFTargets_W57(t *testing.T) {
	g := Guard{Resolver: fakeResolver{
		"hooks.partner.vn":   {"203.113.1.10"},
		"rebind.partner.vn":  {"203.113.1.11", "10.0.0.5"},
		"meta.partner.vn":    {"169.254.169.254"},
		"v6.partner.vn":      {"2001:4860:4860::8888"},
		"v6local.partner.vn": {"fd00::1"},
	}}
	ctx := context.Background()
	bad := map[string]string{
		"http://hooks.partner.vn/x":                                     "https",
		"https://user:pw@hooks.partner.vn/x":                            "đăng nhập",
		"https://hooks.partner.vn/x#frag":                               "fragment",
		"https://hooks.partner.vn:8080/x":                               "cổng",
		"https://localhost/x":                                           "nội bộ",
		"https://api.svc.cluster.local/x":                               "nội bộ",
		"https://metadata.google.internal/x":                            "nội bộ",
		"https://127.0.0.1/x":                                           "dải cấm",
		"https://10.1.2.3/x":                                            "dải cấm",
		"https://172.20.0.1/x":                                          "dải cấm",
		"https://192.168.1.1/x":                                         "dải cấm",
		"https://169.254.169.254/latest/meta-data":                      "dải cấm",
		"https://100.64.0.1/x":                                          "dải cấm",
		"https://0.0.0.0/x":                                             "dải cấm",
		"https://[::1]/x":                                               "dải cấm",
		"https://[::ffff:127.0.0.1]/x":                                  "dải cấm",
		"https://[fe80::1]/x":                                           "dải cấm",
		"https://[fd12::1]/x":                                           "dải cấm",
		"https://[64:ff9b::a00:1]/x":                                    "dải cấm",
		"https://[2002:a00:1::]/x":                                      "dải cấm",
		"https://intranet/x":                                            "tên miền đầy đủ",
		"https://rebind.partner.vn/x":                                   "dải cấm",
		"https://meta.partner.vn/x":                                     "dải cấm",
		"https://v6local.partner.vn/x":                                  "dải cấm",
		"https://unknown.partner.vn/x":                                  "phân giải",
		"https://hooks.partner.vn/" + strings.Repeat("a", MaxURLLength): "dài",
		" https://hooks.partner.vn/x":                                   "khoảng trắng",
	}
	for raw, want := range bad {
		_, err := g.ValidateURL(ctx, raw)
		var re *RejectedError
		if !errors.As(err, &re) || !strings.Contains(re.Reason, want) {
			t.Errorf("%q: muốn từ chối (%s), nhận %v", raw, want, err)
		}
	}
	for _, ok := range []string{"https://hooks.partner.vn/agrigreen?k=1", "https://hooks.partner.vn:8443/x", "https://v6.partner.vn/x",
		"https://203.113.1.10/x"} {
		if _, err := g.ValidateURL(ctx, ok); err != nil {
			t.Errorf("%q: muốn nhận, nhận %v", ok, err)
		}
	}
	allow := g
	allow.AllowHosts = []string{"partner.vn"}
	if _, err := allow.ValidateURL(ctx, "https://hooks.partner.vn/x"); err != nil {
		t.Fatalf("tên con của tên miền cho phép: %v", err)
	}
	for _, raw := range []string{"https://evil.example.com/x", "https://notpartner.vn/x", "https://203.113.1.10/x"} {
		if _, err := allow.CheckStatic(raw); err == nil {
			t.Fatalf("%q ngoài danh sách cho phép phải bị từ chối", raw)
		}
	}
}

// W5.7: kiểm lúc KẾT NỐI (chống DNS rebinding) — địa chỉ thật sắp nối bị chặn dù tên máy đã qua lúc đăng ký;
// AllowNets (dev/test) miễn đúng dải được khai.
func TestGuardDialControlBlocksRebinding_W57(t *testing.T) {
	g := Guard{}
	for _, a := range []string{"127.0.0.1:443", "10.0.0.1:443", "[::1]:443", "169.254.169.254:80", "[::ffff:192.168.0.1]:443", "bad"} {
		if err := g.control("tcp", a, nil); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("%s: muốn ErrBlockedAddress, nhận %v", a, err)
		}
	}
	if err := g.control("tcp", "203.113.1.10:443", nil); err != nil {
		t.Fatalf("địa chỉ công khai: %v", err)
	}
	dev := Guard{AllowNets: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	if err := dev.control("tcp", "127.0.0.1:8443", nil); err != nil {
		t.Fatalf("AllowNets miễn 127.0.0.0/8: %v", err)
	}
	if err := dev.control("tcp", "10.0.0.1:443", nil); err == nil {
		t.Fatal("AllowNets không miễn dải khác")
	}
}

// W5.7 (ADR-0033 §4): chữ ký = HMAC-SHA256(bí mật, "<t>.<thân>"); hai v1 trong chồng lấn xoay; sai thân, sai bí mật,
// quá dung sai → từ chối.
func TestSignVerify_W57(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	body := []byte(`{"feed":"admin_units","cursor":"c1","occurred_at":"2026-10-09T00:00:00Z"}`)
	newS, oldS := []byte("new-secret-0123456789abcdef0123456"), []byte("old-secret-0123456789abcdef0123456")
	h := Sign([][]byte{newS, oldS}, now, body)
	if !strings.HasPrefix(h, "t=1790000000,v1=") || strings.Count(h, "v1=") != 2 {
		t.Fatalf("header: %s", h)
	}
	if err := Verify(h, newS, body, now.Add(time.Minute), 0); err != nil {
		t.Fatalf("bí mật mới: %v", err)
	}
	if err := Verify(h, oldS, body, now, 0); err != nil {
		t.Fatalf("bí mật cũ trong chồng lấn: %v", err)
	}
	if err := Verify(h, newS, append(body, ' '), now, 0); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("thân đổi: %v", err)
	}
	if err := Verify(h, []byte("other"), body, now, 0); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("bí mật khác: %v", err)
	}
	if err := Verify(h, newS, body, now.Add(6*time.Minute), 0); !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("quá dung sai: %v", err)
	}
	for _, bad := range []string{"", "v1=abc", "t=x,v1=abc", "t=1790000000"} {
		if err := Verify(bad, newS, body, now, 0); !errors.Is(err, ErrSignatureMissing) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	// Bí mật dẫn xuất: tất định theo (khóa chủ, webhook, phiên bản, nonce), khác khi bất kỳ thành phần đổi.
	k, err := NewKeyring(2, map[int][]byte{1: []byte(strings.Repeat("a", 32)), 2: []byte(strings.Repeat("b", 32))})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	s1, _ := k.Secret(2, id, 1, "00112233445566778899aabbccddeeff")
	s1b, _ := k.Secret(2, id, 1, "00112233445566778899aabbccddeeff")
	s2, _ := k.Secret(2, id, 2, "00112233445566778899aabbccddeeff")
	s3, _ := k.Secret(1, id, 1, "00112233445566778899aabbccddeeff")
	if string(s1) != string(s1b) || string(s1) == string(s2) || string(s1) == string(s3) || len(s1) != 32 {
		t.Fatal("dẫn xuất bí mật")
	}
	if _, ok := k.Secret(9, id, 1, "x"); ok {
		t.Fatal("khóa chủ không có → ok = false")
	}
	if raw, err := ParseSecret(FormatSecret(s1)); err != nil || string(raw) != string(s1) {
		t.Fatalf("whsec_ khứ hồi: %v", err)
	}
	if _, err := NewKeyring(1, map[int][]byte{1: []byte("short")}); err == nil {
		t.Fatal("khóa < 32 byte phải lỗi")
	}
	env := map[string]string{EnvKeyPrefix + "1": strings.Repeat("x", 32), EnvKeyPrefix + "3": strings.Repeat("y", 32)}
	kr, err := KeyringFromEnv(func(k string) string { return env[k] }, []string{EnvKeyPrefix + "1=…", EnvKeyPrefix + "3=…", "PATH=/bin"})
	if err != nil || kr == nil || kr.Current != 3 {
		t.Fatalf("KeyringFromEnv: %+v %v", kr, err)
	}
	if kr, err := KeyringFromEnv(func(string) string { return "" }, nil); kr != nil || err != nil {
		t.Fatal("không đặt biến → nil, nil")
	}
}

// W5.7 (R-INT-09): closed → open sau đúng N lỗi liên tiếp; thành công xen giữa đặt lại bộ đếm; open → Deny tới hạn →
// Probe; thử lỗi → mở lại với thời gian chờ gấp đôi (trần MaxCooldown); thử thành công → closed.
func TestBreakerTransitions_W57(t *testing.T) {
	p := Policy{Cooldown: time.Minute, MaxCooldown: 3 * time.Minute}
	t0 := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	b := Breaker{State: StateClosed}
	for i := 1; i <= 2; i++ {
		if b.Failure(t0, ErrClassHTTPStatus, 3, p) || b.State != StateClosed {
			t.Fatalf("lỗi %d < N: còn đóng", i)
		}
	}
	b.Success(t0)
	if b.Failures != 0 || b.State != StateClosed {
		t.Fatal("thành công đặt lại bộ đếm")
	}
	for i := 1; i <= 2; i++ {
		b.Failure(t0, ErrClassTimeout, 3, p)
	}
	if b.Decide(t0) != Allow {
		t.Fatal("đóng → Allow")
	}
	if !b.Failure(t0, ErrClassTimeout, 3, p) || b.State != StateOpen || !b.NextProbeAt.Equal(t0.Add(time.Minute)) || b.LastError != ErrClassTimeout {
		t.Fatalf("lỗi thứ N → open, thử sau 1 phút: %+v", b)
	}
	if b.Decide(t0.Add(59*time.Second)) != Deny || b.Decide(t0.Add(time.Minute)) != Probe {
		t.Fatal("open: Deny trước hạn, Probe khi tới hạn")
	}
	t1 := t0.Add(time.Minute)
	b.StartProbe(t1, p)
	if b.State != StateHalfOpen || b.Decide(t1) != Deny {
		t.Fatal("half_open: một lần thử đang chạy → Deny")
	}
	if !b.Failure(t1, ErrClassConnect, 3, p) || b.State != StateOpen || !b.NextProbeAt.Equal(t1.Add(2*time.Minute)) {
		t.Fatalf("thử lỗi → mở lại, chờ ×2: %+v", b)
	}
	t2 := t1.Add(2 * time.Minute)
	b.StartProbe(t2, p)
	b.Failure(t2, ErrClassConnect, 3, p)
	if !b.NextProbeAt.Equal(t2.Add(3 * time.Minute)) {
		t.Fatalf("trần MaxCooldown 3 phút: %v", b.NextProbeAt)
	}
	// Tiến trình thử chết giữa chừng: half_open quá hạn → Probe lại.
	t3 := t2.Add(3 * time.Minute)
	b.StartProbe(t3, p)
	if b.Decide(b.NextProbeAt.Add(time.Second)) != Probe {
		t.Fatal("half_open quá hạn thử → Probe lại")
	}
	if !b.Success(t3) || b.State != StateClosed || b.Failures != 0 || b.OpenedCount != 0 || b.NextProbeAt != nil {
		t.Fatalf("thử thành công → closed: %+v", b)
	}
	b.Failure(t3, "x", 1, p)
	b.Reset()
	if b.State != StateClosed || b.Failures != 0 {
		t.Fatal("Reset thủ công")
	}
}

// W5.7: lùi hàm mũ có nhiễu trong [d/2, d], trần RetryMax.
func TestBackoff_W57(t *testing.T) {
	p := Policy{RetryBase: 10 * time.Second, RetryMax: 80 * time.Second}
	cases := []struct {
		n      int
		j      float64
		lo, hi time.Duration
	}{
		{1, 0, 5 * time.Second, 5 * time.Second},
		{1, 0.999, 5 * time.Second, 10 * time.Second},
		{2, 0, 10 * time.Second, 10 * time.Second},
		{4, 0.5, 40 * time.Second, 80 * time.Second},
		{9, 0.999, 40 * time.Second, 80 * time.Second},
	}
	for _, c := range cases {
		j := c.j
		d := p.Backoff(c.n, func() float64 { return j })
		if d < c.lo || d > c.hi {
			t.Errorf("Backoff(%d, %v) = %v ∉ [%v, %v]", c.n, c.j, d, c.lo, c.hi)
		}
	}
	for range 100 {
		if d := p.Backoff(3, nil); d < 20*time.Second || d > 40*time.Second {
			t.Fatalf("nhiễu ngẫu nhiên ngoài [d/2, d]: %v", d)
		}
	}
	if d := (Policy{}).CooldownFor(20); d != DefaultMaxCooldown {
		t.Fatalf("trần mặc định: %v", d)
	}
}

// W5.7: client giao THẬT bị chặn khi địa chỉ nối là loopback (Control), không theo chuyển hướng.
func TestGuardClientBlocksLoopbackDial_W57(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/", http.StatusFound)
	}))
	defer srv.Close()
	_, err := Guard{AllowAnyPort: true}.Client(ClientOptions{Timeout: 2 * time.Second}).Get(srv.URL)
	if !errors.Is(err, ErrBlockedAddress) || classify(err) != ErrClassBlockedAddr {
		t.Fatalf("nối 127.0.0.1 phải bị chặn: %v", err)
	}
	dev := Guard{AllowAnyPort: true, AllowNets: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	resp, err := dev.Client(ClientOptions{}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("không theo chuyển hướng: %d", resp.StatusCode)
	}
}
