package masterdata_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"

	"github.com/google/uuid"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// W11.41 (BRD CNTS v3.7 FN-06, D4 CN1; pf 000146): danh mục CNTS nạp sẵn (loài, vaccine + ánh xạ loài, quy trình
// tiêm theo ngày tuổi), cơ sở giết mổ nhiều mã mộc dấu GS/GC theo hiệu lực từ ngày (AC-02), mã mộc dấu không trùng
// giữa cơ sở, facility_ref cố định qua lần sửa, cổng CNTSCatalogs cho W11.07/W11.42.
func TestW1141_CNTSCatalogs(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	reader := knOfficer(t, f, tid)
	cnts := extension.NewCNTSCatalogs(f.mod.EffectiveCatalogs(), clock.System{})
	ctx := admin.ctx()

	// Nạp sẵn: 9 cột tiêm phòng CN-02 kèm ánh xạ vaccine–loài; quy trình gà thương phẩm.
	vs, err := cnts.VaccineSpecies(ctx, day("2026-10-12"))
	if err != nil {
		t.Fatal(err)
	}
	if got := vs["VX_VDNC_BO"]; len(got) != 2 || got[0] != "BO" || got[1] != "TRAU" {
		t.Fatalf("ánh xạ VDNC bò → trâu + bò: %v", vs)
	}
	sch, err := cnts.ScheduleAt(ctx, "QT_GA_THUONG_PHAM", day("2026-10-12"))
	if err != nil || len(sch.Doses) != 9 || sch.Doses[0].AgeDays != 3 || sch.Doses[8].Vaccine != "VX_ND_M" {
		t.Fatalf("quy trình tiêm: %+v %v", sch, err)
	}
	if st, body := reader.get("/v1/masterdata/catalogs/cnts_species/HEO"); st != http.StatusOK || body["name"] != "Heo" {
		t.Fatalf("Cấp Tỉnh đọc danh mục: %d %v", st, body)
	}

	// Tham chiếu phải đang dùng: vaccine trỏ loài không có → 422.
	if st, body := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/cnts_vaccine/items", map[string]any{
		"code": "VX_X", "name": "Vaccine X", "attrs": map[string]string{"species": "KHONG_CO", "report_group": "khac"},
	}); st != http.StatusUnprocessableEntity || body["code"] != "masterdata.catalog_ref_invalid" {
		t.Fatalf("loài không có: %d %v", st, body)
	}

	// Cơ sở A: chỉ mộc dấu GS từ 01/09; xã phải có hiệu lực tại ngày áp dụng.
	facility := func(code, gs, gc, from string) (int, map[string]any) {
		attrs := map[string]string{"commune_code": "30292", "facility_kind": "tap_trung", "species": "HEO,TRAU", "stamp_codes_gs": gs}
		if gc != "" {
			attrs["stamp_codes_gc"] = gc
		}
		return admin.adm(http.MethodPost, "/v1/masterdata/catalogs/cnts_slaughter_facility/items", map[string]any{
			"code": code, "name": "Cơ sở " + code, "attrs": attrs, "valid_from": from,
		})
	}
	st, body := facility("CSA", "91.589.05, 91.589.05.1", "", "2026-09-01")
	if st != http.StatusCreated || body["valid_from"] != "2026-09-01" {
		t.Fatalf("tạo cơ sở: %d %v", st, body)
	}
	ref := body["attrs"].(map[string]any)["facility_ref"].(string)
	if _, err := uuid.Parse(ref); err != nil {
		t.Fatalf("facility_ref do hệ thống cấp: %v", body)
	}
	if st, body := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/cnts_slaughter_facility/items", map[string]any{
		"code": "CSX", "name": "Cơ sở X", "valid_from": "2026-09-01",
		"attrs": map[string]string{"commune_code": "89", "facility_kind": "nho_le"},
	}); st != http.StatusUnprocessableEntity || body["code"] != "masterdata.cnts_commune_invalid" {
		t.Fatalf("cơ sở thuộc một xã (BR-02): %d %v", st, body)
	}
	if st, body := facility("CSB", "91.589.05.1", "", "2026-09-01"); st != http.StatusConflict || body["code"] != "masterdata.cnts_stamp_taken" {
		t.Fatalf("mã mộc dấu đã thuộc cơ sở khác: %d %v", st, body)
	}

	// Thêm GC từ 01/10 (AC-02): tháng 9 không có gia cầm, tháng 10 có; ref giữ nguyên dù bên gọi gửi ref khác.
	st, body = admin.adm(http.MethodPut, "/v1/masterdata/catalogs/cnts_slaughter_facility/items/CSA", map[string]any{
		"name": "Cơ sở CSA", "valid_from": "2026-10-01",
		"attrs": map[string]string{"commune_code": "30292", "facility_kind": "tap_trung", "species": "GA,HEO,TRAU",
			"stamp_codes_gs": "91.589.05,91.589.05.1", "stamp_codes_gc": "91.589.06", "facility_ref": uuid.NewString()},
	})
	if st != http.StatusOK || body["attrs"].(map[string]any)["facility_ref"] != ref {
		t.Fatalf("sửa cơ sở giữ facility_ref: %d %v", st, body)
	}
	sep, err := cnts.Facility(ctx, uuid.MustParse(ref), day("2026-09-30"))
	if err != nil || len(sep.StampsGC) != 0 || len(sep.StampsGS) != 2 || sep.CommuneCode != "30292" {
		t.Fatalf("tháng 9 chưa có GC: %+v %v", sep, err)
	}
	oct, err := cnts.Facility(ctx, uuid.MustParse(ref), day("2026-10-01"))
	if err != nil || len(oct.StampsGC) != 1 || oct.StampsGC[0] != "91.589.06" {
		t.Fatalf("tháng 10 có GC: %+v %v", oct, err)
	}
	if _, err := cnts.Facility(ctx, uuid.MustParse(ref), day("2026-08-31")); !errors.Is(err, port.ErrNotEffective) {
		t.Fatalf("trước ngày tạo chưa có hiệu lực: %v", err)
	}
	if fs, err := cnts.FacilitiesAt(ctx, day("2026-10-05"), []string{"30292"}); err != nil || len(fs) != 1 {
		t.Fatalf("cơ sở theo xã: %+v %v", fs, err)
	}
	st, body = reader.get("/v1/masterdata/catalogs/cnts_slaughter_facility/CSA?at=2026-09-15")
	if st != http.StatusOK || body["attrs"].(map[string]any)["stamp_codes_gc"] != nil || body["valid_from"] != "2026-09-01" {
		t.Fatalf("đọc theo ngày qua API: %d %v", st, body)
	}
	st, body = admin.adm(http.MethodGet, "/v1/masterdata/catalogs/cnts_slaughter_facility/items/CSA/versions", nil)
	items, _ := body["items"].([]any)
	if st != http.StatusOK || len(items) != 2 || items[0].(map[string]any)["valid_to"] != "2026-10-01" {
		t.Fatalf("phiên bản hiệu lực: %d %v", st, body)
	}

	// Cổng kiểm phạm vi Cấp Xã theo cơ sở (W11.07).
	other := uuid.New()
	unknown, err := cnts.UnknownFacilities(ctx, []uuid.UUID{uuid.MustParse(ref), other})
	if err != nil || len(unknown) != 1 || unknown[0] != other {
		t.Fatalf("ref lạ: %v %v", unknown, err)
	}

	// Compare against all 17 legacy results frozen independently in package C.
	contract := newLookupContract(t, ref, other.String())
	for _, on := range []string{"2026-08-31", "2026-09-30", "2026-10-01"} {
		at := day(on)
		facility, lookupErr := cnts.Facility(ctx, uuid.MustParse(ref), at)
		contract.check("facility/"+on, facility, lookupErr)
		schedule, lookupErr := cnts.ScheduleAt(ctx, "QT_GA_THUONG_PHAM", at)
		contract.check("schedule/"+on, schedule, lookupErr)
		items, lookupErr := cnts.ListAt(ctx, knport.CatalogCNTSSlaughterFacility, at)
		contract.check("list/"+on, items, lookupErr)
		facilities, lookupErr := cnts.FacilitiesAt(ctx, at, []string{"30292"})
		contract.check("filtered/"+on, facilities, lookupErr)
	}
	item, lookupErr := cnts.ItemAt(ctx, knport.CatalogCNTSSpecies, "HEO", day("2026-10-01"))
	contract.check("item", item, lookupErr)
	missing, lookupErr := cnts.Facility(ctx, other, day("2026-10-01"))
	contract.check("facility-missing", missing, lookupErr)
	species, lookupErr := cnts.VaccineSpecies(ctx, day("2026-10-01"))
	contract.check("vaccine-species", species, lookupErr)
	frozenUnknown, lookupErr := cnts.UnknownFacilities(ctx, []uuid.UUID{uuid.MustParse(ref), other})
	contract.check("unknown", frozenUnknown, lookupErr)
	communes, lookupErr := cnts.FacilityCommunes(ctx, []uuid.UUID{uuid.MustParse(ref), other}, day("2026-10-01"))
	contract.check("communes", communes, lookupErr)

	// BF-CN-SLAUGHTER #169: dữ liệu nền có 4 CSGM mẫu, mỗi cơ sở thuộc một xã có trong danh mục ĐVHC của tenant.
	seededFacilities(t, admin)
	// Xã không có trong ĐVHC bị từ chối cả khi nạp tệp (FN-01 AC-02): dòng đó bị loại kèm lý do, dòng hợp lệ vẫn tạo.
	csv := "catalog,code,name,valid_from,commune_code,facility_kind\n" +
		"cnts_slaughter_facility,CS_NHAP_OK,Cơ sở nhập hợp lệ,2026-10-01,30316,nho_le\n" +
		"cnts_slaughter_facility,CS_NHAP_LA,Cơ sở xã lạ,2026-10-01,99999,nho_le\n"
	st, body = admin.adm(http.MethodPost, "/v1/masterdata/catalog-imports", []byte(csv))
	rows, _ := body["rows"].([]any)
	if st != http.StatusOK || body["created"] != 1.0 || body["rejected"] != 1.0 || len(rows) != 2 ||
		rows[1].(map[string]any)["code"] != "CS_NHAP_LA" || rows[1].(map[string]any)["reason"] != "Cơ sở giết mổ phải thuộc một xã/phường/đặc khu có hiệu lực tại ngày áp dụng" {
		t.Fatalf("nạp tệp có xã lạ: %d %v", st, body)
	}
	if st, _ := reader.get("/v1/masterdata/catalogs/cnts_slaughter_facility/CS_NHAP_LA"); st != http.StatusNotFound {
		t.Fatalf("dòng xã lạ không được tạo: %d", st)
	}
	if st, body := reader.get("/v1/masterdata/catalogs/cnts_slaughter_facility/CS_NHAP_OK"); st != http.StatusOK || body["attrs"].(map[string]any)["commune_code"] != "30316" {
		t.Fatalf("dòng xã hợp lệ được tạo: %d %v", st, body)
	}
	// Mã tỉnh (không phải cấp xã) và xã đã hết hiệu lực tại ngày áp dụng cũng bị từ chối.
	for _, tc := range []struct{ commune, from string }{{"91", "2026-10-01"}, {"30289", "2026-10-01"}, {"30289", "2025-07-01"}} {
		st, body := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/cnts_slaughter_facility/items", map[string]any{
			"code": "CS_SAI_XA", "name": "Cơ sở sai xã", "valid_from": tc.from,
			"attrs": map[string]string{"commune_code": tc.commune, "facility_kind": "nho_le"}})
		constraintIssue(t, st, body, http.StatusUnprocessableEntity, "masterdata.cnts_commune_invalid",
			"Cơ sở giết mổ phải thuộc một xã/phường/đặc khu có hiệu lực tại ngày áp dụng", "attrs.commune_code", tc.commune)
	}

	// valid_from chỉ nhận ở danh mục CNTS.
	if st, _ := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/material/items", map[string]any{
		"code": "VT_X", "name": "Vật tư X", "attrs": map[string]string{"unit": "kg"}, "valid_from": "2026-10-01",
	}); st != http.StatusUnprocessableEntity {
		t.Fatalf("valid_from ở danh mục không theo hiệu lực: %d", st)
	}
}

// BF-CN-CATALOG #170 (CN-11): ngưỡng mặc định gắn từng chỉ tiêu theo dõi (theo ngày hiệu lực), tệp "Xuất danh mục" nạp lại
// được (cột trang_thai bỏ qua), dữ liệu nền có giống, loại mô hình gà chuyên trứng / vịt xiêm, vaccine và quy trình BM-07.
func TestBFCNCatalog_IndicatorThresholdsSeedsExport(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	reader := knOfficer(t, f, tid)
	cnts := extension.NewCNTSCatalogs(f.mod.EffectiveCatalogs(), clock.System{})
	ctx := admin.ctx()

	// Dữ liệu nền BM-07.
	on := day("2026-10-28")
	for _, s := range []struct{ code, model, first string }{{"QT_GA_CHUYEN_TRUNG", "GA_CHUYEN_TRUNG", "VX_ND_F"}, {"QT_VIT_XIEM", "VIT_XIEM", "VX_VIEM_GAN_VIT"}} {
		sch, err := cnts.ScheduleAt(ctx, s.code, on)
		if err != nil || sch.ModelType != s.model || len(sch.Doses) < 5 || sch.Doses[0].Vaccine != s.first {
			t.Fatalf("quy trình %s: %+v %v", s.code, sch, err)
		}
	}
	for _, it := range []struct{ catalog, code string }{{knport.CatalogCNTSBreed, "VIT_XIEM_PHAP"}, {knport.CatalogCNTSBreed, "GA_TRUNG_ISA_BROWN"},
		{knport.CatalogCNTSVaccine, "VX_IB"}, {knport.CatalogCNTSVaccine, "VX_DICH_TA_VIT"}, {knport.CatalogCNTSVaccine, "VX_THT_VIT"},
		{knport.CatalogCNTSIndicator, "DO_MAN"}} {
		if _, err := cnts.ItemAt(ctx, it.catalog, it.code, on); err != nil {
			t.Fatalf("dữ liệu nền %s/%s: %v", it.catalog, it.code, err)
		}
	}
	ph, err := cnts.ItemAt(ctx, knport.CatalogCNTSIndicator, "PH", on)
	if err != nil || ph.Attrs[knport.AttrThresholdMin] != "7.5" || ph.Attrs[knport.AttrThresholdMax] != "8.5" || ph.Attrs[knport.AttrDiaryMetric] != "ph" {
		t.Fatalf("ngưỡng mặc định pH: %+v %v", ph, err)
	}

	// Chỉ tiêu mới kèm ngưỡng, sửa ngưỡng theo ngày hiệu lực (BR-03); số mũ, số âm, chuỗi dài, tối thiểu > tối đa bị từ chối.
	create := func(code, lo, hi, from string) (int, map[string]any) {
		return admin.adm(http.MethodPost, "/v1/masterdata/catalogs/cnts_indicator/items", map[string]any{"code": code, "name": "Nhiệt độ ao " + code,
			"valid_from": from, "attrs": map[string]string{"value_type": "decimal", "unit": "°C", "diary_metric": "temp_c", "threshold_min": lo, "threshold_max": hi}})
	}
	for _, bad := range [][2]string{{"1e100000", "32"}, {"-1", "32"}, {"25", strings.Repeat("9", 5000)}, {"33", "32"}, {"25", "1.23456"}} {
		if st, body := create("NHIET_DO_SAI", bad[0], bad[1], "2026-10-01"); st != http.StatusUnprocessableEntity {
			t.Fatalf("ngưỡng %v phải bị từ chối: %d %v", bad, st, body)
		}
	}
	if st, body := create("NHIET_DO_AO", "25,0", "32", "2026-10-01"); st != http.StatusCreated || body["attrs"].(map[string]any)["threshold_min"] != "25" {
		t.Fatalf("chỉ tiêu mới có ngưỡng: %d %v", st, body)
	}
	if st, body := admin.adm(http.MethodPut, "/v1/masterdata/catalogs/cnts_indicator/items/NHIET_DO_AO", map[string]any{"name": "Nhiệt độ ao NHIET_DO_AO",
		"valid_from": "2026-11-01", "attrs": map[string]string{"value_type": "decimal", "unit": "°C", "diary_metric": "temp_c", "threshold_min": "26", "threshold_max": "31"}}); st != http.StatusOK {
		t.Fatalf("sửa ngưỡng từ 01/11: %d %v", st, body)
	}
	for at, want := range map[string]string{"2026-10-15": "32", "2026-11-02": "31"} {
		it, err := cnts.ItemAt(ctx, knport.CatalogCNTSIndicator, "NHIET_DO_AO", day(at))
		if err != nil || it.Attrs[knport.AttrThresholdMax] != want {
			t.Fatalf("ngưỡng theo ngày %s: %+v %v", at, it, err)
		}
	}

	// Tệp xuất (cùng cột tệp mẫu + trang_thai) nạp lại: mục đã có cùng nội dung → bỏ qua; dòng mới → tạo.
	csv := "catalog,code,name,valid_from,unit,value_type,threshold_min,threshold_max,diary_metric,description,trang_thai\n" +
		"cnts_indicator,PH,pH,2000-01-01,,decimal,7.5,8.5,ph,\"Ngưỡng mặc định 7,5–8,5 (tham số cnts.indicator_min/max:PH)\",Đang dùng\n" +
		"cnts_indicator,DO_TRONG,Độ trong,2026-10-01,cm,decimal,30,60,,,Ngừng dùng\n"
	st, body := admin.adm(http.MethodPost, "/v1/masterdata/catalog-imports", []byte(csv))
	if st != http.StatusOK || body["skipped"] != 1.0 || body["created"] != 1.0 || body["rejected"] != 0.0 {
		t.Fatalf("nạp lại tệp xuất: %d %v", st, body)
	}
	if st, body := reader.get("/v1/masterdata/catalogs/cnts_indicator/DO_TRONG"); st != http.StatusOK || body["attrs"].(map[string]any)["trang_thai"] != nil {
		t.Fatalf("cột trạng thái không thành thuộc tính: %d %v", st, body)
	}
}

// seededFacilities: 4 CSGM mẫu của dữ liệu nền (BF-CN-SLAUGHTER #169) có đủ trường CN-10 và mã xã là xã An Giang còn hiệu lực.
func seededFacilities(t *testing.T, admin caller) {
	t.Helper()
	want := map[string]string{"CSGM_MAU_01": "30316", "CSGM_MAU_02": "30742", "CSGM_MAU_03": "30628", "CSGM_MAU_04": "30463"}
	st, body := admin.adm(http.MethodGet, "/v1/masterdata/catalogs/cnts_slaughter_facility/items?at=2026-10-28", nil)
	items, _ := body["items"].([]any)
	if st != http.StatusOK {
		t.Fatalf("danh sách CSGM: %d %v", st, body)
	}
	seen := 0
	for _, raw := range items {
		it := raw.(map[string]any)
		commune, ok := want[it["code"].(string)]
		if !ok {
			continue
		}
		seen++
		attrs := it["attrs"].(map[string]any)
		if attrs["commune_code"] != commune || attrs["facility_kind"] == nil || attrs["species"] == nil || attrs["address"] == nil || attrs["facility_ref"] == nil {
			t.Fatalf("CSGM mẫu thiếu trường: %v", it)
		}
		if attrs["stamp_codes_gs"] == nil && attrs["stamp_codes_gc"] == nil {
			t.Fatalf("CSGM mẫu không có mộc dấu: %v", it)
		}
		st, unit := admin.get("/v1/masterdata/admin-units/" + commune)
		if st != http.StatusOK || unit["level"] != "commune" || unit["parent_code"] != "91" {
			t.Fatalf("xã của CSGM mẫu %s: %d %v", commune, st, unit)
		}
	}
	if seen != len(want) {
		t.Fatalf("dữ liệu nền phải có %d CSGM mẫu, thấy %d", len(want), seen)
	}
}
