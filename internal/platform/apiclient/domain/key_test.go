package domain

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

func pepper(b byte) []byte { return bytes.Repeat([]byte{b}, port.MinPepperBytes) }

// ADR-0022 mục 4: khóa "agk_<12 base32>_<43 base64url>", ParseKey nhận đúng prefix.
func TestGenerateKeyFormat_ADR0022(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		g, err := GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		p, ok := port.ParseKey(g.Full)
		if !ok || p != g.Prefix || !strings.HasPrefix(g.Full, "agk_"+g.Prefix+"_") {
			t.Fatalf("khóa sai dạng: prefix %q", g.Prefix)
		}
		if seen[g.Prefix] {
			t.Fatal("prefix trùng")
		}
		seen[g.Prefix] = true
	}
}

// ADR-0022 mục 4: HMAC theo phiên bản pepper; xoay pepper không làm hỏng khóa cũ; so sai → false.
func TestPepperVersionsAndRotation_ADR0022(t *testing.T) {
	p1, err := NewPeppers(1, map[int][]byte{1: pepper('a')})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := GenerateKey(nil)
	mac1, _ := p1.MAC(1, g.Full)
	if len(mac1) != 32 || bytes.Contains(mac1, []byte(g.Full)) {
		t.Fatal("HMAC phải 32 byte và không chứa khóa")
	}
	p2, err := NewPeppers(2, map[int][]byte{1: pepper('a'), 2: pepper('b')})
	if err != nil {
		t.Fatal(err)
	}
	if !p2.Verify(1, g.Full, mac1) {
		t.Fatal("khóa cũ (pepper v1) phải còn kiểm được sau khi thêm v2")
	}
	mac2, _ := p2.MAC(p2.Current, g.Full)
	if bytes.Equal(mac1, mac2) {
		t.Fatal("pepper khác phải cho HMAC khác")
	}
	if p2.Verify(1, g.Full+"x", mac1) || p2.Verify(3, g.Full, mac1) {
		t.Fatal("khóa sai hoặc phiên bản pepper không có phải bị từ chối")
	}
	if _, err := NewPeppers(1, map[int][]byte{1: []byte("short")}); err == nil {
		t.Fatal("pepper < 32 byte phải bị từ chối")
	}
	if _, err := NewPeppers(2, map[int][]byte{1: pepper('a')}); err == nil {
		t.Fatal("CURRENT không có pepper phải lỗi")
	}
}

func TestPeppersFromEnv_ADR0022(t *testing.T) {
	env := map[string]string{"AG_APIKEY_PEPPER_CURRENT": "2"}
	get := func(k string) string { return env[k] }
	p, err := PeppersFromEnv(get, []string{"AG_APIKEY_PEPPER_V1=" + string(pepper('a')), "AG_APIKEY_PEPPER_V2=" + string(pepper('b')), "OTHER=1"})
	if err != nil || p == nil || p.Current != 2 {
		t.Fatalf("muốn current 2, nhận %v %v", p, err)
	}
	if p, err := PeppersFromEnv(func(string) string { return "" }, nil); p != nil || err != nil {
		t.Fatal("không đặt biến nào → (nil, nil)")
	}
	if _, err := PeppersFromEnv(func(string) string { return "" }, []string{"AG_APIKEY_PEPPER_V1=short"}); err == nil {
		t.Fatal("pepper ngắn phải lỗi khởi động")
	}
	if p, _ := PeppersFromEnv(func(string) string { return "" }, []string{"AG_APIKEY_PEPPER_V1=" + string(pepper('a'))}); p == nil || p.Current != 1 {
		t.Fatal("không đặt CURRENT → phiên bản lớn nhất")
	}
}

// ADR-0022 mục 4: rotated còn dùng được tới grace_until; revoked/expired không bao giờ.
func TestKeyUsable_ADR0022(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	exp := now.Add(port.DefaultKeyLifetime)
	grace := RotateGrace(now, exp)
	if !grace.Equal(now.Add(7 * 24 * time.Hour)) {
		t.Fatalf("hạn chồng lấn mặc định 7 ngày, nhận %v", grace)
	}
	if !KeyUsable(KeyStatusActive, exp, nil, now) || KeyUsable(KeyStatusActive, exp, nil, exp) {
		t.Fatal("active: dùng được tới trước expires_at")
	}
	if !KeyUsable(KeyStatusRotated, exp, &grace, grace.Add(-time.Second)) || KeyUsable(KeyStatusRotated, exp, &grace, grace) {
		t.Fatal("rotated: chỉ trong hạn chồng lấn")
	}
	if KeyUsable(KeyStatusRevoked, exp, nil, now) || KeyUsable(KeyStatusExpired, exp, nil, now) {
		t.Fatal("revoked/expired không dùng được")
	}
	if g := RotateGrace(now, now.Add(time.Hour)); !g.Equal(now.Add(time.Hour)) {
		t.Fatal("hạn chồng lấn không vượt hạn khóa")
	}
}

func TestNormScopesAndPlan_ADR0022(t *testing.T) {
	p, err := dataproduct.Get(port.ProductCollectionVolume)
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := NormScopes(p, []string{"dp:collection_volume:read", "usage:read", "dp:collection_volume:read"})
	if !ok || len(sc) != 2 {
		t.Fatalf("muốn 2 scope, nhận %v", sc)
	}
	if _, ok := NormScopes(p, []string{"dp:hub_capacity:read"}); ok {
		t.Fatal("scope của sản phẩm khác phải bị từ chối")
	}
	if _, ok := NormScopes(p, nil); ok {
		t.Fatal("rỗng phải bị từ chối")
	}
	if f, _ := PlanProblem(port.Plan{RequestsPerMinute: 60, DailyUnits: 10, MonthlyUnits: 5, MaxExportRows: 1}); f != "daily_units" {
		t.Fatalf("daily > monthly phải lỗi, nhận %q", f)
	}
	if f, _ := PlanProblem(DefaultPlan([16]byte{})); f != "" {
		t.Fatalf("gói mặc định phải hợp lệ, nhận %q", f)
	}
	if m, ok := NormAuthMethods([]string{"mtls"}); ok {
		t.Fatalf("mtls chưa hiện thực, nhận %v", m)
	}
}
