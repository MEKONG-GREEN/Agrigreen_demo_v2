package knaccounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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

// fixtureDeps: cấu hình owner được inject vào accounts.Deps (nil = bridge legacy, như gốc lắp ráp hiện hành).
type fixtureDeps struct {
	Roles *accport.RoleCatalog
}

type fixture struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	dir    *accounts.MemoryDirectory
	tenant uuid.UUID

	national, p91, team30292, coop30292, team31120 uuid.UUID
}

// newFixture dựng module accounts trên PG thật (vai platform_app, idempotency platform_api) với Keycloak bộ nhớ và
// sổ tổ chức giả (phạm vi theo ADR-0009: tỉnh 91 gồm xã 30292; tỉnh 92 gồm xã 31120).
func newFixture(t *testing.T, d fixtureDeps) *fixture {
	t.Helper()
	env := pgtest.Get(t)
	pty := &fakeParty{orgs: map[uuid.UUID]map[uuid.UUID]partyport.Org{}}
	f := &fixture{t: t, iss: authztest.New(t), dir: accounts.NewMemoryDirectory(), tenant: ids.New()}
	f.national = pty.add(f.tenant, partyport.OrgTTKNNational, "")
	f.p91 = pty.add(f.tenant, partyport.OrgTTKNProvince, "91")
	f.team30292 = pty.add(f.tenant, partyport.OrgExtensionTeam, "30292")
	f.coop30292 = pty.add(f.tenant, partyport.OrgCooperative, "30292")
	f.team31120 = pty.add(f.tenant, partyport.OrgExtensionTeam, "31120")
	mod := accounts.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, accounts.Deps{
		Directory: f.dir, Scope: pty, Orgs: pty, Roles: d.Roles,
	})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(),
		idempotency.Middleware(env.Pool(t, migrations.PF, "platform_api"))))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

type caller struct {
	f     *fixture
	token string
}

func (f *fixture) as(org uuid.UUID, roles ...string) caller {
	return caller{f: f, token: f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: roles, OrgRef: org, MFA: true})}
}

type resp struct {
	status int
	body   map[string]any
}

func (c caller) raw(method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.f.srv.URL+path, rd)
	if err != nil {
		c.f.t.Fatal(err)
	}
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
	b, _ := io.ReadAll(r.Body)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return resp{status: r.StatusCode, body: m}
}

func (c caller) do(method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	idem := ""
	if method == http.MethodPost {
		idem = "k-" + ids.New().String()
	}
	return c.raw(method, path, idem, "application/json", b)
}

func person(org string, roles ...string) map[string]any {
	return map[string]any{"email": "cb-" + ids.New().String()[24:] + "@vi-du.vn", "full_name": "Nguyễn Văn An", "org_ref": org, "roles": roles}
}

// createObserved tạo tài khoản và trả trạng thái quan sát được (không id).
func (f *fixture) createObserved(c caller, org, role string) map[string]any {
	f.t.Helper()
	r := c.do(http.MethodPost, "/v1/accounts/users", person(org, role))
	out := map[string]any{"status": r.status, "code": r.body["code"]}
	if r.status == http.StatusCreated {
		for k, v := range f.userState(f.as(f.national, accport.RoleAccountAdmin), userID(r)) {
			out[k] = v
		}
	}
	return out
}

func (f *fixture) mustCreate(c caller, org, role string) string {
	f.t.Helper()
	r := c.do(http.MethodPost, "/v1/accounts/users", person(org, role))
	if r.status != http.StatusCreated {
		f.t.Fatalf("create %s: %d %v", role, r.status, r.body)
	}
	return userID(r)
}

func userID(r resp) string {
	u, _ := r.body["user"].(map[string]any)
	id, _ := u["id"].(string)
	return id
}

// userState: vai realm (Keycloak), required actions và trạng thái MFA trả qua HTTP.
func (f *fixture) userState(reader caller, id string) map[string]any {
	f.t.Helper()
	uid, err := uuid.Parse(id)
	if err != nil {
		f.t.Fatalf("user id %q: %v", id, err)
	}
	ctx := context.Background()
	realm, err := f.dir.RealmRoles(ctx, uid)
	if err != nil {
		f.t.Fatal(err)
	}
	u, err := f.dir.GetUser(ctx, uid)
	if err != nil {
		f.t.Fatal(err)
	}
	r := reader.do(http.MethodGet, "/v1/accounts/users/"+id, nil)
	mfa, _ := r.body["mfa"].(map[string]any)
	return map[string]any{"realm_roles": sorted(realm), "required_actions": sorted(u.RequiredActions), "mfa_required": mfa["required"]}
}

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	if out == nil {
		out = []string{}
	}
	return out
}

func sortedList(t *testing.T, v any) []string {
	t.Helper()
	items, ok := v.([]any)
	if !ok && v != nil {
		t.Fatalf("expected list, got %T", v)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.(string))
	}
	return sorted(out)
}

// fakeParty thực hiện party/port.Orgs, OrgCodes và ScopeResolver theo ADR-0009 (pf/party thật có kiểm thử riêng).
type fakeParty struct {
	mu   sync.Mutex
	orgs map[uuid.UUID]map[uuid.UUID]partyport.Org
}

var communesOf = map[string][]string{"91": {"30292"}, "92": {"31120"}}

func (p *fakeParty) add(tid uuid.UUID, kind partyport.OrgKind, unit string) uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.orgs[tid] == nil {
		p.orgs[tid] = map[uuid.UUID]partyport.Org{}
	}
	ref := ids.New()
	p.orgs[tid][ref] = partyport.Org{Ref: ref, Kind: kind, Code: string(kind) + "-" + ref.String()[:6], Name: "Tổ chức thử", AdminUnitCode: unit, Active: true}
	return ref
}

func (p *fakeParty) Get(ctx context.Context, ref uuid.UUID) (partyport.Org, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return partyport.Org{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	o, ok := p.orgs[tid][ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return o, nil
}

func (p *fakeParty) ByCode(ctx context.Context, code string) (partyport.Org, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return partyport.Org{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, o := range p.orgs[tid] {
		if o.Code == code {
			return o, nil
		}
	}
	return partyport.Org{}, partyport.ErrOrgNotFound
}

func (p *fakeParty) Scope(ctx context.Context) (authz.Scope, error) {
	pr, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, nil
	}
	if pr.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	o, err := p.Get(ctx, pr.OrgRef)
	if err != nil {
		return authz.Scope{}, nil
	}
	switch o.Kind {
	case partyport.OrgTTKNNational, partyport.OrgDCRD, partyport.OrgMKG:
		return authz.Scope{All: true}, nil
	case partyport.OrgTTKNProvince:
		return authz.Scope{Units: append([]string{o.AdminUnitCode}, communesOf[o.AdminUnitCode]...)}, nil
	}
	return authz.Scope{Units: []string{o.AdminUnitCode}}, nil
}

var (
	_ partyport.Orgs          = (*fakeParty)(nil)
	_ partyport.OrgCodes      = (*fakeParty)(nil)
	_ partyport.ScopeResolver = (*fakeParty)(nil)
)
