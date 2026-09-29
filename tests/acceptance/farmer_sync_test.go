package acceptance

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// farmerAccount tạo tài khoản nông dân cho nông hộ hh (quản lý HTX, HTX US-ACC-01) và
// trả người dùng mang token của chính tài khoản đó (sub = user_ref, vai farmer, KHÔNG org_ref).
func (w *world) farmerAccount(mgr *harness.User, hh, phone string) *harness.User {
	w.t.Helper()
	r := w.want(mgr.Do(harness.App, http.MethodPost, "/v1/farmer-accounts", "", map[string]any{
		"household_ref": hh, "phone": phone, "full_name": "Nông dân " + phone,
	}), 201, "tạo tài khoản nông dân "+phone)
	sub := uuid.MustParse(str(obj(r.Body, "farmer"), "user_ref"))
	return w.s.User(harness.UserSpec{Tenant: w.tid, Subject: sub, Roles: []string{"farmer"}})
}

// ownPlot vẽ thửa ở Ninh Kiều rồi gán cho HTX coop và nông hộ hh; trả plot_ref.
func (w *world) ownPlot(coop, hh string, east float64) string {
	w.t.Helper()
	ref := str(props(w.want(w.plot(w.officer, coop, east, 0, 80, 80), 201, "vẽ thửa")), "ref")
	w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/plots/"+ref+"/assign", "", map[string]any{"cooperative_ref": coop, "household_ref": hh}), 200, "gán thửa cho nông hộ")
	return ref
}

type syncChange struct {
	Entity string          `json:"entity"`
	ID     string          `json:"id"`
	Op     string          `json:"op"`
	Data   json.RawMessage `json:"data"`
}

// pullAll đăng ký thiết bị (nếu chưa) rồi pull tới hết trang; trả mọi thay đổi.
func pullAll(t *testing.T, u *harness.User) []syncChange {
	t.Helper()
	if u.DeviceID == "" {
		dev := uuid.Must(uuid.NewV7()).String()
		want(t, u.Do(harness.App, http.MethodPost, "/v1/devices", "", map[string]any{"device_id": dev, "platform": "android", "app_version": "1.0.0+1"}), 201, "đăng ký thiết bị")
		u.DeviceID = dev
	}
	var out []syncChange
	cursor := ""
	for range 50 {
		q := url.Values{"limit": {"1000"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		r := want(t, u.Do(harness.App, http.MethodGet, "/v1/sync/pull?"+q.Encode(), "", nil), 200, "pull")
		var pg struct {
			Changes    []syncChange `json:"changes"`
			NextCursor string       `json:"next_cursor"`
			HasMore    bool         `json:"has_more"`
		}
		if err := json.Unmarshal(r.Raw, &pg); err != nil {
			t.Fatal(err)
		}
		out = append(out, pg.Changes...)
		cursor = pg.NextCursor
		if !pg.HasMore {
			return out
		}
	}
	t.Fatal("pull không dừng")
	return nil
}

// byEntity gom id các bản upsert theo entity.
func byEntity(chs []syncChange) map[string]map[string]json.RawMessage {
	out := map[string]map[string]json.RawMessage{}
	for _, c := range chs {
		if c.Op != "upsert" {
			continue
		}
		if out[c.Entity] == nil {
			out[c.Entity] = map[string]json.RawMessage{}
		}
		out[c.Entity][c.ID] = c.Data
	}
	return out
}

func field(t *testing.T, raw json.RawMessage, k string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	s, _ := m[k].(string)
	return s
}

// App HTX trên máy chủ thật (HTX US-SEASON-01, US-LOG-01, US-SUPPORT-01, R-40): thiết bị
// của nông dân (token không có org_ref) pull ĐÚNG thửa/vụ/nhật ký/yêu cầu hỗ trợ của nông
// hộ mình, danh mục và SOP công khai, khóa công khai của chính hộ mình — không thấy dữ
// liệu của nông hộ khác cùng HTX/xã, không nhận entity của cán bộ (task, directive,
// mrv_catalog), không nhận khóa hộ khác; token cùng sub ở tenant khác không thấy gì của
// tenant này.
func TestFarmerDevicePullsOwnHouseholdOnly(t *testing.T) {
	w := newWorld(t)
	mgr := w.user(w.coopNK, false, "coop_manager", "vault_registrar")
	hh1 := w.household(w.officer, w.coopNK, "Lê Văn Một", "0999000701")
	hh2 := w.household(w.officer, w.coopNK, "Lê Văn Hai", "0999000702") // cùng HTX, cùng xã
	p1 := w.ownPlot(w.coopNK, hh1, 0)
	p2 := w.ownPlot(w.coopNK, hh2, 300)

	// SOP hệ thống đã phát hành, gắn giống OM5451 (Admin API, MFA; phát hành bởi người khác người soạn).
	sopAdmin := w.user("", true, "sop_admin")
	approver := w.user("", true, "param_approver")
	steps := []map[string]any{
		{"seq": 1, "name": "Gieo sạ", "activity_code": "gieo_sa", "day_offset": 0, "window_days": 1, "mandatory": true},
		{"seq": 2, "name": "Bón thúc", "activity_code": "bon_phan", "day_offset": 10, "window_days": 3, "mandatory": true},
		{"seq": 3, "name": "Thu hoạch", "activity_code": "thu_hoach", "day_offset": 95, "window_days": 5, "mandatory": true},
	}
	sop := w.want(sopAdmin.Do(harness.Admin, http.MethodPost, "/v1/cropcycle/sops", "", map[string]any{"code": "SOP-OM5451-A", "name": "SOP lúa OM5451", "steps": steps}), 201, "tạo SOP")
	version := str(list(sop.Body, "versions")[0], "id")
	w.want(approver.Do(harness.Admin, http.MethodPost, "/v1/cropcycle/sop-versions/"+version+"/publish", "", nil), 200, "phát hành SOP")
	w.want(sopAdmin.Do(harness.Admin, http.MethodPost, "/v1/cropcycle/variety-bindings", "", map[string]any{"variety_code": "OM5451", "template_ref": str(sop.Body, "ref")}), 200, "gắn giống")

	f1 := w.farmerAccount(mgr, hh1, "0999000701")
	f2 := w.farmerAccount(mgr, hh2, "0999000702")
	gps := map[string]any{"lon": "105.77", "lat": "10.03", "accuracy_m": "5"}
	today := time.Now().In(clock.Zone()).Format(time.DateOnly)
	open := func(u *harness.User, plot string) string {
		r := w.want(u.Do(harness.App, http.MethodPost, "/v1/cropcycle/seasons", "", map[string]any{"plot_ref": plot, "season_code": "DX", "season_year": 2026,
			"variety_code": "OM5451", "sowing_date": today, "gps": gps}), 201, "nông dân mở vụ")
		return str(obj(r.Body, "season"), "season_ref")
	}
	logOn := func(u *harness.User, plot string) string {
		r := w.want(u.Do(harness.App, http.MethodPost, "/v1/cropcycle/farm-logs", "", map[string]any{"plot_ref": plot, "activity_code": "bon_phan", "sop_step_seq": 2,
			"gps": gps, "note": "Bón thúc đợt 1"}), 201, "nông dân ghi nhật ký")
		return str(obj(r.Body, "log"), "id")
	}
	support := func(u *harness.User) string {
		r := w.want(u.Do(harness.App, http.MethodPost, "/v1/support-requests", "", map[string]any{"category": "dich_hai", "description": "Rầy nâu xuất hiện dày ở thửa, lúa đang đẻ nhánh",
			"photo_refs": []string{}, "location": map[string]any{"lat": "10.03", "lon": "105.77"}}), 201, "gửi yêu cầu hỗ trợ")
		return str(obj(r.Body, "support_request"), "id")
	}
	s1, s2 := open(f1, p1), open(f2, p2)
	l1, l2 := logOn(f1, p1), logOn(f2, p2)
	r1, r2 := support(f1), support(f2)
	if s1 == "" || l1 == "" || r1 == "" {
		t.Fatalf("thiếu mã trả về: vụ %q nhật ký %q yêu cầu %q", s1, l1, r1)
	}
	w.task(w.officer, "Nhiệm vụ cán bộ không được xuống máy nông dân")

	got := byEntity(pullAll(t, f1))
	exact := func(entity string, wantIDs ...string) {
		t.Helper()
		if len(got[entity]) != len(wantIDs) {
			t.Fatalf("%s: muốn %v, nhận %d bản", entity, wantIDs, len(got[entity]))
		}
		for _, id := range wantIDs {
			if _, ok := got[entity][id]; !ok {
				t.Fatalf("%s: thiếu %s (có %v)", entity, id, got[entity])
			}
		}
	}
	exact("plot", p1)
	exact("season", s1)
	exact("farm_log", l1)
	if field(t, got["farm_log"][l1], "plot_ref") != p1 {
		t.Fatalf("nhật ký của thửa khác: %s", got["farm_log"][l1])
	}
	exact("support_request", r1)
	exact("pubkey", hh1)
	exact("sop_version", version)
	if len(got["catalog_item"]) == 0 {
		t.Fatal("danh mục công khai phải xuống máy nông dân")
	}
	for _, e := range []string{"task", "directive", "mrv_catalog"} {
		if len(got[e]) != 0 {
			t.Fatalf("entity của cán bộ %s xuống máy nông dân: %d", e, len(got[e]))
		}
	}
	// Nông dân 2 thấy dữ liệu của mình, không thấy của nông dân 1.
	got2 := byEntity(pullAll(t, f2))
	if _, ok := got2["plot"][p1]; ok || len(got2["plot"]) != 1 || len(got2["season"]) != 1 || got2["support_request"][r2] == nil || got2["support_request"][r1] != nil {
		t.Fatalf("cách ly nông hộ: plot %v season %v support %v", got2["plot"], got2["season"], got2["support_request"])
	}
	if _, ok := got2["pubkey"][hh1]; ok || got2["pubkey"][hh2] == nil {
		t.Fatalf("khóa công khai chỉ của hộ mình: %v", got2["pubkey"])
	}
	if _, ok := got2["farm_log"][l1]; ok || got2["farm_log"][l2] == nil || got2["season"][s2] == nil {
		t.Fatalf("nhật ký/vụ chỉ của hộ mình: %v %v", got2["farm_log"], got2["season"])
	}

	// Cùng sub, tenant khác: không liên kết nông hộ nào ở tenant đó → không thấy gì của tenant này.
	stranger := w.s.User(harness.UserSpec{Tenant: harness.NewTenant(), Subject: f1.Actor, Roles: []string{"farmer"}})
	for e, m := range byEntity(pullAll(t, stranger)) {
		if len(m) > 0 {
			t.Fatalf("tenant khác nhận %s: %d bản", e, len(m))
		}
	}
}
