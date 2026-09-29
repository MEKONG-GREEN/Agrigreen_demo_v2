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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// deliver giao một sự kiện nội bộ cho consumer hub qua inbox (như worker-internal).
func (f *fixture) deliver(tid uuid.UUID, consumer string, h inbox.Handler, eventType string, payload any, eventID uuid.UUID) bool {
	f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	env := outbox.Envelope{EventID: eventID, EventType: eventType, SchemaVersion: 1, TenantID: tid, OccurredAt: f.clk.Now(),
		Producer: "mkg/measurement", Payload: raw}
	ok, err := inbox.Process(context.Background(), f.pool, consumer, env, h)
	if err != nil {
		f.t.Fatalf("xử lý %s: %v", eventType, err)
	}
	return ok
}

// mustFail chạy câu lệnh bằng vai hub_app trong tenant và đòi nó hỏng.
func (f *fixture) mustFail(what, sql string, args ...any) {
	f.t.Helper()
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
	if err == nil {
		f.t.Fatalf("%s: câu lệnh phải hỏng", what)
	}
}

func (f *fixture) ticket(tid uuid.UUID, lot uuid.UUID, point measport.WeighPoint, kg string, supersedes *uuid.UUID) (measport.WeighTicketRecordedV1, uuid.UUID) {
	f.t.Helper()
	rv := "weigh.integrate.v1"
	p := measport.WeighTicketRecordedV1{
		TicketID: ids.New(), LotRef: lot, Point: point, Route: measport.RouteHub, Source: measport.SourceEdge, MassKg: mass.MustKg(kg),
		EventTime: f.clk.Now().Add(-time.Minute), RuleVersion: &rv, SupersedesID: supersedes,
	}
	ev := ids.New()
	f.deliver(tid, port.WeighTicketConsumer, hub.NewConsumers(f.clk).WeighTicket(), measport.EventWeighTicketRecorded, p, ev)
	return p, ev
}

func (f *fixture) lotView(op caller, lot string) resp {
	f.t.Helper()
	r := op.app(http.MethodGet, "/v1/hub/lots/"+lot, nil)
	mustStatus(f.t, r, http.StatusOK, "đọc lô")
	return r
}

func (f *fixture) measurements(op caller, lot string) resp {
	f.t.Helper()
	r := op.app(http.MethodGet, "/v1/hub/lots/"+lot+"/measurements", nil)
	mustStatus(f.t, r, http.StatusOK, "đo lường lô")
	return r
}

// Q-MKG-04 + R-61: phiếu cân HUB_IN → khối lượng lô. Giao lặp (cùng event_id, và
// cùng ticket_id với event_id khác) ghi đúng một lần; lô nhập tay giữ số trong dung
// sai, ngoài dung sai → dịch chuyển adjustment bù (không sửa dòng nhận); phiếu bù
// → điều chỉnh theo đúng chênh; phiếu về trước → nhận bằng weigh_ticket_ref.
func TestWeighTicketConsumerIdempotentCompensating(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-WT-01", "500000")
	h := hub.NewConsumers(f.clk).WeighTicket()

	// Lô nhập tay 1000 kg; phiếu 1100 (ngoài dung sai tạm 3 %) → +100.
	lotA, recvMv := f.receive(op, fac, "1000", "", time.Hour)
	aID := uuid.MustParse(lotA)
	tk, ev := f.ticket(f.tenant, aID, measport.PointHubIn, "1100", nil)
	if f.deliver(f.tenant, port.WeighTicketConsumer, h, measport.EventWeighTicketRecorded, tk, ev) {
		t.Fatal("giao lặp cùng event_id phải bị inbox bỏ qua")
	}
	f.deliver(f.tenant, port.WeighTicketConsumer, h, measport.EventWeighTicketRecorded, tk, ids.New()) // cùng ticket, event khác
	lv := f.lotView(op, lotA)
	mvs := arr(lv.body, "movements")
	if lv.body["remaining_kg"] != "1100.000" || len(mvs) != 2 || mvs[1]["kind"] != "adjustment" || mvs[1]["qty_kg"] != "100.000" ||
		mvs[1]["ref_type"] != "weigh_ticket" || mvs[1]["ref_id"] != tk.TicketID.String() || mvs[0]["id"] != recvMv || mvs[0]["qty_kg"] != "1000.000" {
		t.Fatalf("điều chỉnh bù một lần, dòng nhận giữ nguyên: %s", lv.raw)
	}
	m := f.measurements(op, lotA)
	links := arr(m.body, "links")
	if len(links) != 1 || links[0]["outcome"] != "adjusted" || links[0]["diff_kg"] != "100.000" || links[0]["param_version"] != "hub.weigh.v1#0" ||
		str(m.body, "hub_in", "basis") != "ticket" {
		t.Fatalf("liên kết phiếu: %s", m.raw)
	}
	// Phiếu bù 1090 (lô đã theo phiếu) → −10 dù trong dung sai.
	f.ticket(f.tenant, aID, measport.PointHubIn, "1090", &tk.TicketID)
	if lv := f.lotView(op, lotA); lv.body["remaining_kg"] != "1090.000" {
		t.Fatalf("phiếu bù: %s", lv.raw)
	}
	// Phiếu gốc tới lại sau phiếu bù (giao lệch thứ tự, event mới) → đã ghi, không đổi.
	f.deliver(f.tenant, port.WeighTicketConsumer, h, measport.EventWeighTicketRecorded, tk, ids.New())
	if lv := f.lotView(op, lotA); lv.body["remaining_kg"] != "1090.000" || len(arr(lv.body, "movements")) != 3 {
		t.Fatalf("phiếu cũ không được áp lại: %s", lv.raw)
	}

	// Lô nhập tay 1000; phiếu 1020 trong dung sai → giữ số nhập tay.
	lotB, _ := f.receive(op, fac, "1000", "", time.Hour)
	f.ticket(f.tenant, uuid.MustParse(lotB), measport.PointHubIn, "1020", nil)
	if lv := f.lotView(op, lotB); lv.body["remaining_kg"] != "1000.000" || len(arr(lv.body, "movements")) != 1 {
		t.Fatalf("trong dung sai giữ số nhập tay: %s", lv.raw)
	}
	if m := f.measurements(op, lotB); arr(m.body, "links")[0]["outcome"] != "within_tolerance" || str(m.body, "hub_in", "basis") != "manual" {
		t.Fatalf("%s", m.raw)
	}

	// Phiếu về trước lô: pending → nhận bằng weigh_ticket_ref (khối lượng + id lô theo phiếu).
	lotC := ids.New()
	pk, _ := f.ticket(f.tenant, lotC, measport.PointHubIn, "2345.5", nil)
	if m := f.measurements(op, lotC.String()); arr(m.body, "links")[0]["outcome"] != "pending" {
		t.Fatalf("pending: %s", m.raw)
	}
	body := map[string]any{"product": "straw_bale", "weigh_ticket_ref": pk.TicketID.String(), "event_time": f.ts(-time.Minute),
		"sources": []map[string]any{{"kind": "cooperative", "ref": ids.New().String(), "ratio": "1"}}}
	bad := map[string]any{}
	for k, v := range body {
		bad[k] = v
	}
	bad["mass_kg"] = "2000"
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", bad); code(r) != "hub.weigh_mass_mismatch" {
		t.Fatalf("khối lượng khác phiếu: %d %s", r.status, r.raw)
	}
	r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", body)
	mustStatus(t, r, http.StatusCreated, "nhận bằng phiếu")
	if str(r.body, "lot", "id") != lotC.String() || str(r.body, "lot", "initial_kg") != "2345.500" {
		t.Fatalf("lô theo phiếu: %s", r.raw)
	}
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", body); code(r) != "hub.weigh_ticket_used" {
		t.Fatalf("phiếu dùng hai lần: %d %s", r.status, r.raw)
	}
	unknown := map[string]any{}
	for k, v := range body {
		unknown[k] = v
	}
	unknown["weigh_ticket_ref"] = ids.New().String()
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", unknown); code(r) != "hub.weigh_ticket_unknown" {
		t.Fatalf("phiếu chưa về: %d %s", r.status, r.raw)
	}

	// HUB_OUT gắn với phiếu xuất; CUSTOMER_IN bỏ qua.
	d := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/dispatches", map[string]any{
		"mass_kg": "500", "allocations": []map[string]any{{"lot_id": lotB, "mass_kg": "500"}}})
	mustStatus(t, d, http.StatusCreated, "xuất")
	if str(d.body, "dispatch", "risk_rule") != "hub.risk.v2#0" {
		t.Fatalf("phiếu xuất ghim hub.risk.v2: %s", d.raw)
	}
	f.ticket(f.tenant, uuid.MustParse(lotB), measport.PointHubOut, "498", nil)
	f.ticket(f.tenant, uuid.MustParse(lotB), measport.PointCustomerIn, "490", nil)
	mb := f.measurements(op, lotB)
	if str(mb.body, "hub_out", "dispatch_id") != str(d.body, "dispatch", "id") || len(arr(mb.body, "links")) != 2 ||
		arr(mb.body, "links")[1]["outcome"] != "dispatch_linked" {
		t.Fatalf("HUB_OUT ↔ phiếu xuất: %s", mb.raw)
	}

	// Tham số hub.weigh.v1 bắt buộc phiếu → nhận tay cần manual_reason.
	pv := op.adm(http.MethodPost, "/v1/hub/params/weigh/versions", map[string]any{
		"params": map[string]any{"tolerance_pct": "2.00", "tolerance_min_kg": "0", "ticket_required": true, "kg_per_roll_default": "20"},
		"reason": "Hub đã lắp cân sàn, bắt buộc phiếu cân"})
	mustStatus(t, pv, http.StatusCreated, "đề xuất tham số cân")
	mustStatus(t, f.approver().adm(http.MethodPost, "/v1/hub/params/weigh/versions/1/decision", map[string]any{"decision": "approved"}),
		http.StatusCreated, "duyệt tham số cân")
	manual := map[string]any{"product": "straw_bale", "mass_kg": "10", "sources": []map[string]any{{"kind": "cooperative", "ref": ids.New().String(), "ratio": "1"}}}
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", manual); code(r) != "hub.weigh_ticket_required" {
		t.Fatalf("bắt buộc phiếu: %d %s", r.status, r.raw)
	}
	manual["manual_reason"] = "Cân sàn hỏng cảm biến tải số 2"
	r = op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", manual)
	mustStatus(t, r, http.StatusCreated, "nhận tay có lý do")
	if m := f.measurements(op, str(r.body, "lot", "id")); arr(m.body, "links")[0]["outcome"] != "manual_receipt" {
		t.Fatalf("%s", m.raw)
	}

	// Sổ weigh_link bất biến.
	f.mustFail("UPDATE sổ weigh_link", "UPDATE mkg_hub.weigh_link SET outcome = 'stale'")
}

// Q-MKG-06 + R-56: kiểm kê — người duyệt ≠ người đếm (app + CHECK DB); thừa → lô
// điều chỉnh nguồn stocktake; thiếu → trừ theo rủi ro; sổ đổi sau khi đếm → đếm lại.
func TestStocktakeApprovalSoD(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-ST-01", "500000")
	old, _ := f.receive(op, fac, "600", "", 40*24*time.Hour)
	young, _ := f.receive(op, fac, "400", "", time.Hour)

	both := f.as(f.tenant, true, port.RoleHubOperator, port.RoleHubApprover)
	s1 := both.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/stocktakes", map[string]any{
		"lines": []map[string]any{{"product": "straw_bale", "counted_kg": "950"}}})
	mustStatus(t, s1, http.StatusCreated, "đếm")
	if s1.body["variance_kg"] != "-50.000" || s1.body["status"] != "pending" {
		t.Fatalf("chênh: %s", s1.raw)
	}
	id := str(s1.body, "id")
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/stocktakes", map[string]any{
		"lines": []map[string]any{{"product": "straw_bale", "counted_kg": "1"}}}); code(r) != "hub.stocktake_pending_exists" {
		t.Fatalf("một phiên chờ mỗi cơ sở: %d %s", r.status, r.raw)
	}
	if r := both.app(http.MethodPost, "/v1/hub/stocktakes/"+id+"/decision", map[string]any{"decision": "approved"}); code(r) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt: %d %s", r.status, r.raw)
	}
	mustStatus(t, op.app(http.MethodPost, "/v1/hub/stocktakes/"+id+"/decision", map[string]any{"decision": "approved"}), http.StatusForbidden, "thiếu vai")
	// CHECK DB: ghi thẳng quyết định với actor = proposer bị từ chối.
	f.mustFail("CHECK actor_ref <> proposer_ref", `INSERT INTO mkg_hub.stocktake_decision (id, tenant_id, stream_key, facility_id, stocktake_id,
		    decision, proposer_ref, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'x', $3, $4, 'approved', $5, now(), $5, $6, 'r')`,
		ids.New(), f.tenant, uuid.MustParse(fac), uuid.MustParse(id), both.actor, key())

	// Sổ đổi sau khi đếm → stale.
	f.receive(op, fac, "10", "", time.Minute)
	if r := f.approver().app(http.MethodPost, "/v1/hub/stocktakes/"+id+"/decision", map[string]any{"decision": "approved"}); code(r) != "hub.stocktake_stale" {
		t.Fatalf("sổ đổi: %d %s", r.status, r.raw)
	}
	// Đếm lại (supersedes) rồi duyệt: thiếu 60 → trừ lô rủi ro cao nhất (lô cũ) trước.
	s2 := both.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/stocktakes", map[string]any{
		"lines": []map[string]any{{"product": "straw_bale", "counted_kg": "950"}}, "supersedes_id": id, "reason": "Đếm lại sau khi nhận thêm hàng"})
	mustStatus(t, s2, http.StatusCreated, "đếm lại")
	ap := f.approver()
	d := ap.app(http.MethodPost, "/v1/hub/stocktakes/"+str(s2.body, "id")+"/decision", map[string]any{"decision": "approved"})
	mustStatus(t, d, http.StatusCreated, "duyệt")
	adj := arr(d.body, "adjustments")
	if len(adj) != 1 || adj[0]["lot_id"] != old || adj[0]["qty_kg"] != "-60.000" || d.body["status"] != "approved" {
		t.Fatalf("trừ theo rủi ro: %s", d.raw)
	}
	if lv := f.lotView(op, old); lv.body["remaining_kg"] != "540.000" {
		t.Fatalf("%s", lv.raw)
	}
	if lst := arr(op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/stocktakes", nil).body, "items"); len(lst) != 2 || lst[1]["status"] != "superseded" {
		t.Fatalf("lịch sử kiểm kê giữ đủ: %v", lst)
	}
	_ = young

	// Thừa → lô điều chỉnh origin stocktake, nguồn kind stocktake tỷ lệ 1.
	s3 := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/stocktakes", map[string]any{
		"lines": []map[string]any{{"product": "straw_bale", "counted_kg": "975"}, {"product": "dried_straw", "counted_kg": "0"}}})
	mustStatus(t, s3, http.StatusCreated, "đếm 3")
	d3 := ap.app(http.MethodPost, "/v1/hub/stocktakes/"+str(s3.body, "id")+"/decision", map[string]any{"decision": "approved"})
	mustStatus(t, d3, http.StatusCreated, "duyệt 3")
	a3 := arr(d3.body, "adjustments")
	if len(a3) != 1 || a3[0]["qty_kg"] != "25.000" {
		t.Fatalf("thừa: %s", d3.raw)
	}
	lv := f.lotView(op, str(a3[0], "lot_id"))
	src := arr(obj(lv.body, "lot"), "sources")
	if str(lv.body, "lot", "origin") != "stocktake" || len(src) != 1 || src[0]["kind"] != "stocktake" || src[0]["ratio"] != "1.000000" ||
		src[0]["ref"] != str(s3.body, "id") {
		t.Fatalf("lô điều chỉnh: %s", lv.raw)
	}
	// Từ chối bắt buộc lý do; gọi lặp idempotent.
	s4 := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/stocktakes", map[string]any{
		"lines": []map[string]any{{"product": "straw_bale", "counted_kg": "1"}}})
	mustStatus(t, s4, http.StatusCreated, "đếm 4")
	mustStatus(t, ap.app(http.MethodPost, "/v1/hub/stocktakes/"+str(s4.body, "id")+"/decision", map[string]any{"decision": "rejected"}),
		http.StatusUnprocessableEntity, "từ chối thiếu lý do")
	k := key()
	rj := ap.do(f.app.URL, http.MethodPost, "/v1/hub/stocktakes/"+str(s4.body, "id")+"/decision", k, map[string]any{"decision": "rejected", "reason": "Số đếm sai rõ ràng, đếm lại"})
	mustStatus(t, rj, http.StatusCreated, "từ chối")
	if again := ap.do(f.app.URL, http.MethodPost, "/v1/hub/stocktakes/"+str(s4.body, "id")+"/decision", k, map[string]any{"decision": "rejected", "reason": "Số đếm sai rõ ràng, đếm lại"}); string(again.raw) != string(rj.raw) {
		t.Fatalf("gọi lặp: %s", again.raw)
	}
}

// Q-MKG-09 + R-56: tham số rủi ro có phiên bản, người duyệt ≠ người đặt; phiếu xuất ghim phiên bản đã duyệt.
func TestRiskParamsVersionedApproval(t *testing.T) {
	f := newFixture(t)
	fac := f.approvedFacility("HUB-RP-01", "500000")
	op := f.operator()
	g := op.adm(http.MethodGet, "/v1/hub/params/risk", nil)
	if g.body["active_version"] != "hub.risk.v2#0" || str(g.body, "active", "humidity_crit_pct") != "22" || str(g.body, "active", "temp_crit_c") != "55" {
		t.Fatalf("mặc định tạm: %s", g.raw)
	}
	params := map[string]any{"age_horizon_days": "60", "age_weight": "50", "moisture_warn_pct": "18", "moisture_span_pct": "10",
		"moisture_weight": "30", "alert_saturation": "10", "alert_weight": "20", "humidity_warn_pct": "18", "humidity_crit_pct": "22",
		"temp_warn_c": "40", "temp_crit_c": "55", "crit_alert_weight": 2}
	both := f.as(f.tenant, true, port.RoleHubOperator, port.RoleHubApprover)
	p := both.adm(http.MethodPost, "/v1/hub/params/risk/versions", map[string]any{"params": params, "reason": "Rơm vụ hè thu xuống cấp nhanh hơn"})
	mustStatus(t, p, http.StatusCreated, "đề xuất")
	if r := op.adm(http.MethodPost, "/v1/hub/params/risk/versions", map[string]any{"params": params, "reason": "Đề xuất thứ hai khi đang chờ"}); code(r) != "hub.param_pending_exists" {
		t.Fatalf("một phiên bản chờ: %d %s", r.status, r.raw)
	}
	if r := both.adm(http.MethodPost, "/v1/hub/params/risk/versions/1/decision", map[string]any{"decision": "approved"}); code(r) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt: %d %s", r.status, r.raw)
	}
	bad := map[string]any{}
	for k, v := range params {
		bad[k] = v
	}
	bad["age_weight"] = "90"
	mustStatus(t, f.approver().adm(http.MethodPost, "/v1/hub/params/risk/versions/1/decision", map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt")
	if r := op.adm(http.MethodPost, "/v1/hub/params/risk/versions", map[string]any{"params": bad, "reason": "Tổng trọng số sai cố ý"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("tham số sai: %d %s", r.status, r.raw)
	}
	f.receive(op, fac, "100", "", 30*24*time.Hour)
	inv := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/inventory", nil)
	lots := arr(inv.body, "lots")
	// 30/60 × 50 = 25.0
	if inv.body["risk_rule"] != "hub.risk.v2#1" || len(lots) != 1 || lots[0]["risk_score"] != "25.0" {
		t.Fatalf("tồn chấm theo phiên bản 1: %s", inv.raw)
	}
	d := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/dispatches", map[string]any{"mass_kg": "10"})
	mustStatus(t, d, http.StatusCreated, "xuất")
	if str(d.body, "dispatch", "risk_rule") != "hub.risk.v2#1" {
		t.Fatalf("ghim phiên bản: %s", d.raw)
	}
}

// Q-MKG-10: chuyển kho A → B — đang đi đường, nhận ở B bằng phiếu cân HUB_IN của
// lô nguồn, lô mới nguồn kind = lot tỷ lệ 1, hao hụt đường = xuất − nhận.
func TestTransferLifecycle(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	a := f.approvedFacility("HUB-TA-01", "500000")
	b := f.approvedFacility("HUB-TB-01", "500000")
	l1, _ := f.receive(op, a, "600", "", 10*24*time.Hour)
	l2, _ := f.receive(op, a, "400", "", 5*24*time.Hour)

	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+a+"/transfers", map[string]any{"to_facility_id": a, "mass_kg": "10"}); code(r) != "hub.transfer_same_facility" {
		t.Fatalf("cùng cơ sở: %d %s", r.status, r.raw)
	}
	tr := op.app(http.MethodPost, "/v1/hub/facilities/"+a+"/transfers", map[string]any{"to_facility_id": b, "mass_kg": "800"})
	mustStatus(t, tr, http.StatusCreated, "xuất chuyển")
	tid := str(tr.body, "id")
	if tr.body["status"] != "in_transit" || len(arr(tr.body, "source_lots")) != 2 {
		t.Fatalf("đang đi đường: %s", tr.raw)
	}
	if lv := f.lotView(op, l1); lv.body["remaining_kg"] != "0.000" || arr(lv.body, "movements")[1]["kind"] != "transfer_out" ||
		arr(lv.body, "movements")[1]["ref_id"] != tid {
		t.Fatalf("lô nguồn trừ transfer_out: %s", lv.raw)
	}
	// Dịch chuyển chuyển kho không bù trực tiếp được.
	mv := str(arr(f.lotView(op, l1).body, "movements")[1], "id")
	if r := op.app(http.MethodPost, "/v1/hub/movements/"+mv+"/corrections", map[string]any{"reason": "Thử bù dịch chuyển chuyển kho"}); code(r) != "hub.correction_unsupported" {
		t.Fatalf("bù transfer_out: %d %s", r.status, r.raw)
	}
	// Phiếu HUB_IN của lô nguồn tới B khi đang đi đường → transfer_arrival (không điều chỉnh lô ở A).
	t1, _ := f.ticket(f.tenant, uuid.MustParse(l1), measport.PointHubIn, "590", nil)
	t2, _ := f.ticket(f.tenant, uuid.MustParse(l2), measport.PointHubIn, "195", nil)
	if lv := f.lotView(op, l1); len(arr(lv.body, "movements")) != 2 {
		t.Fatalf("phiếu đến không được điều chỉnh lô ở A: %s", lv.raw)
	}
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+a+"/transfers/"+tid+"/receipt", map[string]any{"mass_kg": "700"}); code(r) != "hub.transfer_wrong_facility" {
		t.Fatalf("nhận sai cơ sở: %d %s", r.status, r.raw)
	}
	rc := op.app(http.MethodPost, "/v1/hub/facilities/"+b+"/transfers/"+tid+"/receipt", map[string]any{
		"weigh_ticket_refs": []string{t1.TicketID.String(), t2.TicketID.String()}})
	mustStatus(t, rc, http.StatusCreated, "nhận ở đích")
	if rc.body["status"] != "received" || rc.body["received_kg"] != "785.000" || rc.body["loss_kg"] != "15.000" {
		t.Fatalf("nhận: %s", rc.raw)
	}
	recv := arr(rc.body, "received_lots")
	var sum mass.Kg
	for _, x := range recv {
		sum = sum.Add(mass.MustKg(str(x, "mass_kg")))
		lv := f.lotView(op, str(x, "lot_id"))
		src := arr(obj(lv.body, "lot"), "sources")
		if str(lv.body, "lot", "facility_id") != b || str(lv.body, "lot", "origin") != "transfer" || len(src) != 1 || src[0]["kind"] != "lot" ||
			src[0]["ref"] != x["source_lot_id"] || src[0]["ratio"] != "1.000000" {
			t.Fatalf("lô ở đích: %s", lv.raw)
		}
	}
	if sum.String() != "785.000" {
		t.Fatalf("Σ lô nhận = %s", sum)
	}
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+b+"/transfers/"+tid+"/receipt", map[string]any{"mass_kg": "1"}); code(r) != "hub.transfer_not_in_transit" {
		t.Fatalf("nhận hai lần: %d %s", r.status, r.raw)
	}
	if g := op.app(http.MethodGet, "/v1/hub/transfers/"+tid, nil); g.body["status"] != "received" {
		t.Fatalf("%s", g.raw)
	}
}

// Quy kết hao hụt + hàng dự kiến về + RB-2: sự kiện của tenant khác không chạm dữ liệu tenant này.
func TestReconciliationRollCountAndTenantIsolation(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-RC-01", "500000")
	lot, _ := f.receive(op, fac, "1000", "", time.Hour)
	lotID := uuid.MustParse(lot)
	rh := hub.NewConsumers(f.clk).Reconciliation()
	kg := func(s string) *mass.Kg { k := mass.MustKg(s); return &k }
	rec := measport.ReconciliationCompletedV1{ReconciliationID: ids.New(), LotRef: lotID, Route: measport.RouteHub, HubInKg: kg("1000"),
		HubOutKg: kg("900"), ExpectedLossKg: kg("60"), AbnormalLossKg: kg("40"), Outcome: measport.OutcomeLossAlert,
		RuleVersion: "measurement.recon.v1", EventTime: f.clk.Now().Add(-time.Hour)}
	ev := ids.New()
	f.deliver(f.tenant, port.ReconciliationConsumer, rh, measport.EventReconciliationCompleted, rec, ev)
	f.deliver(f.tenant, port.ReconciliationConsumer, rh, measport.EventReconciliationCompleted, rec, ev)
	f.deliver(f.tenant, port.ReconciliationConsumer, rh, measport.EventReconciliationCompleted, rec, ids.New())
	// Đối soát cũ hơn tới sau không thay đối soát mới.
	older := rec
	older.ReconciliationID, older.EventTime, older.AbnormalLossKg = ids.New(), f.clk.Now().Add(-2*time.Hour), kg("999")
	f.deliver(f.tenant, port.ReconciliationConsumer, rh, measport.EventReconciliationCompleted, older, ids.New())
	ls := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/losses", nil)
	if ls.body["lots"] != json.Number("1") || ls.body["loss_alerts"] != json.Number("1") || ls.body["abnormal_loss_kg"] != "40.000" {
		t.Fatalf("quy kết hao hụt: %s", ls.raw)
	}
	f.inTenant(func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(context.Background(), "SELECT count(*) FROM mkg_hub.lot_loss").Scan(&n)
		if n != 2 {
			t.Fatalf("sổ lot_loss %d dòng, muốn 2 (idempotent theo reconciliation_id)", n)
		}
		return err
	})

	// Đếm cuộn → hàng dự kiến về; bản bù thay bản cũ.
	ch := hub.NewConsumers(f.clk).RollCount()
	collect := ids.New()
	rc := fieldport.RollCountRecordedV1{RollCountID: ids.New(), LotRef: collect, PlotRef: ids.New(), HTXRef: ids.New(), Rolls: 50,
		Stage: fieldport.StageBale, RuleVersion: "fieldops.v1", EventTime: f.clk.Now()}
	f.deliver(f.tenant, port.RollCountConsumer, ch, fieldport.EventRollCountRecorded, rc, ids.New())
	fix := rc
	fix.RollCountID, fix.Rolls, fix.SupersedesID = ids.New(), 48, &rc.RollCountID
	f.deliver(f.tenant, port.RollCountConsumer, ch, fieldport.EventRollCountRecorded, fix, ids.New())
	f.deliver(f.tenant, port.RollCountConsumer, ch, fieldport.EventRollCountRecorded, rc, ids.New()) // bản cũ tới lại
	m := f.measurements(op, collect.String())
	if obj(m.body, "expected_rolls")["bale"] != json.Number("48") || m.body["expected_kg"] != "960.000" {
		t.Fatalf("hàng dự kiến về: %s", m.raw)
	}

	// Tenant khác: phiếu cân cho cùng lot_ref không chạm lô tenant này; không đọc được dữ liệu.
	other := ids.New()
	f.ticket(other, lotID, measport.PointHubIn, "5000", nil)
	if lv := f.lotView(op, lot); lv.body["remaining_kg"] != "1000.000" || len(arr(lv.body, "movements")) != 1 {
		t.Fatalf("RB-2: phiếu tenant khác chạm lô: %s", lv.raw)
	}
	oop := f.as(other, true, port.RoleHubOperator)
	if r := oop.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/losses", nil); r.status != http.StatusNotFound {
		t.Fatalf("RB-2 hao hụt: %d %s", r.status, r.raw)
	}
	if r := oop.app(http.MethodGet, "/v1/hub/lots/"+collect.String()+"/measurements", nil); r.status != http.StatusNotFound {
		t.Fatalf("RB-2 đo lường: %d %s", r.status, r.raw)
	}
	st := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/stocktakes", map[string]any{"lines": []map[string]any{{"product": "straw_bale", "counted_kg": "1000"}}})
	mustStatus(t, st, http.StatusCreated, "đếm")
	if r := oop.app(http.MethodGet, "/v1/hub/stocktakes/"+str(st.body, "id"), nil); r.status != http.StatusNotFound {
		t.Fatalf("RB-2 kiểm kê: %d %s", r.status, r.raw)
	}
	oap := f.as(other, true, port.RoleHubApprover)
	if r := oap.app(http.MethodPost, "/v1/hub/stocktakes/"+str(st.body, "id")+"/decision", map[string]any{"decision": "approved"}); r.status != http.StatusNotFound {
		t.Fatalf("RB-2 duyệt kiểm kê tenant khác: %d %s", r.status, r.raw)
	}
	if r := oop.app(http.MethodGet, "/v1/hub/transfers/"+ids.New().String(), nil); r.status != http.StatusNotFound {
		t.Fatalf("%d", r.status)
	}
}
