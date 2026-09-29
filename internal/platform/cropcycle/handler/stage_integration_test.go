package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func stagePlots(t *testing.T, r resp) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, x := range list(r.body, "plots") {
		m := x.(map[string]any)
		out[m["plot_ref"].(string)] = m
	}
	return out
}

// GIS US-SEASON-01 (FN-13): giai đoạn theo thửa + giai đoạn trội theo HTX để tô polygon vùng HTX;
// GIS US-YIELD-01 (FN-14): vùng chín rộ trong 1–3 ngày tới; phạm vi như bảng điều hành (hierarchy scope).
func TestStageMap_GIS_US_SEASON_01_US_YIELD_01(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	ma, mb := f.managerA(), f.managerB()
	ma.mustOpen(f.p1)
	ma.mustOpen(f.p3)
	mb.mustOpen(f.p4)

	// Hôm nay (29/09/2026, ngày sạ): sinh trưởng; thu hoạch dự kiến 02/01/2027.
	r := f.platformAdmin().get("/v1/cropcycle/stage-map")
	expect(t, r, http.StatusOK, "đọc lớp giai đoạn")
	if str(r.body, "date") != today || len(list(r.body, "stages")) != 5 {
		t.Fatalf("ngày/chú giải: %s", r.raw)
	}
	plots := stagePlots(t, r)
	if len(plots) != 3 {
		t.Fatalf("muốn 3 thửa có vụ, nhận %d: %s", len(plots), r.raw)
	}
	p1 := plots[f.p1.String()]
	if p1["stage"] != "sinh_truong" || p1["ripening"] != false || p1["expected_harvest_date"] != "2027-01-02" {
		t.Fatalf("thửa p1 hôm nay: %v", p1)
	}

	// 01/01/2027: đã qua cổng thu hoạch (28/12) → "sắp thu", còn 1 ngày → vùng chín rộ.
	r = f.platformAdmin().get("/v1/cropcycle/stage-map?date=2027-01-01")
	expect(t, r, http.StatusOK, "tra ngày 01/01/2027")
	p1 = stagePlots(t, r)[f.p1.String()]
	if p1["stage"] != "sap_thu" || p1["ripening"] != true || num(p1, "days_to_harvest") != 1 {
		t.Fatalf("thửa p1 01/01/2027: %v", p1)
	}
	var coopA map[string]any
	for _, x := range list(r.body, "cooperatives") {
		if m := x.(map[string]any); m["cooperative_ref"] == f.coopA.String() {
			coopA = m
		}
	}
	if coopA == nil || coopA["dominant_stage"] != "sap_thu" || num(coopA, "plot_count") != 2 || num(coopA, "ripening_plot_count") != 2 ||
		coopA["earliest_ripening_date"] != "2027-01-02" || coopA["ripening_area_ha"] != "3.0000" {
		t.Fatalf("tổng hợp HTX A: %v", coopA)
	}

	// Qua ngày thu hoạch dự kiến mà vụ chưa đóng → "chín rộ / đang thu hoạch", không còn cảnh báo "sắp tới".
	p1 = stagePlots(t, must(t, f.platformAdmin().get("/v1/cropcycle/stage-map?date=2027-01-05")))[f.p1.String()]
	if p1["stage"] != "chin_ro" || p1["ripening"] != false {
		t.Fatalf("thửa p1 05/01/2027: %v", p1)
	}

	// Phạm vi: quản lý HTX B chỉ thấy HTX B; lọc HTX khác → 403; KN xã 31117 chỉ thấy HTX A.
	if got := stagePlots(t, must(t, mb.get("/v1/cropcycle/stage-map"))); len(got) != 1 || got[f.p4.String()] == nil {
		t.Fatalf("quản lý HTX B: %v", got)
	}
	expectCode(t, mb.get("/v1/cropcycle/stage-map?cooperative_ref="+f.coopA.String()), http.StatusForbidden, "cropcycle.out_of_scope", "HTX khác")
	if got := stagePlots(t, must(t, f.officerA().get("/v1/cropcycle/stage-map"))); len(got) != 2 || got[f.p4.String()] != nil {
		t.Fatalf("KN xã 31117: %v", got)
	}
	// Tenant khác không thấy gì (tenant isolation).
	other := f.as(ids.New(), uuid.Nil, port.RoleCoopManager)
	if got := must(t, other.get("/v1/cropcycle/stage-map")); len(list(got.body, "plots")) != 0 {
		t.Fatalf("tenant khác thấy dữ liệu: %s", got.raw)
	}
	// Ngày sai dạng / ngoài một năm → 422.
	expect(t, f.platformAdmin().get("/v1/cropcycle/stage-map?date=01-01-2027"), http.StatusUnprocessableEntity, "ngày sai dạng")
	expect(t, f.platformAdmin().get("/v1/cropcycle/stage-map?date=2030-01-01"), http.StatusUnprocessableEntity, "ngoài một năm")
}

func must(t *testing.T, r resp) resp {
	t.Helper()
	expect(t, r, http.StatusOK, "đọc")
	return r
}
