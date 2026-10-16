package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"reflect"
	"testing"
)

// Cumulative coverage retains the legacy golden after deleting the adapter
// that produced it. The owner still compares every byte of the same artifact.
func TestRegisteredCatalogMergesContract(t *testing.T) {
	contribution := contractCatalogContribution(t)
	svc := Service{Catalog: NewCatalogRegistry(contribution)}
	native := domain.NativeSources()
	if len(native) != 4 || len(svc.sourceSpecs()) != 9 || len(svc.builtinTemplates()) != 9 {
		t.Fatal("native and registered catalog cardinality changed")
	}
	for key, spec := range native {
		if !reflect.DeepEqual(spec, svc.sourceSpec(key)) {
			t.Fatalf("native source changed: %s", key)
		}
	}
	for _, template := range domain.NativeBuiltins() {
		got, ok := svc.builtinTemplate(template.Key)
		if !ok || !reflect.DeepEqual(template, got) {
			t.Fatalf("native builtin changed: %s", template.Key)
		}
	}
	for key, spec := range contribution.Sources {
		got := svc.sourceSpec(key)
		if got.Key != key || got.Module != spec.Module || len(got.Columns) != len(spec.Columns) {
			t.Fatalf("registered source schema missing: %s", key)
		}
	}
	for _, template := range contribution.Templates {
		got, ok := svc.builtinTemplate(template.Key)
		if !ok || got.Version != 0 || got.Status != domain.TemplateActive || got.Def.Title != template.Def.Title || got.Def.Source != template.Def.Source || len(got.Def.Columns) != len(template.Def.Columns) {
			t.Fatalf("registered builtin changed: %s", template.Key)
		}
		if err := got.Def.ValidateWithSources(svc.sourceSpecs()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRegisteredCatalogColumnLevels(t *testing.T) {
	svc := Service{Catalog: NewCatalogRegistry(contractCatalogContribution(t))}
	template, ok := svc.builtinTemplate("kn.program_progress")
	if !ok {
		t.Fatal("registered program layout missing")
	}
	def := template.Def
	def.Columns = append(def.Columns, domain.Column{Key: "commune_name", Title: "Xã"})
	has := func(ds domain.Dataset, key string) bool {
		for _, c := range ds.Columns {
			if c.Key == key {
				return true
			}
		}
		return false
	}
	commune := domain.BuildWithSources(def, domain.LevelCommune, nil, nil, svc.sourceSpecs())
	if has(commune, "target_ha") || has(commune, "registered_pct") || !has(commune, "commune_name") {
		t.Fatal("province-only targets/ratio or commune name violated registered level rules")
	}
	province := domain.BuildWithSources(def, domain.LevelProvince, nil, nil, svc.sourceSpecs())
	if has(province, "commune_name") || !has(province, "target_ha") || !has(province, "registered_pct") {
		t.Fatal("registered province level lost targets/ratio or exposed commune name")
	}
}
func testCatalogRegistration() port.CatalogRegistration {
	return port.CatalogRegistration{Sources: map[string]port.CatalogSource{"example.measure": {Key: "example.measure", Module: "example", Levels: []string{"province"}, DefaultLevel: "province", Columns: []port.CatalogColumn{{Key: "amount", Title: "Quantity", Kind: "decimal", Scale: 2}}}}, Templates: []port.CatalogTemplate{{Key: "example.layout", Def: port.CatalogDefinition{Title: "Example report", Source: "example.measure", Grouping: "province", Columns: []port.CatalogTemplateColumn{{Key: "amount", Title: "Quantity"}}, Formats: []string{"xlsx"}}}}}
}
func TestCatalogRegistrationIsolation(t *testing.T) {
	contribution := testCatalogRegistration()
	registry := NewCatalogRegistry(contribution)
	svc := Service{Catalog: registry}
	src := contribution.Sources["example.measure"]
	src.Levels[0] = "mutated"
	src.Columns[0].Title = "mutated"
	contribution.Templates[0].Def.Columns[0].Title = "mutated"
	contribution.Templates[0].Def.Formats[0] = "mutated"
	snapshot := registry.Sources()
	spec := snapshot["example.measure"]
	spec.Levels[0] = "escaped"
	spec.Columns[0].Title = "escaped"
	snapshot["extra"] = spec
	templates := registry.Templates()
	for i := range templates {
		templates[i].Def.Columns[0].Title = "escaped"
		templates[i].Def.Formats[0] = "escaped"
	}
	got := svc.sourceSpec("example.measure")
	tpl, ok := svc.builtinTemplate("example.layout")
	if !ok || got.Levels[0] != "province" || got.Columns[0].Title != "Quantity" || tpl.Def.Columns[0].Title != "Quantity" || tpl.Def.Formats[0] != "xlsx" {
		t.Fatal("catalog changed through registration/snapshot aliases")
	}
	if _, ok := registry.Sources()["extra"]; ok {
		t.Fatal("catalog source map escaped")
	}
	// A source with metadata but no builder must never produce an empty success.
	if _, err := svc.buildDataset(context.Background(), domain.Job{Def: tpl.Def}); err == nil {
		t.Fatal("unconnected source builder succeeded")
	}
}
func TestCatalogRegistrationRejectsAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name          string
		registrations []port.CatalogRegistration
	}{
		{"duplicate source", []port.CatalogRegistration{testCatalogRegistration(), testCatalogRegistration()}},
		{"unknown template source", []port.CatalogRegistration{{Templates: []port.CatalogTemplate{{Key: "example.bad", Def: port.CatalogDefinition{Title: "Example report", Source: "missing", Columns: []port.CatalogTemplateColumn{{Key: "amount"}}, Formats: []string{"xlsx"}}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("ambiguous/invalid catalog accepted")
				}
			}()
			NewCatalogRegistry(tc.registrations...)
		})
	}
}
