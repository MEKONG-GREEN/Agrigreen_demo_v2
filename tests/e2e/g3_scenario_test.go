package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	winternal "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/worker-internal/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Số liệu golden của prototype mekong-green (proto FM-04, FM-10; g3-conventions §1.1):
// cuộn 5 800 → tập kết 5 700 → xếp ghe 4 400; HUB_IN 138 400 − 41 600 = 96 800 kg so với
// 4 400 × 20 = 88 000 kg → +10 % cờ (hợp đồng theo tấn). HUB_OUT sau hao hụt sấy/lưu 1,5 %
// = 95 348 kg; khách cân 86 767 kg (−9 % so với HUB_OUT) → vượt dung sai 5 % (C-4 tạm) → tranh chấp.
const (
	g3Bale, g3Gather, g3BoatLoad = 5800, 5700, 4400
	g3Tare                       = int64(41_600)
	g3HubInNet                   = int64(96_800)
	g3HubOutNet                  = int64(95_348)
	g3CustomerKg                 = "86767.000"
)

// g3 là trạng thái dùng chung giữa các bước của kịch bản G3.
type g3 struct {
	t   *testing.T
	s   *harness.Stack
	p   *g1 // nền tảng: dùng lại các bước 01–03, 05 của kịch bản G1
	tid uuid.UUID
	m   *mkgStack
	pm  *pump

	op, appr, other mkgUser
	mgr             *harness.User // quản lý HTX Ninh Kiều trên nền tảng (coop_manager)
	devIn, devOut   *edgeScale

	today                           clock.Date
	seasonRef, facility, order, lot string
	statementRef, ticketIn          string
	evidenceRef                     string
	saToken                         string
}

// TestG3FieldToCustomer là kịch bản chấp nhận G3 (docs/uat/g3-acceptance.md) — "từ ruộng tới
// nhà máy khách": lệnh gom (lập ≠ duyệt) → đếm cuộn bale → gather → boat_load (proto FM-04) →
// van htx_collection_statement sang nền tảng → quản lý HTX xác nhận trên nền tảng → job MKG kéo
// phản hồi (RB-6 chiều mua) → phiên cân HUB_IN, lô số đo ký Ed25519 qua ingest mTLS → phiếu
// 96 800 kg (R-70) → nhận hub từ phiếu (Q-MKG-04) → xuất hub + phiếu HUB_OUT → CUSTOMER_IN nhập
// tay kèm bằng chứng vượt dung sai → đối soát gắn cờ + mở tranh chấp (RB-6 chiều bán) → van MRV
// bằng chứng thu gom → pf/mrv bằng chứng + độ đầy đủ (KN US-MRV-01..05) → phát lại GIS theo ngày
// (GIS US-REPLAY-01) + giá trị chỉ số ngày (pf/metrics). Cuối: RB-1 (sổ chỉ ghi thêm, số thô
// không xóa R-71), R-61 (giao lặp), cách ly tenant. R-03 do archlint + TestNoPlatformDependency.
func TestG3FieldToCustomer(t *testing.T) {
	s := harness.New(t)
	tid := harness.NewTenant()
	p := &g1{t: t, s: s, tid: tid, pubkeys: map[string][]byte{}}
	p.root = s.User(harness.UserSpec{Tenant: tid, MFA: true,
		Roles: []string{"platform_admin", "masterdata_admin", "registry_admin", "vault_registrar"}})
	g := &g3{t: t, s: s, p: p, tid: tid, pm: newPump(t, s, tid), today: clock.BusinessDate(time.Now())}
	g.devIn, g.devOut = newEdgeScale("e2e-g3-scale-in", "HUB_IN"), newEdgeScale("e2e-g3-scale-out", "HUB_OUT")
	g.m = newMKGStack(t, s, func() string { return g.saToken }, g.devIn.ID, g.devOut.ID)

	steps := []struct {
		name string
		fn   func()
	}{
		{"01_nen_tang_HTX_thua_vu", g.stepPlatform},
		{"02_MKG_api_internal_ingest_mTLS", g.stepMKGStack},
		{"03_co_so_hub_va_so_thiet_bi_R56", g.stepFacilityDevices},
		{"04_lenh_gom_lap_khac_duyet", g.stepOrder},
		{"05_dem_cuon_FM04", g.stepRollCounts},
		{"06_van_bang_ke_sang_nen_tang", g.stepStatementValve},
		{"07_HTX_xac_nhan_MKG_keo_ve_RB6_mua", g.stepHTXConfirm},
		{"08_can_HUB_IN_edge_ingest_R70", g.stepHubIn},
		{"09_nhap_hub_tu_phieu_QMKG04", g.stepHubReceipt},
		{"10_xuat_hub_can_HUB_OUT", g.stepHubOut},
		{"11_CUSTOMER_IN_tay_tranh_chap_RB6_ban", g.stepCustomerIn},
		{"12_van_MRV_bang_chung_thu_gom", g.stepMRV},
		{"13_GIS_phat_lai_va_chi_so_ngay", g.stepGISMetrics},
		{"14_RB1_so_chi_ghi_them_R71", g.stepAppendOnly},
		{"15_R61_giao_lap_va_cach_ly_tenant", g.stepIdempotencyIsolation},
	}
	for _, st := range steps {
		if !t.Run(st.name, func(t *testing.T) { g.t, g.p.t = t, t; st.fn() }) {
			t.Fatalf("bước %s hỏng — dừng kịch bản", st.name)
		}
	}
}

// ---------- trợ giúp ----------

func (g *g3) want(r harness.Resp, status int, what string) harness.Resp {
	g.t.Helper()
	if r.Status != status {
		g.t.Fatalf("%s: muốn %d, nhận %d %s", what, status, r.Status, trim(r.Raw))
	}
	return r
}

func (g *g3) wantFail(r harness.Resp, status int, what string) {
	g.t.Helper()
	if r.Status != status || !r.IsProblem() {
		g.t.Fatalf("%s: muốn problem+json %d, nhận %d %s", what, status, r.Status, trim(r.Raw))
	}
}

func (g *g3) ago(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339) }

func items(r harness.Resp) []map[string]any { return r.Items() }

// ---------- các bước ----------

// 01 — nền tảng: dữ liệu chủ, cây tổ chức + HTX Ninh Kiều, nông hộ (vault), hai thửa (bước G1),
// rồi SOP phát hành (người soạn ≠ người duyệt) + quản lý HTX mở vụ Đông Xuân cho thửa 1.
func (g *g3) stepPlatform() {
	g.p.stepSeed()
	g.p.stepOrgTree()
	g.p.stepHouseholds()
	g.p.stepPlots()

	sopAdmin := g.s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{"sop_admin"}})
	approver := g.s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{"param_approver"}})
	r := g.want(sopAdmin.Do(harness.Admin, http.MethodPost, "/v1/cropcycle/sops", "", map[string]any{"code": "SOP-G3-E2E", "name": "SOP lúa OM5451 (G3 e2e)",
		"steps": []map[string]any{
			{"seq": 1, "name": "Làm đất", "activity_code": "lam_dat", "stage_code": "lam_dat", "day_offset": -3, "window_days": 2, "mandatory": true},
			{"seq": 2, "name": "Gieo sạ", "activity_code": "gieo_sa", "day_offset": 0, "window_days": 1, "mandatory": true},
			{"seq": 3, "name": "Thu hoạch", "activity_code": "thu_hoach", "day_offset": 95, "window_days": 5, "mandatory": true},
		}}), 201, "tạo SOP")
	tmpl := r.Str("ref")
	vers, _ := r.Body["versions"].([]any)
	if len(vers) == 0 {
		g.t.Fatalf("SOP thiếu phiên bản: %s", trim(r.Raw))
	}
	ver := str(vers[0].(map[string]any), "id")
	g.want(approver.Do(harness.Admin, http.MethodPost, "/v1/cropcycle/sop-versions/"+ver+"/publish", "", nil), 200, "phát hành SOP")
	g.want(sopAdmin.Do(harness.Admin, http.MethodPost, "/v1/cropcycle/variety-bindings", "", map[string]any{"variety_code": "OM5451", "template_ref": tmpl}),
		200, "gắn giống")

	g.mgr = g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.p.coopNK), Roles: []string{"coop_manager"}})
	r = g.want(g.mgr.Do(harness.App, http.MethodPost, "/v1/cropcycle/seasons", "", map[string]any{
		"plot_ref": g.p.plots[0], "season_code": "DX", "season_year": 2026, "variety_code": "OM5451",
		"sowing_date": time.Now().In(clock.Zone()).Format(time.DateOnly),
		"gps":         map[string]any{"lon": "105.7704", "lat": "10.0304", "accuracy_m": "4"},
	}), 201, "quản lý HTX mở vụ cho thửa 1")
	g.seasonRef = str(obj(r.Body, "season"), "season_ref")

	// Worker nền tảng: plot.created → nhật ký lớp GIS; season.opened → pf/mrv + nhật ký lớp.
	if n := g.pm.run(); n == 0 {
		g.t.Fatal("không có sự kiện nền tảng nào được giao")
	}
	if g.pm.byConsumer[metricsport.LayerLogConsumer] < 3 {
		g.t.Fatalf("nhật ký lớp GIS phải nhận ≥ 3 sự kiện (2 thửa + vụ): %v", g.pm.byConsumer)
	}
}

// 02 — phía MKG: api-internal (fieldops đọc thửa/HTX qua Application API công khai bằng tài
// khoản dịch vụ), ingest mTLS với CA thiết bị dev. Người đề xuất và người duyệt là hai người (R-56).
func (g *g3) stepMKGStack() {
	// Tài khoản dịch vụ tenant MKG: đọc HTX/thửa + luồng phản hồi HTX (mkg_sync). RB-2 (cấp quyền
	// chia sẻ chương trình → MKG) chưa chốt: kịch bản dùng một tenant cho cả hai vùng.
	sa := g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.p.orgNational), Roles: []string{"kn_national_admin", "plot_viewer", "mkg_sync"}})
	g.saToken = sa.Token
	sign := func(tid uuid.UUID, roles ...string) mkgUser {
		return mkgUser{g: g, tok: g.s.Issuer.Sign(g.t, mkgToken(tid, roles))}
	}
	g.op = sign(g.tid, "field_supervisor", "field_collector", "scale_supervisor", "reconciliation_officer", "dispute_officer", "hub_operator")
	g.appr = sign(g.tid, "field_supervisor", "device_approver", "hub_approver")
	g.other = sign(ids.New(), "field_supervisor", "field_collector", "scale_supervisor", "reconciliation_officer", "dispute_officer", "hub_operator")
	if r := mkgDo(g.t, g.m.App.URL, "", http.MethodGet, "/readyz", nil); r.Status != 200 || r.Str("zone") != "mkg" {
		g.t.Fatalf("api-internal chưa sẵn sàng: %d %s", r.Status, trim(r.Raw))
	}
}

// 03 — cơ sở hub (đề xuất ≠ duyệt) và hai cân HUB_IN/HUB_OUT trong sổ thiết bị: khóa công khai
// Ed25519 + SHA-256 chứng thư mTLS; tự duyệt bị chặn (R-56).
func (g *g3) stepFacilityDevices() {
	r := g.want(g.op.adm(http.MethodPost, "/v1/hub/facilities", map[string]any{
		"code": "HUB-G3-E2E", "name": "Hub Ninh Kiều (G3 e2e)", "kind": "hub",
		"location":      map[string]any{"lat": "10.0452", "lng": "105.7469"},
		"boundary":      map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.74, 10.04}, {105.75, 10.04}, {105.75, 10.05}, {105.74, 10.05}, {105.74, 10.04}}}},
		"province_code": provCT, "capacity_kg": "500000", "effective_from": g.ago(time.Hour),
	}), 201, "đề xuất cơ sở hub")
	g.facility = r.Str("facility_id")
	g.wantFail(g.op.adm(http.MethodPost, "/v1/hub/facilities/"+g.facility+"/versions/1/decision", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "người đề xuất tự duyệt cơ sở (R-56)")
	g.want(g.appr.adm(http.MethodPost, "/v1/hub/facilities/"+g.facility+"/versions/1/decision", map[string]any{"decision": "approved"}), 201, "duyệt cơ sở")

	for _, d := range []*edgeScale{g.devIn, g.devOut} {
		r := g.want(g.op.adm(http.MethodPost, "/v1/measurement/devices", map[string]any{
			"device_id": d.ID, "facility_ref": g.facility, "point": d.Point, "public_key": d.publicKey(), "cert_sha256": g.m.ClientCertSHA,
			"scale_model": "synthetic-weighbridge (C-6 tạm)",
		}), 201, "đề xuất thiết bị "+d.ID)
		ep := int(num(r.Body, "epoch"))
		if ep < 1 {
			ep = 1
		}
		path := fmt.Sprintf("/v1/measurement/devices/%s/epochs/%d/decision", d.ID, ep)
		if r := g.op.adm(http.MethodPost, path, map[string]any{"decision": "approved"}); r.Status < 400 {
			g.t.Fatalf("R-56: người đề xuất không được tự duyệt thiết bị: %d %s", r.Status, trim(r.Raw))
		}
		g.want(g.appr.adm(http.MethodPost, path, map[string]any{"decision": "approved"}), 201, "duyệt thiết bị "+d.ID)
	}
	// Thiết bị chưa ký đúng khóa: ingest từ chối (chữ ký nguồn bắt buộc, ADR-0019).
	bad := newEdgeScale(g.devIn.ID, "HUB_IN")
	bad.Key = newEdgeScale("khoa-khac", "HUB_IN").Key
	bs, _, _ := bad.batches(g.t, weighReadings(1000, 100)[:1])
	if st, body := g.m.push(g.t, bs[0]); st == http.StatusOK {
		g.t.Fatalf("lô ký sai khóa phải bị từ chối: %d %s", st, body)
	}
}

// 04 — lệnh gom cho HTX Ninh Kiều, thửa 1: lập (giám sát hiện trường) ≠ duyệt (R-56). fieldops
// kiểm HTX/thửa qua Application API nền tảng (tài khoản dịch vụ); thửa lạ bị từ chối.
func (g *g3) stepOrder() {
	day := time.Now().In(clock.Zone())
	body := map[string]any{"htx_ref": g.p.coopNK, "window_from": day.Format(time.DateOnly), "window_to": day.AddDate(0, 0, 5).Format(time.DateOnly),
		"plot_refs": []string{g.p.plots[0]}}
	bad := map[string]any{"htx_ref": g.p.coopNK, "window_from": day.Format(time.DateOnly), "window_to": day.AddDate(0, 0, 5).Format(time.DateOnly),
		"plot_refs": []string{ids.New().String()}}
	if r := g.op.app(http.MethodPost, "/v1/fieldops/orders", bad); r.Status < 400 {
		g.t.Fatalf("thửa không có trên nền tảng phải bị từ chối: %d %s", r.Status, trim(r.Raw))
	}
	r := g.want(g.op.app(http.MethodPost, "/v1/fieldops/orders", body), 201, "lập lệnh gom")
	g.order = str(obj(r.Body, "order"), "order_id")
	if r := g.op.app(http.MethodPost, "/v1/fieldops/orders/"+g.order+"/decision", map[string]any{"decision": "approved"}); r.Status < 400 {
		g.t.Fatalf("R-56: người lập không được tự duyệt lệnh gom: %d %s", r.Status, trim(r.Raw))
	}
	g.want(g.appr.app(http.MethodPost, "/v1/fieldops/orders/"+g.order+"/decision", map[string]any{"decision": "approved"}), 201, "duyệt lệnh gom")
}

// 05 — đếm cuộn theo công đoạn (proto FM-04): công đoạn sau ít hơn phải có lý do chênh.
func (g *g3) stepRollCounts() {
	path := "/v1/fieldops/orders/" + g.order + "/roll-counts"
	count := func(stage string, rolls int, reason string, back time.Duration) harness.Resp {
		b := map[string]any{"plot_ref": g.p.plots[0], "stage": stage, "rolls": rolls, "event_time": g.ago(back)}
		if reason != "" {
			b["discrepancy_reason"] = reason
		}
		return g.op.app(http.MethodPost, path, b)
	}
	g.want(count("bale", g3Bale, "", 3*time.Hour), 201, "cuộn")
	g.wantFail(count("gather", g3Gather, "", 2*time.Hour), http.StatusUnprocessableEntity, "tập kết ít hơn cuộn mà không có lý do (FM-04)")
	g.want(count("gather", g3Gather, "100 cuộn ướt để lại bờ ruộng (proto seed)", 2*time.Hour), 201, "tập kết")
	r := g.want(count("boat_load", g3BoatLoad, "Ghe thứ hai 1 300 cuộn thuộc lệnh gom kế tiếp", time.Hour), 201, "xếp ghe")
	g.statementRef = str(obj(r.Body, "reconciliation"), "statement_ref")
	if g.statementRef == "" {
		g.t.Fatalf("xếp ghe phải sinh bảng kê cho HTX: %s", trim(r.Raw))
	}
	c := g.want(g.op.app(http.MethodGet, path, nil), 200, "đọc số cuộn")
	if n := len(c.Items("counts")); n != 3 { // bản bị từ chối (thiếu lý do) không được ghi
		g.t.Fatalf("số bản ghi đếm cuộn: %d %s", n, trim(c.Raw))
	}
}

// 06 — van htx_collection_statement (và collection_schedule) sang nền tảng qua bơm outbox; sự kiện
// nội bộ roll_count.recorded → hub + measurement (vhost mkg.internal). Quản lý HTX thấy bảng kê chờ.
func (g *g3) stepStatementValve() {
	g.pm.run()
	for _, c := range []string{"pf.cropcycle.htx_collection_valve", "mkg.hub.roll_count", winternal.RollCountReconcileConsumer} {
		if g.pm.byConsumer[c] == 0 {
			g.t.Fatalf("consumer %s chưa nhận sự kiện: %v", c, g.pm.byConsumer)
		}
	}
	r := g.want(g.mgr.Do(harness.App, http.MethodGet, "/v1/cropcycle/htx-collection/statements?status=pending", "", nil), 200, "bảng kê chờ")
	it := r.Items()
	if len(it) != 1 || str(it[0], "statement_ref") != g.statementRef || num(it[0], "rolls") != g3BoatLoad || str(it[0], "plot_code") == "" {
		g.t.Fatalf("bảng kê chờ phản hồi (4 400 cuộn, mã thửa): %s", trim(r.Raw))
	}
	for _, k := range []string{"mass_kg", "kg_per_roll", "yield_kg_per_roll", "price"} {
		if _, ok := it[0][k]; ok {
			g.t.Fatalf("bảng kê không được mang %s (không bao giờ qua van): %v", k, it[0])
		}
	}
	if r := g.want(g.mgr.Do(harness.App, http.MethodGet, "/v1/cropcycle/htx-collection/schedules", "", nil), 200, "lịch gom"); len(r.Items()) != 1 {
		g.t.Fatalf("lịch gom HTX: %s", trim(r.Raw))
	}
}

// 07 — RB-6 chiều mua: quản lý HTX xác nhận trên nền tảng; job fieldops_htx_responses (leader
// api-internal) kéo luồng phản hồi qua Application API → xác nhận kênh platform, đối soát khớp.
func (g *g3) stepHTXConfirm() {
	g.want(g.mgr.Do(harness.App, http.MethodPost, "/v1/cropcycle/htx-collection/statements/"+g.statementRef+"/responses", "",
		map[string]any{"revision": 1, "outcome": "confirmed"}), 201, "HTX xác nhận bảng kê")
	ctx := context.Background()
	res, err := g.m.Mods.Fieldops.SyncHTXResponses(ctx, g.tid)
	if err != nil || res.Recorded != 1 {
		g.t.Fatalf("job kéo phản hồi HTX: %+v %v", res, err)
	}
	if res, err := g.m.Mods.Fieldops.SyncHTXResponses(ctx, g.tid); err != nil || res.Recorded != 0 {
		g.t.Fatalf("kéo lặp không được ghi trùng: %+v %v", res, err)
	}
	rec := g.want(g.op.app(http.MethodGet, "/v1/fieldops/reconciliation?order_id="+g.order, nil), 200, "đối soát chiều mua")
	if it := items(rec); len(it) != 1 || str(it[0], "status") != "match" {
		g.t.Fatalf("đối soát HTX phải khớp: %s", trim(rec.Raw))
	}
	cf := g.want(g.op.app(http.MethodGet, "/v1/fieldops/orders/"+g.order+"/htx-confirmations", nil), 200, "xác nhận HTX")
	if it := items(cf); len(it) != 1 || str(it[0], "channel") != "platform" {
		g.t.Fatalf("xác nhận kênh platform: %s", trim(cf.Raw))
	}
}

// weigh đẩy lô số đo ký của một phiên qua mTLS, mở + đóng phiên, chạy vòng tích phân; trả phiếu.
func (g *g3) weigh(d *edgeScale, gross int64, moisture string, order string) map[string]any {
	g.t.Helper()
	bs, from, to := d.batches(g.t, weighReadings(gross, g3Tare))
	for _, b := range bs {
		if st, body := g.m.push(g.t, b); st != http.StatusOK {
			g.t.Fatalf("ingest lô %s [%d..%d]: %d %s", d.ID, b.CounterFrom, b.CounterTo, st, body)
		}
	}
	if st, body := g.m.push(g.t, bs[0]); st != http.StatusOK { // phát lại: idempotent theo (device, epoch, counter)
		g.t.Fatalf("phát lại lô phải là idempotent: %d %s", st, body)
	}
	body := map[string]any{"lot_ref": g.lot, "device_id": d.ID, "epoch": 1, "counter_from": from}
	if order != "" {
		body["collection_order_ref"] = order
	}
	sess := g.want(g.op.app(http.MethodPost, "/v1/measurement/sessions", body), 201, "mở phiên cân "+d.Point)
	g.want(g.op.app(http.MethodPost, "/v1/measurement/sessions/"+sess.Str("id")+"/close",
		map[string]any{"counter_to": to, "tare_kg": fmt.Sprintf("%d.000", g3Tare), "moisture_pct": moisture}), 201, "đóng phiên "+d.Point)
	if _, err := g.m.Loop.RunOnce(context.Background()); err != nil {
		g.t.Fatal(err)
	}
	tk := g.want(g.op.app(http.MethodGet, "/v1/measurement/lots/"+g.lot+"/tickets", nil), 200, "phiếu của lô")
	arr, _ := tk.Body["tickets"].([]any)
	for _, x := range arr {
		if m := x.(map[string]any); str(m, "point") == d.Point {
			return m
		}
	}
	g.t.Fatalf("chưa có phiếu %s: %s", d.Point, trim(tk.Raw))
	return nil
}

// 08 — cân HUB_IN: agent biên ký Ed25519 + bộ đếm, ingest mTLS lưu số thô (ts_weigh), CHỈ ingest
// tích phân thành phiếu (R-70, weigh.integrate.v1 tạm) = 96 800 kg; phiên mang lệnh gom (lô ↔ lệnh gom).
func (g *g3) stepHubIn() {
	g.lot = ids.New().String()
	g.want(g.op.app(http.MethodPost, "/v1/measurement/lots/"+g.lot+"/terms", map[string]any{"pricing_basis": "per_tonne"}), 201, "cách tính giá theo tấn")
	tk := g.weigh(g.devIn, g3HubInNet+g3Tare, "18.00", g.order)
	if str(tk, "mass_kg") != "96800.000" {
		g.t.Fatalf("phiếu HUB_IN golden 96 800 kg: %v", tk)
	}
	g.ticketIn = str(tk, "id")
	if g.ticketIn == "" {
		g.ticketIn = str(tk, "ticket_id")
	}
	// Không có đường tạo phiếu HUB_IN bằng tay khi đã có cân (R-70): phiếu đến từ ingest.
	lk := g.want(g.op.app(http.MethodGet, "/v1/measurement/lots/"+g.lot+"/collection", nil), 200, "liên kết lô ↔ lệnh gom")
	if lk.Body["explicit"] != true || lk.Str("collection_order_ref") != g.order {
		g.t.Fatalf("liên kết tường minh qua phiên cân: %s", trim(lk.Raw))
	}
	// Số thô ở cụm ts (vai ts_reader, RLS theo tenant).
	var raw int
	if err := tenant.WithTenantTx(context.Background(), g.s.Env.Pool(g.t, migrations.TS, mport.TSReaderRole), g.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM ts_weigh.raw_reading WHERE device_id = $1", g.devIn.ID).Scan(&raw)
	}); err != nil {
		g.t.Fatal(err)
	}
	if raw != 9 {
		g.t.Fatalf("ts_weigh.raw_reading phải có đúng 9 số đo (phát lại không nhân đôi), có %d", raw)
	}
}

// 09 — nhận hub từ phiếu (Q-MKG-04): phiếu HUB_IN đến hub trước (pending), nhận bằng
// weigh_ticket_ref — khối lượng và id lô theo phiếu, nguồn là thửa 1.
func (g *g3) stepHubReceipt() {
	g.pm.run()
	if g.pm.byConsumer["mkg.hub.weigh_ticket"] == 0 || g.pm.byConsumer["mkg.fieldops.weigh_ticket"] == 0 {
		g.t.Fatalf("phiếu cân phải tới hub và fieldops: %v", g.pm.byConsumer)
	}
	r := g.want(g.op.app(http.MethodPost, "/v1/hub/facilities/"+g.facility+"/receipts", map[string]any{
		"product": "straw_bale", "weigh_ticket_ref": g.ticketIn, "event_time": g.ago(30 * time.Minute),
		"sources": []map[string]any{{"kind": "plot", "ref": g.p.plots[0], "ratio": "1"}},
	}), 201, "nhận hub bằng phiếu cân")
	if str(obj(r.Body, "lot"), "id") != g.lot || str(obj(r.Body, "lot"), "initial_kg") != "96800.000" {
		g.t.Fatalf("lô hub theo phiếu: %s", trim(r.Raw))
	}
	again := g.op.app(http.MethodPost, "/v1/hub/facilities/"+g.facility+"/receipts", map[string]any{
		"product": "straw_bale", "weigh_ticket_ref": g.ticketIn, "event_time": g.ago(29 * time.Minute),
		"sources": []map[string]any{{"kind": "plot", "ref": g.p.plots[0], "ratio": "1"}}})
	if again.Code() != "hub.weigh_ticket_used" {
		g.t.Fatalf("phiếu dùng hai lần: %d %s", again.Status, trim(again.Raw))
	}
}

// 10 — xuất hub (phân bổ từ lô) rồi cân HUB_OUT qua ingest: 95 348 kg; hub gắn phiếu ↔ phiếu xuất.
func (g *g3) stepHubOut() {
	g.want(g.op.app(http.MethodPost, "/v1/hub/facilities/"+g.facility+"/dispatches", map[string]any{
		"mass_kg": fmt.Sprintf("%d.000", g3HubOutNet), "allocations": []map[string]any{{"lot_id": g.lot, "mass_kg": fmt.Sprintf("%d.000", g3HubOutNet)}},
	}), 201, "xuất hub")
	tk := g.weigh(g.devOut, g3HubOutNet+g3Tare, "14.50", "")
	if str(tk, "mass_kg") != "95348.000" {
		g.t.Fatalf("phiếu HUB_OUT: %v", tk)
	}
	g.pm.run()
	m := g.want(g.op.app(http.MethodGet, "/v1/hub/lots/"+g.lot+"/measurements", nil), 200, "đo lường lô hub")
	var linked bool
	for _, l := range m.Items("links") {
		linked = linked || str(l, "outcome") == "dispatch_linked"
	}
	if !linked {
		g.t.Fatalf("phiếu HUB_OUT phải gắn với phiếu xuất: %s", trim(m.Raw))
	}
}

// 11 — CUSTOMER_IN nhập tay kèm bằng chứng (C-5 tạm): −9 % so với HUB_OUT, vượt dung sai →
// đối soát (hao hụt nội bộ HUB_IN→HUB_OUT, chênh thương mại HUB_OUT→CUSTOMER_IN, kiểm chéo cuộn
// ↔ khối lượng +10 % cờ) và mở tranh chấp (RB-6 chiều bán).
func (g *g3) stepCustomerIn() {
	path := "/v1/measurement/lots/" + g.lot + "/tickets"
	g.wantFail(g.op.app(http.MethodPost, path, map[string]any{"point": "CUSTOMER_IN", "mass_kg": g3CustomerKg, "weighed_at": g.ago(10 * time.Minute),
		"reason": "Phiếu cân nhà máy khách"}), http.StatusUnprocessableEntity, "CUSTOMER_IN thiếu bằng chứng")
	g.wantFail(g.op.app(http.MethodPost, path, map[string]any{"point": "HUB_IN", "mass_kg": "1000", "weighed_at": g.ago(10 * time.Minute),
		"evidence_ref": ids.New().String(), "reason": "Nhập tay thử khi cân vẫn hoạt động"}), http.StatusUnprocessableEntity, "phiếu HUB_IN thứ hai không thay phiếu hiện hành (R-70: sửa = phiếu bù)")
	g.evidenceRef = ids.New().String()
	r := g.want(g.op.app(http.MethodPost, path, map[string]any{"point": "CUSTOMER_IN", "mass_kg": g3CustomerKg, "weighed_at": g.ago(10 * time.Minute),
		"evidence_ref": g.evidenceRef, "reason": "Phiếu cân nhà máy khách (C-5 tạm: nhập tay kèm bằng chứng)"}), 201, "CUSTOMER_IN")
	rc := obj(r.Body, "reconciliation")
	roll, in, co := obj(rc, "roll_check"), obj(rc, "internal"), obj(rc, "commercial")
	if str(rc, "outcome") != mport.OutcomeDisputeOpened || str(rc, "dispute_id") == "" || str(rc, "pricing_basis") != "per_tonne" {
		g.t.Fatalf("vượt dung sai phải mở tranh chấp: %s", trim(r.Raw))
	}
	if str(roll, "status") != "flag" || str(roll, "variance_pct") != "10.00" || str(roll, "estimate_kg") != "88000.000" {
		g.t.Fatalf("kiểm chéo cuộn ↔ khối lượng (proto FM-10): %v", roll)
	}
	if str(in, "loss_kg") != "1452.000" || str(co, "diff_kg") == "" {
		g.t.Fatalf("hao hụt nội bộ 96 800 → 95 348 = 1 452 kg, chênh thương mại: %v %v", in, co)
	}
	d := g.want(g.op.app(http.MethodGet, "/v1/measurement/disputes", nil), 200, "danh sách tranh chấp")
	var dispute map[string]any
	for _, it := range d.Items() {
		if str(it, "lot_ref") == g.lot {
			dispute = it
		}
	}
	if dispute == nil || str(dispute, "status") == "" {
		g.t.Fatalf("tranh chấp của lô: %s", trim(d.Raw))
	}
	g.pm.run() // reconciliation.completed → hub quy kết hao hụt theo cơ sở
	if g.pm.byConsumer["mkg.hub.reconciliation"] == 0 {
		g.t.Fatalf("hub phải nhận kết quả đối soát: %v", g.pm.byConsumer)
	}
}

// 12 — van mrv_collection_evidence: bậc khối lượng theo HUB_IN × tỷ lệ nguồn thửa (không khối
// lượng chính xác) → pf/mrv ghi bằng chứng thu gom gắn vụ + độ đầy đủ; ước tính CO2 là TẠM.
func (g *g3) stepMRV() {
	g.want(g.op.app(http.MethodPost, "/v1/measurement/lots/"+g.lot+"/mrv-evidence", map[string]any{"season_ref": g.seasonRef}), 201, "van MRV")
	pubs := g.want(g.op.app(http.MethodGet, "/v1/measurement/lots/"+g.lot+"/publications", nil), 200, "bản ghi van")
	it := pubs.Items()
	if len(it) != 1 {
		g.t.Fatalf("một thửa → một bản ghi van: %s", trim(pubs.Raw))
	}
	pl := obj(it[0], "payload")
	if str(pl, "collected_kg_bucket") != "10000+" || str(pl, "mass_basis") != mport.MassBasisHubIn || str(pl, "plot_ref") != g.p.plots[0] ||
		str(pl, "season_ref") != g.seasonRef {
		g.t.Fatalf("payload van MRV: %v", pl)
	}
	for _, k := range []string{"mass_kg", "ticket_id", "customer_ref", "dispute_id", "lot_ref"} {
		if _, ok := pl[k]; ok {
			g.t.Fatalf("van MRV không được mang %s: %v", k, pl)
		}
	}
	g.pm.run()
	if g.pm.byConsumer["pf.mrv.collection_evidence"] == 0 {
		g.t.Fatalf("pf/mrv chưa nhận van: %v", g.pm.byConsumer)
	}
	view := g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.p.orgP92), Roles: []string{"mrv_viewer"}})
	c := g.want(view.Do(harness.App, http.MethodGet, "/v1/mrv/seasons/"+g.seasonRef+"/completeness", "", nil), 200, "độ đầy đủ MRV của vụ")
	var col map[string]any
	for _, x := range c.Items() {
		if str(x, "key") == "collection_evidence" {
			col = x
		}
	}
	if col == nil || num(col, "count") != 1 || str(col, "status") != "present" {
		g.t.Fatalf("bằng chứng thu gom trong độ đầy đủ: %s", trim(c.Raw))
	}
	if est := obj(c.Body, "estimate"); est != nil && est["official"] == true {
		g.t.Fatalf("ước tính CO2 phải là TẠM khi chưa ký phương pháp (C-1): %v", est)
	}
}

// 13 — GIS US-REPLAY-01: phát lại lớp thửa theo ngày = ảnh chụp + sự kiện sau watermark, thấy thửa
// + HTX; ngày trước khi số hoá → rỗng. pf/metrics: giá trị ngày của geo.plots_digitised được ghi sổ.
func (g *g3) stepGISMetrics() {
	g.pm.run()
	ctx := context.Background()
	key := metricsport.Key("geo.plots_digitised")
	viewer := g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.p.orgNational), Roles: []string{"kn_national_admin", "plot_viewer", "metrics_viewer"}})
	get := func(path string) harness.Resp { return viewer.Do(harness.App, http.MethodGet, path, "", nil) }
	svc := g.s.Mods.Metrics.Service()
	y := clock.BusinessDate(time.Now().AddDate(0, 0, -1))

	// Job metrics_snapshot (00:45) cho ngày D-1 đã kết thúc: ghi sổ chỉ số + ảnh chụp lớp cuối ngày.
	if _, err := svc.RecordDay(ctx, g.tid, y, []metricsport.Key{key}); err != nil {
		g.t.Fatal(err)
	}
	if _, err := svc.BuildSnapshots(ctx, g.tid, y); err != nil {
		g.t.Fatal(err)
	}
	if r := g.want(get(fmt.Sprintf("/v1/metrics/%s/values?from=%s&to=%s", key, y, y)), 200, "chỉ số D-1"); len(r.Items()) != 1 ||
		str(r.Items()[0], "business_date") != y.String() || !strings.HasPrefix(str(r.Items()[0], "value"), "0") {
		g.t.Fatalf("D-1 (trước khi số hoá) ghi sổ 0 thửa: %s", trim(r.Raw))
	}
	// Ngày D chưa kết thúc: ghi sổ bị từ chối ở app VÀ ở DB (CHECK recorded_at >= cutoff) — kể cả
	// với đồng hồ giả "hai ngày sau"; giá trị "như hệ biết lúc hết ngày D" vẫn tính được (AsOf).
	if _, err := svc.RecordDay(ctx, g.tid, g.today, []metricsport.Key{key}); err == nil {
		g.t.Fatal("ghi sổ ngày chưa kết thúc phải bị từ chối (metrics.not_closed)")
	}
	later := harness.Build(g.s.PF, clock.NewFixed(time.Now().Add(48*time.Hour)), g.s.VaultClient, g.s.Store)
	if _, err := later.Metrics.Service().RecordDay(ctx, g.tid, g.today, []metricsport.Key{key}); err == nil {
		g.t.Fatal("DB phải chặn giá trị ghi trước cutoff của ngày")
	}
	v, err := later.Metrics.Service().AsOf(ctx, key, g.today, metricsport.Scope{TenantID: g.tid, Units: []string{communeNK}})
	if err != nil || v.Value.String() != "2" || !v.Cutoff.Equal(metricsport.Cutoff(g.today)) {
		g.t.Fatalf("AsOf(D) xã %s = 2 thửa: %+v %v", communeNK, v, err)
	}

	// GIS US-REPLAY-01: replay(D) = ảnh chụp mới nhất ≤ D (D-1, rỗng) + sự kiện sau watermark.
	r := g.want(get("/v1/gis/replay?layers=plot&date="+g.today.String()), 200, "phát lại ngày D")
	var plot map[string]any
	features := 0
	for _, l := range r.Items() {
		for _, f := range l["features"].([]any) {
			fm := f.(map[string]any)
			features++
			if str(fm, "feature_ref") == g.p.plots[0] {
				plot = fm
			}
		}
	}
	if features != 2 || plot == nil || plot["retired"] == true || str(obj(plot, "attributes"), "cooperative_ref") != g.p.coopNK ||
		str(obj(plot, "attributes"), "status") != "active" {
		g.t.Fatalf("phát lại lớp thửa (2 thửa, thửa 1 thuộc HTX Ninh Kiều): %s", trim(r.Raw))
	}
	full, err := svc.FullReplay(ctx, g.tid, metricsport.LayerPlot, g.today)
	if err != nil || len(full.Features) != 2 {
		g.t.Fatalf("dựng thẳng từ nhật ký: %d %v", len(full.Features), err)
	}
	r = g.want(get("/v1/gis/replay?layers=plot&date="+y.String()), 200, "phát lại ngày D-1")
	for _, l := range r.Items() {
		if fs, _ := l["features"].([]any); len(fs) != 0 {
			g.t.Fatalf("ngày D-1 chưa có thửa: %s", trim(r.Raw))
		}
	}
}

// ledgerCase là một sổ chỉ ghi thêm cần thử UPDATE/DELETE bằng vai ứng dụng của nó.
type ledgerCase struct {
	cluster migrations.Cluster
	role    string
	table   string
}

// 14 — RB-1: sửa = bản ghi bù; không UPDATE/DELETE trên sổ của cả hai vùng và cụm ts. R-71: số
// đo thô không bao giờ bị xóa (ts_weigh chỉ SELECT/INSERT, trigger chặn cả owner).
func (g *g3) stepAppendOnly() {
	ctx := context.Background()
	cases := []ledgerCase{
		{migrations.MKG, fieldops.DBRole, "mkg_fieldops.roll_count"},
		{migrations.MKG, fieldops.DBRole, "mkg_fieldops.htx_confirmation"},
		{migrations.MKG, fieldops.DBRole, "mkg_fieldops.valve_export"},
		{migrations.MKG, measurement.DBRole, "mkg_measurement.weigh_ticket"},
		{migrations.MKG, measurement.DBRole, "mkg_measurement.reconciliation"},
		{migrations.MKG, measurement.DBRole, "mkg_measurement.dispute_event"},
		{migrations.MKG, measurement.DBRole, "mkg_measurement.lot_collection"},
		{migrations.MKG, measurement.DBRole, "mkg_measurement.valve_export"},
		{migrations.MKG, hub.DBRole, "mkg_hub.weigh_link"},
		{migrations.MKG, hub.DBRole, "mkg_hub.stock_movement"},
		{migrations.TS, mport.TSReaderRole, "ts_weigh.raw_reading"},
		{migrations.TS, mport.TSReaderRole, "ts_weigh.reading_batch"},
		{migrations.PF, cropcycle.DBRole, "pf_cropcycle.collection_valve_receipt"},
		{migrations.PF, cropcycle.DBRole, "pf_cropcycle.collection_response"},
		{migrations.PF, mrv.DBRole, "pf_mrv.collection_evidence"},
		{migrations.PF, metrics.DBRole, "pf_metrics.layer_event"},
		{migrations.PF, metrics.DBRole, "pf_metrics.daily_value"},
	}
	for _, c := range cases {
		pool := g.s.Env.Pool(g.t, c.cluster, c.role)
		var n int
		if err := tenant.WithTenantTx(ctx, pool, g.tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+c.table).Scan(&n)
		}); err != nil {
			g.t.Fatalf("%s: %v", c.table, err)
		}
		if n == 0 {
			g.t.Fatalf("%s: kịch bản phải để lại dữ liệu trong sổ", c.table)
		}
		writers := []string{c.role}
		if c.cluster == migrations.TS {
			writers = append(writers, mport.IngestTSRole) // vai ghi của ingest cũng không sửa/xóa được
		}
		for _, role := range writers {
			wp := g.s.Env.Pool(g.t, c.cluster, role)
			for _, stmt := range []string{"UPDATE " + c.table + " SET tenant_id = tenant_id", "DELETE FROM " + c.table} {
				err := tenant.WithTenantTx(ctx, wp, g.tid, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, stmt); return e })
				if err == nil {
					g.t.Fatalf("RB-1: %s bằng vai %s phải bị chặn", stmt, role)
				}
			}
		}
		// Owner (migrator) cũng bị trigger chặn — chỉ thử trên cụm ts và sổ phiếu cân (R-71, R-70).
		if c.table == "ts_weigh.raw_reading" || c.table == "mkg_measurement.weigh_ticket" {
			admin := g.s.Env.AdminPool(g.t, c.cluster)
			if _, err := admin.Exec(ctx, "DELETE FROM "+c.table+" WHERE tenant_id = $1", g.tid); err == nil {
				g.t.Fatalf("R-71/RB-1: owner xóa được %s", c.table)
			}
		}
	}
}

// 15 — R-61: giao lặp mọi sự kiện (MKG nội bộ, van, nền tảng) không ghi thêm; cách ly tenant ở
// cả hai vùng; không dữ liệu cá nhân trong outbox MKG và log.
func (g *g3) stepIdempotencyIsolation() {
	g.pm.redeliverAll()
	if n := g.pm.run(); n != 0 {
		g.t.Fatalf("không còn sự kiện mới sau khi giao hết, xử lý thêm %d", n)
	}
	// MKG: tenant khác không thấy lệnh gom, phiếu, tranh chấp, lô hub.
	if r := g.want(g.other.app(http.MethodGet, "/v1/fieldops/orders", nil), 200, "tenant khác: lệnh gom"); len(r.Items()) != 0 {
		g.t.Fatalf("tenant khác thấy lệnh gom: %s", trim(r.Raw))
	}
	if r := g.other.app(http.MethodGet, "/v1/measurement/lots/"+g.lot+"/tickets", nil); r.Status == 200 {
		if ts, _ := r.Body["tickets"].([]any); len(ts) != 0 {
			g.t.Fatalf("tenant khác thấy phiếu cân: %s", trim(r.Raw))
		}
	}
	if r := g.want(g.other.app(http.MethodGet, "/v1/measurement/disputes", nil), 200, "tenant khác: tranh chấp"); len(r.Items()) != 0 {
		g.t.Fatalf("tenant khác thấy tranh chấp: %s", trim(r.Raw))
	}
	if r := g.other.app(http.MethodGet, "/v1/hub/lots/"+g.lot, nil); r.Status != http.StatusNotFound {
		g.t.Fatalf("tenant khác đọc lô hub: %d %s", r.Status, trim(r.Raw))
	}
	// Nền tảng: quản lý HTX cùng mã HTX ở tenant khác thấy rỗng; MRV tenant khác không thấy vụ.
	om := g.s.User(harness.UserSpec{Tenant: ids.New(), Org: uuid.MustParse(g.p.coopNK), Roles: []string{"coop_manager"}})
	if r := g.want(om.Do(harness.App, http.MethodGet, "/v1/cropcycle/htx-collection/statements", "", nil), 200, "tenant khác: bảng kê"); len(r.Items()) != 0 {
		g.t.Fatalf("tenant khác thấy bảng kê: %s", trim(r.Raw))
	}
	ov := g.s.User(harness.UserSpec{Tenant: ids.New(), Org: uuid.MustParse(g.p.orgP92), Roles: []string{"mrv_viewer"}})
	if r := ov.Do(harness.App, http.MethodGet, "/v1/mrv/seasons/"+g.seasonRef+"/completeness", "", nil); r.Status != http.StatusNotFound {
		g.t.Fatalf("tenant khác đọc độ đầy đủ MRV: %d %s", r.Status, trim(r.Raw))
	}
	// Không dữ liệu cá nhân (tên/SĐT nông hộ tổng hợp) trong outbox MKG hay log của mọi server.
	var payloads string
	if err := g.s.Env.Pool(g.t, migrations.MKG, "ag_outbox_publisher").QueryRow(context.Background(),
		"SELECT coalesce(string_agg(payload::text, ' '), '') FROM shared.outbox WHERE tenant_id = $1", g.tid).Scan(&payloads); err != nil {
		g.t.Fatal(err)
	}
	logs := g.s.Logs.String()
	for _, x := range pii {
		if strings.Contains(payloads, x) || strings.Contains(logs, x) {
			g.t.Fatalf("dữ liệu cá nhân %q lọt ra outbox MKG / log", x)
		}
	}
}
