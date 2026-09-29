package handler_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

const staffSupportPath = "/v1/extension/support-requests"

func supportIDs(r resp) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, it := range items(r, "items") {
		out[it["id"].(string)] = it
	}
	return out
}

// HTX US-SUPPORT-01 (quản lý HTX biết yêu cầu để hỗ trợ kịp thời) và KN US-TASK-01 /
// US-TASK-03 (cán bộ khuyến nông giám sát yêu cầu và nhiệm vụ tự sinh): danh sách cán
// bộ GET /v1/extension/support-requests — ảnh (mã tệp), trạng thái GPS, nhiệm vụ kèm
// trạng thái, người được giao, ghi chú cuối; cách ly HTX, phạm vi xã/tỉnh, tenant.
func TestStaffSupportListScopeAndFields(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	f.mod.UseFarmers(farmers)
	p.off30001a.must(p.off30001a.post("/v1/extension/roster/status", map[string]any{"status": "on_duty"}), http.StatusCreated, "trực")

	farmer1, hh1 := f.farmerIn(farmers, f.tenant, f.org.c1, "30001")
	farmer2, _ := f.farmerIn(farmers, f.tenant, f.org.c2, "30002")
	farmer3, _ := f.farmerIn(farmers, f.tenant, f.org.c3, "31001")
	submit := func(c caller, body map[string]any) string {
		t.Helper()
		return str(c.must(c.post("/v1/support-requests", body), http.StatusCreated, "gửi yêu cầu").body, "support_request", "id")
	}
	photos := []string{ids.New().String(), ids.New().String()}
	pest := submit(farmer1, map[string]any{"category": "dich_hai", "urgency": "khan", "description": "Rầy nâu mật độ cao ở lô gần kênh",
		"photo_refs": photos, "location": map[string]any{"lat": "10.3861", "lon": "105.4352"}, "gps_accuracy_m": 8})
	rain := submit(farmer1, map[string]any{"category": "thoi_tiet", "description": "Mưa lớn kéo dài, ruộng ngập sâu"})
	c2req := submit(farmer2, map[string]any{"category": "ky_thuat", "description": "Hỏi lịch rút nước lần hai"})
	c3req := submit(farmer3, map[string]any{"category": "may_moc", "description": "Cần đặt lịch máy gặt cho ba lô"})

	// Quản lý HTX c1: chỉ yêu cầu của HTX mình, mới nhất trước, đủ trường.
	lst := p.coop1.must(p.coop1.get(staffSupportPath), http.StatusOK, "quản lý HTX c1")
	got := items(lst, "items")
	if len(got) != 2 || got[0]["id"] != rain || got[1]["id"] != pest {
		t.Fatalf("HTX c1: %v", lst.body)
	}
	it := got[1]
	if it["household_ref"] != hh1.String() || it["category"] != "dich_hai" || it["urgency"] != "khan" || num(it, "photo_count") != 2 ||
		it["gps_missing"] != false || len(it["labels"].([]any)) != 0 || it["gps_accuracy_m"] == nil || it["cooperative_ref"] != f.org.c1.String() ||
		it["assignment_rule"] != port.AssignCommuneOnDuty || it["event_time"] == "" || it["received_at"] == "" {
		t.Fatalf("trường yêu cầu: %v", it)
	}
	if ph := it["photo_refs"].([]any); len(ph) != 2 || ph[0] != photos[0] {
		t.Fatalf("mã ảnh: %v", ph)
	}
	if _, leaked := it["location"]; leaked {
		t.Fatal("không trả toạ độ trong danh sách")
	}
	if str(it, "task", "status") != "assigned" || str(it, "task", "assignee_ref") != p.off30001a.actor.String() ||
		str(it, "task", "assignee_name") != "Cán bộ 30001" || str(it, "task", "source") != "htx_app" || str(it, "task", "priority") != "urgent" {
		t.Fatalf("nhiệm vụ kèm: %v", it["task"])
	}
	if r := got[0]; r["gps_missing"] != true || len(r["labels"].([]any)) != 1 || r["labels"].([]any)[0] != "Thiếu vị trí GPS" {
		t.Fatalf("nhãn thiếu GPS: %v", r)
	}
	// Cách ly HTX: quản lý HTX c1 hỏi HTX c2 → 403; quản lý HTX c2 chỉ thấy của c2.
	if r := p.coop1.get(staffSupportPath + "?cooperative_ref=" + f.org.c2.String()); r.status != http.StatusForbidden || code(r) != "extension.out_of_scope" {
		t.Fatalf("HTX khác: %d %v", r.status, r.body)
	}
	coop2 := f.as(f.org.c2, port.RoleCoopManager)
	if m := supportIDs(coop2.must(coop2.get(staffSupportPath), http.StatusOK, "HTX c2")); len(m) != 1 || m[c2req] == nil {
		t.Fatalf("HTX c2: %v", m)
	}
	noOrg := f.as(uuid.Nil, port.RoleCoopManager)
	if r := noOrg.must(noOrg.get(staffSupportPath), http.StatusOK, "chưa gắn HTX"); len(items(r, "items")) != 0 {
		t.Fatalf("quản lý chưa gắn HTX: %v", r.body)
	}

	// Phạm vi khuyến nông: xã → tỉnh → quốc gia.
	for _, tc := range []struct {
		name string
		c    caller
		want []string
	}{
		{"cán bộ xã 30001", p.off30001a, []string{pest, rain}},
		{"cán bộ xã 30002", p.off30002, []string{c2req}},
		{"TTKN tỉnh 89", p.admin89, []string{pest, rain, c2req}},
		{"TTKN tỉnh 92", p.admin92, []string{c3req}},
		{"TTKNQG", p.national, []string{pest, rain, c2req, c3req}},
	} {
		m := supportIDs(tc.c.must(tc.c.get(staffSupportPath), http.StatusOK, tc.name))
		if len(m) != len(tc.want) {
			t.Fatalf("%s: %d yêu cầu, muốn %d", tc.name, len(m), len(tc.want))
		}
		for _, w := range tc.want {
			if m[w] == nil {
				t.Fatalf("%s: thiếu %s", tc.name, w)
			}
		}
	}

	// Nông dân và vai không đọc nhiệm vụ không dùng được danh sách cán bộ.
	if r := farmer1.get(staffSupportPath); r.status != http.StatusForbidden {
		t.Fatalf("nông dân: %d", r.status)
	}

	// Cách ly tenant: yêu cầu tenant khác không lộ; TTKNQG tenant khác chỉ thấy của mình.
	other := ids.New()
	farmerB, _ := f.farmerIn(farmers, other, f.org.c1, "30001")
	otherReq := submit(farmerB, map[string]any{"category": "khac", "description": "Yêu cầu của tenant khác"})
	if m := supportIDs(p.national.must(p.national.get(staffSupportPath), http.StatusOK, "TTKNQG sau tenant khác")); len(m) != 4 || m[otherReq] != nil {
		t.Fatalf("lộ tenant khác: %v", m)
	}
	natB := f.asTenant(other, f.org.national, port.RoleKNNationalAdmin)
	if m := supportIDs(natB.must(natB.get(staffSupportPath), http.StatusOK, "TTKNQG tenant B")); len(m) != 1 || m[otherReq] == nil {
		t.Fatalf("tenant B: %v", m)
	}
}

// KN US-TASK-03: lọc theo trạng thái nhiệm vụ, loại sự cố, mức khẩn, xã, HTX, GPS;
// phân trang con trỏ; ghi chú cuối của cán bộ đi kèm (HTX US-SUPPORT-01).
func TestStaffSupportListFiltersAndCursor(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	f.mod.UseFarmers(farmers)
	p.off30001a.must(p.off30001a.post("/v1/extension/roster/status", map[string]any{"status": "on_duty"}), http.StatusCreated, "trực")
	farmer1, _ := f.farmerIn(farmers, f.tenant, f.org.c1, "30001")
	farmer2, _ := f.farmerIn(farmers, f.tenant, f.org.c2, "30002")
	submit := func(c caller, body map[string]any) string {
		t.Helper()
		return str(c.must(c.post("/v1/support-requests", body), http.StatusCreated, "gửi yêu cầu").body, "support_request", "id")
	}
	pest := submit(farmer1, map[string]any{"category": "dich_hai", "urgency": "khan", "description": "Rầy nâu mật độ cao ở lô gần kênh",
		"location": map[string]any{"lat": "10.3861", "lon": "105.4352"}})
	rain := submit(farmer1, map[string]any{"category": "thoi_tiet", "description": "Mưa lớn kéo dài, ruộng ngập sâu"})
	tech := submit(farmer2, map[string]any{"category": "ky_thuat", "description": "Hỏi lịch rút nước lần hai"})

	// Cán bộ tiếp nhận yêu cầu dịch hại và ghi tiến độ.
	lst := p.admin89.must(p.admin89.get(staffSupportPath), http.StatusOK, "tỉnh 89")
	pestTask := str(supportIDs(lst)[pest], "task", "task_id")
	p.off30001a.must(p.off30001a.post("/v1/extension/tasks/"+pestTask+"/status", map[string]any{"action": "accepted"}), http.StatusOK, "tiếp nhận")
	p.off30001a.must(p.off30001a.post("/v1/extension/tasks/"+pestTask+"/status", map[string]any{"action": "progress", "note": "Đã hẹn xuống ruộng sáng mai"}), http.StatusOK, "tiến độ")

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"task_status=in_progress", []string{pest}},
		{"task_status=assigned", []string{rain, tech}},
		{"category=ky_thuat", []string{tech}},
		{"urgency=khan", []string{pest}},
		{"commune_code=30002", []string{tech}},
		{"cooperative_ref=" + f.org.c1.String(), []string{pest, rain}},
		{"gps_missing=true", []string{rain, tech}},
		{"gps_missing=false", []string{pest}},
		{"category=thoi_tiet&gps_missing=true", []string{rain}},
	} {
		m := supportIDs(p.admin89.must(p.admin89.get(staffSupportPath+"?"+tc.query), http.StatusOK, tc.query))
		if len(m) != len(tc.want) {
			t.Fatalf("%s: %v", tc.query, m)
		}
		for _, w := range tc.want {
			if m[w] == nil {
				t.Fatalf("%s: thiếu %s", tc.query, w)
			}
		}
	}
	// Ghi chú cuối và trạng thái hiện hành đi kèm.
	it := supportIDs(p.coop1.must(p.coop1.get(staffSupportPath+"?task_status=in_progress"), http.StatusOK, "HTX lọc"))[pest]
	if str(it, "task", "last_note") != "Đã hẹn xuống ruộng sáng mai" || str(it, "task", "status") != "in_progress" {
		t.Fatalf("ghi chú cuối: %v", it)
	}
	// Bộ lọc sai → 422.
	for _, q := range []string{"task_status=done", "category=abc", "urgency=gap", "gps_missing=maybe", "cooperative_ref=x", "cursor=@@@"} {
		if r := p.admin89.get(staffSupportPath + "?" + q); r.status != http.StatusUnprocessableEntity && r.status != http.StatusBadRequest {
			t.Fatalf("%s: %d", q, r.status)
		}
	}

	// Phân trang con trỏ: 2 + 1, không trùng, không sót.
	first := p.admin89.must(p.admin89.get(staffSupportPath+"?limit=2"), http.StatusOK, "trang 1")
	cur, _ := first.body["next_cursor"].(string)
	if len(items(first, "items")) != 2 || cur == "" {
		t.Fatalf("trang 1: %v", first.body)
	}
	second := p.admin89.must(p.admin89.get(staffSupportPath+"?limit=2&cursor="+url.QueryEscape(cur)), http.StatusOK, "trang 2")
	if len(items(second, "items")) != 1 || second.body["next_cursor"] != nil {
		t.Fatalf("trang 2: %v", second.body)
	}
	seen := supportIDs(first)
	for id := range supportIDs(second) {
		if seen[id] != nil {
			t.Fatalf("trùng %s", id)
		}
		seen[id] = map[string]any{}
	}
	if len(seen) != 3 {
		t.Fatalf("sót: %v", seen)
	}
}
