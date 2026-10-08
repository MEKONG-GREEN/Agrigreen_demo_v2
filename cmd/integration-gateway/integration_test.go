package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// intEnv là cổng tích hợp THẬT (Build + integrationPorts thật + gateway.NewHandler) trên DB thật, cùng Admin API của
// pf/apiclient và pf/party để cấp khóa và cấp quyền.
type intEnv struct {
	t        *testing.T
	pg       *pgtest.Env
	iss      *authztest.FakeIssuer
	admin    *httptest.Server
	public   *httptest.Server
	mod      *apiclient.Module
	deps     apiclient.PublicDeps
	log      *slog.Logger
	operator uuid.UUID
	grantor  uuid.UUID
	third    uuid.UUID
}

type intResp struct {
	status int
	header http.Header
	raw    []byte
	body   map[string]any
}

func (r intResp) code() string { s, _ := r.body["code"].(string); return s }

func doReq(t *testing.T, method, url string, hdr map[string]string, body any) intResp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := intResp{status: res.StatusCode, header: res.Header, raw: raw}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

// adminAs gọi Admin API với token MFA của người dùng (tenant, vai).
func (e *intEnv) adminAs(tid uuid.UUID, roles ...string) func(method, path string, body any) intResp {
	tok := e.iss.Sign(e.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles, MFA: true})
	return func(method, path string, body any) intResp {
		e.t.Helper()
		h := map[string]string{"Authorization": "Bearer " + tok}
		if method == http.MethodPost {
			h["Idempotency-Key"] = ids.New().String()
		}
		return doReq(e.t, method, e.admin.URL+path, h, body)
	}
}

func (e *intEnv) call(method, path, key string, body any, extra ...string) intResp {
	e.t.Helper()
	h := map[string]string{apiclientport.KeyHeader: key}
	for i := 0; i+1 < len(extra); i += 2 {
		h[extra[i]] = extra[i+1]
	}
	return doReq(e.t, method, e.public.URL+path, h, body)
}

func want(t *testing.T, r intResp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: muốn %d, nhận %d %s", what, status, r.status, r.raw)
	}
}

func newIntEnv(t *testing.T) *intEnv {
	t.Helper()
	pg := pgtest.Get(t)
	t.Setenv(apiclientport.EnvPepperPrefix+"1", strings.Repeat("p", apiclientport.MinPepperBytes))
	t.Setenv(apiclientport.EnvPepperCurrent, "1")
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil || peppers == nil {
		t.Fatalf("pepper: %v", err)
	}
	store, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e := &intEnv{t: t, pg: pg, iss: authztest.New(t), operator: ids.New(), grantor: ids.New(), third: ids.New()}
	clk := clock.System{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	opened := map[string]bool{}
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		opened[role] = true
		return pg.Pool(t, migrations.PF, role), nil
	}
	// Gốc lắp ráp THẬT: Build → integrationPorts thật (metering, party GrantLookup, ba nguồn).
	mod, d, err := assembly.Build(context.Background(), open, assembly.Config{Operator: e.operator, Peppers: peppers, Store: store, Replicas: 1,
		EgressMode: meter.ModeEnforce, Clock: clk, Log: log})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// W2.6 (ADR-0028): cổng (integration-gateway, bí danh api-public) chỉ mở MỘT vai đăng nhập app_public; mỗi module chạy dưới r_<module>.
	if len(opened) != 1 || !opened[dbadmin.AppPublic] {
		t.Fatalf("cổng chỉ mở vai %s, đã mở %v", dbadmin.AppPublic, opened)
	}
	e.mod, e.deps, e.log = mod, d, log
	// Handler THẬT của cổng (W5.2): router Public + lớp chứng chỉ client; không tệp gắn → chỉ khóa API.
	e.public = httptest.NewServer(gateway.NewHandler(mod, d, nil, log))
	t.Cleanup(e.public.Close)

	// Admin API (MFA) — như api-platform: pf/apiclient (cùng module) + pf/party (cấp quyền dp_*).
	tl := telemetry.NewLogger("test", io.Discard)
	adm := httpx.NewEngine(tl, 1<<20)
	g := adm.Group("/v1", httpx.Authenticate(e.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pg.Pool(t, migrations.PF, "platform_api")))
	mod.RegisterAdmin(g)
	party.New(pg.Pool(t, migrations.PF, party.DBRole), clk).RegisterAdmin(g)
	e.admin = httptest.NewServer(adm)
	t.Cleanup(e.admin.Close)
	return e
}

// seedCollection dựng dữ liệu bên cấp tối thiểu cho collection_volume, ghi thẳng bảng của từng module
// (mỗi module một vai, tenant bên cấp): đơn vị hành chính (pf/masterdata), thửa + nông hộ (pf/geo),
// vụ + bảng kê thu gom hiện hành (pf/cropcycle). Xã 31117: 10 nông hộ tháng 2026-09 (= k mặc định 10 →
// công bố); xã 31120: 3 nông hộ tháng 2026-08 (< k → bị ẩn, không bao giờ trả).
func (e *intEnv) seedCollection() {
	t := e.t
	ctx := context.Background()
	actor, coop := ids.New(), ids.New()
	exec := func(role string, f func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenant.WithTenantTx(ctx, e.pg.Pool(t, migrations.PF, role), e.grantor, f); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
	}
	// W3.3: thửa và vụ ở pf_coop — ghi dưới r_cooperative qua vai đăng nhập đơn vị app_public.
	execCoop := func(f func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenant.WithTenantTx(ctx, db.ForModule(e.pg.Pool(t, migrations.PF, dbadmin.AppPublic), db.Module("pf/cooperative")), e.grantor, f); err != nil {
			t.Fatalf("seed pf/cooperative: %v", err)
		}
	}
	exec("masterdata_app", func(tx pgx.Tx) error {
		for _, u := range []struct{ code, level, parent string }{{"92", "province", ""}, {"31117", "commune", "92"}, {"31120", "commune", "92"}} {
			var parent *string
			if u.parent != "" {
				parent = &u.parent
			}
			if _, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.admin_unit (tenant_id, code, valid_from, name, level, parent_code, source, actor_ref)
			  VALUES ($1, $2, '2025-07-01'::date, $3, $4, $5, 'kiểm thử G5', $6)`, e.grantor, u.code, "Đơn vị "+u.code, u.level, parent, actor); err != nil {
				return err
			}
		}
		return nil
	})
	type plot struct {
		ref, hh, season uuid.UUID
		commune         string
		month           string
	}
	var plots []plot
	for i := range 13 {
		p := plot{ref: ids.New(), hh: ids.New(), season: ids.New(), commune: "31117", month: "2026-09-15"}
		if i >= 10 {
			p.commune, p.month = "31120", "2026-08-15"
		}
		plots = append(plots, p)
	}
	execCoop(func(tx pgx.Tx) error {
		for i, p := range plots {
			if _, err := tx.Exec(ctx, `INSERT INTO pf_coop.plot (tenant_id, ref, code, commune_code, cooperative_ref, household_ref, status, current_version,
			    current_boundary_id, geom, area_ha, source, created_by)
			  VALUES ($1, $2, $3, $4, $5, $6, 'active', 1, $7, ST_GeomFromText('MULTIPOLYGON(((105.7 10.0,105.701 10.0,105.701 10.001,105.7 10.0)))', 4326),
			    1.5000::numeric, 'kiểm thử G5', $8)`,
				e.grantor, p.ref, fmt.Sprintf("G5-%02d", i), p.commune, coop, p.hh, ids.New(), actor); err != nil {
				return err
			}
		}
		return nil
	})
	now := time.Now().UTC()
	execCoop(func(tx pgx.Tx) error {
		for i, p := range plots {
			if _, err := tx.Exec(ctx, `INSERT INTO pf_coop.season_current (tenant_id, season_ref, plot_ref, plot_code, household_ref, cooperative_ref,
			    commune_code, season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no, sowing_date, expected_harvest_date,
			    harvest_open_date, area_ha, status, current_event_id, version, location_status, gps_source, on_behalf, opened_by, opened_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, 'DX', 2026, 'OM5451', $8, $9, 1, '2026-05-01'::date, '2026-08-20'::date, '2026-08-10'::date,
			    1.5000::numeric, 'open', $10, 1, 'verified', 'device', false, $11, $12)`,
				e.grantor, p.season, p.ref, fmt.Sprintf("G5-%02d", i), p.hh, coop, p.commune, ids.New(), ids.New(), ids.New(), actor, now); err != nil {
				return err
			}
		}
		return nil
	})
	exec("cropcycle_app", func(tx pgx.Tx) error {
		for _, p := range plots {
			if _, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.collection_statement_view (tenant_id, statement_ref, cooperative_ref, plot_ref, season_ref,
			    collected_on, rolls, revision, receipt_id, source_tenant_id, published_at, received_at)
			  VALUES ($1, $2, $3, $4, $5, $6::date, 10, 1, $7, $8, $9, $9)`,
				e.grantor, ids.New(), coop, p.ref, p.season, p.month, ids.New(), ids.New(), now.Add(-time.Hour)); err != nil {
				return err
			}
		}
		return nil
	})
}

// onboard làm đúng thủ tục runbook public-api-onboarding: cấp quyền dp_collection_volume (bên cấp,
// đề xuất ≠ duyệt) → khách hàng API (đơn vị vận hành, đề xuất ≠ duyệt) → đăng ký sản phẩm + scope →
// gói hạn mức (daily_units nhỏ để thử 429) → phát hành khóa. Trả bí mật khóa (chỉ một lần).
func (e *intEnv) onboard(dailyUnits int64) (clientID, grantID, key string) {
	t := e.t
	pa, pb := e.adminAs(e.grantor, authz.RolePlatformAdmin), e.adminAs(e.grantor, authz.RolePlatformAdmin)
	r := pa(http.MethodPost, "/v1/share-grants", map[string]any{"grantee_tenant_id": e.third.String(), "kinds": []string{partyport.KindDPCollectionVolume},
		"purpose":  "HĐ dữ liệu số 01/2026 — nhà máy sinh khối lập kế hoạch thu mua rơm (ADR-0025 §2)",
		"valid_to": time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)})
	want(t, r, http.StatusCreated, "đề xuất cấp quyền dp_collection_volume")
	grantID = r.body["id"].(string)
	want(t, pb(http.MethodPost, "/v1/share-grants/"+grantID+"/approve", nil), http.StatusCreated, "duyệt cấp quyền")

	ad, ap := e.adminAs(e.operator, apiclientport.RoleAPIClientAdmin), e.adminAs(e.operator, apiclientport.RoleAPIClientApprover)
	r = ad(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": e.third.String(), "display_name": "Nhà máy sinh khối (thử)",
		"purpose": "Lập kế hoạch thu mua rơm theo xã và vụ", "contact_ref": ids.New().String()})
	want(t, r, http.StatusCreated, "đề xuất khách hàng API")
	clientID = r.body["id"].(string)
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/approve", nil), http.StatusOK, "duyệt khách hàng")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": apiclientport.ProductCollectionVolume,
		"scopes": []string{"dp:collection_volume:read", "dp:collection_volume:export", apiclientport.ScopeUsageRead}})
	want(t, r, http.StatusCreated, "đề xuất đăng ký sản phẩm")
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions/"+r.body["id"].(string)+"/approve", nil), http.StatusOK, "duyệt đăng ký")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": dailyUnits,
		"monthly_units": 1000, "max_export_rows": 1000})
	want(t, r, http.StatusCreated, "đề xuất gói hạn mức")
	want(t, ap(http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", clientID, r.body["version"]), nil), http.StatusOK, "duyệt gói")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/keys", nil)
	want(t, r, http.StatusCreated, "phát hành khóa")
	key, _ = r.body["api_key"].(string)
	if _, ok := apiclientport.ParseKey(key); !ok {
		t.Fatalf("bí mật khóa phải hiện một lần, đúng dạng: %s", r.raw)
	}
	return clientID, grantID, key
}

func (e *intEnv) count(tid uuid.UUID, q string, args ...any) int {
	e.t.Helper()
	var n int
	owner := e.pg.Pool(e.t, migrations.PF, dbadmin.Migrator)
	if err := tenant.WithTenantTx(context.Background(), owner, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), q, args...).Scan(&n)
	}); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Tích hợp G5 (A-2, A-3, P-06, P-10, M-05, D-01, D-04, X-03): cổng tích hợp THẬT (gốc lắp ráp Build +
// integrationPorts thật: pf/metering, party/port/grantlookup, nguồn pf/cropcycle + pf/geo +
// pf/masterdata) trên DB thật. Khóa phát qua Admin API của pf/apiclient, cấp quyền dp_collection_volume
// qua Admin API của pf/party:
//   - GET collection-volumes → 200, ô dưới k không bao giờ có (meta.suppressed_cells), một dòng
//     usage_event ở tenant vận hành + một dòng egress_event kênh public_api ở tenant CHỦ DỮ LIỆU;
//   - /v1/usage chỉ sử dụng của chính khách hàng;
//   - xuất 202 → worker (handler thật) → tải tệp phát luồng có Digest, cùng ngưỡng k;
//   - hết hạn mức ngày → 429 metering.quota_exceeded + Retry-After;
//   - sổ đo lường lỗi → 503 metering.unavailable, không byte dữ liệu;
//   - thu hồi cấp quyền → 403 ở lời gọi kế tiếp.
func TestPublicAPIRealWiring_G5Integration(t *testing.T) {
	e := newIntEnv(t)
	e.seedCollection()
	clientID, grantID, key := e.onboard(3)
	cid := uuid.MustParse(clientID)

	// Danh mục: thấy collection_volume và đúng bên cấp (GrantorLister của grantlookup).
	r := e.call(http.MethodGet, "/v1/data-products", key, nil)
	want(t, r, http.StatusOK, "danh mục")
	if !strings.Contains(string(r.raw), `"collection_volume"`) || !strings.Contains(string(r.raw), e.grantor.String()) {
		t.Fatalf("danh mục phải có collection_volume + bên cấp: %s", r.raw)
	}

	// Đọc ô tổng hợp: chỉ xã 31117 (≥ k), xã 31120 bị ẩn.
	r = e.call(http.MethodGet, "/v1/data/collection-volumes?season_code=2026-DX", key, nil)
	want(t, r, http.StatusOK, "đọc collection_volume")
	items, _ := r.body["items"].([]any)
	meta, _ := r.body["meta"].(map[string]any)
	if len(items) != 1 || meta["suppressed_cells"] != float64(1) || meta["k_applied"] != float64(10) {
		t.Fatalf("một ô công bố, một ô ẩn, k = 10: %s", r.raw)
	}
	cell := items[0].(map[string]any)
	if cell["commune_code"] != "31117" || cell["province_code"] != "92" || cell["period_month"] != "2026-09" || cell["rolls_collected"] != float64(100) || cell["households"] != nil {
		t.Fatalf("ô công bố sai hoặc lộ trường ngoài hợp đồng: %v", cell)
	}
	for _, banned := range []string{"31120", "household_ref", "plot_ref", "cooperative_ref"} {
		if strings.Contains(string(r.raw), banned) {
			t.Fatalf("phản hồi không được có %q: %s", banned, r.raw)
		}
	}
	if r.header.Get(apiclientport.HeaderDataContract) != "collection_volume.v1" || r.header.Get(apiclientport.HeaderQuotaRemaining) == "" ||
		r.header.Get(apiclientport.HeaderRateLimitLimit) == "" {
		t.Fatalf("thiếu header hợp đồng / hạn mức: %v", r.header)
	}
	// Sổ: một dòng usage_event (tenant vận hành) mỗi lời gọi đã xác thực; egress public_api ở tenant chủ dữ liệu.
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid`, cid); n != 2 {
		t.Fatalf("muốn 2 dòng usage_event (danh mục + đọc), nhận %d", n)
	}
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND call_kind = 'read' AND units = 1
	    AND data_tenant_id = $2::uuid`, cid, e.grantor); n != 1 {
		t.Fatalf("dòng đọc phải 1 đơn vị, data_tenant = bên cấp: %d", n)
	}
	if n := e.count(e.grantor, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'public_api' AND counterparty_ref = $1::uuid`, cid); n != 1 {
		t.Fatalf("muốn 1 egress_event public_api ở tenant chủ dữ liệu, nhận %d", n)
	}
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.egress_event`); n != 0 {
		t.Fatalf("egress_event không thuộc tenant vận hành: %d", n)
	}
	// K-05: sổ đọc partner_read_log ở tenant bên cấp, người đọc = mã khách hàng API.
	if n := e.count(e.grantor, `SELECT count(*) FROM pf_party.partner_read_log WHERE actor_ref = $1::uuid AND grantee_tenant_id = $2::uuid
	    AND kind = 'dp_collection_volume' AND endpoint = 'GET /v1/data/collection-volumes' AND item_count = 1`, cid, e.third); n != 1 {
		t.Fatalf("muốn 1 dòng partner_read_log ở tenant bên cấp, nhận %d", n)
	}

	// /v1/usage: của chính khách hàng (client_id từ khóa).
	today := clock.BusinessDate(time.Now()).String()
	r = e.call(http.MethodGet, "/v1/usage?from="+today+"&to="+today, key, nil)
	want(t, r, http.StatusOK, "usage")
	if !strings.Contains(string(r.raw), `"product":"collection_volume"`) || !strings.Contains(string(r.raw), `"units":1`) {
		t.Fatalf("usage của chính mình: %s", r.raw)
	}

	// Xuất 202 → worker (handler thật của module) → tệp phát luồng có Digest.
	r = e.call(http.MethodPost, "/v1/data/collection-volumes/exports", key, map[string]any{"format": "csv", "season_code": "2026-DX"},
		"Idempotency-Key", "g5-int-export-0001")
	want(t, r, http.StatusAccepted, "yêu cầu xuất")
	jobID := r.body["id"].(string)
	ctx := context.Background()
	apPool := e.pg.Pool(t, migrations.PF, apiclient.DBRole)
	if err := tenant.WithTenantTx(ctx, apPool, e.operator, func(tx pgx.Tx) error {
		var env outbox.Envelope
		var payload []byte
		if err := tx.QueryRow(ctx, `SELECT id, payload::text FROM shared.outbox WHERE tenant_id = $1::uuid AND event_type = $2::text`,
			e.operator, apiclientport.EventExportRequested).Scan(&env.EventID, &payload); err != nil {
			return err
		}
		env.EventType, env.TenantID, env.Payload = apiclientport.EventExportRequested, e.operator, payload
		return e.mod.ExportHandler()(ctx, tx, env)
	}); err != nil {
		t.Fatalf("worker xuất: %v", err)
	}
	r = e.call(http.MethodGet, "/v1/jobs/"+jobID, key, nil)
	if r.body["status"] != "completed" || r.body["rows"] != float64(1) {
		t.Fatalf("job phải completed, 1 dòng (ô dưới k bị ẩn cả trong tệp): %s", r.raw)
	}
	r = e.call(http.MethodGet, "/v1/jobs/"+jobID+"/file", key, nil)
	want(t, r, http.StatusOK, "tải tệp")
	sum := sha256.Sum256(r.raw)
	if r.header.Get("Digest") != "sha-256="+base64.StdEncoding.EncodeToString(sum[:]) || !strings.Contains(string(r.raw), "31117") ||
		strings.Contains(string(r.raw), "31120") {
		t.Fatalf("tệp xuất (Digest, ngưỡng k): %v %q", r.header, r.raw)
	}
	// Đơn vị đã dùng hôm nay: đọc 1 + tải tệp 1 = 2 (danh mục, usage, yêu cầu xuất, trạng thái job = 0).

	// Sổ đọc không ghi được (mọi INSERT vào partner_read_log lỗi) → 503, không byte dữ liệu, 0 đơn vị.
	restore := e.failInserts("pf_party.partner_read_log")
	r = e.call(http.MethodGet, "/v1/jobs/"+jobID+"/file", key, nil)
	restore()
	if r.status != http.StatusServiceUnavailable || r.code() != partyport.ErrPartnerAuditUnavailable.Code || strings.Contains(string(r.raw), "31117") {
		t.Fatalf("sổ đọc lỗi → 503 party.partner_audit_unavailable, không dữ liệu: %d %s", r.status, r.raw)
	}

	// Đọc thứ ba → đủ 3 đơn vị; đọc thứ tư → 429 + Retry-After (hạn mức thẩm quyền pf_metering).
	want(t, e.call(http.MethodGet, "/v1/data/collection-volumes", key, nil), http.StatusOK, "đọc lần 2")
	r = e.call(http.MethodGet, "/v1/data/collection-volumes", key, nil)
	if r.status != http.StatusTooManyRequests || r.code() != meter.ErrQuotaExceeded.Code || r.header.Get("Retry-After") == "" {
		t.Fatalf("hết hạn mức ngày → 429 metering.quota_exceeded + Retry-After: %d %v %s", r.status, r.header, r.raw)
	}
	if strings.Contains(string(r.raw), "31117") {
		t.Fatalf("429 không được có dữ liệu: %s", r.raw)
	}

	// K-05: đọc 2 + tải tệp 1 = 3 dòng sổ đọc; lời gọi 429 không có dòng sổ đọc.
	if n := e.count(e.grantor, `SELECT count(*) FROM pf_party.partner_read_log WHERE actor_ref = $1::uuid`, cid); n != 3 {
		t.Fatalf("sổ đọc: đọc 2 + tải tệp 1, 429 không ghi — nhận %d", n)
	}
	// Sổ đo lường lỗi (mọi INSERT vào usage_event lỗi) → 503, không byte dữ liệu; gỡ lỗi giả lập.
	restore = e.failInserts("pf_metering.usage_event")
	r = e.call(http.MethodGet, "/v1/data-products", key, nil)
	restore()
	if r.status != http.StatusServiceUnavailable || r.code() != meter.ErrUnavailable.Code || strings.Contains(string(r.raw), "collection_volume") {
		t.Fatalf("sổ lỗi → 503 metering.unavailable, không dữ liệu: %d %s", r.status, r.raw)
	}

	// Thu hồi cấp quyền ở bên cấp → 403 ngay ở lời gọi kế tiếp (ADR-0025 §2 bước 5).
	rv := e.adminAs(e.grantor, authz.RolePlatformAdmin)(http.MethodPost, "/v1/share-grants/"+grantID+"/revoke",
		map[string]any{"reason": "Kết thúc hợp đồng dữ liệu (kiểm thử)"})
	if rv.status != http.StatusCreated && rv.status != http.StatusOK {
		t.Fatalf("thu hồi cấp quyền: %d %s", rv.status, rv.raw)
	}
	r = e.call(http.MethodGet, "/v1/jobs/"+jobID+"/file", key, nil)
	if r.status != http.StatusForbidden || r.code() != apiclientport.ErrForbidden.Code {
		t.Fatalf("sau thu hồi → 403 apiclient.forbidden: %d %s", r.status, r.raw)
	}

	// Khóa sai → 401, không thêm dòng sổ.
	before := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event`)
	want(t, e.call(http.MethodGet, "/v1/data-products", key[:len(key)-2]+"AA", nil), http.StatusUnauthorized, "khóa sai")
	if after := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event`); after != before {
		t.Fatalf("lời gọi chưa xác thực không ghi sổ: %d → %d", before, after)
	}
}

// failInserts làm MỌI INSERT vào bảng lỗi (trigger BEFORE INSERT do tài khoản quản trị cài) cho tới khi
// gọi restore. Không dựa vào thu quyền của một vai cụ thể: từ W2.3 (pf 000090) vai đăng nhập cũ
// <module>_app là thành viên INHERIT của r_<module> nên REVOKE trên <module>_app không còn làm lỗi lần
// ghi, và vai thực thi sẽ còn đổi (app_<đơn vị> + SET LOCAL ROLE, ADR-0028).
func (e *intEnv) failInserts(table string) (restore func()) {
	e.t.Helper()
	ctx, admin := context.Background(), e.pg.AdminPool(e.t, migrations.PF)
	for _, q := range []string{
		`CREATE OR REPLACE FUNCTION public.agtest_fail_insert() RETURNS trigger LANGUAGE plpgsql AS
		 $f$ BEGIN RAISE EXCEPTION 'agtest: ghi % bị lỗi giả lập', TG_TABLE_NAME; END $f$`,
		`CREATE TRIGGER agtest_fail_insert BEFORE INSERT ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION public.agtest_fail_insert()`,
	} {
		if _, err := admin.Exec(ctx, q); err != nil {
			e.t.Fatalf("cài lỗi giả lập trên %s: %v", table, err)
		}
	}
	return func() {
		e.t.Helper()
		if _, err := admin.Exec(ctx, `DROP TRIGGER agtest_fail_insert ON `+table); err != nil {
			e.t.Fatalf("gỡ lỗi giả lập trên %s: %v", table, err)
		}
	}
}
