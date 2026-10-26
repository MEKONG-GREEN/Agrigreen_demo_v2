package port_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func TestNativeRoleCatalogPreservesNativePolicy(t *testing.T) {
	native, legacy := port.NativeRoleCatalog(), port.LegacyRoleCatalog()
	if !native.Valid() {
		t.Fatal("native base is invalid")
	}
	// The legacy application cohort is deliberately absent from this opt-in
	// base. All other existing managed roles must retain their relative order.
	withoutApplication := func(roles []string) []string {
		return slices.DeleteFunc(slices.Clone(roles), func(role string) bool {
			return slices.Contains(port.KNOfficerRoles, role)
		})
	}
	if !slices.Equal(native.ManagedRoles(), withoutApplication(legacy.ManagedRoles())) ||
		!slices.Equal(native.MFARoles(), legacy.MFARoles()) ||
		!slices.Equal(native.LoginTOTPRoles(), withoutApplication(legacy.LoginTOTPRoles())) {
		t.Fatal("native managed/MFA/login policy or ordering changed")
	}
	for _, role := range legacy.ManagedRoles() {
		if slices.Contains(port.KNOfficerRoles, role) {
			if native.IsManaged(role) || native.NeedsMFA([]string{role}) || native.NeedsLoginTOTP([]string{role}) ||
				len(native.OrgKinds(role)) != 0 || len(native.Grantable([]string{role})) != 0 {
				t.Fatalf("application policy remained in native base: %s", role)
			}
			continue
		}
		if !native.IsManaged(role) || native.NeedsMFA([]string{role}) != legacy.NeedsMFA([]string{role}) ||
			native.NeedsLoginTOTP([]string{role}) != legacy.NeedsLoginTOTP([]string{role}) ||
			!slices.Equal(native.OrgKinds(role), legacy.OrgKinds(role)) {
			t.Fatalf("native policy changed: %s", role)
		}
	}
	expectedKinds := legacy.RoleOrgKinds()
	for _, role := range port.KNOfficerRoles {
		delete(expectedKinds, role)
	}
	if !reflect.DeepEqual(native.RoleOrgKinds(), expectedKinds) {
		t.Fatal("native organization-kind policy changed")
	}
	for _, role := range port.AdminRoles {
		if !slices.Equal(native.Grantable([]string{role}), withoutApplication(legacy.Grantable([]string{role}))) {
			t.Fatalf("native grantor policy or ordering changed: %s", role)
		}
	}
	if !slices.Equal(native.Grantable([]string{port.RoleAccountAdminProvince}), []string{"coop_manager"}) ||
		native.IsManaged("platform_admin") || !native.NeedsMFA([]string{"platform_admin"}) ||
		!native.NeedsLoginTOTP([]string{"platform_admin"}) {
		t.Fatal("native administrator or province floor changed")
	}
}

func TestNativeRoleCatalogCompositionRetainsPolicyAndIsolation(t *testing.T) {
	base, other := port.NativeRoleCatalog(), port.NativeRoleCatalog()
	legacy := port.LegacyRoleCatalog()
	before, legacyBefore := base.Clone(), legacy.Clone()
	input := port.RoleRegistration{Role: "fixture_auditor", MFARole: true,
		OrgKinds: []string{"fixture_center"}, Grantors: []string{port.RoleAccountAdmin}}
	catalog, err := port.ExtendRoleCatalog(base, input,
		port.RoleRegistration{Role: "fixture_viewer", LoginTOTP: true, Grantors: []string{"platform_admin"}})
	if err != nil {
		t.Fatal(err)
	}
	input.OrgKinds[0], input.Grantors[0] = "mutated", "platform_admin"
	if !catalog.IsManaged("fixture_auditor") || !catalog.NeedsMFA([]string{"fixture_auditor"}) ||
		!catalog.NeedsLoginTOTP([]string{"fixture_auditor"}) || catalog.NeedsMFA([]string{"fixture_viewer"}) ||
		!catalog.NeedsLoginTOTP([]string{"fixture_viewer"}) ||
		!slices.Equal(catalog.OrgKinds("fixture_auditor"), []string{"fixture_center"}) {
		t.Fatal("application registration or TOTP distinction was not retained")
	}
	if !slices.Contains(catalog.Grantable([]string{port.RoleAccountAdmin}), "fixture_auditor") ||
		slices.Contains(catalog.Grantable([]string{"platform_admin"}), "fixture_auditor") ||
		!slices.Contains(catalog.Grantable([]string{"platform_admin"}), "fixture_viewer") ||
		slices.Contains(catalog.Grantable([]string{port.RoleAccountAdmin}), "fixture_viewer") ||
		len(catalog.Grantable([]string{"fixture_auditor", "fixture_viewer"})) != 0 ||
		!slices.Equal(catalog.Grantable([]string{port.RoleAccountAdminProvince}), []string{"coop_manager"}) {
		t.Fatal("registration created or implicitly expanded administrator privileges")
	}
	for _, role := range base.ManagedRoles() {
		if !catalog.IsManaged(role) || !slices.Equal(catalog.OrgKinds(role), base.OrgKinds(role)) ||
			catalog.NeedsMFA([]string{role}) != base.NeedsMFA([]string{role}) ||
			catalog.NeedsLoginTOTP([]string{role}) != base.NeedsLoginTOTP([]string{role}) {
			t.Fatalf("composition changed native policy: %s", role)
		}
	}
	for _, role := range base.MFARoles() {
		if !catalog.NeedsMFA([]string{role}) || !catalog.NeedsLoginTOTP([]string{role}) {
			t.Fatalf("composition removed a native MFA floor: %s", role)
		}
	}
	for _, role := range []string{port.RoleAccountAdmin, "param_approver", "vault_eraser", "platform_admin"} {
		if slices.Contains(catalog.Grantable([]string{port.RoleAccountAdmin}), role) {
			t.Fatalf("composition weakened account-admin grant restriction: %s", role)
		}
	}
	registeredBefore := catalog.Clone()
	collections := [][]string{catalog.ManagedRoles(), catalog.MFARoles(), catalog.LoginTOTPRoles(),
		catalog.OrgKinds("fixture_auditor"), catalog.OrgKinds(port.RoleAccountAdminProvince),
		catalog.Grantable([]string{port.RoleAccountAdmin})}
	for _, values := range collections {
		values[0] = "mutated"
	}
	kinds := catalog.RoleOrgKinds()
	for role, values := range kinds {
		values[0] = "mutated"
		delete(kinds, role)
	}
	if !reflect.DeepEqual(catalog, registeredBefore) || !reflect.DeepEqual(base, before) ||
		!reflect.DeepEqual(other, before) || !reflect.DeepEqual(legacy, legacyBefore) ||
		!reflect.DeepEqual(port.NativeRoleCatalog(), before) || !reflect.DeepEqual(port.LegacyRoleCatalog(), legacyBefore) {
		t.Fatal("catalog access/registration mutated an instance or global compatibility policy")
	}
	// Nil remains the legacy bridge, not an implicit native rollout.
	nilBase, err := port.ExtendRoleCatalog(nil)
	if err != nil || !reflect.DeepEqual(nilBase, legacyBefore) {
		t.Fatal("nil extension changed the compatibility default", err)
	}
}

func TestNativeRoleCatalogRejectsPrivilegeChangesAtomically(t *testing.T) {
	base := port.NativeRoleCatalog()
	before := base.Clone()
	cases := []port.RoleRegistration{
		{Role: "coop_manager"}, {Role: port.RoleAccountAdmin},
		{Role: "platform_admin"}, {Role: port.RoleFarmer}, {Role: port.RoleLoginTOTP},
		{Role: "offline_access"}, {Role: "uma_authorization"}, {Role: "cgh_sync"},
		{Role: "default-roles-agrigreen"}, {Role: "service-account-test"},
		{Role: "fixture_role", Grantors: []string{"fixture_owner"}},
		{Role: "fixture_role", Grantors: []string{port.RoleAccountAdmin, port.RoleAccountAdmin}},
		{Role: "fixture_role", OrgKinds: []string{"fixture_center", "fixture_center"}},
	}
	for _, registration := range cases {
		t.Run(registration.Role, func(t *testing.T) {
			got, err := port.ExtendRoleCatalog(base, port.RoleRegistration{Role: "fixture_owner"}, registration)
			if !errors.Is(err, port.ErrRoleCatalog) || got != nil || !reflect.DeepEqual(base, before) ||
				!reflect.DeepEqual(port.NativeRoleCatalog(), before) {
				t.Fatalf("invalid extension changed native policy or returned partial catalog: %v %v", got, err)
			}
		})
	}
	got, err := port.ExtendRoleCatalog(base, port.RoleRegistration{Role: "fixture_duplicate"}, port.RoleRegistration{Role: "fixture_duplicate"})
	if !errors.Is(err, port.ErrRoleCatalog) || got != nil || !reflect.DeepEqual(base, before) {
		t.Fatal("duplicate registration did not fail atomically")
	}
}
