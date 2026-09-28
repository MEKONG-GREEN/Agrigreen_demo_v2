package port_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// Ma trận cấp vai (KN US-ADM-01): quản trị tỉnh chỉ cấp kn_officer/coop_manager;
// không ai cấp được platform_admin qua API; account_admin không tự nhân bản.
func TestGrantMatrix(t *testing.T) {
	prov := port.Grantable([]string{port.RoleAccountAdminProvince})
	slices.Sort(prov)
	if !slices.Equal(prov, []string{"coop_manager", "kn_officer"}) {
		t.Fatalf("quản trị tỉnh: %v", prov)
	}
	for _, r := range []string{"kn_national_admin", "kn_province_admin", port.RoleAccountAdmin, "platform_admin"} {
		if slices.Contains(prov, r) {
			t.Errorf("quản trị tỉnh không được cấp %s", r)
		}
	}
	nat := port.Grantable([]string{port.RoleAccountAdmin})
	for _, r := range []string{port.RoleAccountAdmin, "param_approver", "vault_eraser", "platform_admin"} {
		if slices.Contains(nat, r) {
			t.Errorf("account_admin không được cấp %s", r)
		}
	}
	if !slices.Contains(nat, port.RoleAccountAdminProvince) || !slices.Contains(nat, "kn_national_admin") {
		t.Errorf("account_admin phải cấp được quản trị tỉnh và kn_national_admin: %v", nat)
	}
	if slices.Contains(port.Grantable([]string{"platform_admin"}), "platform_admin") {
		t.Error("platform_admin chỉ cấp trong console Keycloak")
	}
	// TTKN QG/tỉnh không quản trị tài khoản (KN US-ADM-01 AC-4).
	if g := port.Grantable([]string{"kn_national_admin", "kn_province_admin"}); len(g) != 0 {
		t.Errorf("TTKN không có quyền cấp vai: %v", g)
	}
	for _, g := range port.Grants {
		for _, r := range g {
			if !port.IsManaged(r) {
				t.Errorf("vai %s trong ma trận không thuộc ManagedRoles", r)
			}
		}
	}
}

// Mọi vai module cấp/kiểm phải có trong realm dev; MFARoles khớp đúng các vai
// ghi [MFA] trong realm.
func TestRolesExistInRealm(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose", "keycloak", "agrigreen-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm struct {
		Roles struct {
			Realm []struct{ Name, Description string } `json:"realm"`
		} `json:"roles"`
		Clients []struct {
			ClientID                  string `json:"clientId"`
			PublicClient              bool   `json:"publicClient"`
			ServiceAccountsEnabled    bool   `json:"serviceAccountsEnabled"`
			DirectAccessGrantsEnabled bool   `json:"directAccessGrantsEnabled"`
			Secret                    string `json:"secret"`
		} `json:"clients"`
		Users []struct {
			ServiceAccountClientID string              `json:"serviceAccountClientId"`
			ClientRoles            map[string][]string `json:"clientRoles"`
		} `json:"users"`
	}
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatal(err)
	}
	have := map[string]string{}
	var mfa []string
	for _, r := range realm.Roles.Realm {
		have[r.Name] = r.Description
		if strings.Contains(r.Description, "[MFA]") {
			mfa = append(mfa, r.Name)
		}
	}
	for _, r := range append(slices.Clone(port.ManagedRoles), port.MFARoles...) {
		if _, ok := have[r]; !ok {
			t.Errorf("realm thiếu vai %s", r)
		}
	}
	want := slices.Clone(port.MFARoles)
	slices.Sort(want)
	slices.Sort(mfa)
	if !slices.Equal(want, mfa) {
		t.Errorf("MFARoles %v khác vai [MFA] trong realm %v", want, mfa)
	}
	var svc, seed bool
	for _, c := range realm.Clients {
		switch c.ClientID {
		case "agrigreen-accounts":
			// Bí mật không nằm trong repo: placeholder thay bằng biến môi trường lúc nhập realm.
			svc = !c.PublicClient && c.ServiceAccountsEnabled && !c.DirectAccessGrantsEnabled && c.Secret == "${AG_KC_CLIENT_SECRET}"
		case "agrigreen-seed":
			seed = c.PublicClient && c.DirectAccessGrantsEnabled
		default:
			if c.DirectAccessGrantsEnabled {
				t.Errorf("client %s không được bật direct access grants", c.ClientID)
			}
		}
	}
	if !svc || !seed {
		t.Fatalf("thiếu client agrigreen-accounts (dịch vụ, bí mật từ env) hoặc agrigreen-seed (chỉ dev): %v %v", svc, seed)
	}
	ok := false
	for _, u := range realm.Users {
		if u.ServiceAccountClientID == "agrigreen-accounts" {
			got := slices.Clone(u.ClientRoles["realm-management"])
			slices.Sort(got)
			ok = slices.Equal(got, []string{"manage-users", "query-users", "view-users"})
		}
	}
	if !ok {
		t.Fatal("tài khoản dịch vụ agrigreen-accounts phải có đúng manage-users, view-users, query-users")
	}
}
