package acceptance

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Khuyến nông (pf/extension): nhiệm vụ hỗ trợ có SLA, báo cáo hiện trường, dashboard.

func vnPeriod() string { return time.Now().In(clock.Zone()).Format("2006-01") }
func vnToday() string  { return time.Now().In(clock.Zone()).Format("2006-01-02") }

func (w *world) task(u *harness.User, desc string) map[string]any {
	w.t.Helper()
	return w.want(u.Do(harness.App, http.MethodPost, "/v1/extension/tasks", "", map[string]any{
		"commune_code": communeNK, "category": "sau_benh", "priority": "high", "cooperative_ref": w.coopNK, "description": desc,
	}), 201, "tạo nhiệm vụ").Body
}

func (w *world) staff(u *harness.User, actor uuid.UUID, unit string, active bool) {
	w.t.Helper()
	w.want(u.Do(harness.App, http.MethodPost, "/v1/extension/staff", "", map[string]any{
		"actor_ref": actor.String(), "unit_code": unit, "role_title": "Khuyến nông viên xã", "display_name": "KNV " + unit, "active": active,
	}), 200, "đăng ký danh bạ trực")
}

func (w *world) sowing(u *harness.User, commune, coop, variety, area string) harness.Resp {
	return u.Do(harness.App, http.MethodPost, "/v1/extension/field-reports", "", map[string]any{
		"kind": "sowing_progress", "commune_code": commune, "cooperative_ref": coop, "period": vnPeriod(),
		"season_code": "DX", "variety_code": variety, "sown_area_ha": area, "sown_on": vnToday(),
	})
}

func (w *world) dashboard92() map[string]any {
	w.t.Helper()
	r := w.want(w.provAdmin.Do(harness.App, http.MethodGet, "/v1/extension/dashboard?unit="+provCT+"&period="+vnPeriod(), "", nil), 200, "dashboard tỉnh")
	return obj(r.Body, "total")
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-HTX-12/10: yêu cầu hỗ trợ lưu đủ mô tả, loại vấn đề, mức khẩn; thiếu mô tả bị chặn".
// Khác prototype: không nhận tên trường cũ (urgency/content); mô tả không được chứa SĐT/CCCD
// (dữ liệu cá nhân chỉ ở kho định danh, chọn nông hộ bằng mã tham chiếu).
func TestPrototypeYeuCauHoTroThieuMoTa(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-HTX-12/10: yêu cầu hỗ trợ lưu đủ mô tả, loại vấn đề, mức khẩn; thiếu mô tả bị chặn")
	w := newWorld(t)
	post := func(body map[string]any) harness.Resp {
		b := map[string]any{"commune_code": communeNK, "category": "co_gioi", "priority": "urgent"}
		for k, v := range body {
			b[k] = v
		}
		return w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", "", b)
	}
	wantFieldError(t, post(map[string]any{}), "description", "thiếu mô tả")
	wantFieldError(t, post(map[string]any{"description": "Máy gặt hỏng giữa vụ", "category": "khong_co"}), "category", "loại vấn đề ngoài danh mục")
	wantFieldError(t, post(map[string]any{"description": "Gọi ông Ba số 0999000777 để hỏi thêm"}), "description", "mô tả chứa SĐT")
	r := w.want(post(map[string]any{"description": "Máy gặt đập liên hợp hỏng bộ đập giữa vụ."}), 201, "yêu cầu đủ thông tin")
	if str(r.Body, "category") != "co_gioi" || str(r.Body, "priority") != "urgent" || str(r.Body, "description") != "Máy gặt đập liên hợp hỏng bộ đập giữa vụ." {
		t.Fatalf("phải lưu đủ mô tả, loại vấn đề, mức khẩn: %s", trim(r.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Yêu cầu hỗ trợ quá 24 giờ chưa tiếp nhận → leo thang lên TTKN tỉnh một lần duy nhất".
// Khác prototype: leo thang do job của worker (dựng như cmd/worker, đồng hồ giả qua hạn) — sự kiện
// "escalated" trong sổ + pf.extension.task.escalated ở outbox, chạy lặp không sinh thêm.
func TestPrototypeLeoThangMotLanDuyNhat(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Yêu cầu hỗ trợ quá 24 giờ chưa tiếp nhận → leo thang lên TTKN tỉnh một lần duy nhất")
	w := newWorld(t)
	task := w.task(w.officer, "Rầy nâu mật độ cao ở cánh đồng phía đông, cần hướng dẫn phun thuốc")
	id := str(task, "task_id")
	due, err := time.Parse(time.RFC3339Nano, str(task, "due_at"))
	if err != nil || str(task, "sla_hours") != "24" || str(task, "escalation_org_ref") != w.orgP92 {
		t.Fatalf("SLA mặc định 24 giờ, nơi nhận leo thang là TTKN tỉnh: %v", task)
	}
	m := w.s.Mods
	worker := extension.New(w.s.PF(extension.DBRole), clock.NewFixed(due.Add(6*time.Hour)), extension.Deps{
		Scope: m.Party.ScopeResolver(), Orgs: m.Party.Orgs(), Units: m.MasterData.AdminUnits(), Params: m.MasterData.Params(), Catalogs: m.MasterData.Catalogs(),
	})
	jobs := worker.Jobs(extension.OverdueTenants(w.s.PF(dbadmin.OpsJobs)), nil)
	if len(jobs) != 1 || jobs[0].Name != extension.JobEscalation {
		t.Fatalf("job leo thang: %v", jobs)
	}
	for i := 0; i < 2; i++ {
		if err := jobs[0].Run(context.Background()); err != nil {
			t.Fatalf("job leo thang lần %d: %v", i+1, err)
		}
	}
	var n int
	if err := w.s.PF(dbadmin.OutboxPublisher).QueryRow(context.Background(),
		"SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = 'pf.extension.task.escalated' AND payload->>'task_id' = $2", w.tid, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	got := w.want(w.provAdmin.Do(harness.App, http.MethodGet, "/v1/extension/tasks/"+id, "", nil), 200, "TTKN tỉnh đọc nhiệm vụ")
	tk := obj(got.Body, "task")
	escalations := 0
	for _, e := range got.Items("events") {
		if str(e, "action") == "escalated" {
			escalations++
		}
	}
	if n != 1 || escalations != 1 || tk["escalated_at"] == nil || str(tk, "escalated_unit") != provCT {
		t.Fatalf("phải leo thang đúng một lần lên tỉnh %s (outbox %d, sổ %d): %s", provCT, n, escalations, trim(got.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-KN-TASK-01: không phân công cho tài khoản khoá; không nhiệm vụ nào nhận được thì báo lỗi".
// Khác prototype: "tài khoản khoá" ⇔ cán bộ ngừng hoạt động trong danh bạ trực (tài khoản ở IdP);
// người nhận còn phải phụ trách đúng xã/tỉnh; phân công hàng loạt là một giao dịch — một nhiệm vụ
// lỗi thì không nhiệm vụ nào đổi.
func TestPrototypePhanCongCanBoKhongHopLe(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-KN-TASK-01: không phân công cho tài khoản khoá; không nhiệm vụ nào nhận được thì báo lỗi")
	w := newWorld(t)
	w.staff(w.officer, w.officer.Actor, communeNK, true)
	w.staff(w.officerBT, w.officerBT.Actor, communeBT, true)
	locked := w.user(w.teamNK, false, "kn_officer")
	w.staff(w.provAdmin, locked.Actor, communeNK, false)

	open := str(w.task(w.officer, "Hướng dẫn kỹ thuật bón phân đợt hai"), "task_id")
	assign := func(task string, to uuid.UUID) harness.Resp {
		return w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks/"+task+"/assign", "", map[string]any{"assignee_ref": to.String()})
	}
	wantProblem(t, assign(open, locked.Actor), 422, "extension.assignee_invalid", "giao cho cán bộ đã ngừng")
	wantProblem(t, assign(open, uuid.Must(uuid.NewV7())), 422, "extension.assignee_invalid", "giao cho người ngoài danh bạ")
	wantProblem(t, assign(open, w.officerBT.Actor), 422, "extension.assignee_invalid", "giao cho cán bộ xã khác")

	done := str(w.task(w.officer, "Sâu cuốn lá nhỏ xuất hiện rải rác"), "task_id")
	w.want(assign(done, w.officer.Actor), 200, "tự nhận nhiệm vụ")
	w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks/"+done+"/status", "", map[string]any{
		"action": "resolved", "note": "Đã hướng dẫn phun thuốc đúng liều",
	}), 200, "hoàn thành nhiệm vụ")

	bulk := func(tasks ...string) harness.Resp {
		return w.provAdmin.Do(harness.App, http.MethodPost, "/v1/extension/tasks/bulk-assign", "", map[string]any{
			"task_ids": tasks, "assignee_refs": []string{w.officer.Actor.String()},
		})
	}
	wantFieldError(t, bulk(), "task_ids", "chưa chọn nhiệm vụ")
	wantProblem(t, bulk(open, done), 409, "extension.task_state_conflict", "có nhiệm vụ đã hoàn thành trong lô")
	got := w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/extension/tasks/"+open, "", nil), 200, "nhiệm vụ còn mở")
	if str(obj(got.Body, "task"), "status") != "new" {
		t.Fatalf("lô lỗi không được đổi nhiệm vụ nào: %s", trim(got.Raw))
	}
	ok := w.want(bulk(open), 200, "phân công hàng loạt hợp lệ")
	if items := ok.Items(); len(items) != 1 || str(items[0], "status") != "assigned" || str(items[0], "assignee_ref") != w.officer.Actor.String() {
		t.Fatalf("phân công hàng loạt: %s", trim(ok.Raw))
	}
}

// Nguồn: mekong-green/tests/htx-operations.test.ts — "SV-02 — nhập lại cùng đối tượng trong cùng kỳ là CẬP NHẬT, không tạo bản trùng".
// Khác prototype: gửi lại cùng khóa (xã × HTX × kỳ × vụ × giống) tạo PHIÊN BẢN mới trỏ về bản cũ
// (lịch sử giữ nguyên); tổng hợp chỉ cộng phiên bản hiện hành.
func TestPrototypeBaoCaoGuiLaiLaPhienBanMoi(t *testing.T) {
	nguon(t, "htx-operations.test.ts", "SV-02 — nhập lại cùng đối tượng trong cùng kỳ là CẬP NHẬT, không tạo bản trùng")
	w := newWorld(t)
	first := w.want(w.sowing(w.officer, communeNK, w.coopNK, "OM5451", "12.5"), 201, "báo cáo lần 1")
	second := w.want(w.sowing(w.officer, communeNK, w.coopNK, "OM5451", "15"), 201, "báo cáo lần 2")
	if num(second.Body, "version") != 2 || str(second.Body, "previous_id") != str(first.Body, "id") {
		t.Fatalf("lần 2 phải là phiên bản 2 của cùng số liệu: %s", trim(second.Raw))
	}
	if got := dec(t, w.dashboard92()["sown_area_ha"]); !got.Equal(dec(t, "15")) {
		t.Fatalf("tổng hợp chỉ cộng phiên bản hiện hành (15 ha), nhận %s", got)
	}
	d := w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/extension/field-reports/"+str(first.Body, "id"), "", nil), 200, "đọc báo cáo")
	if len(d.Items("versions")) != 2 {
		t.Fatalf("lịch sử phải giữ cả 2 phiên bản: %s", trim(d.Raw))
	}
	cur := w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/extension/field-reports?kind=sowing_progress&period="+vnPeriod(), "", nil), 200, "báo cáo hiện hành")
	if len(cur.Items()) != 1 {
		t.Fatalf("chỉ được có một số liệu hiện hành, có %d", len(cur.Items()))
	}
}

// Nguồn: mekong-green/tests/htx-operations.test.ts — "SV-01/SV-03/SV-05 — mẫu phải ban hành, câu bắt buộc phải trả lời, kỳ đúng định dạng".
// Khác prototype: G1 không có mẫu khảo sát động — báo cáo hiện trường có trường bắt buộc cố định;
// thêm quy tắc chốt kỳ (US-DASH-02): kỳ đã chốt chỉ nhận bản ghi bù kèm lý do ≥ 10 ký tự.
func TestPrototypeKyBaoCaoVaChotKy(t *testing.T) {
	nguon(t, "htx-operations.test.ts", "SV-01/SV-03/SV-05 — mẫu phải ban hành, câu bắt buộc phải trả lời, kỳ đúng định dạng")
	w := newWorld(t)
	post := func(body map[string]any) harness.Resp {
		return w.officer.Do(harness.App, http.MethodPost, "/v1/extension/field-reports", "", body)
	}
	base := func() map[string]any {
		return map[string]any{"kind": "sowing_progress", "commune_code": communeNK, "cooperative_ref": w.coopNK, "period": vnPeriod(),
			"season_code": "DX", "variety_code": "OM18", "sown_area_ha": "3.1"}
	}
	b := base()
	b["period"] = "2026-W12"
	wantFieldError(t, post(b), "period", "kỳ sai định dạng YYYY-MM")
	b = base()
	b["period"] = "2099-01"
	wantFieldError(t, post(b), "period", "kỳ tương lai")
	b = base()
	delete(b, "sown_area_ha")
	wantFieldError(t, post(b), "sown_area_ha", "thiếu trường bắt buộc")

	r := w.want(post(base()), 201, "báo cáo trong kỳ mở")
	w.want(w.provAdmin.Do(harness.App, http.MethodPost, "/v1/extension/report-periods/close", "", map[string]any{"period": vnPeriod(), "province_code": provCT, "note": "Chốt số liệu tháng"}), 201, "chốt kỳ")
	wantProblem(t, post(base()), 409, "extension.period_closed", "báo cáo mới sau khi chốt kỳ")
	corr := "/v1/extension/field-reports/" + str(r.Body, "id") + "/corrections"
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, corr, "", map[string]any{"sown_area_ha": "3.4"}), 422, "extension.correction_reason_required", "bản ghi bù thiếu lý do")
	c := w.want(w.officer.Do(harness.App, http.MethodPost, corr, "", map[string]any{"sown_area_ha": "3.4", "correction_reason": "Đo lại diện tích thực sạ"}), 201, "bản ghi bù")
	if num(c.Body, "version") != 2 || str(c.Body, "supersedes_id") != str(r.Body, "id") {
		t.Fatalf("bản ghi bù phải là phiên bản mới thay bản cũ: %s", trim(c.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Báo cáo tổng hợp KN gắn cờ "tạm tính" khi còn nhật ký chờ duyệt trong phạm vi".
// Khác prototype: G1 không có nhật ký chờ duyệt — "tạm tính" khi còn xã có cán bộ phụ trách
// (danh bạ trực) chưa báo cáo trong kỳ (US-DASH-02 AC-1), kèm danh sách xã còn thiếu.
func TestPrototypeDashboardTamTinh(t *testing.T) {
	nguon(t, "brd-2026.test.ts", `Báo cáo tổng hợp KN gắn cờ "tạm tính" khi còn nhật ký chờ duyệt trong phạm vi`)
	w := newWorld(t)
	w.staff(w.officer, w.officer.Actor, communeNK, true)
	w.staff(w.officerBT, w.officerBT.Actor, communeBT, true)
	w.want(w.sowing(w.officer, communeNK, w.coopNK, "OM5451", "10"), 201, "Ninh Kiều báo cáo")
	tot := w.dashboard92()
	missing, _ := tot["missing_communes"].([]any)
	if tot["provisional"] != true || str(tot, "provisional_label") != "Số liệu tạm tính" || len(missing) != 1 || missing[0] != communeBT {
		t.Fatalf("còn xã chưa báo cáo → tạm tính, chỉ ra xã thiếu: %v", tot)
	}
	w.want(w.sowing(w.officerBT, communeBT, w.coopBT, "OM5451", "5"), 201, "Cái Răng báo cáo")
	if tot = w.dashboard92(); tot["provisional"] != false || !dec(t, tot["sown_area_ha"]).Equal(dec(t, "15")) {
		t.Fatalf("đủ xã báo cáo → hết tạm tính: %v", tot)
	}
}
