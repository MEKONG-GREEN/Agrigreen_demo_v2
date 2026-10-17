package masterdatacatalogs_test

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

func ownedCatalogModule() *masterdata.Module {
	return masterdata.New(nil, clock.System{}, masterdata.Deps{Catalogs: []port.CatalogRegistration{extension.MasterdataCatalogs()}})
}

func normalizeOwnedCatalogItem(it port.CatalogItem) (port.CatalogItem, error) {
	return ownedCatalogModule().NormalizeCatalogItem(it)
}

func TestCNTS_CatalogsRegistered(t *testing.T) {
	catalogs := []string{
		port.CatalogCNTSSpecies, port.CatalogCNTSBreed, port.CatalogCNTSVaccine, port.CatalogCNTSVaccinationSchedule,
		port.CatalogCNTSDisease, port.CatalogCNTSChemical, port.CatalogCNTSSlaughterFacility, port.CatalogCNTSAquacultureObject,
		port.CatalogCNTSFishingGroup, port.CatalogCNTSIndicator, port.CatalogCNTSModelType, port.CatalogCNTSDocumentType,
	}
	metadata := map[string]port.CatalogMetadata{}
	for _, m := range ownedCatalogModule().CatalogMetadata() {
		metadata[m.Catalog] = m
	}
	for _, c := range catalogs {
		m, ok := metadata[c]
		if !ok || !m.EffectiveDated || m.Name == c || m.Name == "" || len(m.Attributes) == 0 {
			t.Fatalf("%s chưa đăng ký đủ", c)
		}
		if !m.Sync {
			t.Fatalf("W11.42: danh mục CNTS %s phải đồng bộ xuống thiết bị", c)
		}
	}
	if metadata[port.CatalogCrop].EffectiveDated {
		t.Fatal("danh mục Trồng trọt không theo hiệu lực")
	}
}

func TestCNTS_FacilityNormalize(t *testing.T) {
	base := map[string]string{
		port.AttrFacilityRef: "0190f1c4-7d3e-7000-8000-000000000001", port.AttrCommuneCode: "30001",
		port.AttrFacilityKind: port.FacilityConcentrated, port.AttrStampsGS: " 91.589.05, 91.589.05.1,91.589.05 ",
	}
	it, err := normalizeOwnedCatalogItem(port.CatalogItem{Catalog: port.CatalogCNTSSlaughterFacility, Code: "CS01", Name: "Cơ sở A", Attrs: base})
	if err != nil {
		t.Fatal(err)
	}
	if it.Attrs[port.AttrStampsGS] != "91.589.05,91.589.05.1" || it.Attrs[port.AttrStampsGC] != "" {
		t.Fatalf("mộc dấu chuẩn hóa: %v", it.Attrs)
	}
	bad := map[string]string{}
	for k, v := range base {
		bad[k] = v
	}
	bad[port.AttrStampsGC] = "91.589.05"
	if _, err := normalizeOwnedCatalogItem(port.CatalogItem{Catalog: port.CatalogCNTSSlaughterFacility, Code: "CS01", Name: "Cơ sở A", Attrs: bad}); err == nil {
		t.Fatal("một mã không được vừa GS vừa GC")
	}
	delete(bad, port.AttrStampsGC)
	delete(bad, port.AttrCommuneCode)
	if _, err := normalizeOwnedCatalogItem(port.CatalogItem{Catalog: port.CatalogCNTSSlaughterFacility, Code: "CS01", Name: "Cơ sở A", Attrs: bad}); err == nil {
		t.Fatal("thiếu xã (BR-02)")
	}
}

func TestCNTS_ScheduleNeedsTarget(t *testing.T) {
	it := port.CatalogItem{Catalog: port.CatalogCNTSVaccinationSchedule, Code: "QT", Name: "QT", Attrs: map[string]string{port.AttrDoses: "VX/1@3"}}
	if _, err := normalizeOwnedCatalogItem(it); err == nil {
		t.Fatal("quy trình phải gắn loại mô hình hoặc giống")
	}
	it.Attrs["breed"] = "GA_TA"
	if _, err := normalizeOwnedCatalogItem(it); err != nil {
		t.Fatal(err)
	}
}

func TestCNTS_VaccineNeedsSpecies(t *testing.T) {
	it := port.CatalogItem{Catalog: port.CatalogCNTSVaccine, Code: "VX", Name: "VX", Attrs: map[string]string{"report_group": "heo", port.AttrSpecies: " , "}}
	if _, err := normalizeOwnedCatalogItem(it); err == nil {
		t.Fatal("ánh xạ vaccine–loài bắt buộc")
	}
}
