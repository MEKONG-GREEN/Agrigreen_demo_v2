package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func (f *fixture) rolls(tid, order, htx uuid.UUID, n int) {
	f.t.Helper()
	f.deliver(tid, port.RollCountConsumer, f.cons.RollCount(), fieldport.EventRollCountRecorded, fieldport.RollCountRecordedV1{
		RollCountID: ids.New(), LotRef: order, PlotRef: ids.New(), HTXRef: htx, Rolls: n, Stage: fieldport.StageBoatLoad,
		EventTime: f.clk.Now().Add(-2 * time.Hour)}, ids.New())
}

func (f *fixture) hubIn(tid, lot, order uuid.UUID, kg string) {
	f.t.Helper()
	f.deliver(tid, port.WeighTicketConsumer, f.cons.WeighTicket(), measport.EventWeighTicketRecorded, measport.WeighTicketRecordedV1{
		TicketID: ids.New(), LotRef: lot, Point: measport.PointHubIn, Route: measport.RouteHub, Source: measport.SourceEdge,
		MassKg: mass.MustKg(kg), EventTime: f.clk.Now().Add(-time.Hour), CollectionOrderRef: &order}, ids.New())
}

func settle(order, htx uuid.UUID) map[string]any {
	return map[string]any{"collection_order_ref": order.String(), "htx_ref": htx.String(), "harvest_date": "2026-09-15"}
}

// proto PM-02 (theo cuộn) + PM-04 (duyệt ≠ người lập, một khoản phải trả, hạn 15 ngày, đóng băng),
// C-13: đã duyệt chỉ hoàn bằng quyết toán điều chỉnh, đã trả không hủy; C-11: một lệnh gom một quyết toán.
func TestSettlementPerBaleApprove_PM02_PM04_C11_C13(t *testing.T) {
	f := newFixture(t)
	tid := f.tenant
	order, htx := ids.New(), ids.New()
	f.contracts.set(htx, ordport.PricingPerBale, "150000")
	f.rolls(tid, order, htx, 4400)
	prep := f.accountant()
	r := prep.app(http.MethodPost, "/v1/costing/settlements", settle(order, htx))
	mustStatus(t, r, http.StatusCreated, "lập quyết toán")
	id := str(r.body, "id")
	if str(r.body, "status") != "pending_confirmation" || str(r.body, "amount") != "660000000.00" || str(r.body, "price_source") != "contract" {
		t.Fatalf("PM-02 theo cuộn → ngay: %s", r.raw)
	}
	r = prep.app(http.MethodPost, "/v1/costing/settlements", settle(order, htx))
	if r.status != http.StatusConflict || code(r) != "costing.settlement_exists" {
		t.Fatalf("PM-01 một lệnh gom một quyết toán: %d %s", r.status, r.raw)
	}
	// R-56: người lập có thêm vai kế toán trưởng vẫn không duyệt được của mình.
	self := f.asActor(tid, prep.actor, port.RoleCostAccountant, port.RoleChiefAccountant)
	r = self.adm(http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil)
	if r.status != http.StatusForbidden || code(r) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt: %d %s", r.status, r.raw)
	}
	chief := f.chief()
	r = chief.adm(http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil)
	mustStatus(t, r, http.StatusCreated, "duyệt")
	wantDue := domain.BusinessDate(f.clk.Now().AddDate(0, 0, 15))
	if str(r.body, "status") != "approved" || str(r.body, "due_on") != wantDue || str(r.body, "approved_by") != chief.actor.String() {
		t.Fatalf("PM-04: %s", r.raw)
	}
	if n := f.count(tid, `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND kind = 'internal' AND payload->>'amount' = '660000000.00'`,
		port.EventSettlementApproved); n != 1 {
		t.Fatalf("settlement.approved = %d (đúng MỘT khoản phải trả)", n)
	}
	// Đóng băng: đếm cuộn mới không làm mới quyết toán đã duyệt.
	f.rolls(tid, order, htx, 100)
	r = prep.app(http.MethodGet, "/v1/costing/settlements/"+id, nil)
	if str(r.body, "amount") != "660000000.00" {
		t.Fatalf("đã duyệt phải đóng băng: %s", r.raw)
	}
	r = prep.app(http.MethodPost, "/v1/costing/settlements/"+id+"/cancel", map[string]any{"reason": "Hủy thử sau khi duyệt"})
	if code(r) != "costing.settlement_frozen" {
		t.Fatalf("hủy sau duyệt: %s", r.raw)
	}
	mustStatus(t, prep.app(http.MethodPost, "/v1/costing/settlements/"+id+"/pay", nil), http.StatusCreated, "trả")
	r = prep.app(http.MethodPost, "/v1/costing/settlements/"+id+"/cancel", map[string]any{"reason": "Hủy thử sau khi trả"})
	if code(r) != "costing.settlement_paid" {
		t.Fatalf("hủy sau trả: %s", r.raw)
	}
	// Quyết toán điều chỉnh (đảo một phần), qua duyệt như gốc.
	adj := f.accountant()
	r = adj.app(http.MethodPost, "/v1/costing/settlements/"+id+"/adjust", map[string]any{"amount": "-1500000.00", "reason": "Trừ 10 cuộn hỏng sau khi trả"})
	mustStatus(t, r, http.StatusCreated, "điều chỉnh")
	aid := str(r.body, "id")
	if str(r.body, "kind") != "adjustment" || str(r.body, "supersedes_id") != id || str(r.body, "status") != "pending_confirmation" {
		t.Fatalf("điều chỉnh: %s", r.raw)
	}
	r = chief.adm(http.MethodPost, "/v1/costing/settlements/"+aid+"/approve", nil)
	mustStatus(t, r, http.StatusCreated, "duyệt điều chỉnh")
	if n := f.count(tid, `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND payload->>'supersedes_id' = $2`,
		port.EventSettlementApproved, id); n != 1 {
		t.Fatalf("settlement.approved điều chỉnh: %d", n)
	}
	// R-56 lớp DB: CHECK approved_by <> prepared_by trên bảng chiếu.
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE mkg_costing.settlement_current SET approved_by = prepared_by WHERE settlement_id = $1",
			uuid.MustParse(id))
		return err
	})
	if err == nil {
		t.Fatal("CHECK approved_by <> prepared_by")
	}
	// C-15 (proto D03): khoản phải trả không cộng vào chi phí giá thành lô.
	if n := f.count(tid, "SELECT count(*) FROM mkg_costing.cost_entry WHERE category = 'straw_purchase'"); n != 0 {
		t.Fatalf("D03: quyết toán không sinh bút toán chi phí lô: %d", n)
	}
	// Dựng lại = hiện hành (settlement_current).
	live := f.dump(tid)
	mustStatus(t, chief.adm(http.MethodPost, "/v1/costing/projections/rebuild", nil), http.StatusOK, "dựng lại")
	if rebuilt := f.dump(tid); len(live["settlement_current"]) != 2 || live["settlement_current"][0] != rebuilt["settlement_current"][0] ||
		live["settlement_current"][1] != rebuilt["settlement_current"][1] {
		t.Fatalf("settlement_current dựng lại:\n%v\n%v", live["settlement_current"], rebuilt["settlement_current"])
	}
}

// proto PM-02 theo tấn: chờ cân đủ mọi ghe (logistics còn chuyến chưa xong) → làm mới sau mỗi lần cân.
func TestSettlementPerTonneAwaitsWeighing_PM02_C11(t *testing.T) {
	f := newFixture(t)
	tid := f.tenant
	order, htx, lot := ids.New(), ids.New(), ids.New()
	f.contracts.set(htx, ordport.PricingPerTonne, "150000")
	f.trips.forLot[order] = []logport.TripView{{TripID: ids.New(), Status: logport.TripDeparted}}
	prep := f.accountant()
	r := prep.app(http.MethodPost, "/v1/costing/settlements", settle(order, htx))
	mustStatus(t, r, http.StatusCreated, "lập")
	id := str(r.body, "id")
	if str(r.body, "status") != "awaiting_weighing" || r.body["amount"] != nil {
		t.Fatalf("chưa cân: %s", r.raw)
	}
	f.hubIn(tid, lot, order, "60000")
	r = prep.app(http.MethodGet, "/v1/costing/settlements/"+id, nil)
	if str(r.body, "status") != "awaiting_weighing" {
		t.Fatalf("còn ghe chưa cân → vẫn chờ: %s", r.raw)
	}
	r = f.chief().adm(http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil)
	if code(r) != "costing.settlement_not_ready" {
		t.Fatalf("duyệt khi chờ cân: %s", r.raw)
	}
	f.trips.forLot[order] = []logport.TripView{{TripID: ids.New(), Status: logport.TripCompleted}}
	f.hubIn(tid, ids.New(), order, "36800")
	r = prep.app(http.MethodGet, "/v1/costing/settlements/"+id, nil)
	if str(r.body, "status") != "pending_confirmation" || str(r.body, "mass_kg") != "96800.000" || str(r.body, "amount") != "14520000.00" {
		t.Fatalf("cân đủ → tấn cân × giá: %s", r.raw)
	}
	mustStatus(t, f.chief().adm(http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil), http.StatusCreated, "duyệt")
}

// proto PM-03 / O-10: không có hợp đồng (ErrNoContract) → quyết toán bị chặn, không có số tiền — không đoán
// giá; người lập nhập giá thỏa thuận (thành người lập), người duyệt khác.
func TestSettlementNoContract_PM03(t *testing.T) {
	f := newFixture(t)
	tid := f.tenant
	order, htx := ids.New(), ids.New()
	f.rolls(tid, order, htx, 1000)
	r := f.accountant().app(http.MethodPost, "/v1/costing/settlements", settle(order, htx))
	mustStatus(t, r, http.StatusCreated, "lập")
	id := str(r.body, "id")
	if str(r.body, "blocked_reason") != "no_contract" || r.body["amount"] != nil || r.body["unit_price"] != nil {
		t.Fatalf("PM-03 không đoán giá: %s", r.raw)
	}
	chief := f.chief()
	if r = chief.adm(http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil); code(r) != "costing.settlement_not_ready" {
		t.Fatalf("duyệt khi chưa có giá: %s", r.raw)
	}
	pricer := f.accountant()
	mustStatus(t, pricer.app(http.MethodPost, "/v1/costing/settlements/"+id+"/price", map[string]any{"basis": "per_bale", "unit_price": "0",
		"reason": "Giá thỏa thuận biên bản HTX"}), http.StatusUnprocessableEntity, "giá 0")
	r = pricer.app(http.MethodPost, "/v1/costing/settlements/"+id+"/price", map[string]any{"basis": "per_bale", "unit_price": "140000",
		"reason": "Giá thỏa thuận biên bản HTX số 12"})
	mustStatus(t, r, http.StatusCreated, "nhập giá")
	if str(r.body, "amount") != "140000000.00" || str(r.body, "price_source") != "manual" || str(r.body, "prepared_by") != pricer.actor.String() {
		t.Fatalf("giá thỏa thuận: %s", r.raw)
	}
	self := f.asActor(tid, pricer.actor, port.RoleChiefAccountant)
	if r = self.adm(http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil); r.status != http.StatusForbidden {
		t.Fatalf("người nhập giá tự duyệt: %s", r.raw)
	}
	mustStatus(t, chief.adm(http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil), http.StatusCreated, "duyệt")
}

// C-14: doanh thu carbon chỉ chia khi có số tiền được ghi; 45 % / 70-15-15, VVB nhận dư; tham số ghim, tạm chờ khách.
func TestCarbonRevenueSplit_C14(t *testing.T) {
	f := newFixture(t)
	r := f.accountant().app(http.MethodPost, "/v1/costing/carbon-revenue", map[string]any{"amount": "1000000.00", "reference": "VCS lô 1"})
	mustStatus(t, r, http.StatusCreated, "ghi doanh thu carbon")
	if str(r.body, "retained_amount") != "450000.00" || str(r.body, "farmers_amount") != "385000.00" ||
		str(r.body, "authority_amount") != "82500.00" || str(r.body, "verifier_amount") != "82500.00" || str(r.body, "label") != domain.ProvisionalLabel {
		t.Fatalf("C-14: %s", r.raw)
	}
	mustStatus(t, f.chief().app(http.MethodPost, "/v1/costing/carbon-revenue", map[string]any{"amount": "1.00"}), http.StatusForbidden, "vai")
	// Idempotent POST: cùng Idempotency-Key → cùng bản ghi, không ghi thêm.
	c := f.accountant()
	k := key()
	r1 := c.do(f.app.URL, http.MethodPost, "/v1/costing/carbon-revenue", k, map[string]any{"amount": "10.00"})
	r2 := c.do(f.app.URL, http.MethodPost, "/v1/costing/carbon-revenue", k, map[string]any{"amount": "10.00"})
	if str(r1.body, "id") == "" || str(r1.body, "id") != str(r2.body, "id") {
		t.Fatalf("idempotent: %s / %s", r1.raw, r2.raw)
	}
	if n := f.count(f.tenant, "SELECT count(*) FROM mkg_costing.carbon_revenue_event"); n != 2 {
		t.Fatalf("số bản ghi carbon %d", n)
	}
	// Ghi tay: idempotent theo Idempotency-Key.
	lot := ids.New()
	k = key()
	body := map[string]any{"lot_ref": lot.String(), "category": "storage", "amount": "250000.00"}
	e1 := c.do(f.app.URL, http.MethodPost, "/v1/costing/entries", k, body)
	e2 := c.do(f.app.URL, http.MethodPost, "/v1/costing/entries", k, body)
	mustStatus(t, e1, http.StatusCreated, "ghi tay")
	if str(e1.body, "id") != str(e2.body, "id") || f.count(f.tenant, "SELECT count(*) FROM mkg_costing.cost_entry WHERE lot_ref = $1", lot) != 1 {
		t.Fatalf("idempotent ghi tay: %s / %s", e1.raw, e2.raw)
	}
	mustStatus(t, c.app(http.MethodPost, "/v1/costing/entries", map[string]any{"lot_ref": lot.String(), "category": "sales_revenue",
		"amount": "1.00"}), http.StatusUnprocessableEntity, "doanh thu không ghi tay")
}
