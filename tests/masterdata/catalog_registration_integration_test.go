package masterdata_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestRegisteredCatalogEffectiveDatesAndIsolation(t *testing.T) {
	reg := port.CatalogRegistration{Definitions: []port.CatalogDefinition{{
		Catalog: "fixture_catalog", Name: "Danh mục chung", Sync: true, EffectiveDated: true, SeedValidFrom: "2000-01-01",
		Attributes: map[string]port.AttributeSpec{"value": {Required: true, Normalize: func(s string) (string, error) {
			s = strings.TrimSpace(s)
			if s == "" {
				return "", errors.New("bắt buộc")
			}
			return s, nil
		}}},
	}}, Seeds: []port.CatalogSeed{{Catalog: "fixture_catalog", Code: "SEED", Name: "Nạp sẵn", Attrs: map[string]string{"value": "seed"}}}}
	f := newFixtureWithDeps(t, masterdata.Deps{Catalogs: []port.CatalogRegistration{reg}})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	code, body := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/fixture_catalog/items", map[string]any{"code": "AA", "name": " Thử ", "attrs": map[string]string{"value": " v1 "}, "valid_from": "2026-01-01"})
	if code != http.StatusCreated || body["attrs"].(map[string]any)["value"] != "v1" {
		t.Fatalf("registered date catalog create: %d %v", code, body)
	}
	if code, body = admin.adm(http.MethodPut, "/v1/masterdata/catalogs/fixture_catalog/items/AA", map[string]any{"name": "Thử", "attrs": map[string]string{"value": "v2"}, "valid_from": "2026-02-01"}); code != http.StatusOK {
		t.Fatalf("future value: %d %v", code, body)
	}
	for _, tc := range []struct{ at, value string }{{"2026-01-15", "v1"}, {"2026-02-01", "v2"}} {
		if code, body = admin.get("/v1/masterdata/catalogs/fixture_catalog/AA?at=" + tc.at); code != http.StatusOK || body["attrs"].(map[string]any)["value"] != tc.value {
			t.Fatalf("effective at %s: %d %v", tc.at, code, body)
		}
	}
	code, body = admin.get("/v1/masterdata/catalogs/fixture_catalog?at=2026-01-15")
	if code != http.StatusOK {
		t.Fatalf("list at date: %d %v", code, body)
	}
	found := false
	for _, raw := range body["items"].([]any) {
		item := raw.(map[string]any)
		if item["code"] == "AA" {
			found = true
			if item["attrs"].(map[string]any)["value"] != "v1" {
				t.Fatalf("list ignored date: %v", item)
			}
		}
	}
	if !found {
		t.Fatal("list at date omitted item")
	}
	if code, body = admin.get("/v1/masterdata/catalogs/fixture_catalog/AA?at=2025-12-31"); code != http.StatusNotFound {
		t.Fatalf("before first value: %d %v", code, body)
	}
	if code, body = f.caller(ids.New(), port.RoleMasterdataAdmin).get("/v1/masterdata/catalogs/fixture_catalog/AA?at=2026-02-01"); code != http.StatusNotFound {
		t.Fatalf("cross tenant read: %d %v", code, body)
	}
	if code, body = f.caller(tid).adm(http.MethodPost, "/v1/masterdata/catalogs/fixture_catalog/items", map[string]any{"code": "NO", "name": "NO", "attrs": map[string]string{"value": "x"}}); code != http.StatusForbidden {
		t.Fatalf("write permissions: %d %v", code, body)
	}
	if code, body = admin.adm(http.MethodGet, "/v1/masterdata/catalogs/fixture_catalog/items/AA/versions", nil); code != http.StatusOK || len(body["items"].([]any)) != 2 {
		t.Fatalf("two immutable versions: %d %v", code, body)
	}
	changes, _, err := f.mod.SyncSource().Changes(admin.ctx(), authz.Scope{All: true}, port.SyncEntityCatalogItem, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(changes)
	if err != nil || !strings.Contains(string(raw), "fixture_catalog") {
		t.Fatalf("registered sync catalog missing: %s %v", raw, err)
	}
	// The generic port must use the same date and tenant rules, without KN.
	catalogs := f.mod.EffectiveCatalogs()
	value, err := catalogs.ItemAt(admin.ctx(), "fixture_catalog", "AA", day("2026-01-15"))
	if err != nil || value.Item.Attrs["value"] != "v1" || value.ValidFrom.Format("2006-01-02") != "2026-01-01" {
		t.Fatalf("generic item at date: %+v %v", value, err)
	}
	values, err := catalogs.ListAt(admin.ctx(), "fixture_catalog", day("2026-01-15"))
	if err != nil || len(values) != 2 {
		t.Fatalf("generic list at date: %+v %v", values, err)
	}
	for _, v := range values {
		if v.Item.Code == "AA" && v.Item.Attrs["value"] != "v1" {
			t.Fatalf("generic list ignored date: %+v", v)
		}
	}
	if _, err := catalogs.ItemAt(admin.ctx(), "fixture_catalog", "AA", day("2025-12-31")); !errors.Is(err, port.ErrNotEffective) {
		t.Fatalf("generic before first value: %v", err)
	}
	if code, err := catalogs.CodeByAttribute(admin.ctx(), "fixture_catalog", "value", "v2"); err != nil || code != "AA" {
		t.Fatalf("generic attribute lookup: %s %v", code, err)
	}
	if code, err := catalogs.CodeByAttribute(admin.ctx(), "fixture_catalog", "value", "v2' OR true --"); err != nil || code != "" {
		t.Fatalf("attribute value must be bound: %s %v", code, err)
	}
	other := f.caller(ids.New(), port.RoleMasterdataAdmin)
	if _, err := catalogs.ItemAt(other.ctx(), "fixture_catalog", "AA", day("2026-02-01")); !errors.Is(err, port.ErrNotEffective) {
		t.Fatalf("generic cross tenant item: %v", err)
	}
	if code, err := catalogs.CodeByAttribute(other.ctx(), "fixture_catalog", "value", "v2"); err != nil || code != "" {
		t.Fatalf("generic cross tenant identity: %s %v", code, err)
	}
	if _, err := catalogs.CodeByAttribute(admin.ctx(), "fixture_catalog", "undeclared", "v2"); err == nil {
		t.Fatal("undeclared attribute accepted")
	}

}
