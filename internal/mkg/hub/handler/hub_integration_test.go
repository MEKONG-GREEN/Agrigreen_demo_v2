package handler_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func contractKeys(t *testing.T, name string) []string {
	t.Helper()
	c, err := publication.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return c.AllowedFields()
}

// ARCH §5 (van công bố) + R-56: dữ liệu chủ có phiên bản, người duyệt ≠ người đề
// xuất, không xóa cứng (ngừng dùng = phiên bản mới), phê duyệt xuất qua van.
func TestFacilityMasterDataApprovalAndValve(t *testing.T) {
	f := newFixture(t)
	op, ap := f.operator(), f.approver()

	p := op.adm(http.MethodPost, "/v1/hub/facilities", f.facilityBody("HUB-CT-01", "500000"))
	mustStatus(t, p, http.StatusCreated, "đề xuất")
	id := str(p.body, "facility_id")
	if p.body["state"] != "pending" || p.body["version"] != json.Number("1") {
		t.Fatalf("phiên bản 1 phải chờ duyệt: %s", p.raw)
	}
	// Chưa duyệt → chưa là dữ liệu chủ hiện hành.
	mustStatus(t, op.app(http.MethodGet, "/v1/hub/facilities/"+id, nil), http.StatusNotFound, "chưa duyệt")
	// Mã trùng với đề xuất đang chờ → 409.
	if r := op.adm(http.MethodPost, "/v1/hub/facilities", f.facilityBody("HUB-CT-01", "1")); r.status != http.StatusConflict || code(r) != "hub.code_taken" {
		t.Fatalf("mã trùng: %d %s", r.status, r.raw)
	}

	// R-56: một người có cả hai vai vẫn không được tự duyệt đề xuất của mình.
	both := f.as(f.tenant, true, port.RoleHubOperator, port.RoleHubApprover)
	p2 := both.adm(http.MethodPost, "/v1/hub/facilities", f.facilityBody("WH-CT-02", "1000"))
	mustStatus(t, p2, http.StatusCreated, "đề xuất 2")
	self := both.adm(http.MethodPost, "/v1/hub/facilities/"+str(p2.body, "facility_id")+"/versions/1/decision", map[string]any{"decision": "approved"})
	if self.status != http.StatusForbidden || code(self) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt phải bị chặn: %d %s", self.status, self.raw)
	}
	// Người vận hành không có vai duyệt.
	mustStatus(t, op.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions/1/decision", map[string]any{"decision": "approved"}), http.StatusForbidden, "thiếu vai")
	// Bề mặt quản trị bắt buộc MFA.
	noMFA := f.as(f.tenant, false, port.RoleHubApprover)
	if r := noMFA.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions/1/decision", map[string]any{"decision": "approved"}); code(r) != "mfa_required" {
		t.Fatalf("MFA: %d %s", r.status, r.raw)
	}

	k := key()
	d := ap.do(f.admin.URL, http.MethodPost, "/v1/hub/facilities/"+id+"/versions/1/decision", k, map[string]any{"decision": "approved"})
	mustStatus(t, d, http.StatusCreated, "duyệt")
	if str(d.body, "facility", "approved_by") != ap.actor.String() || str(d.body, "facility", "proposed_by") != op.actor.String() {
		t.Fatalf("người đặt/duyệt: %s", d.raw)
	}
	// Gọi lặp cùng Idempotency-Key → cùng phản hồi, không xuất van lần hai.
	again := ap.do(f.admin.URL, http.MethodPost, "/v1/hub/facilities/"+id+"/versions/1/decision", k, map[string]any{"decision": "approved"})
	if again.status != http.StatusCreated || string(again.raw) != string(d.raw) {
		t.Fatalf("gọi lặp phải trả đúng phản hồi lần đầu: %d", again.status)
	}

	pubs := arr(d.body, "publications")
	if len(pubs) != 2 || pubs[0]["contract"] != "hub_facility" || pubs[1]["contract"] != "hub_capacity" {
		t.Fatalf("phê duyệt phải xuất hub_facility + hub_capacity: %s", d.raw)
	}
	fp := obj(pubs[0], "payload")
	if strings.Join(keys(fp), ",") != strings.Join(contractKeys(t, "hub_facility"), ",") {
		t.Fatalf("payload hub_facility phải đúng các trường hợp đồng: %v", keys(fp))
	}
	if fp["capacity_tons"] != "500.000000" || fp["unit"] != "t" || obj(fp, "location")["lat"] != json.Number("10.0452") || str(fp, "boundary", "type") != "MultiPolygon" {
		t.Fatalf("payload hub_facility: %v", fp)
	}
	// Outbox của cụm mkg có đúng sự kiện với event_id = id bản ghi xuất.
	f.inTenant(func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE producer = 'mkg/hub'
		    AND ((id = $1 AND event_type = 'mkg.hub.facility.published') OR (id = $2 AND event_type = 'mkg.hub.capacity.published'))`,
			pubs[0]["id"], pubs[1]["id"]).Scan(&n)
		if n != 2 {
			t.Fatalf("outbox phải có 2 sự kiện van, có %d", n)
		}
		return err
	})

	// Thay đổi: thiếu lý do → 422; có lý do → phiên bản 2 chờ duyệt; lần đề xuất thứ hai khi đang chờ → 409.
	chg := f.facilityBody("HUB-CT-01", "450000")
	chg["status"] = "inactive"
	mustStatus(t, op.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions", chg), http.StatusUnprocessableEntity, "thiếu lý do")
	chg["reason"] = "Hub tạm ngừng để sửa lò sấy số 2"
	v2 := op.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions", chg)
	mustStatus(t, v2, http.StatusCreated, "phiên bản 2")
	if v2.body["version"] != json.Number("2") || str(v2.body, "supersedes_id") != str(d.body, "facility", "version_id") {
		t.Fatalf("phiên bản 2 phải trỏ về phiên bản hiện hành: %s", v2.raw)
	}
	if r := op.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions", chg); code(r) != "hub.pending_version_exists" {
		t.Fatalf("chờ duyệt trùng: %d %s", r.status, r.raw)
	}
	// Từ chối bắt buộc lý do; từ chối không đổi dữ liệu hiện hành.
	mustStatus(t, ap.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions/2/decision", map[string]any{"decision": "rejected"}), http.StatusUnprocessableEntity, "từ chối thiếu lý do")
	rj := ap.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions/2/decision", map[string]any{"decision": "rejected", "reason": "Chưa có biên bản kiểm tra lò sấy"})
	mustStatus(t, rj, http.StatusCreated, "từ chối")
	if cur := op.app(http.MethodGet, "/v1/hub/facilities/"+id, nil); cur.body["status"] != "active" || cur.body["version"] != json.Number("1") {
		t.Fatalf("từ chối không được đổi hiện hành: %s", cur.raw)
	}
	if r := ap.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions/2/decision", map[string]any{"decision": "approved"}); code(r) != "hub.version_not_pending" {
		t.Fatalf("đã quyết định: %d %s", r.status, r.raw)
	}
	vs := arr(op.adm(http.MethodGet, "/v1/hub/facilities/"+id+"/versions", nil).body, "items")
	if len(vs) != 2 || vs[0]["state"] != "rejected" || vs[1]["state"] != "approved" {
		t.Fatalf("lịch sử phiên bản giữ đủ (không xóa cứng): %v", vs)
	}
	// Ngừng dùng = phiên bản mới được duyệt; cơ sở vẫn đọc được.
	v3 := op.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions", chg)
	mustStatus(t, v3, http.StatusCreated, "phiên bản 3")
	mustStatus(t, ap.adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions/3/decision", map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt 3")
	cur := op.app(http.MethodGet, "/v1/hub/facilities/"+id, nil)
	if cur.body["status"] != "inactive" || cur.body["capacity_kg"] != "450000.000" {
		t.Fatalf("hiện hành sau ngừng dùng: %s", cur.raw)
	}
	// Cơ sở ngừng dùng không nhận hàng.
	r := op.app(http.MethodPost, "/v1/hub/facilities/"+id+"/receipts", map[string]any{
		"product": "straw_bale", "mass_kg": "10", "sources": []map[string]any{{"kind": "plot", "ref": ids.New().String(), "mass_kg": "10"}},
	})
	if code(r) != "hub.facility_inactive" {
		t.Fatalf("nhận vào cơ sở ngừng dùng: %d %s", r.status, r.raw)
	}
	// Lớp chặn tự duyệt thứ hai ở DB: CHECK actor_ref <> proposer_ref.
	f.inTenant(func(tx pgx.Tx) error {
		sp, _ := tx.Begin(context.Background())
		d := store.Decision{ID: ids.New(), TenantID: f.tenant, FacilityID: uuid.MustParse(id), VersionID: ids.New(), VersionSeq: 9,
			Decision: domain.DecisionApproved, ProposerRef: op.actor, ActorRef: op.actor, EventTime: f.clk.Now(), IdemKey: key()}
		err := store.InsertDecision(context.Background(), sp, &d)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatalf("DB phải chặn người duyệt = người đề xuất: %v", err)
		}
		return sp.Rollback(context.Background())
	})
}

// ARCH §16 mkg/hub: lô có N nguồn kèm tỷ lệ; tỷ lệ tồn tại kể cả N = 1; Σ = 1 chính xác (app + DB).
func TestLotRatioInvariant(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-RATIO", "900000")
	src := func(kind string, m string) map[string]any {
		return map[string]any{"kind": kind, "ref": ids.New().String(), "mass_kg": m}
	}
	r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", map[string]any{
		"product": "straw_bale", "mass_kg": "3000", "moisture_pct": "16.50",
		"sources": []map[string]any{src("plot", "1000"), src("plot", "1000"), src("roll_count", "1000")},
	})
	mustStatus(t, r, http.StatusCreated, "nhận 3 nguồn")
	ss := arr(obj(r.body, "lot"), "sources")
	total := decimal.Zero
	var got []string
	for _, s := range ss {
		got = append(got, s["ratio"].(string))
		total = total.Add(decimal.RequireFromString(s["ratio"].(string)))
	}
	if !total.Equal(decimal.NewFromInt(1)) || strings.Join(got, ",") != "0.333334,0.333333,0.333333" {
		t.Fatalf("tỷ lệ 1:1:1 phải Σ = 1 chính xác: %v", got)
	}
	// N = 1 vẫn có tỷ lệ.
	one := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", map[string]any{
		"product": "straw_bale", "mass_kg": "10", "sources": []map[string]any{{"kind": "cooperative", "ref": ids.New().String(), "ratio": "1"}},
	})
	mustStatus(t, one, http.StatusCreated, "N = 1")
	if s := arr(obj(one.body, "lot"), "sources"); len(s) != 1 || s[0]["ratio"] != "1.000000" {
		t.Fatalf("N = 1: %s", one.raw)
	}
	// Tỷ lệ khai trực tiếp không đủ 1 → 422.
	bad := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", map[string]any{
		"product": "straw_bale", "mass_kg": "10", "sources": []map[string]any{
			{"kind": "plot", "ref": ids.New().String(), "ratio": "0.333333"}, {"kind": "plot", "ref": ids.New().String(), "ratio": "0.333333"},
			{"kind": "plot", "ref": ids.New().String(), "ratio": "0.333333"}},
	})
	if bad.status != http.StatusUnprocessableEntity || code(bad) != "hub.ratio_invalid" {
		t.Fatalf("Σ = 0.999999 phải bị từ chối: %d %s", bad.status, bad.raw)
	}
	// Lớp DB: CHECK mkg_hub.sources_valid chặn cả khi ghi vòng qua app.
	f.inTenant(func(tx pgx.Tx) error {
		for _, ratios := range [][]string{{"0.5", "0.4"}, {"0.5", "0.5000001"}, {"1.5", "-0.5"}} {
			sp, _ := tx.Begin(context.Background())
			l := domain.Lot{ID: ids.New(), TenantID: f.tenant, FacilityID: uuid.MustParse(fac), Code: "LOT-BAD", Product: domain.ProductStrawBale,
				Origin: domain.OriginReceipt, Initial: mass.MustKg("1"), EventTime: f.clk.Now(), ActorRef: op.actor, IdemKey: key()}
			for _, x := range ratios {
				l.Sources = append(l.Sources, domain.Source{Kind: domain.SourcePlot, Ref: ids.New(), Ratio: decimal.RequireFromString(x)})
			}
			err := store.InsertLot(context.Background(), sp, &l, nil)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23514" {
				t.Fatalf("DB phải từ chối tỷ lệ %v: %v", ratios, err)
			}
			_ = sp.Rollback(context.Background())
		}
		return nil
	})
}

// ARCH §16: mẻ ghi thành phần đầu vào tại thời điểm chạy; hao hụt = vào − ra ≥ 0;
// số tầng biến đổi là dữ liệu (sấy → nén); tồn = Σ lô (lô là nguồn sự thật, WH R3/A03).
func TestTransformLossProvenanceAndInventory(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-DRY", "900000")
	a, _ := f.receive(op, fac, "600", "24.00", 48*time.Hour)
	b, _ := f.receive(op, fac, "500", "22.00", 24*time.Hour)

	dry := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/batches", map[string]any{
		"kind": "drying", "inputs": []map[string]any{{"lot_id": a, "mass_kg": "600"}, {"lot_id": b, "mass_kg": "400"}},
		"outputs": []map[string]any{{"product": "dried_straw", "mass_kg": "870.5", "moisture_pct": "12.00"}},
	})
	mustStatus(t, dry, http.StatusCreated, "mẻ sấy")
	batch := obj(dry.body, "batch")
	if batch["loss_kg"] != "129.500" || batch["input_kg"] != "1000.000" {
		t.Fatalf("hao hụt sấy: %v", batch)
	}
	ins := batch["inputs"].([]any)
	if len(ins) != 2 || ins[0].(map[string]any)["moisture_pct"] != "24" || len(ins[0].(map[string]any)["sources"].([]any)) != 1 {
		t.Fatalf("mẻ phải chụp thành phần đầu vào lúc chạy: %v", ins)
	}
	dried := arr(dry.body, "outputs")[0]
	ds := dried["sources"].([]any)
	if len(ds) != 2 || ds[0].(map[string]any)["kind"] != "lot" || ds[0].(map[string]any)["ratio"] != "0.600000" || ds[1].(map[string]any)["ratio"] != "0.400000" {
		t.Fatalf("nguồn lô sấy: %v", ds)
	}
	// Tầng 2: nén lô đã sấy → nguồn là lô sấy, tỷ lệ 1.
	cmp := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/batches", map[string]any{
		"kind": "compression", "inputs": []map[string]any{{"lot_id": dried["id"], "mass_kg": "870.5"}},
		"outputs": []map[string]any{{"product": "compressed_bale", "mass_kg": "860", "code": "KIEN-0001"}},
	})
	mustStatus(t, cmp, http.StatusCreated, "mẻ nén")
	bale := arr(cmp.body, "outputs")[0]
	if bs := bale["sources"].([]any); len(bs) != 1 || bs[0].(map[string]any)["ref"] != dried["id"] || bs[0].(map[string]any)["ratio"] != "1.000000" {
		t.Fatalf("nguồn kiện nén: %v", bs)
	}
	// Hao hụt âm và lấy quá tồn bị từ chối.
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/batches", map[string]any{
		"kind": "drying", "inputs": []map[string]any{{"lot_id": b, "mass_kg": "50"}},
		"outputs": []map[string]any{{"product": "dried_straw", "mass_kg": "60"}},
	}); code(r) != "hub.negative_loss" {
		t.Fatalf("ra > vào: %d %s", r.status, r.raw)
	}
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/batches", map[string]any{
		"kind": "drying", "inputs": []map[string]any{{"lot_id": b, "mass_kg": "100.001"}},
		"outputs": []map[string]any{{"product": "dried_straw", "mass_kg": "90"}},
	}); code(r) != "hub.insufficient_stock" {
		t.Fatalf("lấy quá tồn lô: %d %s", r.status, r.raw)
	}

	inv := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/inventory", nil)
	mustStatus(t, inv, http.StatusOK, "tồn")
	lots := arr(inv.body, "lots")
	if len(lots) != 2 || str(inv.body, "capacity", "used_kg") != "960.000" {
		t.Fatalf("tồn phải = Σ lô còn (100 + 860): %s", inv.raw)
	}
	// R-22: dựng lại bảng chiếu từ sổ cho cùng kết quả.
	rb := f.approver().adm(http.MethodPost, "/v1/hub/projections/rebuild", nil)
	mustStatus(t, rb, http.StatusOK, "dựng lại")
	if again := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/inventory", nil); str(again.body, "capacity", "used_kg") != "960.000" || len(arr(again.body, "lots")) != 2 {
		t.Fatalf("dựng lại bảng chiếu lệch: %s", again.raw)
	}
	// Chuỗi băm của sổ dịch chuyển lô a nguyên vẹn.
	f.inTenant(func(tx pgx.Tx) error {
		res, err := ledger.Verify(context.Background(), tx, "mkg_hub.stock_movement", f.tenant, domain.StreamKey(uuid.MustParse(a)))
		if err == nil && (!res.OK || res.RowsChecked != 2) {
			t.Fatalf("chuỗi băm lô a: %+v", res)
		}
		return err
	})
}

// WH FN-12/FN-19 (prototype src/erp/warehouse): xuất theo rủi ro xuống cấp, không FEFO;
// phân bổ ĐỦ theo lô, không có lô thì không có hàng (WH R3).
func TestDispatchByDegradationRisk(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-RISK", "900000")
	old, _ := f.receive(op, fac, "500", "12.00", 60*24*time.Hour)
	wet, _ := f.receive(op, fac, "300", "28.00", 24*time.Hour)

	sg := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/dispatch-suggestion?mass_kg=450", nil)
	mustStatus(t, sg, http.StatusOK, "đề xuất")
	ls := arr(sg.body, "lots")
	if len(ls) != 2 || ls[0]["lot_id"] != wet || ls[0]["risk_score"] != "35.4" || ls[1]["risk_score"] != "26.7" || sg.body["risk_rule"] != "hub.risk.v1" {
		t.Fatalf("lô ẩm rủi ro cao phải đứng trước lô cũ: %s", sg.raw)
	}
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/dispatches", map[string]any{"mass_kg": "800.001"}); code(r) != "hub.insufficient_stock" {
		t.Fatalf("xuất quá tồn theo lô: %d %s", r.status, r.raw)
	}
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/dispatches", map[string]any{
		"mass_kg": "100", "allocations": []map[string]any{{"lot_id": old, "mass_kg": "90"}},
	}); code(r) != "hub.allocation_mismatch" {
		t.Fatalf("phân bổ tay lệch tổng: %d %s", r.status, r.raw)
	}
	dest := ids.New().String()
	d := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/dispatches", map[string]any{"mass_kg": "450", "destination_ref": dest})
	mustStatus(t, d, http.StatusCreated, "xuất")
	al := arr(obj(d.body, "dispatch"), "allocations")
	if len(al) != 2 || al[0]["lot_id"] != wet || al[0]["mass_kg"] != "300.000" || al[1]["lot_id"] != old || al[1]["mass_kg"] != "150.000" {
		t.Fatalf("phân bổ theo rủi ro: %v", al)
	}
	if str(obj(d.body, "dispatch"), "strategy") != "risk_suggested" || len(arr(d.body, "movements")) != 2 {
		t.Fatalf("phiếu xuất: %s", d.raw)
	}
	inv := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/inventory", nil)
	if lots := arr(inv.body, "lots"); len(lots) != 1 || lots[0]["lot_id"] != old || lots[0]["remaining_kg"] != "350.000" {
		t.Fatalf("tồn sau xuất: %s", inv.raw)
	}
}

// RB-1 / R-20: sổ dịch chuyển bất biến (vai ứng dụng và owner đều không sửa/xóa được);
// sửa sai bằng dòng bù ngược dấu có lý do, mỗi dòng bù một lần.
func TestMovementsImmutableCorrectionByCompensation(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-FIX", "900000")
	lot, mv := f.receive(op, fac, "100", "", time.Hour)

	ctx := context.Background()
	for _, pool := range []*pgxpool.Pool{f.pool, f.env.Pool(t, migrations.MKG, dbadmin.Migrator)} {
		for _, q := range []string{
			"UPDATE mkg_hub.stock_movement SET qty_kg = 1 WHERE id = '" + mv + "'",
			"DELETE FROM mkg_hub.stock_movement WHERE id = '" + mv + "'",
			"TRUNCATE mkg_hub.stock_movement",
		} {
			err := tenant.WithTenantTx(ctx, pool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err })
			if err == nil {
				t.Fatalf("sổ dịch chuyển phải chặn: %s", q)
			}
		}
	}

	if r := op.app(http.MethodPost, "/v1/hub/movements/"+mv+"/corrections", map[string]any{"reason": "sai"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn: %d %s", r.status, r.raw)
	}
	c := op.app(http.MethodPost, "/v1/hub/movements/"+mv+"/corrections", map[string]any{"reason": "Nhập trùng phiếu cân xe 65C-123.45"})
	mustStatus(t, c, http.StatusCreated, "bù")
	if c.body["kind"] != "correction" || c.body["qty_kg"] != "-100.000" || c.body["supersedes_id"] != mv || c.body["balance_after_kg"] != "0.000" {
		t.Fatalf("dòng bù: %s", c.raw)
	}
	if r := op.app(http.MethodPost, "/v1/hub/movements/"+mv+"/corrections", map[string]any{"reason": "Nhập trùng phiếu cân xe 65C-123.45"}); code(r) != "hub.already_corrected" {
		t.Fatalf("bù hai lần: %d %s", r.status, r.raw)
	}
	lv := op.app(http.MethodGet, "/v1/hub/lots/"+lot, nil)
	ms := arr(lv.body, "movements")
	if len(ms) != 2 || ms[0]["kind"] != "receive" || ms[0]["qty_kg"] != "100.000" || lv.body["remaining_kg"] != "0.000" {
		t.Fatalf("dòng gốc giữ nguyên, tồn = Σ dịch chuyển: %s", lv.raw)
	}

	// Bù làm tồn âm (hàng đã xuất) → 409; bù dòng của mẻ → 422.
	lot2, mv2 := f.receive(op, fac, "100", "", time.Hour)
	mustStatus(t, op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/dispatches", map[string]any{"mass_kg": "60"}), http.StatusCreated, "xuất 60")
	if r := op.app(http.MethodPost, "/v1/hub/movements/"+mv2+"/corrections", map[string]any{"reason": "Nhập sai khối lượng cân"}); code(r) != "hub.insufficient_stock" {
		t.Fatalf("bù làm tồn âm: %d %s", r.status, r.raw)
	}
	bt := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/batches", map[string]any{
		"kind": "drying", "inputs": []map[string]any{{"lot_id": lot2, "mass_kg": "40"}}, "outputs": []map[string]any{{"product": "dried_straw", "mass_kg": "35"}},
	})
	mustStatus(t, bt, http.StatusCreated, "mẻ")
	var outMv string
	for _, m := range arr(op.app(http.MethodGet, "/v1/hub/lots/"+lot2, nil).body, "movements") {
		if m["kind"] == "transform_out" {
			outMv = m["id"].(string)
		}
	}
	if r := op.app(http.MethodPost, "/v1/hub/movements/"+outMv+"/corrections", map[string]any{"reason": "Mẻ sấy ghi nhầm lô vào"}); code(r) != "hub.correction_unsupported" {
		t.Fatalf("bù dòng của mẻ: %d %s", r.status, r.raw)
	}
}

// RB-2: cách ly tenant ở tầng dữ liệu — tài khoản khác không thấy, không ghi được vào cơ sở MKG.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-ISO", "900000")
	lot, _ := f.receive(op, fac, "10", "", time.Hour)
	other := f.as(ids.New(), true, port.RoleHubOperator, port.RoleHubApprover)
	for _, p := range []string{"/v1/hub/facilities/" + fac, "/v1/hub/facilities/" + fac + "/inventory", "/v1/hub/lots/" + lot} {
		if r := other.app(http.MethodGet, p, nil); r.status != http.StatusNotFound {
			t.Fatalf("tenant khác đọc %s: %d %s", p, r.status, r.raw)
		}
	}
	if r := other.app(http.MethodGet, "/v1/hub/facilities", nil); len(arr(r.body, "items")) != 0 {
		t.Fatalf("tenant khác liệt kê được cơ sở: %s", r.raw)
	}
	if r := other.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/publications", nil); len(arr(r.body, "items")) != 0 {
		t.Fatalf("tenant khác thấy bản ghi van: %s", r.raw)
	}
	if r := other.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", map[string]any{
		"product": "straw_bale", "mass_kg": "1", "sources": []map[string]any{{"kind": "plot", "ref": ids.New().String(), "mass_kg": "1"}},
	}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác ghi vào cơ sở: %d %s", r.status, r.raw)
	}
	if r := other.adm(http.MethodPost, "/v1/hub/facilities/"+fac+"/versions/1/decision", map[string]any{"decision": "approved"}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác duyệt: %d %s", r.status, r.raw)
	}
	if r := other.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/capacity-snapshots", nil); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác xuất van: %d %s", r.status, r.raw)
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu; kể cả khi
// bảng idempotency mất khóa (tiến trình chết giữa chừng), sổ vẫn không ghi trùng.
func TestIdempotentReceiptReplay(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-IDEM", "900000")
	lotID := ids.New().String()
	body := map[string]any{
		"id": lotID, "product": "straw_bale", "mass_kg": "250.750",
		"sources": []map[string]any{{"kind": "plot", "ref": ids.New().String(), "mass_kg": "250.750"}},
	}
	k := key()
	first := op.do(f.app.URL, http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", k, body)
	mustStatus(t, first, http.StatusCreated, "lần đầu")
	second := op.do(f.app.URL, http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", k, body)
	if second.status != http.StatusCreated || string(second.raw) != string(first.raw) {
		t.Fatalf("gọi lặp: %d %s", second.status, second.raw)
	}
	// Mất khóa ở shared.idempotency_key → handler chạy lại, sổ trả bản ghi cũ (200).
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "DELETE FROM shared.idempotency_key WHERE idem_key = $1", k)
		return err
	})
	third := op.do(f.app.URL, http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", k, body)
	if third.status != http.StatusOK || str(third.body, "lot", "id") != lotID {
		t.Fatalf("gọi lại sau mất khóa: %d %s", third.status, third.raw)
	}
	inv := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/inventory", nil)
	if len(arr(inv.body, "lots")) != 1 || str(inv.body, "capacity", "used_kg") != "250.750" {
		t.Fatalf("không được ghi trùng: %s", inv.raw)
	}
	// id do thiết bị sinh đã dùng, khóa khác → 409.
	if r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", body); code(r) != "hub.id_reused" {
		t.Fatalf("id dùng lại: %d %s", r.status, r.raw)
	}
}

// ARCH §5: chụp sức chứa xuất qua van hub_capacity — bản ghi van chỉ có trường hợp đồng,
// sổ valve_export bất biến, băm SHA-256 khớp đúng byte đã xuất, outbox mang event_id = id bản ghi.
func TestCapacitySnapshotValveOnlyContractFields(t *testing.T) {
	f := newFixture(t)
	op := f.operator()
	fac := f.approvedFacility("HUB-CAP", "1000")
	r := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/receipts", map[string]any{
		"product": "straw_bale", "mass_kg": "1200.5", "sources": []map[string]any{{"kind": "cooperative", "ref": ids.New().String(), "mass_kg": "1200.5"}},
	})
	mustStatus(t, r, http.StatusCreated, "nhận")
	if r.body["over_capacity"] != true {
		t.Fatalf("vượt sức chứa phải được báo (vẫn ghi nhận): %s", r.raw)
	}
	mustStatus(t, f.approver().app(http.MethodPost, "/v1/hub/facilities/"+fac+"/capacity-snapshots", nil), http.StatusForbidden, "vai")
	snap := op.app(http.MethodPost, "/v1/hub/facilities/"+fac+"/capacity-snapshots", nil)
	mustStatus(t, snap, http.StatusCreated, "chụp sức chứa")
	payload := obj(snap.body, "payload")
	if strings.Join(keys(payload), ",") != strings.Join(contractKeys(t, "hub_capacity"), ",") {
		t.Fatalf("bản ghi van phải đúng trường hợp đồng: %v", keys(payload))
	}
	if payload["hub_ref"] != fac || payload["capacity_tons"] != "1.000000" || payload["used_tons"] != "1.200500" ||
		payload["available_tons"] != "0.000000" || payload["unit"] != "t" {
		t.Fatalf("payload hub_capacity: %v", payload)
	}
	if snap.body["event_type"] != port.EventCapacityPublished || snap.body["contract_version"] != json.Number("1") {
		t.Fatalf("siêu dữ liệu van: %s", snap.raw)
	}
	id := snap.body["id"].(string)
	f.inTenant(func(tx pgx.Tx) error {
		ctx := context.Background()
		var outPayload string
		var evType, producer string
		var ver int
		if err := tx.QueryRow(ctx, `SELECT event_type, schema_version, producer, payload::text FROM shared.outbox WHERE id = $1`, id).
			Scan(&evType, &ver, &producer, &outPayload); err != nil {
			t.Fatalf("outbox phải có sự kiện với event_id = id bản ghi van: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(outPayload), &m)
		if evType != "mkg.hub.capacity.published" || ver != 1 || producer != "mkg/hub" || strings.Join(keys(m), ",") != strings.Join(keys(payload), ",") {
			t.Fatalf("sự kiện outbox: %s v%d %s %v", evType, ver, producer, keys(m))
		}
		// Băm lưu kèm = SHA-256 của đúng byte payload đã kiểm (tuần tự hóa Go của struct hợp đồng).
		var sum []byte
		if err := tx.QueryRow(ctx, "SELECT payload_sha256 FROM mkg_hub.valve_export WHERE id = $1", id).Scan(&sum); err != nil {
			return err
		}
		raw, _ := json.Marshal(port.HubCapacityPublishedV1{
			HubRef: uuid.MustParse(fac), AsOf: payload["as_of"].(string), CapacityTons: "1.000000", UsedTons: "1.200500", AvailableTons: "0.000000", Unit: "t",
		})
		if want := sha256.Sum256(raw); string(want[:]) != string(sum) {
			t.Fatal("payload_sha256 không khớp byte đã xuất")
		}
		sp, _ := tx.Begin(ctx)
		if _, err := sp.Exec(ctx, "UPDATE mkg_hub.valve_export SET payload = '{}' WHERE id = $1", id); err == nil {
			t.Fatal("sổ van phải bất biến")
		}
		return sp.Rollback(ctx)
	})
	list := op.app(http.MethodGet, "/v1/hub/facilities/"+fac+"/publications?contract=hub_capacity", nil)
	if items := arr(list.body, "items"); len(items) != 2 || items[0]["id"] != id {
		t.Fatalf("lịch sử van (1 lúc duyệt + 1 chụp): %s", list.raw)
	}
}

// Van từ chối lúc chạy: trường ngoài hợp đồng (kể cả lồng nhau) không đi qua.
func TestValveRuntimeDefaultDeny(t *testing.T) {
	c, err := publication.Get(port.ContractHubFacility)
	if err != nil {
		t.Fatal(err)
	}
	good, _ := json.Marshal(port.HubFacilityPublishedV1{
		HubRef: ids.New(), Code: "HUB-1", Name: "Hub", Kind: "hub", Location: port.GeoPoint{Lat: "10.1", Lng: "105.2"},
		ProvinceCode: "92", Status: "active", CapacityTons: "1.000000", Unit: "t", EffectiveFrom: time.Now().UTC().Format(time.RFC3339),
	})
	if err := c.Validate(good); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(good, &m)
	m["owner_phone"] = "0900000000"
	bad, _ := json.Marshal(m)
	if err := c.Validate(bad); !errors.Is(err, publication.ErrDenied) {
		t.Fatalf("trường ngoài hợp đồng phải bị từ chối: %v", err)
	}
	delete(m, "owner_phone")
	m["location"] = map[string]any{"lat": json.Number("10.1"), "lng": json.Number("105.2"), "accuracy_m": 3}
	bad, _ = json.Marshal(m)
	if err := c.Validate(bad); !errors.Is(err, publication.ErrDenied) {
		t.Fatalf("trường lồng ngoài hợp đồng phải bị từ chối: %v", err)
	}
	if _, err := publication.Get("hub_price"); !errors.Is(err, publication.ErrDenied) {
		t.Fatalf("hợp đồng không tồn tại phải bị từ chối: %v", err)
	}
}
