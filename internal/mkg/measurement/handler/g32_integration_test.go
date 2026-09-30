package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// G3-2 ánh xạ lô: phiên cân mang collection_order_ref → liên kết tường minh lô cân ↔ lệnh
// gom; sự kiện nội bộ mang collection_order_ref; đối soát đọc số cuộn theo LỆNH GOM (RB-6,
// proto FM-10: 4 400 cuộn × 20 = 88 t vs 96,8 t → +10 % cờ). Một lệnh gom một lô; đổi cần lý do.
func TestCollectionLinkDrivesRollCheck(t *testing.T) {
	f := newFixture(t)
	din := f.approvedScale("SCALE-IN-G32", "HUB_IN")
	dout := f.approvedScale("SCALE-OUT-G32", "HUB_OUT")
	lot, order := ids.New(), ids.New()
	f.rolls.set(order, 4400, "20") // số cuộn theo lệnh gom, KHÔNG theo lô cân
	sup := f.supervisor()
	op := sup.app(http.MethodPost, "/v1/measurement/sessions", map[string]any{"lot_ref": lot.String(), "device_id": din.id, "epoch": din.epoch,
		"counter_from": 1, "collection_order_ref": order.String()})
	mustStatus(t, op, http.StatusCreated, "mở phiên có lệnh gom")
	g := sup.app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/collection", nil)
	if g.body["explicit"] != true || str(g.body, "collection_order_ref") != order.String() || str(g.body, "origin") != "session" {
		t.Fatalf("liên kết: %s", g.raw)
	}
	// Lô chưa liên kết: quy ước đồng nhất.
	other := ids.New()
	if g := sup.app(http.MethodGet, "/v1/measurement/lots/"+other.String()+"/collection", nil); g.body["explicit"] != false ||
		str(g.body, "collection_order_ref") != other.String() {
		t.Fatalf("đồng nhất: %s", g.raw)
	}
	// Lệnh gom đã thuộc lô khác → 409.
	if r := sup.app(http.MethodPost, "/v1/measurement/lots/"+other.String()+"/collection", map[string]any{"collection_order_ref": order.String()}); code(r) != "measurement.collection_link_conflict" {
		t.Fatalf("lệnh gom trùng: %d %s", r.status, r.raw)
	}
	tin, err := f.edgeTicket(din, lot, port.PointHubIn, "96800", 1, 10, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.edgeTicket(dout, lot, port.PointHubOut, "95348", 1, 10, nil, ""); err != nil {
		t.Fatal(err)
	}
	// Sự kiện nội bộ mang collection_order_ref.
	f.inTenant(func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(context.Background(), "SELECT payload FROM shared.outbox WHERE id = $1", tin.ID).Scan(&raw); err != nil {
			return err
		}
		var ev port.WeighTicketRecordedV1
		if err := json.Unmarshal(raw, &ev); err != nil || ev.CollectionOrderRef == nil || *ev.CollectionOrderRef != order {
			t.Fatalf("sự kiện: %s %v", raw, err)
		}
		return nil
	})
	c := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN", "mass_kg": "94871",
		"weighed_at": f.ts(-time.Minute), "evidence_ref": ids.New().String(), "reason": "Phiếu cân nhà máy khách (C-5 tạm)"})
	mustStatus(t, c, http.StatusCreated, "CUSTOMER_IN")
	roll := obj(obj(c.body, "reconciliation"), "roll_check")
	if str(roll, "status") != "flag" || str(roll, "variance_pct") != "10.00" || str(roll, "estimate_kg") != "88000.000" {
		t.Fatalf("cuộn ↔ khối lượng theo lệnh gom: %v", roll)
	}
	// Đổi lệnh gom của lô: thiếu lý do → 409; có lý do → bản bù.
	order2 := ids.New()
	if r := f.officer().app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/collection", map[string]any{"collection_order_ref": order2.String()}); code(r) != "measurement.collection_link_conflict" {
		t.Fatalf("đổi không lý do: %d %s", r.status, r.raw)
	}
	ch := f.officer().app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/collection", map[string]any{"collection_order_ref": order2.String(),
		"reason": "Ghi nhầm lệnh gom lúc mở phiên"})
	mustStatus(t, ch, http.StatusCreated, "đổi có lý do")
	if ch.body["supersedes_id"] == nil {
		t.Fatalf("bản bù: %s", ch.raw)
	}
	// Lệnh gom mới chưa có số cuộn → đối soát mới pending_rolls (không còn số của lệnh cũ).
	rs := f.officer().app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/reconciliations", nil)
	if str(obj(obj(rs.body, "latest"), "roll_check"), "status") != "pending_rolls" {
		t.Fatalf("sau đổi lệnh gom: %s", rs.raw)
	}
}

// G3-2 consumer mkg.measurement.roll_count: số cuộn tới SAU khi lô đủ phiếu → đối soát lại
// (pending_rolls → flag). Giao lặp (cùng event_id, hoặc id khác cùng roll_count_id) → như một lần (R-61).
func TestRollCountConsumerReconciles(t *testing.T) {
	f := newFixture(t)
	lot := ids.New() // quy ước đồng nhất: lô cân = lệnh gom
	sup := f.supervisor()
	c := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN", "route": "DIRECT",
		"mass_kg": "96800", "weighed_at": f.ts(-time.Minute), "evidence_ref": ids.New().String(), "reason": "Ghe thẳng nhà máy (proto seed)"})
	mustStatus(t, c, http.StatusCreated, "DIRECT")
	if s := str(obj(obj(c.body, "reconciliation"), "roll_check"), "status"); s != "pending_rolls" {
		t.Fatalf("chưa có cuộn: %s", s)
	}
	h := measurement.NewConsumers(f.clk, measurement.Deps{Rolls: f.rolls}).RollCount()
	f.rolls.set(lot, 4400, "20")
	rc := ids.New()
	raw, _ := json.Marshal(fieldport.RollCountRecordedV1{RollCountID: rc, LotRef: lot, PlotRef: ids.New(), HTXRef: ids.New(), Rolls: 4400,
		Stage: fieldport.StageBoatLoad, RuleVersion: "fieldops.count.v1", EventTime: f.clk.Now()})
	deliver := func(eventID uuid.UUID) bool {
		t.Helper()
		env := outbox.Envelope{EventID: eventID, EventType: fieldport.EventRollCountRecorded, SchemaVersion: 1, TenantID: f.tenant,
			OccurredAt: f.clk.Now(), Producer: "mkg/fieldops", Payload: raw}
		ok, err := inbox.Process(context.Background(), f.pool, "mkg.measurement.roll_count", env, h)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	ev := ids.New()
	if !deliver(ev) || deliver(ev) {
		t.Fatal("inbox: lần đầu xử lý, lần hai bỏ qua")
	}
	deliver(ids.New()) // phát lại với event_id khác: idem theo roll_count_id
	rs := f.officer().app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/reconciliations", nil)
	items := arr(rs.body, "items")
	if len(items) != 2 || str(obj(obj(rs.body, "latest"), "roll_check"), "status") != "flag" || obj(rs.body, "latest")["supersedes_id"] == nil {
		t.Fatalf("đối soát lại: %s", rs.raw)
	}
	// Lô chưa cân: consumer không làm gì.
	raw, _ = json.Marshal(fieldport.RollCountRecordedV1{RollCountID: ids.New(), LotRef: ids.New(), PlotRef: ids.New(), HTXRef: ids.New(),
		Rolls: 10, Stage: fieldport.StageBale, EventTime: f.clk.Now()})
	if !deliver(ids.New()) {
		t.Fatal("lô chưa cân vẫn phải xử lý (no-op)")
	}
}

// G3-2 van MRV tuyến DIRECT: không có HUB_IN → CUSTOMER_IN × tỷ lệ thửa theo LỆNH GOM
// (không cần lô hub, không cần vai hub), mass_basis = CUSTOMER_IN_AGGREGATED; tuyến HUB giữ HUB_IN.
func TestMRVDirectRouteAggregated(t *testing.T) {
	f := newFixture(t)
	lot, order := ids.New(), ids.New()
	plotA, plotB := ids.New(), ids.New()
	f.plots.m[order] = []port.PlotShare{{PlotRef: plotA, Ratio: "0.500000"}, {PlotRef: plotB, Ratio: "0.500000"}}
	off := f.officer()
	if r := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/collection", map[string]any{"collection_order_ref": order.String()}); r.status != http.StatusCreated {
		t.Fatalf("gắn lệnh gom: %d %s", r.status, r.raw)
	}
	if r := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/mrv-evidence", map[string]any{}); code(r) != "measurement.no_hub_in" {
		t.Fatalf("chưa có phiếu: %d %s", r.status, r.raw)
	}
	mustStatus(t, f.supervisor().app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN",
		"route": "DIRECT", "mass_kg": "9000", "weighed_at": f.ts(-time.Minute), "evidence_ref": ids.New().String(),
		"reason": "Ghe thẳng nhà máy khách"}), http.StatusCreated, "CUSTOMER_IN")
	p := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/mrv-evidence", map[string]any{})
	mustStatus(t, p, http.StatusCreated, "van DIRECT")
	pubs := off.app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/publications", nil)
	items := arr(pubs.body, "items")
	if len(items) != 2 {
		t.Fatalf("bản ghi van: %s", pubs.raw)
	}
	for _, it := range items {
		pl := obj(it, "payload")
		if str(pl, "mass_basis") != port.MassBasisCustomerInAggregated || str(pl, "collected_kg_bucket") != "2000-5000" {
			t.Fatalf("payload DIRECT: %v", pl)
		}
		if _, ok := pl["mass_kg"]; ok {
			t.Fatal("không bao giờ gửi khối lượng chính xác")
		}
	}
	// Tuyến HUB có lô hub: HUB_IN, cờ HUB_IN (Origins tenant tường minh).
	d := f.approvedScale("SCALE-IN-MRV2", "HUB_IN")
	hl := ids.New()
	f.lots.m[hl] = hubport.LotInfo{LotID: hl, Sources: []hubport.LotSource{{Kind: "plot", Ref: plotA, Ratio: "1.000000"}}}
	if _, err := f.edgeTicket(d, hl, port.PointHubIn, "700", 1, 5, nil, ""); err != nil {
		t.Fatal(err)
	}
	mustStatus(t, off.app(http.MethodPost, "/v1/measurement/lots/"+hl.String()+"/mrv-evidence", map[string]any{}), http.StatusCreated, "van HUB")
	hp := arr(off.app(http.MethodGet, "/v1/measurement/lots/"+hl.String()+"/publications", nil).body, "items")
	if len(hp) != 1 || str(obj(hp[0], "payload"), "mass_basis") != port.MassBasisHubIn || str(obj(hp[0], "payload"), "collected_kg_bucket") != "500-1000" {
		t.Fatalf("payload HUB: %v", hp)
	}
}
