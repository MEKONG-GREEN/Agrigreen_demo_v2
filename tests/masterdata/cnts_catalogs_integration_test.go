package masterdata_test

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"

	"github.com/google/uuid"

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

	// Temporary legacy comparison: every field, slice order and public error identity.
	legacy := f.mod.CNTSCatalogs()
	for _, on := range []string{"2026-08-31", "2026-09-30", "2026-10-01"} {
		at := day(on)
		oldFacility, oldErr := legacy.Facility(ctx, uuid.MustParse(ref), at)
		newFacility, newErr := cnts.Facility(ctx, uuid.MustParse(ref), at)
		assertCatalogLookupEquivalent(t, "facility/"+on, oldFacility, oldErr, newFacility, newErr)
		oldSchedule, oldErr := legacy.ScheduleAt(ctx, "QT_GA_THUONG_PHAM", at)
		newSchedule, newErr := cnts.ScheduleAt(ctx, "QT_GA_THUONG_PHAM", at)
		assertCatalogLookupEquivalent(t, "schedule/"+on, oldSchedule, oldErr, newSchedule, newErr)
		oldItems, oldErr := legacy.ListAt(ctx, port.CatalogCNTSSlaughterFacility, at)
		newItems, newErr := cnts.ListAt(ctx, port.CatalogCNTSSlaughterFacility, at)
		assertCatalogLookupEquivalent(t, "list/"+on, oldItems, oldErr, newItems, newErr)
		oldFacilities, oldErr := legacy.FacilitiesAt(ctx, at, []string{"30292"})
		newFacilities, newErr := cnts.FacilitiesAt(ctx, at, []string{"30292"})
		assertCatalogLookupEquivalent(t, "filtered/"+on, oldFacilities, oldErr, newFacilities, newErr)
	}
	oldItem, oldErr := legacy.ItemAt(ctx, port.CatalogCNTSSpecies, "HEO", day("2026-10-01"))
	newItem, newErr := cnts.ItemAt(ctx, port.CatalogCNTSSpecies, "HEO", day("2026-10-01"))
	assertCatalogLookupEquivalent(t, "item", oldItem, oldErr, newItem, newErr)
	oldMissing, oldErr := legacy.Facility(ctx, other, day("2026-10-01"))
	newMissing, newErr := cnts.Facility(ctx, other, day("2026-10-01"))
	assertCatalogLookupEquivalent(t, "facility-missing", oldMissing, oldErr, newMissing, newErr)
	oldSpecies, oldErr := legacy.VaccineSpecies(ctx, day("2026-10-01"))
	newSpecies, newErr := cnts.VaccineSpecies(ctx, day("2026-10-01"))
	assertCatalogLookupEquivalent(t, "vaccine-species", oldSpecies, oldErr, newSpecies, newErr)
	oldUnknown, oldErr := legacy.UnknownFacilities(ctx, []uuid.UUID{uuid.MustParse(ref), other})
	newUnknown, newErr := cnts.UnknownFacilities(ctx, []uuid.UUID{uuid.MustParse(ref), other})
	assertCatalogLookupEquivalent(t, "unknown", oldUnknown, oldErr, newUnknown, newErr)
	oldCommunes, oldErr := legacy.FacilityCommunes(ctx, []uuid.UUID{uuid.MustParse(ref), other}, day("2026-10-01"))
	newCommunes, newErr := cnts.FacilityCommunes(ctx, []uuid.UUID{uuid.MustParse(ref), other}, day("2026-10-01"))
	assertCatalogLookupEquivalent(t, "communes", oldCommunes, oldErr, newCommunes, newErr)

	// valid_from chỉ nhận ở danh mục CNTS.
	if st, _ := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/material/items", map[string]any{
		"code": "VT_X", "name": "Vật tư X", "attrs": map[string]string{"unit": "kg"}, "valid_from": "2026-10-01",
	}); st != http.StatusUnprocessableEntity {
		t.Fatalf("valid_from ở danh mục không theo hiệu lực: %d", st)
	}
}

func assertCatalogLookupEquivalent[T any](t *testing.T, label string, old T, oldErr error, got T, gotErr error) {
	t.Helper()
	if !reflect.DeepEqual(old, got) {
		t.Fatalf("%s values differ: old=%+v new=%+v", label, old, got)
	}
	if (oldErr == nil) != (gotErr == nil) || oldErr != nil && oldErr.Error() != gotErr.Error() {
		t.Fatalf("%s errors differ: old=%v new=%v", label, oldErr, gotErr)
	}
	for _, sentinel := range []error{port.ErrNotEffective, port.ErrFacilityNotFound, port.ErrCatalogItemNotFound} {
		if errors.Is(oldErr, sentinel) != errors.Is(gotErr, sentinel) {
			t.Fatalf("%s public error identity changed: %v", label, sentinel)
		}
	}
}
