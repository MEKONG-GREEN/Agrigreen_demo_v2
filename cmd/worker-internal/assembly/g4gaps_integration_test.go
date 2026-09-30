package assembly

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func getJSON(t *testing.T, base, path, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return r.StatusCode, m
}

func items(m map[string]any) []map[string]any {
	xs, _ := m["items"].([]any)
	out := make([]map[string]any, 0, len(xs))
	for _, x := range xs {
		o, _ := x.(map[string]any)
		out = append(out, o)
	}
	return out
}

// milestoneCount đếm sự kiện milestone_recorded (internal) của tenant trên outbox.
func (w *wired) milestoneCount(tid uuid.UUID) int {
	w.t.Helper()
	p, _ := w.pool(logistics.DBRole)
	var n int
	if err := tenant.WithTenantTx(context.Background(), p, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND kind = 'internal'`,
			logport.EventTripMilestoneRecorded).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// proto FM-05 (T-1, G4): "thông báo hàng đến (cho_den → da_den) mang mã chuyến". Chuỗi THẬT qua sổ Consumers của
// worker-internal: lượt xuống ghe → chuyến planned (consumer logistics) → điều phối lập chặng tới hub A → xuất phát →
// mkg.logistics.trip.milestone_recorded (outbox internal, không cước) → consumer mkg.hub.trip_milestone → hub A có
// thông báo expected mang mã chuyến; đổi đích khi đang chạy → hub B expected; đến → arrived (giao lặp không ghi thêm);
// phiếu cân hoàn tất sau đó không đổi; chuyến khác xuất phát rồi hủy → cancelled; chuyến chưa có đích là cơ sở không
// phát mốc; GET /v1/hub/arrivals lọc theo cơ sở / trạng thái, phân trang, cách ly tenant; dựng lại = sống (R-22).
func TestLogisticsTripMilestoneToHubArrival_FM05(t *testing.T) {
	env := pgtest.Get(t)
	w := &wired{t: t, env: env, pools: map[string]*pgxpool.Pool{}, clk: clock.NewFixed(time.Now().UTC().Truncate(time.Second))}
	ctx := context.Background()
	tid := ids.New()
	iss := authztest.New(t)
	sign := func(tenantID uuid.UUID, roles ...string) string {
		return iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tenantID, Roles: roles, MFA: true})
	}
	logPool, _ := w.pool(logistics.DBRole)
	hubPool, _ := w.pool(hub.DBRole)
	logURL := server(t, iss, logPool, false, logistics.New(logPool, w.clk, logistics.Options{}).RegisterApp)
	hubMod := hub.New(hubPool, w.clk)
	hubURL := server(t, iss, hubPool, false, hubMod.RegisterApp)
	disp := sign(tid, logport.RoleDispatcher)
	hubOp := sign(tid, hubport.RoleHubOperator)

	boatLoad := func(rolls int, vessel *string) (uuid.UUID, uuid.UUID, string) {
		lot := ids.New()
		w.deliver(tid, logport.RollCountConsumer, fieldport.EventRollCountRecorded, fieldport.RollCountRecordedV1{
			RollCountID: ids.New(), LotRef: lot, PlotRef: ids.New(), HTXRef: ids.New(), Rolls: rolls, Stage: fieldport.StageBoatLoad,
			RuleVersion: "fieldops.count.v1", EventTime: w.clk.Now().Add(-2 * time.Hour), VesselCode: vessel})
		var trip uuid.UUID
		var code string
		if err := tenant.WithTenantTx(ctx, logPool, tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT trip_id, code FROM mkg_logistics.trip_current WHERE collection_order_ref = $1`, lot).Scan(&trip, &code)
		}); err != nil {
			t.Fatalf("FM-05: chưa có chuyến planned từ lượt xuống ghe: %v", err)
		}
		return lot, trip, code
	}
	toFacility := func(trip uuid.UUID, facility uuid.UUID) {
		f := facility.String()
		st, m := post(t, logURL, "/v1/logistics/trips/"+trip.String()+"/legs", disp, map[string]any{"legs": []any{map[string]any{
			"mode": "waterway", "from": map[string]any{"kind": "point", "lat": "10.0452", "lng": "105.7469"},
			"to": map[string]any{"kind": "facility", "ref": f, "lat": "10.386", "lng": "105.4352"}, "manual_km": "48.5",
			"manual_reason": "Điều phối đo theo kênh Xà No (kiểm thử)"}}})
		if st != http.StatusCreated {
			t.Fatalf("lập chặng: %d %v", st, m)
		}
	}
	milestone := func(trip uuid.UUID, kind string) {
		st, m := post(t, logURL, "/v1/logistics/trips/"+trip.String()+"/milestones", disp, map[string]any{"kind": kind,
			"event_time": w.clk.Now().Add(-time.Minute).Format(time.RFC3339)})
		if st != http.StatusCreated {
			t.Fatalf("mốc %s: %d %v", kind, st, m)
		}
	}
	forward := func() {
		w.deliver(tid, hubport.TripMilestoneConsumer, logport.EventTripMilestoneRecorded,
			w.outboxPayload(tid, logistics.DBRole, logport.EventTripMilestoneRecorded))
	}
	arrival := func(trip uuid.UUID) map[string]any {
		st, m := getJSON(t, hubURL, "/v1/hub/arrivals?limit=200", hubOp)
		if st != http.StatusOK {
			t.Fatalf("GET arrivals: %d %v", st, m)
		}
		for _, it := range items(m) {
			if it["trip_id"] == trip.String() {
				return it
			}
		}
		return nil
	}

	hubA, hubB := ids.New(), ids.New()
	vessel := "CT-01234"
	lot, trip, code := boatLoad(100, &vessel)
	// Chuyến chưa có đích là cơ sở → xuất phát không phát mốc (hub không biết gửi đâu).
	_, orphan, _ := boatLoad(40, nil)
	milestone(orphan, "departed")
	if n := w.milestoneCount(tid); n != 0 {
		t.Fatalf("chuyến không có đích cơ sở không được phát mốc: %d", n)
	}

	toFacility(trip, hubA)
	milestone(trip, "departed")
	var raw map[string]any
	if err := json.Unmarshal(w.outboxPayload(tid, logistics.DBRole, logport.EventTripMilestoneRecorded), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"actual_cost", "planned_cost", "driver_ref", "cost_source"} {
		if _, ok := raw[k]; ok {
			t.Fatalf("mốc chuyến không được mang %s (cước / tài xế ở lại logistics): %v", k, raw)
		}
	}
	if raw["trip_code"] != code || raw["milestone"] != "departed" || raw["facility_ref"] != hubA.String() {
		t.Fatalf("payload mốc: %v", raw)
	}
	forward()
	a := arrival(trip)
	if a == nil || a["status"] != hubport.ArrivalExpected || a["proto_status"] != "cho_den" || a["trip_code"] != code ||
		a["facility_ref"] != hubA.String() || a["planned_kg"] != "2000.000" {
		t.Fatalf("FM-05: hub A phải có thông báo chờ đến mang mã chuyến: %v", a)
	}
	if lots, _ := a["lot_refs"].([]any); len(lots) != 1 || lots[0] != lot.String() {
		t.Fatalf("lô trên chuyến: %v", a)
	}

	// Đổi đích khi đang chạy → hub B chờ hàng.
	toFacility(trip, hubB)
	forward()
	if a = arrival(trip); a["facility_ref"] != hubB.String() || a["status"] != hubport.ArrivalExpected {
		t.Fatalf("đổi đích: %v", a)
	}

	// Đến → da_den; giao lặp cùng sự kiện (phong bì khác) không ghi thêm.
	milestone(trip, "arrived")
	forward()
	forward()
	if a = arrival(trip); a["status"] != hubport.ArrivalArrived || a["proto_status"] != "da_den" || a["arrived_at"] == nil ||
		a["expected_since"] == nil {
		t.Fatalf("đã đến: %v", a)
	}
	var events int
	if err := tenant.WithTenantTx(ctx, hubPool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM mkg_hub.arrival_event WHERE trip_id = $1", trip).Scan(&events)
	}); err != nil || events != 3 {
		t.Fatalf("sổ thông báo: expected A, expected B, arrived = 3 dòng: %d %v", events, err)
	}
	// Phiếu cân đóng chuyến → mốc completed tới hub: đã arrived → không đổi.
	w.deliver(tid, logport.WeighTicketConsumer, measport.EventWeighTicketRecorded, measport.WeighTicketRecordedV1{TicketID: ids.New(),
		LotRef: lot, Point: measport.PointHubIn, Route: measport.RouteHub, Source: measport.SourceEdge, MassKg: mass.MustKg("2100"),
		EventTime: w.clk.Now(), CollectionOrderRef: &lot})
	forward()
	if err := tenant.WithTenantTx(ctx, hubPool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM mkg_hub.arrival_event WHERE trip_id = $1", trip).Scan(&events)
	}); err != nil || events != 3 {
		t.Fatalf("hoàn tất sau khi đã đến không ghi thêm: %d %v", events, err)
	}

	// Chuyến khác xuất phát rồi hủy → cancelled.
	_, trip2, _ := boatLoad(50, nil)
	toFacility(trip2, hubA)
	milestone(trip2, "departed")
	forward()
	if st, m := post(t, logURL, "/v1/logistics/trips/"+trip2.String()+"/cancel", disp, map[string]any{"reason": "Ghe hỏng máy giữa kênh"}); st != http.StatusCreated {
		t.Fatalf("hủy: %d %v", st, m)
	}
	forward()
	if a = arrival(trip2); a["status"] != hubport.ArrivalCancelled || a["cancelled_at"] == nil {
		t.Fatalf("hủy: %v", a)
	}

	// Lọc theo cơ sở / trạng thái, phân trang, 422, cách ly tenant.
	_, m := getJSON(t, hubURL, "/v1/hub/arrivals?facility_id="+hubB.String()+"&status=arrived", hubOp)
	if it := items(m); len(it) != 1 || it[0]["trip_code"] != code {
		t.Fatalf("lọc hub B arrived: %v", m)
	}
	_, m = getJSON(t, hubURL, "/v1/hub/arrivals?limit=1", hubOp)
	next, _ := m["next_cursor"].(string)
	if len(items(m)) != 1 || next == "" {
		t.Fatalf("trang 1: %v", m)
	}
	_, m2 := getJSON(t, hubURL, "/v1/hub/arrivals?limit=1&cursor="+next, hubOp)
	if it := items(m2); len(it) != 1 || it[0]["trip_id"] == items(m)[0]["trip_id"] {
		t.Fatalf("trang 2: %v", m2)
	}
	if st, _ := getJSON(t, hubURL, "/v1/hub/arrivals?status=da_den", hubOp); st != http.StatusUnprocessableEntity {
		t.Fatalf("status lạ phải 422: %d", st)
	}
	if st, m := getJSON(t, hubURL, "/v1/hub/arrivals", sign(ids.New(), hubport.RoleHubOperator)); st != http.StatusOK || len(items(m)) != 0 {
		t.Fatalf("RB-2: tenant khác không thấy: %d %v", st, m)
	}
	if st, _ := getJSON(t, hubURL, "/v1/hub/arrivals", disp); st != http.StatusForbidden {
		t.Fatalf("điều phối không có vai hub: %d", st)
	}

	// R-22: dựng lại arrival_current từ sổ = sống.
	dump := func() string {
		var s string
		if err := tenant.WithTenantTx(ctx, hubPool, tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT coalesce(string_agg((to_jsonb(x) - 'updated_at')::text, E'\n' ORDER BY trip_id), '')
			  FROM mkg_hub.arrival_current x`).Scan(&s)
		}); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := dump()
	admin := env.AdminPool(t, migrations.MKG)
	if _, err := admin.Exec(ctx, "UPDATE mkg_hub.arrival_current SET status = 'expected', arrived_at = NULL WHERE tenant_id = $1", tid); err != nil {
		t.Fatal(err)
	}
	n, err := hubMod.RebuildTenant(ctx, tid)
	if err != nil || n["arrival_current"] != 2 {
		t.Fatalf("dựng lại: %v %v", n, err)
	}
	if after := dump(); after != before {
		t.Fatalf("dựng lại ≠ sống:\n%s\n---\n%s", before, after)
	}
}
