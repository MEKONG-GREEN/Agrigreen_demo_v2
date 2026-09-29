package handler_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// HTX US-YIELD-01 (AC-1 chặn trước giai đoạn thu hoạch của SOP đã ghim; AC-2 đóng vụ
// ngay) và HTX US-YIELD-03 (âm → từ chối; 0 / vượt ngưỡng theo giống → cần xác nhận).
// Sửa sản lượng = bản ghi bù thay khai báo hiện hành.
func TestHarvestDeclare_HTX_US_YIELD_01_03(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1)
	season := fa.mustOpen(f.p1)
	hp := "/v1/cropcycle/seasons/" + season + "/harvest"
	r := fa.post(hp, map[string]any{"mass_kg": "9750"})
	expectCode(t, r, http.StatusConflict, "cropcycle.harvest_too_early", "chưa đến giai đoạn thu hoạch")
	if str(r.body, "harvest_open_date") != "2026-12-28" {
		t.Fatalf("mốc thu hoạch: %s", r.raw)
	}
	f.advanceDays(91)
	expectCode(t, fa.post(hp, map[string]any{"mass_kg": "-5"}), http.StatusUnprocessableEntity, "cropcycle.yield_negative", "âm")
	r = fa.post(hp, map[string]any{"mass_kg": "0"})
	expectCode(t, r, http.StatusConflict, "cropcycle.yield_anomaly", "bằng 0")
	if get(r.body, "needs_confirm") != true || str(r.body, "anomaly_kind") != "zero" {
		t.Fatalf("bất thường 0: %s", r.raw)
	}
	expectCode(t, fa.post(hp, map[string]any{"mass_kg": "30000"}), http.StatusConflict, "cropcycle.yield_anomaly", "20 t/ha > 12")
	// Ngưỡng riêng theo giống (AC-2).
	f.params.set(port.ParamYieldAnomalyMaxTPerHa+":OM5451", "6")
	r = fa.post(hp, map[string]any{"mass_kg": "9750"})
	expectCode(t, r, http.StatusConflict, "cropcycle.yield_anomaly", "6.5 > ngưỡng giống 6")
	if str(r.body, "threshold_t_per_ha") != "6" {
		t.Fatalf("ngưỡng theo giống: %s", r.raw)
	}
	r = fa.post(hp, map[string]any{"mass_kg": "9750", "moisture_pct": "18.5", "confirm_anomaly": true,
		"gps": map[string]any{"lon": "105.6", "lat": "10.05"}})
	expect(t, r, http.StatusCreated, "xác nhận cố ý")
	h1 := str(r.body, "harvest", "id")
	if str(r.body, "harvest", "yield_t_per_ha") != "6.5000" || str(r.body, "harvest", "anomaly_kind") != "above_threshold" ||
		get(r.body, "harvest", "anomaly_confirmed") != true || str(r.body, "season", "status") != "completed" ||
		str(r.body, "harvest", "harvest_date") != "2026-12-29" {
		t.Fatalf("khai sản lượng: %s", r.raw)
	}
	if len(f.outbox(port.EventSeasonCompleted)) != 1 || len(f.outbox(port.EventHarvestDeclared)) != 1 {
		t.Fatal("thiếu season.completed / harvest.declared")
	}
	area := f.outbox(port.EventCoopSeasonAreaChanged)
	if last := area[len(area)-1]; num(last, "completed_seasons") != 1 || num(last, "open_seasons") != 0 || str(last, "area_ha") != "1.5000" {
		t.Fatalf("diện tích vụ sau hoàn thành: %v", area)
	}
	expectCode(t, fa.post(hp, map[string]any{"mass_kg": "9000"}), http.StatusConflict, "cropcycle.season_state", "khai lần hai")
	expectCode(t, fa.log(f.p1, nil), http.StatusConflict, "cropcycle.season_not_open", "ghi nhật ký sau khi đóng vụ")

	cp := "/v1/cropcycle/harvests/" + h1 + "/corrections"
	r = fa.post(cp, map[string]any{"mass_kg": "9000", "correction_reason": "Cân lại tại kho HTX"})
	expect(t, r, http.StatusCreated, "sửa sản lượng")
	if str(r.body, "harvest", "supersedes_id") != h1 || str(r.body, "harvest", "yield_t_per_ha") != "6.0000" || get(r.body, "harvest", "anomaly_kind") != nil {
		t.Fatalf("bản ghi bù sản lượng: %s", r.raw)
	}
	expectCode(t, fa.post(cp, map[string]any{"mass_kg": "8000", "correction_reason": "Sửa trên bản đã bị thay"}),
		http.StatusConflict, "cropcycle.concurrent_edit", "sửa bản cũ")
	d := fa.get("/v1/cropcycle/seasons/" + season)
	if str(d.body, "progress", "actual_kg") != "9000.000" || str(d.body, "progress", "expected_kg") != "10125.000" ||
		str(d.body, "harvest", "id") != str(r.body, "harvest", "id") {
		t.Fatalf("sản lượng dự kiến – thực tế: %s", d.raw)
	}
}

// Nguyên mẫu BR-06: phun thuốc ở bước có thời gian cách ly chặn thu hoạch sớm.
func TestHarvestPHI(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	season := mgr.mustOpen(f.p2)
	f.advanceDays(85)
	expect(t, f.farmer(f.h2).log(f.p2, map[string]any{"activity_code": "phun_thuoc", "sop_step_seq": 4}), http.StatusCreated, "phun thuốc")
	f.advanceDays(6)
	r := mgr.post("/v1/cropcycle/seasons/"+season+"/harvest", map[string]any{"mass_kg": "9750"})
	expectCode(t, r, http.StatusConflict, "cropcycle.phi_violation", "chưa hết cách ly")
	if str(r.body, "earliest_harvest_date") != "2027-01-06" {
		t.Fatalf("ngày sớm nhất: %s", r.raw)
	}
	f.advanceDays(9)
	expect(t, mgr.post("/v1/cropcycle/seasons/"+season+"/harvest", map[string]any{"mass_kg": "9750"}), http.StatusCreated, "hết cách ly")
}

func harvestXLSX(t *testing.T, rows [][]any) []byte {
	t.Helper()
	x := excelize.NewFile()
	header := []any{"Mã lô", "Ngày thu hoạch", "Sản lượng kg", "Độ ẩm"}
	if err := x.SetSheetRow("Sheet1", "A1", &header); err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := x.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if _, err := x.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// HTX US-YIELD-02: nhập sản lượng hàng loạt bằng Excel / bảng nhập nhanh — bỏ qua
// dòng lỗi (thiếu sản lượng, sai định dạng số), chỉ ghi dòng hợp lệ, trả lý do theo dòng.
func TestHarvestImport_HTX_US_YIELD_02(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	mgr.mustOpen(f.p1)
	mgr.mustOpen(f.p2)
	s3 := mgr.mustOpen(f.p3)
	f.advanceDays(91)
	body := harvestXLSX(t, [][]any{
		{"TH0000001", "2026-12-29", 9750, 17},
		{"TH0000002", "2026-12-29", "", ""},
		{"TH0000002", "2026-12-29", "chín tấn", ""},
		{"TH0000009", "2026-12-29", 5000, ""},
	})
	r := mgr.do(f.app.URL, http.MethodPost, "/v1/cropcycle/harvests/import", key(), tabular.ContentTypeXLSX, body)
	expect(t, r, http.StatusOK, "nhập Excel")
	if num(r.body, "accepted") != 1 || num(r.body, "rejected") != 3 {
		t.Fatalf("kết quả nhập: %s", r.raw)
	}
	rows := list(r.body, "rows")
	if str(rows[1], "reason") != "Thiếu sản lượng" || str(rows[2], "reason") != "Sai định dạng số sản lượng" ||
		str(rows[3], "code") != "cropcycle.season_not_open" || num(rows[0], "row") != 2 {
		t.Fatalf("lý do theo dòng: %s", r.raw)
	}
	// Bảng nhập nhanh: một lần bấm, theo mã vụ; dòng bất thường báo needs_confirm.
	r = mgr.post("/v1/cropcycle/harvests/bulk", map[string]any{"rows": []map[string]any{
		{"plot_code": "TH0000002", "mass_kg": "9000"}, {"season_ref": s3, "mass_kg": "0"},
	}})
	expect(t, r, http.StatusOK, "bảng nhập nhanh")
	rows = list(r.body, "rows")
	if num(r.body, "accepted") != 1 || get(rows[1], "needs_confirm") != true {
		t.Fatalf("nhập nhanh: %s", r.raw)
	}
	expectCode(t, f.farmer(f.h1).post("/v1/cropcycle/harvests/bulk", map[string]any{"rows": []map[string]any{{"plot_code": "TH0000003", "mass_kg": "1"}}}),
		http.StatusForbidden, "forbidden", "nông dân không nhập hàng loạt")
	expectCode(t, mgr.post("/v1/cropcycle/harvests/bulk", map[string]any{"cooperative_ref": f.coopB.String(), "rows": []map[string]any{{"plot_code": "X", "mass_kg": "1"}}}),
		http.StatusForbidden, "cropcycle.out_of_scope", "HTX khác")
}

// HTX US-DASH-01: bộ lọc vụ/năm/HTX/giống; quản lý HTX chỉ HTX mình (AC-3); quản trị
// xem toàn bộ hoặc lọc theo HTX (AC-4).
func TestDashboard_HTX_US_DASH_01(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mA, mB := f.managerA(), f.managerB()
	s1 := mA.mustOpen(f.p1)
	mA.mustOpen(f.p2)
	mB.mustOpen(f.p4)
	f.advanceDays(91)
	expect(t, mA.post("/v1/cropcycle/seasons/"+s1+"/harvest", map[string]any{"mass_kg": "9750"}), http.StatusCreated, "khai")
	d := mA.get("/v1/cropcycle/dashboard?season_code=DX&season_year=2026")
	expect(t, d, http.StatusOK, "bảng điều hành HTX A")
	tot := get(d.body, "totals")
	if num(tot, "seasons") != 2 || str(tot, "area_ha") != "3.0000" || str(tot, "expected_kg") != "20250.000" ||
		str(tot, "actual_kg") != "9750.000" || str(tot, "actual_yield_t_per_ha") != "6.5000" || num(tot, "households") != 2 ||
		num(tot, "completed_seasons") != 1 {
		t.Fatalf("thẻ chỉ số HTX A: %s", d.raw)
	}
	if len(list(d.body, "by_variety")) != 1 || str(list(d.body, "by_season")[0], "key") != "DX:2026" {
		t.Fatalf("nhóm: %s", d.raw)
	}
	expectCode(t, mA.get("/v1/cropcycle/dashboard?cooperative_ref="+f.coopB.String()), http.StatusForbidden, "cropcycle.out_of_scope", "HTX A xem HTX B")
	adm := f.platformAdmin()
	if n := num(adm.get("/v1/cropcycle/dashboard").body, "totals", "seasons"); n != 3 {
		t.Fatalf("quản trị xem toàn bộ: %v", n)
	}
	if n := num(adm.get("/v1/cropcycle/dashboard?cooperative_ref="+f.coopB.String()).body, "totals", "seasons"); n != 1 {
		t.Fatalf("quản trị lọc HTX B: %v", n)
	}
	if n := num(mA.get("/v1/cropcycle/dashboard?variety_code=ST25").body, "totals", "seasons"); n != 0 {
		t.Fatalf("lọc giống: %v", n)
	}
}
