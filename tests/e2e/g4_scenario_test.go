package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	internalapi "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-internal/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge"
	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	costport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Số liệu của kịch bản G4 (golden G3 — proto FM-04/FM-10 — nối tiếp tới sổ cái):
//   - hợp đồng mua rơm theo tấn 650 000 đ/t (proto CT-01..03), đơn mua 90 000 kg → HUB_IN 96 800 kg = +7,56 % > 3 %;
//   - ghe CT-4400G đơn giá thuê 50 000 đ/t (GH-03) → cước chuyến thủy 96,8 t × 50 000 = 4 840 000,00;
//   - quyết toán HTX theo tấn 96,8 t × 650 000 = 62 920 000,00 (PM-01/02);
//   - đơn bán 1 500 000 đ/t, CUSTOMER_IN 94 900 kg (−0,47 % so với HUB_OUT, trong dung sai) → 142 350 000,00;
//   - bù HUB_IN sau duyệt: 96 800 → 97 300 kg.
const (
	g4ContractPrice = "650000"
	g4POKg          = "90000"
	g4SalesPrice    = "1500000"
	g4CustomerKg    = "94900.000"
	g4HubInFixKg    = "97300.000"
	g4HandlingCost  = "5000000.00"
	g4HandlingFix   = "5600000.00"
)

// g4 là trạng thái kịch bản G4 — dùng lại bước nền tảng / MKG của G3 (hai tenant, ADR-0020).
type g4 struct {
	*g3
	objDir string

	officer, salesAppr, dispatcher, driver, otherDriver mkgUser
	costAcc, chief, closer, operator, stranger          mkgUser
	driverSub                                           uuid.UUID
	wwGrant                                             harness.ShareGrant

	// "đầu tháng sau": api-internal thứ hai trên CÙNG database, đồng hồ sau khi kỳ kết thúc (đóng kỳ, duyệt, đẩy).
	laterApp, laterAdmin *httptest.Server
	laterClk             *clock.Fixed

	vessel, trip, leg, roadTrip          string
	contract, po, receipt                string
	salesOrder, deliveryID, settlementID string
	periodID, periodCode, resultSHA      string
	journal, externalRef, handlingEntry  string
	valveBefore                          map[string]int
	secret                               []string // giá / tiền / khách G4 — không bao giờ được thấy ở nền tảng
	customerRef                          string
	periodSummaryLines                   int
}

// TestG4TripToLedger là kịch bản chấp nhận G4 (docs/uat/g4-acceptance.md mục 2) — "từ chuyến ghe tới sổ cái":
// hợp đồng mua rơm (lập ≠ duyệt) + đơn mua → lệnh gom → xếp ghe có số hiệu ghe (FM-05) → chuyến thủy planned gán
// ghe → định tuyến đối tác sau cấp quyền waterway_routes (ảnh chụp nguyên văn) → mốc tài xế được giao → HUB_IN qua
// ingest đóng chuyến (trip.completed) + phiếu nhận ↔ đơn mua > 3 % → hub → đơn bán + giao nhận + chuyến bộ →
// CUSTOMER_IN → delivery.accepted → đầu vào giá thành → quyết toán HTX theo tấn (chief_accountant) → đóng kỳ 202 +
// job → period.closed → bút toán tổng hợp (cân, source_hash) → duyệt (≠ người dựng) → đẩy bộ xuất tệp (idempotent)
// → journal.pushed → kỳ posted. Nhánh sửa sai: HUB_IN bù sau khi phiếu nhận đã duyệt → bản bù ở orders, logistics,
// costing (kỳ mở kế tiếp); điều chỉnh chi phí trễ → kỳ mở kế tiếp. Cuối: R-22 dựng lại bảng chiếu, R-61, cách ly
// tenant, không dữ liệu G4 nào qua van (RB-2, ADR-0018, ADR-0020).
func TestG4TripToLedger(t *testing.T) {
	objDir := t.TempDir()
	t.Setenv("AG_ACCTBRIDGE_ADAPTER", "file_export")
	t.Setenv("AG_OBJSTORE_DIR", objDir) // kho đối tượng trong lãnh thổ của bộ xuất tệp (RB-7)
	s := harness.New(t)
	tid, mkg := harness.NewTenant(), harness.NewTenant()
	p := &g1{t: t, s: s, tid: tid, pubkeys: map[string][]byte{}}
	p.root = s.User(harness.UserSpec{Tenant: tid, MFA: true,
		Roles: []string{"platform_admin", "masterdata_admin", "registry_admin", "vault_registrar"}})
	base := &g3{t: t, s: s, p: p, tid: tid, mkg: mkg, pm: newPump(t, s, tid, mkg), today: clock.BusinessDate(time.Now())}
	base.devIn, base.devOut = newEdgeScale("e2e-g4-scale-in", "HUB_IN"), newEdgeScale("e2e-g4-scale-out", "HUB_OUT")
	base.m = newMKGStack(t, s, func() string { return base.saToken }, tid, base.devIn.ID, base.devOut.ID)
	g := &g4{g3: base, objDir: objDir}
	g.laterStack(t)

	steps := []struct {
		name string
		fn   func()
	}{
		{"01_nen_tang_HTX_thua_vu", g.stepPlatform},
		{"02_MKG_api_internal_ingest_mTLS", g.stepMKGStack},
		{"03_co_so_hub_va_so_thiet_bi_R56", g.stepFacilityDevices},
		{"04_vai_G4_va_mang_duong_thuy_phat_hanh", g.stepG4Actors},
		{"05_hop_dong_mua_rom_va_don_mua_R56", g.stepContract},
		{"06_lenh_gom_xep_ghe_so_hieu_ghe_FM05", g.stepBoatLoad},
		{"07_dinh_tuyen_doi_tac_waterway_routes", g.stepPartnerRoute},
		{"08_moc_tai_xe_duoc_giao", g.stepDriverMilestones},
		{"09_HUB_IN_ingest_dong_chuyen_phieu_nhan", g.stepHubInClosesTrip},
		{"10_hub_nhan_xuat_HUB_OUT", g.stepHubFlow},
		{"11_don_ban_giao_nhan_CUSTOMER_IN", g.stepSalesDelivery},
		{"12_dau_vao_gia_thanh", g.stepCostInputs},
		{"13_quyet_toan_HTX_theo_tan", g.stepSettlement},
		{"14_dong_ky_202_job_period_closed", g.stepPeriodClose},
		{"15_but_toan_duyet_day_posted", g.stepJournalPush},
		{"16_sua_sai_HUB_IN_bu_va_dieu_chinh_tre", g.stepCorrections},
		{"17_RB1_so_chi_ghi_them_verify_chain", g.stepLedgersChained},
		{"18_R22_dung_lai_bang_chieu", g.stepRebuild},
		{"19_R61_cach_ly_tenant_van", g.stepIsolationValve},
	}
	for _, st := range steps {
		if !t.Run(st.name, func(t *testing.T) { g.t, g.p.t = t, t; st.fn() }) {
			t.Fatalf("bước %s hỏng — dừng kịch bản", st.name)
		}
	}
}

// ---------- trợ giúp ----------

// laterStack dựng api-internal thứ hai (cùng database, cùng gốc lắp ráp) với đồng hồ "đầu tháng sau + 2 giờ" —
// kỳ tháng hiện tại đã kết thúc nên đóng được (costing.period_not_ended với đồng hồ thật).
func (g *g4) laterStack(t *testing.T) {
	now := time.Now().In(clock.Zone())
	next := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.Zone()).AddDate(0, 1, 0)
	g.laterClk = clock.NewFixed(next.Add(2 * time.Hour))
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return g.s.Env.Pool(t, migrations.MKG, role), nil
	}
	mods, ops, pools, err := internalapi.Build(context.Background(), open, g.laterClk)
	if err != nil {
		t.Fatal(err)
	}
	d := internalapi.Deps{Log: telemetry.NewLogger("api-internal-later", g.s.Logs), Verifier: g.s.Issuer.Verifier(), Ops: ops,
		Modules: mods.List(), Pools: pools}
	g.laterApp, g.laterAdmin = httptest.NewServer(internalapi.NewRouter(d)), httptest.NewServer(internalapi.NewAdminRouter(d))
	t.Cleanup(g.laterApp.Close)
	t.Cleanup(g.laterAdmin.Close)
}

func (g *g4) later(u mkgUser, admin bool, method, path string, body any) harness.Resp {
	g.t.Helper()
	base := g.laterApp.URL
	if admin {
		base = g.laterAdmin.URL
	}
	return mkgDo(g.t, base, u.tok, method, path, body)
}

// sign ký token tenant MKG với subject cho trước (driver_ref = sub — g4-conventions §3.2).
func (g *g4) sign(tid, sub uuid.UUID, roles ...string) mkgUser {
	return mkgUser{g: g.g3, tok: g.s.Issuer.Sign(g.t, authztest.Token{Subject: sub, TenantID: tid, Roles: roles, MFA: true, TTL: 2 * time.Hour})}
}

// at trả một thời điểm sự việc trước "bây giờ" nhưng KHÔNG trước đầu ngày làm việc (giờ VN): kỳ costing là tháng
// dương lịch, lệnh gom mở từ hôm nay — kịch bản chạy sau nửa đêm vẫn nằm trong ngày / tháng của đồng hồ thật.
func (g *g4) at(d time.Duration) string {
	now := time.Now()
	start := clock.StartOfBusinessDate(clock.BusinessDate(now))
	span := now.Sub(start)
	if span < 2*time.Hour {
		d = time.Duration(int64(d) * int64(span) / int64(2*time.Hour))
	}
	return now.Add(-d).UTC().Format(time.RFC3339Nano)
}

func sv(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func g4Flags(m map[string]any) map[string]bool {
	out := map[string]bool{}
	fs, _ := m["flags"].([]any)
	for _, f := range fs {
		if s, ok := f.(string); ok {
			out[s] = true
		}
	}
	return out
}

func g4Line(latlng ...float64) map[string]any {
	var coords [][]float64
	for i := 0; i+1 < len(latlng); i += 2 {
		coords = append(coords, []float64{latlng[i+1], latlng[i]})
	}
	return map[string]any{"type": "LineString", "coordinates": coords}
}

// outboxEvents đọc payload sự kiện của tenant MKG theo loại (vai ag_outbox_publisher, chỉ đọc).
func (g *g4) outboxEvents(eventType string) []outboxRow {
	g.t.Helper()
	var out []outboxRow
	for _, r := range g.pm.rows(migrations.MKG) {
		if r.env.EventType == eventType && r.env.TenantID == g.mkg {
			out = append(out, r)
		}
	}
	return out
}

// valveCounts đếm dòng mỗi (sổ van, hợp đồng) của tenant MKG.
func (g *g4) valveCounts() map[string]int {
	g.t.Helper()
	out := map[string]int{}
	for _, c := range []struct{ role, table string }{
		{"fieldops_app", "mkg_fieldops.valve_export"}, {measurement.DBRole, "mkg_measurement.valve_export"}, {hub.DBRole, "mkg_hub.valve_export"},
	} {
		ctx := context.Background()
		err := tenant.WithTenantTx(ctx, g.s.Env.Pool(g.t, migrations.MKG, c.role), g.mkg, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT contract, count(*) FROM "+c.table+" GROUP BY contract")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var k string
				var n int
				if err := rows.Scan(&k, &n); err != nil {
					return err
				}
				out[c.table+":"+k] = n
			}
			return rows.Err()
		})
		if err != nil {
			g.t.Fatalf("%s: %v", c.table, err)
		}
	}
	return out
}

// ---------- các bước ----------

// 04 — vai G4 của tenant MKG (mỗi người một sub, R-56) + mạng đường thủy của tenant CHƯƠNG TRÌNH: đoạn kênh xác
// nhận → dựng 202 + job (worker nền tảng) → phát hành bởi người khác. CHƯA cấp quyền waterway_routes cho MKG.
func (g *g4) stepG4Actors() {
	g.valveBefore = g.valveCounts()
	g.officer = g.sign(g.mkg, ids.New(), ordport.RoleSalesOfficer)
	g.salesAppr = g.sign(g.mkg, ids.New(), ordport.RoleSalesApprover)
	g.dispatcher = g.sign(g.mkg, ids.New(), logport.RoleDispatcher)
	g.driverSub = ids.New()
	g.driver = g.sign(g.mkg, g.driverSub, logport.RoleDriver)
	g.otherDriver = g.sign(g.mkg, ids.New(), logport.RoleDriver)
	g.costAcc = g.sign(g.mkg, ids.New(), costport.RoleCostAccountant)
	g.chief = g.sign(g.mkg, ids.New(), costport.RoleChiefAccountant)
	g.closer = g.sign(g.mkg, ids.New(), costport.RolePeriodCloser)
	g.operator = g.sign(g.mkg, ids.New(), abport.RoleAccountingBridgeOperator)
	g.stranger = g.sign(ids.New(), ids.New(), ordport.RoleSalesOfficer, logport.RoleDispatcher, costport.RoleCostAccountant,
		costport.RoleChiefAccountant, costport.RolePeriodCloser, abport.RoleAccountingBridgeOperator)
	g.customerRef = ids.New().String()

	editor := g.s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{wwport.RoleEditor}})
	publisher := g.s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{wwport.RolePublisher}})
	g.want(editor.Do(harness.Admin, http.MethodPost, "/v1/waterway/segments", "", map[string]any{"name": "Rạch Ninh Kiều (e2e G4)",
		"waterway_class": wwport.ClassMainCanal, "status": "confirmed", "width_m": "60", "depth_m": "5", "data_source": "survey",
		"geometry": g4Line(10.0304, 105.7704, 10.0380, 105.7600, 10.0452, 105.7469)}), 201, "đoạn kênh")
	b := g.want(editor.Do(harness.Admin, http.MethodPost, "/v1/waterway/networks/builds", "", nil), 202, "dựng mạng (202 + job)")
	build := b.Str("id")
	var env outbox.Envelope
	found := false
	for _, r := range g.pm.rows(migrations.PF) {
		if r.env.EventType == wwport.EventBuildRequested && r.env.TenantID == g.tid && strings.Contains(string(r.env.Payload), build) {
			env, found = r.env, true
		}
	}
	if !found {
		g.t.Fatalf("không thấy sự kiện %s của job %s", wwport.EventBuildRequested, build)
	}
	if _, err := inbox.Process(context.Background(), g.s.Env.Pool(g.t, migrations.PF, waterway.DBRole), wwport.BuildConsumer, env,
		g.s.Mods.Waterway.BuildHandler()); err != nil {
		g.t.Fatal(err)
	}
	st := g.want(editor.Do(harness.Admin, http.MethodGet, "/v1/waterway/networks/builds/"+build, "", nil), 200, "job dựng")
	if st.Str("status") != "completed" {
		g.t.Fatalf("job dựng mạng chưa xong: %s", trim(st.Raw))
	}
	ver := int(num(st.Body, "network_version_no"))
	g.want(publisher.Do(harness.Admin, http.MethodPost, fmt.Sprintf("/v1/waterway/networks/%d/publish", ver), "", nil), 200, "phát hành mạng")
}

// 05 — hợp đồng mua rơm theo tấn cho HTX Ninh Kiều (proto CT-01..03): lập sales_officer ≠ duyệt sales_approver
// (R-56 — người có hai vai tự duyệt bị chặn); đơn mua rơm lấy giá hợp đồng, lập ≠ duyệt.
func (g *g4) stepContract() {
	day := time.Now().In(clock.Zone())
	body := map[string]any{"htx_ref": g.p.coopNK, "basis": "per_tonne", "unit_price": g4ContractPrice, "committed_kg": "300000",
		"valid_from": day.AddDate(0, 0, -5).Format(time.DateOnly), "valid_to": day.AddDate(0, 0, 60).Format(time.DateOnly), "max_moisture_pct": "18.00"}
	both := g.sign(g.mkg, ids.New(), ordport.RoleSalesOfficer, ordport.RoleSalesApprover)
	self := g.want(both.app(http.MethodPost, "/v1/orders/contracts", map[string]any{"htx_ref": ids.New().String(), "basis": "per_tonne",
		"unit_price": g4ContractPrice, "committed_kg": "1000", "valid_from": day.Format(time.DateOnly), "valid_to": day.Format(time.DateOnly)}),
		201, "hợp đồng của người hai vai")
	if r := both.app(http.MethodPost, "/v1/orders/contracts/"+self.Str("contract_id")+"/versions/1/decision",
		map[string]any{"decision": "approved"}); r.Status != http.StatusForbidden || r.Code() != "self_approval_forbidden" {
		g.t.Fatalf("R-56: tự duyệt hợp đồng phải 403 self_approval_forbidden: %d %s", r.Status, trim(r.Raw))
	}
	r := g.want(g.officer.app(http.MethodPost, "/v1/orders/contracts", body), 201, "lập hợp đồng")
	g.contract = r.Str("contract_id")
	d := g.want(g.salesAppr.app(http.MethodPost, "/v1/orders/contracts/"+g.contract+"/versions/1/decision", map[string]any{"decision": "approved"}),
		201, "duyệt hợp đồng")
	if vs, _ := d.Body["versions"].([]any); len(vs) == 0 || str(vs[0].(map[string]any), "status") != "active" {
		g.t.Fatalf("hợp đồng phải active sau duyệt: %s", trim(d.Raw))
	}
	g.wantFail(g.officer.app(http.MethodPost, "/v1/orders/contracts", body), http.StatusConflict, "CT-01: hợp đồng thứ hai chồng thời gian")
	po := g.want(g.officer.app(http.MethodPost, "/v1/orders/purchase-orders", map[string]any{"category": "straw", "supplier_ref": g.p.coopNK,
		"ordered_qty": g4POKg}), 201, "đơn mua rơm theo hợp đồng")
	pom := obj(po.Body, "purchase_order")
	if str(pom, "unit") != "kg" || str(pom, "price_source") != "contract" || str(pom, "unit_price") != g4ContractPrice+".0000" {
		g.t.Fatalf("đơn mua lấy giá hợp đồng: %s", trim(po.Raw))
	}
	g.po = str(pom, "po_id")
	g.wantFail(g.officer.app(http.MethodPost, "/v1/orders/purchase-orders/"+g.po+"/decision", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "sales_officer không duyệt đơn mua")
	g.want(g.salesAppr.app(http.MethodPost, "/v1/orders/purchase-orders/"+g.po+"/decision", map[string]any{"decision": "approved"}), 201, "duyệt đơn mua")
	g.secret = append(g.secret, g4ContractPrice+".0000", g.contract)
}

// 06 — danh mục ghe (GH-01, số hiệu chuẩn hóa), lệnh gom (lập ≠ duyệt, bước G3), đếm cuộn bale → gather → boat_load
// mang số hiệu ghe (proto FM-05) → consumer logistics lập MỘT chuyến đường thủy planned, gán ghe theo số hiệu, tấn
// ước theo cuộn. Quyết toán HTX lập sớm → chờ cân (PM-02: còn ghe chưa cân).
func (g *g4) stepBoatLoad() {
	v := g.want(g.dispatcher.app(http.MethodPost, "/v1/logistics/vessels", map[string]any{"code": "ct-4400g", "kind": "ghe",
		"registered_tons": "100", "straw_payload_tons": "95", "rate_type": "per_ton", "rate_vnd": "50000",
		"registration_expiry": time.Now().AddDate(1, 0, 0).Format(time.DateOnly)}), 201, "thêm ghe vào danh mục")
	if str(obj(v.Body, "vessel"), "code") != "CT-4400G" {
		g.t.Fatalf("GH-01: số hiệu chuẩn hóa chữ hoa: %s", trim(v.Raw))
	}
	g.vessel = str(obj(v.Body, "vessel"), "vessel_id")
	g.stepOrder()
	path := "/v1/fieldops/orders/" + g.order + "/roll-counts"
	count := func(stage string, rolls int, reason string, back time.Duration, vessel string) harness.Resp {
		b := map[string]any{"plot_ref": g.p.plots[0], "stage": stage, "rolls": rolls, "event_time": g.at(back)}
		if reason != "" {
			b["discrepancy_reason"] = reason
		}
		if vessel != "" {
			b["vessel_code"] = vessel
		}
		return g.op.app(http.MethodPost, path, b)
	}
	g.want(count("bale", g3Bale, "", 50*time.Minute, ""), 201, "cuộn")
	g.want(count("gather", g3Gather, "100 cuộn ướt để lại bờ ruộng (proto seed)", 45*time.Minute, ""), 201, "tập kết")
	g.want(count("boat_load", g3BoatLoad, "Ghe thứ hai 1 300 cuộn thuộc lệnh gom kế tiếp", 40*time.Minute, " ct-4400g "), 201, "xếp ghe có số hiệu ghe")
	g.pm.run()
	if g.pm.byConsumer[logport.RollCountConsumer] == 0 || g.pm.byConsumer[costport.RollCountConsumer] == 0 {
		g.t.Fatalf("roll_count.recorded phải tới logistics và costing: %v", g.pm.byConsumer)
	}
	ls := g.want(g.dispatcher.app(http.MethodGet, "/v1/logistics/trips?status=planned", nil), 200, "chuyến planned")
	var tr map[string]any
	for _, it := range ls.Items() {
		if str(it, "collection_order_ref") == g.order {
			if tr != nil {
				g.t.Fatalf("FM-05: một lượt xuống ghe đúng MỘT chuyến: %s", trim(ls.Raw))
			}
			tr = it
		}
	}
	if tr == nil || str(tr, "mode") != string(logport.ModeWaterway) || str(tr, "vehicle_ref") != g.vessel || str(tr, "planned_kg") == "" ||
		g4Flags(tr)["no_vehicle"] || g4Flags(tr)["vessel_unregistered"] {
		g.t.Fatalf("FM-05: chuyến thủy planned gán ghe theo số hiệu: %s", trim(ls.Raw))
	}
	g.trip = str(tr, "trip_id")
	st := g.want(g.costAcc.app(http.MethodPost, "/v1/costing/settlements", map[string]any{"collection_order_ref": g.order, "htx_ref": g.p.coopNK,
		"harvest_date": time.Now().In(clock.Zone()).Format(time.DateOnly)}), 201, "lập quyết toán HTX")
	g.settlementID = str(st.Body, "id")
	if str(st.Body, "status") != string(costport.SettlementAwaitingWeighing) || str(st.Body, "price_source") != "contract" {
		g.t.Fatalf("PM-02: theo tấn còn ghe chưa cân → chờ cân: %s", trim(st.Raw))
	}
	if r := g.chief.adm(http.MethodPost, "/v1/costing/settlements/"+g.settlementID+"/approve", nil); r.Status < 400 {
		g.t.Fatalf("PM-02: không duyệt quyết toán khi còn ghe chưa cân: %d %s", r.Status, trim(r.Raw))
	}
}

// 07 — ADR-0020: chưa có cấp quyền waterway_routes → chặng thủy vẫn lập được (Haversine × #10, cờ
// routing_unavailable — L-04). Tenant chương trình duyệt cấp quyền (đề xuất ≠ duyệt) → lập lại chặng: định tuyến
// trên mạng của BÊN CẤP qua /v1/partner/waterway/route, ảnh chụp nguyên văn + SHA-256 (L-03), sổ đọc bên cấp.
func (g *g4) stepPartnerRoute() {
	legs := map[string]any{"legs": []any{map[string]any{"mode": "waterway",
		"from": map[string]any{"kind": "collection_point", "lat": "10.0304", "lng": "105.7704"},
		"to":   map[string]any{"kind": "facility", "ref": g.facility, "lat": "10.0452", "lng": "105.7469"}}}}
	r := g.want(g.dispatcher.app(http.MethodPost, "/v1/logistics/trips/"+g.trip+"/legs", legs), 201, "chặng khi chưa cấp quyền")
	l0, _ := obj(r.Body, "trip")["legs"].([]any)
	if len(l0) != 1 || str(l0[0].(map[string]any), "route_source") != string(logport.RouteHaversine) || !g4Flags(l0[0].(map[string]any))["routing_unavailable"] {
		g.t.Fatalf("L-04: thiếu cấp quyền → Haversine + routing_unavailable, không chặn: %s", trim(r.Raw))
	}
	g.wwGrant = g.s.GrantShare(g.tid, g.mkg, "MKG định tuyến chuyến ghe rơm trên mạng đường thủy của chương trình (ADR-0020, G4)", "waterway_routes")
	r = g.want(g.dispatcher.app(http.MethodPost, "/v1/logistics/trips/"+g.trip+"/legs", legs), 201, "chặng sau cấp quyền")
	l1, _ := obj(r.Body, "trip")["legs"].([]any)
	if len(l1) != 1 {
		g.t.Fatalf("một chặng: %s", trim(r.Raw))
	}
	leg := l1[0].(map[string]any)
	if src := str(leg, "route_source"); src == string(logport.RouteHaversine) || src == string(logport.RouteManual) || str(leg, "route_snapshot_ref") == "" ||
		leg["network_version"] == nil || g4Flags(leg)["routing_unavailable"] {
		g.t.Fatalf("L-03: định tuyến trên mạng bên cấp + ảnh chụp: %s", trim(r.Raw))
	}
	g.leg = str(leg, "leg_id")
	sn := g.want(g.dispatcher.app(http.MethodGet, "/v1/logistics/trips/"+g.trip+"/legs/"+g.leg+"/route-snapshot", nil), 200, "ảnh chụp định tuyến")
	raw, err := base64.StdEncoding.DecodeString(sn.Str("raw_base64"))
	sum := sha256.Sum256(raw)
	if err != nil || hex.EncodeToString(sum[:]) != sn.Str("raw_sha256") || sn.Str("grantor_tenant") != g.tid.String() ||
		!strings.Contains(string(raw), "distance_km") {
		g.t.Fatalf("ảnh chụp nguyên văn + SHA-256, bên cấp = tenant chương trình: %s", trim(sn.Raw))
	}
	reads := g.want(g.wwGrant.Proposer.Do(harness.Admin, http.MethodGet, "/v1/share-grants/"+g.wwGrant.ID+"/reads?limit=100", "", nil), 200, "sổ đọc")
	seen := false
	for _, it := range reads.Items() {
		seen = seen || str(it, "kind") == "waterway_routes"
	}
	if !seen {
		g.t.Fatalf("mỗi lần định tuyến phải có trong sổ đọc bên cấp (kind waterway_routes): %s", trim(reads.Raw))
	}
}

// 08 — L-14: điều phối gán tài xế (driver_ref = sub); tài xế khác ghi mốc → 403 logistics.not_assigned_driver;
// tài xế được giao ghi departed → arrived (có / không GPS); tài xế chỉ thấy chuyến của mình, không thấy cước.
func (g *g4) stepDriverMilestones() {
	g.want(g.dispatcher.app(http.MethodPost, "/v1/logistics/trips/"+g.trip+"/vehicle", map[string]any{"vehicle_ref": g.vessel,
		"driver_ref": g.driverSub.String()}), 201, "gán ghe + tài xế")
	if r := g.otherDriver.app(http.MethodPost, "/v1/logistics/trips/"+g.trip+"/milestones", map[string]any{"kind": "departed",
		"event_time": g.at(30 * time.Minute)}); r.Status != http.StatusForbidden || r.Code() != "logistics.not_assigned_driver" {
		g.t.Fatalf("tài xế không được giao: muốn 403 logistics.not_assigned_driver, nhận %d %s", r.Status, trim(r.Raw))
	}
	g.want(g.driver.app(http.MethodPost, "/v1/logistics/trips/"+g.trip+"/milestones", map[string]any{"kind": "departed",
		"event_time": g.at(30 * time.Minute), "lat": "10.0304", "lng": "105.7704", "accuracy_m": "6"}), 201, "xuất bến")
	a := g.want(g.driver.app(http.MethodPost, "/v1/logistics/trips/"+g.trip+"/milestones", map[string]any{"kind": "arrived",
		"event_time": g.at(20 * time.Minute)}), 201, "đến hub (máy không GPS)")
	if str(obj(a.Body, "trip"), "status") != string(logport.TripArrived) {
		g.t.Fatalf("L-02: departed → arrived: %s", trim(a.Raw))
	}
	mine := g.want(g.driver.app(http.MethodGet, "/v1/logistics/trips", nil), 200, "chuyến của tài xế")
	if it := mine.Items(); len(it) != 1 || str(it[0], "trip_id") != g.trip || it[0]["planned_cost"] != nil || it[0]["actual_cost"] != nil {
		g.t.Fatalf("tài xế chỉ thấy chuyến của mình, không thấy cước: %s", trim(mine.Raw))
	}
	if r := g.otherDriver.app(http.MethodGet, "/v1/logistics/trips/"+g.trip, nil); r.Status != http.StatusNotFound {
		g.t.Fatalf("tài xế khác đọc chuyến: %d %s", r.Status, trim(r.Raw))
	}
}

// 09 — phiếu nhận hàng của đơn mua lập trước cho lô (chờ cân); HUB_IN qua agent biên ký + ingest mTLS → phiếu
// 96 800 kg → consumer logistics ĐÓNG chuyến (tấn thật, cước thuê ghe GH-03, trip.completed nội bộ); orders gắn
// phiếu vào phiếu nhận: +7,56 % > 3 % → cần biên bản, không chặn (T-2); duyệt ≠ lập; costing ghi sơ chế hub (#55).
func (g *g4) stepHubInClosesTrip() {
	g.lot = ids.New().String()
	rc := g.want(g.officer.app(http.MethodPost, "/v1/orders/purchase-orders/"+g.po+"/receipts", map[string]any{"lot_ref": g.lot}), 201, "phiếu nhận chờ cân")
	g.receipt = str(obj(rc.Body, "receipt"), "receipt_id")
	if str(obj(rc.Body, "receipt"), "status") != "awaiting_weigh" {
		g.t.Fatalf("phiếu nhận rơm theo kg chờ phiếu cân: %s", trim(rc.Raw))
	}
	g.want(g.op.app(http.MethodPost, "/v1/measurement/lots/"+g.lot+"/terms", map[string]any{"pricing_basis": "per_tonne"}), 201, "cách tính giá theo tấn")
	tk := g.weigh(g.devIn, g3HubInNet+g3Tare, "18.00", g.order)
	if str(tk, "mass_kg") != "96800.000" {
		g.t.Fatalf("HUB_IN golden 96 800 kg: %v", tk)
	}
	g.ticketIn = str(tk, "id")
	if g.ticketIn == "" {
		g.ticketIn = str(tk, "ticket_id")
	}
	g.pm.run()
	for _, c := range []string{logport.WeighTicketConsumer, ordport.WeighTicketConsumer, costport.WeighTicketConsumer, costport.TripConsumer} {
		if g.pm.byConsumer[c] == 0 {
			g.t.Fatalf("consumer %s chưa nhận sự kiện: %v", c, g.pm.byConsumer)
		}
	}
	tr := obj(g.want(g.dispatcher.app(http.MethodGet, "/v1/logistics/trips/"+g.trip, nil), 200, "chuyến sau cân").Body, "trip")
	if str(tr, "status") != string(logport.TripCompleted) || str(tr, "actual_kg") != "96800.000" || str(tr, "actual_cost") != "4840000.00" ||
		str(tr, "cost_source") != string(logport.CostByVesselRate) || str(tr, "ticket_ref") != g.ticketIn {
		g.t.Fatalf("plant weighing: phiếu HUB_IN đóng chuyến với tấn thật + cước thuê ghe: %v", tr)
	}
	evs := g.outboxEvents(logport.EventTripCompleted)
	if len(evs) != 1 || evs[0].kind != string(outbox.KindInternal) || !strings.Contains(string(evs[0].env.Payload), "CT-4400G") ||
		!strings.Contains(string(evs[0].env.Payload), "route_snapshot_ref") {
		g.t.Fatalf("một trip.completed nội bộ mang ghe + ảnh chụp định tuyến: %d", len(evs))
	}
	r := obj(g.want(g.officer.app(http.MethodGet, "/v1/orders/receipts/"+g.receipt, nil), 200, "phiếu nhận sau cân").Body, "receipt")
	if str(r, "status") != "pending_approval" || str(r, "ticket_ref") != g.ticketIn || str(r, "variance_pct") != "7.56" ||
		sv(r, "needs_variance_report") != "true" || str(r, "audit_note") == "" {
		g.t.Fatalf("T-2: +7,56 %% > 3 %% → cần biên bản + ghi chú kiểm toán: %v", r)
	}
	g.want(g.officer.app(http.MethodPost, "/v1/orders/receipts/"+g.receipt+"/variance-report", map[string]any{
		"note": "Biên bản chênh lệch: HTX giao dư 6 800 kg so với đơn mua", "file_ref": ids.New().String()}), 201, "biên bản chênh lệch")
	g.wantFail(g.officer.app(http.MethodPost, "/v1/orders/receipts/"+g.receipt+"/decision", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "sales_officer không duyệt phiếu nhận")
	a := g.want(g.salesAppr.app(http.MethodPost, "/v1/orders/receipts/"+g.receipt+"/decision", map[string]any{"decision": "approved"}), 201,
		"duyệt phiếu nhận vượt ngưỡng (không chặn)")
	if str(obj(a.Body, "receipt"), "status") != "approved" || obj(a.Body, "receipt")["payable"] != nil {
		g.t.Fatalf("O-11: rơm không ghi phải trả ở orders: %s", trim(a.Raw))
	}
}

// 10 — hub nhận lô từ phiếu (Q-MKG-04), xuất + HUB_OUT qua ingest (bước G3).
func (g *g4) stepHubFlow() {
	g.stepHubReceipt()
	g.stepHubOut()
}

// 11 — đơn bán (đúng giá bảng → sales_officer xác nhận), lịch giao lô hub (tuyến HUB), chuyến đường bộ chở lô
// (Haversine × #10) gắn vào giao nhận; CUSTOMER_IN nhập tay kèm bằng chứng (C-5 tạm) trong dung sai → consumer
// logistics đóng chuyến bộ (cước theo tham số #22), orders gắn phiếu vào giao nhận → biên bản chấp nhận: khối lượng =
// phiếu, tiền = kg/1000 × đơn giá → mkg.orders.delivery.accepted (nội bộ).
func (g *g4) stepSalesDelivery() {
	so := g.want(g.officer.app(http.MethodPost, "/v1/orders/sales-orders", map[string]any{"customer_ref": g.customerRef, "product": "compressed_bale",
		"ordered_kg": "95000", "list_price": g4SalesPrice, "unit_price": g4SalesPrice}), 201, "lập đơn bán")
	g.salesOrder = str(obj(so.Body, "sales_order"), "order_id")
	g.want(g.officer.app(http.MethodPost, "/v1/orders/sales-orders/"+g.salesOrder+"/confirm", nil), 201, "xác nhận đơn bán")
	dl := g.want(g.officer.app(http.MethodPost, "/v1/orders/sales-orders/"+g.salesOrder+"/deliveries", map[string]any{"lot_ref": g.lot, "route": "HUB",
		"planned_date": time.Now().In(clock.Zone()).Format(time.DateOnly), "planned_kg": "95348"}), 201, "lịch giao")
	g.deliveryID = str(obj(dl.Body, "delivery"), "delivery_id")
	rt := g.want(g.dispatcher.app(http.MethodPost, "/v1/logistics/trips", map[string]any{"mode": "road", "delivery_ref": g.deliveryID,
		"driver_ref": g.driverSub.String(), "cargo": []any{map[string]any{"lot_ref": g.lot, "mass_kg": "95348"}}, "event_time": g.at(9 * time.Minute)}),
		201, "chuyến bộ giao khách")
	g.roadTrip = str(obj(rt.Body, "trip"), "trip_id")
	g.want(g.dispatcher.app(http.MethodPost, "/v1/logistics/trips/"+g.roadTrip+"/legs", map[string]any{"legs": []any{map[string]any{"mode": "road",
		"from": map[string]any{"kind": "facility", "ref": g.facility, "lat": "10.0452", "lng": "105.7469"},
		"to":   map[string]any{"kind": "customer_site", "lat": "10.1200", "lng": "105.6500"}}}}), 201, "chặng bộ")
	g.want(g.officer.app(http.MethodPost, "/v1/orders/deliveries/"+g.deliveryID+"/dispatch", map[string]any{"trip_ref": g.roadTrip}), 201, "xuất giao")
	g.wantFail(g.officer.app(http.MethodPost, "/v1/orders/deliveries/"+g.deliveryID+"/accept", map[string]any{"evidence": map[string]any{
		"file_ref": ids.New().String(), "kind": "epod"}}), http.StatusConflict, "chấp nhận khi chưa có CUSTOMER_IN")
	g.evidenceRef = ids.New().String()
	c := g.want(g.op.app(http.MethodPost, "/v1/measurement/lots/"+g.lot+"/tickets", map[string]any{"point": "CUSTOMER_IN", "mass_kg": g4CustomerKg,
		"weighed_at": g.at(5 * time.Minute), "evidence_ref": g.evidenceRef, "customer_ref": g.customerRef,
		"reason": "Phiếu cân nhà máy khách (C-5 tạm: nhập tay kèm bằng chứng)"}), 201, "CUSTOMER_IN")
	if rc := obj(c.Body, "reconciliation"); rc != nil && str(rc, "outcome") == "dispute_opened" {
		g.t.Fatalf("CUSTOMER_IN −0,47 %% trong dung sai không mở tranh chấp: %s", trim(c.Raw))
	}
	g.pm.run()
	rtr := obj(g.want(g.dispatcher.app(http.MethodGet, "/v1/logistics/trips/"+g.roadTrip, nil), 200, "chuyến bộ").Body, "trip")
	if str(rtr, "status") != string(logport.TripCompleted) || str(rtr, "actual_kg") != g4CustomerKg || str(rtr, "cost_source") != string(logport.CostByParamRate) {
		g.t.Fatalf("CUSTOMER_IN đóng chuyến bộ (cước tham số #22): %v", rtr)
	}
	a := g.want(g.officer.app(http.MethodPost, "/v1/orders/deliveries/"+g.deliveryID+"/accept", map[string]any{"evidence": map[string]any{
		"file_ref": ids.New().String(), "kind": "minutes"}}), 201, "biên bản giao nhận")
	d := obj(a.Body, "delivery")
	if str(d, "status") != "accepted" || str(d, "accepted_kg") != g4CustomerKg || str(d, "amount") != "142350000.00" {
		g.t.Fatalf("O-07: khối lượng = phiếu CUSTOMER_IN, tiền = kg/1000 × đơn giá: %v", d)
	}
	g.pm.run()
	evs := g.outboxEvents(ordport.EventDeliveryAccepted)
	if len(evs) != 1 || evs[0].kind != string(outbox.KindInternal) || g.pm.byConsumer[costport.DeliveryConsumer] == 0 {
		g.t.Fatalf("một delivery.accepted nội bộ tới costing: %d %v", len(evs), g.pm.byConsumer)
	}
	g.secret = append(g.secret, g4SalesPrice+".0000", "142350000.00", g.customerRef, "4840000.00")
}

// 12 — đầu vào giá thành (proto buildCostChain): lô hub có sơ chế #55 (từ HUB_IN), cước bộ (chuyến giao), doanh thu
// giao nhận; lô lệnh gom có cước thủy (chuyến ghe); bút toán tay bốc xếp của kế toán giá thành vào kỳ đang mở.
func (g *g4) stepCostInputs() {
	e := g.want(g.costAcc.app(http.MethodPost, "/v1/costing/entries", map[string]any{"lot_ref": g.lot, "category": "handling", "amount": g4HandlingCost,
		"note": "Bốc xếp lên xe giao khách (e2e G4)"}), 201, "bút toán tay bốc xếp")
	g.handlingEntry = str(e.Body, "id")
	g.periodCode = str(e.Body, "period_code")
	if g.periodCode != time.Now().In(clock.Zone()).Format("2006-01") {
		g.t.Fatalf("bút toán vào kỳ tháng hiện tại: %s", trim(e.Raw))
	}
	lc := g.want(g.costAcc.app(http.MethodGet, "/v1/costing/lots/"+g.lot+"/cost", nil), 200, "giá thành lô hub")
	cats := map[string]string{}
	for _, c := range lc.Items("categories") {
		cats[str(c, "category")] = str(c, "amount")
	}
	if cats["hub_processing"] == "" || cats["transport_road"] == "" || cats["handling"] != g4HandlingCost || str(lc.Body, "revenue") != "142350000.00" ||
		str(lc.Body, "delivered_kg") != g4CustomerKg || lc.Body["cost_per_tonne"] == nil {
		g.t.Fatalf("giá thành lô hub (sơ chế, cước bộ, bốc xếp, doanh thu, đ/t): %s", trim(lc.Raw))
	}
	oc := g.want(g.costAcc.app(http.MethodGet, "/v1/costing/lots/"+g.order+"/cost", nil), 200, "giá thành lô lệnh gom")
	found := false
	for _, c := range oc.Items("categories") {
		found = found || (str(c, "category") == "transport_waterway" && str(c, "amount") == "4840000.00")
	}
	if !found {
		g.t.Fatalf("cước thủy 4 840 000,00 phân bổ về lô trên chuyến: %s", trim(oc.Raw))
	}
}

// 13 — quyết toán HTX theo tấn (PM-01/02/04): sau khi mọi ghe đã cân, quyết toán được làm mới → 96,8 t × 650 000 =
// 62 920 000,00 chờ xác nhận; người lập có cả vai kế toán trưởng tự duyệt bị chặn (R-56); chief_accountant duyệt →
// một khoản phải trả, hạn 15 ngày (tạm), đóng băng, settlement.approved nội bộ.
func (g *g4) stepSettlement() {
	st := g.want(g.costAcc.app(http.MethodGet, "/v1/costing/settlements/"+g.settlementID, nil), 200, "quyết toán")
	if str(st.Body, "status") != string(costport.SettlementPendingConfirmation) || str(st.Body, "amount") != "62920000.00" || str(st.Body, "mass_kg") != "96800.000" {
		g.t.Fatalf("PM-02: cân đủ → quyết toán theo tấn 62 920 000,00: %s", trim(st.Raw))
	}
	// R-56: người lập (cost_accountant) được cấp thêm vai kế toán trưởng vẫn không tự duyệt quyết toán của mình.
	prepAsChief := g.sign(g.mkg, g.actorOf(g.costAcc), costport.RoleCostAccountant, costport.RoleChiefAccountant)
	if r := prepAsChief.adm(http.MethodPost, "/v1/costing/settlements/"+g.settlementID+"/approve", nil); r.Status != http.StatusForbidden {
		g.t.Fatalf("R-56: người lập không tự duyệt quyết toán: %d %s", r.Status, trim(r.Raw))
	}
	g.wantFail(g.costAcc.adm(http.MethodPost, "/v1/costing/settlements/"+g.settlementID+"/approve", nil), http.StatusForbidden,
		"cost_accountant không duyệt quyết toán")
	a := g.want(g.chief.adm(http.MethodPost, "/v1/costing/settlements/"+g.settlementID+"/approve", nil), 201, "kế toán trưởng duyệt")
	due := time.Now().In(clock.Zone()).AddDate(0, 0, 15).Format(time.DateOnly)
	if str(a.Body, "status") != string(costport.SettlementApproved) || !strings.HasPrefix(sv(a.Body, "due_on"), due) {
		g.t.Fatalf("PM-04: duyệt → khoản phải trả hạn 15 ngày: %s", trim(a.Raw))
	}
	g.wantFail(g.chief.adm(http.MethodPost, "/v1/costing/settlements/"+g.settlementID+"/approve", nil), http.StatusConflict, "đóng băng sau duyệt")
	if evs := g.outboxEvents(costport.EventSettlementApproved); len(evs) != 1 || evs[0].kind != string(outbox.KindInternal) {
		g.t.Fatalf("một settlement.approved nội bộ: %d", len(evs))
	}
	g.pm.run()
	g.secret = append(g.secret, "62920000.00")
}

// 14 — ánh xạ tài khoản (vận hành đề xuất ≠ kế toán trưởng duyệt, B-05); đóng kỳ mẫu mục 12: với đồng hồ thật kỳ
// chưa hết → 409; "đầu tháng sau" period_closer yêu cầu → 202 + job (ảnh chụp đầu vào bất biến có băm) → worker
// đóng kỳ → period.closed nội bộ → acctbridge dựng bút toán tổng hợp (job, builder = người đóng kỳ).
func (g *g4) stepPeriodClose() {
	now := time.Now().In(clock.Zone())
	m := g.want(g.operator.adm(http.MethodPost, "/v1/acctbridge/mappings", map[string]any{"reason": "Bảng ánh xạ VAS tạm (e2e G4, chờ khách Q-1)",
		"effective_from": fmt.Sprintf("%d-01-01", now.Year())}), 201, "đề xuất ánh xạ")
	ver := str(obj(m.Body, "mapping"), "version_id")
	if r := g.operator.adm(http.MethodPost, "/v1/acctbridge/mappings/"+ver+"/decision", map[string]any{"decision": "approved"}); r.Status < 400 {
		g.t.Fatalf("R-56: vận hành không tự duyệt ánh xạ: %d %s", r.Status, trim(r.Raw))
	}
	g.want(g.chief.adm(http.MethodPost, "/v1/acctbridge/mappings/"+ver+"/decision", map[string]any{"decision": "approved"}), 201, "duyệt ánh xạ")

	ps := g.want(g.closer.app(http.MethodGet, "/v1/costing/periods", nil), 200, "danh sách kỳ")
	for _, it := range ps.Items() {
		if str(it, "code") == g.periodCode {
			g.periodID = str(it, "id")
		}
	}
	if g.periodID == "" {
		g.t.Fatalf("kỳ %s: %s", g.periodCode, trim(ps.Raw))
	}
	if r := g.closer.app(http.MethodPost, "/v1/costing/periods/"+g.periodID+"/close", nil); r.Status != http.StatusConflict ||
		r.Code() != "costing.period_not_ended" {
		g.t.Fatalf("kỳ chưa kết thúc không đóng được: %d %s", r.Status, trim(r.Raw))
	}
	g.wantFail(g.later(g.costAcc, false, http.MethodPost, "/v1/costing/periods/"+g.periodID+"/close", nil), http.StatusForbidden,
		"chỉ period_closer đóng kỳ")
	j := g.want(g.later(g.closer, false, http.MethodPost, "/v1/costing/periods/"+g.periodID+"/close", nil), 202, "đóng kỳ (202 + job)")
	if j.Header.Get("Location") == "" || str(j.Body, "status") != "queued" || str(j.Body, "input_snapshot_sha256") == "" {
		g.t.Fatalf("mẫu mục 12: 202 + job có băm ảnh chụp: %s", trim(j.Raw))
	}
	g.pm.clk = g.laterClk
	g.pm.run()
	for _, c := range []string{costport.PeriodCloseConsumer, abport.PeriodClosedConsumer, abport.JournalBuildConsumer} {
		if g.pm.byConsumer[c] == 0 {
			g.t.Fatalf("consumer %s chưa chạy: %v", c, g.pm.byConsumer)
		}
	}
	jb := g.want(g.later(g.closer, false, http.MethodGet, "/v1/costing/period-close-jobs/"+str(j.Body, "job_id"), nil), 200, "job đóng kỳ")
	if str(jb.Body, "status") != "completed" {
		g.t.Fatalf("job đóng kỳ phải xong: %s", trim(jb.Raw))
	}
	p := g.want(g.later(g.closer, false, http.MethodGet, "/v1/costing/periods/"+g.periodID, nil), 200, "kỳ đã đóng")
	sum := obj(p.Body, "summary")
	if str(p.Body, "status") != string(costport.PeriodClosed) || sum == nil || str(sum, "result_set_sha256") == "" {
		g.t.Fatalf("kỳ closed + kết quả tổng hợp: %s", trim(p.Raw))
	}
	g.resultSHA = str(sum, "result_set_sha256")
	lines, _ := sum["lines"].([]any)
	g.periodSummaryLines = len(lines)
	cats := map[string]bool{}
	for _, l := range lines {
		cats[str(l.(map[string]any), "category")] = true
	}
	for _, c := range []string{"straw_purchase", "transport_waterway", "transport_road", "hub_processing", "handling", "sales_revenue"} {
		if !cats[c] {
			g.t.Fatalf("kết quả kỳ thiếu nhóm %s: %v", c, cats)
		}
	}
	if evs := g.outboxEvents(costport.EventPeriodClosed); len(evs) != 1 || evs[0].kind != string(outbox.KindInternal) ||
		!strings.Contains(string(evs[0].env.Payload), g.resultSHA) {
		g.t.Fatalf("một period.closed nội bộ mang result_set_sha256: %d", len(evs))
	}
	g.wantFail(g.later(g.closer, false, http.MethodPost, "/v1/costing/periods/"+g.periodID+"/close", nil), http.StatusConflict, "kỳ đã đóng không mở lại")
	g.wantFail(g.later(g.costAcc, false, http.MethodPost, "/v1/costing/entries", map[string]any{"lot_ref": g.lot, "category": "handling",
		"amount": "1000.00", "event_time": g.at(time.Minute)}), http.StatusConflict, "bút toán mới vào kỳ đã đóng")
}

// 15 — bút toán tổng hợp: Σ Nợ = Σ Có > 0, source_hash = result_set_sha256; người dựng (người đóng kỳ) không duyệt
// được, vận hành không duyệt; kế toán trưởng duyệt → job đẩy → bộ xuất tệp CSV + XML + tệp ký trong kho đối tượng →
// journal.pushed → costing đưa kỳ sang posted. Giao lại job đẩy / đẩy lại bộ điều hợp → cùng ExternalRef, không ghi thêm.
func (g *g4) stepJournalPush() {
	l := g.want(g.later(g.operator, false, http.MethodGet, "/v1/acctbridge/journals?period_id="+g.periodID, nil), 200, "bút toán của kỳ")
	it := l.Items()
	if len(it) != 1 || str(it[0], "status") != "built" || str(it[0], "source_hash") != g.resultSHA || str(it[0], "total_debit") != str(it[0], "total_credit") ||
		str(it[0], "total_debit") == "0.00" || str(it[0], "built_by") == "" {
		g.t.Fatalf("B-03: bút toán cân, source_hash = result_set_sha256: %s", trim(l.Raw))
	}
	g.journal = str(it[0], "journal_id")
	closerAsChief := g.sign(g.mkg, g.actorOf(g.closer), abport.RoleChiefAccountant)
	if r := g.later(closerAsChief, true, http.MethodPost, "/v1/acctbridge/journals/"+g.journal+"/approve", nil); r.Status != http.StatusForbidden {
		g.t.Fatalf("B-04: người dựng không tự duyệt bút toán: %d %s", r.Status, trim(r.Raw))
	}
	g.wantFail(g.later(g.operator, true, http.MethodPost, "/v1/acctbridge/journals/"+g.journal+"/approve", nil), http.StatusForbidden, "vận hành không duyệt")
	g.want(g.later(g.chief, true, http.MethodPost, "/v1/acctbridge/journals/"+g.journal+"/approve", nil), 202, "kế toán trưởng duyệt (202 + job đẩy)")
	g.pm.run()
	if g.pm.byConsumer[abport.JournalPushConsumer] == 0 || g.pm.byConsumer[costport.JournalPushedConsumer] == 0 {
		g.t.Fatalf("job đẩy + journal.pushed → costing: %v", g.pm.byConsumer)
	}
	jr := obj(g.want(g.later(g.operator, false, http.MethodGet, "/v1/acctbridge/journals/"+g.journal, nil), 200, "bút toán đã đẩy").Body, "journal")
	if jr == nil {
		jr = g.want(g.later(g.operator, false, http.MethodGet, "/v1/acctbridge/journals/"+g.journal, nil), 200, "bút toán đã đẩy").Body
	}
	g.externalRef = str(jr, "external_ref")
	if str(jr, "status") != "pushed" || str(jr, "adapter") != g4AdapterName() || g.externalRef == "" || str(jr, "artifact_sha256") == "" {
		g.t.Fatalf("B-06: đẩy qua bộ xuất tệp: %v", jr)
	}
	files := g.exportFiles()
	if len(files) != 3 {
		g.t.Fatalf("bộ xuất tệp ghi đúng 3 tệp (csv, xml, sha256): %v", files)
	}
	if evs := g.outboxEvents(abport.EventJournalPushed); len(evs) != 1 || evs[0].kind != string(outbox.KindInternal) {
		g.t.Fatalf("một journal.pushed nội bộ: %d", len(evs))
	}
	p := g.want(g.later(g.closer, false, http.MethodGet, "/v1/costing/periods/"+g.periodID, nil), 200, "kỳ sau khi đẩy")
	if str(p.Body, "status") != string(costport.PeriodPosted) || str(p.Body, "journal_ref") != g.journal {
		g.t.Fatalf("journal.pushed → kỳ posted: %s", trim(p.Raw))
	}
	// B-07: giao lại yêu cầu đẩy (event_id mới — như tin nhắn trùng ngoài inbox) → không gọi đích lần nữa.
	for _, r := range g.outboxEvents(abport.EventJournalPushRequested) {
		env := r.env
		env.EventID = ids.New()
		h, err := g.handlerFor(abport.JournalPushConsumer)
		if err != nil {
			g.t.Fatal(err)
		}
		if _, err := inbox.Process(context.Background(), g.s.Env.Pool(g.t, migrations.MKG, acctbridge.DBRole), abport.JournalPushConsumer, env, h); err != nil {
			g.t.Fatal(err)
		}
	}
	// Bút toán đã đẩy bất biến: duyệt lại → 409 acctbridge.journal_pushed_immutable; đẩy lại tay (chỉ từ DLQ) bị từ chối.
	if r := g.later(g.chief, true, http.MethodPost, "/v1/acctbridge/journals/"+g.journal+"/approve", nil); r.Status != http.StatusConflict ||
		r.Code() != "acctbridge.journal_pushed_immutable" {
		g.t.Fatalf("B-07: bút toán đã đẩy không duyệt / đẩy lại: %d %s", r.Status, trim(r.Raw))
	}
	if r := g.later(g.operator, false, http.MethodPost, "/v1/acctbridge/journals/"+g.journal+"/retry-push", nil); r.Status < 400 || !r.IsProblem() {
		g.t.Fatalf("đẩy lại tay chỉ cho bút toán push_failed: %d %s", r.Status, trim(r.Raw))
	}
	if got := g.exportFiles(); len(got) != 3 {
		g.t.Fatalf("đẩy lại không ghi thêm tệp: %v", got)
	}
	jr2 := g.want(g.later(g.operator, false, http.MethodGet, "/v1/acctbridge/journals/"+g.journal, nil), 200, "bút toán sau giao lại")
	if !strings.Contains(string(jr2.Raw), g.externalRef) || strings.Count(string(jr2.Raw), `"push_attempts":1`) != 1 {
		g.t.Fatalf("giao lại không tạo lần thử mới: %s", trim(jr2.Raw))
	}
	g.pm.clk = nil
	g.secret = append(g.secret, g.externalRef)
}

// g4AdapterName là tên bộ điều hợp mà worker-internal chọn theo AG_ACCTBRIDGE_ADAPTER (bộ xuất tệp).
func g4AdapterName() string {
	_, name := acctbridge.AdapterFromEnv(context.Background(), clock.System{})
	return name
}

// actorOf đọc sub của token mkgUser (người đóng kỳ = người dựng bút toán).
func (g *g4) actorOf(u mkgUser) uuid.UUID {
	g.t.Helper()
	parts := strings.Split(u.tok, ".")
	if len(parts) != 3 {
		g.t.Fatal("token không phải JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		g.t.Fatal(err)
	}
	i := strings.Index(string(raw), `"sub":"`)
	if i < 0 {
		g.t.Fatalf("token thiếu sub: %s", raw)
	}
	return uuid.MustParse(string(raw[i+7 : i+7+36]))
}

func (g *g4) handlerFor(name string) (inbox.Handler, error) {
	for _, h := range g.pm.delivered {
		if h.consumer == name {
			return h.handler, nil
		}
	}
	return nil, fmt.Errorf("chưa giao consumer %s", name)
}

func (g *g4) exportFiles() []string {
	g.t.Helper()
	var out []string
	_ = filepath.WalkDir(g.objDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(filepath.ToSlash(p), "acctbridge/") &&
			(strings.HasSuffix(p, ".csv") || strings.HasSuffix(p, ".xml") || strings.HasSuffix(p, ".sha256")) {
			out = append(out, filepath.Base(p))
		}
		return nil
	})
	return out
}

// 16 — nhánh sửa sai (RB-1): phiếu HUB_IN được bù (96 800 → 97 300 kg) SAU khi phiếu nhận đã duyệt và kỳ đã posted
// → orders ghi bản bù phiếu nhận (vẫn approved, kiểm lại ↔ đơn mua), logistics ghi bản bù hoàn tất chuyến (phát lại
// trip.completed có supersedes_id), costing ghi kết quả mới vào kỳ MỞ kế tiếp (late_from = kỳ đã đóng); quyết toán đã
// duyệt đóng băng. Điều chỉnh chi phí trễ (bốc xếp) → kỳ mở kế tiếp, trỏ bút toán cũ. Kỳ đã đóng không đổi.
func (g *g4) stepCorrections() {
	next := time.Now().In(clock.Zone())
	nextCode := time.Date(next.Year(), next.Month(), 1, 0, 0, 0, 0, clock.Zone()).AddDate(0, 1, 0).Format("2006-01")
	fix := g.want(g.op.app(http.MethodPost, "/v1/measurement/lots/"+g.lot+"/tickets", map[string]any{"point": "HUB_IN", "mass_kg": g4HubInFixKg,
		"weighed_at": g.at(time.Minute), "evidence_ref": ids.New().String(), "reason": "Cân kiểm định lại sau hiệu chuẩn",
		"supersedes_id": g.ticketIn, "correction_reason": "Cân HUB_IN lệch hiệu chuẩn — phiếu bù theo biên bản kiểm định"}), 201, "phiếu HUB_IN bù")
	fixID := str(obj(fix.Body, "ticket"), "id")
	if fixID == "" {
		fixID = str(obj(fix.Body, "ticket"), "ticket_id")
	}
	g.pm.run()
	r := obj(g.want(g.officer.app(http.MethodGet, "/v1/orders/receipts/"+g.receipt, nil), 200, "phiếu nhận sau bù").Body, "receipt")
	if str(r, "status") != "approved" || str(r, "ticket_ref") != fixID || str(r, "received_qty") != g4HubInFixKg || str(r, "variance_pct") != "8.11" {
		g.t.Fatalf("O-12: phiếu nhận bản bù vẫn approved, kiểm lại ↔ đơn mua (+8,11 %%): %v", r)
	}
	if n := g.countIn(migrations.MKG, orders.DBRole, g.mkg, "SELECT count(*) FROM mkg_orders.receipt_event WHERE receipt_id = '"+g.receipt+
		"' AND kind = 'corrected' AND supersedes_id IS NOT NULL"); n != 1 {
		g.t.Fatalf("một bản bù phiếu nhận: %d", n)
	}
	evs := g.outboxEvents(logport.EventTripCompleted)
	var sup int
	for _, e := range evs {
		var pl map[string]any
		if err := json.Unmarshal(e.env.Payload, &pl); err != nil {
			g.t.Fatal(err)
		}
		if str(pl, "trip_id") == g.trip && str(pl, "supersedes_id") != "" && str(pl, "actual_kg") == g4HubInFixKg {
			sup++
		}
	}
	tr := obj(g.want(g.dispatcher.app(http.MethodGet, "/v1/logistics/trips/"+g.trip, nil), 200, "chuyến sau bù").Body, "trip")
	if sup != 1 || str(tr, "actual_kg") != g4HubInFixKg || str(tr, "actual_cost") != "4865000.00" {
		g.t.Fatalf("L-09/RB-1: bản bù hoàn tất chuyến + trip.completed có supersedes_id (%d): %v", sup, tr)
	}
	es := g.want(g.costAcc.app(http.MethodGet, "/v1/costing/lots/"+g.order+"/entries?limit=200", nil), 200, "bút toán lô lệnh gom")
	late := false
	for _, e := range es.Items() {
		if str(e, "category") == "transport_waterway" && str(e, "period_code") == nextCode && str(e, "late_from") == g.periodCode &&
			str(e, "delta_amount") == "25000.00" {
			late = true
		}
	}
	if !late {
		g.t.Fatalf("cước thủy bù (+25 000,00) vào kỳ mở kế tiếp %s, late_from %s: %s", nextCode, g.periodCode, trim(es.Raw))
	}
	st := g.want(g.costAcc.app(http.MethodGet, "/v1/costing/settlements/"+g.settlementID, nil), 200, "quyết toán sau bù")
	if str(st.Body, "status") != string(costport.SettlementApproved) || str(st.Body, "amount") != "62920000.00" {
		g.t.Fatalf("PM-04: quyết toán đã duyệt đóng băng (điều chỉnh = quyết toán điều chỉnh): %s", trim(st.Raw))
	}
	adj := g.want(g.costAcc.app(http.MethodPost, "/v1/costing/entries/"+g.handlingEntry+"/adjust", map[string]any{"amount": g4HandlingFix,
		"reason": "Hóa đơn bốc xếp về trễ — số thực 5,6 triệu"}), 201, "điều chỉnh chi phí trễ")
	if str(adj.Body, "period_code") != nextCode || str(adj.Body, "late_from") != g.periodCode || str(adj.Body, "supersedes_id") != g.handlingEntry ||
		str(adj.Body, "delta_amount") != "600000.00" || str(adj.Body, "entry_kind") != "adjusting" {
		g.t.Fatalf("C-10: điều chỉnh sau đóng kỳ ở kỳ mở kế tiếp, trỏ bút toán cũ: %s", trim(adj.Raw))
	}
	p := g.want(g.closer.app(http.MethodGet, "/v1/costing/periods/"+g.periodID, nil), 200, "kỳ đã đóng sau sửa sai")
	if str(p.Body, "status") != string(costport.PeriodPosted) || str(obj(p.Body, "summary"), "result_set_sha256") != g.resultSHA {
		g.t.Fatalf("kỳ đã đóng không đổi sau sửa sai: %s", trim(p.Raw))
	}
	if l := g.want(g.operator.app(http.MethodGet, "/v1/acctbridge/journals?period_id="+g.periodID, nil), 200, "bút toán kỳ cũ"); len(l.Items()) != 1 {
		g.t.Fatalf("không bút toán mới cho kỳ đã posted: %s", trim(l.Raw))
	}
}

// 17 — RB-1 (K-01): mọi sổ chuỗi băm của bốn module G4 mà kịch bản đã ghi — verify_chain đúng trên TỪNG stream của
// tenant MKG; UPDATE / DELETE bằng vai ứng dụng bị chặn.
func (g *g4) stepLedgersChained() {
	ctx := context.Background()
	for _, c := range []struct{ role, table string }{
		{logistics.DBRole, "mkg_logistics.trip_event"}, {logistics.DBRole, "mkg_logistics.leg_event"},
		{logistics.DBRole, "mkg_logistics.route_snapshot"}, {logistics.DBRole, "mkg_logistics.vessel_event"},
		{orders.DBRole, "mkg_orders.contract_event"}, {orders.DBRole, "mkg_orders.po_event"}, {orders.DBRole, "mkg_orders.receipt_event"},
		{orders.DBRole, "mkg_orders.order_event"}, {orders.DBRole, "mkg_orders.delivery_event"}, {orders.DBRole, "mkg_orders.ticket_event"},
		{costing.DBRole, "mkg_costing.input_event"}, {costing.DBRole, "mkg_costing.cost_entry"}, {costing.DBRole, "mkg_costing.allocation_result"},
		{costing.DBRole, "mkg_costing.conversion_result"}, {costing.DBRole, "mkg_costing.period_event"}, {costing.DBRole, "mkg_costing.period_summary"},
		{costing.DBRole, "mkg_costing.settlement_event"}, {acctbridge.DBRole, "mkg_acctbridge.mapping_event"},
		{acctbridge.DBRole, "mkg_acctbridge.journal_event"},
	} {
		pool := g.s.Env.Pool(g.t, migrations.MKG, c.role)
		var streams []string
		if err := tenant.WithTenantTx(ctx, pool, g.mkg, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT DISTINCT stream_key FROM "+c.table)
			if err != nil {
				return err
			}
			streams, err = pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			for _, sk := range streams {
				var ok bool
				if err := tx.QueryRow(ctx, "SELECT ok FROM shared.verify_chain($1::regclass, $2, $3)", c.table, g.mkg, sk).Scan(&ok); err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("verify_chain %s %s hỏng", c.table, sk)
				}
			}
			return nil
		}); err != nil {
			g.t.Fatalf("%s: %v", c.table, err)
		}
		if len(streams) == 0 {
			g.t.Fatalf("%s: kịch bản phải để lại dữ liệu trong sổ", c.table)
		}
		for _, stmt := range []string{"UPDATE " + c.table + " SET tenant_id = tenant_id", "DELETE FROM " + c.table} {
			if err := tenant.WithTenantTx(ctx, pool, g.mkg, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, stmt); return e }); err == nil {
				g.t.Fatalf("RB-1: %s bằng vai %s phải bị chặn", stmt, c.role)
			}
		}
	}
}

// 18 — R-22: băm mọi bảng chiếu của sáu module (tenant MKG) → dựng lại từ sổ (Module.RebuildTenant) → băm lại: bằng nhau.
func (g *g4) stepRebuild() {
	ctx := context.Background()
	type mod struct {
		schema, role string
		rebuild      func(context.Context, uuid.UUID) (map[string]int64, error)
	}
	m := g.m.Mods
	mods := []mod{
		{"mkg_hub", hub.DBRole, m.Hub.RebuildTenant}, {"mkg_measurement", measurement.DBRole, m.Measurement.RebuildTenant},
		{"mkg_logistics", logistics.DBRole, m.Logistics.RebuildTenant}, {"mkg_orders", orders.DBRole, m.Orders.RebuildTenant},
		{"mkg_costing", costing.DBRole, m.Costing.RebuildTenant}, {"mkg_acctbridge", acctbridge.DBRole, m.Acctbridge.RebuildTenant},
	}
	admin := g.s.Env.AdminPool(g.t, migrations.MKG)
	for _, x := range mods {
		rows, err := admin.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_type = 'BASE TABLE'
		  AND (table_name LIKE '%\_current' OR ($1 = 'mkg_hub' AND table_name = 'lot_balance')) ORDER BY table_name`, x.schema)
		if err != nil {
			g.t.Fatal(err)
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(tables) == 0 {
			g.t.Fatalf("%s: không có bảng chiếu: %v", x.schema, err)
		}
		pool := g.s.Env.Pool(g.t, migrations.MKG, x.role)
		hash := func() map[string]string {
			out := map[string]string{}
			for _, tb := range tables {
				var rows string
				q := fmt.Sprintf(`SELECT coalesce(string_agg(r, E'\n' ORDER BY r), '')
				  FROM (SELECT (to_jsonb(t) - 'updated_at')::text AS r FROM %s.%s t) x`, x.schema, tb)
				if err := tenant.WithTenantTx(ctx, pool, g.mkg, func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&rows) }); err != nil {
					g.t.Fatalf("%s.%s: %v", x.schema, tb, err)
				}
				out[tb] = rows
			}
			return out
		}
		before := hash()
		if _, err := x.rebuild(ctx, g.mkg); err != nil {
			g.t.Fatalf("dựng lại %s: %v", x.schema, err)
		}
		after := hash()
		for tb, h := range before {
			if after[tb] != h {
				g.t.Fatalf("R-22: %s.%s dựng lại từ sổ khác bảng chiếu hiện hành:\n%s\n→\n%s", x.schema, tb, h, after[tb])
			}
		}
	}
}

// 19 — R-61: giao lặp MỌI sự kiện không ghi thêm; cách ly tenant ở mọi bề mặt G4; tenant chương trình không có dòng
// mkg nào; KHÔNG dữ liệu G4 nào (giá, khách, cước, quyết toán, bút toán) qua van: sổ van chỉ thêm hợp đồng van đã
// công bố, không sự kiện G4 nào có kind khác internal, không giá trị G4 nào ở outbox / bảng nhận van của nền tảng.
func (g *g4) stepIsolationValve() {
	g.pm.redeliverAll()
	if n := g.pm.run(); n != 0 {
		g.t.Fatalf("không còn sự kiện mới sau khi giao hết, xử lý thêm %d", n)
	}
	for _, c := range []struct{ path, what string }{
		{"/v1/logistics/trips", "chuyến"}, {"/v1/logistics/vessels", "danh mục ghe"}, {"/v1/orders/contracts", "hợp đồng"},
		{"/v1/orders/purchase-orders", "đơn mua"}, {"/v1/orders/sales-orders", "đơn bán"}, {"/v1/costing/settlements", "quyết toán"},
		{"/v1/acctbridge/journals", "bút toán"},
	} {
		r := g.want(g.stranger.app(http.MethodGet, c.path, nil), 200, "tenant khác: "+c.what)
		if len(r.Items()) != 0 {
			g.t.Fatalf("RB-2: tenant khác thấy %s: %s", c.what, trim(r.Raw))
		}
	}
	for _, path := range []string{"/v1/logistics/trips/" + g.trip, "/v1/orders/contracts/" + g.contract, "/v1/orders/receipts/" + g.receipt,
		"/v1/orders/deliveries/" + g.deliveryID, "/v1/costing/settlements/" + g.settlementID, "/v1/costing/periods/" + g.periodID,
		"/v1/costing/lots/" + g.lot + "/cost", "/v1/acctbridge/journals/" + g.journal} {
		if r := g.stranger.app(http.MethodGet, path, nil); r.Status != http.StatusNotFound {
			g.t.Fatalf("RB-2: tenant khác đọc %s: %d %s", path, r.Status, trim(r.Raw))
		}
	}
	for _, c := range []struct{ role, q string }{
		{logistics.DBRole, "SELECT count(*) FROM mkg_logistics.trip_event"}, {orders.DBRole, "SELECT count(*) FROM mkg_orders.contract_event"},
		{costing.DBRole, "SELECT count(*) FROM mkg_costing.settlement_event"}, {acctbridge.DBRole, "SELECT count(*) FROM mkg_acctbridge.journal_event"},
	} {
		if n := g.countIn(migrations.MKG, c.role, g.tid, c.q); n != 0 {
			g.t.Fatalf("ADR-0020: tenant chương trình không có dòng mkg (%s): %d", c.q, n)
		}
		if n := g.countIn(migrations.MKG, c.role, g.mkg, c.q); n == 0 {
			g.t.Fatalf("kịch bản phải để lại dữ liệu (%s)", c.q)
		}
	}
	// Van: chỉ hợp đồng van đã công bố (contracts/publication), không dòng mới nào ngoài hợp đồng đó.
	allowed := map[string]bool{"collection_schedule": true, "htx_collection_statement": true, "hub_facility": true, "hub_capacity": true,
		"mrv_collection_evidence": true}
	after := g.valveCounts()
	for k, n := range after {
		contract := k[strings.LastIndex(k, ":")+1:]
		if !allowed[contract] {
			g.t.Fatalf("sổ van có hợp đồng lạ %s (%d dòng)", k, n)
		}
		if n < g.valveBefore[k] {
			g.t.Fatalf("sổ van chỉ ghi thêm: %s %d → %d", k, g.valveBefore[k], n)
		}
	}
	g4Types := []string{"mkg.logistics.", "mkg.orders.", "mkg.costing.", "mkg.acctbridge."}
	var platformPayloads strings.Builder
	for _, r := range g.pm.rows(migrations.MKG) {
		for _, p := range g4Types {
			if strings.HasPrefix(r.env.EventType, p) && r.kind != string(outbox.KindInternal) {
				g.t.Fatalf("ADR-0018: sự kiện G4 %s phát kind %s (chỉ internal)", r.env.EventType, r.kind)
			}
		}
		if r.kind != string(outbox.KindInternal) {
			platformPayloads.Write(r.env.Payload)
		}
	}
	for _, r := range g.pm.rows(migrations.PF) {
		platformPayloads.Write(r.env.Payload)
	}
	pf := platformPayloads.String()
	for _, x := range g.secret {
		if x != "" && strings.Contains(pf, x) {
			g.t.Fatalf("K-06: giá trị G4 %q lọt sang vùng nền tảng (van / outbox pf)", x)
		}
	}
	var recv string
	if err := tenant.WithTenantTx(context.Background(), g.s.Env.Pool(g.t, migrations.PF, "cropcycle_app"), g.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT coalesce(string_agg(payload::text, ' '), '') FROM pf_cropcycle.collection_valve_receipt").Scan(&recv)
	}); err != nil {
		g.t.Fatal(err)
	}
	for _, x := range g.secret {
		if x != "" && strings.Contains(recv, x) {
			g.t.Fatalf("K-06: giá trị G4 %q trong sổ nhận van của nền tảng", x)
		}
	}
}
