package extension

import (
	"testing"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

func TestNavigationRegistrationKeepsRoleDefaultsAndOwnsProfiles(t *testing.T) {
	policy, err := NavigationPolicy()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ role, profile, want string }{
		{"kn_tinh", "ke_hoach_tai_chinh", "/kn/khtc/ky-bao-cao"},
		{"kn_tinh", "trong_trot", "/kn/bao-cao-khtc"},
		{"kn_tinh", "chan_nuoi_thuy_san", "/kn/bao-cao-khtc"},
		{"kn_tinh", "thong_tin_truyen_thong", "/kn/bao-cao-khtc"},
		{"kn_tinh", "hanh_chinh_tong_hop", "/kn/bao-cao-khtc"},
		{"kn_tinh", "generic", "/kn/dashboard"}, {"kn_tw", "ke_hoach_tai_chinh", "/kn/dashboard"},
		{"kn_xa", "ke_hoach_tai_chinh", "/kn/nhiem-vu/cong-viec"}, {"admin", "ke_hoach_tai_chinh", "/kn/tong-quan"},
	} {
		got, err := policy(partyport.NavigationRequest{Audience: tc.role, Department: partyport.Department{Code: "custom-code", MenuProfile: tc.profile}})
		if err != nil || len(got) == 0 || got[0] != tc.want {
			t.Fatalf("%s/%s:%v %v", tc.role, tc.profile, got, err)
		}
	}
}
