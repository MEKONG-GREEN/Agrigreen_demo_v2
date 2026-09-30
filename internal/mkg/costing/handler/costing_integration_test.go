package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func moneyPrice(s string) (money.Price, error) { return money.ParsePrice(s) }

func kgp(s string) *mass.Kg { k := mass.MustKg(s); return &k }

// scenario là dữ liệu vào của một kỳ mẫu (tháng trước) cho một tenant.
type scenario struct {
	order, lot1, lot2, htx, cust, trip, ticket uuid.UUID
	at                                         time.Time
	code                                       string
}

// feed giao đủ sự kiện đầu vào của kỳ mẫu: phiếu HUB_IN, đếm cuộn, chuyến đường thủy, giao nhận, đối soát.
func (f *fixture) feed(tid uuid.UUID) scenario {
	f.t.Helper()
	code, from, _ := f.prevMonth()
	s := scenario{order: ids.New(), lot1: ids.New(), lot2: ids.New(), htx: ids.New(), cust: ids.New(), trip: ids.New(), ticket: ids.New(),
		at: from.Add(72 * time.Hour), code: code}
	order := s.order
	f.deliver(tid, port.WeighTicketConsumer, f.cons.WeighTicket(), measport.EventWeighTicketRecorded, measport.WeighTicketRecordedV1{
		TicketID: s.ticket, LotRef: s.lot1, Point: measport.PointHubIn, Route: measport.RouteHub, Source: measport.SourceEdge,
		MassKg: mass.MustKg("96800"), EventTime: s.at, CollectionOrderRef: &order}, ids.New())
	f.deliver(tid, port.RollCountConsumer, f.cons.RollCount(), fieldport.EventRollCountRecorded, fieldport.RollCountRecordedV1{
		RollCountID: ids.New(), LotRef: s.order, PlotRef: ids.New(), HTXRef: s.htx, Rolls: 4400, Stage: fieldport.StageBoatLoad,
		RuleVersion: "roll_count/v1", EventTime: s.at.Add(-time.Hour)}, ids.New())
	f.deliver(tid, port.TripConsumer, f.cons.Trip(), logport.EventTripCompleted, logport.TripCompletedV1{
		TripID: s.trip, Mode: logport.ModeWaterway, Cargo: []logport.TripCargoV1{{LotRef: s.lot1, MassKg: mass.MustKg("60000")},
			{LotRef: s.lot2, MassKg: mass.MustKg("40000")}}, ActualKg: mass.MustKg("100000"), ActualCost: money.MustAmount("3000000.00"),
		CostSource: logport.CostByVesselRate, DepartedAt: s.at, ArrivedAt: s.at.Add(time.Hour), RuleVersion: "logistics.trip_cost.v1",
		EventTime: s.at.Add(time.Hour)}, ids.New())
	f.deliver(tid, port.DeliveryConsumer, f.cons.Delivery(), ordport.EventDeliveryAccepted, ordport.DeliveryAcceptedV1{
		DeliveryID: ids.New(), OrderRef: ids.New(), CustomerRef: s.cust, Route: ordport.RouteHub, CustomerInTicketRef: ids.New(),
		Lots: []ordport.DeliveryLotV1{{LotRef: s.lot1, MassKg: mass.MustKg("90000")}}, AcceptedKg: mass.MustKg("90000"),
		UnitPrice: "2000000.0000", Amount: money.MustAmount("180000000.00"), AcceptedAt: s.at.Add(48 * time.Hour), RuleVersion: "orders.delivery.v1",
		EventTime: s.at.Add(48 * time.Hour)}, ids.New())
	return s
}

// C-01, C-02, C-06, C-07, C-08, C-09, C-10, C-16 (proto buildCostChain): luồng đầy đủ một kỳ —
// consumer idempotent, giá thành lô, quy kết hao hụt, dựng lại = live (golden), đóng kỳ 202 + job,
// giao lặp job → một kết quả, ảnh chụp băm ổn định, kỳ đã đóng không nhận bút toán và không mở lại,
// điều chỉnh sau đóng ở kỳ mở, journal.pushed → posted.
func TestCostingPeriodLifecycle_C01_C09_C10_C16(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tid := f.tenant
	s := f.feed(tid)

	// C-07: đối soát với hao hụt bất thường → quy kết về nguồn theo tỷ lệ nguồn của lô.
	src1, src2 := ids.New(), ids.New()
	f.origins.m[s.lot1] = hubport.LotInfo{LotID: s.lot1, Sources: []hubport.LotSource{{Kind: "plot", Ref: src1, Ratio: "0.600000"},
		{Kind: "plot", Ref: src2, Ratio: "0.400000"}}}
	recon := measport.ReconciliationCompletedV1{ReconciliationID: ids.New(), LotRef: s.lot1, Route: measport.RouteHub, HubInKg: kgp("96800"),
		HubOutKg: kgp("92000"), AbnormalLossKg: kgp("500"), ExpectedLossKg: kgp("4300"), Outcome: measport.OutcomeLossAlert,
		RuleVersion: "measurement.reconcile.v1", EventTime: s.at.Add(24 * time.Hour)}
	f.deliver(tid, port.ReconciliationConsumer, f.cons.Reconciliation(), measport.EventReconciliationCompleted, recon, ids.New())
	r := f.accountant().app(http.MethodGet, "/v1/costing/sources/"+s.lot1.String()+"/allocations", nil)
	mustStatus(t, r, http.StatusOK, "kết quả quy kết")
	var lossLines []domain.LossLine
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		if m["kind"] == "loss_attribution" {
			raw, _ := json.Marshal(m["lines"])
			_ = json.Unmarshal(raw, &lossLines)
		}
	}
	got := map[uuid.UUID]string{}
	for _, l := range lossLines {
		got[l.SourceRef] = l.AbnormalKg
	}
	if got[src1] != "300.000" || got[src2] != "200.000" {
		t.Fatalf("C-07 quy kết theo tỷ lệ nguồn 0,6/0,4: %v", got)
	}

	// R-61: giao lặp (id sự kiện mới, cùng nội dung) không ghi thêm dòng sổ.
	before := f.count(tid, "SELECT count(*) FROM mkg_costing.cost_entry") + f.count(tid, "SELECT count(*) FROM mkg_costing.allocation_result")
	f.deliver(tid, port.TripConsumer, f.cons.Trip(), logport.EventTripCompleted, logport.TripCompletedV1{TripID: s.trip, Mode: logport.ModeWaterway,
		Cargo:      []logport.TripCargoV1{{LotRef: s.lot2, MassKg: mass.MustKg("40000")}, {LotRef: s.lot1, MassKg: mass.MustKg("60000")}},
		ActualCost: money.MustAmount("3000000.00"), CostSource: logport.CostByVesselRate, EventTime: s.at.Add(time.Hour)}, ids.New())
	f.deliver(tid, port.ReconciliationConsumer, f.cons.Reconciliation(), measport.EventReconciliationCompleted, recon, ids.New())
	if after := f.count(tid, "SELECT count(*) FROM mkg_costing.cost_entry") + f.count(tid, "SELECT count(*) FROM mkg_costing.allocation_result"); after != before {
		t.Fatalf("R-61: giao lặp ghi thêm %d dòng", after-before)
	}
	eid := ids.New()
	f.deliver(tid, port.RollCountConsumer, f.cons.RollCount(), fieldport.EventRollCountRecorded, fieldport.RollCountRecordedV1{RollCountID: ids.New(),
		LotRef: ids.New(), PlotRef: ids.New(), HTXRef: s.htx, Rolls: 1, Stage: fieldport.StageBale, EventTime: s.at}, eid)
	if f.deliver(tid, port.RollCountConsumer, f.cons.RollCount(), fieldport.EventRollCountRecorded, fieldport.RollCountRecordedV1{}, eid) {
		t.Fatal("R-61: cùng event_id phải bị inbox bỏ qua")
	}

	// C-08 proto buildCostChain: lô 1 = sơ chế 96,8 t × 120 000 + cước 60 % × 3 000 000; đ/t theo tấn giao.
	r = f.accountant().app(http.MethodGet, "/v1/costing/lots/"+s.lot1.String()+"/cost", nil)
	mustStatus(t, r, http.StatusOK, "giá thành lô")
	if str(r.body, "total_cost") != "13416000.00" || str(r.body, "revenue") != "180000000.00" || str(r.body, "delivered_kg") != "90000.000" ||
		str(r.body, "cost_per_tonne") != "149066.67" {
		t.Fatalf("C-08 giá thành lô 1: %s", r.raw)
	}
	r = f.accountant().app(http.MethodGet, "/v1/costing/lots/"+s.lot2.String()+"/cost", nil)
	if str(r.body, "total_cost") != "1200000.00" || r.body["cost_per_tonne"] != nil || r.body["cost_per_tonne_undetermined"] != true {
		t.Fatalf("C-08 lô 2 chưa giao → đ/t không xác định: %s", r.raw)
	}
	// C-04: yield_kg_per_roll = kg(HUB_IN) / cuộn.
	r = f.accountant().app(http.MethodGet, "/v1/costing/collections/"+s.order.String()+"/conversion", nil)
	mustStatus(t, r, http.StatusOK, "quy đổi")
	if str(r.body, "yield_kg_per_roll") != "22.000" || str(r.body, "moisture_rule") != "none" || str(r.body, "rule_version") != domain.RuleConversion {
		t.Fatalf("C-04/C-05: %s", r.raw)
	}

	// C-01 golden: dựng lại bảng chiếu từ sổ = bảng chiếu hiện hành.
	live := f.dump(tid)
	mustStatus(t, f.chief().adm(http.MethodPost, "/v1/costing/projections/rebuild", nil), http.StatusOK, "dựng lại")
	if rebuilt := f.dump(tid); !reflect.DeepEqual(live, rebuilt) {
		t.Fatalf("C-01: dựng lại ≠ hiện hành:\nlive=%v\nrebuilt=%v", live, rebuilt)
	}

	// C-09: đóng kỳ → 202 + job; kỳ closing chặn bút toán mới (trigger DB); worker tổng hợp.
	pid := f.periodID(tid, s.code)
	closer := f.closer()
	mustStatus(t, f.accountant().app(http.MethodPost, "/v1/costing/periods/"+pid.String()+"/close", nil), http.StatusForbidden, "sai vai")
	r = closer.app(http.MethodPost, "/v1/costing/periods/"+pid.String()+"/close", nil)
	mustStatus(t, r, http.StatusAccepted, "yêu cầu đóng kỳ")
	jobID := uuid.MustParse(str(r.body, "job_id"))
	if str(r.body, "status") != "queued" || len(str(r.body, "input_snapshot_sha256")) != 64 {
		t.Fatalf("job: %s", r.raw)
	}
	if n := f.count(tid, `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND kind = 'internal'`, port.EventPeriodCloseRequested); n != 1 {
		t.Fatalf("outbox period_close.requested = %d", n)
	}
	r = f.accountant().app(http.MethodPost, "/v1/costing/entries", map[string]any{"lot_ref": s.lot1.String(), "category": "storage",
		"amount": "100.00", "event_time": s.at.Format(time.RFC3339)})
	if r.status != http.StatusConflict || code(r) != "costing.period_not_open" {
		t.Fatalf("bút toán vào kỳ đang đóng: %d %s", r.status, r.raw)
	}
	var job struct {
		snapSHA string
		snap    []byte
	}
	f.inTenant(tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT input_snapshot_sha256, snapshot::text FROM mkg_costing.close_job WHERE id = $1", jobID).Scan(&job.snapSHA, &job.snap)
	})
	var snap domain.Snapshot
	if err := json.Unmarshal(job.snap, &snap); err != nil {
		t.Fatal(err)
	}
	if h, _ := snap.SHA256(); h != job.snapSHA {
		t.Fatalf("ảnh chụp đọc lại từ DB → băm %s ≠ %s (không ổn định)", h, job.snapSHA)
	}
	ev := port.PeriodCloseRequestedV1{JobID: jobID, PeriodID: pid, RequestedBy: closer.actor, RequestedAt: f.clk.Now()}
	f.deliver(tid, port.PeriodCloseConsumer, f.cons.PeriodClose(), port.EventPeriodCloseRequested, ev, ids.New())
	f.deliver(tid, port.PeriodCloseConsumer, f.cons.PeriodClose(), port.EventPeriodCloseRequested, ev, ids.New()) // giao lặp
	if n := f.count(tid, "SELECT count(*) FROM mkg_costing.period_summary"); n != 1 {
		t.Fatalf("giao lặp job → %d kết quả", n)
	}
	r = closer.app(http.MethodGet, "/v1/costing/period-close-jobs/"+jobID.String(), nil)
	if str(r.body, "status") != "completed" {
		t.Fatalf("job: %s", r.raw)
	}
	sum, err := f.mod.ClosedPeriods().ClosedPeriod(ctx, tid, pid)
	if err != nil {
		t.Fatal(err)
	}
	sum2, _ := f.mod.ClosedPeriods().ClosedPeriod(ctx, tid, pid)
	if !reflect.DeepEqual(sum, sum2) || sum.Status != port.PeriodClosed || sum.CloseRunID != jobID || sum.InputSnapshotSHA256 != job.snapSHA {
		t.Fatalf("ClosedPeriod không tất định / sai: %+v", sum)
	}
	want := map[port.CostCategory]string{port.CatHubProcessing: "11616000.00", port.CatTransportWaterway: "3000000.00", port.CatSalesRevenue: "180000000.00"}
	for _, l := range sum.Lines {
		if w, ok := want[l.Category]; ok && l.Amount.String() != w {
			t.Errorf("dòng %s = %s, muốn %s", l.Category, l.Amount, w)
		}
		delete(want, l.Category)
	}
	if len(want) != 0 {
		t.Errorf("thiếu dòng: %v", want)
	}
	lines, _ := json.Marshal(sum.Lines)
	rsha := sum.ResultSetSHA256
	if n := f.count(tid, `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND kind = 'internal'
	  AND payload->>'result_set_sha256' = $2`, port.EventPeriodClosed, rsha); n != 1 {
		t.Fatalf("outbox period.closed = %d", n)
	}

	// C-10: không mở lại — đóng lần nữa 409; vai ứng dụng sửa thẳng trạng thái → trigger chặn.
	r = closer.app(http.MethodPost, "/v1/costing/periods/"+pid.String()+"/close", nil)
	if r.status != http.StatusConflict || code(r) != "costing.period_not_open" {
		t.Fatalf("đóng lại: %d %s", r.status, r.raw)
	}
	err = tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE mkg_costing.period_current SET status = 'open' WHERE period_id = $1", pid)
		return err
	})
	if err == nil {
		t.Fatal("C-10: mở lại kỳ đã đóng phải bị trigger chặn")
	}

	// C-10: sai sót sau đóng kỳ = điều chỉnh ở kỳ đang mở (supersedes), kết quả kỳ cũ không đổi.
	var hubEntry uuid.UUID
	f.inTenant(tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id FROM mkg_costing.cost_entry WHERE lot_ref = $1 AND category = 'hub_processing'", s.lot1).Scan(&hubEntry)
	})
	adj := f.accountant()
	r = adj.app(http.MethodPost, "/v1/costing/entries/"+hubEntry.String()+"/adjust", map[string]any{"amount": "11000000.00", "mass_kg": "96800",
		"reason": "Hóa đơn sấy thực tế thấp hơn đơn giá tạm"})
	mustStatus(t, r, http.StatusCreated, "điều chỉnh")
	if str(r.body, "period_code") != domain.PeriodCode(f.clk.Now()) || str(r.body, "late_from") != s.code ||
		str(r.body, "delta_amount") != "-616000.00" || str(r.body, "supersedes_id") != hubEntry.String() || str(r.body, "entry_kind") != "adjusting" {
		t.Fatalf("điều chỉnh: %s", r.raw)
	}
	mustStatus(t, adj.app(http.MethodPost, "/v1/costing/entries/"+hubEntry.String()+"/adjust", map[string]any{"amount": "1.00",
		"reason": "Điều chỉnh lần hai của dòng đã bị thay"}), http.StatusConflict, "dòng đã bị thay")
	sum3, _ := f.mod.ClosedPeriods().ClosedPeriod(ctx, tid, pid)
	if l3, _ := json.Marshal(sum3.Lines); string(l3) != string(lines) || sum3.ResultSetSHA256 != rsha {
		t.Fatal("kết quả kỳ đã đóng bị đổi")
	}
	r = f.accountant().app(http.MethodGet, "/v1/costing/lots/"+s.lot1.String()+"/cost", nil)
	if str(r.body, "total_cost") != "12800000.00" {
		t.Fatalf("giá thành lô sau điều chỉnh: %s", r.raw)
	}

	// C-16: journal.pushed → posted (một lần); ClosedPeriod vẫn đọc được.
	pushed := abport.JournalPushedV1{JournalID: ids.New(), PeriodID: pid, PeriodCode: s.code, CloseRunID: jobID, SourceHash: rsha,
		Adapter: "fake.v1", ExternalRef: "EXT-1", LineCount: 3, TotalDebit: money.MustAmount("1"), TotalCredit: money.MustAmount("1"),
		Attempt: 1, MappingVersion: 1, PushedAt: f.clk.Now(), EventTime: f.clk.Now()}
	f.deliver(tid, port.JournalPushedConsumer, f.cons.JournalPushed(), abport.EventJournalPushed, pushed, ids.New())
	f.deliver(tid, port.JournalPushedConsumer, f.cons.JournalPushed(), abport.EventJournalPushed, pushed, ids.New())
	r = closer.app(http.MethodGet, "/v1/costing/periods/"+pid.String(), nil)
	if str(r.body, "status") != "posted" || r.body["summary"] == nil {
		t.Fatalf("posted: %s", r.raw)
	}
	if ps, err := f.mod.ClosedPeriods().ClosedPeriod(ctx, tid, pid); err != nil || ps.Status != port.PeriodPosted {
		t.Fatalf("ClosedPeriod posted: %v %v", ps.Status, err)
	}

	// Dựng lại sau cả vòng đời vẫn = hiện hành.
	live = f.dump(tid)
	mustStatus(t, f.accountant().adm(http.MethodPost, "/v1/costing/projections/rebuild", nil), http.StatusOK, "dựng lại 2")
	if rebuilt := f.dump(tid); !reflect.DeepEqual(live, rebuilt) {
		t.Fatalf("C-01 (sau đóng kỳ): dựng lại ≠ hiện hành:\nlive=%v\nrebuilt=%v", live, rebuilt)
	}

	// Cách ly tenant: tenant khác không thấy kỳ / lô / job của tenant này.
	other := ids.New()
	if _, err := f.mod.ClosedPeriods().ClosedPeriod(ctx, other, pid); !errors.Is(err, port.ErrPeriodNotFound) {
		t.Fatalf("RB-2 ClosedPeriod tenant khác: %v", err)
	}
	o := f.as(other, port.RoleCostAccountant, port.RolePeriodCloser)
	mustStatus(t, o.app(http.MethodGet, "/v1/costing/lots/"+s.lot1.String()+"/cost", nil), http.StatusNotFound, "RB-2 lô")
	mustStatus(t, o.app(http.MethodGet, "/v1/costing/period-close-jobs/"+jobID.String(), nil), http.StatusNotFound, "RB-2 job")
	mustStatus(t, o.app(http.MethodGet, "/v1/costing/periods/"+pid.String(), nil), http.StatusNotFound, "RB-2 kỳ")
	if _, err := f.mod.ClosedPeriods().ClosedPeriod(ctx, tid, ids.New()); !errors.Is(err, port.ErrPeriodNotFound) {
		t.Fatalf("ErrPeriodNotFound: %v", err)
	}
	openID := f.periodID(tid, domain.PeriodCode(f.clk.Now()))
	if _, err := f.mod.ClosedPeriods().ClosedPeriod(ctx, tid, openID); !errors.Is(err, port.ErrPeriodNotClosed) {
		t.Fatalf("ErrPeriodNotClosed: %v", err)
	}
	// Kỳ tháng hiện tại chưa hết → chưa đóng được.
	mustStatus(t, closer.app(http.MethodPost, "/v1/costing/periods/"+openID.String()+"/close", nil), http.StatusConflict, "kỳ chưa hết")

	// RB-1: sáu phép sửa/xóa trên mọi sổ mới đều thất bại (vai ứng dụng và owner).
	owner := f.env.Pool(t, migrations.MKG, dbadmin.Migrator)
	for _, tb := range []string{"param_version", "param_decision", "input_event", "cost_entry", "conversion_result", "allocation_result",
		"period_event", "period_summary", "settlement_event", "carbon_revenue_event"} {
		for _, who := range []struct {
			name string
			pool interface {
				tenant.Beginner
			}
		}{{"costing_app", f.pool}, {"owner", owner}} {
			for _, sql := range []string{"UPDATE mkg_costing." + tb + " SET idem_key = idem_key", "DELETE FROM mkg_costing." + tb, "TRUNCATE mkg_costing." + tb} {
				err := tenant.WithTenantTx(ctx, who.pool, tid, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err })
				if err == nil {
					// bảng rỗng trong tenant: UPDATE/DELETE 0 dòng không chạm trigger — TRUNCATE vẫn phải hỏng
					if sql[:8] == "TRUNCATE" {
						t.Errorf("RB-1: %s bằng %s phải hỏng", sql, who.name)
					}
					continue
				}
				if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
					t.Errorf("RB-1 %s (%s): muốn lỗi quyền, nhận %v", sql, who.name, err)
				}
			}
		}
	}
}

// C-02, C-03 (R-13, R-56): tham số có phiên bản theo thời điểm sự việc; người đặt ≠ người duyệt (app + CHECK
// DB); tính lại → kết quả MỚI supersedes_id ghim phiên bản mới, kết quả cũ giữ nguyên; tính lại với
// as_of trước quyết định → không đổi.
func TestParamsRecomputeSupersedes_C02_C03(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tid := f.tenant
	s := f.feed(tid)
	var hubAlloc uuid.UUID
	f.inTenant(tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT allocation_id FROM mkg_costing.allocation_current WHERE kind = 'hub_processing' AND superseded_by IS NULL").
			Scan(&hubAlloc)
	})
	r := f.accountant().app(http.MethodGet, "/v1/costing/allocations/"+hubAlloc.String(), nil)
	pins, _ := r.body["param_pins"].(map[string]any)
	if str(r.body, "rule_version") != domain.RuleHubProcessing || pins[port.ParamHubProcessingRate] != json.Number("0") {
		t.Fatalf("C-02 ghim rule_version + tham số #0: %s", r.raw)
	}
	_, from, _ := f.prevMonth()
	setter, chief := f.as(tid, port.RoleCostAccountant, port.RoleChiefAccountant), f.chief()
	mustStatus(t, f.chief().adm(http.MethodPost, "/v1/costing/params", map[string]any{"key": port.ParamHubProcessingRate, "value": "150000",
		"reason": "Đơn giá sấy theo hợp đồng hub"}), http.StatusForbidden, "chief không đặt")
	r = setter.adm(http.MethodPost, "/v1/costing/params", map[string]any{"key": port.ParamHubProcessingRate, "value": "150000",
		"reason": "Đơn giá sấy theo hợp đồng hub", "effective_from": from.Format(time.RFC3339)})
	mustStatus(t, r, http.StatusCreated, "đề xuất tham số")
	mustStatus(t, setter.adm(http.MethodPost, "/v1/costing/params", map[string]any{"key": port.ParamHubProcessingRate, "value": "1",
		"reason": "Đề xuất thứ hai khi đang chờ"}), http.StatusConflict, "một phiên bản chờ")
	path := "/v1/costing/params/" + port.ParamHubProcessingRate + "/versions/1/decision"
	r = setter.adm(http.MethodPost, path, map[string]any{"decision": "approved"})
	if r.status != http.StatusForbidden || code(r) != "self_approval_forbidden" {
		t.Fatalf("R-56 tự duyệt: %d %s", r.status, r.raw)
	}
	// R-56 lớp DB: CHECK actor_ref <> proposer_ref.
	err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.param_decision (id, tenant_id, stream_key, param_key, version_id, version_seq, decision,
		  proposer_ref, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'param:x', 'x', $3, 1, 'approved', $4, now(), $4, $5, 'r')`,
			ids.New(), tid, ids.New(), setter.actor, key())
		return err
	})
	if err == nil {
		t.Fatal("R-56: CHECK DB phải chặn tự duyệt")
	}
	asOfBefore := time.Now().UTC().Add(-time.Second)
	mustStatus(t, chief.adm(http.MethodPost, path, map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt tham số")

	// Tính lại với as_of trước quyết định → cùng tham số #0 → không đổi (R-13).
	r = f.accountant().app(http.MethodPost, "/v1/costing/allocations/"+hubAlloc.String()+"/recompute",
		map[string]any{"as_of": asOfBefore.Format(time.RFC3339Nano), "reason": "Kiểm lại theo quyết định đã biết lúc đó"})
	mustStatus(t, r, http.StatusOK, "tính lại as_of cũ")
	if r.body["changed"] != false || str(r.body, "id") != hubAlloc.String() {
		t.Fatalf("as_of trước quyết định phải không đổi: %s", r.raw)
	}
	// Tính lại hiện hành → kết quả mới supersedes, ghim #1; kết quả cũ không đổi.
	r = f.accountant().app(http.MethodPost, "/v1/costing/allocations/"+hubAlloc.String()+"/recompute",
		map[string]any{"reason": "Áp đơn giá sấy đã duyệt từ đầu kỳ"})
	mustStatus(t, r, http.StatusCreated, "tính lại")
	newID := str(r.body, "id")
	pins, _ = r.body["param_pins"].(map[string]any)
	if r.body["changed"] != true || str(r.body, "supersedes_id") != hubAlloc.String() || pins[port.ParamHubProcessingRate] != json.Number("1") ||
		str(r.body, "total_amount") != "14520000.00" {
		t.Fatalf("C-02 kết quả mới: %s", r.raw)
	}
	r = f.accountant().app(http.MethodGet, "/v1/costing/allocations/"+hubAlloc.String(), nil)
	pins, _ = r.body["param_pins"].(map[string]any)
	if str(r.body, "total_amount") != "11616000.00" || pins[port.ParamHubProcessingRate] != json.Number("0") || str(r.body, "superseded_by") != newID {
		t.Fatalf("kết quả cũ phải giữ nguyên: %s", r.raw)
	}
	mustStatus(t, f.accountant().app(http.MethodPost, "/v1/costing/allocations/"+hubAlloc.String()+"/recompute",
		map[string]any{"reason": "Tính lại kết quả đã bị thay"}), http.StatusConflict, "đã bị thay")
	r = f.accountant().app(http.MethodGet, "/v1/costing/lots/"+s.lot1.String()+"/cost", nil)
	if str(r.body, "total_cost") != "16320000.00" {
		t.Fatalf("giá thành sau tính lại (delta): %s", r.raw)
	}
	// Tham số theo thời điểm sự việc: phiếu HUB_IN mới (sự việc trước effective_from) vẫn dùng #0.
	lot := ids.New()
	f.deliver(tid, port.WeighTicketConsumer, f.cons.WeighTicket(), measport.EventWeighTicketRecorded, measport.WeighTicketRecordedV1{
		TicketID: ids.New(), LotRef: lot, Point: measport.PointHubIn, Route: measport.RouteHub, Source: measport.SourceEdge,
		MassKg: mass.MustKg("1000"), EventTime: from.Add(-48 * time.Hour)}, ids.New())
	r = f.accountant().app(http.MethodGet, "/v1/costing/lots/"+lot.String()+"/cost", nil)
	if str(r.body, "total_cost") != "120000.00" {
		t.Fatalf("C-03 tham số theo thời điểm sự việc: %s", r.raw)
	}
	// Dựng lại = hiện hành sau tính lại.
	live := f.dump(tid)
	mustStatus(t, chief.adm(http.MethodPost, "/v1/costing/projections/rebuild", nil), http.StatusOK, "dựng lại")
	if rebuilt := f.dump(tid); !reflect.DeepEqual(live, rebuilt) {
		t.Fatalf("dựng lại ≠ hiện hành:\n%v\n%v", live, rebuilt)
	}
	// Bản bù chuyến (compensation): chuyến thay hàng hóa → bút toán cũ bị thay, tổng đúng.
	f.deliver(tid, port.TripConsumer, f.cons.Trip(), logport.EventTripCompleted, logport.TripCompletedV1{TripID: s.trip, Mode: logport.ModeWaterway,
		Cargo: []logport.TripCargoV1{{LotRef: s.lot2, MassKg: mass.MustKg("40000")}}, ActualCost: money.MustAmount("2000000.00"),
		CostSource: logport.CostManual, SupersedesID: &s.trip, EventTime: s.at.Add(time.Hour)}, ids.New())
	r = f.accountant().app(http.MethodGet, "/v1/costing/lots/"+s.lot1.String()+"/cost", nil)
	if str(r.body, "total_cost") != "14520000.00" {
		t.Fatalf("bản bù chuyến: lô 1 mất cước: %s", r.raw)
	}
	r = f.accountant().app(http.MethodGet, "/v1/costing/lots/"+s.lot2.String()+"/cost", nil)
	if str(r.body, "total_cost") != "2000000.00" {
		t.Fatalf("bản bù chuyến: lô 2: %s", r.raw)
	}
}

// C-10 (R-56): người đóng kỳ ≠ người lập bút toán điều chỉnh của kỳ — app (403) và DB (CHECK + trigger).
func TestPeriodCloserNotAdjuster_C10(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tid := f.tenant
	s := f.feed(tid)
	var entry uuid.UUID
	f.inTenant(tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id FROM mkg_costing.cost_entry WHERE lot_ref = $1 AND category = 'hub_processing'", s.lot1).Scan(&entry)
	})
	both := f.as(tid, port.RoleCostAccountant, port.RolePeriodCloser)
	// Điều chỉnh vào kỳ đang mở = tháng hiện tại.
	mustStatus(t, both.app(http.MethodPost, "/v1/costing/entries/"+entry.String()+"/adjust", map[string]any{"amount": "11600000.00",
		"reason": "Điều chỉnh đơn giá sấy trong kỳ"}), http.StatusCreated, "điều chỉnh")
	cur := domain.PeriodCode(f.clk.Now())
	pid := f.periodID(tid, cur)
	f.clk.Advance(40 * 24 * time.Hour) // kỳ hiện tại đã hết
	r := both.app(http.MethodPost, "/v1/costing/periods/"+pid.String()+"/close", nil)
	if r.status != http.StatusForbidden || code(r) != "self_approval_forbidden" {
		t.Fatalf("R-56 người lập điều chỉnh đóng kỳ: %d %s", r.status, r.raw)
	}
	// Lớp DB: chèn thẳng job với requested_by là người lập điều chỉnh → trigger / CHECK chặn.
	err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.close_job (id, tenant_id, period_id, period_code, status, requested_by, as_of, snapshot,
		  input_snapshot_sha256, adjusting_authors, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, 'queued', $5, now(), '{}', repeat('a', 64), '{}', $6, 'r')`, ids.New(), tid, pid, cur, both.actor, key())
		return err
	})
	if err == nil {
		t.Fatal("R-56: DB phải chặn job đóng kỳ của người lập điều chỉnh")
	}
	err = tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.close_job (id, tenant_id, period_id, period_code, status, requested_by, as_of, snapshot,
		  input_snapshot_sha256, adjusting_authors, idem_key, rule_version)
		  VALUES ($1, $2, $3, 'x', 'queued', $4, now(), '{}', repeat('a', 64), ARRAY[$4::uuid], $5, 'r')`, ids.New(), tid, ids.New(), both.actor, key())
		return err
	})
	if err == nil {
		t.Fatal("R-56: CHECK requested_by ∉ adjusting_authors")
	}
	// Người đóng khác → 202; idempotent: cùng khóa → cùng job.
	other := f.closer()
	k := key()
	r1 := other.do(f.app.URL, http.MethodPost, "/v1/costing/periods/"+pid.String()+"/close", k, nil)
	mustStatus(t, r1, http.StatusAccepted, "đóng kỳ")
	r2 := other.do(f.app.URL, http.MethodPost, "/v1/costing/periods/"+pid.String()+"/close", k, nil)
	if str(r2.body, "job_id") != str(r1.body, "job_id") {
		t.Fatalf("idempotent POST close: %s / %s", r1.raw, r2.raw)
	}
	var authors []string
	f.inTenant(tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT adjusting_authors::text[] FROM mkg_costing.close_job WHERE id = $1", uuid.MustParse(str(r1.body, "job_id"))).Scan(&authors)
	})
	if len(authors) != 1 || authors[0] != both.actor.String() {
		t.Fatalf("adjusting_authors: %v", authors)
	}
}
