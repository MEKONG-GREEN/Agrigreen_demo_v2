package handler_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// HTX US-SEASON-01: nông dân tự mở vụ; AC-1 tự gán SOP theo giống; AC-2 ngày sạ
// trong [−15, +30]; HTX US-GPS-01 nhãn vị trí; gọi lặp cùng Idempotency-Key.
func TestOpenSeasonFarmer_HTX_US_SEASON_01(t *testing.T) {
	f := newFixture(t)
	tmpl, ver := f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1)

	expectCode(t, fa.open(f.p1, map[string]any{"sowing_date": "2026-09-13"}), http.StatusUnprocessableEntity,
		"cropcycle.sowing_date_out_of_window", "sạ quá 15 ngày trước")
	expectCode(t, fa.open(f.p1, map[string]any{"sowing_date": "2026-10-30"}), http.StatusUnprocessableEntity,
		"cropcycle.sowing_date_out_of_window", "sạ quá 30 ngày sau")
	expectCode(t, fa.open(f.p1, map[string]any{"variety_code": "ST25"}), http.StatusUnprocessableEntity,
		"cropcycle.no_sop_for_variety", "giống chưa gắn SOP")
	expectCode(t, fa.open(f.p1, map[string]any{"variety_code": "KHONGCO"}), http.StatusUnprocessableEntity,
		"cropcycle.catalog_invalid", "giống ngoài danh mục")
	expectCode(t, fa.open(f.p2, nil), http.StatusForbidden, "cropcycle.out_of_scope", "thửa của hộ khác")

	idem := key()
	body := map[string]any{"plot_ref": f.p1.String(), "season_code": "DX", "season_year": 2026, "variety_code": "OM5451",
		"sowing_date": "2026-09-20", "gps": map[string]any{"lon": "105.6", "lat": "10.05", "accuracy_m": "4"}}
	r := fa.postKey("/v1/cropcycle/seasons", idem, body)
	expect(t, r, http.StatusCreated, "mở vụ")
	s := get(r.body, "season")
	if str(s, "sop_template_ref") != tmpl || str(s, "sop_version_id") != ver || str(s, "status") != "open" ||
		str(s, "expected_harvest_date") != "2026-12-24" || str(s, "harvest_open_date") != "2026-12-19" ||
		str(s, "area_ha") != "1.5000" || str(s, "location_status") != "match" || get(s, "on_behalf") != false {
		t.Fatalf("vụ mới: %s", r.raw)
	}
	again := fa.postKey("/v1/cropcycle/seasons", idem, body)
	if again.status != http.StatusCreated || str(again.body, "season", "season_ref") != str(s, "season_ref") {
		t.Fatalf("gọi lặp phải trả đúng phản hồi cũ: %d %s", again.status, again.raw)
	}
	if ev := f.outbox(port.EventSeasonOpened); len(ev) != 1 || str(ev[0], "season_ref") != str(s, "season_ref") {
		t.Fatalf("season.opened: %v", ev)
	}
	area := f.outbox(port.EventCoopSeasonAreaChanged)
	if len(area) != 1 || str(area[0], "area_ha") != "1.5000" || num(area[0], "open_seasons") != 1 || str(area[0], "cooperative_ref") != f.coopA.String() {
		t.Fatalf("coop_season_area.changed: %v", area)
	}
	// Thửa lệch 120 m → "Vị trí không khớp" nhưng vẫn lưu; không GPS → "missing" (US-GPS-01 AC-3/AC-4).
	f2 := f.farmer(f.h2)
	r = f2.open(f.p2, nil)
	expect(t, r, http.StatusCreated, "mở vụ lệch GPS")
	if str(r.body, "season", "location_status") != "mismatch" || str(r.body, "event", "distance_m") != "120.0" {
		t.Fatalf("nhãn lệch vị trí: %s", r.raw)
	}
	r = f.managerA().open(f.p3, map[string]any{"gps": map[string]any{"source": "no_device"}})
	expect(t, r, http.StatusCreated, "mở vụ không GPS")
	if str(r.body, "season", "location_status") != "missing" || str(r.body, "season", "gps_source") != "no_device" ||
		get(r.body, "season", "on_behalf") != true {
		t.Fatalf("mở vụ hộ không GPS: %s", r.raw)
	}
	// HTX ngừng hoạt động → không mở vụ được.
	f.orgs.setActive(f.coopA, false)
	expectCode(t, fa.open(f.p5, nil), http.StatusUnprocessableEntity, "cropcycle.cooperative_inactive", "HTX ngừng hoạt động")
}

// HTX US-SEASON-04: chặn mở vụ trùng trên một thửa, kể cả khi hai yêu cầu chạy song song.
func TestOneOpenSeasonPerPlot_HTX_US_SEASON_04(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	var wg sync.WaitGroup
	results := make([]resp, 6)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = mgr.open(f.p1, nil)
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, r := range results {
		switch {
		case r.status == http.StatusCreated:
			ok++
		case r.status == http.StatusConflict && code(r) == "cropcycle.season_already_open":
		default:
			t.Fatalf("kết quả lạ: %d %s", r.status, r.raw)
		}
	}
	if ok != 1 {
		t.Fatalf("chỉ một vụ được mở, có %d", ok)
	}
	r := mgr.open(f.p1, nil)
	expectCode(t, r, http.StatusConflict, "cropcycle.season_already_open", "mở vụ trùng")
	if str(r.body, "title") != "Lô ruộng đang canh tác vụ khác, không thể mở vụ mới." {
		t.Fatalf("thông báo: %s", r.raw)
	}
}

// HTX US-SEASON-02 + US-SEASON-04 AC-2: quản lý HTX mở vụ hàng loạt; lô đang canh tác
// hoặc ngoài quyền quản lý bị bỏ qua kèm nhãn lý do.
func TestBulkOpen_HTX_US_SEASON_02(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	mgr.mustOpen(f.p3)
	body := map[string]any{"plot_refs": []string{f.p1.String(), f.p2.String(), f.p3.String(), f.p4.String(), ids.New().String()},
		"season_code": "DX", "season_year": 2026, "variety_code": "OM5451", "sowing_date": today}
	r := mgr.post("/v1/cropcycle/seasons/bulk-open", body)
	expect(t, r, http.StatusOK, "mở vụ hàng loạt")
	if num(r.body, "opened") != 2 || num(r.body, "skipped") != 3 {
		t.Fatalf("mở 2, bỏ 3: %s", r.raw)
	}
	reasons := map[string]string{}
	for _, it := range list(r.body, "items") {
		reasons[str(it, "plot_ref")] = str(it, "reason")
	}
	if reasons[f.p3.String()] != "Lô đang canh tác vụ khác" || reasons[f.p4.String()] != "Lô không thuộc quyền quản lý" {
		t.Fatalf("nhãn lý do: %v", reasons)
	}
	expectCode(t, f.farmer(f.h1).post("/v1/cropcycle/seasons/bulk-open", body), http.StatusForbidden, "forbidden", "nông dân không mở hàng loạt")
	// Quản lý HTX B không thấy vụ của HTX A.
	if n := len(list(f.managerB().get("/v1/cropcycle/seasons").body, "items")); n != 0 {
		t.Fatalf("HTX B thấy %d vụ của HTX A", n)
	}
	if n := len(list(mgr.get("/v1/cropcycle/seasons?status=open").body, "items")); n != 3 {
		t.Fatalf("HTX A có 3 vụ mở, thấy %d", n)
	}
}

// HTX US-SEASON-03: sao chép cấu hình vụ trước chỉ khi lô đã hoàn thành vụ.
func TestCopyPreviousSeason_HTX_US_SEASON_03(t *testing.T) {
	f := newFixture(t)
	_, ver := f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	expectCode(t, mgr.get("/v1/cropcycle/plots/"+f.p1.String()+"/previous-config"), http.StatusConflict, "cropcycle.no_previous_season", "chưa có vụ")
	season := mgr.mustOpen(f.p1)
	expectCode(t, mgr.get("/v1/cropcycle/plots/"+f.p1.String()+"/previous-config"), http.StatusConflict,
		"cropcycle.previous_season_not_completed", "đang canh tác")
	expectCode(t, mgr.open(f.p1, map[string]any{"copy_previous": true}), http.StatusConflict, "cropcycle.previous_season_not_completed", "sao chép khi đang canh tác")
	f.advanceDays(92)
	expect(t, mgr.post("/v1/cropcycle/seasons/"+season+"/harvest", map[string]any{"mass_kg": "9750"}), http.StatusCreated, "khai sản lượng")
	r := mgr.get("/v1/cropcycle/plots/" + f.p1.String() + "/previous-config")
	expect(t, r, http.StatusOK, "cấu hình vụ trước")
	if str(r.body, "variety_code") != "OM5451" || str(r.body, "sop_version_id") != ver {
		t.Fatalf("cấu hình: %s", r.raw)
	}
	r = mgr.open(f.p1, map[string]any{"copy_previous": true, "variety_code": "ST25", "season_code": "HT", "season_year": 2027})
	expect(t, r, http.StatusCreated, "mở vụ sao chép")
	s := get(r.body, "season")
	if str(s, "variety_code") != "OM5451" || str(s, "sop_version_id") != ver || str(s, "copied_from_season_ref") != season {
		t.Fatalf("vụ sao chép: %s", r.raw)
	}
}

// Sửa / hủy vụ là bản ghi bù thay sự kiện hiện hành; supersedes cũ → concurrent_edit.
func TestSeasonCorrectionAndCancel(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	_, st25 := f.publishedSOP("SOP ST25", "ST25")
	fa := f.farmer(f.h1)
	r := fa.open(f.p1, nil)
	expect(t, r, http.StatusCreated, "mở vụ")
	season, ev1 := str(r.body, "season", "season_ref"), str(r.body, "season", "current_event_id")
	path := "/v1/cropcycle/seasons/" + season
	expectCode(t, fa.post(path+"/corrections", map[string]any{"supersedes_id": ev1, "correction_reason": "ngắn"}),
		http.StatusUnprocessableEntity, "validation_failed", "lý do ngắn")
	r = fa.post(path+"/corrections", map[string]any{"supersedes_id": ev1, "correction_reason": "Chọn nhầm giống khi mở vụ", "variety_code": "ST25"})
	expect(t, r, http.StatusCreated, "sửa giống")
	if str(r.body, "season", "variety_code") != "ST25" || str(r.body, "season", "sop_version_id") != st25 || num(r.body, "season", "version") != 2 {
		t.Fatalf("sửa giống phải gán lại SOP theo giống: %s", r.raw)
	}
	expectCode(t, fa.post(path+"/corrections", map[string]any{"supersedes_id": ev1, "correction_reason": "Sửa dựa trên bản cũ"}),
		http.StatusConflict, "cropcycle.concurrent_edit", "sửa trên phiên bản cũ")
	expectCode(t, f.officerA().post(path+"/corrections", map[string]any{"supersedes_id": str(r.body, "season", "current_event_id"),
		"correction_reason": "Cán bộ KN không được sửa"}), http.StatusForbidden, "forbidden", "KN chỉ đọc")
	r = fa.post(path+"/cancel", map[string]any{"supersedes_id": str(r.body, "season", "current_event_id"), "reason": "Mở vụ nhầm thửa"})
	expect(t, r, http.StatusCreated, "hủy vụ")
	if str(r.body, "season", "status") != "cancelled" {
		t.Fatalf("hủy: %s", r.raw)
	}
	d := fa.get(path)
	expect(t, d, http.StatusOK, "đọc vụ")
	if evs := list(d.body, "events"); len(evs) != 3 || str(evs[1], "supersedes_id") != ev1 || str(evs[2], "action") != "cancelled" {
		t.Fatalf("lịch sử phải giữ đủ 3 sự kiện: %s", d.raw)
	}
	cor := f.outbox(port.EventSeasonCorrected)
	if len(cor) != 2 || str(cor[1], "action") != "cancelled" {
		t.Fatalf("season.corrected: %v", cor)
	}
	area := f.outbox(port.EventCoopSeasonAreaChanged)
	if last := area[len(area)-1]; str(last, "area_ha") != "0.0000" {
		t.Fatalf("vụ hủy không tính diện tích: %v", area)
	}
	// Sau khi hủy, mở vụ mới được.
	fa.mustOpen(f.p1)
}

// Cách ly tenant + phạm vi cây phân cấp: KN xã 31117 chỉ đọc vụ ở xã mình; tenant khác không thấy gì.
func TestTenantIsolationAndHierarchyScope(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	sA := f.managerA().mustOpen(f.p1)
	sB := f.managerB().mustOpen(f.p4)
	kn := f.officerA()
	items := list(kn.get("/v1/cropcycle/seasons").body, "items")
	if len(items) != 1 || str(items[0], "season_ref") != sA {
		t.Fatalf("KN xã 31117 chỉ thấy vụ xã mình: %v", items)
	}
	expectCode(t, kn.get("/v1/cropcycle/seasons/"+sB), http.StatusNotFound, "cropcycle.season_not_found", "vụ ngoài xã")
	expectCode(t, kn.open(f.p1, nil), http.StatusForbidden, "forbidden", "KN không mở vụ")

	other := f.as(ids.New(), f.coopA, port.RoleCoopManager)
	if n := len(list(other.get("/v1/cropcycle/seasons").body, "items")); n != 0 {
		t.Fatalf("tenant khác thấy %d vụ", n)
	}
	expectCode(t, other.get("/v1/cropcycle/seasons/"+sA), http.StatusNotFound, "cropcycle.season_not_found", "đọc chéo tenant")
	if n := len(list(f.platformAdmin().get("/v1/cropcycle/seasons").body, "items")); n != 2 {
		t.Fatalf("quản trị thấy mọi vụ: %d", n)
	}
}
