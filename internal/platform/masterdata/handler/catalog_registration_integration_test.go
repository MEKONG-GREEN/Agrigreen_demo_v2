package handler_test

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
}
