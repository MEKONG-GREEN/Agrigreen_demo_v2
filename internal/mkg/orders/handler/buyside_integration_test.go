package handler_test

import (
	"context"
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

// proto CT-01..03, O-01/O-02/O-03: hợp đồng mua rơm — kiểm CT-02, người lập ≠ người duyệt (app/ + CHECK DB),
// một hợp đồng hiệu lực mỗi HTX chồng thời gian (CT-01), sửa = phiên bản mới, duyệt thay phiên bản cũ.
func TestContractLifecycle_CT01_CT02_SoD(t *testing.T) {
	f := newFixture(t)
	htx := ids.New()
	base := map[string]any{"htx_ref": htx.String(), "basis": "per_tonne", "unit_price": "650000", "committed_kg": "300000",
		"valid_from": f.day(-5), "valid_to": f.day(60), "max_moisture_pct": "18.50"}
	with := func(k string, v any) body {
		m := body{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}
	// CT-02 / CT-03.
	mustCode(t, f.officer.post("/v1/orders/contracts", with("unit_price", "0")), 422, "validation_failed", "giá 0")
	mustCode(t, f.officer.post("/v1/orders/contracts", with("committed_kg", "0")), 422, "validation_failed", "cam kết 0")
	mustCode(t, f.officer.post("/v1/orders/contracts", with("valid_to", f.day(-6))), 422, "validation_failed", "kết thúc trước bắt đầu")
	mustCode(t, f.officer.post("/v1/orders/contracts", with("basis", "per_kg")), 422, "validation_failed", "cách tính lạ")
	mustCode(t, f.approver.post("/v1/orders/contracts", base), 403, "forbidden", "sales_approver không lập hợp đồng")

	r := f.officer.post("/v1/orders/contracts", base)
	mustStatus(t, r, http.StatusCreated, "lập")
	id := str(r.body, "contract_id")
	v1 := arr(r.body, "versions")[0]
	if str(v1, "status") != "pending" || str(v1, "code") == "" || str(v1, "unit_price") != "650000.0000" || str(v1, "max_moisture_pct") != "18.50" {
		t.Fatalf("phiên bản 1: %v", v1)
	}
	// R-56: người lập có cả vai duyệt vẫn không tự duyệt được.
	both := f.as(f.tenant, port.RoleSalesOfficer, port.RoleSalesApprover)
	r2 := both.post("/v1/orders/contracts", with("htx_ref", ids.New().String()))
	mustStatus(t, r2, http.StatusCreated, "lập bởi người có hai vai")
	mustCode(t, both.post("/v1/orders/contracts/"+str(r2.body, "contract_id")+"/versions/1/decision", map[string]any{"decision": "approved"}),
		403, "self_approval_forbidden", "tự duyệt hợp đồng")
	// R-56 lớp hai: CHECK DB chặn dòng approved có actor = proposer.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `SAVEPOINT s1`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(context.Background(), `INSERT INTO mkg_orders.contract_event (id, tenant_id, stream_key, contract_id, version, kind,
		    status, code, htx_ref, basis, unit_price, committed_kg, valid_from, valid_to, proposer_ref, approver_ref, event_time, actor_ref,
		    idem_key, rule_version)
		  VALUES ($1, $2, 'contract:x', $3, 1, 'approved', 'active', 'HDR99999', $4, 'per_tonne', 1, 1, current_date, current_date, $5, $5,
		    now(), $5, $6, 'x')`, ids.New(), f.tenant, ids.New(), ids.New(), both.actor, key())
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatalf("CHECK R-56 phải chặn tự duyệt ở DB: %v", err)
		}
		_, err = tx.Exec(context.Background(), `ROLLBACK TO SAVEPOINT s1`)
		return err
	})
	d := f.approver.post("/v1/orders/contracts/"+id+"/versions/1/decision", map[string]any{"decision": "approved"})
	mustStatus(t, d, http.StatusCreated, "duyệt")
	if s := str(arr(d.body, "versions")[0], "status"); s != "active" {
		t.Fatalf("sau duyệt: %s", s)
	}
	// CT-01: hợp đồng thứ hai chồng thời gian bị từ chối; không chồng thì được.
	mustCode(t, f.officer.post("/v1/orders/contracts", with("valid_from", f.day(30))), 409, "orders.contract_overlap", "CT-01 chồng lấn")
	mustStatus(t, f.officer.post("/v1/orders/contracts", with("valid_from", f.day(61)).withTo(f.day(90))), http.StatusCreated, "không chồng")

	// Sửa giá = phiên bản 2 chờ duyệt; duyệt → v1 superseded, v2 active; ContractFor trả v2.
	rv := f.officer.post("/v1/orders/contracts/"+id+"/revisions", with("unit_price", "700000").withReason("Điều chỉnh giá theo phụ lục 01"))
	mustStatus(t, rv, http.StatusCreated, "sửa")
	mustCode(t, f.officer.post("/v1/orders/contracts/"+id+"/revisions", with("unit_price", "710000").withReason("Lần sửa thứ hai trùng")),
		409, "orders.contract_transition", "đang có phiên bản chờ duyệt")
	d2 := f.approver.post("/v1/orders/contracts/"+id+"/versions/2/decision", map[string]any{"decision": "approved"})
	mustStatus(t, d2, http.StatusCreated, "duyệt v2")
	vs := arr(d2.body, "versions")
	if len(vs) != 2 || str(vs[0], "status") != "active" || str(vs[1], "status") != "superseded" {
		t.Fatalf("phiên bản sau duyệt v2: %v", vs)
	}
	terms, err := f.svc.ContractFor(context.Background(), f.tenant, htx, f.clk.Now())
	if err != nil || terms.Version != 2 || terms.UnitPrice.String() != "700000.0000" || terms.Basis != port.PricingPerTonne ||
		terms.MaxMoisturePct == nil || *terms.MaxMoisturePct != "18.50" {
		t.Fatalf("ContractFor: %+v %v", terms, err)
	}
	// Chấm dứt → không còn hợp đồng hiệu lực.
	mustStatus(t, f.approver.post("/v1/orders/contracts/"+id+"/terminate", map[string]any{"reason": "HTX ngừng cung cấp rơm vụ này"}),
		http.StatusCreated, "chấm dứt")
	if _, err := f.svc.ContractFor(context.Background(), f.tenant, htx, f.clk.Now()); !errors.Is(err, port.ErrNoContract) {
		t.Fatalf("sau chấm dứt phải ErrNoContract: %v", err)
	}
}

type body map[string]any

func (b body) withTo(v string) body     { b["valid_to"] = v; return b }
func (b body) withReason(v string) body { b["reason"] = v; return b }

// O-10 (proto PM-03): Contracts.ContractFor không có hợp đồng → ErrNoContract (không đoán giá); đơn mua
// rơm không hợp đồng mà không nhập giá → 422 orders.price_required; có hợp đồng → giá lấy từ hợp đồng.
func TestContractForErrNoContract_O10(t *testing.T) {
	f := newFixture(t)
	htx := ids.New()
	ctx := context.Background()
	if _, err := f.svc.ContractFor(ctx, f.tenant, htx, f.clk.Now()); !errors.Is(err, port.ErrNoContract) {
		t.Fatalf("HTX chưa có hợp đồng: %v", err)
	}
	mustCode(t, f.officer.get("/v1/orders/contract-terms?htx_ref="+htx.String()), 404, "orders.no_contract", "HTTP không hợp đồng")
	mustCode(t, f.officer.post("/v1/orders/purchase-orders", map[string]any{"category": "straw", "supplier_ref": htx.String(), "unit": "kg",
		"ordered_qty": "10000"}), 422, "orders.price_required", "không đoán giá")
	f.contract(htx, "per_bale", "12000", -1, 30)
	// Ngoài khoảng hiệu lực → vẫn ErrNoContract.
	if _, err := f.svc.ContractFor(ctx, f.tenant, htx, f.clk.Now().AddDate(0, 0, 45)); !errors.Is(err, port.ErrNoContract) {
		t.Fatalf("ngoài hiệu lực: %v", err)
	}
	// Tenant khác không thấy hợp đồng (RB-2).
	if _, err := f.svc.ContractFor(ctx, ids.New(), htx, f.clk.Now()); !errors.Is(err, port.ErrNoContract) {
		t.Fatalf("tenant khác: %v", err)
	}
	tm := f.officer.get("/v1/orders/contract-terms?htx_ref=" + htx.String() + "&on=" + f.day(0))
	mustStatus(t, tm, http.StatusOK, "điều khoản")
	if str(tm.body, "basis") != "per_bale" || str(tm.body, "unit_price") != "12000.0000" {
		t.Fatalf("điều khoản: %s", tm.raw)
	}
	mustCode(t, f.officer.post("/v1/orders/purchase-orders", map[string]any{"category": "straw", "supplier_ref": htx.String(), "unit": "kg",
		"ordered_qty": "10000"}), 422, "orders.basis_mismatch", "đơn vị khác cách tính hợp đồng")
	p := f.officer.post("/v1/orders/purchase-orders", map[string]any{"category": "straw", "supplier_ref": htx.String(), "ordered_qty": "400"})
	mustStatus(t, p, http.StatusCreated, "đơn mua theo hợp đồng")
	po := p.body["purchase_order"].(map[string]any)
	if str(po, "unit") != "bale" || str(po, "unit_price") != "12000.0000" || str(po, "price_source") != "contract" || str(po, "contract_ref") == "" {
		t.Fatalf("đơn mua theo hợp đồng: %v", po)
	}
}

// proto createPurchaseOrder, O-04, O-03: tấn > 0, giá > 0; draft → approved; người lập ≠ người duyệt;
// hủy có lý do.
func TestPurchaseOrderRules_O04(t *testing.T) {
	f := newFixture(t)
	sup := ids.New().String()
	mustCode(t, f.officer.post("/v1/orders/purchase-orders", map[string]any{"category": "other", "supplier_ref": sup, "unit": "kg",
		"ordered_qty": "0", "unit_price": "1000"}), 422, "validation_failed", "khối lượng 0")
	mustCode(t, f.officer.post("/v1/orders/purchase-orders", map[string]any{"category": "other", "supplier_ref": sup, "unit": "kg",
		"ordered_qty": "10", "unit_price": "0"}), 422, "validation_failed", "giá 0")
	mustCode(t, f.officer.post("/v1/orders/purchase-orders", map[string]any{"category": "other", "supplier_ref": sup, "unit": "bale",
		"ordered_qty": "10.5", "unit_price": "10"}), 422, "validation_failed", "cuộn lẻ")
	both := f.as(f.tenant, port.RoleSalesOfficer, port.RoleSalesApprover)
	r := both.post("/v1/orders/purchase-orders", map[string]any{"category": "other", "supplier_ref": sup, "unit": "kg", "ordered_qty": "1000",
		"unit_price": "2000000"})
	mustStatus(t, r, http.StatusCreated, "lập")
	id := str(r.body, "purchase_order", "po_id")
	mustCode(t, both.post("/v1/orders/purchase-orders/"+id+"/decision", map[string]any{"decision": "approved"}), 403, "self_approval_forbidden",
		"tự duyệt đơn mua")
	mustCode(t, f.approver.post("/v1/orders/purchase-orders/"+id+"/cancel", map[string]any{"reason": "ngắn"}), 422, "validation_failed", "lý do ngắn")
	mustStatus(t, f.approver.post("/v1/orders/purchase-orders/"+id+"/decision", map[string]any{"decision": "approved"}), 201, "duyệt")
	mustCode(t, f.approver.post("/v1/orders/purchase-orders/"+id+"/decision", map[string]any{"decision": "approved"}), 409, "orders.po_transition",
		"duyệt lại")
	c := f.approver.post("/v1/orders/purchase-orders/"+id+"/cancel", map[string]any{"reason": "Nhà cung cấp hủy giao hàng"})
	mustStatus(t, c, http.StatusCreated, "hủy")
	if str(c.body, "purchase_order", "status") != "cancelled" {
		t.Fatalf("hủy: %s", c.raw)
	}
}

// T-2 (g3-acceptance "ngoài G3 → G4"), proto createGoodsReceipt, đơn theo TẤN: phiếu nhận chờ phiếu cân
// HUB_IN; consumer mkg.orders.weigh_ticket gắn phiếu và kiểm ↔ đơn mua; lệch > 3 % → cần biên bản chênh lệch
// + ghi chú kiểm toán, KHÔNG chặn duyệt. O-11: rơm KHÔNG ghi phải trả ở đây.
func TestReceiptVsPOVariancePerTonne_T2(t *testing.T) {
	f := newFixture(t)
	htx := ids.New()
	f.contract(htx, "per_tonne", "650000", -1, 30)
	po := f.po(map[string]any{"category": "straw", "supplier_ref": htx.String(), "ordered_qty": "10000"})
	lot := ids.New()
	r := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": lot.String()})
	mustStatus(t, r, http.StatusCreated, "lập phiếu nhận")
	rid := str(r.body, "receipt", "receipt_id")
	if str(r.body, "receipt", "status") != "awaiting_weigh" {
		t.Fatalf("chưa có phiếu cân: %s", r.raw)
	}
	mustCode(t, f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": ids.New().String(), "received_qty": "9000"}),
		422, "validation_failed", "rơm theo kg không nhập tay")
	mustCode(t, f.approver.post("/v1/orders/receipts/"+rid+"/decision", map[string]any{"decision": "approved"}), 409, "orders.receipt_not_weighed",
		"duyệt khi chưa cân")
	// HUB_IN 10 500 kg → +5,00 % > 3 %.
	tk := f.weigh(lot, mport.PointHubIn, "10500", nil)
	g := f.officer.get("/v1/orders/receipts/" + rid)
	rc := g.body["receipt"].(map[string]any)
	if str(rc, "status") != "pending_approval" || str(rc, "ticket_ref") != tk.String() || str(rc, "received_qty") != "10500.000" ||
		str(rc, "variance_pct") != "5.00" || str(rc, "threshold_pct") != "3.00" || str(rc, "needs_variance_report") != "true" ||
		str(rc, "audit_note") == "" || str(rc, "param_version") != port.ParamReceiptVariancePct+":default" {
		t.Fatalf("kiểm ↔ đơn mua: %v", rc)
	}
	check, err := f.svc.ReceiptCheck(context.Background(), f.tenant, uuid.MustParse(rid))
	if err != nil || check == nil || !check.NeedsVarianceReport || check.VariancePct != "5.00" || check.OrderedKg.String() != "10000.000" ||
		check.ReceivedKg.String() != "10500.000" || check.Unit != "kg" || check.ParamVersion != 0 {
		t.Fatalf("port ReceiptCheck: %+v %v", check, err)
	}
	mustStatus(t, f.officer.post("/v1/orders/receipts/"+rid+"/variance-report", map[string]any{"note": "Biên bản chênh lệch: HTX giao dư 500 kg",
		"file_ref": ids.New().String()}), http.StatusCreated, "biên bản chênh lệch")
	// Không chặn: duyệt được dù vượt ngưỡng.
	a := f.approver.post("/v1/orders/receipts/"+rid+"/decision", map[string]any{"decision": "approved"})
	mustStatus(t, a, http.StatusCreated, "duyệt vượt ngưỡng (không chặn)")
	if str(a.body, "receipt", "status") != "approved" || a.body["receipt"].(map[string]any)["payable"] != nil {
		t.Fatalf("rơm không ghi phải trả ở orders: %s", a.raw)
	}
	if n := f.count("SELECT count(*) FROM mkg_orders.payable_event"); n != 0 {
		t.Fatalf("O-11: phải trả mua rơm chỉ từ quyết toán costing, có %d dòng", n)
	}
	// Trong ngưỡng: 10 200 kg → +2,00 % → không cần biên bản. Phiếu cân về TRƯỚC phiếu nhận.
	lot2 := ids.New()
	f.weigh(lot2, mport.PointHubIn, "10200", nil)
	r2 := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": lot2.String()})
	mustStatus(t, r2, http.StatusCreated, "phiếu nhận 2")
	if rc := r2.body["receipt"].(map[string]any); str(rc, "variance_pct") != "2.00" || str(rc, "needs_variance_report") != "false" ||
		str(rc, "status") != "pending_approval" {
		t.Fatalf("trong ngưỡng: %v", rc)
	}
	// Đúng biên 3,00 % → không vượt (chỉ > ngưỡng mới cần biên bản).
	lot3 := ids.New()
	f.weigh(lot3, mport.PointHubIn, "9700", nil)
	r3 := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": lot3.String()})
	if rc := r3.body["receipt"].(map[string]any); str(rc, "variance_pct") != "-3.00" || str(rc, "needs_variance_report") != "false" {
		t.Fatalf("đúng biên: %v", rc)
	}
}

// T-2, proto createGoodsReceipt, đơn theo CUỘN: kiểm theo số cuộn đếm lúc nhận; phiếu cân HUB_IN gắn để tham
// chiếu; sửa số cuộn trước duyệt = bản ghi bù có lý do (RB-1).
func TestReceiptVsPOVariancePerBale_T2(t *testing.T) {
	f := newFixture(t)
	htx := ids.New()
	f.contract(htx, "per_bale", "12000", -1, 30)
	po := f.po(map[string]any{"category": "straw", "supplier_ref": htx.String(), "ordered_qty": "500"})
	lot := ids.New()
	mustCode(t, f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": lot.String()}), 422, "validation_failed",
		"theo cuộn bắt buộc số cuộn")
	r := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": lot.String(), "received_qty": "480"})
	mustStatus(t, r, http.StatusCreated, "nhận 480 cuộn")
	rid := str(r.body, "receipt", "receipt_id")
	rc := r.body["receipt"].(map[string]any)
	if str(rc, "variance_pct") != "-4.00" || str(rc, "needs_variance_report") != "true" || str(rc, "qty_source") != "count" ||
		str(rc, "received_qty") != "480" {
		t.Fatalf("theo cuộn: %v", rc)
	}
	// Phiếu cân HUB_IN của lô gắn vào phiếu nhận (không đổi số cuộn).
	f.weigh(lot, mport.PointHubIn, "9600", nil)
	g := f.officer.get("/v1/orders/receipts/" + rid).body["receipt"].(map[string]any)
	if str(g, "ticket_kg") != "9600.000" || str(g, "received_qty") != "480" || str(g, "variance_pct") != "-4.00" {
		t.Fatalf("gắn phiếu cân vào đơn theo cuộn: %v", g)
	}
	// Bù: đếm lại 490 cuộn → −2,00 %, không còn cần biên bản; dòng cũ còn nguyên trong sổ.
	c := f.officer.post("/v1/orders/receipts/"+rid+"/corrections", map[string]any{"received_qty": "490", "reason": "Đếm lại tại cổng hub"})
	mustStatus(t, c, http.StatusCreated, "bù số cuộn")
	if rc := c.body["receipt"].(map[string]any); str(rc, "variance_pct") != "-2.00" || str(rc, "needs_variance_report") != "false" {
		t.Fatalf("sau bù: %v", rc)
	}
	if n := f.count(`SELECT count(*) FROM mkg_orders.receipt_event WHERE receipt_id = $1 AND kind = 'corrected'
	    AND supersedes_id IS NOT NULL AND correction_reason IS NOT NULL`, uuid.MustParse(rid)); n != 1 {
		t.Fatalf("bản ghi bù: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM mkg_orders.receipt_event WHERE receipt_id = $1 AND received_qty = 480`, uuid.MustParse(rid)); n < 1 {
		t.Fatal("dòng gốc phải còn nguyên")
	}
	check, err := f.svc.ReceiptCheck(context.Background(), f.tenant, uuid.MustParse(rid))
	if err != nil || check.Unit != "bale" || check.OrderedBales == nil || *check.OrderedBales != 500 || *check.ReceivedBales != 490 ||
		check.ReceivedKg.String() != "9600.000" {
		t.Fatalf("port ReceiptCheck theo cuộn: %+v %v", check, err)
	}
}

// Ngưỡng 3 % là tham số có phiên bản (tạm, chờ khách): người đặt ≠ người duyệt (R-56); phiếu nhận ghim
// phiên bản ngưỡng đã áp.
func TestReceiptVarianceParamVersioned(t *testing.T) {
	f := newFixture(t)
	path := "/v1/orders/params/" + port.ParamReceiptVariancePct
	g := f.officer.get(path)
	mustStatus(t, g, http.StatusOK, "đọc tham số")
	if str(g.body, "effective_value") != "3" || str(g.body, "provisional") != "true" {
		t.Fatalf("mặc định tạm: %s", g.raw)
	}
	p := f.officer.post(path+"/versions", map[string]any{"value": "5", "reason": "Khách chốt dung sai 5 % cho vụ đông xuân"})
	mustStatus(t, p, http.StatusCreated, "đề xuất")
	ver := str(p.body, "param", "version_id")
	both := f.as(f.tenant, port.RoleSalesOfficer, port.RoleSalesApprover)
	p2 := both.post(path+"/versions", map[string]any{"value": "4", "reason": "Đề xuất thử để tự duyệt"})
	mustCode(t, both.post(path+"/versions/"+str(p2.body, "param", "version_id")+"/decision", map[string]any{"decision": "approved"}), 403,
		"self_approval_forbidden", "tự duyệt tham số")
	mustStatus(t, f.approver.post(path+"/versions/"+ver+"/decision", map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt")
	po := f.po(map[string]any{"category": "other", "supplier_ref": ids.New().String(), "unit": "kg", "ordered_qty": "1000", "unit_price": "100000"})
	r := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"received_qty": "1040"})
	rc := r.body["receipt"].(map[string]any)
	if str(rc, "variance_pct") != "4.00" || str(rc, "needs_variance_report") != "false" || str(rc, "param_version") != port.ParamReceiptVariancePct+":v1" {
		t.Fatalf("ngưỡng phiên bản 1: %v", rc)
	}
}

// O-11: đơn mua KHÁC — đúng MỘT khoản phải trả từ phiếu nhận đã duyệt (không ghi trùng như prototype);
// duyệt lặp (cùng khóa) không ghi thêm; hạn 15 ngày (tạm, chờ khách).
func TestOtherPurchasePayableSingleSource_O11(t *testing.T) {
	f := newFixture(t)
	po := f.po(map[string]any{"category": "other", "supplier_ref": ids.New().String(), "unit": "kg", "ordered_qty": "1000", "unit_price": "2000000"})
	r := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"received_qty": "999.5"})
	mustStatus(t, r, http.StatusCreated, "phiếu nhận")
	rid := str(r.body, "receipt", "receipt_id")
	k := key()
	a := f.approver.do(http.MethodPost, "/v1/orders/receipts/"+rid+"/decision", k, map[string]any{"decision": "approved"})
	mustStatus(t, a, http.StatusCreated, "duyệt")
	b := f.approver.do(http.MethodPost, "/v1/orders/receipts/"+rid+"/decision", k, map[string]any{"decision": "approved"})
	if b.status != http.StatusCreated && b.status != http.StatusOK {
		t.Fatalf("duyệt lặp: %d", b.status)
	}
	pay := a.body["receipt"].(map[string]any)["payable"].(map[string]any)
	// 999,5 kg × 2 000 000 đ/tấn = 1 999 000,00 đ.
	if str(pay, "amount") != "1999000.00" || str(pay, "payment_term_days") != "15" || str(pay, "due_date") != f.clk.Now().Add(7*time.Hour).AddDate(0, 0, 15).Format("2006-01-02") {
		t.Fatalf("phải trả: %v", pay)
	}
	if n := f.count("SELECT count(*) FROM mkg_orders.payable_event WHERE receipt_id = $1", uuid.MustParse(rid)); n != 1 {
		t.Fatalf("đúng một khoản phải trả: %d", n)
	}
	l := f.officer.get("/v1/orders/payables?po_id=" + po)
	if items := arr(l.body, "items"); len(items) != 1 || str(items[0], "amount") != "1999000.00" {
		t.Fatalf("danh sách phải trả: %s", l.raw)
	}
}
