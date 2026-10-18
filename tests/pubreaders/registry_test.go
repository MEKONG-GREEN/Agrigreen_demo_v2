package pubreaders_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

func TestDeclaredPublicationReadersMatchRuntimeRoles(t *testing.T) {
	roles := map[string]dbadmin.Role{}
	for _, role := range dbadmin.Roles {
		roles[role.Name] = role
	}
	for _, entry := range modreg.Catalog() {
		if len(entry.OwnsSchema) == 0 || entry.Name == dbadmin.AnalyticsModule {
			continue
		}
		role, ok := roles[modreg.DBRole(entry.Name)]
		if !ok {
			t.Fatalf("%s: missing runtime module role", entry.Name)
		}
		var actual, want []string
		for _, parent := range role.MemberOf {
			if strings.HasSuffix(parent, "_pub") {
				actual = append(actual, parent)
			}
		}
		for _, owner := range entry.PubReadOwners {
			want = append(want, modreg.PubRole(owner))
		}
		slices.Sort(actual)
		slices.Sort(want)
		if !slices.Equal(actual, want) {
			t.Errorf("%s: runtime publication roles %v, declared %v", entry.Name, actual, want)
		}
	}
	for _, entry := range modreg.Catalog() {
		if len(entry.PubReadOwners) == 0 {
			continue
		}
		entry.PubReadOwners[0] = "corrupt"
		fresh, ok := modreg.Entry(entry.Name)
		if !ok || fresh.PubReadOwners[0] == "corrupt" {
			t.Fatal("catalog reader declarations alias caller data")
		}
	}
}
