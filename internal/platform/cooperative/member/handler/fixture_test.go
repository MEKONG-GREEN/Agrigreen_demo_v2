package handler_test

// Bộ dựng kiểm thử của nông hộ/xã viên (pf/cooperative, W3.3 — chuyển từ pf/party/handler). Nút tổ chức là của
// pf/party: kiểm thử dùng bản giả các cổng partyport (Orgs, OrgCodes, OrgDirectory, ScopeResolver, ConsentStates)
// và ghi dòng pf_party.org thật bằng pool quản trị để FK household_membership/cooperative → pf_party.org (D-7)
// được thỏa.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port/farmerid"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// ---------------------------------------------------------------------------
// Bản giả pf/party
// ---------------------------------------------------------------------------

// fakeParty là sổ tổ chức giả trong bộ nhớ (theo tenant) + phạm vi theo cây (ADR-0009) + đồng ý.
type fakeParty struct {
	mu       sync.Mutex
	units    map[string]partyport.AdminUnit
	orgs     map[uuid.UUID]map[uuid.UUID]partyport.Org // tenant → ref → Nút
	consents map[uuid.UUID][]partyport.ConsentState    // chủ thể → đồng ý
}

func newFakeParty() *fakeParty {
	f := &fakeParty{units: map[string]partyport.AdminUnit{}, orgs: map[uuid.UUID]map[uuid.UUID]partyport.Org{},
		consents: map[uuid.UUID][]partyport.ConsentState{}}
	for _, u := range []partyport.AdminUnit{
		{Code: "89", Name: "An Giang", Level: partyport.LevelProvince},
		{Code: "30001", Name: "Xã A", Level: partyport.LevelCommune, ParentCode: "89"},
		{Code: "30002", Name: "Xã B", Level: partyport.LevelCommune, ParentCode: "89"},
		{Code: "92", Name: "Cần Thơ", Level: partyport.LevelProvince},
		{Code: "31001", Name: "Xã C", Level: partyport.LevelCommune, ParentCode: "92"},
	} {
		f.units[u.Code] = u
	}
	return f
}

func (f *fakeParty) put(tid uuid.UUID, o partyport.Org) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.orgs[tid] == nil {
		f.orgs[tid] = map[uuid.UUID]partyport.Org{}
	}
	f.orgs[tid][o.Ref] = o
}

func (f *fakeParty) get(tid, ref uuid.UUID) (partyport.Org, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orgs[tid][ref]
	return o, ok
}

func (f *fakeParty) Get(ctx context.Context, ref uuid.UUID) (partyport.Org, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return partyport.Org{}, err
	}
	o, ok := f.get(tid, ref)
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return o, nil
}

func (f *fakeParty) ByCode(ctx context.Context, code string) (partyport.Org, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return partyport.Org{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.orgs[tid] {
		if o.Code == code {
			return o, nil
		}
	}
	return partyport.Org{}, partyport.ErrOrgNotFound
}

func (f *fakeParty) OrgsByRefs(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]partyport.Org, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]partyport.Org{}
	for _, r := range refs {
		if o, ok := f.get(tid, r); ok {
			out[r] = o
		}
	}
	return out, nil
}

func (f *fakeParty) OrgsOfKind(ctx context.Context, kind partyport.OrgKind, units []string, activeOnly bool) ([]partyport.Org, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []partyport.Org
	for _, o := range f.orgs[tid] {
		if o.Kind != kind || (activeOnly && !o.Active) || (units != nil && !slices.Contains(units, o.AdminUnitCode)) {
			continue
		}
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b partyport.Org) int { return bytes.Compare(a.Ref[:], b.Ref[:]) })
	return out, nil
}

// Scope theo cây phân cấp như pf/party (ADR-0009).
func (f *fakeParty) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, problem.Unauthenticated
	}
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	o, ok := f.get(p.TenantID, p.OrgRef)
	if p.OrgRef == uuid.Nil || !ok || !o.Active {
		return authz.Scope{}, nil
	}
	switch o.Kind {
	case partyport.OrgTTKNNational, partyport.OrgDCRD, partyport.OrgMKG:
		return authz.Scope{All: true}, nil
	case partyport.OrgTTKNProvince:
		codes := []string{o.AdminUnitCode}
		for c, u := range f.units {
			if u.ParentCode == o.AdminUnitCode {
				codes = append(codes, c)
			}
		}
		return authz.Scope{Units: codes}, nil
	case partyport.OrgExtensionTeam, partyport.OrgCooperative:
		return authz.Scope{Units: []string{o.AdminUnitCode}}, nil
	}
	return authz.Scope{}, nil
}

func (f *fakeParty) ConsentStates(_ context.Context, subjects []uuid.UUID) (map[uuid.UUID][]partyport.ConsentState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID][]partyport.ConsentState{}
	for _, s := range subjects {
		if c, ok := f.consents[s]; ok {
			out[s] = c
		}
	}
	return out, nil
}

var (
	_ partyport.Orgs          = (*fakeParty)(nil)
	_ partyport.OrgCodes      = (*fakeParty)(nil)
	_ partyport.OrgDirectory  = (*fakeParty)(nil)
	_ partyport.ScopeResolver = (*fakeParty)(nil)
	_ partyport.ConsentStates = (*fakeParty)(nil)
)

// ---------------------------------------------------------------------------
// Bộ dựng
// ---------------------------------------------------------------------------

type fixture struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	app    *httptest.Server
	admin  *httptest.Server
	vault  *fakeVault
	party  *fakeParty
	svc    *app.Service
	db     tenant.Beginner // pool của pf/cooperative (vai r_cooperative)
	tenant uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	return newFixtureWith(t, nil)
}

// newFixtureWith dựng fixture; mw (tùy chọn) chen sau xác thực (vd ngữ cảnh phiên KN giả).
func newFixtureWith(t *testing.T, mw gin.HandlerFunc) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	ops := env.Pool(t, migrations.PF, "platform_api")
	db := shareddb.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), shareddb.Module("pf/cooperative"))
	vault, party := newFakeVault(), newFakeParty()
	svc := &app.Service{DB: db, Clock: clock.System{}, Orgs: party, Codes: party, Dir: party, Scope: party, Consents: party, Vault: farmerid.Households(vault)}
	log := telemetry.NewLogger("test", io.Discard)
	h := handler.Handler{Svc: svc}
	appEng := httpx.NewEngine(log, 1<<20)
	mws := []gin.HandlerFunc{httpx.Authenticate(iss.Verifier())}
	if mw != nil {
		mws = append(mws, mw)
	}
	h.RegisterApp(appEng.Group("/v1", append(mws, idempotency.Middleware(ops))...))
	adminEng := httpx.NewEngine(log, 1<<20)
	h.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appSrv, adminSrv := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(appSrv.Close)
	t.Cleanup(adminSrv.Close)
	return &fixture{t: t, env: env, iss: iss, app: appSrv, admin: adminSrv, vault: vault, party: party, svc: svc, db: db, tenant: ids.New()}
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

// root là quản trị toàn quốc của tenant mặc định.
func (f *fixture) root() caller {
	return f.as(f.tenant, uuid.Nil, partyport.RoleRegistryAdmin, authz.RolePlatformAdmin)
}

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (c caller) call(base, method, path, idem, ctype string, body []byte) resp {
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
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) appDo(method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	idem := ""
	if body != nil {
		b, _ = json.Marshal(body)
	}
	if method == http.MethodPost {
		idem = key()
	}
	return c.call(c.f.app.URL, method, path, idem, "application/json", b)
}

func str(m map[string]any, path ...string) string {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	s, _ := cur.(string)
	return s
}

func items(r resp) []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func rowsOf(r resp) []map[string]any {
	raw, _ := r.body["rows"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func farmer(coop uuid.UUID, phone string) map[string]any {
	return map[string]any{
		"cooperative_ref": coop.String(), "cultivated_area_ha": "1.25", "member_since": "2024-01-15",
		"identity": map[string]any{"full_name": "Nguyễn Văn An", "phone": phone, "address": "Ấp 1"},
	}
}

// createOrg giả lập tạo tổ chức ở pf/party (Admin POST /v1/orgs): dòng pf_party.org thật (FK D-7) + bản giả
// Orgs; HTX thêm hồ sơ qua bước 2 của pf/cooperative (CreateProfile). Trả ref.
func (c caller) createOrg(body map[string]any) uuid.UUID {
	c.f.t.Helper()
	f := c.f
	s := func(k string) string { v, _ := body[k].(string); return v }
	o := partyport.Org{Ref: ids.New(), Kind: partyport.OrgKind(s("kind")), Code: s("code"), Name: s("name"), AdminUnitCode: s("admin_unit_code"), Active: true}
	if p := s("parent_ref"); p != "" {
		o.ParentRef = uuid.MustParse(p)
	}
	if o.Kind == partyport.OrgCooperative {
		o.Code = fmt.Sprintf("HTX%06d", rand.IntN(900000))
	}
	var unit, parent *string
	if o.AdminUnitCode != "" {
		unit = &o.AdminUnitCode
	}
	if o.ParentRef != uuid.Nil {
		ps := o.ParentRef.String()
		parent = &ps
	}
	if _, err := f.env.AdminPool(f.t, migrations.PF).Exec(context.Background(), `INSERT INTO pf_party.org
	    (tenant_id, ref, kind, code, name, admin_unit_code, parent_ref, active, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::uuid, true, $8, 1)`, c.tid, o.Ref, string(o.Kind), o.Code, o.Name, unit, parent, ids.New()); err != nil {
		f.t.Fatalf("ghi pf_party.org: %v", err)
	}
	f.party.put(c.tid, o)
	if o.Kind == partyport.OrgCooperative {
		var prof partyport.CoopProfile
		if cp, ok := body["cooperative"].(map[string]any); ok {
			cs := func(k string) string { v, _ := cp[k].(string); return v }
			prof.TaxCode, prof.Address = cs("tax_code"), cs("address")
			if v := cs("area_ha"); v != "" {
				d := decimal.RequireFromString(v)
				prof.AreaHa = &d
			}
			if v := cs("established_on"); v != "" {
				d, _ := time.Parse(time.DateOnly, v)
				prof.EstablishedOn = &d
			}
			if loc, ok := cp["location"].(map[string]any); ok {
				lat, lon := decimal.RequireFromString(loc["lat"].(string)), decimal.RequireFromString(loc["lon"].(string))
				prof.Lat, prof.Lon = &lat, &lon
			}
		}
		if err := f.svc.CreateProfile(tenant.WithID(context.Background(), c.tid), o.Ref, prof); err != nil {
			f.t.Fatalf("hồ sơ HTX: %v", err)
		}
	}
	return o.Ref
}

// tree dựng cây: TW → tỉnh 89 (tổ xã 30001, 30002) và tỉnh 92; HTX ở 30001 và 31001.
type tree struct {
	national, p89, p92, t30001, t30002, c1, c2 uuid.UUID
}

func (f *fixture) seedTree(root caller) tree {
	var tr tree
	tr.national = root.createOrg(map[string]any{"kind": "ttkn_national", "code": "TTKNQG-" + ids.New().String()[:8], "name": "Trung tâm Khuyến nông Quốc gia"})
	tr.p89 = root.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN89-" + ids.New().String()[:8], "name": "TTKN An Giang", "admin_unit_code": "89", "parent_ref": tr.national.String()})
	tr.p92 = root.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN92-" + ids.New().String()[:8], "name": "TTKN Cần Thơ", "admin_unit_code": "92", "parent_ref": tr.national.String()})
	tr.t30001 = root.createOrg(map[string]any{"kind": "extension_team", "code": "TKN30001-" + ids.New().String()[:6], "name": "Tổ KNCĐ Xã A", "admin_unit_code": "30001", "parent_ref": tr.p89.String()})
	tr.t30002 = root.createOrg(map[string]any{"kind": "extension_team", "code": "TKN30002-" + ids.New().String()[:6], "name": "Tổ KNCĐ Xã B", "admin_unit_code": "30002", "parent_ref": tr.p89.String()})
	tr.c1 = root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Lúa Xanh", "admin_unit_code": "30001", "parent_ref": tr.t30001.String(),
		"cooperative": map[string]any{"area_ha": "120.5", "established_on": "2019-03-01", "location": map[string]any{"lat": "10.3861", "lon": "105.4352"}}})
	tr.c2 = root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Cần Thơ Xanh", "admin_unit_code": "31001", "parent_ref": tr.p92.String()})
	return tr
}

// coopCodes trả Mã HTX theo ref.
func (f *fixture) coopCodes(tr tree) (string, string) {
	c1, _ := f.party.get(f.tenant, tr.c1)
	c2, _ := f.party.get(f.tenant, tr.c2)
	return c1.Code, c2.Code
}
