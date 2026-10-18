package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func sampleCatalogRegistration() port.CatalogRegistration {
	return port.CatalogRegistration{Definitions: []port.CatalogDefinition{{
		Catalog: "fixture_catalog", Name: "Danh mục thử", Sync: true, EffectiveDated: true, SeedValidFrom: "2000-01-01",
		Attributes: map[string]port.AttributeSpec{"value": {Required: true, Normalize: func(s string) (string, error) {
			s = strings.TrimSpace(s)
			if s == "" {
				return "", errors.New("bắt buộc")
			}
			return s, nil
		}}},
	}}, Seeds: []port.CatalogSeed{{Catalog: "fixture_catalog", Code: "A", Name: "A", Attrs: map[string]string{"value": "x"}}}}
}

func TestCatalogRegistryCopiesAndIsolation(t *testing.T) {
	in := sampleCatalogRegistration()
	r := NewCatalogRegistry(in)
	in.Definitions[0].Attributes["value"] = port.AttributeSpec{}
	in.Seeds[0].Attrs["value"] = "corrupt"
	d, _ := r.definition("fixture_catalog")
	d.Attributes["value"] = port.AttributeSpec{}
	r.Seeds()[0].Attrs["value"] = "corrupt"
	svc := &Service{CatalogRegistry: r}
	got, err := svc.NormalizeCatalogItem(port.CatalogItem{Catalog: "fixture_catalog", Code: " A ", Name: " A ", Attrs: map[string]string{"value": " x "}})
	if err != nil || got.Code != "A" || got.Name != "A" || got.Attrs["value"] != "x" {
		t.Fatalf("aliased schema: %+v %v", got, err)
	}
	if r.Seeds()[0].Attrs["value"] != "x" {
		t.Fatal("aliased seed")
	}
	if (&Service{CatalogRegistry: NewCatalogRegistry()}).catalogKnown("fixture_catalog") {
		t.Fatal("catalog leaked across instances")
	}
}

func TestCatalogRegistryRejectsInvalid(t *testing.T) {
	for _, name := range []string{"duplicate", "native", "key", "name", "attribute", "normalizer", "date", "date-disabled", "seed-unowned", "seed-duplicate", "seed-invalid", "import-detail"} {
		t.Run(name, func(t *testing.T) {
			in := sampleCatalogRegistration()
			switch name {
			case "duplicate":
				in.Definitions = append(in.Definitions, in.Definitions[0])
			case "native":
				in.Definitions[0].Catalog = "rice_variety"
				in.Seeds = nil
			case "key":
				in.Definitions[0].Catalog = "INVALID"
			case "name":
				in.Definitions[0].Name = ""
			case "attribute":
				in.Definitions[0].Attributes["bad;key"] = in.Definitions[0].Attributes["value"]
			case "normalizer":
				in.Definitions[0].Attributes["value"] = port.AttributeSpec{}
			case "date":
				in.Definitions[0].SeedValidFrom = "1999-01-01"
			case "date-disabled":
				in.Definitions[0].EffectiveDated = false
			case "seed-unowned":
				in.Seeds[0].Catalog = "other_catalog"
			case "seed-duplicate":
				in.Seeds = append(in.Seeds, in.Seeds[0])
			case "seed-invalid":
				delete(in.Seeds[0].Attrs, "value")
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid catalog accepted")
				}
			}()
			if name == "import-detail" {
				in.InvalidEffectiveDateDetail = "first"
				NewCatalogRegistry(in, port.CatalogRegistration{InvalidEffectiveDateDetail: "other"})
			} else {
				NewCatalogRegistry(in)
			}
		})
	}
}
