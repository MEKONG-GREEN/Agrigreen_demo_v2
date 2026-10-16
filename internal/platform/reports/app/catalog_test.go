package app

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

// Capture the verified legacy metadata as an independent owner-migration oracle.
func legacyCatalogContribution() port.CatalogRegistration {
	r := port.CatalogRegistration{Sources: map[string]port.CatalogSource{}, Templates: []port.CatalogTemplate{}}
	for key, src := range domain.Sources {
		if src.Module == "extension" {
			out := port.CatalogSource{Key: key, Module: src.Module, Levels: append([]string{}, src.Levels...), DefaultLevel: src.DefaultLevel, NeedsPeriod: src.NeedsPeriod, UsesSeason: src.UsesSeason, NeedsCompare: src.NeedsCompare}
			for _, c := range src.Columns {
				out.Columns = append(out.Columns, port.CatalogColumn{Key: c.Key, Title: c.Title, Kind: string(c.Kind), Scale: c.Scale, MinLevel: c.MinLevel, MaxLevel: c.MaxLevel})
			}
			r.Sources[key] = out
		}
	}
	for _, tpl := range domain.Builtins() {
		if _, ok := r.Sources[tpl.Def.Source]; ok {
			d := tpl.Def
			def := port.CatalogDefinition{Title: d.Title, Source: d.Source, Grouping: d.Grouping, Formats: append([]string{}, d.Formats...), Provisional: d.Provisional, FormNote: d.FormNote}
			for _, c := range d.Columns {
				def.Columns = append(def.Columns, port.CatalogTemplateColumn{Key: c.Key, Title: c.Title})
			}
			r.Templates = append(r.Templates, port.CatalogTemplate{Key: tpl.Key, Def: def})
		}
	}
	return r
}
func TestLegacyReportCatalogContract(t *testing.T) {
	contribution := legacyCatalogContribution()
	if len(contribution.Sources) != 5 || len(contribution.Templates) != 5 {
		t.Fatal("legacy catalog capture must contain five sources and five layouts")
	}
	got, err := json.MarshalIndent(contribution, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-report-catalog.json")
	if os.Getenv("AG_UPDATE_KN_REPORT_CATALOG") == "1" {
		if err := os.WriteFile(path, got, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("legacy metadata differs from the recorded catalog contract")
	}
}
func TestRegisteredCatalogMatchesLegacy(t *testing.T) {
	old := Service{}
	registered := Service{Catalog: NewCatalogRegistry(legacyCatalogContribution())}
	if !reflect.DeepEqual(old.sourceSpecs(), registered.sourceSpecs()) {
		t.Fatal("source schema changed during registration")
	}
	a, b := old.builtinTemplates(), registered.builtinTemplates()
	sort.Slice(a, func(i, j int) bool { return a[i].Key < a[j].Key })
	sort.Slice(b, func(i, j int) bool { return b[i].Key < b[j].Key })
	if !reflect.DeepEqual(a, b) {
		t.Fatal("builtin layout metadata changed during registration")
	}
	for _, tpl := range a {
		before, after := tpl.Def, tpl.Def
		if err := before.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := after.ValidateWithSources(registered.sourceSpecs()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("definition normalization changed")
		}
		rows := []domain.Row{{"sown_area_ha": domain.Int(3), "capacity": domain.Int(4)}}
		x := domain.Build(before, before.Grouping, rows, nil)
		y := domain.BuildWithSources(after, after.Grouping, rows, nil, registered.sourceSpecs())
		if !reflect.DeepEqual(x, y) {
			t.Fatal("export columns/values changed during catalog registration")
		}
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
