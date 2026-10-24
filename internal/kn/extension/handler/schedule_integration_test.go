package handler_test

// KN v7.7 P5 — lịch công tác tuần: FN-XA-03.08 (Cán bộ xã đăng ký, gửi duyệt), FN-TI-02.07 (Cấp Tỉnh duyệt,
// yêu cầu chỉnh sửa, Chưa đăng ký, Lịch của tôi). t0 = Thứ Ba 15/09/2026 10:00 giờ VN → tuần kế tiếp 21/09/2026,
// hạn đăng ký mặc định 17:00 Thứ Sáu 18/09/2026.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

const nextWeek = "2026-09-21"

func sItem(day, session, start, end, content string) map[string]any {
	m := map[string]any{"day": day, "session": session, "content": content}
	if start != "" {
		m["start_time"] = start
	}
	if end != "" {
		m["end_time"] = end
	}
	return m
}

func (c caller) saveSched(week, action string, base any, items ...map[string]any) resp {
	c.f.t.Helper()
	if items == nil {
		items = []map[string]any{}
	}
	c.fill(items...)
	return c.post("/v1/extension/work-schedules", map[string]any{"week_start": week, "action": action, "base_version": base, "items": items})
}

// commune: xã phụ trách của cán bộ xã gọi (theo đơn vị tổ chức của fixture).
func (c caller) commune() string {
	if c.org == c.f.org.t30002 {
		return "30002"
	}
	return "30001"
}

// fill: log bug TT-05/CN-04 — Xã/phường (cán bộ xã) và Địa điểm bắt buộc; mục mẫu không ghi thì lấy xã phụ trách
// của chính người gọi và địa điểm mặc định. Mục đã ghi sẵn (kể cả xã sai để kiểm 422) giữ nguyên.
func (c caller) fill(items ...map[string]any) []map[string]any {
	for _, it := range items {
		if _, ok := it["commune_code"]; !ok {
			it["commune_code"] = c.commune()
		}
		if _, ok := it["location"]; !ok && it["place"] == nil {
			it["location"] = "Trụ sở UBND xã"
		}
	}
	return items
}

func schedID(r resp) string      { return str(r.body, "schedule", "id") }
func schedStatus(r resp) string  { return str(r.body, "schedule", "status") }
func schedVersion(r resp) int    { return int(num(r.body, "schedule", "version")) }
func schedItems(r resp) []any    { v, _ := r.body["items"].([]any); return v }
func warningCodes(r resp) string { return joinField(r.body["warnings"], "code") }

func joinField(v any, field string) string {
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			s, _ := m[field].(string)
			out = append(out, s)
		}
	}
	return strings.Join(out, ",")
}

func (c caller) review(id, decision string, version int, reason string) resp {
	c.f.t.Helper()
	path := "/v1/extension/work-schedules/" + id + "/approve"
	body := map[string]any{"version": version}
	if decision == "change" {
		path = "/v1/extension/work-schedules/" + id + "/request-changes"
		body["reason"] = reason
	}
	return c.post(path, body)
}

func (f *fixture) scheduleOutbox(eventType, scheduleID string) []map[string]any {
	f.t.Helper()
	var out []map[string]any
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT payload::text FROM shared.outbox WHERE event_type = $1
		    AND ($2 = '' OR payload->>'schedule_id' = $2) ORDER BY occurred_at, id`, eventType, scheduleID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			var m map[string]any
			_ = json.Unmarshal([]byte(s), &m)
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// FN-XA-03.08 luồng chính + FN-TI-02.07: Nháp → Chờ duyệt → Yêu cầu chỉnh sửa (lý do bắt buộc) → gửi lại →
// Đã duyệt; mặc định tuần kế tiếp; trùng giờ là cảnh báo; gọi lặp cùng khóa; thông báo qua sự kiện; Log cũ → mới.
func TestFN_XA_03_08_RegisterSubmitReviewFlow(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	off := p.off30001a

	mine := off.must(off.get("/v1/extension/work-schedules/mine"), http.StatusOK, "lịch của tôi")
	if str(mine.body, "week_start") != nextWeek || mine.body["schedule"] != nil || mine.body["late_now"] != false {
		t.Fatalf("mặc định tuần kế tiếp, chưa có lịch: %v", mine.body)
	}
	// BF-SCHED R-B1: tuần chưa có lịch (schedule null) vẫn trả cấp và xã phụ trách theo phiên KN để app chọn xã.
	if str(mine.body, "owner_level") != "commune" || joinRefs(mine.body["commune_codes"]) != "30001" {
		t.Fatalf("mine tuần mới phải có owner_level=commune, commune_codes=[30001]: %v", mine.body)
	}
	if !strings.HasPrefix(str(mine.body, "submit_due_at"), "2026-09-18T10:00:00") { // 17:00 VN Thứ Sáu
		t.Fatalf("hạn đăng ký mặc định 17:00 Thứ Sáu tuần trước: %v", mine.body["submit_due_at"])
	}

	key := key()
	body := map[string]any{"week_start": nextWeek, "action": "draft", "items": off.fill(
		sItem("2026-09-21", "morning", "08:00", "10:00", "Kiểm tra đồng ruộng ấp 1"),
		sItem("2026-09-21", "morning", "09:00", "11:00", "Họp HTX Tân Phú"),
		sItem("2026-09-23", "afternoon", "", "", "Tập huấn IPM cho nông dân"),
	)}
	d := off.must(off.call(http.MethodPost, "/v1/extension/work-schedules", key, body), http.StatusCreated, "lưu nháp")
	if schedStatus(d) != "draft" || schedVersion(d) != 1 || len(schedItems(d)) != 3 {
		t.Fatalf("nháp phiên bản 1 với 3 mục: %v", d.body)
	}
	if !strings.Contains(warningCodes(d), "time_overlap") || num(d.body, "schedule", "conflict_count") != 2 {
		t.Fatalf("trùng giờ 08–10 và 09–11 → cảnh báo, không chặn: %v", d.body["warnings"])
	}
	id := schedID(d)
	// Gọi lặp cùng Idempotency-Key: không thêm phiên bản.
	if r := off.call(http.MethodPost, "/v1/extension/work-schedules", key, body); r.status != http.StatusCreated && r.status != http.StatusOK {
		t.Fatalf("gọi lặp: %d", r.status)
	} else if schedVersion(r) != 1 {
		t.Fatalf("gọi lặp giữ phiên bản 1: %v", r.body["schedule"])
	}
	// Nháp chưa gửi thì Cấp Tỉnh không thấy ở Chờ duyệt.
	if l := p.admin89.must(p.admin89.get("/v1/extension/work-schedules?week_start="+nextWeek+"&status=pending"), http.StatusOK, "chờ duyệt"); len(items(l, "items")) != 0 {
		t.Fatal("nháp không hiện ở Chờ duyệt")
	}

	sub := off.must(off.saveSched(nextWeek, "submit", 1,
		sItem("2026-09-21", "morning", "08:00", "10:00", "Kiểm tra đồng ruộng ấp 1"),
		sItem("2026-09-23", "afternoon", "", "", "Tập huấn IPM cho nông dân")), http.StatusCreated, "gửi duyệt")
	if schedStatus(sub) != "pending" || schedVersion(sub) != 2 || sub.body["schedule"].(map[string]any)["late"] != false {
		t.Fatalf("gửi duyệt → Chờ duyệt v2, không muộn: %v", sub.body["schedule"])
	}
	// FN-TI-02.07 AC: lịch Cán bộ xã gửi hiện ngay ở "Chờ duyệt".
	l := p.admin89.must(p.admin89.get("/v1/extension/work-schedules?week_start="+nextWeek+"&status=pending"), http.StatusOK, "chờ duyệt")
	if got := items(l, "items"); len(got) != 1 || str(got[0], "id") != id || len(got[0]["items"].([]any)) != 2 {
		t.Fatalf("Cấp Tỉnh thấy ngay lịch chờ duyệt kèm mục: %v", l.body)
	}
	if ev := f.scheduleOutbox(port.EventWorkScheduleSubmitted, id); len(ev) != 1 || !strings.Contains(joinRefs(ev[0]["approver_refs"]), p.admin89.actor.String()) {
		t.Fatalf("sự kiện gửi duyệt kèm người duyệt cấp tỉnh: %v", ev)
	}

	// Yêu cầu chỉnh sửa bắt buộc lý do.
	if r := p.admin89.review(id, "change", 2, "ngắn"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do dưới 10 ký tự → 422, nhận %d", r.status)
	}
	if r := p.admin89.review(id, "change", 1, "Bổ sung địa điểm cụ thể cho buổi tập huấn"); r.status != http.StatusConflict || code(r) != "extension.schedule_version_conflict" {
		t.Fatalf("duyệt phiên bản cũ → 409: %d %v", r.status, r.body)
	}
	rc := p.admin89.must(p.admin89.review(id, "change", 2, "Bổ sung địa điểm cụ thể cho buổi tập huấn"), http.StatusOK, "yêu cầu chỉnh sửa")
	if schedStatus(rc) != "changes_requested" || str(rc.body, "schedule", "last_reason") != "Bổ sung địa điểm cụ thể cho buổi tập huấn" {
		t.Fatalf("Yêu cầu chỉnh sửa kèm lý do: %v", rc.body["schedule"])
	}
	// FN-XA-03.08 AC: Cán bộ xã nhận thông báo kết quả duyệt kèm lý do (sự kiện cho pf/notify).
	if ev := f.scheduleOutbox(port.EventWorkScheduleReviewed, id); len(ev) != 1 || ev[0]["decision"] != "change_requested" ||
		ev[0]["reason"] != "Bổ sung địa điểm cụ thể cho buổi tập huấn" || ev[0]["owner_ref"] != off.actor.String() {
		t.Fatalf("sự kiện kết quả duyệt kèm lý do: %v", ev)
	}
	mine = off.must(off.get("/v1/extension/work-schedules/mine"), http.StatusOK, "lịch của tôi")
	if str(mine.body, "schedule", "schedule", "last_reason") == "" || mine.body["schedule"].(map[string]any)["can"].(map[string]any)["submit"] != true {
		t.Fatalf("cán bộ xã thấy lý do và được gửi lại: %v", mine.body["schedule"])
	}

	re := off.must(off.saveSched(nextWeek, "submit", 2,
		sItem("2026-09-21", "morning", "08:00", "10:00", "Kiểm tra đồng ruộng ấp 1"),
		map[string]any{"id": str(items(rc, "items")[1], "id"), "day": "2026-09-23", "session": "afternoon", "content": "Tập huấn IPM cho nông dân", "location": "Nhà văn hóa ấp 2"}),
		http.StatusCreated, "gửi lại")
	if schedStatus(re) != "pending" || schedVersion(re) != 3 {
		t.Fatalf("gửi lại → Chờ duyệt v3: %v", re.body["schedule"])
	}
	ap := p.admin89.must(p.admin89.review(id, "approve", 3, ""), http.StatusOK, "duyệt")
	if schedStatus(ap) != "approved" || num(ap.body, "schedule", "approved_version") != 3 || str(ap.body, "schedule", "reviewer_ref") != p.admin89.actor.String() {
		t.Fatalf("Đã duyệt v3: %v", ap.body["schedule"])
	}
	// Duyệt lặp (khác khóa) → không còn chờ duyệt.
	if r := p.admin89.review(id, "approve", 3, ""); r.status != http.StatusConflict || code(r) != "extension.schedule_state_conflict" {
		t.Fatalf("duyệt lại lịch đã duyệt → 409: %d %v", r.status, r.body)
	}

	// AC-CHUNG: Log có mọi sự việc, người, thời điểm, cũ → mới, lý do — mới nhất trước.
	lg := off.must(off.get("/v1/extension/work-schedules/"+id+"/log"), http.StatusOK, "log")
	evs := items(lg, "items")
	var names []string
	for _, e := range evs {
		names = append(names, str(e, "event"))
	}
	if strings.Join(names, ",") != "approved,submitted,change_requested,submitted,draft_saved" {
		t.Fatalf("thứ tự Log: %v", names)
	}
	if str(evs[2], "reason") != "Bổ sung địa điểm cụ thể cho buổi tập huấn" {
		t.Fatalf("Log yêu cầu chỉnh sửa có lý do: %v", evs[2])
	}
	if !hasChange(evs[1], "Địa điểm", "", "Nhà văn hóa ấp 2") || !hasChange(evs[3], "Bỏ mục", "", "") {
		t.Fatalf("Log ghi cũ → mới của mục: %v / %v", evs[1]["changes"], evs[3]["changes"])
	}
	if str(evs[0], "actor_display") != "Cán bộ 89" {
		t.Fatalf("Log tra tên người duyệt lúc đọc (RB-3): %v", evs[0])
	}
	// Phân trang Log theo seq.
	pg := off.must(off.get("/v1/extension/work-schedules/"+id+"/log?limit=2"), http.StatusOK, "log trang 1")
	if len(items(pg, "items")) != 2 || pg.body["next_cursor"] == nil {
		t.Fatalf("trang 1 của Log: %v", pg.body)
	}
	cur := strconv.Itoa(int(pg.body["next_cursor"].(float64)))
	if pg2 := off.must(off.get("/v1/extension/work-schedules/"+id+"/log?limit=2&cursor="+cur), http.StatusOK, "log trang 2"); str(items(pg2, "items")[0], "event") != "change_requested" {
		t.Fatalf("trang 2 của Log: %v", pg2.body)
	}
	// Phiên bản cũ vẫn xem được (sổ chỉ ghi thêm, RB-1).
	if v1 := off.must(off.get("/v1/extension/work-schedules/"+id+"?version=1"), http.StatusOK, "phiên bản 1"); len(schedItems(v1)) != 3 || num(v1.body, "viewing_version") != 1 {
		t.Fatalf("xem phiên bản 1: %v", v1.body)
	}
}

func joinRefs(v any) string {
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		s, _ := x.(string)
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

func hasChange(entry map[string]any, field, old, new string) bool {
	arr, _ := entry["changes"].([]any)
	for _, x := range arr {
		m := x.(map[string]any)
		f, _ := m["field"].(string)
		o, _ := m["old"].(string)
		n, _ := m["new"].(string)
		if strings.HasSuffix(f, field) && (old == "" || o == old) && (new == "" || n == new) {
			return true
		}
	}
	return false
}

// R-56 (FN-TI-02.07): người duyệt ≠ người đăng ký — cán bộ tỉnh không tự duyệt "Lịch của tôi"; cán bộ tỉnh khác
// cùng phòng ban duyệt được (Q-16 "peer"); cấu hình "auto" → tự duyệt khi gửi, ghi sự việc duyệt của Hệ thống.
func TestFN_TI_02_07_SelfApprovalBlockedAndProvinceOwnSchedule(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	peer := f.as(f.org.p89, port.RoleKNProvinceAdmin)

	own := p.admin89.must(p.admin89.saveSched(nextWeek, "submit", nil, sItem("2026-09-22", "morning", "", "", "Kiểm tra tiến độ xuống giống khu vực 1")),
		http.StatusCreated, "lịch của tôi")
	if str(own.body, "schedule", "owner_level") != "province" || schedStatus(own) != "pending" {
		t.Fatalf("lịch cán bộ tỉnh chờ duyệt: %v", own.body["schedule"])
	}
	id := schedID(own)
	if own.body["can"].(map[string]any)["approve"] != false {
		t.Fatal("người đăng ký không thấy nút Duyệt lịch của mình")
	}
	if r := p.admin89.review(id, "approve", 1, ""); r.status != http.StatusForbidden || code(r) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt → 403 self_approval_forbidden: %d %v", r.status, r.body)
	}
	if r := p.admin89.review(id, "change", 1, "Tự yêu cầu chỉnh sửa lịch của chính mình"); r.status != http.StatusForbidden || code(r) != "self_approval_forbidden" {
		t.Fatalf("tự yêu cầu chỉnh sửa → 403: %d %v", r.status, r.body)
	}
	peer.must(peer.review(id, "approve", 1, ""), http.StatusOK, "cán bộ tỉnh khác duyệt")

	// DB chặn lần hai (CHECK actor_ref <> owner_ref) kể cả khi tầng ứng dụng bị bỏ qua.
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_extension.work_schedule_event
		  (id, tenant_id, stream_key, schedule_id, version, action, status_after, owner_ref, owner_level, department_code,
		   province_code, week_start, item_count, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'wsched:x', $3, 1, 'approved', 'approved', $4, 'province', 'trong_trot', '89', '2026-09-21', 1, now(), $4, $5, 'x')`,
			ids.New(), f.tenant, uuid.MustParse(id), p.admin89.actor, key())
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "check") {
		t.Fatalf("DB phải chặn tự duyệt: %v", err)
	}

	// Q-16 "auto" theo tỉnh 89: gửi = tự duyệt.
	f.params.setText(port.ParamScheduleProvinceApproval+":89", port.ProvinceApprovalAuto)
	st := peer.must(peer.get("/v1/extension/work-schedules/settings"), http.StatusOK, "cấu hình")
	if st.body["province_approval"] != "auto" || st.body["provisional"] != false {
		t.Fatalf("cấu hình theo tỉnh: %v", st.body)
	}
	auto := peer.must(peer.saveSched(nextWeek, "submit", nil, sItem("2026-09-24", "afternoon", "13:30", "16:00", "Họp giao ban phòng Trồng trọt")),
		http.StatusCreated, "gửi khi tự duyệt")
	if schedStatus(auto) != "approved" || str(auto.body, "schedule", "reviewer_display") != "Hệ thống" {
		t.Fatalf("auto: Đã duyệt bởi Hệ thống: %v", auto.body["schedule"])
	}
	if ev := f.scheduleOutbox(port.EventWorkScheduleReviewed, schedID(auto)); len(ev) != 1 || ev[0]["auto"] != true {
		t.Fatalf("sự kiện duyệt tự động: %v", ev)
	}
}

// FN-TI-02.07 AC: không xem được lịch Phòng ban/tỉnh khác; Cấp Xã chỉ thấy lịch của mình; Cấp Quốc gia không có
// quyền; tách tenant (RLS).
func TestFN_TI_02_07_ScopeAndTenantIsolation(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	d := p.off30001a.must(p.off30001a.saveSched(nextWeek, "submit", nil, sItem("2026-09-21", "morning", "", "", "Thăm đồng lúa Thu Đông")),
		http.StatusCreated, "gửi")
	id := schedID(d)

	for _, c := range []struct {
		name string
		who  caller
	}{{"cán bộ xã cùng xã", p.off30001b}, {"cán bộ xã xã khác", p.off30002}, {"tỉnh khác", p.admin92}, {"cán bộ xã tỉnh khác", p.off31001}} {
		if r := c.who.get("/v1/extension/work-schedules/" + id); r.status != http.StatusNotFound || code(r) != "extension.schedule_not_found" {
			t.Fatalf("%s không thấy lịch: %d %v", c.name, r.status, r.body)
		}
		if r := c.who.get("/v1/extension/work-schedules/" + id + "/log"); r.status != http.StatusNotFound {
			t.Fatalf("%s không thấy Log: %d", c.name, r.status)
		}
	}
	if l := p.admin92.must(p.admin92.get("/v1/extension/work-schedules?week_start="+nextWeek), http.StatusOK, "tỉnh 92"); len(items(l, "items")) != 0 {
		t.Fatal("tỉnh 92 không thấy lịch tỉnh 89")
	}
	if r := p.admin92.review(id, "approve", 1, ""); r.status != http.StatusNotFound {
		t.Fatalf("tỉnh khác duyệt → 404: %d", r.status)
	}
	if l := p.off30002.must(p.off30002.get("/v1/extension/work-schedules?week_start="+nextWeek), http.StatusOK, "xã khác"); len(items(l, "items")) != 0 {
		t.Fatal("cán bộ xã khác chỉ thấy lịch của mình")
	}
	if r := p.off30001a.review(id, "approve", 1, ""); r.status != http.StatusForbidden {
		t.Fatalf("Cấp Xã không duyệt: %d", r.status)
	}
	if r := p.national.get("/v1/extension/work-schedules"); r.status != http.StatusForbidden {
		t.Fatalf("Cấp Quốc gia không có quyền lịch công tác: %d", r.status)
	}
	if r := p.national.saveSched(nextWeek, "draft", nil, sItem("2026-09-21", "morning", "", "", "Thử")); r.status != http.StatusForbidden {
		t.Fatalf("Cấp Quốc gia không đăng ký lịch: %d", r.status)
	}
	// Admin xem mọi lịch (R), không duyệt.
	adm := f.as(uuid.Nil, "platform_admin")
	if g := adm.must(adm.get("/v1/extension/work-schedules/"+id), http.StatusOK, "admin xem"); g.body["can"].(map[string]any)["approve"] != false {
		t.Fatal("Admin chỉ xem")
	}
	if r := adm.review(id, "approve", 1, ""); r.status != http.StatusForbidden {
		t.Fatalf("Admin không duyệt: %d", r.status)
	}

	// Tenant khác cùng cây tổ chức giả: không thấy gì.
	other := f.asTenant(ids.New(), f.org.p89, port.RoleKNProvinceAdmin)
	if r := other.get("/v1/extension/work-schedules/" + id); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác → 404: %d", r.status)
	}
	if l := other.must(other.get("/v1/extension/work-schedules?week_start="+nextWeek), http.StatusOK, "tenant khác"); len(items(l, "items")) != 0 {
		t.Fatal("tenant khác không thấy lịch")
	}
	// Cán bộ xã chỉ chọn địa điểm trong xã phụ trách.
	bad := sItem("2026-09-22", "morning", "", "", "Thăm HTX xã khác")
	bad["commune_code"] = "30002"
	if r := p.off30001a.saveSched("2026-09-28", "draft", nil, bad); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("xã ngoài xã phụ trách → 422: %d %v", r.status, r.body)
	}
}

// FN-XA-03.08 luồng phụ: điều chỉnh lịch đã duyệt → phần điều chỉnh chờ duyệt lại (bản đã duyệt vẫn hiệu lực);
// mỗi lần sửa là phiên bản mới (RB-1); base_version cũ → 409; không lưu nháp đè lên lịch đã gửi.
func TestFN_XA_03_08_AmendmentVersioning(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	off := p.off30002
	d := off.must(off.saveSched(nextWeek, "submit", nil,
		sItem("2026-09-21", "morning", "07:30", "", "Kiểm tra sâu bệnh cánh đồng lớn"),
		sItem("2026-09-25", "afternoon", "", "", "Tổng hợp số liệu tuần")), http.StatusCreated, "gửi")
	id := schedID(d)
	first := items(d, "items")
	p.admin89.must(p.admin89.review(id, "approve", 1, ""), http.StatusOK, "duyệt")

	if r := off.saveSched(nextWeek, "draft", 1); r.status != http.StatusConflict || code(r) != "extension.schedule_state_conflict" {
		t.Fatalf("lưu nháp đè lịch đã duyệt → 409: %d %v", r.status, r.body)
	}
	amend := map[string]any{"id": str(first[0], "id"), "day": "2026-09-21", "session": "morning", "start_time": "08:00",
		"content": "Kiểm tra sâu bệnh cánh đồng lớn"}
	keep := map[string]any{"id": str(first[1], "id"), "day": "2026-09-25", "session": "afternoon", "content": "Tổng hợp số liệu tuần"}
	am := off.must(off.saveSched(nextWeek, "submit", 1, amend, keep), http.StatusCreated, "điều chỉnh")
	sc := am.body["schedule"].(map[string]any)
	if sc["status"] != "pending" || sc["amendment"] != true || num(am.body, "schedule", "approved_version") != 1 || schedVersion(am) != 2 {
		t.Fatalf("điều chỉnh: chờ duyệt lại, bản duyệt v1 giữ: %v", sc)
	}
	its := items(am, "items")
	if its[0]["changed"] != true || its[1]["changed"] != false || len(items(am, "approved_items")) != 2 {
		t.Fatalf("đánh dấu phần điều chỉnh so bản đã duyệt: %v", am.body)
	}
	if ev := f.scheduleOutbox(port.EventWorkScheduleSubmitted, id); len(ev) != 2 || ev[1]["amendment"] != true {
		t.Fatalf("sự kiện gửi điều chỉnh: %v", ev)
	}
	// Máy ngoại tuyến sửa trên bản cũ.
	if r := off.saveSched(nextWeek, "submit", 1, keep); r.status != http.StatusConflict || code(r) != "extension.schedule_version_conflict" {
		t.Fatalf("base_version cũ → 409: %d %v", r.status, r.body)
	}
	// Tạo mới khi đã có lịch (máy thứ hai chưa tải về) → 409 để tải lại, không tạo lịch trùng.
	if r := off.saveSched(nextWeek, "draft", nil, keep); r.status != http.StatusConflict || code(r) != "extension.schedule_version_conflict" {
		t.Fatalf("tạo trùng tuần → 409: %d %v", r.status, r.body)
	}
	lg := off.must(off.get("/v1/extension/work-schedules/"+id+"/log"), http.StatusOK, "log")
	top := items(lg, "items")[0]
	if str(top, "event") != "amended" || !hasChange(top, "Giờ bắt đầu", "07:30", "08:00") {
		t.Fatalf("Log điều chỉnh ghi giờ cũ → mới: %v", top)
	}
	ok := p.admin89.must(p.admin89.review(id, "approve", 2, ""), http.StatusOK, "duyệt điều chỉnh")
	if num(ok.body, "schedule", "approved_version") != 2 || ok.body["schedule"].(map[string]any)["amendment"] != false {
		t.Fatalf("duyệt điều chỉnh: %v", ok.body["schedule"])
	}
	// Bản ghi sổ không sửa được (RB-1).
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE pf_extension.work_schedule_item SET content = 'sửa lén' WHERE schedule_id = $1", uuid.MustParse(id))
		return err
	})
	if err == nil {
		t.Fatal("sổ mục lịch phải chặn UPDATE")
	}
}

// FN-TI-02.07 / FN-XA-03.08: "lịch tuần đã kết thúc bị khóa" — không lưu, không duyệt; job ghi sự việc khóa (Log).
func TestFN_TI_02_07_EndedWeekLocked(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	thisWeek := "2026-09-14"
	d := p.off30001a.must(p.off30001a.saveSched(thisWeek, "submit", nil, sItem("2026-09-16", "morning", "", "", "Thăm đồng tuần này")),
		http.StatusCreated, "gửi tuần hiện tại")
	if d.body["schedule"].(map[string]any)["late"] != true || !strings.Contains(warningCodes(d), "late_registration") {
		t.Fatalf("gửi sau hạn đăng ký → Đăng ký muộn: %v", d.body["schedule"])
	}
	id := schedID(d)
	f.clk.Advance(6 * 24 * time.Hour) // Thứ Hai 21/09 10:00
	if r := p.off30001a.saveSched(thisWeek, "submit", 1, sItem("2026-09-16", "morning", "", "", "Sửa sau khi tuần đã hết")); r.status != http.StatusConflict || code(r) != "extension.week_locked" {
		t.Fatalf("tuần đã kết thúc → 409 week_locked: %d %v", r.status, r.body)
	}
	if r := p.admin89.review(id, "approve", 1, ""); r.status != http.StatusConflict || code(r) != "extension.week_locked" {
		t.Fatalf("duyệt tuần đã kết thúc → 409: %d %v", r.status, r.body)
	}
	g := p.off30001a.must(p.off30001a.get("/v1/extension/work-schedules/"+id), http.StatusOK, "xem")
	if g.body["schedule"].(map[string]any)["locked"] != true || g.body["can"].(map[string]any)["edit"] != false {
		t.Fatalf("lịch bị khóa, chỉ xem: %v", g.body)
	}
	r, err := f.mod.ScheduleJobRun(context.Background(), f.tenant)
	if err != nil || r.Locked != 1 {
		t.Fatalf("job khóa 1 lịch: %+v %v", r, err)
	}
	if r, _ := f.mod.ScheduleJobRun(context.Background(), f.tenant); r.Locked != 0 {
		t.Fatal("job khóa idempotent")
	}
	lg := p.off30001a.must(p.off30001a.get("/v1/extension/work-schedules/"+id+"/log"), http.StatusOK, "log")
	if str(items(lg, "items")[0], "event") != "locked" {
		t.Fatalf("Log có sự việc khóa: %v", lg.body)
	}
}

// FN-TI-02.07 luồng phụ: tab Chưa đăng ký, số đếm, duyệt hàng loạt (bỏ qua lịch của mình), nhắc hạn đăng ký và
// nhắc người duyệt quá hạn (mỗi tuần một lần), xuất Excel/PDF.
func TestFN_TI_02_07_UnregisteredBulkRemindersExport(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	a := p.off30001a.must(p.off30001a.saveSched(nextWeek, "submit", nil, sItem("2026-09-21", "morning", "", "", "Thăm đồng xã 30001")), http.StatusCreated, "a")
	p.off30001b.must(p.off30001b.saveSched(nextWeek, "draft", nil, sItem("2026-09-22", "morning", "", "", "Nháp xã 30001")), http.StatusCreated, "b nháp")
	own := p.admin89.must(p.admin89.saveSched(nextWeek, "submit", nil, sItem("2026-09-23", "morning", "", "", "Lịch của tôi")), http.StatusCreated, "tỉnh")

	un := p.admin89.must(p.admin89.get("/v1/extension/work-schedules/unregistered?week_start="+nextWeek), http.StatusOK, "chưa đăng ký")
	got := map[string]string{}
	for _, u := range items(un, "items") {
		got[str(u, "actor_ref")] = str(u, "status")
	}
	if len(got) != 2 || got[p.off30001b.actor.String()] != "draft" || got[p.off30002.actor.String()] != "none" {
		t.Fatalf("Chưa đăng ký: cán bộ 30001b (nháp) và 30002 (chưa có): %v", un.body)
	}
	if u2 := p.admin89.must(p.admin89.get("/v1/extension/work-schedules/unregistered?week_start="+nextWeek+"&commune_code=30002"), http.StatusOK, "lọc xã"); len(items(u2, "items")) != 1 {
		t.Fatalf("lọc xã: %v", u2.body)
	}
	cnt := p.admin89.must(p.admin89.get("/v1/extension/work-schedules/counts?week_start="+nextWeek), http.StatusOK, "đếm")
	if num(cnt.body, "pending") != 2 || num(cnt.body, "draft") != 1 || num(cnt.body, "unregistered") != 2 || num(cnt.body, "pending_open_weeks") != 1 {
		t.Fatalf("số đếm (chờ duyệt mở = 1, không tính lịch của mình): %v", cnt.body)
	}

	// Xuất Excel; PDF cần bộ dựng của pf/reports.
	x := p.admin89.call(http.MethodGet, "/v1/extension/work-schedules/export?week_start="+nextWeek, "", nil)
	if x.status != http.StatusOK || !strings.Contains(x.hdr.Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("xuất Excel: %d %v", x.status, x.hdr)
	}
	if r := p.admin89.call(http.MethodGet, "/v1/extension/work-schedules/export?week_start="+nextWeek+"&format=pdf", "", nil); r.status != http.StatusServiceUnavailable {
		t.Fatalf("PDF chưa nối bộ dựng → 503: %d", r.status)
	}
	pdf := &fakePDF{}
	f.mod.UseTablePDF(pdf)
	// A5 (03/10/2026): xuất lịch là xuất NỘI BỘ — qua điểm kiểm soát dùng chung (ghi egress_event) nhưng không bị chặn
	// kể cả enforce + checkpoint báo vượt hạn mức hoặc lỗi.
	cp := &denyCP{}
	f.mod.UseEgress(cp, meter.ModeEnforce, nil)
	if r := p.admin89.call(http.MethodGet, "/v1/extension/work-schedules/export?week_start="+nextWeek+"&format=pdf", "", nil); r.status != http.StatusOK || pdf.rows != 3 {
		t.Fatalf("xuất PDF 3 dòng: %d %d", r.status, pdf.rows)
	}
	if x := p.admin89.call(http.MethodGet, "/v1/extension/work-schedules/export?week_start="+nextWeek, "", nil); x.status != http.StatusOK {
		t.Fatalf("A5: xuất Excel ở enforce bị chặn: %d", x.status)
	}
	if len(cp.got) != 2 || !cp.got[0].Internal || cp.got[0].Channel != meter.ChannelReportExport || cp.got[0].Product != "kn_work_schedule" ||
		cp.got[0].Records != 3 || cp.got[0].CounterpartyRef == uuid.Nil {
		t.Fatalf("A5: lần xuất phải qua checkpoint với Internal: %+v", cp.got)
	}
	f.mod.UseEgress(nil, "", nil)

	bulk := p.admin89.must(p.admin89.post("/v1/extension/work-schedules/bulk-approve", map[string]any{"ids": []string{schedID(a), schedID(own), ids.New().String()}}),
		http.StatusOK, "duyệt hàng loạt")
	if joinRefs(bulk.body["approved"]) != schedID(a) || joinField(bulk.body["skipped"], "code") != "self_approval_forbidden,extension.schedule_not_found" {
		t.Fatalf("duyệt hàng loạt bỏ qua lịch của mình và lịch không có: %v", bulk.body)
	}

	// Nhắc hạn đăng ký: Thứ Năm 17/09 18:00 VN (≤ 24 giờ trước 17:00 Thứ Sáu).
	f.clk.Advance(2*24*time.Hour + 8*time.Hour)
	r, err := f.mod.ScheduleJobRun(context.Background(), f.tenant)
	if err != nil || r.Reminders != 2 {
		t.Fatalf("nhắc hạn đăng ký cho tỉnh 89 và 92: %+v %v", r, err)
	}
	ev := province89(f.scheduleOutbox(port.EventWorkScheduleReminded, ""))
	if len(ev) != 1 || ev[0]["kind"] != "submit_due" || !strings.Contains(joinRefs(ev[0]["recipient_refs"]), p.off30002.actor.String()) ||
		strings.Contains(joinRefs(ev[0]["recipient_refs"]), p.off30001a.actor.String()) {
		t.Fatalf("nhắc cán bộ chưa gửi (không nhắc người đã gửi): %v", ev)
	}
	if r, _ := f.mod.ScheduleJobRun(context.Background(), f.tenant); r.Reminders != 0 {
		t.Fatal("không nhắc lặp")
	}
	// Quá hạn duyệt (00:00 Thứ Hai 21/09) mà lịch của cán bộ tỉnh vẫn chờ duyệt → nhắc người duyệt.
	f.clk.Advance(4 * 24 * time.Hour) // Thứ Hai 21/09 18:00
	if r, err := f.mod.ScheduleJobRun(context.Background(), f.tenant); err != nil || r.Reminders != 1 {
		t.Fatalf("nhắc người duyệt quá hạn: %+v %v", r, err)
	}
	ev = province89(f.scheduleOutbox(port.EventWorkScheduleReminded, ""))
	if len(ev) != 2 || ev[1]["kind"] != "review_overdue" || num(ev[1], "pending") != 1 {
		t.Fatalf("nhắc quá hạn duyệt: %v", ev)
	}
}

func province89(ev []map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range ev {
		if e["province_code"] == "89" {
			out = append(out, e)
		}
	}
	return out
}

type fakePDF struct{ rows int }

func (p *fakePDF) RenderTablePDF(_ string, _ [][2]string, _ []string, rows [][]string, _ time.Time) ([]byte, error) {
	p.rows = len(rows)
	return []byte("%PDF-1.4 giả"), nil
}

// P1 cắm vào qua port.KNContextResolver: cấp quản lý quyết định người duyệt (Q-05, tạm "truong_phong").
type fakeKNContext struct {
	byActor map[uuid.UUID]accountsport.KNContext
}

func (f fakeKNContext) KNContext(ctx context.Context) (accountsport.KNContext, bool, error) {
	pr, ok := principalOf(ctx)
	if !ok {
		return accountsport.KNContext{}, false, nil
	}
	c, ok := f.byActor[pr]
	return c, ok, nil
}

func principalOf(ctx context.Context) (uuid.UUID, bool) {
	p, ok := authz.FromContext(ctx)
	return p.ActorRef, ok
}

func TestFN_TI_02_07_ApproverMgmtLevelViaP1Context(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	head := f.as(f.org.p89, port.RoleKNProvinceAdmin)
	spec := f.as(f.org.p89, port.RoleKNProvinceAdmin)
	f.mod.UseKNContext(fakeKNContext{byActor: map[uuid.UUID]accountsport.KNContext{
		p.off30001a.actor: {Role: port.KNRoleXa, Department: "trong_trot", ProvinceCode: "89", Communes: []string{"30001", "30002"}},
		head.actor:        {Role: port.KNRoleTinh, Department: "trong_trot", ProvinceCode: "89", MgmtLevel: "Truong_Phong"},
		spec.actor:        {Role: port.KNRoleTinh, Department: "trong_trot", ProvinceCode: "89", MgmtLevel: port.MgmtLevelSpecialist},
	}}, nil)
	it := sItem("2026-09-22", "afternoon", "", "", "Thăm đồng xã 30002")
	it["commune_code"] = "30002" // P1: Cấp Xã phụ trách 2 xã
	d := p.off30001a.must(p.off30001a.saveSched(nextWeek, "submit", nil, it), http.StatusCreated, "gửi")
	if joinRefs(d.body["schedule"].(map[string]any)["commune_codes"]) != "30001,30002" {
		t.Fatalf("xã phụ trách lấy từ ngữ cảnh P1: %v", d.body["schedule"])
	}
	if r := spec.review(schedID(d), "approve", 1, ""); r.status != http.StatusForbidden || code(r) != "extension.not_schedule_approver" {
		t.Fatalf("Chuyên viên không duyệt: %d %v", r.status, r.body)
	}
	f.params.setText(port.ParamScheduleApproverLevels, "truong_phong, chuyen_vien")
	spec.must(spec.review(schedID(d), "change", 1, "Bổ sung thành phần phối hợp"), http.StatusOK, "Chuyên viên được ủy quyền (cấu hình)")
	// Văn bản tự do không chứa SĐT (RB-3).
	bad := sItem("2026-09-22", "afternoon", "", "", "Thăm đồng")
	bad["participants"] = "Anh Ba 0912345678"
	if r := p.off30001a.saveSched(nextWeek, "submit", 2, bad); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("SĐT trong thành phần phối hợp → 422: %d %v", r.status, r.body)
	}
	if r := head.review(schedID(d), "approve", 1, ""); r.status != http.StatusConflict {
		t.Fatalf("đã yêu cầu chỉnh sửa, không còn chờ duyệt: %d", r.status)
	}
}

// FN-XA-03.08 luồng phụ: sao chép tuần trước (ngày +7, ưu tiên bản đã duyệt); kiểm tuần, ngày, buổi.
func TestFN_XA_03_08_CopyPreviousAndValidation(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	off := p.off30002
	if r := off.post("/v1/extension/work-schedules/copy-previous", map[string]any{"week_start": "2026-09-28"}); r.status != http.StatusNotFound || code(r) != "extension.schedule_no_previous" {
		t.Fatalf("chưa có tuần trước → 404: %d %v", r.status, r.body)
	}
	off.must(off.saveSched(nextWeek, "submit", nil, sItem("2026-09-21", "morning", "07:00", "09:00", "Kiểm tra mực nước kênh"),
		sItem("2026-09-26", "afternoon", "", "", "Họp tổ khuyến nông")), http.StatusCreated, "tuần 21/09")
	m := off.must(off.get("/v1/extension/work-schedules/mine?week_start=2026-09-28"), http.StatusOK, "tuần 28/09")
	if m.body["previous_available"] != true || m.body["schedule"] != nil {
		t.Fatalf("có tuần trước để sao chép: %v", m.body)
	}
	cp := off.must(off.post("/v1/extension/work-schedules/copy-previous", map[string]any{"week_start": "2026-09-28"}), http.StatusCreated, "sao chép")
	its := items(cp, "items")
	if schedStatus(cp) != "draft" || len(its) != 2 || str(its[0], "day") != "2026-09-28" || str(its[1], "day") != "2026-10-03" || str(its[0], "start_time") != "07:00" {
		t.Fatalf("nháp sao chép, ngày +7: %v", cp.body)
	}
	for _, c := range []struct {
		name string
		body map[string]any
	}{
		{"không phải Thứ Hai", map[string]any{"week_start": "2026-09-22", "action": "draft", "items": []any{}}},
		{"ngày ngoài tuần", map[string]any{"week_start": "2026-10-05", "action": "draft", "items": off.fill(sItem("2026-10-12", "morning", "", "", "Ngoài tuần"))}},
		{"buổi sai", map[string]any{"week_start": "2026-10-05", "action": "draft", "items": off.fill(sItem("2026-10-05", "evening", "", "", "Buổi tối"))}},
		{"giờ kết thúc trước giờ bắt đầu", map[string]any{"week_start": "2026-10-05", "action": "draft", "items": off.fill(sItem("2026-10-05", "morning", "10:00", "09:00", "Giờ sai"))}},
		{"gửi lịch rỗng", map[string]any{"week_start": "2026-10-05", "action": "submit", "items": []any{}}},
		{"quá 8 tuần", map[string]any{"week_start": "2027-01-04", "action": "draft", "items": []any{}}},
	} {
		if r := off.post("/v1/extension/work-schedules", c.body); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("%s → 422, nhận %d %v", c.name, r.status, r.body)
		}
	}
	// id do máy sinh (ngoại tuyến) được dùng làm mã lịch; dùng lại cho tuần khác → 409.
	cid := ids.New().String()
	r := off.must(off.post("/v1/extension/work-schedules", map[string]any{"id": cid, "week_start": "2026-10-05", "action": "draft",
		"items": off.fill(sItem("2026-10-05", "morning", "", "", "Lịch ngoại tuyến"))}), http.StatusCreated, "id máy sinh")
	if schedID(r) != cid {
		t.Fatalf("giữ id máy sinh: %v", schedID(r))
	}
	if r := off.post("/v1/extension/work-schedules", map[string]any{"id": cid, "week_start": "2026-10-12", "action": "draft", "items": []any{}}); r.status != http.StatusConflict || code(r) != "extension.id_reused" {
		t.Fatalf("id dùng lại → 409: %d %v", r.status, r.body)
	}
}

// denyCP là điểm kiểm soát xuất giả luôn báo vượt hạn mức (A5: xuất nội bộ vẫn phải đi qua và không bị chặn).
type denyCP struct {
	mu  sync.Mutex
	got []meter.Egress
}

func (d *denyCP) Pass(_ context.Context, e meter.Egress) (meter.Decision, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got = append(d.got, e)
	return meter.Decision{Allowed: false, Reason: "quota_exceeded", Mode: meter.ModeEnforce}, nil
}
