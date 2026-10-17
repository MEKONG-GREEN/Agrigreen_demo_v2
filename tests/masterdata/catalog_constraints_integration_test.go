package masterdata_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func constraintFixtureRegistration() port.CatalogRegistration {
	text := port.AttributeSpec{Normalize: func(s string) (string, error) { return strings.TrimSpace(s), nil }}
	return port.CatalogRegistration{Definitions: []port.CatalogDefinition{
		{Catalog: "fixture_parent", Name: "Mục cha", Attributes: map[string]port.AttributeSpec{}, Constraints: &port.CatalogConstraints{}},
		{Catalog: "fixture_asset", Name: "Tài sản", EffectiveDated: true, SeedValidFrom: "2000-01-01",
			Attributes: map[string]port.AttributeSpec{"identity": {Required: true, Normalize: func(s string) (string, error) {
				u, e := uuid.Parse(s)
				if e != nil {
					return "", e
				}
				return u.String(), nil
			}}, "parent": text, "unit": text, "tags_a": text, "tags_b": text},
			Constraints: &port.CatalogConstraints{
				StableUUIDAttribute: "identity",
				References: func(attrs map[string]string) ([]port.CatalogReference, error) {
					if attrs["parent"] == "" {
						return nil, nil
					}
					return []port.CatalogReference{{Catalog: "fixture_parent", Code: attrs["parent"], Field: "attrs.parent"}}, nil
				},
				AdminUnit: &port.CatalogAdminUnitConstraint{Attribute: "unit", Level: "commune",
					Issue: port.CatalogConstraintIssue{Status: 422, Code: "fixture.unit", Title: "Đơn vị không hợp lệ", Field: "attrs.unit"}},
				UniqueTokens: &port.CatalogUniqueTokensConstraint{Attributes: []string{"tags_a", "tags_b"}, Separator: "|",
					Issue: port.CatalogConstraintIssue{Status: 409, Code: "fixture.token", Title: "Mã đã có", Field: "attrs.tokens"}},
			}},
	}}
}

func constraintIssue(t *testing.T, status int, body map[string]any, want int, code, title, field, reason string) {
	t.Helper()
	errs, _ := body["errors"].([]any)
	if status != want || body["code"] != code || body["title"] != title || len(errs) != 1 {
		t.Fatalf("problem contract: %d %v, want %d/%s/%s", status, body, want, code, title)
	}
	e := errs[0].(map[string]any)
	if e["field"] != field || e["reason"] != reason {
		t.Fatalf("problem fields: %v want %s/%s", e, field, reason)
	}
}

func TestRegisteredCatalogTransactionConstraints(t *testing.T) {
	// No KN registration: this exercises the capability's generic transaction rules.
	f := newFixtureWithDeps(t, masterdata.Deps{Catalogs: []port.CatalogRegistration{constraintFixtureRegistration()}})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	createParent := func(c caller) {
		t.Helper()
		st, b := c.adm(http.MethodPost, "/v1/masterdata/catalogs/fixture_parent/items", map[string]any{"code": "P", "name": "Cha"})
		if st != 201 {
			t.Fatalf("parent: %d %v", st, b)
		}
	}
	createParent(admin)
	create := func(c caller, code, parent, unit, a, b, from string) (int, map[string]any) {
		return c.adm(http.MethodPost, "/v1/masterdata/catalogs/fixture_asset/items", map[string]any{
			"code": code, "name": "Tài sản " + code, "valid_from": from,
			"attrs": map[string]string{"identity": "caller-invalid-uuid", "parent": parent, "unit": unit, "tags_a": a, "tags_b": b}})
	}
	st, b := create(admin, "MISSING", "ABSENT", "30292", "", "", "2026-01-01")
	constraintIssue(t, st, b, 422, "masterdata.catalog_ref_invalid", "Mục tham chiếu (nhóm cây trồng, cây trồng bị ảnh hưởng) không có hoặc đã ngừng dùng", "attrs.parent", "ABSENT")
	st, b = create(admin, "LEVEL", "P", "89", "", "", "2026-01-01")
	constraintIssue(t, st, b, 422, "fixture.unit", "Đơn vị không hợp lệ", "attrs.unit", "89")
	st, b = create(admin, "PAST", "P", "30289", "", "", "2024-01-01")
	if st != 201 {
		t.Fatalf("historical unit: %d %v", st, b)
	}
	st, b = create(admin, "CLOSED", "P", "30289", "", "", "2026-01-01")
	constraintIssue(t, st, b, 422, "fixture.unit", "Đơn vị không hợp lệ", "attrs.unit", "30289")
	st, b = create(admin, "A", "P", "30292", "alpha|beta", "gamma", "2026-01-01")
	if st != 201 {
		t.Fatalf("create: %d %v", st, b)
	}
	identity := b["attrs"].(map[string]any)["identity"].(string)
	if u, e := uuid.Parse(identity); e != nil || u == uuid.Nil {
		t.Fatalf("system identity: %s %v", identity, e)
	}
	st, b = create(admin, "B", "P", "30292", "gamma", "", "2026-01-01")
	constraintIssue(t, st, b, 409, "fixture.token", "Mã đã có", "attrs.tokens", "gamma → A")
	st, b = admin.adm(http.MethodPut, "/v1/masterdata/catalogs/fixture_asset/items/A", map[string]any{
		"name": "Tài sản A đã sửa", "valid_from": "2026-02-01",
		"attrs": map[string]string{"identity": uuid.NewString(), "parent": "P", "unit": "30292", "tags_a": "alpha|beta", "tags_b": "gamma"}})
	if st != 200 || b["attrs"].(map[string]any)["identity"] != identity {
		t.Fatalf("stable identity after update: %d %v", st, b)
	}
	// Import also preserves existing identity and generates one for a new item.
	csv := "catalog,code,name,identity,parent,unit,tags_a,tags_b,valid_from\n" +
		"fixture_asset,A,Tài sản A đã sửa,ignored,P,30292,alpha|beta,gamma,2026-02-01\n" +
		"fixture_asset,IMPORTED,Tài sản nhập,ignored,P,30292,imported,,2026-02-01\n"
	st, b = admin.adm(http.MethodPost, "/v1/masterdata/catalog-imports", []byte(csv))
	if st != 200 || b["created"] != 1.0 || b["skipped"] != 1.0 {
		t.Fatalf("identity in import: %d %v", st, b)
	}
	st, b = admin.get("/v1/masterdata/catalogs/fixture_asset/IMPORTED")
	if st != 200 || b["attrs"].(map[string]any)["identity"] == "ignored" {
		t.Fatalf("import identity: %d %v", st, b)
	}
	// A conflict rolls back preceding rows in the same import transaction.
	csv = "catalog,code,name,parent,unit,tags_a,valid_from\nfixture_asset,ROLLBACK,Mục phải hoàn tác,P,30292,free,2026-01-01\nfixture_asset,CONFLICT,Mục trùng,P,30292,alpha,2026-01-01\n"
	st, b = admin.adm(http.MethodPost, "/v1/masterdata/catalog-imports", []byte(csv))
	constraintIssue(t, st, b, 409, "fixture.token", "Mã đã có", "attrs.tokens", "alpha → A")
	if st, b = admin.get("/v1/masterdata/catalogs/fixture_asset/ROLLBACK"); st != 404 {
		t.Fatalf("partial import committed: %d %v", st, b)
	}
	st, b = admin.adm(http.MethodPost, "/v1/masterdata/catalogs/fixture_asset/items/A/deactivate", map[string]any{"reason": "Tạm ngừng sử dụng tài sản"})
	if st != 200 {
		t.Fatalf("deactivate asset: %d %v", st, b)
	}
	st, b = create(admin, "B", "P", "30292", "", "beta", "2026-01-01")
	constraintIssue(t, st, b, 409, "fixture.token", "Mã đã có", "attrs.tokens", "beta → A")
	st, b = admin.adm(http.MethodPost, "/v1/masterdata/catalogs/fixture_parent/items/P/deactivate", map[string]any{"reason": "Tạm ngừng sử dụng mục cha"})
	if st != 200 {
		t.Fatalf("deactivate reference: %d %v", st, b)
	}
	st, b = admin.adm(http.MethodPost, "/v1/masterdata/catalogs/fixture_asset/items/A/reactivate", map[string]any{})
	constraintIssue(t, st, b, 422, "masterdata.catalog_ref_invalid", "Mục tham chiếu (nhóm cây trồng, cây trồng bị ảnh hưởng) không có hoặc đã ngừng dùng", "attrs.parent", "P")
	// Same code and tokens in another tenant must not see the first tenant's reservations.
	other := f.caller(f.seeded(), port.RoleMasterdataAdmin)
	st, b = create(other, "NO_PARENT", "P", "30292", "alpha", "", "2026-01-01")
	constraintIssue(t, st, b, 422, "masterdata.catalog_ref_invalid", "Mục tham chiếu (nhóm cây trồng, cây trồng bị ảnh hưởng) không có hoặc đã ngừng dùng", "attrs.parent", "P")
	createParent(other)
	st, b = create(other, "A", "P", "30292", "alpha", "gamma", "2026-01-01")
	if st != 201 || b["attrs"].(map[string]any)["identity"] == identity {
		t.Fatalf("tenant isolation: %d %v", st, b)
	}
	// Quotes and SQL-looking text are token values, never a SQL fragment.
	st, b = create(other, "QUOTE", "P", "30292", "literal' OR true --", "", "2026-01-01")
	if st != 201 {
		t.Fatalf("bound token create: %d %v", st, b)
	}
	st, b = create(other, "QUOTE_DUP", "P", "30292", "", "literal' OR true --", "2026-01-01")
	constraintIssue(t, st, b, 409, "fixture.token", "Mã đã có", "attrs.tokens", "literal' OR true -- → QUOTE")
}
