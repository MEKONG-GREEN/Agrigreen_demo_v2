package handler_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// L-02: trạng thái chuyến chỉ tiến planned → departed → arrived → completed, hủy có lý do từ
// planned/departed — cưỡng chế ở domain/app (409) VÀ ở DB (trigger trip_forward_only).
func TestLogisticsL02ForwardOnlyStatus(t *testing.T) {
	f := newFixture(t)
	d, drv := f.dispatcher(), f.driver()
	trip := f.createTrip(d, "road", ids.New(), "12000")
	ms := func(kind string) resp {
		return drv.post("/v1/logistics/trips/"+trip+"/milestones", map[string]any{"kind": kind, "event_time": f.ts(-time.Minute),
			"lat": "10.0452", "lng": "105.7469"})
	}
	mustCode(t, ms("arrived"), http.StatusConflict, "logistics.trip_transition", "planned → arrived (nhảy bậc)")
	mustStatus(t, ms("departed"), http.StatusCreated, "xuất phát")
	mustCode(t, ms("departed"), http.StatusConflict, "logistics.trip_transition", "departed → departed")
	r := ms("arrived")
	mustStatus(t, r, http.StatusCreated, "đến")
	if str(r.body, "trip", "status") != "arrived" || str(r.body, "event", "lat") != "10.0452" {
		t.Fatalf("mốc đến: %s", r.raw)
	}
	mustCode(t, d.post("/v1/logistics/trips/"+trip+"/cancel", map[string]any{"reason": "Hủy thử sau khi đã đến bến"}),
		http.StatusConflict, "logistics.trip_transition", "arrived → cancelled")
	// Tài xế không lập / hủy chuyến; điều phối hủy phải có lý do.
	mustStatus(t, drv.post("/v1/logistics/trips", map[string]any{"mode": "road"}), http.StatusForbidden, "tài xế lập chuyến")
	t2 := f.createTrip(d, "road", ids.New(), "1000")
	mustStatus(t, d.post("/v1/logistics/trips/"+t2+"/cancel", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity, "lý do ngắn")
	mustStatus(t, d.post("/v1/logistics/trips/"+t2+"/cancel", map[string]any{"reason": "Khách hoãn nhận hàng tới tuần sau"}),
		http.StatusCreated, "hủy")
	mustCode(t, d.post("/v1/logistics/trips/"+t2+"/vehicle", map[string]any{"vehicle_ref": ids.New().String()}),
		http.StatusConflict, "logistics.trip_closed", "gán xe cho chuyến đã hủy")
	// Lớp DB: lùi trạng thái bằng UPDATE thẳng bảng chiếu bị trigger chặn.
	f.mustFail(f.pool, "arrived → planned", "UPDATE mkg_logistics.trip_current SET status = 'planned' WHERE trip_id = $1", trip)
	f.mustFail(f.pool, "cancelled → planned", "UPDATE mkg_logistics.trip_current SET status = 'planned' WHERE trip_id = $1", t2)
}

// Idempotent POST (R-51): gọi lặp cùng Idempotency-Key → cùng chuyến, 200, sổ chỉ một dòng.
func TestLogisticsIdempotentPostReplay(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	k := key()
	body := map[string]any{"mode": "road", "cargo": []map[string]any{{"lot_ref": ids.New().String(), "mass_kg": "5000"}}}
	a := d.do(http.MethodPost, "/v1/logistics/trips", k, body)
	mustStatus(t, a, http.StatusCreated, "lần 1")
	b := d.do(http.MethodPost, "/v1/logistics/trips", k, body)
	if b.status != http.StatusCreated && b.status != http.StatusOK {
		t.Fatalf("lần 2: %d %s", b.status, b.raw)
	}
	if str(a.body, "trip", "trip_id") != str(b.body, "trip", "trip_id") {
		t.Fatal("gọi lặp phải trả cùng chuyến")
	}
	// Lớp sổ: bỏ qua middleware (gọi service thẳng cùng khóa) vẫn không ghi thêm.
	legKey := key()
	trip := str(a.body, "trip", "trip_id")
	legBody := map[string]any{"legs": []any{waterLeg()}}
	l1 := d.do(http.MethodPost, "/v1/logistics/trips/"+trip+"/legs", legKey, legBody)
	mustStatus(t, l1, http.StatusCreated, "lập chặng")
	l2 := d.do(http.MethodPost, "/v1/logistics/trips/"+trip+"/legs", legKey, legBody)
	if l2.status >= 300 {
		t.Fatalf("lập chặng lặp: %d %s", l2.status, l2.raw)
	}
	// Lớp sổ (idem_key của sổ, không qua middleware): gọi service thẳng cùng khóa → replayed, không định tuyến lại.
	pctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: d.actor, TenantID: f.tenant, Roles: []string{port.RoleDispatcher}})
	pt := port.GeoPoint{Lat: decimal.RequireFromString("10.1"), Lng: decimal.RequireFromString("105.1")}
	if _, replayed, err := f.svc.PlanLegs(pctx, parse(t, trip), []app.LegInput{{Mode: port.ModeRoad,
		From: app.Endpoint{Kind: "point", Point: pt}, To: app.Endpoint{Kind: "point", Point: pt}}}, legKey); err != nil || !replayed {
		t.Fatalf("lập chặng lặp ở lớp sổ: replayed=%v err=%v", replayed, err)
	}
	if r, replayed, err := f.svc.CreateTrip(pctx, app.CreateTripInput{Mode: port.ModeRoad, IdemKey: k}); err != nil || !replayed ||
		r.Trip.TripID.String() != trip {
		t.Fatalf("lập chuyến lặp ở lớp sổ: replayed=%v err=%v", replayed, err)
	}
	var trips, legs int
	f.inTenant(func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM mkg_logistics.trip_event").Scan(&trips); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM mkg_logistics.leg_event").Scan(&legs)
	})
	if trips != 1 || legs != 1 || f.ww.CallCount() != 1 {
		t.Fatalf("sổ chuyến %d, chặng %d, lần gọi định tuyến %d (muốn 1/1/1)", trips, legs, f.ww.CallCount())
	}
}

// L-03 + L-04: chặng đường thủy định tuyến qua bề mặt đối tác, NGUYÊN VĂN phản hồi + SHA-256 lưu vào
// route_snapshot (DB kiểm băm), phiên bản mạng ghim; đọc chặng cũ không gọi lại nền tảng; cước kế hoạch
// = tấn × km × đơn giá tham số, rule_version ghim.
func TestLogisticsL03RouteSnapshotStoredWithHash(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	trip := f.createTrip(d, "waterway", ids.New(), "96800")
	r := d.post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{waterLeg()}})
	mustStatus(t, r, http.StatusCreated, "lập chặng")
	legs := arr(r.body, "trip", "legs")
	if len(legs) != 1 {
		t.Fatalf("chặng: %s", r.raw)
	}
	l := legs[0]
	if str(l, "route_source") != "routed" || str(l, "distance_km") != "42.500" || str(l, "network_version") != "3" ||
		str(l, "planned_cost") != "3702600.00" || str(l, "param_version") != port.ParamWaterwayRatePerTonKm+":default" ||
		str(l, "rule_version") != domain.RuleTripCost {
		t.Fatalf("chặng đường thủy: %v", l)
	}
	if str(r.body, "trip", "planned_cost") != "3702600.00" || hasFlag(r.body, domain.FlagNoRoute, "trip", "flags") {
		t.Fatalf("chuyến sau lập chặng: %s", r.raw)
	}
	calls := f.ww.CallCount()
	s := d.get("/v1/logistics/trips/" + trip + "/legs/" + str(l, "leg_id") + "/route-snapshot")
	mustStatus(t, s, http.StatusOK, "ảnh chụp")
	raw, err := base64.StdEncoding.DecodeString(str(s.body, "raw_base64"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != str(s.body, "raw_sha256") || str(s.body, "provider") != store.ProviderWaterway ||
		str(s.body, "network_version") != "3" {
		t.Fatalf("ảnh chụp: %s", s.raw)
	}
	// Đọc lại chuyến / ảnh chụp không gọi lại nền tảng.
	mustStatus(t, d.get("/v1/logistics/trips/"+trip), http.StatusOK, "đọc chuyến")
	if f.ww.CallCount() != calls {
		t.Fatal("đọc chặng cũ không được gọi lại định tuyến")
	}
	// DB: băm phải khớp nguyên văn.
	f.mustFail(f.pool, "băm sai", `INSERT INTO mkg_logistics.route_snapshot (id, tenant_id, stream_key, trip_id, leg_id, provider, request, raw,
	    raw_sha256, fetched_at, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'route:x', $3, $3, 'road_haversine', '{}', '{"a":1}', sha256('khác'), now(), now(), $3, 'k-hash-sai', 'x')`,
		ids.New(), f.tenant, ids.New())
	// DB: chặng tính km phải có ảnh chụp; km tay phải có lý do.
	f.mustFail(f.pool, "chặng không ảnh chụp", `INSERT INTO mkg_logistics.leg_event (id, tenant_id, stream_key, trip_id, leg_id, plan_revision,
	    leg_seq, mode, from_kind, from_lat, from_lng, to_kind, to_lat, to_lng, distance_km, route_source, rate_vnd_per_t_km, planned_cost,
	    param_version, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'legs:x', $3, $3, 1, 1, 'road', 'point', 10, 105, 'point', 10, 105, 1, 'haversine', 1, 1, 'x', now(), $3, 'k-no-snap', 'x')`,
		ids.New(), f.tenant, ids.New())
}

// L-04: nền tảng lỗi / thiếu cấp quyền waterway_routes / mạng không có đường → chuyến vẫn lập chặng
// được: dự phòng Haversine × 1,3 (nhãn haversine) kèm cờ; km tay phải có lý do.
func TestLogisticsL04FallbackWhenRoutingDown(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	hk := domain.HaversineKm(port.GeoPoint{Lat: decimal.RequireFromString("10.0452"), Lng: decimal.RequireFromString("105.7469")},
		port.GeoPoint{Lat: decimal.RequireFromString("10.386"), Lng: decimal.RequireFromString("105.4352")})
	want := hk.Mul(decimal.RequireFromString("1.3")).Round(3).StringFixed(3)
	for _, c := range []struct {
		name                   string
		down, noGrant, missing bool
		flag                   string
	}{
		{"nền tảng lỗi", true, false, false, domain.FlagRoutingUnavailable},
		{"thiếu cấp quyền", false, true, false, domain.FlagRoutingUnavailable},
		{"không có kênh", false, false, true, domain.FlagNoWaterwayRoute},
	} {
		f.ww.Set(c.down, c.noGrant, c.missing)
		trip := f.createTrip(d, "waterway", ids.New(), "50000")
		r := d.post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{waterLeg()}})
		mustStatus(t, r, http.StatusCreated, c.name)
		l := arr(r.body, "trip", "legs")[0]
		if str(l, "route_source") != "haversine" || str(l, "distance_km") != want || !hasFlag(l, c.flag, "flags") ||
			str(l, "route_snapshot_ref") == "" {
			t.Fatalf("%s: %v (muốn %s km)", c.name, l, want)
		}
	}
	f.ww.Set(true, false, false)
	trip := f.createTrip(d, "waterway", ids.New(), "50000")
	manual := waterLeg()
	manual["manual_km"] = "38.200"
	mustCode(t, d.post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{manual}}), http.StatusUnprocessableEntity,
		"logistics.manual_km_reason_required", "km tay không lý do")
	manual["manual_reason"] = "Đo theo hải đồ của chủ ghe, tuyến kênh mới nạo vét"
	r := d.post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{manual}})
	mustStatus(t, r, http.StatusCreated, "km tay có lý do")
	l := arr(r.body, "trip", "legs")[0]
	if str(l, "route_source") != "manual" || str(l, "distance_km") != "38.200" || str(l, "route_snapshot_ref") != "" {
		t.Fatalf("km tay: %v", l)
	}
}

// L-05 + L-06 (GIS FN-08 BR-04, proto decideMode): đường bộ = Haversine × 1,3 (nhãn haversine); xa hơn
// #27 (120 km) và có kênh tiếp cận → gợi ý đường thủy (cờ), điều phối vẫn giữ phương thức đã chọn.
func TestLogisticsL05L06RoadHaversineAndModeSuggestion(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	trip := f.createTrip(d, "road", ids.New(), "20000")
	r := d.post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{
		roadLeg("10.0452", "105.7469", "10.3860", "105.4352"), // ~51 km × 1,3
		roadLeg("10.3860", "105.4352", "10.7769", "106.7009"), // Long Xuyên → TP.HCM ~145 km × 1,3 > 120
	}})
	mustStatus(t, r, http.StatusCreated, "lập chặng đường bộ")
	legs := arr(r.body, "trip", "legs")
	if len(legs) != 2 || str(legs[0], "route_source") != "haversine" || hasFlag(legs[0], domain.FlagWaterwaySuggested, "flags") ||
		!hasFlag(legs[1], domain.FlagWaterwaySuggested, "flags") || str(legs[1], "mode") != "road" {
		t.Fatalf("chặng đường bộ: %s", r.raw)
	}
	s := d.get("/v1/logistics/trips/" + trip + "/legs/" + str(legs[0], "leg_id") + "/route-snapshot")
	mustStatus(t, s, http.StatusOK, "ảnh chụp Haversine")
	if str(s.body, "provider") != store.ProviderRoad {
		t.Fatalf("nhà cung cấp: %s", s.raw)
	}
}

// L-07 + L-08 (proto GH-01, GH-02): danh mục ghe có phiên bản, số hiệu chuẩn hóa duy nhất, đơn giá > 0
// khi có loại đơn giá; đăng kiểm hết hạn chỉ CẢNH BÁO — gán ghe vẫn được; cảnh báo có trong /alerts.
func TestLogisticsL07L08VesselRegisterAndExpiry(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	r := d.post("/v1/logistics/vessels", map[string]any{"code": " ct 12 345 ", "kind": "ghe", "straw_payload_tons": "90",
		"registration_expiry": f.clk.Now().AddDate(0, 0, -3).Format("2006-01-02"), "rate_type": "per_ton", "rate_vnd": "50000"})
	mustStatus(t, r, http.StatusCreated, "thêm ghe")
	vessel := str(r.body, "vessel", "vessel_id")
	if str(r.body, "vessel", "code") != "CT12345" || str(r.body, "vessel", "version") != "1" ||
		str(r.body, "vessel", "registration_status") != domain.ExpiryExpired || str(r.body, "vessel", "warning") != domain.FlagRegistrationExpire {
		t.Fatalf("ghe: %s", r.raw)
	}
	mustCode(t, d.post("/v1/logistics/vessels", map[string]any{"code": "CT12345", "kind": "ghe"}), http.StatusConflict,
		"logistics.vessel_code_taken", "trùng số hiệu")
	mustStatus(t, d.post("/v1/logistics/vessels", map[string]any{"code": "CT999", "kind": "ghe", "rate_type": "per_ton"}),
		http.StatusUnprocessableEntity, "loại đơn giá không có đơn giá")
	mustCode(t, d.post("/v1/logistics/vessels/"+vessel+"/versions", map[string]any{"code": "CT12345", "kind": "ghe", "expected_version": 5,
		"reason": "Gia hạn đăng kiểm tới năm sau"}), http.StatusConflict, "logistics.vessel_version_stale", "phiên bản cũ")
	// Ghe hết hạn vẫn gán được — chỉ cảnh báo (GH-02).
	trip := f.createTrip(d, "waterway", ids.New(), "80000")
	a := d.post("/v1/logistics/trips/"+trip+"/vehicle", map[string]any{"vehicle_ref": vessel})
	mustStatus(t, a, http.StatusCreated, "gán ghe hết hạn")
	if !hasFlag(a.body, domain.FlagRegistrationExpire, "warnings") || hasFlag(a.body, domain.FlagNoVehicle, "warnings") {
		t.Fatalf("cảnh báo gán ghe: %s", a.raw)
	}
	al := d.get("/v1/logistics/alerts")
	mustStatus(t, al, http.StatusOK, "cảnh báo")
	if len(arr(al.body, "vessels")) != 1 {
		t.Fatalf("quét đăng kiểm: %s", al.raw)
	}
	up := d.post("/v1/logistics/vessels/"+vessel+"/versions", map[string]any{"code": "CT12345", "kind": "ghe", "expected_version": 1,
		"straw_payload_tons": "90", "registration_expiry": f.clk.Now().AddDate(1, 0, 0).Format("2006-01-02"), "rate_type": "per_ton",
		"rate_vnd": "50000", "reason": "Gia hạn đăng kiểm tới năm sau"})
	mustStatus(t, up, http.StatusCreated, "sửa ghe")
	if str(up.body, "vessel", "version") != "2" || str(up.body, "vessel", "registration_status") != domain.ExpiryValid {
		t.Fatalf("phiên bản 2: %s", up.raw)
	}
}

// T-1 / proto FM-05: mỗi lượt xuống ghe (boat_load) sinh MỘT chuyến đường thủy planned chưa gán ghe, tấn
// ước = cuộn × kg/cuộn; công đoạn khác bỏ qua; giao lặp (R-61) không lập thêm; bản bù đếm cuộn sửa tấn ước.
func TestLogisticsT1FM05TripFromBoatLoad(t *testing.T) {
	f := newFixture(t)
	lot := ids.New()
	at := f.clk.Now().Add(-2 * time.Hour)
	rc := f.boatLoad(f.tenant, lot, 4400, nil, at)
	ts := f.tripsForLot(f.tenant, lot)
	if len(ts) != 1 {
		t.Fatalf("muốn 1 chuyến, có %d", len(ts))
	}
	tr := ts[0]
	if tr.Status != port.TripPlanned || tr.Mode != port.ModeWaterway || tr.VehicleRef != nil || tr.PlannedKg == nil ||
		tr.PlannedKg.String() != "96800.000" || tr.CollectionOrderRef == nil || *tr.CollectionOrderRef != lot {
		t.Fatalf("chuyến FM-05: %+v", tr)
	}
	g := f.dispatcher().get("/v1/logistics/trips/" + tr.TripID.String())
	if !hasFlag(g.body, domain.FlagNoVehicle, "trip", "flags") || str(g.body, "trip", "source_ref") != rc.RollCountID.String() {
		t.Fatalf("cờ FM-05: %s", g.raw)
	}
	// Giao lặp cùng sự kiện (inbox) và phát lại với event_id khác (idem theo roll_count_id): không lập thêm.
	f.deliver(f.tenant, port.RollCountConsumer, f.cons.HandleRollCount, "mkg.fieldops.roll_count.recorded", rc, ids.New())
	// Công đoạn khác boat_load: bỏ qua.
	other := rc
	other.RollCountID, other.Stage = ids.New(), "gather"
	f.deliver(f.tenant, port.RollCountConsumer, f.cons.HandleRollCount, "mkg.fieldops.roll_count.recorded", other, ids.New())
	if n := len(f.tripsForLot(f.tenant, lot)); n != 1 {
		t.Fatalf("giao lặp / công đoạn khác lập thêm chuyến: %d", n)
	}
	// Bản bù đếm cuộn → tấn ước mới trên cùng chuyến (revised), cờ rolls_corrected.
	f.boatLoad(f.tenant, lot, 4000, &rc.RollCountID, at.Add(time.Minute))
	ts = f.tripsForLot(f.tenant, lot)
	if len(ts) != 1 || ts[0].PlannedKg.String() != "88000.000" {
		t.Fatalf("bản bù đếm cuộn: %+v", ts)
	}
	// Tách lệnh gom khác: một lượt xuống ghe nữa → chuyến thứ hai.
	f.boatLoad(f.tenant, ids.New(), 100, nil, at)
	if l := f.dispatcher().get("/v1/logistics/trips?status=planned"); len(arr(l.body, "items")) != 2 {
		t.Fatalf("mỗi lượt xuống ghe một chuyến: %s", l.raw)
	}
}

func parse(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// L-09 + L-12 (proto plant weighing, GH-03): phiếu cân HUB_IN đóng chuyến với tấn thật; cước = đơn giá thuê
// ghe per_ton; lệch > 5 % so với tấn ước → cờ; phát mkg.logistics.trip.completed NỘI BỘ (outbox kind internal)
// kèm chặng + ảnh chụp định tuyến; phiếu bù → bản bù supersedes_id, phát lại có supersedes_id.
func TestLogisticsInternalEventTripCompletedOnWeighTicket(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	lot := ids.New()
	f.boatLoad(f.tenant, lot, 4400, nil, f.clk.Now().Add(-3*time.Hour))
	tr := f.tripsForLot(f.tenant, lot)[0]
	trip := tr.TripID.String()
	v := d.post("/v1/logistics/vessels", map[string]any{"code": "CT-0001", "kind": "ghe", "straw_payload_tons": "95",
		"rate_type": "per_ton", "rate_vnd": "50000"})
	mustStatus(t, v, http.StatusCreated, "thêm ghe")
	mustStatus(t, d.post("/v1/logistics/trips/"+trip+"/vehicle", map[string]any{"vehicle_ref": str(v.body, "vessel", "vessel_id")}),
		http.StatusCreated, "gán ghe")
	mustStatus(t, d.post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{waterLeg()}}), http.StatusCreated, "chặng")
	t1 := f.weigh(f.tenant, lot, "88000", nil)
	// Giao lặp phiếu: không đóng hai lần.
	f.deliver(f.tenant, port.WeighTicketConsumer, f.cons.HandleWeighTicket, "mkg.measurement.weigh_ticket.recorded", t1, ids.New())
	g := d.get("/v1/logistics/trips/" + trip)
	if str(g.body, "trip", "status") != "completed" || str(g.body, "trip", "actual_kg") != "88000.000" ||
		str(g.body, "trip", "actual_cost") != "4400000.00" || str(g.body, "trip", "cost_source") != "vessel_rate" ||
		str(g.body, "trip", "ticket_ref") != t1.TicketID.String() || !hasFlag(g.body, domain.FlagWeighVariance, "trip", "flags") {
		t.Fatalf("chuyến sau cân: %s", g.raw)
	}
	evs := f.completedEvents(f.tenant)
	if len(evs) != 1 {
		t.Fatalf("muốn 1 trip.completed, có %d", len(evs))
	}
	p := evs[0].Payload
	if evs[0].Kind != "internal" || p.TripID != tr.TripID || p.ActualKg.String() != "88000.000" || p.ActualCost.String() != "4400000.00" ||
		p.CostSource != port.CostByVesselRate || p.Mode != port.ModeWaterway || len(p.Legs) != 1 || p.Legs[0].RouteSnapshotRef == nil ||
		p.Legs[0].NetworkVersion == nil || *p.Legs[0].NetworkVersion != 3 || p.Legs[0].DistanceKm != "42.500" || len(p.Cargo) != 1 ||
		p.Cargo[0].TicketRef == nil || *p.Cargo[0].TicketRef != t1.TicketID || p.Vessel == nil || p.Vessel.Code != "CT-0001" ||
		p.CollectionOrderRef == nil || *p.CollectionOrderRef != lot || p.SupersedesID != nil || p.RuleVersion != domain.RuleTripCost {
		t.Fatalf("payload trip.completed: %s", evs[0].Raw)
	}
	// Phiếu bù → bản bù của lần hoàn tất (sổ chỉ ghi thêm), phát lại có supersedes_id.
	f.weigh(f.tenant, lot, "88500", &t1.TicketID)
	evs = f.completedEvents(f.tenant)
	if len(evs) != 2 || evs[1].Payload.SupersedesID == nil || evs[1].Payload.ActualKg.String() != "88500.000" ||
		evs[1].Payload.ActualCost.String() != "4425000.00" {
		t.Fatalf("bản bù phiếu cân: %d sự kiện", len(evs))
	}
	e := d.get("/v1/logistics/trips/" + trip + "/events")
	items := arr(e.body, "items")
	last := items[len(items)-1]
	if str(last, "kind") != "completed" || str(last, "supersedes_id") != evs[1].Payload.SupersedesID.String() ||
		str(last, "correction_reason") == "" {
		t.Fatalf("sổ chuyến: %s", e.raw)
	}
}

// L-10 (proto FM-05, #51): tấn ước > 1,1 × tải rơm thực chở → cảnh báo (không chặn); xuống ghe quá 48 giờ
// chưa cân → cảnh báo vận hành.
func TestLogisticsL10OverloadAndUnweighedAlert(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	lot := ids.New()
	f.boatLoad(f.tenant, lot, 5000, nil, f.clk.Now().Add(-49*time.Hour)) // 110 t > 1,1 × 90 (tham số #51)
	trip := f.tripsForLot(f.tenant, lot)[0].TripID.String()
	v := d.post("/v1/logistics/vessels", map[string]any{"code": "ST-77", "kind": "ghe"})
	mustStatus(t, v, http.StatusCreated, "ghe không khai tải")
	a := d.post("/v1/logistics/trips/"+trip+"/vehicle", map[string]any{"vehicle_ref": str(v.body, "vessel", "vessel_id")})
	mustStatus(t, a, http.StatusCreated, "gán ghe quá tải")
	if !hasFlag(a.body, domain.FlagOverload, "warnings") {
		t.Fatalf("cảnh báo quá tải: %s", a.raw)
	}
	// Ghe ngoài danh mục: cảnh báo, không chặn.
	b := d.post("/v1/logistics/trips/"+trip+"/vehicle", map[string]any{"vehicle_ref": ids.New().String()})
	mustStatus(t, b, http.StatusCreated, "ghe ngoài danh mục")
	if !hasFlag(b.body, domain.FlagVesselUnregistered, "warnings") || hasFlag(b.body, domain.FlagOverload, "warnings") {
		t.Fatalf("cảnh báo ngoài danh mục: %s", b.raw)
	}
	f.boatLoad(f.tenant, ids.New(), 100, nil, f.clk.Now().Add(-time.Hour)) // chưa quá 48 giờ
	al := d.get("/v1/logistics/alerts")
	mustStatus(t, al, http.StatusOK, "cảnh báo")
	if xs := arr(al.body, "unweighed_trips"); len(xs) != 1 || str(xs[0], "trip_id") != trip || str(al.body, "unweighed_hours") != "48" {
		t.Fatalf("ghe chưa cân: %s", al.raw)
	}
}

// RB-1: sửa chuyến đã hoàn tất bằng BẢN GHI BÙ (supersedes_id + correction_reason), không UPDATE; bản cũ giữ
// nguyên; sổ chặn UPDATE/DELETE/TRUNCATE ở cả vai ứng dụng lẫn chủ bảng (sáu phép thử × mọi sổ mới).
func TestLogisticsCorrectionByCompensatingRecord(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	trip := f.createTrip(d, "road", ids.New(), "10000")
	mustStatus(t, d.post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{roadLeg("10.0452", "105.7469", "10.386", "105.4352")}}),
		http.StatusCreated, "chặng")
	c1 := d.post("/v1/logistics/trips/"+trip+"/completion", map[string]any{"actual_kg": "10000"})
	mustStatus(t, c1, http.StatusCreated, "hoàn tất")
	if str(c1.body, "trip", "cost_source") != "param_rate" || str(c1.body, "event", "param_version") != port.ParamRoadRatePerTonKm+":default" {
		t.Fatalf("cước theo tham số: %s", c1.raw)
	}
	first := str(c1.body, "event", "event_id")
	mustCode(t, d.post("/v1/logistics/trips/"+trip+"/completion", map[string]any{"actual_kg": "9800"}), http.StatusUnprocessableEntity,
		"logistics.correction_reason_required", "sửa không lý do")
	mustStatus(t, d.post("/v1/logistics/trips/"+trip+"/completion", map[string]any{"actual_kg": "9800", "actual_cost": "1500000",
		"correction_reason": "Nhập nhầm khối lượng, sửa theo phiếu giao"}), http.StatusUnprocessableEntity, "cước tay không chứng từ")
	c2 := d.post("/v1/logistics/trips/"+trip+"/completion", map[string]any{"actual_kg": "9800", "actual_cost": "1500000",
		"reason": "Hóa đơn cước số 0012345 của nhà xe", "correction_reason": "Nhập nhầm khối lượng, sửa theo phiếu giao"})
	mustStatus(t, c2, http.StatusCreated, "bản bù")
	if str(c2.body, "event", "supersedes_id") != first || str(c2.body, "trip", "actual_kg") != "9800.000" ||
		str(c2.body, "trip", "actual_cost") != "1500000.00" || str(c2.body, "trip", "cost_source") != "manual" {
		t.Fatalf("bản bù: %s", c2.raw)
	}
	var kg string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT actual_kg::text FROM mkg_logistics.trip_event WHERE id = $1", first).Scan(&kg)
	})
	if kg != "10000.000" {
		t.Fatalf("bản cũ phải giữ nguyên: %s", kg)
	}
	// Mọi sổ mới có dòng (trigger dòng của chủ bảng chỉ chạy khi có dòng).
	mustStatus(t, d.post("/v1/logistics/vessels", map[string]any{"code": "SO-1", "kind": "sa_lan"}), http.StatusCreated, "ghe")
	mustStatus(t, d.post("/v1/logistics/trips/"+f.createTrip(d, "waterway", ids.New(), "1000")+"/legs", map[string]any{"legs": []any{waterLeg()}}),
		http.StatusCreated, "chặng thủy")
	mustStatus(t, d.post("/v1/logistics/params/"+port.ParamExpiryWarnDays+"/versions", map[string]any{"value": "45",
		"reason": "Chủ ghe cần thêm thời gian gia hạn"}), http.StatusCreated, "tham số")
	ctx := context.Background()
	for _, pool := range []*pgxpool.Pool{f.pool, f.env.Pool(t, migrations.MKG, dbadmin.Migrator)} {
		for _, tb := range []string{"trip_event", "leg_event", "route_snapshot", "vessel_event", "param_version"} {
			for _, q := range []string{
				"UPDATE mkg_logistics." + tb + " SET rule_version = 'x'",
				"DELETE FROM mkg_logistics." + tb,
				"TRUNCATE mkg_logistics." + tb,
			} {
				if err := tenant.WithTenantTx(ctx, pool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); err == nil {
					t.Fatalf("sổ phải chặn: %s", q)
				}
			}
		}
	}
}

// R-56: tham số cước — dispatcher đề xuất ≠ chief_accountant duyệt (app/ + CHECK DB); tham số mới áp theo
// thời điểm sự việc vào cước kế hoạch, ghim phiên bản.
func TestLogisticsParamSoD(t *testing.T) {
	f := newFixture(t)
	both := f.as(f.tenant, port.RoleDispatcher, "chief_accountant")
	p := both.post("/v1/logistics/params/"+port.ParamWaterwayRatePerTonKm+"/versions", map[string]any{"value": "1000",
		"reason": "Giá cước ghe quý 4 theo khảo sát chủ ghe", "provisional": true})
	mustStatus(t, p, http.StatusCreated, "đề xuất")
	ver := str(p.body, "param", "version_id")
	path := "/v1/logistics/params/" + port.ParamWaterwayRatePerTonKm + "/versions/" + ver + "/decision"
	mustCode(t, both.post(path, map[string]any{"decision": "approved"}), http.StatusForbidden, "self_approval_forbidden", "tự duyệt")
	mustStatus(t, f.dispatcher().post(path, map[string]any{"decision": "approved"}), http.StatusForbidden, "điều phối duyệt")
	mustStatus(t, f.chief().post(path, map[string]any{"decision": "approved"}), http.StatusCreated, "kế toán trưởng duyệt")
	mustCode(t, f.chief().post(path, map[string]any{"decision": "approved"}), http.StatusConflict, "logistics.param_decided", "duyệt lại")
	g := f.dispatcher().get("/v1/logistics/params/" + port.ParamWaterwayRatePerTonKm)
	if str(g.body, "effective", "value") != "1000" || str(g.body, "effective", "param_version") != port.ParamWaterwayRatePerTonKm+":v1" {
		t.Fatalf("tham số hiệu lực: %s", g.raw)
	}
	trip := f.createTrip(f.dispatcher(), "waterway", ids.New(), "10000")
	r := f.dispatcher().post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{waterLeg()}})
	mustStatus(t, r, http.StatusCreated, "chặng")
	if l := arr(r.body, "trip", "legs")[0]; str(l, "rate_vnd_per_t_km") != "1000.0000" || str(l, "planned_cost") != "425000.00" ||
		str(l, "param_version") != port.ParamWaterwayRatePerTonKm+":v1" {
		t.Fatalf("cước kế hoạch theo tham số mới: %v", l)
	}
	mustStatus(t, both.post("/v1/logistics/params/logistics.khong_co/versions", map[string]any{"value": "1", "reason": "Khóa không tồn tại thử"}),
		http.StatusNotFound, "khóa lạ")
	// DB: người duyệt trùng người đề xuất bị CHECK chặn (lớp hai).
	f.mustFail(f.pool, "R-56 CHECK", `INSERT INTO mkg_logistics.param_version (id, tenant_id, stream_key, param_key, version_id, version_seq,
	    kind, value, effective_from, provisional, proposer_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'param:x', 'logistics.rate.road_vnd_per_t_km', $3, 9, 'approved', 1, current_date, true, $3, now(), $3, 'k-sod', 'x')`,
		ids.New(), f.tenant, ids.New())
}

// L-01 (R-22): dựng lại trip_current thẳng từ sổ = bảng chiếu sống (mọi trạng thái, bản bù, chặng).
func TestLogisticsL01ProjectionRebuildEqualsLive(t *testing.T) {
	f := newFixture(t)
	d, drv := f.dispatcher(), f.driver()
	lot := ids.New()
	f.boatLoad(f.tenant, lot, 4400, nil, f.clk.Now().Add(-5*time.Hour))
	fm05 := f.tripsForLot(f.tenant, lot)[0].TripID.String()
	mustStatus(t, d.post("/v1/logistics/trips/"+fm05+"/legs", map[string]any{"legs": []any{waterLeg()}}), http.StatusCreated, "chặng")
	f.weigh(f.tenant, lot, "90000", nil)
	a := f.createTrip(d, "road", ids.New(), "12000")
	mustStatus(t, d.post("/v1/logistics/trips/"+a+"/legs", map[string]any{"legs": []any{roadLeg("10.0452", "105.7469", "10.386", "105.4352")}}),
		http.StatusCreated, "chặng a")
	mustStatus(t, d.post("/v1/logistics/trips/"+a+"/legs", map[string]any{"legs": []any{roadLeg("10.0452", "105.7469", "10.5", "105.5")}}),
		http.StatusCreated, "chặng a lần 2")
	mustStatus(t, drv.post("/v1/logistics/trips/"+a+"/milestones", map[string]any{"kind": "departed", "event_time": f.ts(-time.Hour),
		"lat": "10.0452", "lng": "105.7469"}), http.StatusCreated, "xuất phát")
	b := f.createTrip(d, "road", ids.New(), "3000")
	mustStatus(t, d.post("/v1/logistics/trips/"+b+"/cancel", map[string]any{"reason": "Khách đổi lịch nhận hàng"}), http.StatusCreated, "hủy")
	c := f.createTrip(d, "road", ids.New(), "3000")
	mustStatus(t, d.post("/v1/logistics/trips/"+c+"/completion", map[string]any{"actual_kg": "2900"}), http.StatusCreated, "hoàn tất c")
	mustStatus(t, d.post("/v1/logistics/trips/"+c+"/completion", map[string]any{"actual_kg": "2950",
		"correction_reason": "Sửa theo phiếu giao nhận có chữ ký"}), http.StatusCreated, "bản bù c")

	snap := func() []store.Trip {
		var out []store.Trip
		f.inTenant(func(tx pgx.Tx) error {
			var err error
			out, err = store.AllTrips(context.Background(), tx)
			return err
		})
		return out
	}
	live := snap()
	r := d.post("/v1/logistics/projections/rebuild", nil)
	mustStatus(t, r, http.StatusOK, "dựng lại")
	if str(r.body, "trip_current") != "4" {
		t.Fatalf("dựng lại: %s", r.raw)
	}
	rebuilt := snap()
	if len(live) != 4 || !reflect.DeepEqual(live, rebuilt) {
		for i := range live {
			if i < len(rebuilt) && !reflect.DeepEqual(live[i], rebuilt[i]) {
				t.Errorf("khác ở %s:\nsống    %+v\ndựng lại %+v", live[i].Code, live[i], rebuilt[i])
			}
		}
		t.Fatalf("dựng lại ≠ sống (%d / %d dòng)", len(live), len(rebuilt))
	}
}

// Cách ly tenant (RB-2): tenant khác không thấy chuyến / ghe / ảnh chụp; port.Trips theo tenant tường minh.
func TestLogisticsTenantIsolation(t *testing.T) {
	f := newFixture(t)
	trip := f.createTrip(f.dispatcher(), "waterway", ids.New(), "5000")
	mustStatus(t, f.dispatcher().post("/v1/logistics/trips/"+trip+"/legs", map[string]any{"legs": []any{waterLeg()}}), http.StatusCreated, "chặng")
	other := f.as(ids.New(), port.RoleDispatcher)
	mustCode(t, other.get("/v1/logistics/trips/"+trip), http.StatusNotFound, "logistics.trip_not_found", "tenant khác đọc chuyến")
	mustCode(t, other.post("/v1/logistics/trips/"+trip+"/cancel", map[string]any{"reason": "Hủy chuyến của tenant khác"}),
		http.StatusNotFound, "logistics.trip_not_found", "tenant khác hủy")
	if l := other.get("/v1/logistics/trips"); len(arr(l.body, "items")) != 0 {
		t.Fatalf("tenant khác liệt kê: %s", l.raw)
	}
	id := parse(t, trip)
	if _, err := f.svc.Trip(context.Background(), other.tid, id); err == nil {
		t.Fatal("port.Trips tenant khác phải không thấy")
	}
	if _, err := f.svc.Trip(context.Background(), f.tenant, id); err != nil {
		t.Fatal(err)
	}
}

// port.Trips.Completed: ảnh chụp đầu vào bất biến cho đóng kỳ costing — chỉ bản ghi recorded_at < asOf,
// bản bù thay bản cũ, phân trang theo con trỏ.
func TestLogisticsTripsPortCompletedAsOf(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	ctx := context.Background()
	trips := []string{f.createTrip(d, "road", ids.New(), "1000"), f.createTrip(d, "road", ids.New(), "2000")}
	for _, tr := range trips {
		mustStatus(t, d.post("/v1/logistics/trips/"+tr+"/completion", map[string]any{"actual_kg": "1000"}), http.StatusCreated, "hoàn tất")
	}
	from, to := f.clk.Now().Add(-24*time.Hour), f.clk.Now().Add(24*time.Hour)
	var mid time.Time
	f.inTenant(func(tx pgx.Tx) error { return tx.QueryRow(ctx, "SELECT now()").Scan(&mid) })
	mustStatus(t, d.post("/v1/logistics/trips/"+trips[0]+"/completion", map[string]any{"actual_kg": "1100",
		"correction_reason": "Sửa theo biên bản giao nhận cuối ngày"}), http.StatusCreated, "bản bù")
	page, err := f.svc.Completed(ctx, f.tenant, from, to, mid, "", 1)
	if err != nil || len(page.Items) != 1 || page.Next == "" {
		t.Fatalf("trang 1: %+v %v", page, err)
	}
	p2, err := f.svc.Completed(ctx, f.tenant, from, to, mid, page.Next, 1)
	if err != nil || len(p2.Items) != 1 || p2.Items[0].TripID == page.Items[0].TripID {
		t.Fatalf("trang 2: %+v %v", p2, err)
	}
	for _, it := range append(page.Items, p2.Items...) {
		if it.ActualKg.String() != "1000.000" {
			t.Fatalf("asOf trước bản bù phải thấy bản cũ: %s", it.ActualKg)
		}
	}
	now, err := f.svc.Completed(ctx, f.tenant, from, to, f.clk.Now().Add(time.Hour), "", 10)
	if err != nil || len(now.Items) != 2 {
		t.Fatalf("hiện tại: %+v %v", now, err)
	}
	found := false
	for _, it := range now.Items {
		if it.TripID.String() == trips[0] && it.ActualKg.String() == "1100.000" {
			found = true
		}
	}
	if !found {
		t.Fatal("asOf sau bản bù phải thấy bản mới")
	}
}
