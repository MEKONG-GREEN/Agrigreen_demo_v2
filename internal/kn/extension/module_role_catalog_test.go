package extension_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

var updateRoleCatalogGolden = flag.Bool("update-role-catalog", false, "ghi lại testdata/role_catalog.golden.json")

// roleCatalogSnapshot là chính sách quan sát được của catalog, không phụ thuộc thứ tự collection (thứ tự thuộc #57).
type roleCatalogSnapshot struct {
	Managed   []string            `json:"managed"`
	MFA       []string            `json:"mfa"`
	LoginTOTP []string            `json:"login_totp"`
	OrgKinds  map[string][]string `json:"org_kinds"`
	Grantable map[string][]string `json:"grantable"` // theo từng vai người gọi
}

// probeCallers: ba vai quản trị tài khoản, các vai KN (không được cấp gì) và vai nghiệp vụ native mẫu.
var probeCallers = []string{"platform_admin", accport.RoleAccountAdmin, accport.RoleAccountAdminProvince,
	extension.RealmRoleKNNationalAdmin, extension.RealmRoleKNProvinceAdmin, extension.RealmRoleKNOfficer, extension.RealmRoleKNUser,
	"coop_manager", "registry_admin"}

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	if out == nil {
		out = []string{}
	}
	return out
}

func snapshot(c *accport.RoleCatalog) roleCatalogSnapshot {
	s := roleCatalogSnapshot{Managed: sorted(c.ManagedRoles()), MFA: sorted(c.MFARoles()), LoginTOTP: sorted(c.LoginTOTPRoles()),
		OrgKinds: map[string][]string{}, Grantable: map[string][]string{}}
	for role, kinds := range c.RoleOrgKinds() {
		s.OrgKinds[role] = sorted(kinds)
	}
	for _, caller := range probeCallers {
		s.Grantable[caller] = sorted(c.Grantable([]string{caller}))
	}
	return s
}

func knCatalog(t *testing.T) *accport.RoleCatalog {
	t.Helper()
	c, err := extension.RoleCatalog(accport.NativeRoleCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Golden "trước/sau": bridge legacy (trước) và nền native + khai báo KN (sau) cho cùng một snapshot chính sách, và
// snapshot này khớp tệp golden đã chốt.
func TestKNRoleCatalogParityWithLegacyGolden(t *testing.T) {
	before, after := snapshot(accport.LegacyRoleCatalog()), snapshot(knCatalog(t))
	b, _ := json.MarshalIndent(before, "", "  ")
	a, _ := json.MarshalIndent(after, "", "  ")
	if string(a) != string(b) {
		t.Fatalf("KN owner catalog differs from legacy bridge\nlegacy:\n%s\nkn:\n%s", b, a)
	}
	path := filepath.Join("testdata", "role_catalog.golden.json")
	if *updateRoleCatalogGolden {
		if err := os.WriteFile(path, append(a, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(a)+"\n" {
		t.Fatalf("effective role catalog changed vs %s:\n%s", path, a)
	}
}

// Truy vấn từng vai (kể cả vai ngoài catalog) cho cùng kết quả ở legacy và catalog KN.
func TestKNRoleCatalogPerRoleParity(t *testing.T) {
	legacy, kn := accport.LegacyRoleCatalog(), knCatalog(t)
	roles := append(legacy.ManagedRoles(), accport.AdminRoles...)
	roles = append(roles, accport.MFARoles...)
	roles = append(roles, accport.RoleFarmer, accport.RoleLoginTOTP, "offline_access", "cgh_sync", "kn_unknown", "")
	for _, role := range roles {
		one := []string{role}
		if legacy.IsManaged(role) != kn.IsManaged(role) || legacy.NeedsMFA(one) != kn.NeedsMFA(one) ||
			legacy.NeedsLoginTOTP(one) != kn.NeedsLoginTOTP(one) || !slices.Equal(legacy.OrgKinds(role), kn.OrgKinds(role)) ||
			!slices.Equal(sorted(legacy.Grantable(one)), sorted(kn.Grantable(one))) {
			t.Fatalf("policy differs for %q", role)
		}
	}
	// Hợp nhiều vai của người gọi.
	multi := []string{accport.RoleAccountAdminProvince, extension.RealmRoleKNProvinceAdmin}
	if !slices.Equal(sorted(legacy.Grantable(multi)), sorted(kn.Grantable(multi))) {
		t.Fatal("union of caller roles differs")
	}
}

// D24 + không tăng quyền: bốn vai KN cần TOTP đăng nhập nhưng không là vai [MFA], không cấp được vai nào; quản trị tỉnh
// chỉ cấp được kn_officer trong số vai KN.
func TestKNRoleCatalogDeclarations(t *testing.T) {
	kn := knCatalog(t)
	want := []string{extension.RealmRoleKNNationalAdmin, extension.RealmRoleKNProvinceAdmin, extension.RealmRoleKNOfficer, extension.RealmRoleKNUser}
	if !slices.Equal(want, accport.KNOfficerRoles) {
		t.Fatal("legacy KNOfficerRoles changed; re-check owner declarations")
	}
	for _, r := range extension.RoleRegistrations() {
		if r.MFARole || !r.LoginTOTP || slices.Contains(accport.AdminRoles, r.Role) {
			t.Fatalf("registration flags changed: %+v", r)
		}
	}
	for _, role := range want {
		if !kn.IsManaged(role) || !kn.NeedsLoginTOTP([]string{role}) || kn.NeedsMFA([]string{role}) ||
			len(kn.Grantable([]string{role})) != 0 || slices.Contains(kn.Grantable([]string{"platform_admin"}), "platform_admin") {
			t.Fatalf("KN role policy wrong: %s", role)
		}
	}
	prov := kn.Grantable([]string{accport.RoleAccountAdminProvince})
	if !slices.Contains(prov, extension.RealmRoleKNOfficer) || slices.Contains(prov, extension.RealmRoleKNProvinceAdmin) ||
		slices.Contains(prov, extension.RealmRoleKNNationalAdmin) || slices.Contains(prov, extension.RealmRoleKNUser) {
		t.Fatalf("province grantor policy wrong: %v", prov)
	}
	if len(kn.OrgKinds(extension.RealmRoleKNUser)) != 0 {
		t.Fatal("kn_user gained an organization-kind restriction")
	}
	// Thứ tự: ExtendRoleCatalog nối vai mới vào cuối — khác bridge; đối chiếu thứ tự thuộc #57, ở đây chỉ chốt hành vi.
	if got := kn.ManagedRoles(); !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("composition order: %v", got)
	}
}

func TestKNRoleCatalogFailsClosed(t *testing.T) {
	for name, base := range map[string]*accport.RoleCatalog{
		"nil": nil, "zero": {}, "legacy-already-has-kn": accport.LegacyRoleCatalog(),
	} {
		if c, err := extension.RoleCatalog(base); c != nil || !errors.Is(err, extension.ErrKNRoleCatalog) {
			t.Fatalf("%s: expected configuration error, got %v %v", name, c, err)
		}
	}
	kn := knCatalog(t)
	if c, err := extension.RoleCatalog(kn); c != nil || !errors.Is(err, accport.ErrRoleCatalog) {
		t.Fatalf("double registration accepted: %v", err)
	}
}

func TestKNRoleCatalogDefensiveCopies(t *testing.T) {
	native := accport.NativeRoleCatalog()
	before := snapshot(native)
	regs := extension.RoleRegistrations()
	regs[2].Grantors[0], regs[0].OrgKinds[0] = "mutated", "mutated"
	again := extension.RoleRegistrations()
	if again[2].Grantors[0] != "platform_admin" || again[0].OrgKinds[0] != "ttkn_national" {
		t.Fatal("registrations share mutable state between calls")
	}
	kn, err := extension.RoleCatalog(native)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(snapshot(native))
	if string(b1) != string(b2) {
		t.Fatal("composition mutated the native base")
	}
	kinds := kn.RoleOrgKinds()
	kinds[extension.RealmRoleKNOfficer][0] = "cooperative"
	if !slices.Equal(kn.OrgKinds(extension.RealmRoleKNOfficer), []string{"extension_team"}) {
		t.Fatal("catalog exposes mutable organization kinds")
	}
	other, _ := extension.RoleCatalog(accport.NativeRoleCatalog())
	if other == kn || !slices.Equal(other.ManagedRoles(), kn.ManagedRoles()) {
		t.Fatal("instances are not independent snapshots")
	}
}
