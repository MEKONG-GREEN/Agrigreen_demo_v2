package extension

import (
	"net/http"
	"sort"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// The owner describes CNTS semantics; masterdata retains authorization, transaction and tenant scope.
func catalogTransactionConstraints(catalog string) *mdport.CatalogConstraints {
	c := &mdport.CatalogConstraints{
		References: func(attrs map[string]string) ([]mdport.CatalogReference, error) {
			return catalogTransactionReferences(catalog, attrs)
		},
	}
	if catalog == mdport.CatalogCNTSSlaughterFacility {
		c.StableUUIDAttribute = mdport.AttrFacilityRef
		c.AdminUnit = &mdport.CatalogAdminUnitConstraint{
			Attribute: mdport.AttrCommuneCode, Level: string(partyport.LevelCommune),
			Issue: mdport.CatalogConstraintIssue{Status: http.StatusUnprocessableEntity, Code: "masterdata.cnts_commune_invalid",
				Title: "Cơ sở giết mổ phải thuộc một xã/phường/đặc khu có hiệu lực tại ngày áp dụng", Field: "attrs.commune_code"},
		}
		c.UniqueTokens = &mdport.CatalogUniqueTokensConstraint{
			Attributes: []string{mdport.AttrStampsGS, mdport.AttrStampsGC}, Separator: ",",
			Issue: mdport.CatalogConstraintIssue{Status: http.StatusConflict, Code: "masterdata.cnts_stamp_taken",
				Title: "Mã số khắc mộc dấu đã thuộc cơ sở giết mổ khác", Field: "attrs.stamp_codes"},
		}
	}
	return c
}

func catalogTransactionReferences(catalog string, attrs map[string]string) ([]mdport.CatalogReference, error) {
	catalogs := map[string]map[string]string{
		mdport.CatalogCNTSBreed:               {mdport.AttrSpecies: mdport.CatalogCNTSSpecies},
		mdport.CatalogCNTSDisease:             {mdport.AttrSpecies: mdport.CatalogCNTSSpecies},
		mdport.CatalogCNTSVaccine:             {mdport.AttrSpecies: mdport.CatalogCNTSSpecies, "diseases": mdport.CatalogCNTSDisease},
		mdport.CatalogCNTSSlaughterFacility:   {mdport.AttrSpecies: mdport.CatalogCNTSSpecies},
		mdport.CatalogCNTSModelType:           {mdport.AttrSpecies: mdport.CatalogCNTSSpecies},
		mdport.CatalogCNTSVaccinationSchedule: {"model_type": mdport.CatalogCNTSModelType, "breed": mdport.CatalogCNTSBreed},
	}
	refs := catalogs[catalog]
	keys := make([]string, 0, len(refs))
	for a := range refs {
		keys = append(keys, a)
	}
	sort.Strings(keys)
	var out []mdport.CatalogReference
	for _, a := range keys {
		for _, code := range catalogCodes(attrs[a]) {
			out = append(out, mdport.CatalogReference{Catalog: refs[a], Code: code, Field: "attrs." + a})
		}
	}
	if catalog == mdport.CatalogCNTSVaccinationSchedule {
		doses, err := catalogParseDoses(attrs[mdport.AttrDoses])
		if err != nil {
			return nil, mdport.FieldError{Field: "attrs.doses", Err: err}
		}
		seen := map[string]bool{}
		for _, d := range doses {
			if seen[d.Vaccine] {
				continue
			}
			seen[d.Vaccine] = true
			out = append(out, mdport.CatalogReference{Catalog: mdport.CatalogCNTSVaccine, Code: d.Vaccine, Field: "attrs.doses"})
		}
	}
	return out, nil
}
