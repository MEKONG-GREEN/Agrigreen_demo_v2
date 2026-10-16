package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// FR-03: KNV xã báo cáo tiến độ Đề án theo HTX × vụ, Admin tỉnh duyệt (không tự
// duyệt), mục tiêu tỉnh là tham số; phiên bản mới phải duyệt lại.
func TestProgramProgressFR03(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	f.params.set(port.ProgramTargetKey("89"), "1000")
	body := func(commune, coop, reg, dep, comp string) map[string]any {
		return map[string]any{"commune_code": commune, "cooperative_ref": coop, "season_code": "HT", "season_year": 2026,
			"registered_area_ha": reg, "deploying_area_ha": dep, "completed_area_ha": comp}
	}
	if r := p.off30001a.post("/v1/extension/program-reports", body("30001", f.org.c1.String(), "100", "80", "30")); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("đang triển khai + hoàn thành vượt đăng ký: %d", r.status)
	}
	if r := p.off30001a.post("/v1/extension/program-reports", body("31001", f.org.c3.String(), "10", "1", "1")); r.status != http.StatusForbidden {
		t.Fatalf("ngoài xã phụ trách: %d", r.status)
	}
	r1 := p.off30001a.must(p.off30001a.post("/v1/extension/program-reports", body("30001", f.org.c1.String(), "300.5", "100", "50.25")),
		http.StatusCreated, "báo cáo Đề án")
	id1 := str(r1.body, "report", "id")
	if r := p.off30001b.post("/v1/extension/program-reports/"+id1+"/approve", map[string]any{}); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã không duyệt: %d", r.status)
	}
	if r := p.admin92.post("/v1/extension/program-reports/"+id1+"/approve", map[string]any{}); r.status != http.StatusNotFound {
		t.Fatalf("tỉnh khác không thấy: %d", r.status)
	}
	p.admin89.must(p.admin89.post("/v1/extension/program-reports/"+id1+"/approve", map[string]any{"note": "Đã kiểm tra"}), http.StatusOK, "duyệt")
	if r := p.admin89.post("/v1/extension/program-reports/"+id1+"/approve", map[string]any{}); r.status != http.StatusConflict {
		t.Fatalf("duyệt lặp: %d", r.status)
	}
	// Admin tỉnh tự gửi thì không tự duyệt (R-56).
	own := p.admin89.must(p.admin89.post("/v1/extension/program-reports", body("30002", f.org.c2.String(), "200", "0", "0")), http.StatusCreated, "tỉnh gửi")
	if r := p.admin89.post("/v1/extension/program-reports/"+str(own.body, "report", "id")+"/approve", map[string]any{}); r.status != http.StatusForbidden || code(r) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt: %d %v", r.status, r.body)
	}
	pr := p.national.must(p.national.get("/v1/extension/program-progress?season_code=HT&season_year=2026"), http.StatusOK, "tiến độ").body
	if n := len(pr["items"].([]any)); n != 2 || num(pr, "unapproved") != 1 || pr["provisional"] != true || str(pr, "targets", "89") != "1000" {
		t.Fatalf("toàn quốc: %v", pr)
	}
	// Cổng cho pf/reports: lọc đúng phạm vi truyền vào; tỉnh 92 không có dòng nào.
	ctx := tenant.WithID(context.Background(), f.tenant)
	d, err := f.mod.ReportData().ProgramProgress(ctx, port.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{Units: []string{"30001"}}})
	if err != nil || len(d.Rows) != 1 || d.Rows[0].RegisteredHa.String() != "300.5" || d.Rows[0].CompletedHa.String() != "50.25" || !d.Rows[0].Approved {
		t.Fatalf("cổng đọc theo phạm vi xã: %+v %v", d, err)
	}
	if d, _ := f.mod.ReportData().ProgramProgress(ctx, port.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{Units: []string{"92", "31001"}}}); len(d.Rows) != 0 {
		t.Fatalf("tỉnh khác: %+v", d.Rows)
	}
	// Gửi lại cùng HTX × vụ → phiên bản 2, phải duyệt lại.
	r2 := p.off30001a.must(p.off30001a.post("/v1/extension/program-reports", body("30001", f.org.c1.String(), "320", "120", "60")), http.StatusCreated, "phiên bản 2")
	if num(r2.body, "report", "version") != 2 {
		t.Fatalf("phiên bản: %v", r2.body)
	}
	d, _ = f.mod.ReportData().ProgramProgress(ctx, port.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{Units: []string{"30001"}}})
	if d.Rows[0].Approved || d.Rows[0].RegisteredHa.String() != "320" || !d.Completeness.Provisional {
		t.Fatalf("phiên bản mới chưa duyệt: %+v", d)
	}
	if r := p.admin89.post("/v1/extension/program-reports/"+id1+"/approve", map[string]any{}); r.status != http.StatusConflict || code(r) != "extension.report_superseded" {
		t.Fatalf("duyệt phiên bản cũ: %d", r.status)
	}
}

// US-DASH-02 AC-1, FR-01/FR-05: cổng dữ liệu báo cáo — diện tích theo giống × vụ,
// xã chưa báo cáo → tạm tính, tổng hợp kỳ quý gom đủ các tháng.
func TestReportDataSowingAndSummary(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	sow := func(c caller, commune, coop, variety, ha string) {
		c.must(c.post("/v1/extension/field-reports", map[string]any{"kind": "sowing_progress", "commune_code": commune,
			"cooperative_ref": coop, "period": "2026-09", "season_code": "HT", "variety_code": variety, "sown_area_ha": ha}), http.StatusCreated, "gieo sạ")
	}
	sow(p.off30001a, "30001", f.org.c1.String(), "ST25", "10.1234")
	sow(p.off30001a, "30001", f.org.c1.String(), "OM5451", "0.0001")
	sow(p.off31001, "31001", f.org.c3.String(), "ST25", "5")
	ctx := tenant.WithID(context.Background(), f.tenant)
	rd := f.mod.ReportData()
	q := port.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}, Periods: []string{"2026-09"}, At: t0}
	d, err := rd.Sowing(ctx, q)
	if err != nil || len(d.Rows) != 2 {
		t.Fatalf("gieo sạ tỉnh 89: %+v %v", d, err)
	}
	if d.Rows[0].VarietyCode != "OM5451" || d.Rows[0].SownAreaHa.String() != "0.0001" || d.Rows[1].SownAreaHa.String() != "10.1234" {
		t.Fatalf("diện tích theo giống: %+v", d.Rows)
	}
	c := d.Completeness
	if c.ExpectedCommunes != 2 || c.ReportedCommunes != 1 || len(c.MissingCommunes) != 1 || c.MissingCommunes[0] != "30002" || !c.Provisional {
		t.Fatalf("xã 30002 chưa báo cáo → tạm tính: %+v", c)
	}
	// Quý III gom tháng 7–9; toàn quốc thấy cả 31001.
	sum, err := rd.PeriodSummary(ctx, port.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{All: true},
		Periods: []string{"2026-07", "2026-08", "2026-09"}, At: t0})
	if err != nil || len(sum.Rows) != 2 || sum.Rows[0].SownAreaHa.String() != "10.1235" || sum.Rows[1].SownAreaHa.String() != "5" {
		t.Fatalf("tổng hợp kỳ quý: %+v %v", sum.Rows, err)
	}
	if d, _ := rd.Sowing(ctx, port.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{}, Periods: []string{"2026-09"}}); len(d.Rows) != 0 {
		t.Fatal("phạm vi rỗng không thấy gì")
	}
	if _, err := rd.Sowing(ctx, port.ReportQuery{TenantID: f.tenant, Scope: authz.Scope{All: true}}); err == nil {
		t.Fatal("thiếu kỳ phải lỗi")
	}
}
