package masterdata_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// Compare all thirteen complete HTTP problems with the old path frozen in E, including field order.
// The contract is read-only; no legacy implementation or expected-data generator remains.
func TestKNConstraintsMatchFrozenLegacy(t *testing.T) {
	run := func() []map[string]any {
		reg := extension.MasterdataCatalogs()
		f := newFixtureWithDeps(t, masterdata.Deps{Catalogs: []port.CatalogRegistration{reg}})
		admin := f.caller(f.seeded(), port.RoleMasterdataAdmin)
		var out []map[string]any
		errorCase := func(catalog, code string, attrs map[string]string, want int) {
			t.Helper()
			st, b := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/"+catalog+"/items",
				map[string]any{"code": code, "name": "Mục " + code, "valid_from": "2026-01-01", "attrs": attrs})
			if st != want {
				t.Fatalf("%s: %d %v, want %d", code, st, b, want)
			}
			delete(b, "instance")
			delete(b, "trace_id")
			out = append(out, b)
		}
		success := func(catalog, code string, attrs map[string]string) {
			t.Helper()
			st, b := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/"+catalog+"/items",
				map[string]any{"code": code, "name": "Mục " + code, "valid_from": "2026-01-01", "attrs": attrs})
			if st != 201 {
				t.Fatalf("valid %s: %d %v", code, st, b)
			}
		}
		errorCase("cnts_breed", "MISSING_BREED_SPECIES", map[string]string{"species": "MISSING"}, 422)
		errorCase("cnts_disease", "MISSING_DISEASE_SPECIES", map[string]string{"species": "MISSING"}, 422)
		errorCase("cnts_vaccine", "SORTED_REFERENCES", map[string]string{"species": "MISSING_SPECIES", "diseases": "MISSING_DISEASE", "report_group": "khac"}, 422)
		errorCase("cnts_model_type", "MISSING_MODEL_SPECIES", map[string]string{"sector": "chan_nuoi", "species": "MISSING"}, 422)
		errorCase("cnts_vaccination_schedule", "MISSING_SCHEDULE_REFS", map[string]string{"breed": "MISSING_BREED", "model_type": "MISSING_MODEL", "doses": "MISSING_VACCINE/1@1"}, 422)
		success("cnts_breed", "B", map[string]string{"species": "HEO"})
		errorCase("cnts_vaccination_schedule", "MISSING_VACCINE", map[string]string{"breed": "B", "doses": "MISSING_VACCINE/1@1,MISSING_VACCINE/2@2"}, 422)
		facility := func(gs, gc, unit, species string) map[string]string {
			return map[string]string{
				"facility_kind": "tap_trung", "commune_code": unit, "species": species, "stamp_codes_gs": gs, "stamp_codes_gc": gc}
		}
		errorCase("cnts_slaughter_facility", "FACILITY_SPECIES", facility("", "", "89", "MISSING"), 422)
		errorCase("cnts_slaughter_facility", "FACILITY_PROVINCE", facility("", "", "89", "HEO"), 422)
		errorCase("cnts_slaughter_facility", "FACILITY_MISSING_UNIT", facility("", "", "ABSENT", "HEO"), 422)
		errorCase("cnts_slaughter_facility", "FACILITY_CLOSED_UNIT", facility("", "", "30289", "HEO"), 422)
		success("cnts_slaughter_facility", "OWNER", facility("91.01,91.02", "91.03", "30292", "HEO"))
		errorCase("cnts_slaughter_facility", "CROSS_GC", facility("91.03", "", "30292", "HEO"), 409)
		errorCase("cnts_slaughter_facility", "ORDERED_TOKENS", facility("91.02,91.01", "91.03", "30292", "HEO"), 409)
		st, b := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/cnts_slaughter_facility/items/OWNER/deactivate", map[string]any{"reason": "Tạm ngừng hoạt động cơ sở"})
		if st != 200 {
			t.Fatalf("deactivate: %d %v", st, b)
		}
		errorCase("cnts_slaughter_facility", "INACTIVE_RESERVATION", facility("", "91.01", "30292", "HEO"), 409)
		return out
	}
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../golden/kn-masterdata-constraints.json"))
	if err != nil {
		t.Fatal(err)
	}
	var legacy []map[string]any
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	owned := run()
	if len(legacy) != 13 || len(owned) != 13 || !reflect.DeepEqual(legacy, owned) {
		t.Fatalf("owner constraints changed frozen legacy problems:\nlegacy=%+v\nowner=%+v", legacy, owned)
	}
}
