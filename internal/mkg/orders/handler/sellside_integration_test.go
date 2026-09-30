package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// O-06, O-03 (mkg.md §5.5): giá / chiết khấu vượt ngưỡng (tham số có phiên bản, chờ khách — mặc định 0)
// → chỉ sales_approver KHÁC người lập xác nhận đơn (app/ + CHECK DB). O-09: tiền 2 số, đơn giá 4 số.
func TestSalesOrderPriceApproval_O06(t *testing.T) {
	f := newFixture(t)
	r := f.officer.post("/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": "compressed_bale",
		"ordered_kg": "20000", "list_price": "1500000", "unit_price": "1500000", "discount_pct": "5"})
	mustStatus(t, r, http.StatusCreated, "lập đơn chiết khấu 5 %")
	so := r.body["sales_order"].(map[string]any)
	// net = 1 500 000 × 0,95 = 1 425 000; 20 t × 1 425 000 = 28 500 000,00.
	if str(so, "needs_price_approval") != "true" || str(so, "net_price") != "1425000.0000" || str(so, "amount") != "28500000.00" ||
		str(so, "discount_pct") != "5.00" || str(so, "payment_term_days") != "15" {
		t.Fatalf("đơn chiết khấu: %v", so)
	}
	id := str(so, "order_id")
	mustCode(t, f.officer.post("/v1/orders/sales-orders/"+id+"/confirm", nil), 403, "orders.price_approval_required", "officer xác nhận vượt ngưỡng")
	// Người lập có cả vai duyệt: tự duyệt bị chặn.
	both := f.as(f.tenant, port.RoleSalesOfficer, port.RoleSalesApprover)
	r2 := both.post("/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": "dried_straw", "ordered_kg": "1000",
		"list_price": "1000000", "unit_price": "900000"})
	mustStatus(t, r2, http.StatusCreated, "lập đơn lệch giá bảng 10 %")
	if str(r2.body, "sales_order", "needs_price_approval") != "true" {
		t.Fatalf("lệch giá bảng phải cần duyệt: %s", r2.raw)
	}
	mustCode(t, both.post("/v1/orders/sales-orders/"+str(r2.body, "sales_order", "order_id")+"/confirm", nil), 403, "self_approval_forbidden",
		"tự duyệt giá")
	c := f.approver.post("/v1/orders/sales-orders/"+id+"/confirm", nil)
	mustStatus(t, c, http.StatusCreated, "sales_approver xác nhận")
	if str(c.body, "sales_order", "status") != "confirmed" || str(c.body, "sales_order", "approved_by") != f.approver.actor.String() {
		t.Fatalf("sau xác nhận: %s", c.raw)
	}
	// Trong ngưỡng (đúng giá bảng, không chiết khấu) → sales_officer tự xác nhận được.
	f.confirmedSO("1200000")
	// CHECK DB lớp hai: dòng confirmed cần duyệt giá mà actor = người lập bị từ chối.
	f.inTenant(func(tx pgx.Tx) error {
		ctx := context.Background()
		if _, err := tx.Exec(ctx, "SAVEPOINT s1"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO mkg_orders.order_event (id, tenant_id, stream_key, order_id, kind, status, code, customer_ref, product,
		    ordered_kg, unit_price, discount_pct, net_price, amount, needs_price_approval, price_rule, payment_term_days, creator_ref, approver_ref,
		    event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'order:x', $3, 'confirmed', 'confirmed', 'SO999999', $4, 'dried_straw', 1, 1, 5, 1, 1, true, 'x', 15, $5, NULL, now(), $5,
		    $6, 'x')`, ids.New(), f.tenant, ids.New(), ids.New(), both.actor, key())
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatalf("CHECK R-56 đơn bán: %v", err)
		}
		_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s1")
		return err
	})
}

// proto createSalesOrder / advanceSalesOrder, O-05: khối lượng > 0, đơn giá > 0; trạng thái chỉ tiến.
func TestSalesOrderForwardOnly_O05(t *testing.T) {
	f := newFixture(t)
	mustCode(t, f.officer.post("/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": "dried_straw",
		"ordered_kg": "0", "unit_price": "1000"}), 422, "validation_failed", "khối lượng 0")
	mustCode(t, f.officer.post("/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": "dried_straw",
		"ordered_kg": "10", "unit_price": "0"}), 422, "validation_failed", "đơn giá 0")
	id := f.confirmedSO("1000000")
	mustStatus(t, f.officer.post("/v1/orders/sales-orders/"+id+"/advance", map[string]any{"status": "delivering"}), 201, "đang giao")
	mustCode(t, f.officer.post("/v1/orders/sales-orders/"+id+"/advance", map[string]any{"status": "delivering"}), 409, "orders.order_transition",
		"lặp trạng thái")
	mustStatus(t, f.officer.post("/v1/orders/sales-orders/"+id+"/advance", map[string]any{"status": "delivered"}), 201, "đã giao")
	mustCode(t, f.officer.post("/v1/orders/sales-orders/"+id+"/advance", map[string]any{"status": "cancelled", "reason": "Khách hủy đơn hàng"}),
		409, "orders.order_transition", "hủy sau khi giao")
	mustStatus(t, f.officer.post("/v1/orders/sales-orders/"+id+"/advance", map[string]any{"status": "completed"}), 201, "hoàn tất")
}

// O-07, O-08: lịch giao → xuất giao → phiếu cân CUSTOMER_IN (consumer mkg.orders.weigh_ticket) gắn vào giao
// nhận → biên bản giao nhận: khối lượng = phiếu measurement (không nhập tay), tiền = kg/1000 × đơn giá; phát
// mkg.orders.delivery.accepted (KindInternal, vhost mkg.internal — không bao giờ qua van).
func TestDeliveryCustomerInAccepted_O07_O08(t *testing.T) {
	f := newFixture(t)
	order := f.confirmedSO("1250000.5")
	lot, trip := ids.New(), ids.New()
	p := f.officer.post("/v1/orders/sales-orders/"+order+"/deliveries", map[string]any{"lot_ref": lot.String(), "route": "HUB",
		"planned_date": f.day(1), "planned_kg": "20000"})
	mustStatus(t, p, http.StatusCreated, "lịch giao")
	did := str(p.body, "delivery", "delivery_id")
	mustCode(t, f.officer.post("/v1/orders/deliveries/"+did+"/accept", map[string]any{"evidence": map[string]any{"file_ref": ids.New().String(),
		"kind": "epod"}}), 409, "orders.delivery_transition", "chấp nhận khi chưa xuất")
	mustStatus(t, f.officer.post("/v1/orders/deliveries/"+did+"/dispatch", map[string]any{"trip_ref": trip.String()}), 201, "xuất giao")
	if s := str(f.officer.get("/v1/orders/sales-orders/"+order).body, "sales_order", "status"); s != "delivering" {
		t.Fatalf("đơn bán phải sang delivering: %s", s)
	}
	mustCode(t, f.officer.post("/v1/orders/deliveries/"+did+"/accept", map[string]any{"evidence": map[string]any{"file_ref": ids.New().String(),
		"kind": "epod"}}), 409, "orders.customer_in_missing", "chưa có CUSTOMER_IN")
	// HUB_OUT không thuộc orders; CUSTOMER_IN của lô → gắn vào giao nhận.
	f.weigh(lot, mport.PointHubOut, "20100", nil)
	tk := f.weigh(lot, mport.PointCustomerIn, "19850.5", nil)
	g := f.officer.get("/v1/orders/deliveries/" + did).body["delivery"].(map[string]any)
	if str(g, "customer_in_ticket_ref") != tk.String() || str(g, "ticket_kg") != "19850.500" || str(g, "status") != "dispatched" {
		t.Fatalf("gắn CUSTOMER_IN: %v", g)
	}
	file, dispute := ids.New(), ids.New()
	a := f.officer.post("/v1/orders/deliveries/"+did+"/accept", map[string]any{"evidence": map[string]any{"file_ref": file.String(), "kind": "minutes"},
		"dispute_ref": dispute.String()})
	mustStatus(t, a, http.StatusCreated, "chấp nhận")
	d := a.body["delivery"].(map[string]any)
	// 19 850,5 kg / 1000 × 1 250 000,5 = 24 813 134,925… → 24 813 134,93.
	if str(d, "status") != "accepted" || str(d, "accepted_kg") != "19850.500" || str(d, "amount") != "24813134.93" ||
		str(d, "unit_price") != "1250000.5000" || str(d, "acceptance_id") == "" {
		t.Fatalf("biên bản: %v", d)
	}
	ev := f.acceptedEvents(uuid.MustParse(did))
	if len(ev) != 1 {
		t.Fatalf("một sự kiện delivery.accepted: %d", len(ev))
	}
	e := ev[0]
	if e.kind != "internal" || e.id.String() != str(d, "acceptance_id") || e.p.CustomerInTicketRef != tk || e.p.AcceptedKg.String() != "19850.500" ||
		e.p.Amount.String() != "24813134.93" || e.p.UnitPrice != "1250000.5000" || e.p.Route != "HUB" || e.p.TripRef == nil || *e.p.TripRef != trip ||
		len(e.p.Lots) != 1 || e.p.Lots[0].LotRef != lot || e.p.SupersedesID != nil || e.p.Evidence == nil || e.p.Evidence.Kind != "minutes" ||
		e.p.DisputeRef == nil || *e.p.DisputeRef != dispute || e.p.RuleVersion != "orders.delivery.v1" {
		t.Fatalf("payload: %+v kind=%s", e.p, e.kind)
	}
	// Không có tên khách / trường ngoài hợp đồng trong payload (chỉ *_ref).
	var raw map[string]any
	if err := json.Unmarshal(e.raw, &raw); err != nil {
		t.Fatal(err)
	}
	for k := range raw {
		switch k {
		case "delivery_id", "order_ref", "customer_ref", "route", "trip_ref", "customer_in_ticket_ref", "lots", "accepted_kg", "unit_price",
			"amount", "dispute_ref", "accepted_at", "rule_version", "supersedes_id", "event_time", "evidence":
		default:
			t.Errorf("trường lạ trong payload: %s", k)
		}
	}
	// port.Deliveries cho costing.
	v, err := f.svc.Delivery(context.Background(), f.tenant, uuid.MustParse(did))
	if err != nil || v.Status != port.DeliveryAccepted || v.Amount == nil || v.Amount.String() != "24813134.93" || v.CustomerInTicketRef == nil {
		t.Fatalf("port Delivery: %+v %v", v, err)
	}
	if _, err := f.svc.Delivery(context.Background(), ids.New(), uuid.MustParse(did)); !errors.Is(err, port.ErrDeliveryNotFound) {
		t.Fatalf("tenant khác: %v", err)
	}
}

type acceptedEvent struct {
	id   uuid.UUID
	kind string
	raw  []byte
	p    port.DeliveryAcceptedV1
}

func (f *fixture) acceptedEvents(delivery uuid.UUID) []acceptedEvent {
	f.t.Helper()
	var out []acceptedEvent
	f.inTenant(func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT id, kind, payload::text FROM shared.outbox
		    WHERE event_type = $1 AND payload->>'delivery_id' = $2 ORDER BY id`, port.EventDeliveryAccepted, delivery.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e acceptedEvent
			var s string
			if err := rows.Scan(&e.id, &e.kind, &s); err != nil {
				return err
			}
			e.raw = []byte(s)
			if err := json.Unmarshal(e.raw, &e.p); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out
}

// RB-1, O-08 (bản bù): phiếu CUSTOMER_IN bù của measurement tới sau khi đã chấp nhận → chấp nhận bù tự
// động (dòng accepted mới supersedes dòng cũ, có lý do) + sự kiện bản bù supersedes_id; dòng cũ còn nguyên;
// port.Deliveries.Accepted với asOf trước bản bù vẫn trả số cũ. Bù tay (API) cũng vậy.
func TestDeliveryCompensatingCorrection_RB1(t *testing.T) {
	f := newFixture(t)
	order := f.confirmedSO("1000000")
	lot := ids.New()
	did := str(f.officer.post("/v1/orders/sales-orders/"+order+"/deliveries", map[string]any{"lot_ref": lot.String(), "route": "DIRECT",
		"planned_date": f.day(0), "planned_kg": "10000"}).body, "delivery", "delivery_id")
	mustStatus(t, f.officer.post("/v1/orders/deliveries/"+did+"/dispatch", map[string]any{}), 201, "xuất")
	t1 := f.weigh(lot, mport.PointCustomerIn, "10000", nil)
	mustStatus(t, f.officer.post("/v1/orders/deliveries/"+did+"/accept", map[string]any{"evidence": map[string]any{"file_ref": ids.New().String(),
		"kind": "ebill"}}), 201, "chấp nhận")
	from, to := f.clk.Now().Add(-24*time.Hour), f.clk.Now().Add(24*time.Hour)
	time.Sleep(20 * time.Millisecond)
	asOf := time.Now()
	time.Sleep(20 * time.Millisecond)
	// Phiếu bù của measurement: 9 800 kg.
	t2 := f.weigh(lot, mport.PointCustomerIn, "9800", &t1)
	ev := f.acceptedEvents(uuid.MustParse(did))
	if len(ev) != 2 || ev[1].p.SupersedesID == nil || *ev[1].p.SupersedesID != ev[0].id || ev[1].p.AcceptedKg.String() != "9800.000" ||
		ev[1].p.Amount.String() != "9800000.00" || ev[1].p.CustomerInTicketRef != t2 || !ev[1].p.AcceptedAt.Equal(ev[0].p.AcceptedAt) {
		t.Fatalf("sự kiện bản bù: %+v", ev)
	}
	if n := f.count(`SELECT count(*) FROM mkg_orders.delivery_event WHERE delivery_id = $1 AND kind = 'accepted' AND accepted_kg = 10000`,
		uuid.MustParse(did)); n != 1 {
		t.Fatal("dòng chấp nhận gốc phải còn nguyên")
	}
	if n := f.count(`SELECT count(*) FROM mkg_orders.delivery_event WHERE delivery_id = $1 AND supersedes_id = $2 AND correction_reason IS NOT NULL`,
		uuid.MustParse(did), ev[0].id); n != 1 {
		t.Fatal("dòng bù phải supersedes dòng gốc, có lý do")
	}
	ctx := context.Background()
	old, err := f.svc.Accepted(ctx, f.tenant, from, to, asOf, "", 10)
	if err != nil || len(old.Items) != 1 || old.Items[0].AcceptedKg.String() != "10000.000" {
		t.Fatalf("Accepted asOf trước bản bù: %+v %v", old, err)
	}
	cur, err := f.svc.Accepted(ctx, f.tenant, from, to, time.Now().Add(time.Second), "", 10)
	if err != nil || len(cur.Items) != 1 || cur.Items[0].AcceptedKg.String() != "9800.000" {
		t.Fatalf("Accepted hiện hành: %+v %v", cur, err)
	}
	// Bù tay: gắn lại phiếu gốc (vd phiếu bù bị measurement hủy) — có lý do; phiếu lạ bị từ chối.
	mustCode(t, f.officer.post("/v1/orders/deliveries/"+did+"/corrections", map[string]any{"customer_in_ticket_ref": ids.New().String(),
		"reason": "Gắn nhầm phiếu cân của khách"}), 422, "orders.ticket_unknown", "phiếu lạ")
	mustCode(t, f.officer.post("/v1/orders/deliveries/"+did+"/corrections", map[string]any{"reason": "ngắn"}), 422, "validation_failed", "lý do ngắn")
	c := f.officer.post("/v1/orders/deliveries/"+did+"/corrections", map[string]any{"customer_in_ticket_ref": t1.String(),
		"reason": "Khách xác nhận phiếu cân gốc là đúng"})
	mustStatus(t, c, http.StatusCreated, "bù tay")
	if str(c.body, "delivery", "accepted_kg") != "10000.000" {
		t.Fatalf("bù tay: %s", c.raw)
	}
	if ev := f.acceptedEvents(uuid.MustParse(did)); len(ev) != 3 || *ev[2].p.SupersedesID != ev[1].id {
		t.Fatalf("chuỗi bản bù: %d", len(ev))
	}
	// Giao lặp phiếu bù (inbox khác / phát lại) → không thêm dòng, không thêm sự kiện.
	f.ticket(f.tenant, mport.WeighTicketRecordedV1{TicketID: t2, LotRef: lot, Point: mport.PointCustomerIn, Route: mport.RouteDirect,
		Source: mport.SourceEdge, MassKg: mustKg("9800"), EventTime: f.clk.Now(), SupersedesID: &t1})
	if ev := f.acceptedEvents(uuid.MustParse(did)); len(ev) != 3 {
		t.Fatalf("giao lặp không được phát thêm: %d", len(ev))
	}
}
