package reportstest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// completedFile trả tệp của việc xuất đã xong (theo sự kiện hoàn tất).
func (f *fixture) completedFile(jobID string) []byte {
	f.t.Helper()
	for _, e := range f.events(port.EventExportCompleted) {
		var pl port.ExportCompletedV1
		_ = json.Unmarshal(e.Payload, &pl)
		if pl.JobID == jobID {
			return f.file(pl.ObjectKey)
		}
	}
	f.t.Fatalf("việc xuất %s chưa xong", jobID)
	return nil
}

// HTX US-DASH-02 AC-1, AC-2: quản lý HTX xuất báo cáo sản lượng (202 + việc xuất nền); tệp
// khớp bộ lọc hiện hành, chỉ HTX của mình; số decimal giữ nguyên (kg → tấn chia 1000 chính
// xác), dòng tổng lấy số hộ phân biệt từ cổng (không cộng các dòng); Excel và PDF cùng số.
func TestHTXProductionExport_HTX_US_DASH_02(t *testing.T) {
	f := newFixture(t, false)
	mgr := f.as(f.org.coop, ccport.RoleCoopManager)
	tpl := items(mgr.must(mgr.get("/v1/reports/templates"), http.StatusOK, "mẫu"))
	if len(tpl) != 1 || tpl[0]["key"] != "htx.production" {
		t.Fatalf("quản lý HTX chỉ thấy mẫu sản lượng HTX: %v", tpl)
	}
	params := map[string]any{"season_code": "HT", "season_year": 2026}
	x := mgr.must(mgr.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx", "params": params}),
		http.StatusAccepted, "xuất Excel").body
	p := mgr.must(mgr.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "pdf", "params": params}),
		http.StatusAccepted, "xuất PDF").body
	if got, _ := x["params"].(map[string]any); got["cooperative_ref"] != f.org.coop.String() || x["status"] != "queued" {
		t.Fatalf("phạm vi HTX ghi vào bộ lọc của việc xuất: %v", x)
	}
	f.runWorker()
	q := f.cc.lastQuery()
	if !q.CoopOnly || q.CooperativeRef != f.org.coop || q.TenantID != f.tenant || q.SeasonCode != "HT" || q.SeasonYear != 2026 ||
		!q.ByCooperative || !q.BySeason || !q.ByVariety || q.Scope.All || len(q.Scope.Units) != 0 {
		t.Fatalf("worker đọc đúng HTX của người yêu cầu, cùng bộ lọc: %+v", q)
	}
	job := mgr.must(mgr.get("/v1/reports/exports/"+str(x, "id")), http.StatusOK, "trạng thái").body
	if job["status"] != "completed" || job["row_count"].(float64) != 2 || job["provisional"] != true {
		t.Fatalf("việc xuất xong, còn lô đang canh tác → tạm tính: %v", job)
	}
	s := f.sheet(f.completedFile(str(x, "id")))
	if got := raw(t, s, "A1"); got != "Báo cáo sản lượng HTX (dự kiến – thực tế)" {
		t.Fatalf("tiêu đề: %q", got)
	}
	for cell, want := range map[string]string{"A3": "HTX: HTX Lúa Xanh Bình Đức", "A4": "Vụ: Hè Thu 2026", "A5": "Giống lúa: Tất cả"} {
		if got := raw(t, s, cell); got != want {
			t.Errorf("dòng đầu %s = %q, muốn %q", cell, got, want)
		}
	}
	hr := findRow(t, s, "HTX")
	want := [][]string{
		{"HTX", "Vụ", "Giống lúa", "Số lô (vụ)", "Số nông hộ", "Diện tích (ha)", "Diện tích đã khai sản lượng (ha)", "Sản lượng dự kiến (tấn)",
			"Sản lượng thực tế (tấn)", "Năng suất thực tế (tấn/ha)", "Tiến độ SOP (%)"},
		{"HTX Lúa Xanh Bình Đức", "Hè Thu 2026", "OM 5451", "2", "2", "3", "1.5", "20.25", "9.750125", "6.5001", "40"},
		{"HTX Lúa Xanh Bình Đức", "Hè Thu 2026", "ST25", "1", "1", "1.5", "0", "9", "0", "", "0"},
		{"Tổng cộng", "", "", "3", "2", "4.5", "1.5", "29.25", "9.750125", "6.5001", "26.7"},
	}
	for i, row := range want {
		for j, v := range row {
			if got := raw(t, s, cellName(j+1, hr+i)); got != v {
				t.Errorf("ô %s = %q, muốn %q", cellName(j+1, hr+i), got, v)
			}
		}
	}
	if got := raw(t, s, cellName(1, hr+5)); !strings.HasPrefix(got, "Số liệu tạm tính") {
		t.Fatalf("chân trang tạm tính: %q", got)
	}
	pdf := f.completedFile(str(p, "id"))
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(pdf) < 5000 {
		t.Fatalf("PDF: %d byte", len(pdf))
	}
}

// HTX US-DASH-02: phạm vi — quản lý HTX không xuất HTX khác / mẫu KN; việc xuất của HTX chỉ
// người cùng HTX và quản trị toàn bộ thấy (cán bộ KN cùng xã, HTX khác, tenant khác → 404);
// cán bộ KN xuất theo xã của mình; tỉnh lọc xã → phạm vi thu hẹp.
func TestHTXProductionScope_HTX_US_DASH_02(t *testing.T) {
	f := newFixture(t, false)
	mgr := f.as(f.org.coop, ccport.RoleCoopManager)
	if r := mgr.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx",
		"params": map[string]any{"cooperative_ref": f.org.coopCaiKhe.String()}}); r.status != http.StatusForbidden || code(r) != "reports.out_of_scope" {
		t.Fatalf("quản lý HTX lọc HTX khác: %d %v", r.status, r.body)
	}
	if r := mgr.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX không xuất mẫu KN: %d", r.status)
	}
	noOrg := f.as(uuid.Nil, ccport.RoleCoopManager)
	if r := noOrg.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx"}); r.status != http.StatusForbidden {
		t.Fatalf("tài khoản chưa gắn HTX: %d", r.status)
	}
	id := str(mgr.must(mgr.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx",
		"params": map[string]any{"cooperative_ref": f.org.coop.String(), "variety_code": "OM5451"}}), http.StatusAccepted, "xuất HTX mình").body, "id")
	f.runWorker()
	if q := f.cc.lastQuery(); !q.CoopOnly || q.VarietyCode != "OM5451" {
		t.Fatalf("lọc giống: %+v", q)
	}
	for who, c := range map[string]caller{
		"quản lý HTX khác":     f.as(f.org.coopCaiKhe, ccport.RoleCoopManager),
		"cán bộ KN cùng xã":    f.as(f.org.tBinhDuc, extport.RoleKNOfficer),
		"quản lý HTX tenant B": f.asTenant(ids.New(), f.org.coop, ccport.RoleCoopManager),
	} {
		if r := c.get("/v1/reports/exports/" + id); r.status != http.StatusNotFound {
			t.Fatalf("%s thấy việc xuất của HTX: %d", who, r.status)
		}
	}
	peer := f.as(f.org.coop, ccport.RoleCoopManager)
	peer.must(peer.get("/v1/reports/exports/"+id), http.StatusOK, "quản lý khác cùng HTX")
	adm := f.as(uuid.Nil, authz.RolePlatformAdmin)
	adm.must(adm.get("/v1/reports/exports/"+id), http.StatusOK, "quản trị toàn bộ")

	off := f.as(f.org.tBinhDuc, extport.RoleKNOfficer)
	off.must(off.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx"}), http.StatusAccepted, "cán bộ KN")
	f.runWorker()
	if q := f.cc.lastQuery(); q.CoopOnly || q.Scope.All || len(q.Scope.Units) != 1 || q.Scope.Units[0] != cBinhDuc {
		t.Fatalf("cán bộ KN theo xã: %+v", q)
	}
	prov := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	if r := prov.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx",
		"params": map[string]any{"unit_code": cCaiKhe}}); r.status != http.StatusForbidden {
		t.Fatalf("tỉnh khác: %d", r.status)
	}
	pid := str(prov.must(prov.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx",
		"params": map[string]any{"unit_code": cBinhDuc}}), http.StatusAccepted, "tỉnh lọc xã").body, "id")
	f.runWorker()
	if q := f.cc.lastQuery(); len(q.Scope.Units) != 1 || q.Scope.Units[0] != cBinhDuc {
		t.Fatalf("tỉnh lọc xã: %+v", q)
	}
	if j := prov.get("/v1/reports/exports/" + pid).body; j["row_count"].(float64) != 2 {
		t.Fatalf("hai dòng HTX Bình Đức: %v", j)
	}
	if r := prov.post("/v1/reports/exports", map[string]any{"template_key": "htx.production", "format": "xlsx",
		"params": map[string]any{"variety_code": "OM 5451"}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("mã giống sai: %d", r.status)
	}
}

// HTX US-DASH-03 AC-1: quản lý HTX lập lịch gửi báo cáo sản lượng định kỳ; đến mốc, việc
// xuất theo lịch chỉ trong HTX của người lập và email gửi đúng người nhận đã đăng ký.
func TestHTXProductionSchedule_HTX_US_DASH_03(t *testing.T) {
	f := newFixture(t, true)
	mgr := f.as(f.org.coop, ccport.RoleCoopManager)
	rcpt := ids.New()
	f.dir.emails[rcpt] = "quan.ly@htx-binhduc.test"
	if r := mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx",
		"frequency": "weekly", "weekday": 1, "hour": 7, "recipients": []string{rcpt.String()}}); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX không lập lịch mẫu KN: %d", r.status)
	}
	if r := mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "htx.production", "format": "xlsx",
		"frequency": "weekly", "weekday": 1, "hour": 7, "recipients": []string{rcpt.String()},
		"params": map[string]any{"cooperative_ref": f.org.coopCaiKhe.String()}}); r.status != http.StatusForbidden {
		t.Fatalf("lịch cho HTX khác: %d", r.status)
	}
	sch := mgr.must(mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "htx.production", "format": "pdf",
		"frequency": "monthly", "day_of_month": 1, "hour": 6, "recipients": []string{rcpt.String()},
		"params": map[string]any{"season_code": "HT", "season_year": 2026}}), http.StatusCreated, "lập lịch").body
	if pr, _ := sch["params"].(map[string]any); pr["cooperative_ref"] != f.org.coop.String() {
		t.Fatalf("lịch ghi HTX của người lập: %v", sch)
	}
	other := f.as(f.org.coopCaiKhe, ccport.RoleCoopManager)
	if l := items(other.must(other.adminGet("/v1/reports/schedules"), http.StatusOK, "lịch HTX khác")); len(l) != 0 {
		t.Fatalf("HTX khác không thấy lịch: %v", l)
	}
	if l := items(mgr.must(mgr.adminGet("/v1/reports/schedules"), http.StatusOK, "lịch của mình")); len(l) != 1 {
		t.Fatalf("lịch của mình: %v", l)
	}
	f.clk.Advance(4 * 24 * time.Hour) // qua mốc 01/10 06:00
	if err := f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runWorker()
	if q := f.cc.lastQuery(); !q.CoopOnly || q.CooperativeRef != f.org.coop || q.SeasonCode != "HT" {
		t.Fatalf("việc xuất theo lịch trong HTX của người lập: %+v", q)
	}
	for _, e := range f.events(port.EventExportCompleted) {
		f.deliver(port.DeliveryConsumer, e, f.mod.DeliveryHandler())
	}
	msgs := f.mailer.messages()
	if len(msgs) != 1 || msgs[0].To != "quan.ly@htx-binhduc.test" || !strings.Contains(msgs[0].Subject+msgs[0].Text, "sản lượng HTX") {
		t.Fatalf("email gửi đúng người nhận: %+v", msgs)
	}
}
