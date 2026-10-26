package port_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// TT-13: KN business assignments and their realm entry roles never become an
// account-administration role, including through transitive realm composites.
func TestTT13KNRolesDoNotGrantAccountAdministration(t *testing.T) {
	roles := append([]string{port.KNTinh, port.KNXa, port.KNTW}, port.KNOfficerRoles...)
	for _, role := range roles {
		if got := port.Grantable([]string{role}); len(got) != 0 {
			t.Errorf("legacy KN role %s grants %v", role, got)
		}
		if got := port.LegacyRoleCatalog().Grantable([]string{role}); len(got) != 0 {
			t.Errorf("catalog KN role %s grants %v", role, got)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose", "keycloak", "agrigreen-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm struct {
		Roles struct {
			Realm []struct {
				Name       string
				Composites struct{ Realm []string }
			}
		}
	}
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatal(err)
	}
	graph := map[string][]string{}
	for _, r := range realm.Roles.Realm {
		graph[r.Name] = r.Composites.Realm
	}
	for _, role := range port.KNOfficerRoles {
		if _, ok := graph[role]; !ok {
			t.Fatalf("KN realm role missing: %s", role)
		}
		seen := map[string]bool{}
		pending := []string{role}
		for len(pending) > 0 {
			next := pending[0]
			pending = pending[1:]
			if seen[next] {
				continue
			}
			seen[next] = true
			if next == port.RoleAccountAdmin || next == port.RoleAccountAdminProvince || next == "platform_admin" {
				t.Errorf("KN role %s inherits administrator %s", role, next)
			}
			pending = append(pending, graph[next]...)
		}
	}
}
