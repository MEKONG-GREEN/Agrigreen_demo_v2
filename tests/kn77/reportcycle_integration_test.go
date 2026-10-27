package kn77_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// cycle là trạng thái chung của kịch bản đầy đủ.
type cycle struct {
	f                    *fixture
	forms                map[string]string
	tinh, xa1, xa2, bvtv caller
	tw, tinh92           caller
	admin                uuid.UUID // Admin nền tảng (chế độ hỗ trợ)
	xa1User, xa2User     uuid.UUID
	batch                string
	p1, p2               string
}

func setupCycle(t *testing.T) *cycle { return setupCycleWith(t, fixtureOpts{}) }

func setupCycleWith(t *testing.T, o fixtureOpts) *cycle {
	f := newFixture(t, o)
	c := &cycle{f: f, forms: f.publishForms("T01", "T05"), admin: ids.New(), xa1User: ids.New(), xa2User: ids.New()}
	c1, c2, c3 := f.communes89[0], f.communes89[1], f.communes89[2]
	f.asg.l = []accport.Assignment{
		{ID: ids.New(), UserRef: c.xa1User, Role: authz.KNXa, Departments: []string{dept}, Communes: []string{c1, c2}, Active: true, ValidFrom: t0.AddDate(0, -1, 0)},
		{ID: ids.New(), UserRef: c.xa2User, Role: authz.KNXa, Departments: []string{dept}, Communes: []string{c3}, Active: true, ValidFrom: t0.AddDate(0, -1, 0)},
		{ID: ids.New(), UserRef: ids.New(), Role: authz.KNXa, Departments: []string{"bao_ve_thuc_vat"}, Communes: []string{c1}, Active: true, ValidFrom: t0.AddDate(0, -1, 0)},
	}
	c.tinh = f.kn(f.tenant, authz.KNTinh, dept, f.p89, nil)
	c.xa1 = f.knFor(f.tenant, c.xa1User, authz.KNXa, dept, f.p89, []string{c1, c2}, nil)
	c.xa2 = f.knFor(f.tenant, c.xa2User, authz.KNXa, dept, f.p89, []string{c3}, nil)
	c.bvtv = f.kn(f.tenant, authz.KNXa, "bao_ve_thuc_vat", f.p89, []string{c1})
	c.tw = f.kn(f.tenant, authz.KNTW, dept, f.national, nil)
	c.tinh92 = f.kn(f.tenant, authz.KNTinh, dept, f.p92, nil)
	return c
}

func (c *cycle) issue(t *testing.T, codes ...string) map[string]any {
	tpls := []string{}
	for _, code := range codes {
		tpls = append(tpls, c.forms[code])
	}
	c1, c2, c3 := c.f.communes89[0], c.f.communes89[1], c.f.communes89[2]
	body := map[string]any{"report_type": "weekly", "template_ids": tpls, "scope": map[string]any{"mode": "province"},
		"from_date": "2026-09-07", "to_date": "2026-09-20", "season_label": "Thu Đông 2026", "note": "Đợt kiểm thử",
		"provincial_values": map[string]any{c.forms["T01"]: map[string]any{c1: map[string]string{"D": "100"}, c2: map[string]string{"D": "50"}, c3: map[string]string{"D": "80"}}}}
	r := c.tinh.must(c.tinh.do(c.f.app, http.MethodPost, "/v1/extension/report-batches", "issue-batch-0001", body), http.StatusCreated, "phát hành")
	// HTTP POST lặp cùng Idempotency-Key → cùng kết quả, không sinh đợt thứ hai.
	again := c.tinh.must(c.tinh.do(c.f.app, http.MethodPost, "/v1/extension/report-batches", "issue-batch-0001", body), http.StatusCreated, "phát hành lặp")
	if str(obj(again.body, "batch"), "id") != str(obj(r.body, "batch"), "id") {
		t.Fatalf("idempotency: hai đợt khác nhau")
	}
	c.batch = str(obj(r.body, "batch"), "id")
	ps := arr(r.body, "periods")
	c.p1, c.p2 = str(ps[0], "id"), str(ps[1], "id")
	return r.body
}

func (c *cycle) task(t *testing.T, who caller, form, commune, period string) map[string]any {
	t.Helper()
	l := who.must(who.get("/v1/extension/report-tasks?period_id="+period+"&form_code="+form+"&commune="+commune), http.StatusOK, "công việc")
	items := arr(l.body, "items")
	if len(items) != 1 {
		t.Fatalf("muốn 1 công việc %s/%s, có %d", form, commune, len(items))
	}
	return items[0]
}

func submit(who caller, taskID, kind string, input map[string]string) resp {
	return who.post("/v1/extension/report-tasks/"+taskID+"/submissions", map[string]any{"kind": kind, "rows": []any{map[string]any{"input": input}}})
}

func totalCell(totals []map[string]any, kind, region, letter string) string {
	for _, r := range totals {
		if str(r, "kind") == kind && str(r, "region") == region && str(r, "indicator") == "" {
			return str(obj(r, "cells"), letter)
		}
	}
	return ""
}

// TestFN_TI_02_KN77FullCycle: phát hành → công việc → nháp → gửi → nhắc → tổng hợp (nhật ký ô) → yêu cầu điều chỉnh →
// gửi lại → Cập nhật lần 2 → dashboard = tổng hợp → chốt kỳ (Hệ thống tổng hợp) → gửi sau chốt bị từ chối → mở lại
// cho một xã/biểu → gửi lại → hết hạn mở, job chốt lại (tổng hợp lại) → lịch sử phiên bản bất biến. Phạm vi theo ngữ
// cảnh (xã chỉ xã mình, phòng ban khác không thấy, tỉnh khác không thấy, TW chỉ xem), nhập thay của Admin + chặn tự
// xác nhận/chốt bản mình nhập thay, idempotency, cách ly tenant.
// FN-TI-02.01…02.05, FN-TW-01.01/01.02, FN-XA-03.01/03.02/03.07.
func TestFN_TI_02_KN77FullCycle(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	c1, c2, c3 := f.communes89[0], f.communes89[1], f.communes89[2]

	// Bước 1: mẫu đã gán cho Báo cáo tuần của phòng ban.
	fl := c.tinh.must(c.tinh.get("/v1/extension/report-forms?report_type=weekly&from_date=2026-09-07"), http.StatusOK, "mẫu tuần")
	if len(arr(fl.body, "items")) != 2 {
		t.Fatalf("2 mẫu tuần đã phát hành: %s", string(fl.raw))
	}
	// Xem trước (FN-TI-02.02 B5): 2 biểu × 3 xã × 2 kỳ = 12 công việc; tiêu đề cột theo kỳ đã chia.
	pv := c.tinh.must(c.tinh.post("/v1/extension/report-batches/preview", map[string]any{"report_type": "weekly",
		"template_ids": []string{c.forms["T01"], c.forms["T05"]}, "scope": map[string]any{"mode": "province"},
		"from_date": "2026-09-07", "to_date": "2026-09-20", "season_label": "Thu Đông 2026"}), http.StatusOK, "xem trước")
	if num(pv.body, "task_count") != 12 || num(pv.body, "period_count") != 2 || len(arr(pv.body, "communes")) != 3 {
		t.Fatalf("xem trước: %s", string(pv.raw))
	}
	if p := arr(pv.body, "periods")[0]; str(p, "label") != "Tuần 1" || str(p, "due_at") != "2026-09-15T10:00:00Z" || str(p, "auto_close_at") != "2026-09-18T10:00:00Z" {
		t.Fatalf("kỳ 1 (hạn Thứ Ba tuần sau 17:00, chốt +3 ngày): %v", p)
	}
	// Cấp Quốc gia chỉ xem; Cấp Xã không phát hành được.
	c.tw.fail(c.tw.post("/v1/extension/report-batches/preview", map[string]any{"report_type": "weekly"}), http.StatusForbidden, "extension.read_only", "TW phát hành")
	if r := c.xa1.post("/v1/extension/report-batches/preview", map[string]any{"report_type": "weekly"}); r.status != http.StatusForbidden {
		t.Fatalf("Cấp Xã phát hành: %d", r.status)
	}
	issued := c.issue(t, "T01", "T05")
	if num(obj(issued, "batch"), "task_count") != 12 {
		t.Fatalf("12 công việc: %v", obj(issued, "batch"))
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.report_task WHERE tenant_id = $1`, f.tenant); n != 12 {
		t.Fatalf("bảng công việc: %d", n)
	}
	ev := f.events(extport.EventReportBatchIssued)
	var ip extport.ReportBatchIssuedV1
	if len(ev) != 1 || json.Unmarshal(ev[0].Payload, &ip) != nil || ip.TaskCount != 12 || len(ip.Assignees) != 3 {
		t.Fatalf("sự kiện phát hành (một lần dù POST lặp): %d %+v", len(ev), ip)
	}
	for _, a := range ip.Assignees {
		if a.CommuneCode == c1 && (len(a.OfficerRefs) != 1 || a.OfficerRefs[0] != c.xa1User.String()) {
			t.Fatalf("FN-TI-02.02 AC: chỉ cán bộ cùng phòng ban nhận công việc xã %s: %v", c1, a.OfficerRefs)
		}
	}

	// FN-XA-03.01: Cán bộ xã thấy công việc của xã phụ trách; phòng ban khác không thấy.
	l1 := c.xa1.must(c.xa1.get("/v1/extension/report-tasks"), http.StatusOK, "công việc xã 1")
	if len(arr(l1.body, "items")) != 8 || num(obj(l1.body, "counts"), "todo") != 8 {
		t.Fatalf("xã 1 có 2 xã × 2 biểu × 2 kỳ = 8: %d %v", len(arr(l1.body, "items")), obj(l1.body, "counts"))
	}
	if str(arr(l1.body, "items")[0], "status") != "not_started" || str(arr(l1.body, "items")[0], "period_label") != "Tuần 1" {
		t.Fatalf("sắp theo hạn gần nhất, Chưa nhập: %v", arr(l1.body, "items")[0])
	}
	if l := c.bvtv.must(c.bvtv.get("/v1/extension/report-tasks"), http.StatusOK, "BVTV"); len(arr(l.body, "items")) != 0 {
		t.Fatalf("Cán bộ xã khác phòng ban không nhận: %d", len(arr(l.body, "items")))
	}
	c.xa1.fail(c.xa1.get("/v1/extension/report-tasks?commune="+c3), http.StatusForbidden, "extension.out_of_scope", "xã ngoài phạm vi")
	// Số đếm menu (P2 badge_source): công việc còn phải gửi của kỳ đã bắt đầu.
	bg := c.xa1.must(c.xa1.get("/v1/extension/report-badges"), http.StatusOK, "số đếm menu")
	if num(obj(bg.body, "counts"), "extension.report_tasks.todo") != 4 {
		t.Fatalf("badge công việc cần làm: %v", bg.body)
	}

	// FN-XA-03.02: lưu nháp rồi gửi (T01 Tuần 1, số tăng thêm tuần ở cột F).
	t1c1 := c.task(t, c.xa1, "T01", c1, c.p1)
	tid := str(t1c1, "id")
	d := c.xa1.must(submit(c.xa1, tid, "draft", map[string]string{"F": "10"}), http.StatusCreated, "lưu nháp")
	if str(obj(d.body, "task"), "status") != "draft" {
		t.Fatalf("Lưu nháp: %v", obj(d.body, "task"))
	}
	// Chỉ cột của kỳ đang mở được nhập (cột G = Tuần 2) → không cho gửi, báo tại ô.
	bad := submit(c.xa1, tid, "submitted", map[string]string{"F": "20", "G": "5"})
	if bad.status != http.StatusUnprocessableEntity || str(bad.body, "code") != "extension.submission_invalid" || !strings.Contains(string(bad.raw), "reports.cell_period_closed") {
		t.Fatalf("khe kỳ khác: %d %s", bad.status, string(bad.raw))
	}
	s1 := c.xa1.must(submit(c.xa1, tid, "submitted", map[string]string{"E": "90", "F": "20"}), http.StatusCreated, "gửi")
	if str(obj(s1.body, "task"), "status") != "submitted" {
		t.Fatalf("FN-XA-03.01 AC: Đã nộp ngay sau khi gửi: %v", obj(s1.body, "task"))
	}
	rows := arr(obj(s1.body, "submission"), "rows")
	if str(obj(rows[0], "cells"), "M") != "20" || str(obj(rows[0], "cells"), "D") != "100" {
		t.Fatalf("Lũy kế M = Σ tuần, D = Tỉnh giao: %v", obj(rows[0], "cells"))
	}
	// Ô Tỉnh giao không nhập tay; xã khác/TW không gửi được.
	if r := submit(c.xa1, tid, "draft", map[string]string{"D": "1"}); r.status != http.StatusCreated || !strings.Contains(string(r.raw), "reports.cell_not_editable") {
		t.Fatalf("nháp vẫn lưu, báo ô Tỉnh giao không sửa: %d %s", r.status, string(r.raw))
	}
	c.xa2.fail(submit(c.xa2, tid, "submitted", map[string]string{"F": "1"}), http.StatusNotFound, "extension.report_task_not_found", "xã khác")
	c.tw.fail(submit(c.tw, tid, "submitted", map[string]string{"F": "1"}), http.StatusForbidden, "extension.read_only", "TW gửi")
	// Lần nháp sau khi gửi không đổi trạng thái "Đã nộp".
	again := c.xa1.must(submit(c.xa1, tid, "submitted", map[string]string{"E": "90", "F": "20"}), http.StatusCreated, "gửi lại cùng số")
	_ = again
	c.xa1.must(submit(c.xa1, str(c.task(t, c.xa1, "T01", c2, c.p1), "id"), "submitted", map[string]string{"F": "15"}), http.StatusCreated, "xã 2 gửi")

	// FN-AD-04.07: Admin nhập thay xã 3 (chế độ hỗ trợ) — bản nộp mang on_behalf_of; thiếu lý do → 422.
	sup := &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: c.xa2User}
	adminXa := f.knFor(f.tenant, c.admin, authz.KNXa, dept, f.p89, []string{c3}, sup)
	t1c3 := str(c.task(t, adminXa, "T01", c3, c.p1), "id")
	adminXa.fail(submit(adminXa, t1c3, "submitted", map[string]string{"F": "30"}), http.StatusUnprocessableEntity, "extension.support_reason_required", "thiếu lý do")
	adminXa.reason = "Hỗ trợ xã nhập thay do mất mạng"
	sa := adminXa.must(submit(adminXa, t1c3, "submitted", map[string]string{"F": "30"}), http.StatusCreated, "nhập thay")
	if str(obj(sa.body, "submission"), "on_behalf_of") != c.xa2User.String() || str(obj(sa.body, "submission"), "actor_ref") != c.admin.String() {
		t.Fatalf("on_behalf_of: %v", obj(sa.body, "submission"))
	}

	// FN-TI-02.03: trạng thái nộp thời gian thực + nhắc.
	st := c.tinh.must(c.tinh.get("/v1/extension/report-batches/"+c.batch+"/status?period_id="+c.p1+"&template_id="+c.forms["T01"]), http.StatusOK, "trạng thái")
	if num(obj(st.body, "counts"), "submitted") != 3 || len(arr(st.body, "items")) != 3 {
		t.Fatalf("3 xã đã nộp T01: %v", obj(st.body, "counts"))
	}
	rm := c.tinh.must(c.tinh.post("/v1/extension/report-batches/"+c.batch+"/reminders", map[string]any{"period_id": c.p1, "template_id": c.forms["T05"],
		"all_pending": true, "message": "Đề nghị nộp PL 04 trước 17:00"}), http.StatusCreated, "nhắc")
	if num(obj(rm.body, "reminder"), "recipient_count") != 2 || len(obj(rm.body, "reminder")["communes"].([]any)) != 3 {
		t.Fatalf("nhắc 3 xã chưa nộp T05 tới 2 cán bộ: %v", obj(rm.body, "reminder"))
	}
	if ev := f.events(extport.EventReportTaskReminded); len(ev) != 1 {
		t.Fatalf("sự kiện nhắc: %d", len(ev))
	}
	l1 = c.xa1.must(c.xa1.get("/v1/extension/report-tasks"), http.StatusOK, "công việc + nhắc")
	if str(obj(l1.body, "last_reminder"), "message") != "Đề nghị nộp PL 04 trước 17:00" {
		t.Fatalf("khung nhắc mới nhất: %v", l1.body["last_reminder"])
	}

	// FN-TI-02.05: "Cập nhật" → lần tổng hợp 1 (mọi ô "Gửi mới"); Khu vực, Toàn tỉnh = Σ xã.
	cons := c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusCreated, "cập nhật 1")
	if len(arr(cons.body, "runs")) != 2 {
		t.Fatalf("2 biểu: %s", string(cons.raw))
	}
	view := c.tinh.must(c.tinh.get("/v1/extension/consolidations?period_id="+c.p1+"&form_code=T01"), http.StatusOK, "bảng tổng hợp")
	run1 := obj(view.body, "run")
	if num(run1, "communes_with_data") != 3 || str(run1, "trigger") != "manual" || run1["provisional"] != true {
		t.Fatalf("lần 1: %v", run1)
	}
	if totalCell(arr(view.body, "totals"), "province", "", "M") != "65" || totalCell(arr(view.body, "totals"), "region", "KV1", "M") != "35" {
		t.Fatalf("Toàn tỉnh M = 20+15+30, KV1 = 35: %v", arr(view.body, "totals"))
	}
	for _, m := range arr(view.body, "changed_cells") {
		if str(m, "kind") != "new" && str(m, "kind") != "computed" {
			t.Fatalf("lần 1 chỉ Gửi mới/Tự tính: %v", m)
		}
	}
	nop := c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusOK, "cập nhật lặp")
	if str(nop.body, "message") != "Dữ liệu đã mới nhất" {
		t.Fatalf("không thay đổi: %s", string(nop.raw))
	}

	// FN-TI-02.04: yêu cầu điều chỉnh bắt buộc lý do.
	c.tinh.fail(c.tinh.post("/v1/extension/report-tasks/"+tid+"/adjustment-request", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity,
		"validation_failed", "lý do ngắn")
	adj := c.tinh.must(c.tinh.post("/v1/extension/report-tasks/"+tid+"/adjustment-request", map[string]any{"reason": "Số tuần 1 lệch so với báo cáo nhanh của xã"}),
		http.StatusOK, "yêu cầu điều chỉnh")
	if str(obj(adj.body, "task"), "status") != "adjustment_requested" || str(obj(adj.body, "task"), "adjustment_reason") == "" {
		t.Fatalf("Yêu cầu điều chỉnh: %v", obj(adj.body, "task"))
	}
	var ar extport.SubmissionAdjustmentRequestedV1
	if ev := f.events(extport.EventSubmissionAdjustmentRequested); len(ev) != 1 || json.Unmarshal(ev[0].Payload, &ar) != nil || ar.RecipientRefs[0] != c.xa1User.String() {
		t.Fatalf("Cán bộ xã nhận yêu cầu kèm lý do: %+v", ar)
	}
	// Maker-checker: Admin đã nhập thay xã 3 không tự xác nhận/chốt bản của mình (kể cả khi chuyển sang vai Cấp Tỉnh).
	adminTinh := f.knFor(f.tenant, c.admin, authz.KNTinh, dept, f.p89, nil, &authz.KNSupport{SessionID: ids.New()})
	adminTinh.reason = "Hỗ trợ Cấp Tỉnh rà soát báo cáo"
	adminTinh.fail(adminTinh.post("/v1/extension/report-tasks/"+t1c3+"/confirm", map[string]any{}), http.StatusForbidden, "self_approval_forbidden", "tự xác nhận")
	adminTinh.fail(adminTinh.post("/v1/extension/report-periods/"+c.p1+"/close", map[string]any{}), http.StatusForbidden, "self_approval_forbidden", "tự chốt")
	c.tinh.must(c.tinh.post("/v1/extension/report-tasks/"+t1c3+"/confirm", map[string]any{}), http.StatusOK, "người khác xác nhận")

	// Xã gửi lại theo yêu cầu → "Thay đổi chờ cập nhật" (Điều chỉnh theo yêu cầu).
	c.xa1.must(submit(c.xa1, tid, "submitted", map[string]string{"E": "90", "F": "25"}), http.StatusCreated, "gửi lại")
	pend := c.tinh.must(c.tinh.get("/v1/extension/consolidations/pending-changes?period_id="+c.p1), http.StatusOK, "chờ cập nhật")
	if num(pend.body, "changes") != 1 || str(arr(pend.body, "items")[0], "kind") != "adjusted" {
		t.Fatalf("Có 1 thay đổi mới từ 1 xã: %s", string(pend.raw))
	}
	cons2 := c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1, "template_ids": []string{c.forms["T01"]}}),
		http.StatusCreated, "cập nhật 2")
	run2 := arr(cons2.body, "runs")[0]
	if num(run2, "run_no") != 2 || num(run2, "communes_changed") != 1 {
		t.Fatalf("lần 2: %v", run2)
	}
	ch := c.tinh.must(c.tinh.get("/v1/extension/consolidations/"+str(run2, "id")+"/changes"), http.StatusOK, "nhật ký ô")
	kinds := map[string]string{}
	for _, it := range arr(ch.body, "items") {
		kinds[str(it, "letter")] = str(it, "kind") + ":" + str(it, "old") + "→" + str(it, "new")
		if str(it, "commune_code") != c1 || str(it, "changed_by") != c.xa1User.String() {
			t.Fatalf("ô thay đổi: %v", it)
		}
	}
	if kinds["F"] != "adjusted:20→25" || kinds["M"] != "computed:20→25" {
		t.Fatalf("FN-TI-02.05 nhật ký: cột Excel, cũ → mới, loại: %v", kinds)
	}

	// FN-TI-02.01 AC: số dashboard = bảng tổng hợp cùng kỳ; TW chỉ xem, phải chọn tỉnh.
	view2 := c.tinh.must(c.tinh.get("/v1/extension/consolidations?period_id="+c.p1+"&form_code=T01"), http.StatusOK, "bảng 2")
	consM := totalCell(arr(view2.body, "totals"), "province", "", "M")
	dash := c.tinh.must(c.tinh.get("/v1/extension/kn-dashboard?period_id="+c.p1), http.StatusOK, "dashboard tỉnh")
	if v := obj(obj(dash.body, "cards"), "sown_area")["value"]; v != consM || consM != "70" {
		t.Fatalf("dashboard DT xuống giống %v ≠ tổng hợp %s", v, consM)
	}
	if v := obj(obj(dash.body, "cards"), "sown_plan")["value"]; v != "230" {
		t.Fatalf("KH tỉnh giao: %v", v)
	}
	dr := c.tinh.must(c.tinh.get("/v1/extension/kn-dashboard?period_id="+c.p1+"&region=KV1"), http.StatusOK, "dashboard khu vực")
	if v := obj(obj(dr.body, "cards"), "sown_area")["value"]; v != totalCell(arr(view2.body, "totals"), "region", "KV1", "M") {
		t.Fatalf("lọc Khu vực: %v", v)
	}
	c.tw.fail(c.tw.get("/v1/extension/kn-dashboard"), http.StatusUnprocessableEntity, "validation_failed", "TW chưa chọn tỉnh")
	dtw := c.tw.must(c.tw.get("/v1/extension/kn-dashboard?province=89&period_id="+c.p1), http.StatusOK, "dashboard TW")
	if obj(obj(dtw.body, "cards"), "sown_area")["value"] != consM || dtw.body["read_only"] != true {
		t.Fatalf("TW cùng số: %v", obj(dtw.body, "cards"))
	}
	c.tw.must(c.tw.get("/v1/extension/consolidations?period_id="+c.p1+"&form_code=T01"), http.StatusOK, "TW xem báo cáo tỉnh")
	c.tw.fail(c.tw.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusForbidden, "extension.read_only", "TW cập nhật")
	// Tỉnh khác không thấy đợt, kỳ của tỉnh 89.
	c.tinh92.fail(c.tinh92.get("/v1/extension/report-batches/"+c.batch), http.StatusNotFound, "extension.report_batch_not_found", "tỉnh khác")
	c.tinh92.fail(c.tinh92.get("/v1/extension/consolidations?period_id="+c.p1), http.StatusNotFound, "extension.report_period_not_found", "tỉnh khác xem")

	// FN-TI-02.04: chốt kỳ → Hệ thống tổng hợp một lần lúc chốt; gửi sau chốt bị từ chối.
	cl := c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/close", map[string]any{}), http.StatusOK, "chốt kỳ")
	if str(obj(cl.body, "period"), "status") != "closed" {
		t.Fatalf("đã chốt: %v", obj(cl.body, "period"))
	}
	sysRun := arr(cl.body, "runs")[0]
	if str(sysRun, "trigger") != "system" || str(sysRun, "actor_ref") != extport.SystemActor.String() {
		t.Fatalf("FN-TI-02.05 phụ: lần tổng hợp 'Hệ thống' lúc chốt: %v", sysRun)
	}
	// W6.2: ghi song song sang cap/forms — sự kiện closed (mirror) + ảnh chụp đóng băng, băm SHA-256 khớp nội dung.
	if n := f.count(`SELECT count(*) FROM pf_forms.close_snapshot s JOIN pf_forms.period_event e ON e.id = s.period_event_id
	    WHERE s.tenant_id = $1 AND s.period_ref = $2::uuid AND s.seq = 1 AND e.origin = 'mirror' AND e.action = 'closed' AND e.mode = 'manual'
	      AND s.profile = 'kn' AND s.content_hash = sha256(convert_to(s.content::text, 'UTF8')) AND jsonb_array_length(s.content->'tasks') > 0`,
		f.tenant, c.p1); n != 1 {
		t.Fatalf("cap/forms: ảnh chụp lúc chốt: %d", n)
	}
	c.xa1.fail(submit(c.xa1, tid, "submitted", map[string]string{"F": "26"}), http.StatusConflict, "extension.period_closed", "gửi sau chốt")
	c.tinh.fail(c.tinh.post("/v1/extension/report-tasks/"+tid+"/adjustment-request", map[string]any{"reason": "Cần sửa lại số tuần 1 của xã"}), http.StatusConflict,
		"extension.adjustment_after_close", "điều chỉnh sau chốt")
	// Mở lại kỳ cho một xã/biểu (lý do + hạn mở) → xã gửi lại.
	until := f.clk.Now().Add(48 * time.Hour).Format(time.RFC3339)
	c.tinh.fail(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/reopen", map[string]any{"communes": []string{c1}, "reason": "x", "until": until}),
		http.StatusUnprocessableEntity, "validation_failed", "mở lại thiếu lý do")
	ro := c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/reopen", map[string]any{"communes": []string{c1}, "template_ids": []string{c.forms["T01"]},
		"reason": "Xã phát hiện nhập nhầm số tuần 1", "until": until}), http.StatusOK, "mở lại")
	if len(ro.body["task_ids"].([]any)) != 1 || ro.body["dispatch_changed"] != false {
		t.Fatalf("mở lại 1 công việc: %v", ro.body)
	}
	c.xa1.must(submit(c.xa1, tid, "submitted", map[string]string{"E": "90", "F": "26"}), http.StatusCreated, "gửi trong hạn mở")
	c.xa1.fail(submit(c.xa1, str(c.task(t, c.xa1, "T01", c2, c.p1), "id"), "submitted", map[string]string{"F": "16"}), http.StatusConflict,
		"extension.period_closed", "xã khác vẫn chốt")
	// Hết hạn mở → job chốt lại + tổng hợp lại ("phiên bản điều chỉnh").
	f.clk.Advance(49 * time.Hour)
	jr, err := f.ext.ReportCycleJobRun(context.Background(), f.tenant, f.clk.Now())
	if err != nil || jr.Reclosed != 1 {
		t.Fatalf("job chốt lại: %+v %v", jr, err)
	}
	// W6.2: mở lại + chốt lại (Hệ thống) cũng ghi sang cap/forms — ảnh chụp mới, ảnh lúc chốt giữ nguyên.
	if n := f.count(`SELECT count(*) FROM pf_forms.period_event WHERE tenant_id = $1 AND period_ref = $2::uuid AND origin = 'mirror'`, f.tenant, c.p1); n != 3 {
		t.Fatalf("cap/forms: sự kiện chốt, mở lại, chốt lại: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_forms.close_snapshot WHERE tenant_id = $1 AND period_ref = $2::uuid
	    AND content_hash = sha256(convert_to(content::text, 'UTF8'))`, f.tenant, c.p1); n != 2 {
		t.Fatalf("cap/forms: hai ảnh chụp nguyên vẹn: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_forms.period_event WHERE tenant_id = $1 AND period_ref = $2::uuid AND action = 'reopened'
	    AND units = ARRAY[$3]::text[] AND reason IS NOT NULL`, f.tenant, c.p1, c1); n != 1 {
		t.Fatalf("cap/forms: mở lại xã %s: %d", c1, n)
	}
	c.xa1.fail(submit(c.xa1, tid, "submitted", map[string]string{"F": "27"}), http.StatusConflict, "extension.period_closed", "sau hạn mở")
	view3 := c.tinh.must(c.tinh.get("/v1/extension/consolidations?period_id="+c.p1+"&form_code=T01"), http.StatusOK, "bảng sau chốt lại")
	if totalCell(arr(view3.body, "totals"), "province", "", "M") != "71" || str(obj(view3.body, "run"), "trigger") != "system" || str(view3.body, "status_label") != "Đã chốt" {
		t.Fatalf("tổng hợp lại sau chốt lại: %v %v", obj(view3.body, "run"), arr(view3.body, "totals"))
	}
	runs := c.tinh.must(c.tinh.get("/v1/extension/consolidations/runs?period_id="+c.p1+"&template_id="+c.forms["T01"]), http.StatusOK, "nhật ký cập nhật")
	if n := len(arr(runs.body, "items")); n != 4 {
		t.Fatalf("4 lần tổng hợp T01 (2 tay, chốt, chốt lại) — không xóa: %d", n)
	}

	// RB-1: mọi lần lưu/gửi là phiên bản bất biến; sổ không sửa/xóa được.
	h := c.xa1.must(c.xa1.get("/v1/extension/report-tasks/"+tid+"/history"), http.StatusOK, "lịch sử")
	if n := len(arr(h.body, "items")); n != 6 {
		t.Fatalf("6 phiên bản (nháp, nháp lỗi, gửi, gửi cùng số, gửi lại, gửi khi mở lại): %d", n)
	}
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE pf_extension.submission_version SET kind = 'draft' WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("sổ bài nộp phải chặn UPDATE")
	}
	lg := c.tinh.must(c.tinh.get("/v1/extension/report-tasks/"+tid+"/log"), http.StatusOK, "log công việc")
	if !strings.Contains(string(lg.raw), "task.adjustment_requested") || !strings.Contains(string(lg.raw), "batch.issued") {
		t.Fatalf("khung Log: %s", string(lg.raw))
	}

	// FN-XA-03.02 kỳ 2: cột Tuần 1 lấy giá trị đã gửi (chỉ xem), Lũy kế cộng dồn các tuần.
	t2c1 := str(c.task(t, c.xa1, "T01", c1, c.p2), "id")
	td := c.xa1.must(c.xa1.get("/v1/extension/report-tasks/"+t2c1), http.StatusOK, "chi tiết kỳ 2")
	if str(obj(obj(td.body, "carried"), c1), "F") != "26" || len(obj(td.body, "layout")["open_slots"].([]any)) != 1 {
		t.Fatalf("giá trị khe kỳ khác: %v %v", td.body["carried"], obj(td.body, "layout"))
	}
	// BF-TT-FORMS (#107, UP-04) Nhập từ Excel: đọc tệp theo hàng chữ cái cột, không ghi; Cấp Tỉnh không được nhập.
	gridImport := func(who caller, body string) resp {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, f.app.URL+"/v1/extension/report-tasks/"+t2c1+"/grid-import", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+who.token)
		req.Header.Set("Content-Type", "text/csv; charset=utf-8")
		req.Header.Set(authz.ContextHeader, who.ctxID.String())
		req.Header.Set("Idempotency-Key", key())
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Body.Close() }()
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return resp{status: r.StatusCode, body: m, raw: raw}
	}
	csvOf := func(lines ...string) string { return strings.Join(lines, "\n") + "\n" }
	before := len(arr(c.xa1.get("/v1/extension/report-tasks/"+t2c1+"/history").body, "items"))
	gi := c.xa1.must(gridImport(c.xa1, csvOf("A,B,C,D,E,F", "Cố định,Cố định,Cố định,Tỉnh giao,Nhập,Kỳ", "1,KV,Xã A,,90,26")), http.StatusOK, "nhập từ Excel")
	if rows := arr(gi.body, "rows"); len(rows) != 1 || str(obj(rows[0], "cells"), "F") != "26" || rows[0]["line"] != float64(3) {
		t.Fatalf("nhập từ Excel: %s", string(gi.raw))
	}
	if n := len(arr(c.xa1.get("/v1/extension/report-tasks/"+t2c1+"/history").body, "items")); n != before {
		t.Fatalf("nhập từ Excel không được ghi phiên bản: %d → %d", before, n)
	}
	c.xa1.fail(gridImport(c.xa1, csvOf("Xã,Diện tích", "A,1")), http.StatusUnprocessableEntity, "extension.grid_import_invalid", "tệp sai bố cục")
	c.tinh.fail(gridImport(c.tinh, csvOf("A,B,C", "1,2,3")), http.StatusForbidden, "forbidden", "Cấp Tỉnh nhập lưới")
	s2 := c.xa1.must(submit(c.xa1, t2c1, "submitted", map[string]string{"E": "90", "F": "26", "G": "5"}), http.StatusCreated, "gửi kỳ 2")
	if str(obj(arr(obj(s2.body, "submission"), "rows")[0], "cells"), "M") != "31" {
		t.Fatalf("Lũy kế kỳ 2 = 26 + 5: %v", arr(obj(s2.body, "submission"), "rows"))
	}

	// N-07: máy ngoại tuyến gửi lại cùng id (UUIDv7) với Idempotency-Key khác → 200 trả bản đã có, không thêm phiên bản.
	cid := ids.New().String()
	off := map[string]any{"id": cid, "kind": "draft", "rows": []any{map[string]any{"input": map[string]string{"E": "90", "F": "26", "G": "5"}}}}
	c.xa1.must(c.xa1.post("/v1/extension/report-tasks/"+t2c1+"/submissions", off), http.StatusCreated, "ngoại tuyến lần 1")
	rp := c.xa1.must(c.xa1.post("/v1/extension/report-tasks/"+t2c1+"/submissions", off), http.StatusOK, "ngoại tuyến gửi lại")
	if str(obj(rp.body, "submission"), "id") != cid || len(arr(c.xa1.get("/v1/extension/report-tasks/"+t2c1+"/history").body, "items")) != 2 {
		t.Fatalf("gửi lại cùng id: %s", string(rp.raw))
	}

	// FN-XA-03.07: thống kê của xã khớp bản đã gửi.
	cs := c.xa1.must(c.xa1.get("/v1/extension/commune-reports/stats?commune="+c1), http.StatusOK, "thống kê xã")
	if obj(cs.body, "sown_area")["value"] != "31" || obj(cs.body, "sown_plan")["value"] != "100" {
		t.Fatalf("thống kê xã: %v", cs.body)
	}
	c.xa1.fail(c.xa1.get("/v1/extension/commune-reports/stats?commune="+c3), http.StatusForbidden, "extension.out_of_scope", "thống kê xã khác")

	// Cách ly tenant: ngữ cảnh của tenant khác không thấy đợt (RLS).
	other := f.kn(ids.New(), authz.KNTinh, dept, f.p89, nil)
	other.fail(other.get("/v1/extension/report-batches/"+c.batch), http.StatusNotFound, "extension.report_batch_not_found", "tenant khác")
	if l := other.must(other.get("/v1/extension/report-tasks"), http.StatusOK, "tenant khác"); len(arr(l.body, "items")) != 0 {
		t.Fatalf("tenant khác thấy công việc")
	}

	// P2: kỳ chưa chốt của nhánh Nút (bảng Ảnh hưởng) và dữ liệu theo phòng ban.
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: f.tenant})
	if n, err := f.ext.OpenPeriodsImpact().OrgImpact(ctx, f.p89, []string{c1}); err != nil || n != 1 {
		t.Fatalf("kỳ chưa chốt của xã %s: %d %v", c1, n, err)
	}
	if u, err := f.ext.ReportUsage().DepartmentUsage(ctx, f.p89, dept); err != nil || u.Records != 12 {
		t.Fatalf("số bản ghi của phòng ban: %+v %v", u, err)
	}
}

// TestFN_TI_02_06_ChiCucExportAndEmail: xuất Excel/PDF từ lần tổng hợp gần nhất (số trong tệp = số tổng hợp), qua điểm
// kiểm soát DLP, gửi email kèm tệp tới hộp thư đơn vị + CC (bộ gửi giả), log gửi; Google Sheet tắt mặc định; mở lại
// kỳ đã gửi Chi cục → cảnh báo "số liệu đã thay đổi".
func TestFN_TI_02_06_ChiCucExportAndEmail(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	c1 := f.communes89[0]
	c.issue(t, "T01")
	c.xa1.must(submit(c.xa1, str(c.task(t, c.xa1, "T01", c1, c.p1), "id"), "submitted", map[string]string{"F": "12.5"}), http.StatusCreated, "gửi")
	c.tinh.fail(c.tinh.post("/v1/reports/dispatches", map[string]any{"period_id": c.p1}), http.StatusConflict, "reports.dispatch_no_data", "chưa tổng hợp")
	c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusCreated, "cập nhật")

	// Danh bạ: hộp thư đơn vị (Q-09 tạm), địa chỉ sai → 422.
	c.tinh.fail(c.tinh.post("/v1/reports/dispatch-recipients", map[string]any{"label": "Chi cục", "address": "khong-hop-le", "kind": "to"}),
		http.StatusUnprocessableEntity, "validation_failed", "địa chỉ sai")
	to := c.tinh.must(c.tinh.post("/v1/reports/dispatch-recipients", map[string]any{"label": "Chi cục Trồng trọt và BVTV", "address": "trongtrot.chicuc@angiang.gov.vn",
		"kind": "to"}), http.StatusCreated, "người nhận")
	c.tinh.must(c.tinh.post("/v1/reports/dispatch-recipients", map[string]any{"label": "Văn thư TTKN", "address": "vanthu@ttkn.angiang.gov.vn", "kind": "cc"}),
		http.StatusCreated, "CC")
	if l := c.tw.must(c.tw.get("/v1/reports/dispatch-recipients?province=89"), http.StatusOK, "TW xem danh bạ"); !strings.Contains(string(l.raw), "•••") {
		t.Fatalf("TW chỉ thấy địa chỉ đã che: %s", string(l.raw))
	}
	c.tinh.fail(c.tinh.post("/v1/reports/dispatches", map[string]any{"period_id": c.p1, "formats": []string{"gsheet"}}), http.StatusConflict,
		"reports.gsheet_disabled", "Google Sheet tắt mặc định (ADR-0026)")

	ex := c.tinh.must(c.tinh.post("/v1/reports/dispatches", map[string]any{"period_id": c.p1, "formats": []string{"xlsx", "pdf"}}), http.StatusCreated, "xuất file")
	files := arr(ex.body, "files")
	if len(files) != 2 || str(obj(ex.body, "dispatch"), "status") != "exported" || obj(ex.body, "dispatch")["provisional"] != true {
		t.Fatalf("2 tệp Tạm tính: %s", string(ex.raw))
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.export_audit WHERE tenant_id = $1 AND kind = 'dispatch'`, f.tenant); n != 2 {
		t.Fatalf("điểm kiểm soát DLP: %d", n)
	}
	// Số trong tệp Excel = số tổng hợp.
	var xlsxKey string
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT object_key FROM pf_reports.dispatch_file WHERE tenant_id = $1 AND format = 'xlsx'`,
		f.tenant).Scan(&xlsxKey); err != nil {
		t.Fatal(err)
	}
	did := str(obj(ex.body, "dispatch"), "id")
	dl := c.tinh.must(c.tinh.get("/v1/reports/dispatches/"+did+"/files/"+str(files[0], "id")+"/download"), http.StatusOK, "tải tệp")
	if !strings.HasPrefix(str(dl.body, "url"), "http://files.test") {
		t.Fatalf("URL tải: %v", dl.body)
	}

	// Gửi email (202) → worker gửi kèm tệp tới người nhận + CC.
	sd := c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{}), http.StatusAccepted, "gửi email")
	if str(obj(sd.body, "dispatch"), "status") != "sending" || len(obj(sd.body, "dispatch")["recipient_ids"].([]any)) != 1 {
		t.Fatalf("đang gửi tới người nhận mặc định: %v", obj(sd.body, "dispatch"))
	}
	ev := f.events(repport.EventDispatchRequested)
	if len(ev) != 1 {
		t.Fatalf("sự kiện gửi: %d", len(ev))
	}
	if err := tenant.WithTenantTx(context.Background(), f.repPool, f.tenant, func(tx pgx.Tx) error {
		return f.rep.DispatchHandler()(context.Background(), tx, ev[0])
	}); err != nil {
		t.Fatal(err)
	}
	ms := f.mail.all()
	if len(ms) != 1 || ms[0].To != "trongtrot.chicuc@angiang.gov.vn" || len(ms[0].CC) != 1 || len(ms[0].Attachments) != 2 {
		t.Fatalf("email kèm 2 tệp + CC: %+v", ms)
	}
	if !strings.Contains(ms[0].Subject, "Tuần 1") || !strings.Contains(ms[0].Text, "TẠM TÍNH") {
		t.Fatalf("tiêu đề/nội dung tự điền: %q %q", ms[0].Subject, ms[0].Text)
	}
	var data []byte
	for _, a := range ms[0].Attachments {
		if strings.HasSuffix(a.Name, ".xlsx") {
			data = a.Data
		}
	}
	x, err := excelize.OpenReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	rows, _ := x.GetRows(x.GetSheetName(0))
	for _, r := range rows {
		for i, v := range r {
			if v == "12.5" && i == 5 { // cột F
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Excel phải có số 12.5 ở cột F (số trong file = số tổng hợp): %v", rows)
	}
	g := c.tinh.must(c.tinh.get("/v1/reports/dispatches/"+did), http.StatusOK, "log gửi")
	if str(obj(g.body, "dispatch"), "status") != "sent" || !strings.Contains(string(g.raw), "dispatch.sent") || !strings.Contains(string(g.raw), "dispatch.exported") {
		t.Fatalf("mọi lần gửi có trong log: %s", string(g.raw))
	}
	_ = to
	// Mở lại kỳ đã gửi Chi cục → cảnh báo "số liệu đã thay đổi".
	c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/close", map[string]any{}), http.StatusOK, "chốt")
	ro := c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/reopen", map[string]any{"reason": "Mở lại để xã bổ sung số liệu",
		"until": f.clk.Now().Add(24 * time.Hour).Format(time.RFC3339)}), http.StatusOK, "mở lại")
	if ro.body["dispatch_changed"] != true {
		t.Fatalf("cảnh báo số liệu đã thay đổi: %v", ro.body["dispatch_changed"])
	}
	var pr extport.PeriodReopenedV1
	if e := f.events(extport.EventPeriodReopened); len(e) != 1 || json.Unmarshal(e[0].Payload, &pr) != nil || !pr.DispatchChanged {
		t.Fatalf("sự kiện mở lại: %+v", pr)
	}
}

// TestFN_TI_02_04_AutoCloseAndReminders: job tự nhắc trước hạn (một lần trong cửa sổ), khi trễ hạn, rồi tự chốt theo
// lịch (hạn + số ngày chốt) kèm tổng hợp "Hệ thống"; sửa hạn chỉ áp kỳ chưa đến hạn; thu hồi đợt khi đã có xã nộp bị
// chặn (FN-TI-02.02 luồng phụ, FN-TI-02.03 phụ, Q-15 tạm 24 giờ).
func TestFN_TI_02_04_AutoCloseAndReminders(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	c.issue(t, "T01")
	ctx := context.Background()
	// 24 giờ trước hạn kỳ 1 (15/09 17:00 VN).
	f.setClock(time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC))
	if r, err := f.ext.ReportCycleJobRun(ctx, f.tenant, f.clk.Now()); err != nil || r.Reminders != 1 {
		t.Fatalf("tự nhắc trước hạn: %+v %v", r, err)
	}
	if r, _ := f.ext.ReportCycleJobRun(ctx, f.tenant, f.clk.Now().Add(time.Hour)); r.Reminders != 0 {
		t.Fatalf("không nhắc lặp trong cửa sổ: %+v", r)
	}
	// Sửa hạn: kỳ 1 chưa đến hạn → đổi; lý do bắt buộc.
	dl := c.tinh.must(c.tinh.post("/v1/extension/report-batches/"+c.batch+"/deadline", map[string]any{"deadline": map[string]any{"offset_days": 3, "time": "17:00"},
		"reason": "Lùi hạn do mưa bão ảnh hưởng điều tra"}), http.StatusOK, "sửa hạn")
	if len(arr(dl.body, "changes")) != 2 || str(arr(dl.body, "changes")[0], "new_due_at") != "2026-09-16T10:00:00Z" {
		t.Fatalf("sửa hạn: %s", string(dl.raw))
	}
	// Quá hạn mới → tự nhắc khi trễ hạn.
	f.setClock(time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC))
	if r, _ := f.ext.ReportCycleJobRun(ctx, f.tenant, f.clk.Now()); r.Reminders != 1 {
		t.Fatalf("tự nhắc khi trễ: %+v", r)
	}
	l := c.xa1.must(c.xa1.get("/v1/extension/report-tasks?period_id="+c.p1), http.StatusOK, "trễ hạn")
	if str(arr(l.body, "items")[0], "status") != "overdue" {
		t.Fatalf("Trễ hạn: %v", arr(l.body, "items")[0])
	}
	// Trễ hạn vẫn nộp được tới khi chốt kỳ.
	c.xa1.must(submit(c.xa1, str(arr(l.body, "items")[0], "id"), "submitted", map[string]string{"F": "3"}), http.StatusCreated, "nộp trễ")
	c.tinh.fail(c.tinh.post("/v1/extension/report-batches/"+c.batch+"/withdraw", map[string]any{"reason": "Phát hành nhầm mẫu cho đợt"}), http.StatusConflict,
		"extension.batch_has_submissions", "thu hồi khi đã có xã nộp")
	// Giờ chốt tự động = hạn + 3 ngày.
	f.setClock(time.Date(2026, 9, 19, 10, 0, 1, 0, time.UTC))
	r, err := f.ext.ReportCycleJobRun(ctx, f.tenant, f.clk.Now())
	if err != nil || r.Closed != 1 {
		t.Fatalf("tự chốt: %+v %v", r, err)
	}
	pv := c.tinh.must(c.tinh.get("/v1/extension/report-periods/"+c.p1), http.StatusOK, "kỳ")
	if str(obj(pv.body, "period"), "close_mode") != "auto" || len(arr(pv.body, "runs")) != 1 || str(arr(pv.body, "runs")[0], "trigger") != "system" {
		t.Fatalf("chốt tự động + tổng hợp Hệ thống: %s", string(pv.raw))
	}
	if r, _ := f.ext.ReportCycleJobRun(ctx, f.tenant, f.clk.Now()); r.Closed != 0 {
		t.Fatalf("job idempotent: %+v", r)
	}
	var pc extport.PeriodClosedV1
	if e := f.events(extport.EventPeriodClosed); len(e) != 1 || json.Unmarshal(e[0].Payload, &pc) != nil || pc.Mode != "auto" {
		t.Fatalf("sự kiện chốt: %+v", pc)
	}
	// Đợt chưa ai nộp → thu hồi được (sự kiện bù).
	b2 := c.tinh.must(c.tinh.post("/v1/extension/report-batches", map[string]any{"report_type": "weekly", "template_ids": []string{c.forms["T05"]},
		"scope": map[string]any{"mode": "communes", "communes": []string{f.communes89[0]}}, "from_date": "2026-09-21", "to_date": "2026-09-27",
		"season_label": "Thu Đông 2026"}), http.StatusCreated, "đợt 2")
	bid := str(obj(b2.body, "batch"), "id")
	w := c.tinh.must(c.tinh.post("/v1/extension/report-batches/"+bid+"/withdraw", map[string]any{"reason": "Phát hành nhầm phạm vi xã"}), http.StatusOK, "thu hồi")
	if str(w.body, "status") != "withdrawn" {
		t.Fatalf("thu hồi: %v", w.body)
	}
	if l := c.xa1.must(c.xa1.get("/v1/extension/report-tasks?batch_id="+bid), http.StatusOK, "sau thu hồi"); len(arr(l.body, "items")) != 0 {
		t.Fatalf("công việc của đợt thu hồi không còn hiện")
	}
}
