package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"Gọi 0912345678 để hỗ trợ":        "Gọi *** để hỗ trợ",
		"SĐT +84 912 345 678":             "SĐT ***",
		"CCCD 079123456789":               "CCCD ***",
		"Hạn 15/09/2026 10:00, xã 30292":  "Hạn 15/09/2026 10:00, xã 30292",
		"Mật độ 3000 con/m2 trên 12,5 ha": "Mật độ 3000 con/m2 trên 12,5 ha",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, muốn %q", in, got, want)
		}
	}
}

// Quy tắc khớp chủ đề: toàn quốc chỉ nhận chủ đề National; đơn vị theo giao phạm vi;
// tổ chức theo org_ref; vai lọc thêm.
func TestAudienceMatches(t *testing.T) {
	org := uuid.New()
	ward := Viewer{Roles: []string{"kn_officer"}, Scope: authz.Scope{Units: []string{"30292"}}, OrgRef: org}
	nat := Viewer{Roles: []string{"kn_national_admin"}, Scope: authz.Scope{All: true}}
	empty := Viewer{Roles: []string{"kn_officer"}}
	cases := []struct {
		a    Audience
		v    Viewer
		want bool
	}{
		{Audience{Units: []string{"30292"}}, ward, true},
		{Audience{Units: []string{"30301"}}, ward, false},
		{Audience{Units: []string{"30292"}}, nat, false},
		{Audience{National: true}, nat, true},
		{Audience{National: true}, empty, false},
		{Audience{Units: []string{"30292"}, Roles: []string{"kn_province_admin"}}, ward, false},
		{Audience{OrgRef: &org}, ward, true},
		{Audience{OrgRef: &org}, nat, false},
	}
	for i, c := range cases {
		if got := c.a.Matches(c.v); got != c.want {
			t.Errorf("trường hợp %d: %v", i, got)
		}
	}
}

func TestNormalizeAndPushData(t *testing.T) {
	r := uuid.New()
	n := Template{Kind: "alert", Title: "  Gọi 0912345678  ", Body: "x", DeepLink: "/documents/" + r.String(), CreatedAt: time.Now()}.For(r)
	if !n.Normalize() || n.Title != "Gọi ***" || n.Severity != "normal" {
		t.Fatalf("Normalize: %+v", n)
	}
	bad := Template{Kind: "alert", Title: "a", DeepLink: "https://ngoai.vn/x"}.For(r)
	if bad.Normalize() {
		t.Fatal("deep_link phải là đường dẫn trong app")
	}
	empty := Template{Kind: "alert", Title: "a", DeepLink: "/x"}.ForAudience(Audience{})
	if empty.Normalize() {
		t.Fatal("chủ đề rỗng phải bị từ chối")
	}
	d := PushMessage{Token: "tok", Kind: "sync_now"}.Data()
	if len(d) != 1 || d["kind"] != "sync_now" {
		t.Fatalf("Data: %v", d)
	}
}
