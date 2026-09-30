package handler_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// boatLoadCode giao sự kiện đếm cuộn boat_load mang số hiệu ghe (trường tùy chọn G4 của roll_count.recorded v1).
func (f *fixture) boatLoadCode(lot uuid.UUID, rolls int, code string, at time.Time) {
	f.t.Helper()
	p := fieldport.RollCountRecordedV1{RollCountID: ids.New(), LotRef: lot, PlotRef: ids.New(), HTXRef: ids.New(), Rolls: rolls,
		Stage: fieldport.StageBoatLoad, RuleVersion: "fieldops.count.v1", EventTime: at, VesselCode: &code}
	f.deliver(f.tenant, port.RollCountConsumer, f.cons.HandleRollCount, fieldport.EventRollCountRecorded, p, ids.New())
}

// T-1 (proto FM-05, G4 tích hợp mục 2): lượt xuống ghe mang số hiệu ghe khớp danh mục → chuyến planned được GÁN
// phương tiện ngay (chuẩn hóa GH-01, cảnh báo quá tải như gán tay); số hiệu ngoài danh mục → cờ vessel_unregistered
// + no_vehicle, điều phối gán sau; sự kiện không có số hiệu (app cũ) → no_vehicle như trước.
func TestLogisticsT1FM05VesselCodeAssignsVehicle(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	v := d.post("/v1/logistics/vessels", map[string]any{"code": "CT-0001", "kind": "ghe", "straw_payload_tons": "80"})
	mustStatus(t, v, http.StatusCreated, "thêm ghe")
	vessel := str(v.body, "vessel", "vessel_id")
	at := f.clk.Now().Add(-time.Hour)

	lot := ids.New()
	f.boatLoadCode(lot, 4400, " ct-0001 ", at) // 4 400 × 22 kg = 96,8 t > 1,1 × 80 t → quá tải (chỉ cảnh báo)
	tr := f.tripsForLot(f.tenant, lot)
	if len(tr) != 1 || tr[0].VehicleRef == nil || tr[0].VehicleRef.String() != vessel {
		t.Fatalf("gán ghe theo số hiệu: %+v", tr)
	}
	g := d.get("/v1/logistics/trips/" + tr[0].TripID.String())
	if hasFlag(g.body, domain.FlagNoVehicle, "trip", "flags") || !hasFlag(g.body, domain.FlagOverload, "trip", "flags") ||
		str(g.body, "trip", "vehicle_ref") != vessel {
		t.Fatalf("cờ chuyến có ghe: %s", g.raw)
	}

	lot2 := ids.New()
	f.boatLoadCode(lot2, 100, "KG-9999", at)
	tr2 := f.tripsForLot(f.tenant, lot2)
	g2 := d.get("/v1/logistics/trips/" + tr2[0].TripID.String())
	if tr2[0].VehicleRef != nil || !hasFlag(g2.body, domain.FlagVesselUnregistered, "trip", "flags") || !hasFlag(g2.body, domain.FlagNoVehicle, "trip", "flags") {
		t.Fatalf("ghe ngoài danh mục: %s", g2.raw)
	}
	// Điều phối gán ghe sau → hết cờ vessel_unregistered / no_vehicle.
	a := d.post("/v1/logistics/trips/"+tr2[0].TripID.String()+"/vehicle", map[string]any{"vehicle_ref": vessel})
	mustStatus(t, a, http.StatusCreated, "gán sau")
	if hasFlag(a.body, domain.FlagVesselUnregistered, "trip", "flags") || hasFlag(a.body, domain.FlagNoVehicle, "trip", "flags") {
		t.Fatalf("gán sau: %s", a.raw)
	}

	lot3 := ids.New()
	f.boatLoad(f.tenant, lot3, 100, nil, at) // không có số hiệu (tương thích ngược)
	if tr3 := f.tripsForLot(f.tenant, lot3); len(tr3) != 1 || tr3[0].VehicleRef != nil {
		t.Fatalf("không số hiệu: %+v", tr3)
	}
}

// L-08 / L-10 (proto GH-02, nhắc ghe chưa cân — G4 tích hợp mục 3): job logistics_alerts quét định kỳ → một thông
// báo mỗi điều kiện (chống lặp giữa các lượt quét), metric theo loại cho Alertmanager, chỉ cảnh báo. Tenant tìm qua
// vai ag_outbox_publisher (chỉ đọc cột tenant_id, status).
func TestLogisticsL08AlertJobNotices(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d := f.dispatcher()
	f.svc.Metrics = app.NewMetrics(nil)
	old := ids.New()
	f.boatLoad(f.tenant, old, 1000, nil, f.clk.Now().Add(-50*time.Hour)) // > 48 giờ chưa cân
	f.boatLoad(f.tenant, ids.New(), 1000, nil, f.clk.Now().Add(-2*time.Hour))
	mustStatus(t, d.post("/v1/logistics/vessels", map[string]any{"code": "HH-1", "kind": "ghe",
		"registration_expiry": f.clk.Now().AddDate(0, 0, -2).Format("2006-01-02")}), http.StatusCreated, "ghe hết hạn")
	mustStatus(t, d.post("/v1/logistics/vessels", map[string]any{"code": "SH-1", "kind": "sa_lan",
		"registration_expiry": f.clk.Now().AddDate(0, 0, 10).Format("2006-01-02")}), http.StatusCreated, "sà lan sắp hết hạn")
	mustStatus(t, d.post("/v1/logistics/vessels", map[string]any{"code": "OK-1", "kind": "ghe",
		"registration_expiry": f.clk.Now().AddDate(1, 0, 0).Format("2006-01-02")}), http.StatusCreated, "ghe còn hạn")

	all, err := logistics.ActiveTenants(f.env.Pool(t, migrations.MKG, dbadmin.OutboxPublisher))(ctx)
	if err != nil || !slices.Contains(all, f.tenant) {
		t.Fatalf("ActiveTenants qua ag_outbox_publisher: %v %v", err, err)
	}
	only := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }
	r, err := f.svc.ScanAlerts(ctx, only)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tenants != 1 || r.Open[app.NoticeTripUnweighed] != 1 || r.Open[app.NoticeRegistrationExpired] != 1 ||
		r.Open[app.NoticeRegistrationExpiring] != 1 || r.NewNotices[app.NoticeTripUnweighed] != 1 {
		t.Fatalf("lượt 1: %+v", r)
	}
	// Lượt 2: điều kiện vẫn mở, KHÔNG gửi lại.
	r, _ = f.svc.ScanAlerts(ctx, only)
	if r.Open[app.NoticeTripUnweighed] != 1 || len(r.NewNotices) != 0 {
		t.Fatalf("lượt 2 phải không có thông báo mới: %+v", r)
	}
	m := f.svc.Metrics
	if m.Count("open:"+app.NoticeTripUnweighed) != 1 || m.Count("notices:"+app.NoticeRegistrationExpired) != 1 || m.Count("scans:ok") != 2 {
		t.Fatalf("metric: open=%d notices=%d scans=%d", m.Count("open:"+app.NoticeTripUnweighed),
			m.Count("notices:"+app.NoticeRegistrationExpired), m.Count("scans:ok"))
	}
	l := d.get("/v1/logistics/alert-notices")
	mustStatus(t, l, http.StatusOK, "thông báo")
	items := arr(l.body, "items")
	if len(items) != 3 {
		t.Fatalf("3 thông báo: %s", l.raw)
	}
	kinds := map[string]map[string]any{}
	for _, it := range items {
		kinds[str(it, "kind")] = it
	}
	un := kinds[app.NoticeTripUnweighed]
	if str(un, "subject_ref") != f.tripsForLot(f.tenant, old)[0].TripID.String() || str(un, "detail", "threshold_hours") != "48" ||
		len(un["audience"].([]any)) != 2 || kinds[app.NoticeRegistrationExpired]["subject_code"] != "HH-1" {
		t.Fatalf("nội dung thông báo: %s", l.raw)
	}
	if u := d.get("/v1/logistics/alert-notices?kind=registration_expiring"); len(arr(u.body, "items")) != 1 {
		t.Fatalf("lọc loại: %s", u.raw)
	}
	mustStatus(t, f.driver().get("/v1/logistics/alert-notices"), http.StatusForbidden, "tài xế không đọc thông báo điều phối")
	// Cân xong → điều kiện đóng (open = 0), sổ thông báo giữ nguyên (chỉ ghi thêm).
	f.weigh(f.tenant, old, "22000", nil)
	r, _ = f.svc.ScanAlerts(ctx, only)
	if r.Open[app.NoticeTripUnweighed] != 0 || m.Count("open:"+app.NoticeTripUnweighed) != 0 {
		t.Fatalf("sau khi cân: %+v", r)
	}
	f.mustFail(f.pool, "RB-1 alert_notice chỉ ghi thêm", "UPDATE mkg_logistics.alert_notice SET kind = kind")
	// RB-2: tenant khác không thấy thông báo.
	if o := f.as(ids.New(), port.RoleDispatcher).get("/v1/logistics/alert-notices"); len(arr(o.body, "items")) != 0 {
		t.Fatalf("RB-2: %s", o.raw)
	}
}

// G4 tích hợp mục 8–12 (app tài xế): danh sách "chuyến của tôi" (driver_ref = sub của token, tự lọc với người
// chỉ có vai driver), chỉ tài xế được giao / điều phối ghi mốc (403 logistics.not_assigned_driver), tài xế không
// thấy cước, GPS thiếu → NULL (không 0,0) + accuracy_m tùy chọn, delta updated_since theo recorded_at.
func TestLogisticsDriverScopeMilestonesAndDelta_G4(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	drvA, drvB := f.driver(), f.driver()
	a := f.createTripFor(d, "road", ids.New(), "12000", drvA.actor)
	b := f.createTripFor(d, "road", ids.New(), "8000", drvB.actor)
	f.createTrip(d, "road", ids.New(), "5000") // chưa giao tài xế
	mustStatus(t, d.post("/v1/logistics/trips/"+a+"/legs", map[string]any{"legs": []any{roadLeg("10.0452", "105.7469", "10.386", "105.4352")}}),
		http.StatusCreated, "chặng a")

	// Mục 8: tài xế chỉ thấy chuyến của mình; điều phối thấy mọi chuyến, assigned_to_me lọc theo sub.
	la := drvA.get("/v1/logistics/trips")
	if items := arr(la.body, "items"); len(items) != 1 || str(items[0], "trip_id") != a {
		t.Fatalf("tài xế A: %s", la.raw)
	}
	if l := drvA.get("/v1/logistics/trips?assigned_to_me=false"); len(arr(l.body, "items")) != 1 {
		t.Fatalf("tài xế luôn bị lọc: %s", l.raw)
	}
	if l := d.get("/v1/logistics/trips"); len(arr(l.body, "items")) != 3 {
		t.Fatalf("điều phối: %s", l.raw)
	}
	if l := d.get("/v1/logistics/trips?assigned_to_me=true"); len(arr(l.body, "items")) != 0 {
		t.Fatalf("điều phối assigned_to_me: %s", l.raw)
	}
	mustStatus(t, d.get("/v1/logistics/trips?assigned_to_me=yes"), http.StatusUnprocessableEntity, "assigned_to_me sai")
	mustCode(t, drvA.get("/v1/logistics/trips/"+b), http.StatusNotFound, "logistics.trip_not_found", "tài xế đọc chuyến người khác")

	// Mục 10: tài xế không thấy cước (chuyến, chặng); điều phối thấy.
	ga, gd := drvA.get("/v1/logistics/trips/"+a), d.get("/v1/logistics/trips/"+a)
	tripA, tripD := ga.body["trip"].(map[string]any), gd.body["trip"].(map[string]any)
	legA, legD := arr(tripA, "legs")[0], arr(tripD, "legs")[0]
	if _, ok := tripA["planned_cost"]; ok || legA["planned_cost"] != nil || legA["rate_vnd_per_t_km"] != nil {
		t.Fatalf("tài xế thấy cước: %s", ga.raw)
	}
	if str(tripD, "planned_cost") == "" || str(legD, "rate_vnd_per_t_km") == "" || str(legA, "distance_km") == "" {
		t.Fatalf("điều phối phải thấy cước / tài xế thấy km: %s | %s", gd.raw, ga.raw)
	}
	for _, it := range arr(la.body, "items") {
		if _, ok := it["planned_cost"]; ok {
			t.Fatalf("danh sách tài xế có cước: %s", la.raw)
		}
	}

	// Mục 9: tài xế khác → 403; điều phối ghi được cho mọi chuyến.
	ms := func(c caller, trip string, body map[string]any) resp { return c.post("/v1/logistics/trips/"+trip+"/milestones", body) }
	mustCode(t, ms(drvB, a, map[string]any{"kind": "departed", "event_time": f.ts(-time.Minute)}), http.StatusForbidden,
		"logistics.not_assigned_driver", "tài xế không được giao")
	mustStatus(t, ms(d, b, map[string]any{"kind": "departed", "event_time": f.ts(-time.Minute), "lat": "10.1", "lng": "105.1"}),
		http.StatusCreated, "điều phối ghi mốc thay")

	// Delta: mốc trước khi ghi thêm.
	all := d.get("/v1/logistics/trips?updated_since=" + url.QueryEscape(f.clk.Now().Add(-24*time.Hour).Format(time.RFC3339)))
	items := arr(all.body, "items")
	if len(items) != 3 {
		t.Fatalf("delta toàn bộ: %s", all.raw)
	}
	since := str(items[len(items)-1], "recorded_at") // sắp recorded_at tăng dần

	// Mục 11: không GPS → NULL; 0,0 (app cũ) → NULL; chỉ lat → 422; accuracy_m lưu sổ riêng.
	r := ms(drvA, a, map[string]any{"kind": "departed", "event_time": f.ts(-time.Minute)})
	mustStatus(t, r, http.StatusCreated, "xuất phát không GPS")
	if r.body["event"].(map[string]any)["lat"] != nil || r.body["event"].(map[string]any)["lng"] != nil {
		t.Fatalf("không GPS phải NULL: %s", r.raw)
	}
	if _, ok := r.body["trip"].(map[string]any)["planned_cost"]; ok {
		t.Fatalf("phản hồi mốc cho tài xế có cước: %s", r.raw)
	}
	mustCode(t, ms(drvA, a, map[string]any{"kind": "arrived", "event_time": f.ts(0), "lat": "10.2"}), http.StatusUnprocessableEntity,
		"validation_failed", "chỉ lat")
	r = ms(drvA, a, map[string]any{"kind": "arrived", "event_time": f.ts(0), "lat": "10.386", "lng": "105.4352", "accuracy_m": "7.5"})
	mustStatus(t, r, http.StatusCreated, "đến có GPS")
	if str(r.body, "event", "accuracy_m") != "7.5" || str(r.body, "event", "lat") != "10.386" {
		t.Fatalf("accuracy_m: %s", r.raw)
	}
	ev := d.get("/v1/logistics/trips/" + a + "/events")
	var dep, arrv map[string]any
	for _, e := range arr(ev.body, "items") {
		switch str(e, "kind") {
		case "departed":
			dep = e
		case "arrived":
			arrv = e
		}
	}
	if dep["lat"] != nil || str(arrv, "accuracy_m") != "7.5" {
		t.Fatalf("sổ mốc: %s", ev.raw)
	}
	c := f.createTripFor(d, "road", ids.New(), "1000", drvB.actor)
	r = ms(drvB, c, map[string]any{"kind": "departed", "event_time": f.ts(-time.Minute), "lat": 0, "lng": 0})
	if r.status != http.StatusCreated || r.body["event"].(map[string]any)["lat"] != nil {
		t.Fatalf("0,0 phải lưu NULL: %s", r.raw)
	}

	// Mục 12: delta sau mốc since → chỉ chuyến có sự kiện mới (a, c), tăng dần; phân trang theo recorded_at.
	dl := d.get("/v1/logistics/trips?updated_since=" + url.QueryEscape(since))
	got := []string{}
	for _, it := range arr(dl.body, "items") {
		got = append(got, str(it, "trip_id"))
	}
	if len(got) != 2 || !slices.Contains(got, a) || !slices.Contains(got, c) {
		t.Fatalf("delta: %v (%s)", got, dl.raw)
	}
	p1 := d.get("/v1/logistics/trips?limit=1&updated_since=" + url.QueryEscape(since))
	next := str(p1.body, "next_cursor")
	p2 := d.get("/v1/logistics/trips?limit=1&updated_since=" + url.QueryEscape(since) + "&cursor=" + url.QueryEscape(next))
	if next == "" || len(arr(p2.body, "items")) != 1 || str(arr(p2.body, "items")[0], "trip_id") == str(arr(p1.body, "items")[0], "trip_id") {
		t.Fatalf("phân trang delta: %s | %s", p1.raw, p2.raw)
	}
	if l := drvB.get("/v1/logistics/trips?updated_since=" + url.QueryEscape(since)); len(arr(l.body, "items")) != 1 {
		t.Fatalf("delta tài xế B (chỉ c): %s", l.raw)
	}
	mustStatus(t, d.get("/v1/logistics/trips?updated_since=hôm-qua"), http.StatusUnprocessableEntity, "updated_since sai")
}
