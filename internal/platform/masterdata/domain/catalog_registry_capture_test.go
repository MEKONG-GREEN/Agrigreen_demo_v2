package domain

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/seeddata"
)

type legacyCatalogCase struct {
	Input  port.CatalogItem
	Output port.CatalogItem
	Error  string
}
type legacyCatalogContract struct {
	Metadata []port.CatalogMetadata
	Seeds    []port.CatalogSeed
	Cases    []legacyCatalogCase
}

// Removed with the legacy definitions; the captured artifact then stays frozen.
func TestCaptureLegacyCatalogContract(t *testing.T) {
	var out legacyCatalogContract
	for _, key := range CNTSCatalogs {
		out.Metadata = append(out.Metadata, port.CatalogMetadata{Catalog: key, Name: CatalogName(key), Attributes: CatalogAttrs(key), EffectiveDated: EffectiveDated(key), Sync: slices.Contains(SyncCatalogs, key), SeedValidFrom: "2000-01-01"})
	}
	var cf struct{ Items []port.CatalogSeed }
	if err := json.Unmarshal(seeddata.Catalogs, &cf); err != nil {
		t.Fatal(err)
	}
	samples := map[string]port.CatalogItem{}
	add := func(in port.CatalogItem) {
		in.Attrs = maps.Clone(in.Attrs)
		it, err := (Item{Catalog: in.Catalog, Code: in.Code, Name: in.Name, Attrs: in.Attrs, Active: in.Active}).Normalize()
		c := legacyCatalogCase{Input: in, Output: port.CatalogItem{Catalog: it.Catalog, Code: it.Code, Name: it.Name, Attrs: it.Attrs, Active: it.Active}}
		if err != nil {
			c.Error = err.Error()
		}
		out.Cases = append(out.Cases, c)
	}
	for _, seed := range cf.Items {
		if !strings.HasPrefix(seed.Catalog, "cnts_") {
			continue
		}
		out.Seeds = append(out.Seeds, seed)
		in := port.CatalogItem{Catalog: seed.Catalog, Code: seed.Code, Name: seed.Name, Attrs: seed.Attrs}
		add(in)
		samples[in.Catalog] = in
	}
	samples[port.CatalogCNTSBreed] = port.CatalogItem{Catalog: port.CatalogCNTSBreed, Code: "GIONG_A", Name: "Giống thử", Attrs: map[string]string{"species": "GA"}}
	samples[port.CatalogCNTSSlaughterFacility] = port.CatalogItem{Catalog: port.CatalogCNTSSlaughterFacility, Code: "CS_A", Name: "Cơ sở thử", Attrs: map[string]string{"facility_ref": "0190f1c4-7d3e-7000-8000-000000000001", "commune_code": "30292", "facility_kind": "tap_trung", "stamp_codes_gs": "91.589.05,91.589.05.1"}}
	for _, key := range CNTSCatalogs {
		sample, ok := samples[key]
		if !ok {
			t.Fatal("missing valid sample " + key)
		}
		add(sample)
		bad := sample
		bad.Code = "BAD CODE"
		add(bad)
		bad = sample
		bad.Name = " "
		add(bad)
		bad = sample
		bad.Attrs = maps.Clone(sample.Attrs)
		bad.Attrs["unexpected"] = "x"
		add(bad)
		for _, attr := range CatalogAttrs(key) {
			if attrSpecs[key][attr].Required {
				bad = sample
				bad.Attrs = maps.Clone(sample.Attrs)
				delete(bad.Attrs, attr)
				add(bad)
			}
			bad = sample
			bad.Attrs = maps.Clone(sample.Attrs)
			bad.Attrs[attr] = " invalid,?? "
			add(bad)
		}
	}
	sch := samples[port.CatalogCNTSVaccinationSchedule]
	sch.Attrs = maps.Clone(sch.Attrs)
	delete(sch.Attrs, "model_type")
	delete(sch.Attrs, "breed")
	add(sch)
	for _, doses := range []string{" VX_B/2@28,VX_A/1@3,VX_B/1@7 ", "", "VX/0@1", "VX/1@4000", "VX/1@3,VX/1@4"} {
		sch = samples[port.CatalogCNTSVaccinationSchedule]
		sch.Attrs = maps.Clone(sch.Attrs)
		sch.Attrs["doses"] = doses
		add(sch)
	}
	facility := samples[port.CatalogCNTSSlaughterFacility]
	facility.Attrs = maps.Clone(facility.Attrs)
	facility.Attrs["stamp_codes_gc"] = "91.589.05"
	add(facility)
	if len(out.Metadata) != 12 || len(out.Seeds) != 60 || len(out.Cases) < 150 {
		t.Fatalf("inventory %d/%d/%d", len(out.Metadata), len(out.Seeds), len(out.Cases))
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-masterdata-catalogs.json")
	if os.Getenv("AG_CAPTURE_LEGACY_CATALOGS") == "1" {
		if err := os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(raw) {
		t.Fatal("legacy catalog contract changed")
	}
}
