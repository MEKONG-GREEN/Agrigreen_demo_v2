package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func TestCatalogsRequireRegistration(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-masterdata-catalogs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var frozen struct {
		Metadata []port.CatalogMetadata
		Seeds    []port.CatalogSeed
	}
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	if len(frozen.Metadata) != 12 || len(frozen.Seeds) != 80 { // +4 CSGM mẫu (BF-CN-SLAUGHTER #169), +16 mục BM-07/chỉ tiêu (BF-CN-CATALOG #170)
		t.Fatal("frozen inventory changed")
	}
	for _, tc := range []struct {
		name     string
		registry *CatalogRegistry
	}{{"nil", nil}, {"empty", NewCatalogRegistry()}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{CatalogRegistry: tc.registry}
			for _, m := range frozen.Metadata {
				if svc.catalogKnown(m.Catalog) || svc.catalogEffective(m.Catalog) {
					t.Fatalf("unregistered catalog inferred: %s", m.Catalog)
				}
			}
			seeds, err := svc.catalogSeeds()
			if err != nil {
				t.Fatal(err)
			}
			for _, seed := range seeds {
				for _, owned := range frozen.Seeds {
					if seed.Catalog == owned.Catalog && seed.Code == owned.Code {
						t.Fatalf("unregistered seed inferred: %s/%s", seed.Catalog, seed.Code)
					}
				}
			}
		})
	}
}
