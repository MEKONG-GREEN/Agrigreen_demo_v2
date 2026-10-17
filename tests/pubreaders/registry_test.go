package pubreaders_test

import (
	"reflect"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

func TestDeclaredPublicationReadersMatchLegacyBootstrap(t *testing.T) {
	declared := map[string][]string{}
	for _, entry := range modreg.Catalog() {
		if len(entry.PubReadOwners) > 0 {
			declared[entry.Name] = entry.PubReadOwners
		}
	}
	if !reflect.DeepEqual(declared, dbadmin.PubReaders) {
		t.Fatalf("source-owned readers differ from old bootstrap: %v / %v", declared, dbadmin.PubReaders)
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
