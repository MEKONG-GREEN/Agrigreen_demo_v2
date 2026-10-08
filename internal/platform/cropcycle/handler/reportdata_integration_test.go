package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// reportSeed: HTX A mở 3 lô (h1 hai lô p1, p5; h2 lô p2), khai sản lượng p1; HTX B mở p4.
func reportSeed(t *testing.T) *fixture {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mA, mB := f.managerA(), f.managerB()
	s1 := mA.mustOpen(f.p1)
	mA.mustOpen(f.p2)
	mA.mustOpen(f.p5)
	mB.mustOpen(f.p4)
	f.advanceDays(91)
	expect(t, mA.post("/v1/cropcycle/seasons/"+s1+"/harvest", map[string]any{"mass_kg": "9750.125"}), http.StatusCreated, "khai")
	return f
}

func decEq(t *testing.T, what string, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(decimal.RequireFromString(want)) {
		t.Errorf("%s = %s, muốn %s", what, got, want)
	}
}

// HTX US-DASH-02 AC-1: cổng báo cáo sản lượng trả ĐÚNG số của bảng điều hành (cùng bộ lọc) —
// quản lý HTX chỉ HTX mình, decimal/kg chính xác, số hộ đếm phân biệt (không cộng giữa nhóm).
func TestReportData_Production_HTX_US_DASH_02(t *testing.T) {
	f := reportSeed(t)
	rd := f.mod.ReportData()
	ctx := context.Background()
	d, err := rd.Production(ctx, cropcyclesrc.ProductionQuery{TenantID: f.tenant, CoopOnly: true, CooperativeRef: f.coopA,
		SeasonCode: "DX", SeasonYear: 2026, ByCooperative: true, BySeason: true, ByVariety: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rows) != 1 {
		t.Fatalf("một dòng HTX A × DX 2026 × OM5451: %+v", d.Rows)
	}
	r := d.Rows[0]
	if r.CooperativeRef != f.coopA || r.SeasonCode != "DX" || r.SeasonYear != 2026 || r.VarietyCode != "OM5451" {
		t.Fatalf("chiều phân nhóm: %+v", r)
	}
	tot := d.Totals
	if tot.Seasons != 3 || tot.OpenSeasons != 2 || tot.CompletedSeasons != 1 || tot.Households != 2 {
		t.Fatalf("đếm lô / hộ (h1 hai lô → 2 hộ): %+v", tot)
	}
	decEq(t, "diện tích", tot.AreaHa, "4.5")
	decEq(t, "diện tích đã khai", tot.HarvestedAreaHa, "1.5")
	if tot.ExpectedKg.String() != "30375.000" || tot.ActualKg.String() != "9750.125" {
		t.Fatalf("kg decimal chính xác (mass.Kg): dự kiến %s, thực tế %s", tot.ExpectedKg, tot.ActualKg)
	}
	if tot.ActualYieldTPerHa == nil || tot.ActualYieldTPerHa.String() != "6.5001" {
		t.Fatalf("năng suất thực tế 9750.125 kg / 1.5 ha: %v", tot.ActualYieldTPerHa)
	}
	// Khớp bảng điều hành (HTX US-DASH-01) cùng bộ lọc.
	dash := f.managerA().get("/v1/cropcycle/dashboard?season_code=DX&season_year=2026")
	expect(t, dash, http.StatusOK, "bảng điều hành")
	dt := get(dash.body, "totals")
	if str(dt, "expected_kg") != tot.ExpectedKg.String() || str(dt, "actual_kg") != tot.ActualKg.String() ||
		int64(num(dt, "households")) != tot.Households || !decimal.RequireFromString(str(dt, "area_ha")).Equal(tot.AreaHa) {
		t.Fatalf("báo cáo lệch bảng điều hành: %s vs %+v", dash.raw, tot)
	}
	if sop, ok := get(dt, "sop_progress_pct").(string); ok {
		if tot.SOPProgressPct == nil || !tot.SOPProgressPct.Equal(decimal.RequireFromString(sop)) {
			t.Fatalf("tiến độ SOP: bảng %s, báo cáo %v", sop, tot.SOPProgressPct)
		}
	}
	// Chỉ theo HTX (gộp vụ, giống): một dòng HTX A, hộ phân biệt = 2 (không phải 3 lô).
	byCoop, err := rd.Production(ctx, cropcyclesrc.ProductionQuery{TenantID: f.tenant, Scope: authz.Scope{All: true}, ByCooperative: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(byCoop.Rows) != 2 || byCoop.Totals.Seasons != 4 || byCoop.Totals.Households != 3 {
		t.Fatalf("toàn bộ theo HTX: %+v", byCoop)
	}
	for _, r := range byCoop.Rows {
		if r.SeasonCode != "" || r.SeasonYear != 0 || r.VarietyCode != "" {
			t.Fatalf("chiều không chọn phải trống: %+v", r)
		}
		if r.CooperativeRef == f.coopA && (r.Seasons != 3 || r.Households != 2) {
			t.Fatalf("HTX A: %+v", r)
		}
	}
	// Bộ lọc giống không có dữ liệu → không dòng, tổng bằng 0.
	none, err := rd.Production(ctx, cropcyclesrc.ProductionQuery{TenantID: f.tenant, Scope: authz.Scope{All: true}, VarietyCode: "ST25", ByCooperative: true})
	if err != nil || len(none.Rows) != 0 || none.Totals.Seasons != 0 || none.Totals.ActualYieldTPerHa != nil {
		t.Fatalf("lọc giống không có vụ: %+v %v", none, err)
	}
}

// HTX US-DASH-02, US-DASH-03: phạm vi của cổng — cách ly tenant, quản lý HTX chỉ HTX mình
// (bộ lọc HTX khác không mở rộng phạm vi), cán bộ KN theo xã, phạm vi rỗng không thấy gì.
func TestReportData_Scope_HTX_US_DASH_03(t *testing.T) {
	f := reportSeed(t)
	rd := f.mod.ReportData()
	ctx := context.Background()
	seasons := func(q cropcyclesrc.ProductionQuery) int64 {
		t.Helper()
		d, err := rd.Production(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return d.Totals.Seasons
	}
	if n := seasons(cropcyclesrc.ProductionQuery{TenantID: ids.New(), Scope: authz.Scope{All: true}}); n != 0 {
		t.Fatalf("tenant khác (RLS): %d", n)
	}
	if n := seasons(cropcyclesrc.ProductionQuery{TenantID: f.tenant, CoopOnly: true, CooperativeRef: f.coopB}); n != 1 {
		t.Fatalf("HTX B chỉ lô của mình: %d", n)
	}
	if n := seasons(cropcyclesrc.ProductionQuery{TenantID: f.tenant, Scope: authz.Scope{Units: []string{"31117"}}}); n != 3 {
		t.Fatalf("cán bộ KN xã 31117: %d", n)
	}
	if n := seasons(cropcyclesrc.ProductionQuery{TenantID: f.tenant, Scope: authz.Scope{Units: []string{"31117"}}, CooperativeRef: f.coopB}); n != 0 {
		t.Fatalf("lọc HTX ngoài xã không mở rộng phạm vi: %d", n)
	}
	if n := seasons(cropcyclesrc.ProductionQuery{TenantID: f.tenant, Scope: authz.Scope{All: true}, CooperativeRef: f.coopB}); n != 1 {
		t.Fatalf("toàn bộ lọc HTX B: %d", n)
	}
	if n := seasons(cropcyclesrc.ProductionQuery{TenantID: f.tenant}); n != 0 {
		t.Fatalf("phạm vi rỗng: %d", n)
	}
	for _, q := range []cropcyclesrc.ProductionQuery{
		{TenantID: uuid.Nil, Scope: authz.Scope{All: true}},
		{TenantID: f.tenant, CoopOnly: true},
		{TenantID: f.tenant, Scope: authz.Scope{All: true}, SeasonCode: "DX'; --"},
	} {
		if _, err := rd.Production(ctx, q); err == nil {
			t.Fatalf("truy vấn sai phải lỗi: %+v", q)
		}
	}
}
