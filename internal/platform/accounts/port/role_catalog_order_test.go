package port

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestRoleCatalogOrderPreservesPolicyAndIsolation(t *testing.T) {
	c, err := ExtendRoleCatalog(NativeRoleCatalog(), RoleRegistration{Role: "fixture_owner", LoginTOTP: true, OrgKinds: []string{"fixture_org"}, Grantors: []string{RoleAccountAdmin}})
	if err != nil {
		t.Fatal(err)
	}
	before := c.Clone()
	order := c.Clone()
	slices.Reverse(order.managed)
	slices.Reverse(order.loginTOTP)
	slices.Reverse(order.mfa)
	slices.Reverse(order.adminRoles)
	for caller := range order.grants {
		slices.Reverse(order.grants[caller])
	}
	// The reference is not a policy source: even its organization restrictions
	// must not overwrite the actual owner's restrictions.
	order.orgKinds["fixture_owner"] = []string{"wrong_org"}
	got, err := WithRoleCatalogOrder(c, order)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.ManagedRoles(), order.ManagedRoles()) || !slices.Equal(got.LoginTOTPRoles(), order.LoginTOTPRoles()) || !slices.Equal(got.MFARoles(), order.MFARoles()) {
		t.Fatal("order not applied")
	}
	if !reflect.DeepEqual(got.RoleOrgKinds(), before.RoleOrgKinds()) || !got.NeedsLoginTOTP([]string{"fixture_owner"}) || got.NeedsMFA([]string{"fixture_owner"}) {
		t.Fatal("reference changed owner policy")
	}
	for _, caller := range before.adminRoles {
		if !slices.Equal(got.Grantable([]string{caller}), order.Grantable([]string{caller})) {
			t.Fatal("grant ordering changed")
		}
	}
	got.managed[0] = "corrupted"
	got.grants[RoleAccountAdmin][0] = "corrupted"
	got.orgKinds["fixture_owner"][0] = "corrupted"
	if !reflect.DeepEqual(c, before) || slices.Contains(order.ManagedRoles(), "corrupted") || slices.Contains(order.Grantable([]string{RoleAccountAdmin}), "corrupted") {
		t.Fatal("catalog or reference mutated")
	}
}

func TestRoleCatalogOrderRejectsPolicyChangesAtomically(t *testing.T) {
	c := LegacyRoleCatalog()
	before := c.Clone()
	for _, tc := range []struct {
		name   string
		mutate func(*RoleCatalog)
	}{
		{"drop managed", func(r *RoleCatalog) { r.managed = r.managed[1:] }},
		{"unknown managed", func(r *RoleCatalog) { r.managed[0] = "fixture_unknown" }},
		{"duplicate managed", func(r *RoleCatalog) { r.managed[0] = r.managed[1] }},
		{"drop MFA", func(r *RoleCatalog) { r.mfa = r.mfa[1:] }},
		{"drop login TOTP", func(r *RoleCatalog) { r.loginTOTP = r.loginTOTP[1:] }},
		{"drop admin", func(r *RoleCatalog) { r.adminRoles = r.adminRoles[1:] }},
		{"drop grantor", func(r *RoleCatalog) { delete(r.grants, RoleAccountAdmin) }},
		{"add grantor", func(r *RoleCatalog) { r.grants["fixture_unknown"] = []string{"coop_manager"} }},
		{"drop grant", func(r *RoleCatalog) { r.grants[RoleAccountAdminProvince] = []string{"coop_manager"} }},
		{"change grant", func(r *RoleCatalog) { r.grants[RoleAccountAdminProvince] = []string{"platform_admin", "coop_manager"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := c.Clone()
			tc.mutate(order)
			if got, err := WithRoleCatalogOrder(c, order); got != nil || !errors.Is(err, ErrRoleCatalog) {
				t.Fatalf("accepted policy change: %v %v", got, err)
			}
			if !reflect.DeepEqual(c, before) {
				t.Fatal("rejected order mutated original")
			}
		})
	}
	for _, bad := range []*RoleCatalog{nil, {}} {
		if got, err := WithRoleCatalogOrder(c, bad); got != nil || !errors.Is(err, ErrRoleCatalog) {
			t.Fatal("invalid reference accepted")
		}
		if got, err := WithRoleCatalogOrder(bad, c); got != nil || !errors.Is(err, ErrRoleCatalog) {
			t.Fatal("invalid catalog accepted")
		}
	}
}
