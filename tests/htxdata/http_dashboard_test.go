package htxdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	kn "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// dashboardStack is the production api-platform composition (assembly.Build +
// NewRouter/NewAdminRouter): real pf/party ScopeResolver, pf/accounts selected
// session middleware with the KN owner registry, module gate and Idempotency.
// Nothing in it is replaced for the test; only the identity provider is a test
// issuer and the Keycloak directory is the in-memory directory.
type dashboardStack struct {
	t          *testing.T
	iss        *authztest.FakeIssuer
	app, admin *httptest.Server
	root       string
	phone      int
}

func newDashboardStack(t *testing.T, tid uuid.UUID) *dashboardStack {
	env := pgtest.Get(t)
	dir := accounts.NewMemoryDirectory()
	clk := clock.NewFixed(time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC))
	mods, pools, err := platformasm.Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clk, platformasm.External{Vault: clienttest.New(), Accounts: dir})
	if err != nil {
		t.Fatal(err)
	}
	s := &dashboardStack{t: t, iss: authztest.New(t)}
	d := platformasm.Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: s.iss.Verifier(),
		Ops: env.Pool(t, migrations.PF, dbadmin.AppPlatform), Modules: mods.List(), Pools: pools}
	s.app, s.admin = httptest.NewServer(platformasm.NewRouter(d)), httptest.NewServer(platformasm.NewAdminRouter(d))
	t.Cleanup(s.app.Close)
	t.Cleanup(s.admin.Close)
	rootID := dir.Seed(accport.NewUser{Username: "admin.htxdata", Email: "admin.htxdata@vi-du.vn", FirstName: "Fixture", LastName: "Admin", TenantID: tid}, true, authz.RolePlatformAdmin)
	s.root = s.token(rootID, tid, uuid.Nil, true, authz.RolePlatformAdmin)
	return s
}

func (s *dashboardStack) token(sub, tid, org uuid.UUID, mfa bool, roles ...string) string {
	return s.iss.Sign(s.t, authztest.Token{Subject: sub, TenantID: tid, OrgRef: org, Roles: roles, MFA: mfa, TTL: time.Hour})
}

func (s *dashboardStack) do(srv *httptest.Server, token, method, path, sessionID string, body any) (int, map[string]any) {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", ids.New().String())
	}
	if sessionID != "" {
		req.Header.Set(authz.ContextHeader, sessionID)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var out map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

// knUser creates a KN account through the Admin API and selects a server-side
// context for each assignment through POST /v1/me/context. It returns the user
// and, per session role, the selected context id and the assignment id.
func (s *dashboardStack) knUser(tid uuid.UUID, assignments ...map[string]any) (uuid.UUID, map[string][2]string) {
	s.t.Helper()
	s.phone++
	n := ids.New().String()[24:]
	status, body := s.do(s.admin, s.root, http.MethodPost, "/v1/accounts/kn-users", "", map[string]any{
		"full_name": "Cán bộ kiểm thử", "email": "kn-" + n + "@vi-du.vn", "phone": fmt.Sprintf("0912%06d", s.phone), "assignments": assignments})
	if status != http.StatusCreated {
		s.t.Fatalf("create KN account: %d %v", status, body)
	}
	user := uuid.MustParse(body["user"].(map[string]any)["id"].(string))
	out := map[string][2]string{}
	self := s.token(user, tid, uuid.Nil, false, accport.RoleKNUser)
	for _, x := range body["assignments"].([]any) {
		a := x.(map[string]any)
		st, sel := s.do(s.app, self, http.MethodPost, "/v1/me/context", "", map[string]any{"assignment_id": a["assignment_id"], "department_code": "trong_trot"})
		if st != http.StatusCreated {
			s.t.Fatalf("select KN context %v: %d %v", a["role"], st, sel)
		}
		out[a["role"].(string)] = [2]string{sel["context_id"].(string), a["assignment_id"].(string)}
	}
	return user, out
}

func dashboardAssignment(role string, org uuid.UUID, communes ...string) map[string]any {
	if communes == nil {
		communes = []string{}
	}
	return map[string]any{"role": role, "org_ref": org.String(), "department_codes": []string{"trong_trot"},
		"commune_codes": communes, "mgmt_level": "chuyen_vien"}
}

type dashboardCall struct {
	name          string
	tid, org, sub uuid.UUID
	role, session string
	path          string
}

// GET /v1/dashboard (G-02 unified dashboard, KN realm readers) through the
// production router. HTX organizations, households and plots are pf_party/pf_coop
// source rows; KN reads them only through the pf_party_pub/pf_cooperative_pub views
// under its own module role. Positive province/commune numbers require the caller
// node to be projected by the #130 source trigger and the HTX to be in its source
// subtree, or in the commune list of its selected session. No k_ref row is
// inserted by this test and no scope resolver or session is substituted.
func TestKNHTTPDashboardPublishedHTXData(t *testing.T) {
	f := newFixtureWith(t, true)
	empty := ids.New()
	for _, tid := range []uuid.UUID{f.tid, empty} {
		for _, u := range []struct{ code, level, parent string }{{"89", "province", ""}, {"92", "province", ""}, {"89001", "commune", "89"}, {"92001", "commune", "92"}} {
			f.exec(`INSERT INTO pf_masterdata.admin_unit(tenant_id,code,valid_from,name,level,parent_code,source,actor_ref)
    VALUES($1,$2,'2020-01-01',$2,$3,NULLIF($4,''),'published-dashboard-fixture',$5)`, tid, u.code, u.level, u.parent, ref(900))
		}
	}
	// A TTKN province node without HTX in its source subtree, and the same shape in
	// a foreign KN tenant. Both are source rows projected by the trigger.
	f.exec(`INSERT INTO pf_party.org(tenant_id,ref,kind,code,name,admin_unit_code,active,last_event_id,last_seq)
    VALUES($1,$2,'ttkn_province','N904','KN fixture','89',true,$3,1)`, f.tid, ref(904), ref(1904))
	f.exec(`INSERT INTO pf_party.org(tenant_id,ref,kind,code,name,admin_unit_code,active,last_event_id,last_seq)
    VALUES($1,$2,'ttkn_province','N905','KN fixture','89',true,$3,1)`, empty, ref(905), ref(1905))
	f.exec(`INSERT INTO pf_party.org(tenant_id,ref,kind,code,name,active,last_event_id,last_seq)
    VALUES($1,$2,'ttkn_national','N907','KN fixture',true,$3,1)`, empty, ref(907), ref(1907))
	var closure int
	if err := f.admin.QueryRow(context.Background(), `SELECT count(*) FROM k_ref.node_closure
    WHERE tenant_id=$1 AND ancestor=ANY($2::uuid[]) AND descendant=ANY($3::uuid[])`,
		f.tid, []uuid.UUID{ref(901), ref(903)}, []uuid.UUID{ref(101), ref(102), ref(104)}).Scan(&closure); err != nil || closure != 6 {
		t.Fatalf("source trigger closure 901/903 -> HTX: %d %v", closure, err)
	}

	s := newDashboardStack(t, f.tid)
	if status, body := s.do(s.admin, s.root, http.MethodPost, "/v1/orgs/"+ref(901).String()+"/departments/seed", "", nil); status != http.StatusOK {
		t.Fatalf("seed departments: %d %v", status, body)
	}
	user, sessions := s.knUser(f.tid, dashboardAssignment(accport.KNTinh, ref(901)), dashboardAssignment(accport.KNXa, ref(901), "89001"))
	tinh, xa := sessions[accport.KNTinh], sessions[accport.KNXa]

	check := func(c dashboardCall, coops, households, plots int, area string) {
		t.Helper()
		sub := c.sub
		if sub == uuid.Nil {
			sub = ids.New()
		}
		status, body := s.do(s.app, s.token(sub, c.tid, c.org, false, accport.RoleKNUser, c.role), http.MethodGet, c.path, c.session, nil)
		if status != http.StatusOK {
			t.Fatalf("%s: dashboard %d: %v", c.name, status, body)
		}
		b := body["total"].(map[string]any)
		if b["cooperatives"] != float64(coops) || b["households"] != float64(households) || b["digitized_plots"] != float64(plots) || b["digitized_area_ha"] != area {
			t.Fatalf("%s: published dashboard total: %v", c.name, b)
		}
	}
	deny := func(c dashboardCall, want int) {
		t.Helper()
		sub := c.sub
		if sub == uuid.Nil {
			sub = ids.New()
		}
		status, body := s.do(s.app, s.token(sub, c.tid, c.org, false, accport.RoleKNUser, c.role), http.MethodGet, c.path, c.session, nil)
		if status != want {
			t.Fatalf("%s: want %d, got %d %v", c.name, want, status, body)
		}
	}
	const all, p89, p92, c89 = "/v1/dashboard?period=2026-10", "/v1/dashboard?period=2026-10&unit=89", "/v1/dashboard?period=2026-10&unit=92", "/v1/dashboard?period=2026-10&unit=89001"

	// Positive, authorised callers. National TTKN node 906 is the root of the KN tree.
	check(dashboardCall{name: "national realm", tid: f.tid, org: ref(906), role: kn.RoleKNNationalAdmin, path: all}, 3, 3, 4, "6.5680")
	// Realm token org_ref -> app.node_id; HTX inside the TTKN/team source subtree.
	check(dashboardCall{name: "province 89 realm", tid: f.tid, org: ref(901), role: kn.RoleKNProvinceAdmin, path: p89}, 2, 3, 3, "6.5679")
	check(dashboardCall{name: "province 92 realm", tid: f.tid, org: ref(902), role: kn.RoleKNProvinceAdmin, path: p92}, 1, 0, 1, "0.0001")
	check(dashboardCall{name: "commune team realm", tid: f.tid, org: ref(903), role: kn.RoleKNOfficer, path: c89}, 2, 3, 3, "6.5679")
	// Server-selected KN sessions (X-AG-Context resolved by pf/accounts, owner
	// payload bound by the KN registry): app.node_id/app.xa_ids come from the
	// selected session, not from the token.
	check(dashboardCall{name: "province selected session", tid: f.tid, org: ref(901), sub: user, role: kn.RoleKNProvinceAdmin, session: tinh[0], path: p89}, 2, 3, 3, "6.5679")
	check(dashboardCall{name: "commune selected session", tid: f.tid, org: ref(903), sub: user, role: kn.RoleKNOfficer, session: xa[0], path: c89}, 2, 3, 3, "6.5679")

	// Negative: outside province (app scope), session of another user/tenant, foreign tenant.
	deny(dashboardCall{name: "outside province", tid: f.tid, org: ref(902), role: kn.RoleKNProvinceAdmin, path: "/v1/dashboard?unit=89"}, http.StatusForbidden)
	deny(dashboardCall{name: "session of another user", tid: f.tid, org: ref(901), role: kn.RoleKNProvinceAdmin, session: tinh[0], path: p89}, http.StatusForbidden)
	deny(dashboardCall{name: "session from another tenant", tid: empty, org: ref(905), sub: user, role: kn.RoleKNProvinceAdmin, session: tinh[0], path: p89}, http.StatusForbidden)
	check(dashboardCall{name: "foreign KN tenant province", tid: empty, org: ref(905), role: kn.RoleKNProvinceAdmin, path: p89}, 0, 0, 0, "0.0000")
	check(dashboardCall{name: "foreign KN tenant national", tid: empty, org: ref(907), role: kn.RoleKNNationalAdmin, path: all}, 0, 0, 0, "0.0000")
	// Projected node without HTX in its subtree and without a commune list: HTX
	// facts stay closed. The org directory still counts 2 HTX and the draft plot
	// without HTX remains (W3.4 branch e). No province-wide fallback.
	check(dashboardCall{name: "province node without HTX subtree", tid: f.tid, org: ref(904), role: kn.RoleKNProvinceAdmin, path: p89}, 2, 0, 1, "2.0001")

	// Revoked assignment: the stored context stops resolving immediately.
	status, body := s.do(s.admin, s.root, http.MethodPost, fmt.Sprintf("/v1/accounts/users/%s/assignments/%s/revoke", user, xa[1]), "",
		map[string]any{"reason": "Thu hồi phân công kiểm thử bảng điều hành"})
	if status != http.StatusOK {
		t.Fatalf("revoke assignment: %d %v", status, body)
	}
	deny(dashboardCall{name: "revoked commune session", tid: f.tid, org: ref(903), sub: user, role: kn.RoleKNOfficer, session: xa[0], path: c89}, http.StatusForbidden)

	// Missing projection: a tenant whose k_ref has not been rebuilt (#130 rollout).
	// Delete only this tenant's projection, as the operator drill does. The source
	// tree is unchanged, yet no caller with a node reads pf_coop rows: fail-closed
	// instead of the former tenant-wide fallback.
	f.exec("DELETE FROM k_ref.node_closure WHERE tenant_id=$1", f.tid)
	f.exec("DELETE FROM k_ref.org WHERE tenant_id=$1", f.tid)
	check(dashboardCall{name: "province without projection", tid: f.tid, org: ref(901), role: kn.RoleKNProvinceAdmin, path: p89}, 2, 0, 0, "0.0000")
	check(dashboardCall{name: "province session without projection", tid: f.tid, org: ref(901), sub: user, role: kn.RoleKNProvinceAdmin, session: tinh[0], path: p89}, 2, 0, 0, "0.0000")
	check(dashboardCall{name: "commune team without projection", tid: f.tid, org: ref(903), role: kn.RoleKNOfficer, path: c89}, 2, 0, 0, "0.0000")
}
