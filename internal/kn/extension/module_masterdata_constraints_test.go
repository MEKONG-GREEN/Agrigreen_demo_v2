package extension

import (
	"reflect"
	"testing"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func TestMasterdataTransactionReferences(t *testing.T) {
	for _, d := range MasterdataCatalogs().Definitions {
		if d.Constraints == nil {
			t.Fatalf("missing explicit constraints: %s", d.Catalog)
		}
	}
	cases := []struct {
		catalog string
		attrs   map[string]string
		want    []mdport.CatalogReference
	}{
		{mdport.CatalogCNTSVaccine, map[string]string{"species": "BO,HEO", "diseases": "D"},
			[]mdport.CatalogReference{{Catalog: mdport.CatalogCNTSDisease, Code: "D", Field: "attrs.diseases"},
				{Catalog: mdport.CatalogCNTSSpecies, Code: "BO", Field: "attrs.species"}, {Catalog: mdport.CatalogCNTSSpecies, Code: "HEO", Field: "attrs.species"}}},
		{mdport.CatalogCNTSVaccinationSchedule, map[string]string{"breed": "B", "model_type": "M", "doses": "V/1@1,V/2@2,W/1@3"},
			[]mdport.CatalogReference{{Catalog: mdport.CatalogCNTSBreed, Code: "B", Field: "attrs.breed"},
				{Catalog: mdport.CatalogCNTSModelType, Code: "M", Field: "attrs.model_type"},
				{Catalog: mdport.CatalogCNTSVaccine, Code: "V", Field: "attrs.doses"}, {Catalog: mdport.CatalogCNTSVaccine, Code: "W", Field: "attrs.doses"}}},
		{mdport.CatalogCNTSChemical, map[string]string{}, nil},
	}
	for _, tc := range cases {
		got, err := catalogTransactionConstraints(tc.catalog).References(tc.attrs)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %+v %v", tc.catalog, got, err)
		}
	}
}
