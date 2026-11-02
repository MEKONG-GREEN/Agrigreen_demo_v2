package assembly

import (
	"reflect"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func TestAccountRoleCatalogPreservesObservableOrder(t *testing.T) {
	c, err := AccountRoleCatalog()
	if err != nil {
		t.Fatal(err)
	}
	legacy := accport.LegacyRoleCatalog()
	if !slices.Equal(c.ManagedRoles(), legacy.ManagedRoles()) || !slices.Equal(c.LoginTOTPRoles(), legacy.LoginTOTPRoles()) || !slices.Equal(c.MFARoles(), legacy.MFARoles()) {
		t.Fatal("observable role ordering changed")
	}
	if !reflect.DeepEqual(c.RoleOrgKinds(), legacy.RoleOrgKinds()) {
		t.Fatal("organization restrictions changed")
	}
	for _, caller := range append(legacy.ManagedRoles(), append(slices.Clone(accport.AdminRoles), "fixture_unknown")...) {
		if !slices.Equal(c.Grantable([]string{caller}), legacy.Grantable([]string{caller})) {
			t.Fatalf("grants/order changed for %s", caller)
		}
	}
	for _, callers := range [][]string{{accport.RoleAccountAdminProvince, accport.RoleAccountAdmin}, {accport.RoleAccountAdmin, "platform_admin"}} {
		if !slices.Equal(c.Grantable(callers), legacy.Grantable(callers)) {
			t.Fatalf("multi-caller ordering changed: %v", callers)
		}
	}
	for _, r := range extension.RoleRegistrations() {
		if !c.IsManaged(r.Role) || !c.NeedsLoginTOTP([]string{r.Role}) || c.NeedsMFA([]string{r.Role}) || len(c.Grantable([]string{r.Role})) != 0 {
			t.Fatalf("owner role policy changed: %s", r.Role)
		}
	}
}
