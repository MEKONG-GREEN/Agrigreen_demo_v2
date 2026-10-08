package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// G3-2 consumer mkg.fieldops.weigh_ticket (thay đường làm mới tay): phiếu HUB_IN của lô
// gom → bảng đệm học kg/cuộn; collection_order_ref thắng lot_ref; phiếu bù thay phiếu gốc,
// phiếu gốc tới sau bị bỏ (stale); giao lặp → như một lần (R-61). kg/cuộn học có mốc
// thời điểm và bỏ chính lô (tất định cho đối soát RB-6).
func TestWeighTicketConsumerLearnsKgPerRoll(t *testing.T) {
	f := newFixture(t, nil)
	order := f.approvedOrder()
	oid := uuid.MustParse(order)
	col := f.collector()
	mustStatus(t, f.count(col, order, f.plots[0], "bale", 4400, nil), http.StatusCreated, "bale")
	mustStatus(t, f.count(col, order, f.plots[0], "gather", 4400, nil), http.StatusCreated, "gather")
	mustStatus(t, f.count(col, order, f.plots[0], "boat_load", 4400, nil), http.StatusCreated, "boat_load")
	h := fieldops.NewConsumers(f.clk).WeighTicket()
	deliver := func(eventID uuid.UUID, ev mport.WeighTicketRecordedV1) bool {
		t.Helper()
		raw, _ := json.Marshal(ev)
		ok, err := inbox.Process(context.Background(), f.pool, "mkg.fieldops.weigh_ticket", outbox.Envelope{EventID: eventID,
			EventType: mport.EventWeighTicketRecorded, SchemaVersion: 1, TenantID: f.tenant, OccurredAt: f.clk.Now(), Producer: "mkg/measurement",
			Payload: raw}, h)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	weighed := func() (kg string, ticket *uuid.UUID) {
		f.inTenant(func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT weighed_kg::text, ticket_ref FROM mkg_fieldops.weighed_loading_current
			    WHERE order_id = $1`, oid).Scan(&kg, &ticket)
		})
		return kg, ticket
	}
	lot := ids.New() // lô cân khác mã lệnh gom → liên kết tường minh qua collection_order_ref
	orig := mport.WeighTicketRecordedV1{TicketID: ids.New(), LotRef: lot, Point: mport.PointHubIn, Route: mport.RouteHub, Source: mport.SourceEdge,
		MassKg: mass.MustKg("96800"), EventTime: f.clk.Now().Add(-2 * time.Hour), CollectionOrderRef: &oid}
	// HUB_OUT và CUSTOMER_IN tuyến HUB không phải lần cân đầu → bỏ qua.
	out := orig
	out.TicketID, out.Point = ids.New(), mport.PointHubOut
	deliver(ids.New(), out)
	ev := ids.New()
	if !deliver(ev, orig) || deliver(ev, orig) {
		t.Fatal("inbox: một lần")
	}
	if kg, tk := weighed(); kg != "96800.000" || tk == nil || *tk != orig.TicketID {
		t.Fatalf("bảng đệm: %s %v", kg, tk)
	}
	// Phiếu bù tới trước bản sao gốc phát lại với event_id khác → gốc bị bỏ.
	fix := orig
	fix.TicketID, fix.MassKg, fix.SupersedesID = ids.New(), mass.MustKg("95000"), &orig.TicketID
	deliver(ids.New(), fix)
	deliver(ids.New(), orig)
	if kg, tk := weighed(); kg != "95000.000" || *tk != fix.TicketID {
		t.Fatalf("phiếu bù: %s %v", kg, tk)
	}
	// Học kg/cuộn: 95 000 / 4 400 = 21,6 (HTX); mốc trước lần cân → không có mẫu → cấu hình/mặc định 20; bỏ chính lô → 20.
	kp := fieldops.New(pgtest.Get(t).Pool(t, migrations.MKG, dbadmin.AppInternal), f.clk, fieldops.Options{}).KgPerRoll()
	if e, err := kp.KgPerRollAsOf(context.Background(), f.tenant, f.htx, f.clk.Now(), ids.New()); err != nil || e.Kg.String() != "21.600" || e.Source != "htx" {
		t.Fatalf("học HTX: %+v %v", e, err)
	}
	if e, _ := kp.KgPerRollAsOf(context.Background(), f.tenant, f.htx, f.clk.Now().Add(-3*time.Hour), ids.New()); e.Source != "default" || e.Kg.String() != "20.000" {
		t.Fatalf("mốc trước lần cân: %+v", e)
	}
	if e, _ := kp.KgPerRollAsOf(context.Background(), f.tenant, f.htx, f.clk.Now(), oid); e.Source != "default" {
		t.Fatalf("bỏ chính lô: %+v", e)
	}
	// Sửa số xếp ghe sau khi cân → bảng đệm theo số mới.
	c := f.supervisor().get("/v1/fieldops/orders/" + order + "/roll-counts")
	var boat string
	for _, x := range arr(c.body, "counts") {
		if str(x, "stage") == "boat_load" {
			boat = str(x, "id")
		}
	}
	mustStatus(t, col.post("/v1/fieldops/roll-counts/"+boat+"/corrections", map[string]any{"rolls": 4000,
		"discrepancy_reason": "400 cuộn chuyển ghe sau", "correction_reason": "Đếm lại khi xếp ghe", "event_time": f.ts(-time.Minute)}),
		http.StatusCreated, "bù boat_load")
	var rolls int
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT rolls FROM mkg_fieldops.weighed_loading_current WHERE order_id = $1", oid).Scan(&rolls)
	})
	if rolls != 4000 {
		t.Fatalf("cuộn bảng đệm sau bù: %d", rolls)
	}
	// RollTotals: số hiệu lực + HTXConfirmed (chưa HTX xác nhận → false).
	lr, err := fieldops.New(pgtest.Get(t).Pool(t, migrations.MKG, dbadmin.AppInternal), f.clk, fieldops.Options{}).RollTotals().LotRolls(context.Background(), f.tenant, oid)
	if err != nil || lr.Rolls != 4000 || lr.Stage != "boat_load" || lr.HTXConfirmed {
		t.Fatalf("LotRolls: %+v %v", lr, err)
	}
}

// G3-2 dấu GPS của app hiện trường (gps + gps_status) — tùy chọn, không bao giờ chặn lưu,
// qua cả POST roll-counts và sync push; đọc lại ở GET roll-counts (không float, R-10).
func TestRollCountGPSOptionalNeverBlocks(t *testing.T) {
	f := newFixture(t, nil)
	order := f.approvedOrder()
	col := f.collector()
	mustStatus(t, f.count(col, order, f.plots[0], "bale", 100, map[string]any{
		"gps": map[string]any{"lat": 10.4520001, "lng": 105.341, "accuracy_m": 6.5}, "gps_status": "ok"}), http.StatusCreated, "gps ok")
	// Tọa độ hỏng → vẫn lưu, gps_status = invalid.
	mustStatus(t, f.count(col, order, f.plots[1], "bale", 50, map[string]any{"gps": map[string]any{"lat": 123, "lng": 105}, "gps_status": "ok"}),
		http.StatusCreated, "gps hỏng không chặn")
	col.device = ids.New().String()
	push := col.post("/v1/fieldops/sync/push", map[string]any{"device_id": col.device, "records": []any{
		map[string]any{"id": ids.New().String(), "idem_key": "dev:" + col.device + ":1", "entity": "roll_count", "op": "create",
			"event_time": f.ts(-time.Hour), "payload": map[string]any{"order_id": order, "plot_ref": f.plots[0].String(), "stage": "gather",
				"rolls": 100, "gps_status": "denied"}},
		map[string]any{"id": ids.New().String(), "idem_key": "dev:" + col.device + ":2", "entity": "roll_count", "op": "create",
			"event_time": f.ts(-time.Hour), "payload": map[string]any{"order_id": order, "plot_ref": f.plots[1].String(), "stage": "gather",
				"rolls": 50, "gps": map[string]any{"lat": 9.78, "lon": 105.47}, "gps_status": "weak"}},
	}})
	mustStatus(t, push, http.StatusOK, "push")
	for _, r := range arr(push.body, "results") {
		if str(r, "status") != "accepted" {
			t.Fatalf("push: %s", push.raw)
		}
	}
	c := f.supervisor().get("/v1/fieldops/orders/" + order + "/roll-counts")
	got := map[string]string{}
	for _, x := range arr(c.body, "counts") {
		g, _ := x["gps"].(map[string]any)
		got[str(x, "stage")+"/"+str(x, "rolls")] = str(x, "gps_status") + "|" + str(g, "lat") + "|" + str(g, "lng") + "|" + str(g, "accuracy_m")
	}
	want := map[string]string{"bale/100": "ok|10.4520001|105.3410000|6.50", "bale/50": "invalid|||", "gather/100": "denied|||",
		"gather/50": "weak|9.7800000|105.4700000|"}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: %q (muốn %q) — %s", k, got[k], v, c.raw)
		}
	}
}
