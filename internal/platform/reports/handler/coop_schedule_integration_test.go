package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// HTX US-DASH-03: màn lập lịch của Quản lý HTX dùng Application API (không cần MFA của Admin
// API) — chỉ HTX của mình; người nhận chỉ chọn trong tài khoản đã đăng ký của HTX (email đã
// xác minh); ngừng lịch cần lý do, không xóa; đến mốc thì gửi đúng hòm thư đã chọn.
func TestCoopScheduleAppSurface_HTX_US_DASH_03(t *testing.T) {
	f := newFixture(t, true)
	mgr := f.as(f.org.coop, ccport.RoleCoopManager)
	member, outsider := ids.New(), ids.New()
	f.dir.emails[member] = "ke.toan@htx-binhduc.test"
	f.dir.members[member] = f.org.coop
	f.dir.emails[outsider] = "nguoi.ngoai@htx-caikhe.test"
	f.dir.members[outsider] = f.org.coopCaiKhe

	rc := mgr.must(mgr.get("/v1/reports/recipients"), http.StatusOK, "danh sách người nhận")
	list := items(rc)
	if len(list) != 1 || list[0]["actor_ref"] != member.String() || strings.Contains(list[0]["masked_email"].(string), "ke.toan@") {
		t.Fatalf("chỉ tài khoản của HTX, email đã che: %v", list)
	}
	body := func(rcpt uuid.UUID) map[string]any {
		return map[string]any{"template_key": "htx.production", "format": "xlsx", "frequency": "monthly", "day_of_month": 1, "hour": 6,
			"recipients": []string{rcpt.String()}, "params": map[string]any{"season_code": "HT", "season_year": 2026}}
	}
	// Người nhận ngoài HTX → 422 kèm thông điệp AC-2.
	if r := mgr.post("/v1/reports/schedules", body(outsider)); r.status != http.StatusUnprocessableEntity ||
		r.body["code"] != "reports.recipient_not_in_cooperative" || r.body["title"] != "Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại." {
		t.Fatalf("người nhận ngoài HTX: %d %v", r.status, r.body)
	}
	// Idempotent: gọi lặp cùng khóa → cùng lịch.
	k := key()
	s1 := mgr.do(f.app, http.MethodPost, "/v1/reports/schedules", k, body(member))
	s2 := mgr.do(f.app, http.MethodPost, "/v1/reports/schedules", k, body(member))
	if s1.status != http.StatusCreated || s2.body["id"] != s1.body["id"] {
		t.Fatalf("lập lịch + gọi lặp: %d %v / %v", s1.status, s1.body, s2.body)
	}
	if pr, _ := s1.body["params"].(map[string]any); pr["cooperative_ref"] != f.org.coop.String() {
		t.Fatalf("lịch ghi HTX của người lập: %v", s1.body)
	}
	id := s1.body["id"].(string)
	if l := items(mgr.must(mgr.get("/v1/reports/schedules"), http.StatusOK, "lịch của tôi")); len(l) != 1 {
		t.Fatalf("lịch của mình: %v", l)
	}
	// HTX khác, tenant khác không thấy; cán bộ KN dùng Admin API, không dùng màn này.
	other := f.as(f.org.coopCaiKhe, ccport.RoleCoopManager)
	if l := items(other.must(other.get("/v1/reports/schedules"), http.StatusOK, "HTX khác")); len(l) != 0 {
		t.Fatalf("HTX khác thấy lịch: %v", l)
	}
	if r := other.post("/v1/reports/schedules/"+id+"/deactivate", map[string]any{"reason": "Ngừng lịch của HTX khác"}); r.status != http.StatusNotFound {
		t.Fatalf("HTX khác ngừng lịch: %d", r.status)
	}
	stranger := f.asTenant(ids.New(), f.org.coop, ccport.RoleCoopManager)
	if l := items(stranger.must(stranger.get("/v1/reports/schedules"), http.StatusOK, "tenant khác")); len(l) != 0 {
		t.Fatalf("tenant khác thấy lịch: %v", l)
	}
	if r := f.as(f.org.pAG, extport.RoleKNProvinceAdmin).get("/v1/reports/schedules"); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ KN trên Application API: %d", r.status)
	}
	// Mẫu KN không lập được từ màn HTX.
	kn := body(member)
	kn["template_key"] = "kn.sowing_progress"
	if r := mgr.post("/v1/reports/schedules", kn); r.status != http.StatusForbidden {
		t.Fatalf("mẫu KN: %d %v", r.status, r.body)
	}

	// Đến mốc: gửi đúng hòm thư đã chọn; lần chạy hiện trạng thái gửi.
	f.clk.Advance(4 * 24 * time.Hour)
	if err := f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runWorker()
	for _, e := range f.events(port.EventExportCompleted) {
		f.deliver(port.DeliveryConsumer, e, f.mod.DeliveryHandler())
	}
	if msgs := f.mailer.messages(); len(msgs) != 1 || msgs[0].To != "ke.toan@htx-binhduc.test" {
		t.Fatalf("email: %+v", msgs)
	}
	runs := items(mgr.must(mgr.get("/v1/reports/schedules/"+id+"/runs"), http.StatusOK, "lần chạy"))
	if len(runs) != 1 || runs[0]["delivery_status"] != "sent" {
		t.Fatalf("lần chạy: %v", runs)
	}
	// Ngừng lịch: lý do bắt buộc; không xóa.
	if r := mgr.post("/v1/reports/schedules/"+id+"/deactivate", map[string]any{"reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn: %d", r.status)
	}
	d := mgr.must(mgr.post("/v1/reports/schedules/"+id+"/deactivate", map[string]any{"reason": "HTX đổi kỳ báo cáo sang quý"}), http.StatusOK, "ngừng lịch")
	if d.body["active"] != false {
		t.Fatalf("ngừng lịch: %v", d.body)
	}
	if l := items(mgr.get("/v1/reports/schedules")); len(l) != 1 || l[0]["active"] != false {
		t.Fatalf("lịch đã ngừng vẫn còn trong danh sách: %v", l)
	}
}
