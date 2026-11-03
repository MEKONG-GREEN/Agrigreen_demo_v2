package port

import (
	"errors"
	"testing"
	"time"
)

func validUnitDefinition() InternalUnitDefinition {
	return InternalUnitDefinition{
		Kinds:           []CatalogItem{{Code: "phong", Label: "Phòng"}, {Code: "tram", Label: "Trạm"}},
		DepartmentKinds: []string{"phong"},
		OrgKinds:        []string{"ttkn_province"},
		SeedStatus:      "tạm",
		Seed:            []InternalUnitSeedItem{{Code: "trong_trot", Kind: "phong", Name: "Phòng Trồng trọt", DepartmentCode: "trong_trot", SortOrder: 10}},
	}
}

func validDelegationDefinition() DelegationDefinition {
	return DelegationDefinition{Powers: []CatalogItem{{Code: "mo_lai_ky", Label: "Mở lại kỳ"}}, MaxDuration: time.Hour, GranteeRoles: []string{"kn_tinh"}}
}

func TestUnitDefinitionValidationFailsClosed(t *testing.T) {
	if err := ValidateInternalUnitDefinition(validUnitDefinition()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*InternalUnitDefinition){
		"zero":              func(d *InternalUnitDefinition) { *d = InternalUnitDefinition{} },
		"no kinds":          func(d *InternalUnitDefinition) { d.Kinds = nil },
		"bad kind code":     func(d *InternalUnitDefinition) { d.Kinds[0].Code = "Phong" },
		"duplicate kind":    func(d *InternalUnitDefinition) { d.Kinds[1].Code = "phong" },
		"blank label":       func(d *InternalUnitDefinition) { d.Kinds[0].Label = " " },
		"untrimmed label":   func(d *InternalUnitDefinition) { d.Kinds[0].Label = "Phòng " },
		"unknown dept kind": func(d *InternalUnitDefinition) { d.DepartmentKinds = []string{"kho"} },
		"duplicate dept":    func(d *InternalUnitDefinition) { d.DepartmentKinds = []string{"phong", "phong"} },
		"no org kinds":      func(d *InternalUnitDefinition) { d.OrgKinds = nil },
		"bad org kind":      func(d *InternalUnitDefinition) { d.OrgKinds = []string{""} },
		"no seed status":    func(d *InternalUnitDefinition) { d.SeedStatus = " " },
		"seed unknown kind": func(d *InternalUnitDefinition) { d.Seed[0].Kind = "kho" },
		"seed bad code":     func(d *InternalUnitDefinition) { d.Seed[0].Code = "x" },
		"seed duplicate": func(d *InternalUnitDefinition) {
			d.Seed = append(d.Seed, d.Seed[0])
		},
		"seed blank name":      func(d *InternalUnitDefinition) { d.Seed[0].Name = "" },
		"seed untrimmed name":  func(d *InternalUnitDefinition) { d.Seed[0].Name = " Phòng" },
		"seed sort negative":   func(d *InternalUnitDefinition) { d.Seed[0].SortOrder = -1 },
		"seed sort too large":  func(d *InternalUnitDefinition) { d.Seed[0].SortOrder = 100001 },
		"seed dept wrong kind": func(d *InternalUnitDefinition) { d.Seed[0].Kind = "tram" },
		"seed bad dept code":   func(d *InternalUnitDefinition) { d.Seed[0].DepartmentCode = "Trong Trot" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := validUnitDefinition()
			mutate(&d)
			if err := ValidateInternalUnitDefinition(d); !errors.Is(err, ErrUnitDelegationDefinition) {
				t.Fatalf("accepted invalid definition: %v", err)
			}
		})
	}
}

func TestDelegationDefinitionValidationFailsClosed(t *testing.T) {
	if err := ValidateDelegationDefinition(validDelegationDefinition()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*DelegationDefinition){
		"zero":              func(d *DelegationDefinition) { *d = DelegationDefinition{} },
		"no powers":         func(d *DelegationDefinition) { d.Powers = nil },
		"duplicate power":   func(d *DelegationDefinition) { d.Powers = append(d.Powers, d.Powers[0]) },
		"bad power":         func(d *DelegationDefinition) { d.Powers[0].Code = "mở" },
		"unlimited":         func(d *DelegationDefinition) { d.MaxDuration = 0 },
		"negative":          func(d *DelegationDefinition) { d.MaxDuration = -time.Second },
		"no grantee roles":  func(d *DelegationDefinition) { d.GranteeRoles = nil },
		"bad grantee role":  func(d *DelegationDefinition) { d.GranteeRoles = []string{"KN"} },
		"duplicate grantee": func(d *DelegationDefinition) { d.GranteeRoles = []string{"kn_tinh", "kn_tinh"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := validDelegationDefinition()
			mutate(&d)
			if err := ValidateDelegationDefinition(d); !errors.Is(err, ErrUnitDelegationDefinition) {
				t.Fatalf("accepted invalid definition: %v", err)
			}
		})
	}
}
