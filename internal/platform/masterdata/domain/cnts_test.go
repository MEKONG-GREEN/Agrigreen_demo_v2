package domain_test

import (
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func TestCNTS_CatalogsRegistered(t *testing.T) {
	for _, c := range domain.CNTSCatalogs {
		if !domain.KnownCatalog(c) || !domain.EffectiveDated(c) || domain.CatalogName(c) == c || len(domain.CatalogAttrs(c)) == 0 {
			t.Fatalf("%s chưa đăng ký đủ", c)
		}
	}
	if domain.EffectiveDated(port.CatalogCrop) {
		t.Fatal("danh mục Trồng trọt không theo hiệu lực")
	}
	for _, c := range domain.CNTSCatalogs {
		if !slices.Contains(domain.SyncCatalogs, c) {
			t.Fatalf("W11.42: danh mục CNTS %s phải đồng bộ xuống thiết bị", c)
		}
	}
}

func TestCNTS_ParseDoses(t *testing.T) {
	ds, err := domain.ParseDoses(" VX_GUMBORO/2@28, VX_ND_F/1@3 ,VX_GUMBORO/1@7")
	if err != nil {
		t.Fatal(err)
	}
	if got := domain.FormatDoses(ds); got != "VX_ND_F/1@3,VX_GUMBORO/1@7,VX_GUMBORO/2@28" {
		t.Fatalf("sắp theo ngày tuổi: %s", got)
	}
	for _, bad := range []string{"", "VX/1", "VX@3", "VX/0@3", "VX/1@-1", "VX/1@4000", "V X/1@3", "VX/1@3,VX/1@5"} {
		if _, err := domain.ParseDoses(bad); err == nil {
			t.Fatalf("%q phải lỗi", bad)
		}
	}
	d := port.Dose{Vaccine: "VX", Shot: 1, AgeDays: 17}
	if got := d.ExpectedDate(domain.Day(2026, 10, 1), 1); !got.Equal(domain.Day(2026, 10, 17)) {
		t.Fatalf("ngày dự kiến = nhận giống + ngày tuổi − tuổi lúc nhận: %v", got)
	}
}

func TestCNTS_FacilityNormalize(t *testing.T) {
	base := map[string]string{
		port.AttrFacilityRef: "0190f1c4-7d3e-7000-8000-000000000001", port.AttrCommuneCode: "30001",
		port.AttrFacilityKind: port.FacilityConcentrated, port.AttrStampsGS: " 91.589.05, 91.589.05.1,91.589.05 ",
	}
	it, err := domain.Item{Catalog: port.CatalogCNTSSlaughterFacility, Code: "CS01", Name: "Cơ sở A", Attrs: base}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if it.Attrs[port.AttrStampsGS] != "91.589.05,91.589.05.1" || len(it.Stamps()) != 2 {
		t.Fatalf("mộc dấu chuẩn hóa: %v", it.Attrs)
	}
	bad := map[string]string{}
	for k, v := range base {
		bad[k] = v
	}
	bad[port.AttrStampsGC] = "91.589.05"
	if _, err := (domain.Item{Catalog: port.CatalogCNTSSlaughterFacility, Code: "CS01", Name: "Cơ sở A", Attrs: bad}).Normalize(); err == nil {
		t.Fatal("một mã không được vừa GS vừa GC")
	}
	delete(bad, port.AttrCommuneCode)
	if _, err := (domain.Item{Catalog: port.CatalogCNTSSlaughterFacility, Code: "CS01", Name: "Cơ sở A", Attrs: bad}).Normalize(); err == nil {
		t.Fatal("thiếu xã (BR-02)")
	}
}

func TestCNTS_ScheduleNeedsTarget(t *testing.T) {
	it := domain.Item{Catalog: port.CatalogCNTSVaccinationSchedule, Code: "QT", Name: "QT", Attrs: map[string]string{port.AttrDoses: "VX/1@3"}}
	if _, err := it.Normalize(); err == nil {
		t.Fatal("quy trình phải gắn loại mô hình hoặc giống")
	}
	it.Attrs["breed"] = "GA_TA"
	if _, err := it.Normalize(); err != nil {
		t.Fatal(err)
	}
}

func TestCNTS_VaccineNeedsSpecies(t *testing.T) {
	it := domain.Item{Catalog: port.CatalogCNTSVaccine, Code: "VX", Name: "VX", Attrs: map[string]string{"report_group": "heo", port.AttrSpecies: " , "}}
	if _, err := it.Normalize(); err == nil {
		t.Fatal("ánh xạ vaccine–loài bắt buộc")
	}
}
