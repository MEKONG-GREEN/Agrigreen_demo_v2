package knaccounts_test

import (
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// Compare the actual production composition, not synthetic registrations, with
// the legacy HTTP contract across create/assignment/import/MFA and native roles.
func TestProductionRoleCatalogHTTPParityOnPostgres(t *testing.T) {
	roles, err := assembly.AccountRoleCatalog()
	if err != nil {
		t.Fatal(err)
	}
	before, after := observeRoleCatalog(t, nil), observeRoleCatalog(t, roles)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("production role rollout changed create/grant/assignment/import/MFA behavior")
	}
	created := after["create/account_admin/team30292/kn_officer"].(map[string]any)
	if created["status"] != http.StatusCreated || created["mfa_required"] != true || !slices.Contains(created["required_actions"].([]string), accport.ActionConfigureTOTP) {
		t.Fatal("D24 lost for actual owner role")
	}
	imp := after["import"].(map[string]any)
	if imp["created"] != float64(1) || imp["rejected"] != float64(5) {
		t.Fatalf("CSV outcome changed: %v", imp)
	}
	f := newFixture(t, fixtureDeps{Roles: roles})
	r := f.as(f.p91, accport.RoleAccountAdminProvince).do(http.MethodGet, "/v1/accounts/grants", nil)
	values, ok := r.body["grantable"].([]any)
	if r.status != http.StatusOK || !ok || len(values) != 2 || values[0] != "kn_officer" || values[1] != "coop_manager" {
		t.Fatalf("raw HTTP ordering changed: %v", r.body)
	}
}
