package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type api struct {
	t          *testing.T
	iss        *authztest.FakeIssuer
	tid        uuid.UUID
	app, admin string
}

func (a api) call(base, path string, body any, roles ...string) (int, map[string]any) {
	a.t.Helper()
	tok := a.iss.Sign(a.t, authztest.Token{Subject: ids.New(), TenantID: a.tid, Roles: roles, MFA: true})
	method := http.MethodGet
	var rd io.Reader
	if body != nil {
		method = http.MethodPost
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	if body != nil {
		req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	if r.StatusCode >= 300 {
		a.t.Logf("%s %s → %d %s", method, path, r.StatusCode, raw)
	}
	return r.StatusCode, m
}

// G4 tích hợp mục 5 (O-05, proto stockWarning): api-internal nối mkg/orders với hub/port.Stock (cổng đọc thêm do
// mkg/hub cài đặt, pool vai hub_app) — đơn bán vượt tồn hub của dạng hàng chỉ CẢNH BÁO (orders.stock_shortage),
// không chặn lập / xác nhận (orders.sales.stock_shortage_blocking = 0).
func TestOrdersStockShortageWarningFromHub_O05(t *testing.T) {
	env := pgtest.Get(t)
	ctx := context.Background()
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) { return env.Pool(t, migrations.MKG, role), nil }
	mods, ops, pools, err := Build(ctx, open, clk)
	if err != nil {
		t.Fatal(err)
	}
	iss := authztest.New(t)
	d := Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: iss.Verifier(), Ops: ops, Modules: mods.List(), Pools: pools}
	appSrv, admSrv := httptest.NewServer(NewRouter(d)), httptest.NewServer(NewAdminRouter(d))
	t.Cleanup(appSrv.Close)
	t.Cleanup(admSrv.Close)
	a := api{t: t, iss: iss, tid: ids.New(), app: appSrv.URL, admin: admSrv.URL}

	// Hub: cơ sở đã duyệt (hai người) + một lô rơm cuộn 12 000 kg.
	st, m := a.call(a.admin, "/v1/hub/facilities", map[string]any{"code": "HUBO05", "name": "Hub O-05", "kind": "hub",
		"location":      map[string]any{"lat": json.Number("10.0452"), "lng": json.Number("105.7469")},
		"boundary":      json.RawMessage(`{"type":"Polygon","coordinates":[[[105.74,10.04],[105.75,10.04],[105.75,10.05],[105.74,10.05],[105.74,10.04]]]}`),
		"province_code": "92", "capacity_kg": "500000", "effective_from": clk.Now().Add(-time.Hour).Format(time.RFC3339)}, hubport.RoleHubOperator)
	if st != http.StatusCreated {
		t.Fatalf("đề xuất cơ sở: %d", st)
	}
	fac := m["facility_id"].(string)
	if st, _ := a.call(a.admin, "/v1/hub/facilities/"+fac+"/versions/1/decision", map[string]any{"decision": "approved"}, hubport.RoleHubApprover); st != http.StatusCreated {
		t.Fatalf("duyệt cơ sở: %d", st)
	}
	if st, _ := a.call(a.app, "/v1/hub/facilities/"+fac+"/receipts", map[string]any{"product": "straw_bale", "mass_kg": "12000",
		"event_time": clk.Now().Add(-time.Hour).Format(time.RFC3339),
		"sources":    []map[string]any{{"kind": "cooperative", "ref": ids.New().String(), "mass_kg": "12000"}}}, hubport.RoleHubOperator); st != http.StatusCreated {
		t.Fatalf("nhận lô: %d", st)
	}

	so := func(product, kg string) map[string]any {
		st, m := a.call(a.app, "/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": product,
			"ordered_kg": kg, "list_price": "1000000", "unit_price": "1000000"}, ordport.RoleSalesOfficer)
		if st != http.StatusCreated {
			t.Fatalf("lập đơn bán %s %s: %d", product, kg, st)
		}
		return m["sales_order"].(map[string]any)
	}
	// Đủ tồn: không cảnh báo.
	ok := so("straw_bale", "10000")
	sc := ok["stock_check"].(map[string]any)
	if sc["source"] != "hub" || sc["available_kg"] != "12000.000" || sc["shortage"] != false || ok["warnings"] != nil {
		t.Fatalf("đủ tồn: %v", ok)
	}
	// Vượt tồn: cảnh báo, KHÔNG chặn (201) — xác nhận vẫn được.
	short := so("straw_bale", "15000")
	sc = short["stock_check"].(map[string]any)
	ws, _ := short["warnings"].([]any)
	if sc["shortage"] != true || sc["shortage_kg"] != "3000.000" || sc["blocking"] != false || len(ws) != 1 || ws[0] != "orders.stock_shortage" ||
		sc["param_version"] != ordport.ParamStockShortageBlocking+":default" {
		t.Fatalf("thiếu tồn: %v", short)
	}
	st, m = a.call(a.app, "/v1/orders/sales-orders/"+short["order_id"].(string)+"/confirm", map[string]any{}, ordport.RoleSalesOfficer)
	if st != http.StatusCreated || m["sales_order"].(map[string]any)["status"] != "confirmed" {
		t.Fatalf("xác nhận đơn thiếu tồn phải được (chỉ cảnh báo): %d %v", st, m)
	}
	// Dạng hàng không có tồn → thiếu toàn bộ.
	none := so("compressed_bale", "500")
	if sc := none["stock_check"].(map[string]any); sc["available_kg"] != "0.000" || sc["shortage_kg"] != "500.000" || sc["facilities"] != json.Number("0") {
		t.Fatalf("không tồn: %v", none)
	}
	// Tenant khác không thấy tồn của tenant này (RB-2).
	b := a
	b.tid = ids.New()
	st, m = b.call(b.app, "/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": "straw_bale",
		"ordered_kg": "100", "unit_price": "1000000"}, ordport.RoleSalesOfficer)
	if st != http.StatusCreated || m["sales_order"].(map[string]any)["stock_check"].(map[string]any)["available_kg"] != "0.000" {
		t.Fatalf("RB-2 tồn tenant khác: %d %v", st, m)
	}
}
