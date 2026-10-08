package handler_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/mechanizationsrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// FN-10 BR-04: cổng báo cáo cho pf/reports trả ĐÚNG số đã lưu của lần cân đối —
// đổi diện tích sau khi tính không làm đổi báo cáo; 3 tab đọc cùng kết quả.
func TestReportDataEqualsSavedResults(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o1, o2 := adm.owner(f.c1, "0909000001"), adm.owner(f.c2, "0909000002")
	for _, sn := range []string{"R1", "R2", "R3"} {
		adm.machine(f.c1, o1, "MLD", sn, "2025-06-01")
	}
	for _, sn := range []string{"S1", "S2", "S3", "S4", "S5", "S6"} {
		adm.machine(f.c2, o2, "MLD", sn, "2025-06-01")
	}
	adm.area(f.c1, "DX", 2025, "500", false)
	adm.area(f.c2, "DX", 2025, "500", false)
	run := adm.compute("DX", 2025)
	saved := adm.results(run["id"].(string))[f.c1.String()+"|MLD"]
	adm.area(f.c1, "DX", 2025, "2000", false) // đổi sau khi tính
	adm.area(f.c1, "DX", 2026, "250", false)
	adm.area(f.c2, "DX", 2026, "500", false)
	adm.compute("DX", 2026)

	ctx := tenant.WithID(context.Background(), f.tenant)
	rd := f.mod.ReportData()
	q := mechanizationsrc.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{All: true}, SeasonCode: "DX", SeasonYear: 2025}
	sh, err := rd.Shortage(ctx, q)
	if err != nil || len(sh.Rows) != 1 {
		t.Fatalf("thiếu hụt: %+v %v", sh, err)
	}
	r := sh.Rows[0]
	if r.CooperativeRef != f.c1 || r.CooperativeCode != "HTX000001" || r.Capacity != num(saved, "capacity") || *r.RequiredWhole != num(saved, "required_whole") ||
		r.Shortage != -num(saved, "gap") || r.CoveragePct.String() != str(saved, "coverage_pct") || r.Required.String() != str(saved, "required") ||
		r.AreaSource != "manual" || sh.Run.RunID.String() != run["id"] {
		t.Fatalf("dòng thiếu hụt = kết quả đã lưu %v: %+v", saved, r)
	}
	if r.CoveragePct.String() != "60" || r.Shortage != 2 {
		t.Fatalf("không tính lại theo diện tích mới: %+v", r)
	}
	cov, err := rd.Coverage(ctx, q)
	if err != nil || len(cov.Rows) != 4 || cov.Rows[1].Status != "no_norm" || cov.Rows[0].ProvinceCode != "89" || cov.Rows[0].Capacity != 3 || cov.Rows[0].CoveragePct.String() != "60" ||
		cov.Rows[0].StatusLabel != "Cần chú ý" || cov.Rows[0].Cooperatives != 1 {
		t.Fatalf("tổng hợp theo tỉnh × khâu × chủng loại: %+v %v", cov.Rows, err)
	}
	cmp, err := rd.SeasonCompare(ctx, mechanizationsrc.CompareQuery{ReportQuery: q, SeasonCodeB: "DX", SeasonYearB: 2026})
	if err != nil || len(cmp.Rows) != 4 {
		t.Fatalf("đối chiếu: %+v %v", cmp, err)
	}
	c := cmp.Rows[0] // tỉnh 89: vụ A 3 máy/5 cần (60%), vụ B 3 máy/2,5 cần (120%)
	if c.A.CoveragePct.String() != "60" || c.B.CoveragePct.String() != "120" || *c.CapacityDiff != 0 || c.PctDiff.String() != "60" {
		t.Fatalf("chênh lệch B−A: %+v A=%+v B=%+v", c, c.A, c.B)
	}
	if _, err := rd.SeasonCompare(ctx, mechanizationsrc.CompareQuery{ReportQuery: q, SeasonCodeB: "DX", SeasonYearB: 2025}); err == nil {
		t.Fatal("hai vụ trùng nhau phải bị chặn")
	}
	// Phạm vi tỉnh 92: chỉ thấy HTX c2 (thừa máy) → không có dòng thiếu hụt.
	q92 := q
	q92.Scope = authz.Scope{Units: []string{"92", "31001"}}
	if sh, _ := rd.Shortage(ctx, q92); len(sh.Rows) != 0 {
		t.Fatalf("phạm vi tỉnh 92: %+v", sh.Rows)
	}
}
