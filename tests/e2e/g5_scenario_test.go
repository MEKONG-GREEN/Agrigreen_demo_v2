package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	publicasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// g5UpdateGolden ghi lại tests/e2e/testdata/g5_statement.golden.csv (go test ./tests/e2e -run TestG5 -g5-update).
var g5UpdateGolden = flag.Bool("g5-update", false, "ghi lại golden bảng kê G5")

// Số liệu của kịch bản G5 (g5-conventions §6, §11.3; proto SUB-DATA):
//   - tenant chương trình (chủ dữ liệu): xã 31135 (P. Ninh Kiều, TP Cần Thơ) — 6 nông hộ, 6 thửa × 1,5 ha, 6 bảng kê × 10
//     cuộn tháng 2026-09; xã 31120 — 4 nông hộ tháng 2026-08. k mặc định 10 → cả hai ô bị ẩn; k = 5 (tham số
//     publicdata.k_min có phiên bản, người đặt ≠ người duyệt) → công bố ô 31135 (60 cuộn, 9,00 ha), ô 31120 vẫn ẩn;
//   - tenant MKG: bảng kê thu gom HTX + hub qua VAN (consumer thật của nền tảng, egress kênh valve ở tenant MKG);
//   - tenant đơn vị vận hành: khách hàng API, khóa, gói hạn mức, sổ sử dụng, bảng kê;
//   - tenant nhà máy (bên thứ ba): collection_volume + hub_capacity; tenant đơn vị thẩm định: mrv_package_status.
const (
	g5CommunePub  = "31135" // ≥ k sau khi k = 5
	g5CommuneHide = "31120" // < k — không bao giờ xuất hiện
	g5KMin        = "5"
	g5DailyUnits  = 5
)

// g5 là trạng thái kịch bản G5.
type g5 struct {
	t  *testing.T
	s  *harness.Stack
	pm *g5pump

	prog, mkg, operator, factory, verifier uuid.UUID
	root                                   *harness.User
	orgNational, orgP92                    string
	today, period                          string

	pub        *httptest.Server // cmd/api-public THẬT (publicasm.Build + router của module)
	laterAdmin *httptest.Server // Admin API pf/metering với đồng hồ "đầu tháng sau" (bảng kê kỳ đã kết thúc)

	coop               uuid.UUID
	plots              []g5plot
	hubA, hubB         uuid.UUID
	mrvExport          string
	mrvExporter        *harness.User
	collGrant, hubGrnt harness.ShareGrant
	mrvGrant           harness.ShareGrant

	apiAdmin, apiApprover                 *harness.User
	factoryClient                         string
	verifierClient                        string
	factoryKey, factoryKeyID, verifierKey string
	secrets                               []string // mọi bí mật khóa đã phát — không bao giờ được thấy trong log / DB

	// Mọi phản hồi Public API (RB-3 quét cuối kịch bản) và bộ đếm theo khách hàng.
	publicRaw [][]byte
	keyClient map[string]string
	calls     map[string]int // lời gọi đã xác thực (≠ 401) → một dòng usage_event
	dataReads map[string]int // GET dữ liệu / tệp 2xx → một egress_event public_api + một partner_read_log ở bên cấp

	exportJob   string
	exportFile  []byte
	statementID string
	anomalyEvts int
}

type g5plot struct {
	ref, hh, season uuid.UUID
	commune, month  string
}

// TestG5ThirdPartyToStatement là kịch bản chấp nhận G5 (docs/uat/g5-acceptance.md mục 2) — "từ bên thứ ba tới bảng
// kê": dữ liệu tenant chương trình + van MKG → đơn vị vận hành cấp bên thứ ba theo runbook public-api-onboarding
// (tenant nhà máy + đơn vị thẩm định, cấp quyền dp_* đề xuất ≠ duyệt, khách hàng + đăng ký + gói hạn mức R-56, khóa
// bí mật một lần) → cmd/api-public THẬT gọi từng sản phẩm (k-ẩn danh với publicdata.k_min ≠ mặc định, hub chỉ bậc,
// MRV chỉ trạng thái) → xuất 202 → tệp → sổ sử dụng + egress ở tenant chủ dữ liệu + sổ đọc partner_read_log (K-05)
// → hết hạn mức 429 → DLP enforce: xuất báo cáo / tải gói MRV vượt hạn mức kênh bị chặn, bất thường →
// pf.notify.egress_anomaly → xoay khóa (chồng lấn), thu hồi khóa 401, thu hồi cấp quyền 403 → cuối kỳ: bảng kê 202 +
// job, duyệt ≠ yêu cầu, CSV golden + kiểm băm → giao lặp mọi sự kiện không đổi gì → cách ly tenant + RB-3 (quét mọi
// phản hồi và tệp xuất) → verify_chain mọi sổ G5.
func TestG5ThirdPartyToStatement(t *testing.T) {
	g := &g5{t: t, prog: harness.NewTenant(), mkg: harness.NewTenant(), operator: harness.NewTenant(), factory: harness.NewTenant(),
		verifier: harness.NewTenant(), keyClient: map[string]string{}, calls: map[string]int{}, dataReads: map[string]int{}}
	// Cấu hình Phase K (ADR-0025 §1) cho cả api-platform lẫn api-public: pepper từ trình quản lý bí mật, tenant đơn vị
	// vận hành, DLP enforce. Đặt TRƯỚC harness.New (gốc lắp ráp đọc môi trường).
	t.Setenv(apiclientport.EnvPepperPrefix+"1", strings.Repeat("e2e-g5-pepper-", 4))
	t.Setenv(apiclientport.EnvPepperCurrent, "1")
	t.Setenv(apiclientport.EnvOperatorTenant, g.operator.String())
	t.Setenv(meter.EnvEgressMode, string(meter.ModeEnforce))
	g.s = harness.New(t)
	g.pm = newG5Pump(t, g.s, g.prog, g.mkg, g.operator, g.factory, g.verifier)
	now := time.Now().In(clock.Zone())
	g.today, g.period = clock.BusinessDate(now).String(), now.Format("2006-01")
	g.root = g.s.User(harness.UserSpec{Tenant: g.prog, MFA: true, Roles: []string{"platform_admin", "masterdata_admin", "registry_admin"}})
	g.startPublicAPI()
	g.startLaterAdmin()

	steps := []struct {
		name string
		fn   func()
	}{
		{"01_du_lieu_chuong_trinh_va_van_MKG", g.stepProgrammeData},
		{"02_tenant_ben_thu_ba_cap_quyen_dp", g.stepThirdPartyGrants},
		{"03_khach_hang_dang_ky_goi_han_muc_R56", g.stepClients},
		{"04_khoa_bi_mat_mot_lan", g.stepKeys},
		{"05_doc_san_pham_k_an_danh_bac_hub_trang_thai_MRV", g.stepReads},
		{"06_xuat_202_tep_Digest", g.stepExport},
		{"07_so_su_dung_egress_so_doc", g.stepLedgers},
		{"08_het_han_muc_429_goi_moi", g.stepQuota},
		{"09_DLP_enforce_bat_thuong_thong_bao", g.stepDLP},
		{"10_xoay_khoa_thu_hoi_401_403", g.stepRotateRevoke},
		{"11_bang_ke_cuoi_ky_R56_CSV_golden", g.stepStatement},
		{"12_R61_giao_lap_moi_su_kien", g.stepRedelivery},
		{"13_cach_ly_tenant_RB3", g.stepIsolationRB3},
		{"14_RB1_verify_chain_so_G5", g.stepLedgersChained},
	}
	for _, st := range steps {
		if !t.Run(st.name, func(t *testing.T) { g.t = t; st.fn() }) {
			t.Fatalf("bước %s hỏng — dừng kịch bản", st.name)
		}
	}
}

// ---------- dựng bề mặt ----------

// startPublicAPI dựng cmd/api-public bằng CHÍNH gốc lắp ráp của tiến trình (publicasm.Build → integrationPorts thật:
// pf/metering, party/port/grantlookup, nguồn pf/cropcycle + pf/geo + pf/mrv + pf/masterdata), cùng database và kho
// đối tượng với api-platform (tệp xuất do worker của nền tảng dựng).
func (g *g5) startPublicAPI() {
	t := g.t
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil || peppers == nil {
		t.Fatalf("pepper: %v", err)
	}
	opened := map[string]bool{}
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		opened[role] = true
		return g.s.PF(role), nil
	}
	mod, d, err := publicasm.Build(context.Background(), open, publicasm.Config{Operator: g.operator, Peppers: peppers, Store: g.s.Store,
		Replicas: 1, EgressMode: meter.ModeEnforce, Clock: clock.System{}, Log: telemetry.NewLogger("api-public", g.s.Logs)})
	if err != nil {
		t.Fatalf("api-public Build: %v", err)
	}
	if len(opened) != 1 || !opened[dbadmin.AppPublic] { // W2.6 (ADR-0028): một vai đăng nhập app_public
		t.Fatalf("api-public chỉ mở vai %s: %v", dbadmin.AppPublic, opened)
	}
	g.pub = httptest.NewServer(mod.PublicRouter(d))
	t.Cleanup(g.pub.Close)
}

// startLaterAdmin dựng Admin API của pf/metering với đồng hồ "đầu tháng sau + 2 giờ" (kỳ tháng hiện tại đã kết
// thúc — metering.period_not_ended với đồng hồ thật), cùng database, cùng gói hạn mức và tham số.
func (g *g5) startLaterAdmin() {
	now := time.Now().In(clock.Zone())
	next := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, clock.Zone()).AddDate(0, 1, 0)
	laterClk := clock.NewFixed(next.Add(2 * time.Hour))
	meter := metering.New(g.s.PF(metering.DBRole), laterClk, metering.Deps{Params: g.s.Mods.MasterData.Params(), Mode: meter.ModeEnforce})
	meter.UsePlans(g.s.Mods.APIClient.Plans())
	eng := httpx.NewEngine(telemetry.NewLogger("api-platform-later", g.s.Logs), 1<<20)
	meter.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(g.s.Issuer.Verifier()), httpx.RequireMFA(), idempotency.Middleware(g.s.PF("platform_api"))))
	g.laterAdmin = httptest.NewServer(eng)
	g.t.Cleanup(g.laterAdmin.Close)
}

// ---------- trợ giúp ----------

func (g *g5) want(r harness.Resp, status int, what string) harness.Resp {
	g.t.Helper()
	if r.Status != status {
		g.t.Fatalf("%s: muốn %d, nhận %d %s", what, status, r.Status, trim(r.Raw))
	}
	return r
}

func (g *g5) wantCode(r harness.Resp, status int, code, what string) harness.Resp {
	g.t.Helper()
	if r.Status != status || r.Code() != code {
		g.t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.Status, trim(r.Raw))
	}
	return r
}

// user phát token MFA (Admin API cần) cho một người của tenant.
func (g *g5) user(tid uuid.UUID, org string, roles ...string) *harness.User {
	spec := harness.UserSpec{Tenant: tid, MFA: true, Roles: roles}
	if org != "" {
		spec.Org = uuid.MustParse(org)
	}
	return g.s.User(spec)
}

// raw gọi một server bất kỳ (api-public, Admin "đầu tháng sau") với header tùy ý.
func (g *g5) raw(base, method, path string, hdr map[string]string, body any) harness.Resp {
	g.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		g.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	out := harness.Resp{Status: res.StatusCode, Header: res.Header, Raw: b}
	_ = json.Unmarshal(b, &out.Body)
	return out
}

// later gọi Admin API "đầu tháng sau" bằng token của u (POST luôn có Idempotency-Key, trừ khi idem cho sẵn).
func (g *g5) later(u *harness.User, method, path, idem string, body any) harness.Resp {
	g.t.Helper()
	h := map[string]string{"Authorization": "Bearer " + u.Token}
	if method == http.MethodPost {
		if idem == "" {
			idem = harness.NewIdem()
		}
		h["Idempotency-Key"] = idem
	}
	return g.raw(g.laterAdmin.URL, method, path, h, body)
}

// public gọi Public API bằng khóa (X-API-Key); ghi phản hồi để quét RB-3 và đếm lời gọi đã xác thực / lần đọc dữ liệu.
func (g *g5) public(key, method, path string, body any, extra ...string) harness.Resp {
	g.t.Helper()
	h := map[string]string{apiclientport.KeyHeader: key}
	for i := 0; i+1 < len(extra); i += 2 {
		h[extra[i]] = extra[i+1]
	}
	r := g.raw(g.pub.URL, method, path, h, body)
	g.publicRaw = append(g.publicRaw, r.Raw)
	if c, ok := g.keyClient[key]; ok && r.Status != http.StatusUnauthorized {
		g.calls[c]++
		if method == http.MethodGet && r.Status/100 == 2 && (strings.HasPrefix(path, "/v1/data/") || strings.HasSuffix(strings.Split(path, "?")[0], "/file")) {
			g.dataReads[c]++
		}
	}
	return r
}

// count đếm dòng trong tenant tid bằng vai quản trị migrate (chỉ kiểm tra — RLS áp theo tenant).
func (g *g5) count(tid uuid.UUID, q string, args ...any) int {
	g.t.Helper()
	var n int
	if err := tenant.WithTenantTx(context.Background(), g.s.PF(dbadmin.Migrator), tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), q, args...).Scan(&n)
	}); err != nil {
		g.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// param đặt một tham số có phiên bản ở pf/masterdata của tenant tid: param_setter đề xuất → gửi duyệt → tự duyệt bị
// chặn (R-56) → param_approver khác duyệt. Hiệu lực từ hôm nay (giờ VN).
func (g *g5) param(tid uuid.UUID, key, value, unit string) {
	g.t.Helper()
	setter := g.user(tid, "", "param_setter", "param_approver")
	approver := g.user(tid, "", "param_approver")
	v := g.want(setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions", "", map[string]any{
		"key": key, "value": value, "unit": unit, "effective_from": g.today, "source_doc": "QĐ thử nghiệm G5 số 05/2026 (tạm, chờ khách)",
	}), 201, "đề xuất "+key)
	id := str(v.Body, "id")
	g.want(setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/submit", "", nil), 200, "gửi duyệt "+key)
	g.wantCode(setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", "", map[string]any{}), 403,
		"self_approval_forbidden", "R-56: người đặt tự duyệt "+key)
	g.want(approver.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", "", map[string]any{}), 200, "duyệt "+key)
}

// valve giao một phong bì van của tenant MKG cho consumer THẬT của nền tảng (như outbox-publisher + RabbitMQ).
func (g *g5) valve(evType, producer string, payload map[string]any) outbox.Envelope {
	g.t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		g.t.Fatal(err)
	}
	now := time.Now().UTC()
	env := outbox.Envelope{EventID: ids.New(), EventType: evType, SchemaVersion: 1, TenantID: g.mkg, OccurredAt: now, RecordedAt: now,
		Producer: producer, Payload: b}
	if n := g.pm.deliver(env); n != 1 {
		g.t.Fatalf("van %s: muốn một consumer xử lý, nhận %d", evType, n)
	}
	return env
}

// ---------- các bước ----------

// 01 — tenant chương trình: dữ liệu chủ (đơn vị hành chính), cây tổ chức, danh mục MRV duyệt (R-56); thửa + vụ của 10
// nông hộ (pf/geo, pf/cropcycle — dữ kiện của G2); tenant MKG công bố qua VAN (cấp quyền valve_collection + valve_hub
// đề xuất ≠ duyệt): 10 bảng kê thu gom HTX, hai hub + sức chứa → consumer thật, egress kênh valve ở tenant MKG (bên
// nhận = bên cấp); một minh chứng AWD → xuất gói MRV 202 + job → worker → completed (egress mrv_package).
func (g *g5) stepProgrammeData() {
	t, s := g.t, g.s
	g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", "", map[string]any{}), 200, "seed dữ liệu chủ")
	org := func(body map[string]any) string {
		r := g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", body), 201, "tạo tổ chức "+fmt.Sprint(body["name"]))
		return str(obj(r.Body, "org"), "ref")
	}
	g.orgNational = org(map[string]any{"kind": "ttkn_national", "code": "TTKNQG", "name": "Trung tâm Khuyến nông Quốc gia"})
	g.orgP92 = org(map[string]any{"kind": "ttkn_province", "code": "TTKN92", "name": "TTKN TP Cần Thơ", "admin_unit_code": provCT, "parent_ref": g.orgNational})

	// Thửa + vụ (dữ kiện của pf/geo, pf/cropcycle — mỗi module một vai, tenant chương trình).
	ctx := context.Background()
	g.coop = ids.New()
	actor := ids.New()
	for i := range 10 {
		p := g5plot{ref: ids.New(), hh: ids.New(), season: ids.New(), commune: g5CommunePub, month: "2026-09-15"}
		if i >= 6 {
			p.commune, p.month = g5CommuneHide, "2026-08-15"
		}
		g.plots = append(g.plots, p)
	}
	exec := func(role string, f func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenant.WithTenantTx(ctx, s.PF(role), g.prog, f); err != nil {
			t.Fatalf("dữ kiện %s: %v", role, err)
		}
	}
	exec("geo_app", func(tx pgx.Tx) error {
		for i, p := range g.plots {
			if _, err := tx.Exec(ctx, `INSERT INTO pf_geo.plot (tenant_id, ref, code, commune_code, cooperative_ref, household_ref, status, current_version,
			    current_boundary_id, geom, area_ha, source, created_by)
			  VALUES ($1, $2, $3, $4, $5, $6, 'active', 1, $7, ST_GeomFromText('MULTIPOLYGON(((105.7 10.0,105.701 10.0,105.701 10.001,105.7 10.0)))', 4326),
			    1.5000::numeric, 'kiểm thử E2E G5', $8)`, g.prog, p.ref, fmt.Sprintf("E2E-G5-%02d", i), p.commune, g.coop, p.hh, ids.New(), actor); err != nil {
				return err
			}
		}
		return nil
	})
	exec("cropcycle_app", func(tx pgx.Tx) error {
		for i, p := range g.plots {
			if _, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.season_current (tenant_id, season_ref, plot_ref, plot_code, household_ref, cooperative_ref,
			    commune_code, season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no, sowing_date, expected_harvest_date,
			    harvest_open_date, area_ha, status, current_event_id, version, location_status, gps_source, on_behalf, opened_by, opened_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, 'DX', 2026, 'OM5451', $8, $9, 1, '2026-05-01'::date, '2026-08-20'::date, '2026-08-10'::date,
			    1.5000::numeric, 'open', $10, 1, 'verified', 'device', false, $11, now())`,
				g.prog, p.season, p.ref, fmt.Sprintf("E2E-G5-%02d", i), p.hh, g.coop, p.commune, ids.New(), ids.New(), ids.New(), actor); err != nil {
				return err
			}
		}
		return nil
	})

	// Van MKG (ADR-0020): tenant chương trình cấp valve_collection + valve_hub cho tenant MKG.
	s.GrantShare(g.prog, g.mkg, "Van công bố MKG → nền tảng (E2E G5)", partyport.KindValveCollection, partyport.KindValveHub)
	for _, p := range g.plots {
		g.valve(ccport.EventMKGHTXCollectionStatementPublished, "mkg/fieldops", map[string]any{"statement_ref": ids.New(), "htx_ref": g.coop,
			"plot_ref": p.ref, "season_ref": p.season, "collected_on": p.month, "rolls": 10, "revision": 1})
	}
	eff := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	g.hubA, g.hubB = ids.New(), ids.New()
	facility := func(hub uuid.UUID, code, kind string) map[string]any {
		return map[string]any{"hub_ref": hub.String(), "code": code, "name": "Kho MKG " + code, "kind": kind,
			"location":      map[string]any{"lat": json.Number("10.0312345"), "lng": json.Number("105.7654321")},
			"province_code": provCT, "status": "active", "capacity_tons": "1200.000000", "unit": "t", "effective_from": eff.Format(time.RFC3339)}
	}
	g.valve(geoport.EventMKGHubFacilityPublished, "mkg/hub", facility(g.hubA, "MKG-KHO-CT1", "warehouse"))
	g.valve(geoport.EventMKGHubFacilityPublished, "mkg/hub", facility(g.hubB, "MKG-HUB-CT2", "hub"))
	g.valve(geoport.EventMKGHubCapacityPublished, "mkg/hub", map[string]any{"hub_ref": g.hubA.String(), "as_of": eff.Add(time.Hour).Format(time.RFC3339),
		"capacity_tons": "1200.000000", "used_tons": "1100.000000", "available_tons": "100.000000", "unit": "t"})
	// DLP (L-05): mỗi phong bì × bên cấp một egress_event kênh valve ở tenant MKG (tenant phong bì), bên nhận = bên cấp.
	if n := g.count(g.mkg, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'valve' AND counterparty_ref = $1::uuid AND decision = 'allowed'`,
		g.prog); n != len(g.plots)+3 {
		t.Fatalf("egress valve ở tenant MKG: muốn %d, nhận %d", len(g.plots)+3, n)
	}

	// MRV: danh mục seed → gửi → người khác duyệt; một minh chứng AWD (không nông hộ → không cần đồng ý) → xuất gói.
	setter, approver := g.user(g.prog, "", "param_setter"), g.user(g.prog, "", "param_approver")
	seed := g.want(setter.Do(harness.Admin, http.MethodPost, "/v1/mrv/catalog/seed", "", nil), 200, "seed danh mục MRV")
	for _, it := range seed.Items() {
		v := obj(it, "version")
		if v == nil {
			continue
		}
		id := str(v, "id")
		g.want(setter.Do(harness.Admin, http.MethodPost, "/v1/mrv/catalog/versions/"+id+"/submit", "", nil), 200, "gửi duyệt danh mục MRV")
		g.want(approver.Do(harness.Admin, http.MethodPost, "/v1/mrv/catalog/versions/"+id+"/approve", "", map[string]any{"note": "Duyệt tạm G5"}), 200, "duyệt danh mục MRV")
	}
	col := g.user(g.prog, g.orgP92, mrvport.RoleCollector)
	g.want(col.Do(harness.App, http.MethodPost, "/v1/mrv/evidence", "", map[string]any{
		"evidence_type": mrvport.EvidenceAWDLog, "admin_unit_code": g5CommunePub, "plot_ref": g.plots[0].ref.String(), "season_code": "2026-DX",
		"values":     map[string]any{"stage": "de_nhanh", "awd_water_level_cm": "-12.5", "awd_dry_days": 5},
		"file_refs":  []any{map[string]any{"file_ref": ids.New().String(), "sha256": strings.Repeat("ab", 32)}},
		"gps":        map[string]any{"lat": "10.0300000", "lon": "105.7800000", "accuracy_m": "6.5"},
		"event_time": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}), 201, "minh chứng AWD")
	g.mrvExporter = g.user(g.prog, g.orgP92, mrvport.RoleExporter, mrvport.RoleViewer)
	r := g.want(g.mrvExporter.Do(harness.App, http.MethodPost, "/v1/mrv/exports", "", map[string]any{"filters": map[string]any{"evidence_type": "awd_log",
		"season_code": "2026-DX"}}), 202, "yêu cầu xuất gói MRV")
	g.mrvExport = r.Str("id")
	g.pm.run()
	if j := g.want(g.mrvExporter.Do(harness.App, http.MethodGet, "/v1/mrv/exports/"+g.mrvExport, "", nil), 200, "job MRV"); j.Str("status") != "completed" {
		t.Fatalf("gói MRV phải completed: %s", trim(j.Raw))
	}
	if n := g.count(g.prog, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'mrv_package' AND source_id = $1::uuid AND records = 1`,
		g.mrvExport); n != 1 {
		t.Fatalf("yêu cầu xuất gói MRV qua checkpoint kênh mrv_package: %d", n)
	}
}

// 02 — runbook bước 1–2: tenant nhà máy + tenant đơn vị thẩm định (tổ chức gốc loại partner ở pf/party); bên cấp đề
// xuất ≠ duyệt cấp quyền dp_collection_volume, dp_hub_capacity (nhà máy) và dp_mrv_package_status (thẩm định).
func (g *g5) stepThirdPartyGrants() {
	for tid, o := range map[uuid.UUID][2]string{g.factory: {"NM-SINHKHOI", "Nhà máy sinh khối (thử E2E)"}, g.verifier: {"VVB-THAMDINH", "Đơn vị thẩm định VVB (thử E2E)"}} {
		code, name := o[0], o[1]
		adm := g.user(tid, "", "platform_admin")
		r := g.want(adm.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{"kind": "partner", "code": code, "name": name}), 201, "tổ chức gốc "+name)
		if str(obj(r.Body, "org"), "kind") != "partner" {
			g.t.Fatalf("tổ chức gốc bên thứ ba phải loại partner: %s", trim(r.Raw))
		}
	}
	purpose := "HĐ dữ liệu số 05/2026 — "
	g.collGrant = g.s.GrantShare(g.prog, g.factory, purpose+"nhà máy lập kế hoạch thu mua rơm theo xã và vụ", partyport.KindDPCollectionVolume)
	g.hubGrnt = g.s.GrantShare(g.prog, g.factory, purpose+"nhà máy xem bậc sức chứa hub (Q5-6 tạm)", partyport.KindDPHubCapacity)
	g.mrvGrant = g.s.GrantShare(g.prog, g.verifier, purpose+"đơn vị thẩm định xem trạng thái gói MRV (KN US-MRV-05)", partyport.KindDPMRVPackageStatus)
}

// 03 — runbook bước 3 (tenant đơn vị vận hành): khách hàng API, đăng ký sản phẩm + scope, gói hạn mức có phiên bản; người
// đề xuất có cả vai duyệt tự duyệt → 403 apiclient.self_approval (R-56); người khác duyệt.
func (g *g5) stepClients() {
	g.apiAdmin = g.user(g.operator, "", apiclientport.RoleAPIClientAdmin, apiclientport.RoleAPIClientApprover)
	g.apiApprover = g.user(g.operator, "", apiclientport.RoleAPIClientApprover)
	adm := func(method, path string, body any) harness.Resp {
		return g.apiAdmin.Do(harness.Admin, method, path, "", body)
	}
	apr := func(method, path string, body any) harness.Resp {
		return g.apiApprover.Do(harness.Admin, method, path, "", body)
	}
	onboard := func(third uuid.UUID, name string, products map[string][]string, daily int64) string {
		r := g.want(adm(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": third.String(), "display_name": name,
			"purpose": "Theo HĐ dữ liệu số 05/2026", "contact_ref": ids.New().String()}), 201, "đề xuất khách hàng "+name)
		id := r.Str("id")
		g.wantCode(adm(http.MethodPost, "/v1/api-clients/"+id+"/approve", nil), 403, apiclientport.ErrSelfApproval.Code, "R-56: tự duyệt khách hàng")
		g.want(apr(http.MethodPost, "/v1/api-clients/"+id+"/approve", nil), 200, "duyệt khách hàng")
		for product, scopes := range products {
			r = g.want(adm(http.MethodPost, "/v1/api-clients/"+id+"/subscriptions", map[string]any{"product": product, "scopes": scopes}), 201, "đề xuất đăng ký "+product)
			sub := r.Str("id")
			g.wantCode(adm(http.MethodPost, "/v1/api-clients/"+id+"/subscriptions/"+sub+"/approve", nil), 403, apiclientport.ErrSelfApproval.Code, "R-56: tự duyệt đăng ký")
			g.want(apr(http.MethodPost, "/v1/api-clients/"+id+"/subscriptions/"+sub+"/approve", nil), 200, "duyệt đăng ký "+product)
		}
		r = g.want(adm(http.MethodPost, "/v1/api-clients/"+id+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": daily,
			"monthly_units": 1000, "max_export_rows": 1000}), 201, "đề xuất gói hạn mức")
		ver := fmt.Sprint(r.Body["version"])
		g.wantCode(adm(http.MethodPost, "/v1/api-clients/"+id+"/plans/"+ver+"/approve", nil), 403, apiclientport.ErrSelfApproval.Code, "R-56: tự duyệt gói")
		g.want(apr(http.MethodPost, "/v1/api-clients/"+id+"/plans/"+ver+"/approve", nil), 200, "duyệt gói hạn mức")
		return id
	}
	g.factoryClient = onboard(g.factory, "Nhà máy sinh khối (thử E2E)", map[string][]string{
		apiclientport.ProductCollectionVolume: {"dp:collection_volume:read", "dp:collection_volume:export", apiclientport.ScopeUsageRead},
		apiclientport.ProductHubCapacity:      {"dp:hub_capacity:read"},
	}, g5DailyUnits)
	g.verifierClient = onboard(g.verifier, "Đơn vị thẩm định VVB (thử E2E)", map[string][]string{
		apiclientport.ProductMRVPackageStatus: {"dp:mrv_package_status:read"},
	}, 100)
}

// issue phát hành khóa cho khách hàng: bí mật đúng dạng, một lần, Cache-Control: no-store; gọi lặp cùng Idempotency-Key
// trả lại khóa KHÔNG kèm bí mật.
func (g *g5) issue(client string) (secret, keyID string) {
	g.t.Helper()
	idem := harness.NewIdem()
	r := g.want(g.apiAdmin.Do(harness.Admin, http.MethodPost, "/v1/api-clients/"+client+"/keys", idem, nil), 201, "phát hành khóa")
	secret, keyID = r.Str("api_key"), r.Str("key_id")
	if _, ok := apiclientport.ParseKey(secret); !ok || r.Header.Get("Cache-Control") != "no-store" {
		g.t.Fatalf("bí mật khóa hiện một lần, đúng dạng, no-store: %v %s", r.Header, trim(r.Raw))
	}
	again := g.want(g.apiAdmin.Do(harness.Admin, http.MethodPost, "/v1/api-clients/"+client+"/keys", idem, nil), 201, "phát hành khóa gọi lặp")
	if again.Str("key_id") != keyID || again.Str("api_key") != "" || strings.Contains(string(again.Raw), secret) {
		t := g.t
		t.Fatalf("gọi lặp Idempotency-Key không trả lại bí mật: %s", trim(again.Raw))
	}
	g.secrets = append(g.secrets, secret)
	g.keyClient[secret] = client
	return secret, keyID
}

// 04 — runbook bước 4: khóa của hai khách hàng; khóa sai → 401 apiclient.unauthenticated, không có dòng sổ.
func (g *g5) stepKeys() {
	g.factoryKey, g.factoryKeyID = g.issue(g.factoryClient)
	g.verifierKey, _ = g.issue(g.verifierClient)
	bad := g.factoryKey[:len(g.factoryKey)-2] + "AA"
	if bad == g.factoryKey {
		bad = g.factoryKey[:len(g.factoryKey)-2] + "BB"
	}
	before := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event`)
	g.wantCode(g.public(bad, http.MethodGet, "/v1/data-products", nil), 401, apiclientport.ErrUnauthenticated.Code, "khóa sai")
	g.wantCode(g.public("", http.MethodGet, "/v1/data-products", nil), 401, apiclientport.ErrUnauthenticated.Code, "không có khóa")
	if after := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event`); after != before {
		g.t.Fatalf("lời gọi chưa xác thực không ghi sổ: %d → %d", before, after)
	}
}

// 05 — runbook bước 5 (thử chấp nhận): danh mục đúng sản phẩm + bên cấp; collection_volume với k mặc định 10 → mọi ô
// ẩn; publicdata.k_min = 5 (masterdata, người đặt ≠ người duyệt) → chỉ ô 31135 (≥ k), ô 31120 KHÔNG BAO GIỜ có;
// hub_capacity chỉ bậc sử dụng (không tấn tồn); mrv_package_status chỉ trạng thái + băm + kiểm lại; mỗi bên thứ ba
// gọi sản phẩm không đăng ký / không cấp quyền → 403 (vẫn một dòng sổ).
func (g *g5) stepReads() {
	t := g.t
	r := g.want(g.public(g.factoryKey, http.MethodGet, "/v1/data-products", nil), 200, "danh mục nhà máy")
	for _, w := range []string{`"collection_volume"`, `"hub_capacity"`, g.prog.String()} {
		if !strings.Contains(string(r.Raw), w) {
			t.Fatalf("danh mục phải có %s: %s", w, trim(r.Raw))
		}
	}
	if strings.Contains(string(r.Raw), `"mrv_package_status"`) && strings.Contains(string(r.Raw), `"subscribed":true,"product":"mrv_package_status"`) {
		t.Fatalf("nhà máy không đăng ký mrv_package_status: %s", trim(r.Raw))
	}

	path := "/v1/data/collection-volumes?season_code=2026-DX"
	r = g.want(g.public(g.factoryKey, http.MethodGet, path, nil), 200, "collection_volume k mặc định")
	if meta := obj(r.Body, "meta"); len(r.Items()) != 0 || num(meta, "k_applied") != 10 || num(meta, "suppressed_cells") != 2 {
		t.Fatalf("k mặc định 10 (C-3 tạm): mọi ô < k bị ẩn: %s", trim(r.Raw))
	}
	g.param(g.prog, apiclientport.ParamKMin, g5KMin, "hộ")
	r = g.want(g.public(g.factoryKey, http.MethodGet, path, nil), 200, "collection_volume k = 5")
	meta := obj(r.Body, "meta")
	items := r.Items()
	if len(items) != 1 || num(meta, "k_applied") != 5 || num(meta, "suppressed_cells") != 1 {
		t.Fatalf("k = 5: một ô công bố, một ô ẩn: %s", trim(r.Raw))
	}
	c := items[0]
	if str(c, "commune_code") != g5CommunePub || str(c, "province_code") != provCT || str(c, "period_month") != "2026-09" ||
		num(c, "rolls_collected") != 60 || str(c, "area_ha_collected") != "9.00" || num(c, "k_applied") != 5 {
		t.Fatalf("ô %s: %v", g5CommunePub, c)
	}
	if r.Header.Get(apiclientport.HeaderDataContract) != "collection_volume.v1" || r.Header.Get(apiclientport.HeaderRateLimitLimit) == "" ||
		r.Header.Get(apiclientport.HeaderRateLimitRemaining) == "" || r.Header.Get(apiclientport.HeaderQuotaRemaining) == "" ||
		r.Header.Get(apiclientport.HeaderRequestID) == "" {
		t.Fatalf("thiếu header hợp đồng / giới hạn: %v", r.Header)
	}

	r = g.want(g.public(g.factoryKey, http.MethodGet, "/v1/data/hub-capacities?province_code="+provCT, nil), 200, "hub_capacity")
	hubs := r.Items()
	if len(hubs) != 2 || r.Header.Get(apiclientport.HeaderDataContract) != "hub_capacity.v1" {
		t.Fatalf("hai hub đã công bố: %s", trim(r.Raw))
	}
	bands := map[string]string{}
	for _, h := range hubs {
		bands[str(h, "hub_ref")] = str(h, "utilization_band")
	}
	if b := bands[g.hubA.String()]; b != "warning" && b != "full" && b != "normal" {
		t.Fatalf("hub có số sức chứa → bậc sử dụng: %v", bands)
	}
	if bands[g.hubB.String()] != "unknown" {
		t.Fatalf("hub chưa có số → unknown: %v", bands)
	}
	for _, banned := range []string{"used_tons", "available_tons", "1100", "fill", "lot"} {
		if strings.Contains(string(r.Raw), banned) {
			t.Fatalf("hub_capacity không được có %q (chỉ bậc): %s", banned, trim(r.Raw))
		}
	}

	r = g.want(g.public(g.verifierKey, http.MethodGet, "/v1/data/mrv-packages", nil), 200, "mrv_package_status danh sách")
	pk := r.Items()
	if len(pk) != 1 || str(pk[0], "export_id") != g.mrvExport || str(pk[0], "status") != "completed" || num(pk[0], "evidence_count") != 1 ||
		len(str(pk[0], "package_sha256")) != 64 || pk[0]["last_verify_match"] != nil {
		t.Fatalf("danh sách gói MRV: %s", trim(r.Raw))
	}
	r = g.want(g.public(g.verifierKey, http.MethodGet, "/v1/data/mrv-packages/"+g.mrvExport, nil), 200, "mrv_package_status một gói")
	if r.Body["last_verify_match"] != true || str(r.Body, "export_id") != g.mrvExport {
		t.Fatalf("một gói + kiểm lại băm: %s", trim(r.Raw))
	}

	// Sản phẩm không đăng ký / không cấp quyền → 403 (một dòng sổ, 0 đơn vị).
	g.wantCode(g.public(g.verifierKey, http.MethodGet, "/v1/data/collection-volumes", nil), 403, apiclientport.ErrForbidden.Code, "thẩm định đọc collection_volume")
	g.wantCode(g.public(g.verifierKey, http.MethodGet, "/v1/usage?from="+g.today+"&to="+g.today, nil), 403, apiclientport.ErrForbidden.Code, "thẩm định không có usage:read")
	g.wantCode(g.public(g.factoryKey, http.MethodGet, "/v1/data/mrv-packages", nil), 403, apiclientport.ErrForbidden.Code, "nhà máy đọc mrv_package_status")
	// grantor chỉ CHỌN bên cấp — tenant không cấp quyền → 403, không bao giờ đổi tenant bên gọi (P-07).
	g.wantCode(g.public(g.factoryKey, http.MethodGet, "/v1/data/collection-volumes?grantor="+g.mkg.String(), nil), 403, apiclientport.ErrForbidden.Code,
		"grantor không cấp quyền")
}

// 06 — xuất 202 + Location; gọi lặp Idempotency-Key → đúng byte phản hồi đầu (0 đơn vị); worker nền tảng (consumer
// pf.apiclient.export qua bơm outbox) dựng tệp tất định; tải qua API, Digest khớp; ô dưới k không có trong tệp.
func (g *g5) stepExport() {
	t := g.t
	body := map[string]any{"format": "csv", "season_code": "2026-DX"}
	g.wantCode(g.public(g.factoryKey, http.MethodPost, "/v1/data/collection-volumes/exports", body), 400, "idempotency_key_required", "xuất thiếu Idempotency-Key")
	r := g.want(g.public(g.factoryKey, http.MethodPost, "/v1/data/collection-volumes/exports", body, "Idempotency-Key", "e2e-g5-export-0001"), 202, "yêu cầu xuất")
	g.exportJob = r.Str("id")
	if r.Header.Get("Location") != "/v1/jobs/"+g.exportJob {
		t.Fatalf("202 + Location: %v", r.Header)
	}
	again := g.want(g.public(g.factoryKey, http.MethodPost, "/v1/data/collection-volumes/exports", body, "Idempotency-Key", "e2e-g5-export-0001"), 202, "xuất gọi lặp")
	if !bytes.Equal(again.Raw, r.Raw) || again.Header.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("gọi lặp → đúng phản hồi đầu: %s vs %s", trim(again.Raw), trim(r.Raw))
	}
	if n := g.pm.run(); n < 1 {
		t.Fatalf("worker xuất phải xử lý pf.apiclient.export.requested")
	}
	j := g.want(g.public(g.factoryKey, http.MethodGet, "/v1/jobs/"+g.exportJob, nil), 200, "trạng thái job")
	if j.Str("status") != "completed" || num(j.Body, "rows") != 1 {
		t.Fatalf("job completed, 1 dòng (ô dưới k bị ẩn cả trong tệp): %s", trim(j.Raw))
	}
	f := g.want(g.public(g.factoryKey, http.MethodGet, "/v1/jobs/"+g.exportJob+"/file", nil), 200, "tải tệp")
	sum := sha256.Sum256(f.Raw)
	if f.Header.Get("Digest") != "sha-256="+base64.StdEncoding.EncodeToString(sum[:]) || j.Str("sha256") == "" ||
		!strings.Contains(string(f.Raw), g5CommunePub) || strings.Contains(string(f.Raw), g5CommuneHide) {
		t.Fatalf("tệp xuất (Digest, ngưỡng k): %v %q", f.Header, f.Raw)
	}
	g.exportFile = f.Raw
	// Tệp nằm trong kho đối tượng trong lãnh thổ, không có URL có chữ ký trong phản hồi (R-52).
	if strings.Contains(string(j.Raw), "X-Amz") || strings.Contains(string(j.Raw), "signature") {
		t.Fatalf("không URL có chữ ký: %s", trim(j.Raw))
	}
}

// 07 — sổ (ADR-0023/0024, K-05): mỗi lời gọi đã xác thực một dòng usage_event ở tenant ĐƠN VỊ VẬN HÀNH (kể cả 403);
// mỗi lần đọc / tải dữ liệu 2xx một egress_event kênh public_api ở tenant CHỦ DỮ LIỆU và một dòng partner_read_log ở
// tenant bên cấp (người đọc = mã khách hàng API); gọi lặp xuất → 0 đơn vị; /v1/usage chỉ của chính khách hàng.
func (g *g5) stepLedgers() {
	t := g.t
	for _, c := range []string{g.factoryClient, g.verifierClient} {
		if n := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid`, c); n != g.calls[c] {
			t.Fatalf("usage_event của %s: muốn %d (mọi lời gọi đã xác thực), nhận %d", c, g.calls[c], n)
		}
		if n := g.count(g.prog, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'public_api' AND counterparty_kind = 'api_client'
		    AND counterparty_ref = $1::uuid`, c); n != g.dataReads[c] {
			t.Fatalf("egress public_api của %s ở tenant chủ dữ liệu: muốn %d, nhận %d", c, g.dataReads[c], n)
		}
		if n := g.count(g.prog, `SELECT count(*) FROM pf_party.partner_read_log WHERE actor_ref = $1::uuid`, c); n != g.dataReads[c] {
			t.Fatalf("partner_read_log (K-05) của %s ở tenant bên cấp: muốn %d, nhận %d", c, g.dataReads[c], n)
		}
	}
	if n := g.count(g.prog, `SELECT count(*) FROM pf_party.partner_read_log WHERE actor_ref = $1::uuid AND grantee_tenant_id = $2::uuid
	    AND kind = 'dp_mrv_package_status' AND grant_id = $3::uuid`, g.verifierClient, g.verifier, g.mrvGrant.ID); n != 2 {
		t.Fatalf("KN US-MRV-05: mọi lần đơn vị thẩm định truy cập có nhật ký ở bên cấp: %d", n)
	}
	if n := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND status = 202 AND replayed AND units = 0`,
		g.factoryClient); n != 1 {
		t.Fatalf("gọi lặp xuất → 0 đơn vị, replayed: %d", n)
	}
	if n := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND status = 403 AND units = 0`,
		g.verifierClient); n != 2 {
		t.Fatalf("403 vẫn một dòng sổ, 0 đơn vị: %d", n)
	}
	r := g.want(g.public(g.factoryKey, http.MethodGet, "/v1/usage?from="+g.today+"&to="+g.today, nil), 200, "usage của nhà máy")
	// Chỉ của chính khách hàng: tổng lời gọi = số lời gọi đã xác thực của nhà máy trước lời gọi /v1/usage này
	// (lần 403 của chính nhà máy vào mrv_package_status có mặt, 0 đơn vị; lời gọi của đơn vị thẩm định thì không).
	calls := 0
	for _, it := range r.Items() {
		calls += int(num(it, "calls"))
		if str(it, "product") == apiclientport.ProductMRVPackageStatus && num(it, "units") != 0 {
			t.Fatalf("lần 403 không tính đơn vị: %s", trim(r.Raw))
		}
	}
	if calls != g.calls[g.factoryClient]-1 || !strings.Contains(string(r.Raw), `"product":"collection_volume"`) {
		t.Fatalf("/v1/usage chỉ của chính khách hàng (%d lời gọi): %s", g.calls[g.factoryClient]-1, trim(r.Raw))
	}
	// metering_viewer của bên cấp xem nhật ký xuất của tenant mình (Admin API).
	v := g.user(g.prog, "", meteringport.RoleMeteringViewer)
	e := g.want(v.Do(harness.Admin, http.MethodGet, "/v1/metering/egress?channel=public_api", "", nil), 200, "nhật ký xuất bên cấp")
	if len(e.Items()) != g.dataReads[g.factoryClient]+g.dataReads[g.verifierClient] {
		t.Fatalf("nhật ký xuất public_api của bên cấp: %d dòng, muốn %d", len(e.Items()), g.dataReads[g.factoryClient]+g.dataReads[g.verifierClient])
	}
}

// 08 — hạn mức ngày (gói v1 daily_units = 5) hết → 429 metering.quota_exceeded + Retry-After, dòng sổ status 429 units
// 0, không dữ liệu; gói v2 (đề xuất ≠ duyệt, có phiên bản) nâng hạn mức → đọc lại được.
func (g *g5) stepQuota() {
	t := g.t
	used := g.count(g.operator, `SELECT coalesce(sum(units), 0)::int FROM pf_metering.usage_event WHERE client_id = $1::uuid`, g.factoryClient)
	for i := used; i < g5DailyUnits; i++ {
		g.want(g.public(g.factoryKey, http.MethodGet, "/v1/data/collection-volumes", nil), 200, "đọc trong hạn mức")
	}
	r := g.public(g.factoryKey, http.MethodGet, "/v1/data/collection-volumes", nil)
	if r.Status != http.StatusTooManyRequests || r.Code() != meter.ErrQuotaExceeded.Code || r.Header.Get("Retry-After") == "" ||
		strings.Contains(string(r.Raw), g5CommunePub) {
		t.Fatalf("hết hạn mức ngày → 429 metering.quota_exceeded + Retry-After, không dữ liệu: %d %v %s", r.Status, r.Header, trim(r.Raw))
	}
	if n := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND status = 429 AND units = 0
	    AND problem_code = 'metering.quota_exceeded'`, g.factoryClient); n != 1 {
		t.Fatalf("dòng sổ 429, 0 đơn vị: %d", n)
	}
	r = g.want(g.apiAdmin.Do(harness.Admin, http.MethodPost, "/v1/api-clients/"+g.factoryClient+"/plans", "", map[string]any{"requests_per_minute": 600,
		"daily_units": 1000, "monthly_units": 5000, "max_export_rows": 1000}), 201, "đề xuất gói v2")
	if fmt.Sprint(r.Body["version"]) != "2" {
		t.Fatalf("gói hạn mức có phiên bản: %s", trim(r.Raw))
	}
	g.want(g.apiApprover.Do(harness.Admin, http.MethodPost, "/v1/api-clients/"+g.factoryClient+"/plans/2/approve", "", nil), 200, "duyệt gói v2")
	g.want(g.public(g.factoryKey, http.MethodGet, "/v1/data/collection-volumes", nil), 200, "đọc sau khi nâng gói")
}

// 09 — DLP enforce (ADR-0024): tham số kênh có phiên bản ở tenant chương trình (người đặt ≠ người duyệt):
// report_export daily_records_quota = 1, anomaly_records = 1; mrv_package daily_records_quota = 1. Xuất báo cáo lần 2
// → 429 metering.egress_quota_exceeded (dòng egress denied, không export_audit) + bất thường lần đầu trong ngày → đúng
// một pf.metering.egress.anomaly_detected → consumer pf.notify.egress_anomaly → metering_viewer + platform_admin của
// tenant chủ dữ liệu nhận cảnh báo khẩn; cấp URL tải gói MRV vượt hạn mức kênh → 429, không cấp URL.
func (g *g5) stepDLP() {
	t := g.t
	g.param(g.prog, meteringport.EgressParam(meteringport.ParamEgressDailyQuotaFmt, meter.ChannelReportExport), "1", "bản ghi")
	g.param(g.prog, meteringport.EgressParam(meteringport.ParamEgressAnomalyRecordsFmt, meter.ChannelReportExport), "1", "bản ghi")
	g.param(g.prog, meteringport.EgressParam(meteringport.ParamEgressDailyQuotaFmt, meter.ChannelMRVPackage), "1", "bằng chứng")
	kn := g.user(g.prog, g.orgP92, partyport.RoleKNProvinceAdmin)
	body := map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}
	g.want(kn.Do(harness.App, http.MethodPost, "/v1/reports/exports", "", body), 202, "xuất báo cáo lần 1 (trong hạn mức kênh)")
	g.wantCode(kn.Do(harness.App, http.MethodPost, "/v1/reports/exports", "", body), 429, meter.ErrEgressQuotaExceeded.Code, "xuất báo cáo lần 2 vượt hạn mức kênh")
	if n := g.count(g.prog, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'report_export' AND counterparty_ref = $1::uuid AND decision = 'denied'
	    AND mode = 'enforce'`, kn.Actor); n != 1 {
		t.Fatalf("egress report_export denied: %d", n)
	}
	if n := g.count(g.prog, `SELECT count(*) FROM pf_reports.export_audit WHERE kind = 'export'`); n != 1 {
		t.Fatalf("bị từ chối → không có dòng export_audit: %d", n)
	}
	g.wantCode(g.mrvExporter.Do(harness.App, http.MethodGet, "/v1/mrv/exports/"+g.mrvExport+"/package", "", nil), 429, meter.ErrEgressQuotaExceeded.Code,
		"tải gói MRV vượt hạn mức kênh")
	if n := g.count(g.prog, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'mrv_package' AND decision = 'denied'`); n != 1 {
		t.Fatalf("egress mrv_package denied: %d", n)
	}
	// Bất thường: đúng một dòng egress_anomaly + đúng một sự kiện cho (report_export × người xuất × ngày).
	if n := g.count(g.prog, `SELECT count(*) FROM pf_metering.egress_anomaly WHERE channel = 'report_export'`); n != 1 {
		t.Fatalf("một bất thường report_export: %d", n)
	}
	g.wantCode(kn.Do(harness.App, http.MethodPost, "/v1/reports/exports", "", body), 429, meter.ErrEgressQuotaExceeded.Code, "xuất báo cáo lần 3")
	g.anomalyEvts = g.count(g.prog, `SELECT count(*) FROM shared.outbox WHERE event_type = $1`, meteringport.EventEgressAnomaly)
	if g.anomalyEvts != 1 {
		t.Fatalf("lần đầu vượt ngưỡng → đúng một pf.metering.egress.anomaly_detected: %d", g.anomalyEvts)
	}
	if n := g.pm.run(); n < 1 || g.pm.byConsumer[notifyport.ConsumerEgressAnomaly] != 1 {
		t.Fatalf("consumer pf.notify.egress_anomaly: %v", g.pm.byConsumer)
	}
	for name, u := range map[string]*harness.User{
		"metering_viewer": g.user(g.prog, g.orgNational, meteringport.RoleMeteringViewer),
		"platform_admin":  g.user(g.prog, g.orgNational, "platform_admin"),
	} {
		in := g.want(u.Do(harness.App, http.MethodGet, "/v1/notifications", "", nil), 200, "hộp thư "+name)
		var hit []map[string]any
		for _, it := range in.Items() {
			if str(it, "kind") == notifyport.KindEgressAnomaly {
				hit = append(hit, it)
			}
		}
		if len(hit) != 1 || str(hit[0], "severity") != notifyport.SeverityUrgent || strings.Contains(fmt.Sprint(hit[0]), kn.Actor.String()) {
			t.Fatalf("%s nhận đúng một cảnh báo DLP khẩn, không có mã người xuất: %v", name, hit)
		}
	}
	other := g.user(g.operator, "", meteringport.RoleMeteringViewer, "platform_admin")
	for _, it := range g.want(other.Do(harness.App, http.MethodGet, "/v1/notifications", "", nil), 200, "hộp thư tenant vận hành").Items() {
		if str(it, "kind") == notifyport.KindEgressAnomaly {
			t.Fatal("tenant khác không nhận cảnh báo DLP của tenant chủ dữ liệu")
		}
	}
	v := g.user(g.prog, "", meteringport.RoleMeteringViewer)
	a := g.want(v.Do(harness.Admin, http.MethodGet, "/v1/metering/egress/anomalies", "", nil), 200, "bất thường bên cấp")
	if len(a.Items()) != 1 {
		t.Fatalf("GET /v1/metering/egress/anomalies: %s", trim(a.Raw))
	}
}

// 10 — runbook mục 7–8: xoay khóa → khóa mới (bí mật một lần), khóa cũ còn dùng trong hạn chồng lấn; thu hồi khóa cũ
// (có lý do) → 401 ngay, khóa mới vẫn dùng; thu hồi cấp quyền dp_hub_capacity ở bên cấp → 403 ngay ở lời gọi kế
// tiếp, collection_volume (cấp quyền khác) vẫn 200.
func (g *g5) stepRotateRevoke() {
	t := g.t
	idem := harness.NewIdem()
	r := g.want(g.apiAdmin.Do(harness.Admin, http.MethodPost, "/v1/api-clients/"+g.factoryClient+"/keys/"+g.factoryKeyID+"/rotate", idem, nil), 201, "xoay khóa")
	newKey := r.Str("api_key")
	if _, ok := apiclientport.ParseKey(newKey); !ok || r.Str("previous_key_grace_until") == "" || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("xoay: bí mật mới một lần + hạn chồng lấn: %s", trim(r.Raw))
	}
	g.secrets = append(g.secrets, newKey)
	g.keyClient[newKey] = g.factoryClient
	g.want(g.public(g.factoryKey, http.MethodGet, "/v1/data-products", nil), 200, "khóa cũ trong hạn chồng lấn")
	g.want(g.public(newKey, http.MethodGet, "/v1/data-products", nil), 200, "khóa mới")
	g.wantCode(g.apiAdmin.Do(harness.Admin, http.MethodPost, "/v1/api-clients/"+g.factoryClient+"/keys/"+g.factoryKeyID+"/revoke", "", map[string]any{}),
		422, "validation_failed", "thu hồi thiếu lý do")
	g.want(g.apiAdmin.Do(harness.Admin, http.MethodPost, "/v1/api-clients/"+g.factoryClient+"/keys/"+g.factoryKeyID+"/revoke", "",
		map[string]any{"reason": "Nghi lộ khóa cũ sau khi xoay (E2E G5)"}), 200, "thu hồi khóa cũ")
	before := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event`)
	g.wantCode(g.public(g.factoryKey, http.MethodGet, "/v1/data-products", nil), 401, apiclientport.ErrUnauthenticated.Code, "khóa đã thu hồi")
	if after := g.count(g.operator, `SELECT count(*) FROM pf_metering.usage_event`); after != before {
		t.Fatalf("khóa thu hồi → 401, không dòng sổ: %d → %d", before, after)
	}
	g.factoryKey = newKey
	g.want(g.public(g.factoryKey, http.MethodGet, "/v1/data/hub-capacities", nil), 200, "hub trước khi thu hồi cấp quyền")
	rv := g.hubGrnt.Proposer.Do(harness.Admin, http.MethodPost, "/v1/share-grants/"+g.hubGrnt.ID+"/revoke", "",
		map[string]any{"reason": "Kết thúc thử hub_capacity (Q5-6 chưa chốt)"})
	if rv.Status != http.StatusCreated && rv.Status != http.StatusOK {
		t.Fatalf("thu hồi cấp quyền: %d %s", rv.Status, trim(rv.Raw))
	}
	g.wantCode(g.public(g.factoryKey, http.MethodGet, "/v1/data/hub-capacities", nil), 403, apiclientport.ErrForbidden.Code, "sau thu hồi cấp quyền")
	g.want(g.public(g.factoryKey, http.MethodGet, "/v1/data/collection-volumes", nil), 200, "cấp quyền khác vẫn hiệu lực")
	if n := g.count(g.operator, `SELECT count(*) FROM pf_apiclient.api_key_event WHERE client_id = $1::uuid`, g.factoryClient); n < 3 {
		t.Fatalf("vòng đời khóa là sổ (issued, rotated, issued, revoked): %d", n)
	}
}

// 11 — cuối kỳ (ADR-0023 mục 4, runbook mục 6): với đồng hồ thật kỳ chưa hết → 409 metering.period_not_ended; "đầu tháng
// sau" billing_officer yêu cầu → 202 + job (gọi lặp → cùng job); worker dựng ảnh chụp bất biến → draft; người yêu cầu
// có cả vai duyệt tự duyệt → 403 metering.self_approval; billing_approver khác duyệt → approved, duyệt lại → 409; kiểm
// lại băm khớp sổ; CSV tất định = golden (sau khi chuẩn hóa mã/băm), tải qua checkpoint report_export.
func (g *g5) stepStatement() {
	t := g.t
	officer := g.user(g.operator, "", meteringport.RoleBillingOfficer, meteringport.RoleBillingApprover)
	approver := g.user(g.operator, "", meteringport.RoleBillingApprover)
	req := map[string]any{"period": g.period, "client_id": g.factoryClient}
	g.wantCode(officer.Do(harness.Admin, http.MethodPost, "/v1/metering/statements", "", req), 409, "metering.period_not_ended", "kỳ chưa hết (đồng hồ thật)")
	idem := harness.NewIdem()
	r := g.want(g.later(officer, http.MethodPost, "/v1/metering/statements", idem, req), 202, "yêu cầu bảng kê")
	if !strings.HasPrefix(r.Header.Get("Location"), "/v1/metering/statement-jobs/") {
		t.Fatalf("202 + Location: %v", r.Header)
	}
	if again := g.later(officer, http.MethodPost, "/v1/metering/statements", idem, req); again.Str("id") != r.Str("id") {
		t.Fatalf("gọi lặp → cùng job: %s", trim(again.Raw))
	}
	g.pm.run()
	job := g.want(g.later(officer, http.MethodGet, "/v1/metering/statement-jobs/"+r.Str("id"), "", nil), 200, "job bảng kê")
	sids, _ := job.Body["statement_ids"].([]any)
	if job.Str("status") != "completed" || len(sids) != 1 {
		t.Fatalf("job bảng kê: %s", trim(job.Raw))
	}
	g.statementID = sids[0].(string)
	st := g.want(g.later(officer, http.MethodGet, "/v1/metering/statements/"+g.statementID, "", nil), 200, "bảng kê")
	if st.Str("status") != "draft" || len(st.Str("snapshot_sha256")) != 64 || len(st.Str("chain_head_hash")) != 64 || st.Str("client_id") != g.factoryClient {
		t.Fatalf("bảng kê draft: %s", trim(st.Raw))
	}
	g.wantCode(g.later(officer, http.MethodPost, "/v1/metering/statements/"+g.statementID+"/approve", "", nil), 403, meteringport.ErrSelfApproval.Code,
		"R-56: người yêu cầu tự duyệt bảng kê")
	g.want(g.later(approver, http.MethodPost, "/v1/metering/statements/"+g.statementID+"/approve", "", nil), 201, "billing_approver duyệt")
	g.want(g.later(approver, http.MethodPost, "/v1/metering/statements/"+g.statementID+"/approve", "", nil), 200, "duyệt lặp của chính người duyệt")
	g.wantCode(g.later(g.user(g.operator, "", meteringport.RoleBillingApprover), http.MethodPost, "/v1/metering/statements/"+g.statementID+"/approve", "", nil),
		409, "metering.statement_frozen", "người khác duyệt lại bảng kê đã duyệt")
	v := g.want(g.later(approver, http.MethodGet, "/v1/metering/statements/"+g.statementID+"/verify", "", nil), 200, "kiểm băm")
	if v.Body["match"] != true || v.Str("stored_sha256") != st.Str("snapshot_sha256") {
		t.Fatalf("kiểm lại băm ảnh chụp: %s", trim(v.Raw))
	}
	csv1 := g.want(g.later(approver, http.MethodGet, "/v1/metering/statements/"+g.statementID+"/export.csv", "", nil), 200, "tải CSV")
	csv2 := g.want(g.later(approver, http.MethodGet, "/v1/metering/statements/"+g.statementID+"/export.csv", "", nil), 200, "tải CSV lần 2")
	if !bytes.Equal(csv1.Raw, csv2.Raw) || csv1.Header.Get("AG-Snapshot-SHA256") != st.Str("snapshot_sha256") {
		t.Fatal("CSV bảng kê tất định byte-cho-byte")
	}
	// Chuẩn hóa phần thay đổi giữa các lần chạy (mã, tenant, kỳ, băm) rồi so golden.
	norm := string(csv1.Raw)
	for from, to := range map[string]string{g.statementID: "<statement_id>", g.factoryClient: "<client_id>", g.factory.String(): "<client_tenant_id>",
		st.Str("snapshot_sha256"): "<snapshot_sha256>", st.Str("chain_head_hash"): "<chain_head_hash>", g.period: "<period>"} {
		norm = strings.ReplaceAll(norm, from, to)
	}
	golden := filepath.Join("testdata", "g5_statement.golden.csv")
	if *g5UpdateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(norm), 0o644); err != nil { //nolint:gosec // tệp golden của kiểm thử
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden) //nolint:gosec // tệp golden của kiểm thử
	if err != nil {
		t.Fatalf("golden %s (chạy -g5-update để tạo): %v", golden, err)
	}
	if norm != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("CSV bảng kê lệch golden:\n%s", norm)
	}
	if n := g.count(g.operator, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'report_export' AND counterparty_ref = $1::uuid`,
		approver.Actor); n != 2 {
		t.Fatalf("tải CSV qua checkpoint report_export (tenant đơn vị vận hành): %d", n)
	}
	// Sửa sai = bảng kê BÙ (supersedes_id + lý do, RB-1): bảng kê cũ giữ nguyên, CSV cũ không đổi.
	r = g.want(g.later(officer, http.MethodPost, "/v1/metering/statements", "", map[string]any{"period": g.period, "client_id": g.factoryClient,
		"supersedes_id": g.statementID, "correction_reason": "Bảng kê bù thử nghiệm E2E G5"}), 202, "yêu cầu bảng kê bù")
	g.pm.run()
	job = g.want(g.later(officer, http.MethodGet, "/v1/metering/statement-jobs/"+r.Str("id"), "", nil), 200, "job bảng kê bù")
	if sids, _ := job.Body["statement_ids"].([]any); job.Str("status") != "completed" || len(sids) != 1 || sids[0] == g.statementID {
		t.Fatalf("bảng kê bù: %s", trim(job.Raw))
	}
	old := g.want(g.later(approver, http.MethodGet, "/v1/metering/statements/"+g.statementID+"/export.csv", "", nil), 200, "CSV bảng kê cũ")
	if !bytes.Equal(old.Raw, csv1.Raw) {
		t.Fatal("bảng kê cũ giữ nguyên sau khi có bảng kê bù")
	}
}

// 12 — R-61: giao lại MỌI sự kiện đã giao (van MKG, xuất MRV, xuất Public API, bảng kê, cảnh báo DLP) → inbox bỏ qua;
// mọi bảng G5 không đổi.
func (g *g5) stepRedelivery() {
	snap := g.snapshot()
	g.pm.redeliverAll()
	if after := g.snapshot(); fmt.Sprint(after) != fmt.Sprint(snap) {
		g.t.Fatalf("giao lặp đổi dữ liệu:\ntrước %v\nsau   %v", snap, after)
	}
	if len(g.pm.delivered) < len(g.plots)+3+4 {
		g.t.Fatalf("kịch bản phải giao đủ sự kiện: %d", len(g.pm.delivered))
	}
}

// snapshot đếm dòng các bảng G5 (và bảng nhận van) của mọi tenant kịch bản.
func (g *g5) snapshot() map[string]int {
	out := map[string]int{}
	for _, tb := range []string{"pf_metering.usage_event", "pf_metering.egress_event", "pf_metering.egress_anomaly", "pf_metering.statement",
		"pf_metering.statement_job", "pf_apiclient.export_job", "pf_apiclient.export_file", "pf_party.partner_read_log", "pf_cropcycle.collection_statement_view",
		"pf_geo.hub_view", "pf_mrv.export_job", "pf_notify.notification", "shared.outbox"} {
		for _, tid := range []uuid.UUID{g.prog, g.mkg, g.operator, g.factory, g.verifier} {
			out[tb+"@"+tid.String()[:8]] = g.count(tid, "SELECT count(*) FROM "+tb)
		}
	}
	return out
}

var (
	g5Phone = regexp.MustCompile(`(^|[^0-9A-Za-z-])0[0-9]{9}([^0-9A-Za-z-]|$)`) // không khớp chuỗi số trong UUID / băm
	g5Email = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
)

// 13 — cách ly tenant + RB-3: (a) mọi phản hồi Public API và tệp xuất không có dữ liệu cá nhân, mã thửa / nông hộ / HTX,
// mã người dùng, khóa đối tượng, tham chiếu kho định danh, trường ngoài hợp đồng (mỗi phần tử qua Validate mặc định từ
// chối); (b) sổ sử dụng chỉ ở tenant đơn vị vận hành, egress ở tenant chủ dữ liệu, sổ đọc ở bên cấp; metering_viewer
// mỗi tenant chỉ thấy nhật ký của mình; (c) bí mật khóa không có trong log hay bất kỳ bảng nào.
func (g *g5) stepIsolationRB3() {
	t := g.t
	banned := []string{g.coop.String(), g.mrvExporter.Actor.String(), g.mkg.String(), "household", "plot_ref", "cooperative", "htx_ref", "actor",
		"vault", "subject_ref", "person_ref", "packages/", "object_key", "requested_by", "gps", "full_name", "phone"}
	for _, p := range g.plots {
		banned = append(banned, p.ref.String(), p.hh.String(), p.season.String())
	}
	allowedTop := []string{"items", "next_cursor", "meta"}
	for i, raw := range append(append([][]byte(nil), g.publicRaw...), g.exportFile) {
		s := string(raw)
		for _, b := range banned {
			if strings.Contains(s, b) {
				t.Fatalf("RB-3: phản hồi Public API #%d chứa %q: %s", i, b, trim(raw))
			}
		}
		if g5Phone.MatchString(s) || g5Email.MatchString(s) || strings.Contains(s, g5CommuneHide) {
			t.Fatalf("RB-3: phản hồi #%d có dạng SĐT / email / ô dưới k: %s", i, trim(raw))
		}
	}
	// Trường ngoài hợp đồng: đọc lại mỗi sản phẩm, kiểm từng phần tử + khóa mức gốc.
	for _, c := range []struct{ key, path, product string }{
		{g.factoryKey, "/v1/data/collection-volumes", apiclientport.ProductCollectionVolume},
		{g.verifierKey, "/v1/data/mrv-packages", apiclientport.ProductMRVPackageStatus},
	} {
		r := g.want(g.public(c.key, http.MethodGet, c.path, nil), 200, "đọc lại "+c.product)
		p, err := dataproduct.Get(c.product)
		if err != nil {
			t.Fatal(err)
		}
		for k := range r.Body {
			if !slices.Contains(allowedTop, k) {
				t.Fatalf("%s: khóa gốc ngoài hợp đồng %q", c.product, k)
			}
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(r.Raw, &page); err != nil || len(page.Items) == 0 {
			t.Fatalf("%s: %v %s", c.product, err, trim(r.Raw))
		}
		for _, it := range page.Items {
			if err := p.Validate(it); err != nil {
				t.Fatalf("%s: phần tử ngoài hợp đồng: %v %s", c.product, err, it)
			}
		}
	}
	// (b) Sổ đúng tenant.
	for _, tid := range []uuid.UUID{g.prog, g.mkg, g.factory, g.verifier} {
		if n := g.count(tid, `SELECT count(*) FROM pf_metering.usage_event`); n != 0 {
			t.Fatalf("usage_event chỉ ở tenant đơn vị vận hành, tenant %s có %d", tid, n)
		}
	}
	for _, tid := range []uuid.UUID{g.operator, g.mkg, g.factory, g.verifier} {
		if n := g.count(tid, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'public_api'`); n != 0 {
			t.Fatalf("egress public_api chỉ ở tenant chủ dữ liệu, tenant %s có %d", tid, n)
		}
		if n := g.count(tid, `SELECT count(*) FROM pf_party.partner_read_log`); n != 0 {
			t.Fatalf("partner_read_log chỉ ở tenant bên cấp, tenant %s có %d", tid, n)
		}
	}
	mkgViewer := g.user(g.mkg, "", meteringport.RoleMeteringViewer)
	for _, it := range g.want(mkgViewer.Do(harness.Admin, http.MethodGet, "/v1/metering/egress", "", nil), 200, "nhật ký xuất tenant MKG").Items() {
		if str(it, "channel") != "valve" {
			t.Fatalf("metering_viewer MKG chỉ thấy egress van của tenant mình: %v", it)
		}
	}
	thirdViewer := g.user(g.factory, "", meteringport.RoleMeteringViewer)
	if r := g.want(thirdViewer.Do(harness.Admin, http.MethodGet, "/v1/metering/egress", "", nil), 200, "nhật ký xuất tenant nhà máy"); len(r.Items()) != 0 {
		t.Fatalf("tenant bên thứ ba không thấy nhật ký xuất của bên cấp: %s", trim(r.Raw))
	}
	// Khóa của nhà máy không đọc được job / tệp của khách hàng khác, và ngược lại.
	g.wantCode(g.public(g.verifierKey, http.MethodGet, "/v1/jobs/"+g.exportJob, nil), 404, "apiclient.job_not_found", "job của khách hàng khác")
	// (c) Bí mật khóa: không ở log của ba server, không ở bất kỳ bảng nào của pf_apiclient / pf_metering / pf_party / shared.
	logs := g.s.Logs.String()
	ctx := context.Background()
	adminPool := g.s.AdminPF()
	rows, err := adminPool.Query(ctx, `SELECT table_schema || '.' || table_name FROM information_schema.tables WHERE table_type = 'BASE TABLE'
	  AND table_schema IN ('pf_apiclient', 'pf_metering', 'pf_party', 'shared')`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(tables) < 10 {
		t.Fatalf("liệt kê bảng: %v %d", err, len(tables))
	}
	for _, sec := range g.secrets {
		_, tail, _ := strings.Cut(strings.TrimPrefix(sec, "agk_"), "_")
		if strings.Contains(logs, tail) {
			t.Fatal("R-14: bí mật khóa có trong log")
		}
		for _, tb := range tables {
			var n int
			if err := adminPool.QueryRow(ctx, "SELECT count(*) FROM "+tb+" x WHERE strpos(row_to_json(x)::text, $1) > 0", tail).Scan(&n); err != nil {
				t.Fatalf("%s: %v", tb, err)
			}
			if n != 0 {
				t.Fatalf("R-92: bí mật khóa có trong bảng %s", tb)
			}
		}
	}
	if !strings.Contains(logs, "api-public") {
		t.Fatal("log của api-public phải được gom (kiểm tra quét có tác dụng)")
	}
}

// 14 — RB-1: verify_chain đúng trên TỪNG stream của mọi sổ G5 mà kịch bản đã ghi (pf_apiclient bốn sổ, pf_metering ba
// sổ, pf_party cấp quyền + sổ đọc), ở mọi tenant có dữ liệu; UPDATE / DELETE bằng vai ứng dụng bị chặn.
func (g *g5) stepLedgersChained() {
	ctx := context.Background()
	for _, c := range []struct {
		role, table string
		tids        []uuid.UUID
	}{
		{apiclientport.DBRole, "pf_apiclient.client_event", []uuid.UUID{g.operator}},
		{apiclientport.DBRole, "pf_apiclient.api_key_event", []uuid.UUID{g.operator}},
		{apiclientport.DBRole, "pf_apiclient.subscription_event", []uuid.UUID{g.operator}},
		{apiclientport.DBRole, "pf_apiclient.quota_plan", []uuid.UUID{g.operator}},
		{metering.DBRole, "pf_metering.usage_event", []uuid.UUID{g.operator}},
		{metering.DBRole, "pf_metering.egress_event", []uuid.UUID{g.prog, g.mkg, g.operator}},
		{metering.DBRole, "pf_metering.statement", []uuid.UUID{g.operator}},
		{party.DBRole, "pf_party.share_grant_event", []uuid.UUID{g.prog}},
		{party.DBRole, "pf_party.partner_read_log", []uuid.UUID{g.prog}},
	} {
		pool := g.s.PF(c.role)
		for _, tid := range c.tids {
			var streams []string
			if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
				rows, err := tx.Query(ctx, "SELECT DISTINCT stream_key FROM "+c.table)
				if err != nil {
					return err
				}
				if streams, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
					return err
				}
				for _, sk := range streams {
					r, err := ledger.Verify(ctx, tx, c.table, tid, sk)
					if err != nil {
						return err
					}
					if !r.OK || r.RowsChecked == 0 {
						return fmt.Errorf("verify_chain %s %s: %+v", c.table, sk, r)
					}
				}
				return nil
			}); err != nil {
				g.t.Fatalf("%s: %v", c.table, err)
			}
			if len(streams) == 0 {
				g.t.Fatalf("%s (tenant %s): kịch bản phải để lại dữ liệu trong sổ", c.table, tid)
			}
			for _, stmt := range []string{"UPDATE " + c.table + " SET tenant_id = tenant_id", "DELETE FROM " + c.table} {
				if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, stmt); return e }); err == nil {
					g.t.Fatalf("RB-1: %s bằng vai %s phải bị chặn", stmt, c.role)
				}
			}
		}
	}
}

// ---------- bơm outbox G5 ----------

// g5consumer là một consumer của cmd/worker mà kịch bản G5 cần: consumer liên module (assembly.Consumers — van MKG,
// pf.apiclient.export, pf.metering.statement), xuất gói MRV và cảnh báo DLP của pf/notify.
type g5consumer struct {
	name    string
	module  db.ModuleID // W2.6: giao dịch inbox trên app_worker, SET LOCAL ROLE r_<module>
	events  []string
	handler inbox.Handler
}

type g5delivery struct {
	c   g5consumer
	env outbox.Envelope
}

// g5pump thay outbox-publisher + RabbitMQ cho cụm pf: giao mỗi sự kiện của các tenant kịch bản tới đúng consumer qua
// shared.inbox trên vai DB của module nhận (R-61), cùng sổ đăng ký với tiến trình thật.
type g5pump struct {
	t          *testing.T
	s          *harness.Stack
	tids       []uuid.UUID
	consumers  []g5consumer
	seen       map[uuid.UUID]bool
	delivered  []g5delivery
	byConsumer map[string]int
}

func newG5Pump(t *testing.T, s *harness.Stack, tids ...uuid.UUID) *g5pump {
	p := &g5pump{t: t, s: s, tids: tids, seen: map[uuid.UUID]bool{}, byConsumer: map[string]int{}}
	for _, c := range platformasm.Consumers {
		p.consumers = append(p.consumers, g5consumer{name: c.Name, module: c.Module, events: c.Events, handler: c.Handler(s.Mods)})
	}
	p.consumers = append(p.consumers,
		g5consumer{name: mrvport.ExportConsumer, module: mrv.DBModule, events: []string{mrvport.EventExportRequested}, handler: s.Mods.MRV.ExportHandler()})
	for _, c := range s.Mods.Notify.Consumers() {
		if c.Name == notifyport.ConsumerEgressAnomaly {
			p.consumers = append(p.consumers, g5consumer{name: c.Name, module: notify.DBModule, events: c.Events, handler: c.Handler})
		}
	}
	return p
}

// deliver giao một phong bì cho mọi consumer nhận loại của nó; trả số lượt xử lý mới.
func (p *g5pump) deliver(env outbox.Envelope) int {
	p.t.Helper()
	n := 0
	for _, c := range p.consumers {
		if !slices.Contains(c.events, env.EventType) {
			continue
		}
		ok, err := inbox.Process(context.Background(), db.ForModule(p.s.PF(dbadmin.AppWorker), c.module), c.name, env, c.handler)
		if err != nil {
			p.t.Fatalf("consumer %s xử lý %s (%s): %v", c.name, env.EventType, env.EventID, err)
		}
		if ok {
			n++
			p.byConsumer[c.name]++
		}
		p.delivered = append(p.delivered, g5delivery{c: c, env: env})
	}
	return n
}

// run giao mọi sự kiện outbox chưa giao của các tenant tới khi không còn sự kiện mới.
func (p *g5pump) run() int {
	p.t.Helper()
	ctx := context.Background()
	tids := make([]string, 0, len(p.tids))
	for _, t := range p.tids {
		tids = append(tids, t.String())
	}
	n := 0
	for {
		rs, err := p.s.PF(dbadmin.OutboxPublisher).Query(ctx, `SELECT id, tenant_id, event_type, schema_version, occurred_at, recorded_at, producer, payload
		  FROM shared.outbox WHERE tenant_id = ANY($1::text[]::uuid[]) ORDER BY id`, tids)
		if err != nil {
			p.t.Fatal(err)
		}
		var envs []outbox.Envelope
		for rs.Next() {
			var e outbox.Envelope
			if err := rs.Scan(&e.EventID, &e.TenantID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.RecordedAt, &e.Producer, &e.Payload); err != nil {
				p.t.Fatal(err)
			}
			envs = append(envs, e)
		}
		rs.Close()
		progressed := false
		for _, e := range envs {
			if p.seen[e.EventID] {
				continue
			}
			p.seen[e.EventID], progressed = true, true
			n += p.deliver(e)
		}
		if !progressed {
			return n
		}
	}
}

// redeliverAll giao lại MỌI sự kiện đã giao (RabbitMQ giao ít nhất một lần): inbox phải bỏ qua (R-61).
func (p *g5pump) redeliverAll() {
	p.t.Helper()
	for _, d := range p.delivered {
		ok, err := inbox.Process(context.Background(), db.ForModule(p.s.PF(dbadmin.AppWorker), d.c.module), d.c.name, d.env, d.c.handler)
		if err != nil || ok {
			p.t.Fatalf("R-61: giao lặp %s → %s phải bị bỏ qua: processed=%v err=%v", d.env.EventType, d.c.name, ok, err)
		}
	}
}
