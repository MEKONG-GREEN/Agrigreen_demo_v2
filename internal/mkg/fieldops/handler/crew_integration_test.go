package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func (f *fixture) day(n int) string { return f.clk.Now().In(vn).AddDate(0, 0, n).Format("2006-01-02") }

var vn = func() *time.Location {
	l, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		panic(err)
	}
	return l
}()

// dbRejects chạy sql trong savepoint và đòi lỗi chứa want (lớp hai: CHECK / quyền DB).
func (f *fixture) dbRejects(what, want, sql string, args ...any) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		ctx := context.Background()
		if _, err := tx.Exec(ctx, `SAVEPOINT s1`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, sql, args...)
		if err == nil || !strings.Contains(err.Error(), want) {
			f.t.Fatalf("%s: DB phải từ chối (%s): %v", what, want, err)
		}
		_, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT s1`)
		return err
	})
}

// proto FM-01: không cuộn trước ngày gặt của thửa (ngày gặt từ nền tảng qua cổng
// HarvestCalendar — bề mặt partner ADR-0020). Chưa biết ngày gặt → vẫn ghi nhưng gắn cờ và cảnh báo;
// nền tảng không gọi được → dùng bảng đệm; không có bảng đệm → cờ unavailable. Không im lặng.
func TestBaleBeforeHarvestRejectedFM01(t *testing.T) {
	f := newFixture(t, nil)
	p0, p1 := f.plots[0], f.plots[1]
	season := ids.New()
	f.harvest.Set(p0, season, f.day(2), "", "open") // gặt dự kiến sau 2 ngày
	id := f.approvedOrder()                         // lập lệnh → bảng đệm ngày gặt
	col := f.collector()

	r := f.count(col, id, p0, port.StageBale, 100, nil)
	mustCode(t, r, http.StatusUnprocessableEntity, "fieldops.before_harvest", "cuộn trước ngày gặt")
	if !strings.Contains(str(r.body, "detail"), f.day(2)) {
		t.Fatalf("chi tiết phải nêu ngày gặt: %s", r.raw)
	}
	// Nền tảng không gọi được → vẫn chặn theo bảng đệm (không bỏ qua quy tắc khi mất kết nối).
	f.harvest.Down = true
	mustCode(t, f.count(col, id, p0, port.StageBale, 100, nil), http.StatusUnprocessableEntity, "fieldops.before_harvest",
		"bảng đệm khi nền tảng lỗi")
	f.harvest.Down = false

	// HTX khai gặt thực tế hôm qua → cuộn được; nguồn partner, ghi kết quả vào sổ.
	f.harvest.Set(p0, season, f.day(-1), f.day(-4), "open") // đã tới cửa sổ gặt
	ok := f.count(col, id, p0, port.StageBale, 100, nil)
	mustStatus(t, ok, http.StatusCreated, "cuộn sau ngày gặt")
	if str(ok.body, "harvest_check", "check") != "ok" || str(ok.body, "harvest_check", "harvest_date") != f.day(-1) ||
		str(ok.body, "harvest_check", "source") != "partner" || len(arr(ok.body, "warnings")) != 0 {
		t.Fatalf("kiểm ngày gặt ok: %s", ok.raw)
	}
	// Thửa chưa có ngày gặt trên nền tảng (chưa chia sẻ, RB-2) → ghi được, GẮN CỜ + cảnh báo.
	unk := f.count(col, id, p1, port.StageBale, 80, nil)
	mustStatus(t, unk, http.StatusCreated, "chưa biết ngày gặt")
	if str(unk.body, "harvest_check", "check") != "unknown" || !strings.Contains(string(unk.raw), "fieldops.harvest_date_unknown") {
		t.Fatalf("chưa biết ngày gặt phải gắn cờ: %s", unk.raw)
	}
	// Sổ đếm cuộn hiện cờ theo bản ghi.
	counts := arr(f.supervisor().get("/v1/fieldops/orders/"+id+"/roll-counts").body, "counts")
	flags := map[string]string{}
	for _, c := range counts {
		hc, _ := c["harvest_check"].(map[string]any)
		flags[str(c, "plot_ref")] = str(hc, "check")
	}
	if flags[p0.String()] != "ok" || flags[p1.String()] != "unknown" {
		t.Fatalf("cờ theo bản ghi: %v", flags)
	}

	// Không gọi được nền tảng và không có bảng đệm → unavailable (lệnh mới lập khi nền tảng lỗi).
	f.harvest.Down = true
	id2 := f.approvedOrder()
	un := f.count(col, id2, p1, port.StageBale, 10, nil)
	mustStatus(t, un, http.StatusCreated, "nền tảng lỗi, không bảng đệm")
	if str(un.body, "harvest_check", "check") != "unavailable" || !strings.Contains(string(un.raw), "fieldops.harvest_date_unavailable") {
		t.Fatalf("unavailable phải gắn cờ: %s", un.raw)
	}
	f.harvest.Down = false

	// Đồng bộ app hiện trường: bản ghi trước ngày gặt → rejected theo từng bản ghi.
	f.harvest.Set(p0, season, f.day(3), "", "open")
	id3 := f.approvedOrder()
	mustStatus(t, f.assign(f.supervisor(), id3, f.crew("TO-FM01", "5000", col.actor), 100, nil), http.StatusCreated, "giao tổ")
	col.device = ids.New().String()
	push := col.post("/v1/fieldops/sync/push", map[string]any{"device_id": col.device, "records": []any{map[string]any{
		"id": ids.New().String(), "idem_key": "dev:" + col.device + ":1", "entity": "roll_count", "op": "create", "event_time": f.ts(-time.Minute),
		"payload": map[string]any{"order_id": id3, "plot_ref": p0.String(), "stage": "bale", "rolls": 40}}}})
	if res := arr(push.body, "results"); len(res) != 1 || str(res[0], "code") != "fieldops.before_harvest" {
		t.Fatalf("push trước ngày gặt: %s", push.raw)
	}

	// Lớp hai: CHECK DB chặn dòng kiểm "ok" có ngày cuộn trước ngày gặt.
	f.dbRejects("FM-01 CHECK", "check", `INSERT INTO mkg_fieldops.roll_count_harvest
	    (id, tenant_id, stream_key, roll_count_id, order_id, plot_ref, bale_date, harvest_check, harvest_date, harvest_source,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'harvest:x', $1, $1, $1, DATE '2026-10-01', 'ok', DATE '2026-10-05', 'partner', now(), $1, $3, 'x')`,
		ids.New(), f.tenant, key())
}

// proto FM-02: rơm phải cuộn xong trong N ngày sau gặt (N = baling_deadline_days, mặc định 3,
// tham số có phiên bản — tạm, chờ khách). Quá hạn KHÔNG chặn ghi, nhưng gắn cờ trên thửa,
// lệnh gom và danh sách hạn cuộn. Đổi N cần người đề xuất ≠ người duyệt (R-56).
func TestBalingDeadlineOverdueFM02(t *testing.T) {
	f := newFixture(t, nil)
	p0, p1 := f.plots[0], f.plots[1]
	season := ids.New()
	f.harvest.Set(p0, season, f.day(-5), f.day(-8), "open")
	f.harvest.Set(p1, season, f.day(-1), f.day(-4), "open")
	id := f.approvedOrder()
	sup := f.supervisor()

	over := sup.get("/v1/fieldops/baling-deadlines?status=overdue")
	mustStatus(t, over, http.StatusOK, "danh sách quá hạn")
	items := arr(over.body, "items")
	if len(items) != 1 || str(items[0], "plot_ref") != p0.String() || str(items[0], "days_on_field") != "5" ||
		str(items[0], "deadline") != f.day(-2) || str(over.body, "param_version") != "provisional-default" ||
		str(over.body, "baling_deadline_days") != "3" {
		t.Fatalf("quá hạn: %s", over.raw)
	}
	o := sup.get("/v1/fieldops/orders/" + id)
	if b, _ := o.body["order"].(map[string]any)["baling_overdue"].(bool); !b {
		t.Fatalf("cờ quá hạn trên lệnh gom: %s", o.raw)
	}
	status := map[string]string{}
	for _, x := range arr(o.body, "baling") {
		status[str(x, "plot_ref")] = str(x, "status")
	}
	if status[p0.String()] != "overdue" || status[p1.String()] != "pending" {
		t.Fatalf("hạn cuộn theo thửa: %v", status)
	}
	if l := arr(sup.get("/v1/fieldops/orders").body, "items"); len(l) != 1 || l[0]["baling_overdue"] != true {
		t.Fatalf("cờ trong danh sách lệnh gom: %v", l)
	}

	// Quá hạn không chặn: cuộn trễ ghi được, trạng thái chuyển baled_late (vẫn gắn cờ).
	mustStatus(t, f.count(f.collector(), id, p0, port.StageBale, 300, nil), http.StatusCreated, "cuộn trễ")
	mustStatus(t, f.count(f.collector(), id, p1, port.StageBale, 200, nil), http.StatusCreated, "cuộn trong hạn")
	fl := sup.get("/v1/fieldops/baling-deadlines?status=flagged&order_id=" + id)
	if xs := arr(fl.body, "items"); len(xs) != 1 || str(xs[0], "status") != "baled_late" || str(xs[0], "baled_on") != f.day(0) {
		t.Fatalf("cuộn trễ vẫn gắn cờ: %s", fl.raw)
	}
	if n := len(arr(sup.get("/v1/fieldops/baling-deadlines?status=overdue").body, "items")); n != 0 {
		t.Fatalf("đã cuộn thì hết quá hạn: %d", n)
	}
	if b, _ := sup.get("/v1/fieldops/orders/" + id).body["order"].(map[string]any)["baling_overdue"].(bool); b {
		t.Fatal("lệnh gom hết cờ quá hạn sau khi cuộn")
	}

	// Tham số có phiên bản: đề xuất 7 ngày; người đề xuất tự duyệt → 403; người khác duyệt → hiệu lực.
	a, b := f.supervisor(), f.supervisor()
	mustCode(t, a.post("/v1/fieldops/params/baling_deadline_days/versions", map[string]any{"value": "7"}),
		http.StatusUnprocessableEntity, "validation_failed", "thiếu lý do")
	pr := a.post("/v1/fieldops/params/baling_deadline_days/versions", map[string]any{"value": "7", "reason": "khách chốt 7 ngày mùa khô"})
	mustStatus(t, pr, http.StatusCreated, "đề xuất")
	ver := str(pr.body, "param", "version_id")
	if pm, _ := pr.body["param"].(map[string]any); str(pm, "status") != "pending" || pm["provisional"] != true {
		t.Fatalf("đề xuất: %s", pr.raw)
	}
	if n := str(sup.get("/v1/fieldops/params/baling_deadline_days").body, "effective_value"); n != "3" {
		t.Fatalf("chưa duyệt → vẫn 3: %s", n)
	}
	mustCode(t, a.post("/v1/fieldops/params/baling_deadline_days/versions/"+ver+"/decision", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "self_approval_forbidden", "tự duyệt tham số")
	mustStatus(t, b.post("/v1/fieldops/params/baling_deadline_days/versions/"+ver+"/decision", map[string]any{"decision": "approved"}),
		http.StatusCreated, "duyệt")
	mustCode(t, b.post("/v1/fieldops/params/baling_deadline_days/versions/"+ver+"/decision", map[string]any{"decision": "rejected",
		"reason": "đổi ý sau khi đã duyệt"}), http.StatusConflict, "fieldops.param_already_decided", "quyết định lại")
	all := sup.get("/v1/fieldops/baling-deadlines?order_id=" + id)
	if str(all.body, "param_version") != "baling_deadline_days:v1" || str(all.body, "baling_deadline_days") != "7" {
		t.Fatalf("phiên bản tham số ghim vào kết quả: %s", all.raw)
	}
	for _, x := range arr(all.body, "items") {
		if str(x, "status") != "baled_on_time" {
			t.Fatalf("N = 7 → cả hai thửa cuộn trong hạn: %s", all.raw)
		}
	}
	// Lớp hai: CHECK DB chặn quyết định do chính người đề xuất ghi.
	f.dbRejects("R-56 tham số", "check", `INSERT INTO mkg_fieldops.param_version
	    (id, tenant_id, stream_key, param_key, version_id, version_seq, kind, value, effective_from, provisional, proposer_ref,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'param:baling_deadline_days', 'baling_deadline_days', $1, 9, 'approved', 3, current_date, true, $3, now(), $3, $4, 'x')`,
		ids.New(), f.tenant, a.actor, key())
}

// proto FM-07 + sổ tổ gom: tổ có mã, tên, thành viên (subject MKG), năng lực/ngày theo cuộn
// hoặc ha, trạng thái; mỗi thay đổi là một phiên bản; không xóa cứng (RB-1); chỉ tổ đang
// hoạt động nhận việc.
func TestCrewDirectoryVersionedFM07(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	mustCode(t, f.collector().post("/v1/fieldops/crews", crewBody("TO-A", "1000")), http.StatusForbidden, "forbidden", "tổ gom không tạo tổ")
	body := crewBody("to-a", "1000")
	k := key()
	c := sup.do(http.MethodPost, "/v1/fieldops/crews", k, body)
	mustStatus(t, c, http.StatusCreated, "tạo tổ")
	if again := sup.do(http.MethodPost, "/v1/fieldops/crews", k, body); again.status != http.StatusCreated || string(again.raw) != string(c.raw) {
		t.Fatalf("POST lặp cùng Idempotency-Key: %d %s", again.status, again.raw)
	}
	crew := str(c.body, "crew", "crew_id")
	if str(c.body, "crew", "code") != "TO-A" || str(c.body, "crew", "version") != "1" || str(c.body, "crew", "status") != "active" {
		t.Fatalf("tổ mới: %s", c.raw)
	}
	mustCode(t, sup.post("/v1/fieldops/crews", crewBody("TO-A", "500")), http.StatusConflict, "fieldops.crew_code_taken", "trùng mã")
	bad := crewBody("TO-B", "10.5")
	mustCode(t, sup.post("/v1/fieldops/crews", bad), http.StatusUnprocessableEntity, "validation_failed", "cuộn/ngày lẻ")
	ha := crewBody("TO-HA", "4.5")
	ha["capacity_unit"] = "ha"
	mustStatus(t, sup.post("/v1/fieldops/crews", ha), http.StatusCreated, "tổ tính theo ha")

	rev := crewBody("TO-A", "1500")
	rev["expected_version"] = 1
	rv := sup.post("/v1/fieldops/crews/"+crew+"/revisions", rev)
	mustStatus(t, rv, http.StatusCreated, "sửa tổ")
	if str(rv.body, "crew", "version") != "2" || str(rv.body, "crew", "capacity_per_day") != "1500" {
		t.Fatalf("phiên bản 2: %s", rv.raw)
	}
	mustCode(t, sup.post("/v1/fieldops/crews/"+crew+"/revisions", rev), http.StatusConflict, "fieldops.crew_version_conflict", "phiên bản cũ")
	mustCode(t, sup.post("/v1/fieldops/crews/"+crew+"/deactivate", map[string]any{}), http.StatusUnprocessableEntity, "validation_failed",
		"ngừng thiếu lý do")
	mustStatus(t, sup.post("/v1/fieldops/crews/"+crew+"/deactivate", map[string]any{"reason": "máy cuộn hỏng, nghỉ hết vụ"}),
		http.StatusCreated, "ngừng tổ")
	mustCode(t, sup.post("/v1/fieldops/crews/"+crew+"/deactivate", map[string]any{"reason": "máy cuộn hỏng, nghỉ hết vụ"}),
		http.StatusConflict, "fieldops.crew_transition", "ngừng lần hai")

	// FM-07: tổ ngừng hoạt động không nhận việc; tổ lạ không nhận việc; bật lại → nhận được.
	id := f.approvedOrder()
	mustCode(t, f.assign(sup, id, crew, 500, nil), http.StatusConflict, "fieldops.crew_inactive", "giao tổ ngừng")
	mustCode(t, f.assign(sup, id, ids.New().String(), 500, nil), http.StatusUnprocessableEntity, "fieldops.crew_unknown", "giao tổ lạ")
	mustStatus(t, sup.post("/v1/fieldops/crews/"+crew+"/reactivate", map[string]any{"reason": "đã sửa xong máy cuộn"}),
		http.StatusCreated, "bật lại")
	mustStatus(t, f.assign(sup, id, crew, 500, nil), http.StatusCreated, "giao tổ đang hoạt động")

	g := f.collector().get("/v1/fieldops/crews/" + crew)
	mustStatus(t, g, http.StatusOK, "đọc tổ")
	hist := arr(g.body, "history")
	if len(hist) != 4 || str(hist[0], "kind") != "created" || str(hist[2], "kind") != "deactivated" || str(hist[3], "kind") != "reactivated" ||
		str(hist[1], "snapshot", "capacity_per_day") != "1500" {
		t.Fatalf("lịch sử 4 phiên bản: %s", g.raw)
	}
	if l := arr(sup.get("/v1/fieldops/crews?status=active").body, "items"); len(l) != 2 {
		t.Fatalf("danh sách tổ đang hoạt động: %v", l)
	}
	// Tenant khác không thấy tổ (RLS).
	mustCode(t, f.as(ids.New(), port.RoleFieldSupervisor).get("/v1/fieldops/crews/"+crew), http.StatusNotFound, "fieldops.crew_not_found",
		"tenant khác")
	// Không xóa cứng: vai ứng dụng không được DELETE / UPDATE sổ tổ gom.
	f.dbRejects("xóa sổ tổ", "permission denied", `DELETE FROM mkg_fieldops.crew_event`)
	f.dbRejects("sửa sổ tổ", "permission denied", `UPDATE mkg_fieldops.crew_event SET name = 'x'`)
}

// proto FM-06: phân công không vượt năng lực/ngày của tổ; vượt → 409, trừ khi giám sát giao
// tay kèm lý do (gắn cờ over_capacity + cảnh báo). Lệnh đóng trả năng lực lại cho tổ.
func TestAssignCapacityCheckFM06(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	crew := f.crew("TO-CAP", "1000")
	o1, o2, o3 := f.approvedOrder(), f.approvedOrder(), f.approvedOrder()
	d0, d2 := f.clk.Now().Format("2006-01-02"), f.clk.Now().AddDate(0, 0, 2).Format("2006-01-02")
	win := map[string]any{"planned_from": d0, "planned_to": d2}

	a1 := f.assign(sup, o1, crew, 3000, win)
	mustStatus(t, a1, http.StatusCreated, "vừa khít 1000/ngày")
	asg, _ := a1.body["assignment"].(map[string]any)
	if str(asg, "daily_load") != "1000" || str(asg, "peak_load") != "1000" || asg["over_capacity"] != false || str(asg, "mode") != "manual" {
		t.Fatalf("phân công 1: %s", a1.raw)
	}
	r := f.assign(sup, o2, crew, 600, win)
	mustCode(t, r, http.StatusConflict, "fieldops.crew_over_capacity", "vượt năng lực")
	if !strings.Contains(str(r.body, "detail"), "1200") {
		t.Fatalf("chi tiết phải nêu tải: %s", r.raw)
	}
	mustCode(t, f.assign(sup, o2, crew, 600, map[string]any{"planned_from": d0, "planned_to": d2, "mode": "suggested",
		"override_reason": "khách gấp, chấp nhận tăng ca"}), http.StatusUnprocessableEntity, "validation_failed", "gợi ý không được vượt")
	ov := f.assign(sup, o2, crew, 600, map[string]any{"planned_from": d0, "planned_to": d2, "override_reason": "khách gấp, tổ tăng ca đêm"})
	mustStatus(t, ov, http.StatusCreated, "giám sát giao vượt kèm lý do")
	if a, _ := ov.body["assignment"].(map[string]any); a["over_capacity"] != true || str(a, "peak_load") != "1200" ||
		!strings.Contains(string(ov.raw), "fieldops.crew_over_capacity_override") {
		t.Fatalf("vượt có lý do phải gắn cờ: %s", ov.raw)
	}
	// Đơn vị: tổ tính theo ha cần expected_ha; ngày ngoài khung lệnh gom bị từ chối.
	haBody := crewBody("TO-HA", "5")
	haBody["capacity_unit"] = "ha"
	hr := sup.post("/v1/fieldops/crews", haBody)
	haCrew := str(hr.body, "crew", "crew_id")
	mustCode(t, f.assign(sup, o3, haCrew, 100, nil), http.StatusUnprocessableEntity, "fieldops.workload_required", "thiếu ha")
	mustCode(t, f.assign(sup, o3, crew, 100, map[string]any{"planned_from": f.clk.Now().AddDate(0, 0, -1).Format("2006-01-02")}),
		http.StatusUnprocessableEntity, "validation_failed", "ngoài khung")
	mustStatus(t, sup.post("/v1/fieldops/orders/"+o3+"/assign", map[string]any{"crew_ref": haCrew, "expected_ha": "12.5"}),
		http.StatusCreated, "giao tổ ha")

	// Lệnh đóng trả năng lực: đóng o1, o2 → tổ nhận thêm 1000/ngày.
	mustStatus(t, sup.post("/v1/fieldops/orders/"+o1+"/close", map[string]any{}), http.StatusCreated, "đóng o1")
	mustStatus(t, sup.post("/v1/fieldops/orders/"+o2+"/close", map[string]any{}), http.StatusCreated, "đóng o2")
	mustStatus(t, f.assign(sup, o3, crew, 3000, win), http.StatusCreated, "đổi tổ sau khi tổ rảnh")
	if h := arr(sup.get("/v1/fieldops/orders/"+o3+"/assignments").body, "items"); len(h) != 2 {
		t.Fatalf("lịch sử phân công o3: %v", h)
	}
	// Lớp hai: CHECK DB chặn phân công vượt năng lực không có lý do.
	f.dbRejects("FM-06 CHECK", "check", `INSERT INTO mkg_fieldops.crew_assignment
	    (id, tenant_id, stream_key, order_id, order_event_id, crew_id, crew_version, capacity_unit, capacity_per_day, planned_from, planned_to,
	     expected_qty, daily_load, peak_load, over_capacity, mode, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'assign:x', $1, $1, $1, 1, 'bales', 1000, current_date, current_date, 2000, 2000, 2000, true, 'manual', now(), $1, $3, 'x')`,
		ids.New(), f.tenant, key())
}

// proto FM-08: gợi ý phân công tất định — tổ đang hoạt động, vừa năng lực, gần nhất (xã phục
// vụ) rồi ít tải nhất; giám sát xác nhận (mode suggested); xác nhận lặp không sinh phân công trùng.
func TestAutoAssignSuggestionFM08(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	a := f.crew("TO-A", "1000") // gần, nhỏ → không vừa 1500
	b := f.crew("TO-B", "2000") // gần, vừa (0,75)
	cb := crewBody("TO-C", "5000")
	cb["service_communes"] = []string{"99999"} // xa, vừa (0,3)
	c := str(sup.post("/v1/fieldops/crews", cb).body, "crew", "crew_id")
	d := f.crew("TO-D", "9000")
	mustStatus(t, sup.post("/v1/fieldops/crews/"+d+"/deactivate", map[string]any{"reason": "tổ giải thể sau vụ hè thu"}), http.StatusCreated, "ngừng D")
	eb := crewBody("TO-E", "20")
	eb["capacity_unit"] = "ha"
	mustStatus(t, sup.post("/v1/fieldops/crews", eb), http.StatusCreated, "tổ E (ha)")

	o1, o2 := f.approvedOrder(), f.approvedOrder()
	d0 := f.clk.Now().Format("2006-01-02")
	q := "?planned_from=" + d0 + "&planned_to=" + d0 + "&expected_bales=1500"
	s := sup.get("/v1/fieldops/orders/" + o1 + "/crew-suggestions" + q)
	mustStatus(t, s, http.StatusOK, "gợi ý")
	sg := arr(s.body, "suggestions")
	if len(sg) != 2 || str(sg[0], "crew_ref") != b || str(sg[0], "rank") != "1" || str(sg[1], "crew_ref") != c || sg[1]["local"] != false ||
		str(s.body, "suggested", "crew_ref") != b || str(s.body, "inactive_crews") != "1" {
		t.Fatalf("thứ tự gợi ý: %s", s.raw)
	}
	unfit := map[string]string{}
	for _, x := range arr(s.body, "unfit") {
		unfit[str(x, "crew_ref")] = str(x, "unfit")
	}
	if unfit[a] != "over_capacity" || len(unfit) != 2 {
		t.Fatalf("không vừa: %v", unfit)
	}
	for _, x := range arr(s.body, "suggestions") {
		if str(x, "crew_ref") == d {
			t.Fatal("FM-07: tổ ngừng không bao giờ được gợi ý")
		}
	}
	if again := sup.get("/v1/fieldops/orders/" + o1 + "/crew-suggestions" + q); string(again.raw) != string(s.raw) {
		t.Fatalf("gợi ý phải tất định:\n%s\n%s", s.raw, again.raw)
	}
	mustCode(t, f.collector().get("/v1/fieldops/orders/"+o1+"/crew-suggestions"+q), http.StatusForbidden, "forbidden", "chỉ giám sát")

	// Giám sát xác nhận gợi ý hàng đầu; xác nhận lại (khóa khác) → không sinh phân công mới.
	conf := map[string]any{"planned_from": d0, "planned_to": d0, "mode": "suggested", "suggestion_rank": 1}
	mustStatus(t, f.assign(sup, o1, b, 1500, conf), http.StatusCreated, "xác nhận gợi ý")
	again := f.assign(sup, o1, b, 1500, conf)
	mustStatus(t, again, http.StatusOK, "xác nhận lặp")
	if h := arr(sup.get("/v1/fieldops/orders/"+o1+"/assignments").body, "items"); len(h) != 1 || str(h[0], "mode") != "suggested" ||
		str(h[0], "suggestion_rank") != "1" {
		t.Fatalf("không trùng phân công: %v", h)
	}
	if n := len(arr(sup.get("/v1/fieldops/orders/"+o1).body, "history")); n != 3 {
		t.Fatalf("lịch sử lệnh: planned, approved, assigned — có %d", n)
	}
	// Tải đã giao được tính: lệnh thứ hai cùng ngày → B không còn vừa, C lên đầu.
	s2 := sup.get("/v1/fieldops/orders/" + o2 + "/crew-suggestions" + q)
	if sg := arr(s2.body, "suggestions"); len(sg) != 1 || str(sg[0], "crew_ref") != c {
		t.Fatalf("gợi ý sau khi B đã nhận việc: %s", s2.raw)
	}
	// Xác nhận gợi ý đã cũ (B hết năng lực) → 409, không vượt năng lực qua đường gợi ý.
	mustCode(t, f.assign(sup, o2, b, 1500, conf), http.StatusConflict, "fieldops.crew_over_capacity", "gợi ý cũ")
}

// Lọc pull theo thành viên tổ (sổ tổ gom): tổ gom chỉ nhận lệnh đã giao cho tổ mình; giám sát
// nhận mọi lệnh; đổi thành viên / đổi tổ → máy cũ nhận tombstone, máy mới nhận upsert.
func TestSyncPullFiltersByCrewMembership(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	x, y, z := f.collector(), f.collector(), f.collector()
	for _, c := range []*caller{&x, &y, &z} {
		c.device = ids.New().String()
	}
	sup.device = ids.New().String()
	id := f.approvedOrder()
	op := func(c caller, cursor string) (string, string) {
		t.Helper()
		pl := c.get("/v1/fieldops/sync/pull?cursor=" + cursor)
		mustStatus(t, pl, http.StatusOK, "pull")
		last := ""
		for _, ch := range arr(pl.body, "changes") {
			if str(ch, "id") == id {
				last = str(ch, "op")
			}
		}
		return last, str(pl.body, "next_cursor")
	}
	if got, _ := op(sup, ""); got != "upsert" {
		t.Fatalf("giám sát thấy lệnh đã duyệt: %s", got)
	}
	gx, cx := op(x, "")
	gy, cy := op(y, "")
	if gx != "tombstone" || gy != "tombstone" {
		t.Fatalf("chưa giao tổ → tổ gom không thấy: %s %s", gx, gy)
	}
	crewA := f.crew("TO-X", "5000", x.actor)
	mustStatus(t, f.assign(sup, id, crewA, 100, nil), http.StatusCreated, "giao tổ X")
	if gx, cx = op(x, cx); gx != "upsert" {
		t.Fatalf("thành viên tổ được giao nhận upsert: %q", gx)
	}
	if gy, cy = op(y, cy); gy != "tombstone" {
		t.Fatalf("người ngoài tổ nhận tombstone: %q", gy)
	}
	// Đổi thành viên tổ: X ra, Y vào.
	rev := crewBody("TO-X", "5000", y.actor)
	mustStatus(t, sup.post("/v1/fieldops/crews/"+crewA+"/revisions", rev), http.StatusCreated, "đổi thành viên")
	if gx, cx = op(x, cx); gx != "tombstone" {
		t.Fatalf("thành viên cũ nhận tombstone: %q", gx)
	}
	if gy, cy = op(y, cy); gy != "upsert" {
		t.Fatalf("thành viên mới nhận upsert: %q", gy)
	}
	// Đổi tổ: sang tổ của Z.
	mustStatus(t, f.assign(sup, id, f.crew("TO-Z", "5000", z.actor), 100, nil), http.StatusCreated, "đổi tổ")
	if gy, _ = op(y, cy); gy != "tombstone" {
		t.Fatalf("tổ cũ nhận tombstone khi đổi tổ: %q", gy)
	}
	if gz, _ := op(z, ""); gz != "upsert" {
		t.Fatalf("tổ mới nhận upsert: %q", gz)
	}
	_ = cx
}
