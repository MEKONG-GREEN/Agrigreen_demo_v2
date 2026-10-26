package port_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func TestRoleCatalogLegacySnapshotMatchesExistingPolicy(t *testing.T) {
	catalog := port.LegacyRoleCatalog()
	if !catalog.Valid() || !reflect.DeepEqual(catalog.ManagedRoles(), port.ManagedRoles) ||
		!reflect.DeepEqual(catalog.MFARoles(), port.MFARoles) ||
		!reflect.DeepEqual(catalog.LoginTOTPRoles(), port.LoginTOTPRoles) ||
		!reflect.DeepEqual(catalog.RoleOrgKinds(), port.RoleOrgKinds) {
		t.Fatal("legacy snapshot changed role policy")
	}
	for _, role := range append(slices.Clone(port.ManagedRoles), "platform_admin", "offline_access", "fixture_unknown") {
		if catalog.IsManaged(role) != port.IsManaged(role) ||
			catalog.NeedsMFA([]string{role}) != port.NeedsMFA([]string{role}) ||
			catalog.NeedsLoginTOTP([]string{role}) != port.NeedsLoginTOTP([]string{role}) {
			t.Fatalf("legacy policy differs for %s", role)
		}
	}
	for _, caller := range append(slices.Clone(port.AdminRoles), "kn_national_admin", "kn_province_admin", "fixture_unknown") {
		if !reflect.DeepEqual(catalog.Grantable([]string{caller}), port.Grantable([]string{caller})) {
			t.Fatalf("legacy grants differ for %s", caller)
		}
	}
}

func TestRoleCatalogInstanceIsolationAndDefensiveCopies(t *testing.T) {
	base := port.LegacyRoleCatalog()
	input := port.RoleRegistration{Role: "fixture_officer", LoginTOTP: true,
		OrgKinds: []string{"fixture_center"}, Grantors: []string{port.RoleAccountAdmin}}
	registered, err := port.ExtendRoleCatalog(base, input)
	if err != nil {
		t.Fatal(err)
	}
	input.OrgKinds[0] = "mutated"
	input.Grantors[0] = "platform_admin"
	if !registered.IsManaged("fixture_officer") || !registered.NeedsLoginTOTP([]string{"fixture_officer"}) ||
		registered.NeedsMFA([]string{"fixture_officer"}) ||
		!slices.Equal(registered.OrgKinds("fixture_officer"), []string{"fixture_center"}) ||
		!slices.Contains(registered.Grantable([]string{port.RoleAccountAdmin}), "fixture_officer") {
		t.Fatal("application registration was not retained independently")
	}
	if base.IsManaged("fixture_officer") || base.NeedsLoginTOTP([]string{"fixture_officer"}) ||
		slices.Contains(base.Grantable([]string{port.RoleAccountAdmin}), "fixture_officer") ||
		port.IsManaged("fixture_officer") || port.NeedsLoginTOTP([]string{"fixture_officer"}) {
		t.Fatal("registration leaked into another instance or the global bridge")
	}
	if slices.Contains(registered.Grantable([]string{"fixture_officer"}), "fixture_officer") ||
		slices.Contains(registered.Grantable([]string{"platform_admin"}), "fixture_officer") {
		t.Fatal("registration implicitly created or expanded an administrator")
	}
	managed := registered.ManagedRoles()
	managed[len(managed)-1] = "mutated"
	mfa := registered.MFARoles()
	mfa[0] = "mutated"
	login := registered.LoginTOTPRoles()
	login[len(login)-1] = "mutated"
	kinds := registered.OrgKinds("fixture_officer")
	kinds[0] = "mutated"
	kindMap := registered.RoleOrgKinds()
	kindMap["fixture_officer"][0] = "mutated"
	kindMap["coop_manager"] = []string{"mutated"}
	grants := registered.Grantable([]string{port.RoleAccountAdmin})
	grants[len(grants)-1] = "mutated"
	if !registered.IsManaged("fixture_officer") || !registered.NeedsLoginTOTP([]string{"fixture_officer"}) ||
		!registered.NeedsMFA([]string{"platform_admin"}) ||
		!slices.Equal(registered.OrgKinds("fixture_officer"), []string{"fixture_center"}) ||
		!slices.Equal(registered.OrgKinds("coop_manager"), []string{"cooperative"}) ||
		!slices.Contains(registered.Grantable([]string{port.RoleAccountAdmin}), "fixture_officer") {
		t.Fatal("caller mutated catalog through an accessor")
	}
	clone := registered.Clone()
	if clone == registered || !reflect.DeepEqual(clone, registered) {
		t.Fatal("clone is not an independent equal snapshot")
	}
}

func TestRoleCatalogMFARegistrationRetainsNativeFloors(t *testing.T) {
	base := port.LegacyRoleCatalog()
	catalog, err := port.ExtendRoleCatalog(base,
		port.RoleRegistration{Role: "fixture_approver", MFARole: true, Grantors: []string{"platform_admin"}},
		port.RoleRegistration{Role: "fixture_viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.NeedsMFA([]string{"fixture_approver"}) || !catalog.NeedsLoginTOTP([]string{"fixture_approver"}) {
		t.Fatal("MFA role did not imply login TOTP")
	}
	if catalog.NeedsMFA([]string{"fixture_viewer"}) || catalog.NeedsLoginTOTP([]string{"fixture_viewer"}) {
		t.Fatal("ordinary role acquired MFA policy")
	}
	for _, role := range base.LoginTOTPRoles() {
		if !catalog.NeedsLoginTOTP([]string{role}) {
			t.Fatalf("native login TOTP floor was removed: %s", role)
		}
	}
	for _, role := range base.MFARoles() {
		if !catalog.NeedsMFA([]string{role}) {
			t.Fatalf("native Admin API MFA floor was removed: %s", role)
		}
	}
	if catalog.IsManaged("platform_admin") || slices.Contains(catalog.Grantable([]string{"platform_admin"}), "platform_admin") ||
		slices.Contains(catalog.Grantable([]string{port.RoleAccountAdmin}), port.RoleAccountAdmin) ||
		slices.Contains(catalog.Grantable([]string{port.RoleAccountAdmin}), "param_approver") ||
		slices.Contains(catalog.Grantable([]string{port.RoleAccountAdmin}), "vault_eraser") ||
		len(catalog.Grantable([]string{"kn_national_admin", "kn_province_admin"})) != 0 {
		t.Fatal("registration weakened native administration restrictions")
	}
}

func TestRoleCatalogRejectsConflictingOrPrivilegedRegistrationAtomically(t *testing.T) {
	base := port.LegacyRoleCatalog()
	before := base.Clone()
	cases := []port.RoleRegistration{
		{Role: ""}, {Role: " fixture_role"}, {Role: "fixture-role"},
		{Role: "platform_admin"}, {Role: port.RoleFarmer}, {Role: port.RoleLoginTOTP},
		{Role: "offline_access"}, {Role: "uma_authorization"}, {Role: "cgh_sync"},
		{Role: "default-roles-agrigreen"}, {Role: "service-account-test"},
		{Role: "coop_manager"}, {Role: "registry_admin"}, {Role: port.RoleKNUser},
		{Role: "fixture_role", Grantors: []string{"fixture_admin"}},
		{Role: "fixture_role", Grantors: []string{"kn_province_admin"}},
		{Role: "fixture_role", Grantors: []string{port.RoleAccountAdmin, port.RoleAccountAdmin}},
		{Role: "fixture_role", Grantors: []string{" "}},
		{Role: "fixture_role", OrgKinds: []string{""}},
		{Role: "fixture_role", OrgKinds: []string{"fixture_center", "fixture_center"}},
		{Role: "fixture_role", OrgKinds: []string{" fixture_center"}},
	}
	for _, registration := range cases {
		t.Run(registration.Role, func(t *testing.T) {
			got, err := port.ExtendRoleCatalog(base, registration)
			if !errors.Is(err, port.ErrRoleCatalog) || got != nil || !reflect.DeepEqual(base, before) {
				t.Fatalf("invalid registration changed base or produced a catalog: %v %v", got, err)
			}
		})
	}
	got, err := port.ExtendRoleCatalog(base, port.RoleRegistration{Role: "fixture_role"}, port.RoleRegistration{Role: "fixture_role"})
	if !errors.Is(err, port.ErrRoleCatalog) || got != nil || !reflect.DeepEqual(base, before) {
		t.Fatal("duplicate registration was not atomic")
	}
}

func TestRoleCatalogInvalidZeroValueDoesNotFallBack(t *testing.T) {
	for _, catalog := range []*port.RoleCatalog{nil, {}} {
		if catalog.Valid() || catalog.Clone() != nil || catalog.IsManaged("coop_manager") ||
			catalog.NeedsMFA([]string{"platform_admin"}) || catalog.NeedsLoginTOTP([]string{port.RoleKNUser}) ||
			len(catalog.ManagedRoles()) != 0 || len(catalog.MFARoles()) != 0 || len(catalog.LoginTOTPRoles()) != 0 ||
			len(catalog.OrgKinds("coop_manager")) != 0 || len(catalog.RoleOrgKinds()) != 0 ||
			len(catalog.Grantable([]string{"platform_admin"})) != 0 {
			t.Fatal("invalid catalog fell back to legacy policy")
		}
	}
	got, err := port.ExtendRoleCatalog(&port.RoleCatalog{})
	if !errors.Is(err, port.ErrRoleCatalog) || got != nil {
		t.Fatal("uninitialized configured base was accepted")
	}
	got, err = port.ExtendRoleCatalog(nil, port.RoleRegistration{Role: "fixture_reader"})
	if err != nil || !got.Valid() || !got.IsManaged("fixture_reader") || !got.IsManaged("coop_manager") {
		t.Fatal("nil construction base did not choose the explicit bridge", err)
	}
}
