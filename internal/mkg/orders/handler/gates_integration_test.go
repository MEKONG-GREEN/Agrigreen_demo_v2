package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ledgers là mọi sổ của mkg_orders (mkg 000016).
var ledgers = []string{"contract_event", "po_event", "receipt_event", "payable_event", "order_event", "delivery_event", "ticket_event",
	"param_version"}

// flow chạy toàn bộ đường ghi của module cho tenant tid (mọi sổ và bảng chiếu có dữ liệu): hợp đồng, tham
// số, đơn mua rơm + phiếu nhận + HUB_IN, đơn mua khác + phải trả, đơn bán + giao nhận + CUSTOMER_IN + chấp nhận.
// Không dùng t (an toàn trong goroutine).
func (f *fixture) flow(ctx context.Context, tid uuid.UUID, officer, approver string, round int) error {
	post := func(tok, path string, body any, want ...int) (map[string]any, error) {
		return lkDo(ctx, f.srv.URL, tok, http.MethodPost, path, body, want...)
	}
	sub := func(m map[string]any, k, id string) string {
		o, _ := m[k].(map[string]any)
		s, _ := o[id].(string)
		return s
	}
	now := f.clk.Now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	htx := ids.New()
	c, err := post(officer, "/v1/orders/contracts", map[string]any{"htx_ref": htx.String(), "basis": "per_tonne", "unit_price": "650000",
		"committed_kg": "100000", "valid_from": day(-1), "valid_to": day(30)}, 201)
	if err != nil {
		return err
	}
	cid, _ := c["contract_id"].(string)
	if _, err := post(approver, "/v1/orders/contracts/"+cid+"/versions/1/decision", map[string]any{"decision": "approved"}, 201); err != nil {
		return err
	}
	pv, err := post(officer, "/v1/orders/params/"+port.ParamPaymentTermDays+"/versions", map[string]any{"value": "15",
		"reason": fmt.Sprintf("Giữ hạn thanh toán 15 ngày vòng %d", round)}, 201)
	if err != nil {
		return err
	}
	if _, err := post(approver, "/v1/orders/params/"+port.ParamPaymentTermDays+"/versions/"+sub(pv, "param", "version_id")+"/decision",
		map[string]any{"decision": "approved"}, 201); err != nil {
		return err
	}
	// Đơn mua rơm + phiếu nhận + HUB_IN.
	p1, err := post(officer, "/v1/orders/purchase-orders", map[string]any{"category": "straw", "supplier_ref": htx.String(), "ordered_qty": "10000"}, 201)
	if err != nil {
		return err
	}
	po1 := sub(p1, "purchase_order", "po_id")
	if _, err := post(approver, "/v1/orders/purchase-orders/"+po1+"/decision", map[string]any{"decision": "approved"}, 201); err != nil {
		return err
	}
	lot1 := ids.New()
	if _, err := post(officer, "/v1/orders/purchase-orders/"+po1+"/receipts", map[string]any{"lot_ref": lot1.String()}, 201); err != nil {
		return err
	}
	if err := f.deliver(tid, mport.WeighTicketRecordedV1{TicketID: ids.New(), LotRef: lot1, Point: mport.PointHubIn, Route: mport.RouteHub,
		Source: mport.SourceEdge, MassKg: mustKg("10400"), EventTime: now.Add(-time.Minute)}); err != nil {
		return err
	}
	// Đơn mua khác + phải trả.
	p2, err := post(officer, "/v1/orders/purchase-orders", map[string]any{"category": "other", "supplier_ref": ids.New().String(), "unit": "kg",
		"ordered_qty": "100", "unit_price": "500000"}, 201)
	if err != nil {
		return err
	}
	po2 := sub(p2, "purchase_order", "po_id")
	if _, err := post(approver, "/v1/orders/purchase-orders/"+po2+"/decision", map[string]any{"decision": "approved"}, 201); err != nil {
		return err
	}
	r2, err := post(officer, "/v1/orders/purchase-orders/"+po2+"/receipts", map[string]any{"received_qty": "100"}, 201)
	if err != nil {
		return err
	}
	if _, err := post(approver, "/v1/orders/receipts/"+sub(r2, "receipt", "receipt_id")+"/decision", map[string]any{"decision": "approved"}, 201); err != nil {
		return err
	}
	// Đơn bán + giao nhận + CUSTOMER_IN + chấp nhận.
	so, err := post(officer, "/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": "dried_straw",
		"ordered_kg": "5000", "list_price": "900000", "unit_price": "900000"}, 201)
	if err != nil {
		return err
	}
	oid := sub(so, "sales_order", "order_id")
	if _, err := post(officer, "/v1/orders/sales-orders/"+oid+"/confirm", nil, 201); err != nil {
		return err
	}
	lot2 := ids.New()
	d, err := post(officer, "/v1/orders/sales-orders/"+oid+"/deliveries", map[string]any{"lot_ref": lot2.String(), "route": "HUB",
		"planned_date": day(1), "planned_kg": "5000"}, 201)
	if err != nil {
		return err
	}
	did := sub(d, "delivery", "delivery_id")
	if _, err := post(officer, "/v1/orders/deliveries/"+did+"/dispatch", map[string]any{}, 201); err != nil {
		return err
	}
	if err := f.deliver(tid, mport.WeighTicketRecordedV1{TicketID: ids.New(), LotRef: lot2, Point: mport.PointCustomerIn, Route: mport.RouteHub,
		Source: mport.SourceCustomerIntegration, MassKg: mustKg("4990"), EventTime: now.Add(-time.Minute)}); err != nil {
		return err
	}
	_, err = post(officer, "/v1/orders/deliveries/"+did+"/accept", map[string]any{"evidence": map[string]any{"file_ref": ids.New().String(),
		"kind": "epod"}}, 201)
	return err
}

func (f *fixture) tokens(tid uuid.UUID) (officer, approver string) {
	return f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{port.RoleSalesOfficer}}),
		f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{port.RoleSalesApprover}})
}

// R-51: POST lặp cùng Idempotency-Key trả cùng tài nguyên, không ghi thêm dòng sổ (middleware + idem_key
// của sổ); R-61: giao lặp phiếu cân không ghi thêm.
func TestIdempotentReplay_R51_R61(t *testing.T) {
	f := newFixture(t)
	k := key()
	b := map[string]any{"customer_ref": ids.New().String(), "product": "straw_bale", "ordered_kg": "1000", "unit_price": "800000"}
	a := f.officer.do(http.MethodPost, "/v1/orders/sales-orders", k, b)
	mustStatus(t, a, http.StatusCreated, "lần 1")
	a2 := f.officer.do(http.MethodPost, "/v1/orders/sales-orders", k, b)
	if (a2.status != http.StatusCreated && a2.status != http.StatusOK) || str(a2.body, "sales_order", "order_id") != str(a.body, "sales_order", "order_id") {
		t.Fatalf("phát lại: %d %s", a2.status, a2.raw)
	}
	if n := f.count("SELECT count(*) FROM mkg_orders.order_event"); n != 1 {
		t.Fatalf("một dòng sổ: %d", n)
	}
	// Lớp hai (không qua middleware): gọi use case lặp cùng idem_key → replayed, không thêm dòng.
	ck := key()
	cb := map[string]any{"htx_ref": ids.New().String(), "basis": "per_bale", "unit_price": "12000", "committed_kg": "1000",
		"valid_from": f.day(0), "valid_to": f.day(10)}
	c1 := f.officer.do(http.MethodPost, "/v1/orders/contracts", ck, cb)
	mustStatus(t, c1, http.StatusCreated, "hợp đồng lần 1")
	c2 := f.officer.do(http.MethodPost, "/v1/orders/contracts", ck, cb)
	if str(c2.body, "contract_id") != str(c1.body, "contract_id") {
		t.Fatalf("hợp đồng phát lại: %s", c2.raw)
	}
	if n := f.count("SELECT count(*) FROM mkg_orders.contract_event"); n != 1 {
		t.Fatalf("hợp đồng một dòng sổ: %d", n)
	}
	lot, tk := ids.New(), ids.New()
	w := mport.WeighTicketRecordedV1{TicketID: tk, LotRef: lot, Point: mport.PointHubIn, Route: mport.RouteHub, Source: mport.SourceEdge,
		MassKg: mustKg("1000"), EventTime: f.clk.Now()}
	f.ticket(f.tenant, w)
	f.ticket(f.tenant, w)
	if n := f.count("SELECT count(*) FROM mkg_orders.ticket_event WHERE ticket_id = $1", tk); n != 1 {
		t.Fatalf("giao lặp phiếu cân: %d dòng", n)
	}
}

// R-22: POST /v1/orders/projections/rebuild (bề mặt quản trị, MFA, sales_approver) dựng lại mọi bảng chiếu
// thẳng từ sổ — kết quả trùng khớp bảng chiếu ghi thường, kể cả khi bảng chiếu bị xóa trắng trước.
func TestProjectionRebuild_R22(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	off, appr := f.tokens(f.tenant)
	if err := f.flow(ctx, f.tenant, off, appr, 0); err != nil {
		t.Fatal(err)
	}
	before := f.projectionDigest()
	for tb, n := range before {
		if n.rows == 0 {
			t.Errorf("bảng chiếu %s rỗng — luồng kiểm thử không phủ", tb)
		}
	}
	mustCode(t, f.asAdmin(port.RoleSalesOfficer).post("/v1/orders/projections/rebuild", nil), 403, "forbidden", "officer không dựng lại")
	r := f.asAdmin(port.RoleSalesApprover).post("/v1/orders/projections/rebuild", nil)
	mustStatus(t, r, http.StatusOK, "dựng lại")
	if after := f.projectionDigest(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("dựng lại lệch bảng chiếu:\ntrước %v\nsau   %v", before, after)
	}
	// Xóa trắng bảng chiếu (mô phỏng mất bảng chiếu) rồi dựng lại.
	f.inTenant(func(tx pgx.Tx) error {
		for _, tb := range store.Projections {
			if _, err := tx.Exec(ctx, "DELETE FROM mkg_orders."+tb); err != nil {
				return err
			}
		}
		return nil
	})
	mustStatus(t, f.asAdmin(port.RoleSalesApprover).post("/v1/orders/projections/rebuild", nil), http.StatusOK, "dựng lại sau xóa")
	if after := f.projectionDigest(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("dựng lại từ trắng lệch:\ntrước %v\nsau   %v", before, after)
	}
}

type digest struct {
	rows int
	md5  string
}

func (f *fixture) projectionDigest() map[string]digest {
	f.t.Helper()
	out := map[string]digest{}
	f.inTenant(func(tx pgx.Tx) error {
		for _, tb := range store.Projections {
			var d digest
			if err := tx.QueryRow(context.Background(), "SELECT count(*), coalesce(md5(string_agg(t::text, '|' ORDER BY t::text)), '') FROM mkg_orders."+
				tb+" t").Scan(&d.rows, &d.md5); err != nil {
				return err
			}
			out[tb] = d
		}
		return nil
	})
	return out
}

// R-20: UPDATE / DELETE / TRUNCATE trên mọi sổ của mkg_orders bằng vai orders_app và owner đều thất bại.
func TestOrdersLedgersAppendOnlySixWays_R20(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	off, appr := f.tokens(f.tenant)
	if err := f.flow(ctx, f.tenant, off, appr, 0); err != nil {
		t.Fatal(err)
	}
	owner := f.env.Pool(t, migrations.MKG, dbadmin.Migrator)
	for _, who := range []struct {
		name string
		pool *pgxpool.Pool
	}{{port.DBRole, f.pool}, {"owner ag_migrator", owner}} {
		for _, tb := range ledgers {
			for op, sql := range map[string]string{
				"UPDATE":   "UPDATE mkg_orders." + tb + " SET rule_version = 'x'",
				"DELETE":   "DELETE FROM mkg_orders." + tb,
				"TRUNCATE": "TRUNCATE mkg_orders." + tb,
			} {
				err := tenant.WithTenantTx(ctx, who.pool, f.tenant, func(tx pgx.Tx) error {
					var n int
					if err := tx.QueryRow(ctx, "SELECT count(*) FROM mkg_orders."+tb).Scan(&n); err != nil {
						return err
					}
					if n == 0 {
						return fmt.Errorf("%s rỗng — luồng không phủ", tb)
					}
					_, err := tx.Exec(ctx, sql)
					return err
				})
				if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
					t.Errorf("%s %s bằng %s phải bị chặn (42501), nhận %v", op, tb, who.name, err)
				}
			}
		}
	}
	// Chuỗi băm của một sổ hợp lệ.
	f.inTenant(func(tx pgx.Tx) error {
		var stream string
		if err := tx.QueryRow(ctx, "SELECT stream_key FROM mkg_orders.delivery_event LIMIT 1").Scan(&stream); err != nil {
			return err
		}
		r, err := ledger.Verify(ctx, tx, "mkg_orders.delivery_event", f.tenant, stream)
		if err != nil || !r.OK || r.RowsChecked < 3 {
			t.Fatalf("verify_chain: %+v %v", r, err)
		}
		return nil
	})
}

// R-32 (+ R-20, R-31): rò rỉ tenant qua PgBouncer transaction mode cho schema mkg_orders — mỗi tenant chạy
// toàn bộ đường ghi (HTTP + consumer phiếu cân) song song, xen kẽ đọc/ghi trên cùng pool orders_app.
func TestOrdersTenantLeakParallel(t *testing.T) {
	f := newFixture(t)
	tenants, rounds := lkSize(t, f.env.ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	const schema = "mkg_orders"
	pools := []lkPool{{role: port.DBRole, pool: f.pool, tables: lkTables(ctx, t, f.pool, schema)}}
	t.Logf("%s: %d bảng: %v", schema, len(pools[0].tables), pools[0].tables)
	prepare := func(_ int, tid uuid.UUID) func(context.Context, int) error {
		off, appr := f.tokens(tid)
		return func(ctx context.Context, r int) error { return f.flow(ctx, tid, off, appr, r) }
	}
	want := make([]string, 0, len(ledgers)+len(store.Projections))
	for _, tb := range append(append([]string{}, ledgers...), store.Projections...) {
		want = append(want, schema+"."+tb)
	}
	lkRun(t, ctx, f.env.AdminPool(t, migrations.MKG), pools, tenants, rounds, prepare, want)
}
