package extension

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// NewCNTSCatalogs interprets the KN catalog schemas over the generic capability.
// KN owns the typed contracts; the capability keeps storage, transactions and tenant authority.
func NewCNTSCatalogs(c mdport.EffectiveCatalogs, clk clock.Clock) knport.CNTSCatalogs {
	if c == nil || clk == nil {
		panic("extension: catalog capability and clock are required")
	}
	return cntsCatalogLookup{catalogs: c, clock: clk}
}

type cntsCatalogLookup struct {
	catalogs mdport.EffectiveCatalogs
	clock    clock.Clock
}

var _ knport.CNTSCatalogs = cntsCatalogLookup{}

func (p cntsCatalogLookup) ItemAt(ctx context.Context, catalog, code string, on time.Time) (mdport.CatalogItem, error) {
	v, err := p.catalogs.ItemAt(ctx, catalog, code, on)
	return v.Item, err
}

func (p cntsCatalogLookup) ListAt(ctx context.Context, catalog string, on time.Time) ([]mdport.CatalogItem, error) {
	values, err := p.catalogs.ListAt(ctx, catalog, on)
	if err != nil {
		return nil, err
	}
	out := make([]mdport.CatalogItem, len(values))
	for i, v := range values {
		out[i] = v.Item
	}
	return out, nil
}

func lookupFacility(v mdport.EffectiveCatalogItem) knport.SlaughterFacility {
	it := v.Item
	ref, _ := uuid.Parse(it.Attrs[knport.AttrFacilityRef])
	return knport.SlaughterFacility{
		Ref: ref, Code: it.Code, Name: it.Name, CommuneCode: it.Attrs[knport.AttrCommuneCode], Kind: it.Attrs[knport.AttrFacilityKind],
		StampsGS: catalogCodes(it.Attrs[knport.AttrStampsGS]), StampsGC: catalogCodes(it.Attrs[knport.AttrStampsGC]),
		Species: catalogCodes(it.Attrs[knport.AttrSpecies]), Address: it.Attrs["address"], Active: it.Active, ValidFrom: v.ValidFrom,
	}
}

func (p cntsCatalogLookup) FacilitiesAt(ctx context.Context, on time.Time, communeCodes []string) ([]knport.SlaughterFacility, error) {
	values, err := p.catalogs.ListAt(ctx, knport.CatalogCNTSSlaughterFacility, on)
	if err != nil {
		return nil, err
	}
	out := []knport.SlaughterFacility{}
	for _, v := range values {
		if len(communeCodes) == 0 || slices.Contains(communeCodes, v.Item.Attrs[knport.AttrCommuneCode]) {
			out = append(out, lookupFacility(v))
		}
	}
	return out, nil
}

func (p cntsCatalogLookup) Facility(ctx context.Context, ref uuid.UUID, on time.Time) (knport.SlaughterFacility, error) {
	code, err := p.catalogs.CodeByAttribute(ctx, knport.CatalogCNTSSlaughterFacility, knport.AttrFacilityRef, ref.String())
	if err != nil {
		return knport.SlaughterFacility{}, err
	}
	if code == "" {
		return knport.SlaughterFacility{}, knport.ErrFacilityNotFound
	}
	v, err := p.catalogs.ItemAt(ctx, knport.CatalogCNTSSlaughterFacility, code, on)
	if err != nil {
		return knport.SlaughterFacility{}, err
	}
	return lookupFacility(v), nil
}

func (p cntsCatalogLookup) UnknownFacilities(ctx context.Context, refs []uuid.UUID) ([]uuid.UUID, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	fs, err := p.FacilitiesAt(ctx, p.clock.Now(), nil)
	if err != nil {
		return nil, err
	}
	known := make(map[uuid.UUID]bool, len(fs))
	for _, f := range fs {
		known[f.Ref] = true
	}
	var out []uuid.UUID
	for _, r := range refs {
		if !known[r] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (p cntsCatalogLookup) FacilityCommunes(ctx context.Context, refs []uuid.UUID, on time.Time) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(refs) == 0 {
		return out, nil
	}
	fs, err := p.FacilitiesAt(ctx, on, nil)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		if slices.Contains(refs, f.Ref) {
			out[f.Ref] = f.CommuneCode
		}
	}
	return out, nil
}

func (p cntsCatalogLookup) ScheduleAt(ctx context.Context, code string, on time.Time) (knport.VaccinationSchedule, error) {
	v, err := p.catalogs.ItemAt(ctx, knport.CatalogCNTSVaccinationSchedule, code, on)
	if err != nil {
		return knport.VaccinationSchedule{}, err
	}
	it := v.Item
	ds, err := catalogParseDoses(it.Attrs[knport.AttrDoses])
	if err != nil {
		return knport.VaccinationSchedule{}, err
	}
	return knport.VaccinationSchedule{Code: it.Code, Name: it.Name, ModelType: it.Attrs["model_type"], Breed: it.Attrs["breed"],
		Doses: ds, ValidFrom: v.ValidFrom}, nil
}

func (p cntsCatalogLookup) VaccineSpecies(ctx context.Context, on time.Time) (map[string][]string, error) {
	values, err := p.catalogs.ListAt(ctx, knport.CatalogCNTSVaccine, on)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(values))
	for _, v := range values {
		out[v.Item.Code] = catalogCodes(v.Item.Attrs[knport.AttrSpecies])
	}
	return out, nil
}
