package departments_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func sampleMenu() partyport.MenuView {
	d := partyport.Department{Code: "trong_trot", Name: "Phòng Trồng trọt", Status: partyport.DeptActive, MenuProfile: partyport.MenuProfileTrongTrot}
	return partyport.MenuView{Version: "kn.menu/v6", Audience: partyport.AudienceXa, UnitRef: unit, Department: &d, DefaultRoute: "/kn/nhiem-vu/cong-viec",
		Groups: []partyport.MenuGroupView{{Key: "dieu_hanh", Label: "Điều hành", Items: []partyport.MenuItemView{
			{Key: "xa.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Enabled: true, Children: []partyport.MenuItemView{
				{Key: "xa.nhiem_vu.cong_viec", Label: "Công việc cần làm", Route: "/kn/nhiem-vu/cong-viec", Icon: "list",
					BadgeSource: "extension.report_tasks.todo", Enabled: true},
			}},
			{Key: "xa.ban_do", Label: "Bản đồ", Route: "/kn/ban-do", Icon: "map", Status: "coming_soon", StatusLabel: "Sắp có"},
		}}}}
}

// Thân KNMenu giữ nguyên byte-cho-byte như khi binding còn ở pf/party (khóa gốc theo thứ tự chữ cái của map cũ, mục theo
// thứ tự trường, status/status_label bỏ khi rỗng); Cache-Control: no-store; "role" là tên cũ của "audience".
func TestMenuBinding(t *testing.T) {
	f := &fakeAdmin{menu: sampleMenu()}
	app := mount(t, f, false)
	want := `{"audience":"kn_xa","default_route":"/kn/nhiem-vu/cong-viec",` +
		`"department":{"code":"trong_trot","name":"Phòng Trồng trọt","status":"active","menu_profile":"trong_trot"},` +
		`"groups":[{"key":"dieu_hanh","label":"Điều hành","items":[` +
		`{"key":"xa.nhiem_vu","label":"Nhiệm vụ","route":null,"icon":"task","badge_source":null,"enabled":true,"children":[` +
		`{"key":"xa.nhiem_vu.cong_viec","label":"Công việc cần làm","route":"/kn/nhiem-vu/cong-viec","icon":"list",` +
		`"badge_source":"extension.report_tasks.todo","enabled":true,"children":[]}]},` +
		`{"key":"xa.ban_do","label":"Bản đồ","route":"/kn/ban-do","icon":"map","badge_source":null,"enabled":false,` +
		`"status":"coming_soon","status_label":"Sắp có","children":[]}]}],` +
		`"menu_version":"kn.menu/v6","org_ref":"` + unit.String() + `"}`

	r := app.do(t, http.MethodGet, "/v1/menu?audience=kn_xa&department=trong_trot&org_ref="+unit.String(), "")
	if r.status != http.StatusOK || r.raw != want || r.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("menu: %d %v\n got %s\nwant %s", r.status, r.hdr, r.raw, want)
	}
	if f.query != (partyport.MenuQuery{Audience: partyport.AudienceXa, Department: "trong_trot", UnitRef: unit}) {
		t.Fatalf("menu query: %+v", f.query)
	}
	// ?role= (tên cũ, hợp đồng tạm P6) vẫn nhận; audience thắng khi có cả hai.
	app.do(t, http.MethodGet, "/v1/menu?role=kn_tinh", "")
	if f.query.Audience != partyport.AudienceTinh {
		t.Fatalf("?role=: %+v", f.query)
	}
	app.do(t, http.MethodGet, "/v1/menu?role=kn_tinh&audience=admin", "")
	if f.query.Audience != partyport.AudienceAdmin {
		t.Fatalf("audience thắng role: %+v", f.query)
	}
	r = app.do(t, http.MethodGet, "/v1/departments/thong_tin_truyen_thong/modules?role=admin&org_ref="+unit.String(), "")
	if r.status != http.StatusOK || r.raw != want || f.code != "thong_tin_truyen_thong" ||
		f.query != (partyport.MenuQuery{Audience: partyport.AudienceAdmin, UnitRef: unit}) || r.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("modules: %d %s %q %+v", r.status, r.raw, f.code, f.query)
	}
	// Đơn vị rỗng → org_ref null.
	f.menu.UnitRef = uuid.Nil
	if r := app.do(t, http.MethodGet, "/v1/menu", ""); r.body["org_ref"] != nil || r.raw[len(r.raw)-len(`"org_ref":null}`):] != `"org_ref":null}` {
		t.Fatalf("org_ref rỗng: %s", r.raw)
	}
	// org_ref sai → 422, không gọi use case.
	n := len(f.calls)
	for _, p := range []string{"/v1/menu?org_ref=khong-phai-uuid", "/v1/departments/trong_trot/modules?org_ref=1"} {
		if r := app.do(t, http.MethodGet, p, ""); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d %s", p, r.status, r.raw)
		}
	}
	if len(f.calls) != n {
		t.Fatalf("org_ref sai vẫn gọi use case: %v", f.calls[n:])
	}
	// Lỗi luật của pf/party chuyển nguyên mã HTTP + mã lỗi, không ghi no-store.
	f.err = problem.New(http.StatusForbidden, "party.menu_audience_forbidden", "x")
	if r := app.do(t, http.MethodGet, "/v1/menu?audience=kn_tinh", ""); r.status != http.StatusForbidden ||
		r.body["code"] != "party.menu_audience_forbidden" || r.hdr.Get("Cache-Control") == "no-store" {
		t.Fatalf("lỗi menu: %d %v %s", r.status, r.hdr, r.raw)
	}
	f.err = problem.New(http.StatusConflict, "party.department_not_open", "x")
	if r := app.do(t, http.MethodGet, "/v1/departments/chan_nuoi_thuy_san/modules", ""); r.status != http.StatusConflict ||
		r.body["code"] != "party.department_not_open" {
		t.Fatalf("lỗi modules: %d %s", r.status, r.raw)
	}
}

// POST /v1/orgs/{ref}/departments/seed: thân JSON như trước khi chuyển; ref không phải UUID → 404 party.org_not_found.
func TestSeedBinding(t *testing.T) {
	f := &fakeAdmin{}
	f.seed = partyport.DepartmentSeedResult{Created: []partyport.Department{f.dept()}, Existing: 4, Status: "draft",
		LegacyRetired: []string{"phong_ban_6"}, LegacyKept: []string{}}
	adm := mount(t, f, true)
	r := adm.do(t, http.MethodPost, "/v1/orgs/"+unit.String()+"/departments/seed", "")
	want := `{"created":[{"id":"` + dept.String() + `","org_ref":"` + unit.String() + `","code":"trong_trot","name":"Phòng Trồng trọt",` +
		`"description":null,"sort_order":1,"status":"active","status_label":"Đang áp dụng","menu_profile":"trong_trot","retired":false,` +
		`"selectable":true,"account_count":null,"record_count":null,"updated_by":"` + actor.String() + `","updated_at":"2026-11-03T08:00:00Z"}],` +
		`"existing":4,"legacy_kept":[],"legacy_retired":["phong_ban_6"],"status":"draft"}`
	if r.status != http.StatusOK || r.raw != want || f.unit != unit {
		t.Fatalf("seed: %d\n got %s\nwant %s", r.status, r.raw, want)
	}
	n := len(f.calls)
	if r := adm.do(t, http.MethodPost, "/v1/orgs/abc/departments/seed", ""); r.status != http.StatusNotFound || r.body["code"] != "party.org_not_found" {
		t.Fatalf("ref sai: %d %s", r.status, r.raw)
	}
	if len(f.calls) != n {
		t.Fatalf("ref sai vẫn gọi use case: %v", f.calls[n:])
	}
	f.err = problem.IdempotencyRequired
	if r := adm.do(t, http.MethodPost, "/v1/orgs/"+unit.String()+"/departments/seed", ""); r.status != problem.IdempotencyRequired.Status ||
		r.body["code"] != problem.IdempotencyRequired.Code {
		t.Fatalf("lỗi seed: %d %s", r.status, r.raw)
	}
}
