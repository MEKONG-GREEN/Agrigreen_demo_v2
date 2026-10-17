package app

import (
	"reflect"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func sampleConstraints() *port.CatalogConstraints {
	return &port.CatalogConstraints{StableUUIDAttribute: "value",
		AdminUnit: &port.CatalogAdminUnitConstraint{Attribute: "value", Level: "commune",
			Issue: port.CatalogConstraintIssue{Status: 422, Code: "fixture.unit", Title: "Đơn vị không hợp lệ", Field: "attrs.value"}},
		UniqueTokens: &port.CatalogUniqueTokensConstraint{Attributes: []string{"value"}, Separator: ",",
			Issue: port.CatalogConstraintIssue{Status: 409, Code: "fixture.token", Title: "Mã đã có", Field: "attrs.tokens"}},
	}
}

func TestCatalogConstraintsCopiesAndValidation(t *testing.T) {
	reg := sampleCatalogRegistration()
	reg.Definitions[0].Constraints = sampleConstraints()
	r := NewCatalogRegistry(reg)
	reg.Definitions[0].Constraints.AdminUnit.Issue.Title = "corrupt"
	reg.Definitions[0].Constraints.UniqueTokens.Attributes[0] = "corrupt"
	reg.Definitions[0].Constraints.StableUUIDAttribute = "corrupt"
	d, _ := r.definition("fixture_catalog")
	d.Constraints.AdminUnit.Level = "corrupt"
	d.Constraints.UniqueTokens.Attributes[0] = "corrupt"
	d.Constraints.UniqueTokens.Issue.Title = "corrupt"
	d.Constraints.StableUUIDAttribute = "corrupt"
	d, _ = r.definition("fixture_catalog")
	if !reflect.DeepEqual(d.Constraints, sampleConstraints()) {
		t.Fatalf("mutable constraints: %+v", d.Constraints)
	}
	for _, name := range []string{"uuid", "unit-attr", "unit-level", "unit-status", "unit-code", "unit-title", "unit-field", "tokens-attr", "tokens-empty", "tokens-duplicate", "tokens-separator", "tokens-issue"} {
		t.Run(name, func(t *testing.T) {
			in := sampleCatalogRegistration()
			c := sampleConstraints()
			switch name {
			case "uuid":
				c.StableUUIDAttribute = "undeclared"
			case "unit-attr":
				c.AdminUnit.Attribute = "undeclared"
			case "unit-level":
				c.AdminUnit.Level = "world"
			case "unit-status":
				c.AdminUnit.Issue.Status = 200
			case "unit-code":
				c.AdminUnit.Issue.Code = ""
			case "unit-title":
				c.AdminUnit.Issue.Title = ""
			case "unit-field":
				c.AdminUnit.Issue.Field = "not an attribute"
			case "tokens-attr":
				c.UniqueTokens.Attributes[0] = "undeclared"
			case "tokens-empty":
				c.UniqueTokens.Attributes = nil
			case "tokens-duplicate":
				c.UniqueTokens.Attributes = []string{"value", "value"}
			case "tokens-separator":
				c.UniqueTokens.Separator = ""
			case "tokens-issue":
				c.UniqueTokens.Issue.Status = 500
			}
			in.Definitions[0].Constraints = c
			defer func() {
				if recover() == nil {
					t.Fatal("invalid constraint accepted")
				}
			}()
			NewCatalogRegistry(in)
		})
	}
}

func TestCatalogConstraintCallbackCannotMutateItem(t *testing.T) {
	attrs := map[string]string{"value": "original"}
	c := port.CatalogConstraints{References: func(in map[string]string) ([]port.CatalogReference, error) {
		in["value"] = "corrupt"
		delete(in, "new")
		return []port.CatalogReference{{Catalog: "fixture_parent", Code: "A", Field: "attrs.value"}}, nil
	}}
	got, err := catalogReferences(c, attrs)
	if err != nil || len(got) != 1 || attrs["value"] != "original" {
		t.Fatalf("callback mutated normalized item: %v %v %v", attrs, got, err)
	}
}

func TestEmptyCatalogConstraintsPreserveIdentity(t *testing.T) {
	in := sampleCatalogRegistration()
	in.Definitions[0].Constraints = &port.CatalogConstraints{}
	s := &Service{CatalogRegistry: NewCatalogRegistry(in)}
	item := domain.Item{Catalog: "fixture_catalog", Attrs: map[string]string{"value": "original"}}
	if !reflect.DeepEqual(s.prepareCatalogIdentity(item), item) || !reflect.DeepEqual(s.keepCatalogIdentity(item, item), item) {
		t.Fatal("empty constraints changed identity")
	}
}
