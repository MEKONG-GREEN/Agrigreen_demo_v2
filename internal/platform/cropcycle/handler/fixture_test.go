package handler_test

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/porttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// startTime: 29/09/2026 10:00 giờ Việt Nam.
var startTime = time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)

const today = "2026-09-29"

type fixture struct {
	t        *testing.T
	iss      *authztest.FakeIssuer
	app      *httptest.Server
	admin    *httptest.Server
	mod      *cropcycle.Module
	pool     *pgxpool.Pool
	clk      *clock.Fixed
	plots    *fakePlots
	orgs     *fakeOrgs
	params   *fakeParams
	farmers  *fakeFarmers
	tenant   uuid.UUID
	mkg      uuid.UUID         // tenant MKG riêng (ADR-0020) — tenant phong bì van, tenant tài khoản mkg_sync
	share    *porttest.Sharing // cấp quyền chia sẻ giả tenant → mkg (pf/party)
	coopA    uuid.UUID         // xã 31117
	coopB    uuid.UUID         // xã 31120
	knA      uuid.UUID         // tổ KN xã 31117
	h1, h2   uuid.UUID         // nông hộ của HTX A
	h3, h4   uuid.UUID         // h3: HTX A; h4: HTX B
	p1, p2   uuid.UUID         // thửa HTX A (h1, h2); p2 lệch GPS 120 m
	p3, p4   uuid.UUID         // p3: HTX A (h3); p4: HTX B (h4)
	p5       uuid.UUID         // thửa thứ hai của h1
	sopAdmin caller
	approver caller
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, cropcycle.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	f := &fixture{t: t, iss: iss, pool: pool, clk: clock.NewFixed(startTime), tenant: ids.New(), mkg: ids.New(), share: porttest.New(),
		coopA: ids.New(), coopB: ids.New(), knA: ids.New(), h1: ids.New(), h2: ids.New(), h3: ids.New(), h4: ids.New(),
		p1: ids.New(), p2: ids.New(), p3: ids.New(), p4: ids.New(), p5: ids.New()}
	sc := &fakeScope{scopes: map[uuid.UUID]authz.Scope{f.knA: {Units: []string{"31117"}}}}
	f.plots = &fakePlots{plots: map[uuid.UUID]coopport.Plot{}, dist: map[uuid.UUID]decimal.Decimal{}}
	area := decimal.RequireFromString("1.5000")
	for i, p := range []struct {
		ref, coop, hh uuid.UUID
		commune       string
		dist          int64
	}{{f.p1, f.coopA, f.h1, "31117", 10}, {f.p2, f.coopA, f.h2, "31117", 120}, {f.p3, f.coopA, f.h3, "31117", 0},
		{f.p4, f.coopB, f.h4, "31120", 5}, {f.p5, f.coopA, f.h1, "31117", 30}} {
		f.plots.put(coopport.Plot{Ref: p.ref, Code: "TH000000" + string(rune('1'+i)), CommuneCode: p.commune, CooperativeRef: p.coop,
			HouseholdRef: p.hh, Status: coopport.PlotActive, Version: 1, AreaHa: area}, p.dist)
	}
	f.orgs = &fakeOrgs{orgs: map[uuid.UUID]partyport.Org{
		f.coopA: {Ref: f.coopA, Kind: partyport.OrgCooperative, Code: "HTXA", AdminUnitCode: "31117", Active: true},
		f.coopB: {Ref: f.coopB, Kind: partyport.OrgCooperative, Code: "HTXB", AdminUnitCode: "31120", Active: true},
	}}
	f.params = &fakeParams{vals: map[string]decimal.Decimal{}}
	f.farmers = &fakeFarmers{links: map[uuid.UUID][]uuid.UUID{}}
	f.mod = cropcycle.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), f.clk, cropcycle.Deps{Scope: sc, Orgs: f.orgs, Plots: f.plots, Params: f.params,
		Catalogs: newFakeCatalogs(), Farmers: f.farmers})
	f.share.Grant(f.tenant, f.mkg, partyport.ShareKinds...) // ADR-0020: tenant chương trình → tenant MKG, mọi loại
	f.mod.UseSharing(f.share, f.share)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 4<<20)
	f.mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 4<<20)
	f.mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.app, f.admin = httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	f.sopAdmin = f.as(f.tenant, uuid.Nil, port.RoleSOPAdmin)
	f.approver = f.as(f.tenant, uuid.Nil, authz.RoleParamApprover)
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, token: tok, actor: actor}
}

func (f *fixture) platformAdmin() caller { return f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin) }
func (f *fixture) managerA() caller      { return f.as(f.tenant, f.coopA, port.RoleCoopManager) }
func (f *fixture) managerB() caller      { return f.as(f.tenant, f.coopB, port.RoleCoopManager) }
func (f *fixture) officerA() caller      { return f.as(f.tenant, f.knA, partyport.RoleKNOfficer) }

// farmer tạo tài khoản nông dân liên kết với các nông hộ.
func (f *fixture) farmer(hh ...uuid.UUID) caller {
	c := f.as(f.tenant, f.coopA, port.RoleFarmer)
	f.farmers.link(c.actor, hh...)
	return c
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func key() string { return "k-" + ids.New().String() }

func (c caller) do(base, method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", ctype)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, raw: raw, body: m}
}

func (c caller) post(path string, body any) resp {
	c.f.t.Helper()
	return c.postKey(path, key(), body)
}

func (c caller) postKey(path, idem string, body any) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.do(c.f.app.URL, http.MethodPost, path, idem, "application/json", b)
}

func (c caller) get(path string) resp {
	c.f.t.Helper()
	return c.do(c.f.app.URL, http.MethodGet, path, "", "application/json", nil)
}

func (c caller) adminPost(path string, body any) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.do(c.f.admin.URL, http.MethodPost, path, key(), "application/json", b)
}

func (c caller) adminGet(path string) resp {
	c.f.t.Helper()
	return c.do(c.f.admin.URL, http.MethodGet, path, "", "application/json", nil)
}

func get(m any, path ...string) any {
	cur := m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m any, path ...string) string {
	s, _ := get(m, path...).(string)
	return s
}

func num(m any, path ...string) float64 {
	n, _ := get(m, path...).(float64)
	return n
}

func list(m any, path ...string) []any {
	l, _ := get(m, path...).([]any)
	return l
}

func code(r resp) string { return str(r.body, "code") }

func expect(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: HTTP %d, muốn %d: %s", what, r.status, status, r.raw)
	}
}

func expectCode(t *testing.T, r resp, status int, c, what string) {
	t.Helper()
	if r.status != status || code(r) != c {
		t.Fatalf("%s: HTTP %d %s, muốn %d %s: %s", what, r.status, code(r), status, c, r.raw)
	}
}

// steps là quy trình mẫu: làm đất, gieo sạ, bón phân, phun thuốc (PHI 14), thu hoạch (95 ± 5).
func steps() []map[string]any {
	return []map[string]any{
		{"seq": 1, "name": "Làm đất", "activity_code": "lam_dat", "stage_code": "lam_dat", "day_offset": -3, "window_days": 2, "mandatory": true},
		{"seq": 2, "name": "Gieo sạ", "activity_code": "gieo_sa", "day_offset": 0, "window_days": 1, "mandatory": true, "evidence_kinds": []string{"anh_hien_truong"}},
		{"seq": 3, "name": "Bón thúc", "activity_code": "bon_phan", "day_offset": 10, "window_days": 3, "mandatory": true},
		{"seq": 4, "name": "Phun thuốc", "activity_code": "phun_thuoc", "day_offset": 40, "window_days": 3, "mandatory": false, "phi_days": 14},
		{"seq": 5, "name": "Thu hoạch", "activity_code": "thu_hoach", "day_offset": 95, "window_days": 5, "mandatory": true},
	}
}

// publishedSOP tạo, phát hành mẫu SOP hệ thống và (nếu variety ≠ "") gắn với giống.
func (f *fixture) publishedSOP(name, variety string) (tmpl, version string) {
	f.t.Helper()
	r := f.sopAdmin.adminPost("/v1/cropcycle/sops", map[string]any{"code": "SOP-" + ids.New().String()[30:], "name": name, "steps": steps()})
	expect(f.t, r, http.StatusCreated, "tạo SOP")
	tmpl, version = str(r.body, "ref"), str(list(r.body, "versions")[0], "id")
	expect(f.t, f.approver.adminPost("/v1/cropcycle/sop-versions/"+version+"/publish", nil), http.StatusOK, "phát hành SOP")
	if variety != "" {
		expect(f.t, f.sopAdmin.adminPost("/v1/cropcycle/variety-bindings", map[string]any{"variety_code": variety, "template_ref": tmpl}), http.StatusOK, "gắn giống")
	}
	return tmpl, version
}

// open mở vụ cho thửa bằng caller (giống OM5451, ngày sạ hôm nay, GPS thiết bị).
func (c caller) open(plot uuid.UUID, extra map[string]any) resp {
	c.f.t.Helper()
	body := map[string]any{"plot_ref": plot.String(), "season_code": "DX", "season_year": 2026, "variety_code": "OM5451",
		"sowing_date": c.f.clk.Now().In(clock.Zone()).Format(time.DateOnly), "gps": map[string]any{"lon": "105.6", "lat": "10.05", "accuracy_m": "4"}}
	for k, v := range extra {
		body[k] = v
	}
	return c.post("/v1/cropcycle/seasons", body)
}

func (c caller) mustOpen(plot uuid.UUID) string {
	c.f.t.Helper()
	r := c.open(plot, nil)
	expect(c.f.t, r, http.StatusCreated, "mở vụ")
	return str(r.body, "season", "season_ref")
}

// log ghi nhật ký cho thửa.
func (c caller) log(plot uuid.UUID, extra map[string]any) resp {
	c.f.t.Helper()
	body := map[string]any{"plot_ref": plot.String(), "activity_code": "bon_phan", "sop_step_seq": 3,
		"gps": map[string]any{"lon": "105.6", "lat": "10.05"}, "machine_codes": []string{"MPT"},
		"inputs": []map[string]any{{"name": "NPK 20-20-15", "qty": "50", "unit": "kg"}}, "note": "Bón thúc đợt 1"}
	for k, v := range extra {
		body[k] = v
	}
	return c.post("/v1/cropcycle/farm-logs", body)
}

func (f *fixture) outbox(eventType string) []map[string]any {
	f.t.Helper()
	var out []map[string]any
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT payload FROM shared.outbox WHERE event_type = $1::text ORDER BY id`, eventType)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// advanceDays dời đồng hồ nghiệp vụ n ngày.
func (f *fixture) advanceDays(n int) { f.clk.Advance(time.Duration(n) * 24 * time.Hour) }
