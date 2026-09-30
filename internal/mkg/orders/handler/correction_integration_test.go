package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// RB-1 (G4 tích hợp mục 4): phiếu HUB_IN bị bù ở measurement SAU KHI phiếu nhận đã duyệt → orders không sửa
// phiếu nhận / khoản phải trả đã ghi mà ghi BẢN BÙ: phiếu nhận `corrected` (supersedes_id = dòng hiệu lực, vẫn
// approved, người duyệt giữ nguyên, kiểm ↔ đơn mua lại — chỉ cảnh báo) và khoản phải trả bù (supersedes_id =
// khoản cũ, số tiền theo tấn mới, giữ hạn đã chốt). Rơm: chỉ bản bù phiếu nhận, không phải trả ở orders (O-11).
func TestHubInCorrectionAfterReceiptApproved_RB1(t *testing.T) {
	f := newFixture(t)
	po := f.po(map[string]any{"category": "other", "supplier_ref": ids.New().String(), "unit": "kg", "ordered_qty": "1000", "unit_price": "2000000"})
	lot := ids.New()
	tk1 := f.weigh(lot, mport.PointHubIn, "1000", nil)
	r := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": lot.String()})
	mustStatus(t, r, http.StatusCreated, "phiếu nhận")
	rid := str(r.body, "receipt", "receipt_id")
	a := f.approver.post("/v1/orders/receipts/"+rid+"/decision", map[string]any{"decision": "approved"})
	mustStatus(t, a, http.StatusCreated, "duyệt")
	pay1 := a.body["receipt"].(map[string]any)["payable"].(map[string]any)
	if str(pay1, "amount") != "2000000.00" {
		t.Fatalf("phải trả ban đầu: %v", pay1)
	}
	approver := str(a.body, "receipt", "approver_ref")

	// Phiếu bù 1 100 kg thay phiếu 1 000 kg.
	tk2 := f.weigh(lot, mport.PointHubIn, "1100", &tk1)
	g := f.officer.get("/v1/orders/receipts/" + rid)
	rc := g.body["receipt"].(map[string]any)
	if str(rc, "status") != "approved" || str(rc, "ticket_ref") != tk2.String() || str(rc, "received_qty") != "1100.000" ||
		str(rc, "variance_pct") != "10.00" || str(rc, "needs_variance_report") != "true" || str(rc, "approver_ref") != approver {
		t.Fatalf("bản bù phiếu nhận: %v", rc)
	}
	pay := rc["payable"].(map[string]any)
	if str(pay, "amount") != "2200000.00" || str(pay, "qty") != "1100.000" || str(pay, "supersedes_id") != str(pay1, "payable_id") ||
		str(pay, "correction_reason") == "" || str(pay, "due_date") != str(pay1, "due_date") {
		t.Fatalf("bản bù khoản phải trả: %v", pay)
	}
	rID := uuid.MustParse(rid)
	if n := f.count(`SELECT count(*) FROM mkg_orders.receipt_event WHERE receipt_id = $1 AND kind = 'corrected'
	    AND supersedes_id IS NOT NULL AND correction_reason IS NOT NULL AND status = 'approved'`, rID); n != 1 {
		t.Fatalf("một bản bù phiếu nhận, có %d", n)
	}
	// Khoản cũ không bị sửa (sổ chỉ ghi thêm); hai dòng trong stream payable:<receipt>.
	if n := f.count(`SELECT count(*) FROM mkg_orders.payable_event WHERE receipt_id = $1`, rID); n != 2 {
		t.Fatalf("hai dòng phải trả (gốc + bù), có %d", n)
	}
	if n := f.count(`SELECT count(*) FROM mkg_orders.payable_event WHERE id = $1 AND amount = 2000000.00`, uuid.MustParse(str(pay1, "payable_id"))); n != 1 {
		t.Fatal("khoản phải trả gốc phải giữ nguyên")
	}
	// Giao lặp phiếu bù (R-61) → không ghi thêm.
	f.ticket(f.tenant, mport.WeighTicketRecordedV1{TicketID: tk2, LotRef: lot, Point: mport.PointHubIn, Route: mport.RouteHub,
		Source: mport.SourceEdge, MassKg: mustKg("1100"), EventTime: f.clk.Now(), SupersedesID: &tk1})
	if n := f.count(`SELECT count(*) FROM mkg_orders.payable_event WHERE receipt_id = $1`, rID); n != 2 {
		t.Fatalf("giao lặp ghi thêm phải trả: %d", n)
	}
	l := f.officer.get("/v1/orders/payables?po_id=" + po)
	if items := arr(l.body, "items"); len(items) != 2 || str(items[1], "supersedes_id") != str(pay1, "payable_id") {
		t.Fatalf("danh sách phải trả: %s", l.raw)
	}

	// Rơm: bản bù phiếu nhận, KHÔNG có phải trả ở orders.
	htx := ids.New()
	f.contract(htx, "per_tonne", "650000", -1, 30)
	spo := f.po(map[string]any{"category": "straw", "supplier_ref": htx.String(), "ordered_qty": "10000"})
	slot := ids.New()
	st1 := f.weigh(slot, mport.PointHubIn, "10000", nil)
	sr := f.officer.post("/v1/orders/purchase-orders/"+spo+"/receipts", map[string]any{"lot_ref": slot.String()})
	srid := str(sr.body, "receipt", "receipt_id")
	mustStatus(t, f.approver.post("/v1/orders/receipts/"+srid+"/decision", map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt rơm")
	f.weigh(slot, mport.PointHubIn, "9500", &st1)
	sg := f.officer.get("/v1/orders/receipts/" + srid)
	if rc := sg.body["receipt"].(map[string]any); str(rc, "status") != "approved" || str(rc, "received_qty") != "9500.000" || rc["payable"] != nil {
		t.Fatalf("rơm: %v", rc)
	}
	if n := f.count(`SELECT count(*) FROM mkg_orders.payable_event WHERE receipt_id = $1`, uuid.MustParse(srid)); n != 0 {
		t.Fatalf("O-11: rơm không có phải trả ở orders, có %d", n)
	}
}
