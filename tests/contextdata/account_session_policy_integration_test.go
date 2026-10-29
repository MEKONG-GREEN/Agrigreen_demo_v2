package contextdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type accountPolicyDepartments struct {
	units map[uuid.UUID]uuid.UUID
	calls int
}

func (d *accountPolicyDepartments) DepartmentUnit(_ context.Context, org uuid.UUID) (uuid.UUID, error) {
	d.calls++
	if unit, ok := d.units[org]; ok {
		return unit, nil
	}
	return uuid.Nil, partyport.ErrOrgNotFound
}
func (*accountPolicyDepartments) UnitDepartments(context.Context, uuid.UUID) ([]partyport.Department, error) {
	return nil, nil
}
func (*accountPolicyDepartments) DepartmentByCode(_ context.Context, unit uuid.UUID, code string) (partyport.Department, error) {
	return partyport.Department{OrgRef: unit, Code: code, Name: "Synthetic department", Status: partyport.DeptActive}, nil
}

type accountPolicyScope struct{}

func (accountPolicyScope) Scope(context.Context) (authz.Scope, error) {
	return authz.Scope{All: true}, nil
}

type accountPolicyDB struct {
	tenant.Beginner
	calls int
}

func (b *accountPolicyDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	b.calls++
	return b.Beginner.BeginTx(ctx, opts)
}

// This is a read fixture for existing projections. Append-only internal-unit
// lifecycle, hashes and support lifecycle remain covered by accounts/handler.
func TestAccountDefaultUnitUsesOwnerWithoutLegacyThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	tid, other, actor, tokenOrg, unitA, unitB := ids.New(), ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	for _, row := range []struct {
		tenant, unit uuid.UUID
		title        string
	}{{tid, unitA, "Native unit"}, {tid, unitB, "Owned unit"}, {other, unitB, "Other tenant unit"}} {
		_, err := admin.Exec(context.Background(), `INSERT INTO pf_accounts.internal_unit (tenant_id,id,org_ref,code,kind,title,last_event_id,last_seq,updated_by) VALUES($1,$2,$3,'station','tram',$4,$5,1,$6)`, row.tenant, ids.New(), row.unit, row.title, ids.New(), actor)
		if err != nil {
			t.Fatal(err)
		}
	}
	deps := &accountPolicyDepartments{units: map[uuid.UUID]uuid.UUID{tokenOrg: unitA}}
	counted := &accountPolicyDB{Beginner: env.Pool(t, migrations.PF, dbadmin.AppPlatform)}
	mod := accounts.New(counted, clock.System{}, accounts.Deps{Departments: deps, SessionPolicies: extension.AccountSessionPolicies(deps)})
	makeOwner := func(unit uuid.UUID) authz.Session {
		s, err := extport.NewSessionContext(extport.SessionContext{ID: ids.New(), Role: extport.SessionRoleProvince, Department: "trong_trot", OrgRef: unitB, UnitRef: unit})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	owner, empty := makeOwner(unitB), makeOwner(uuid.Nil)
	adminOwner, err := extport.NewSessionContext(extport.SessionContext{ID: ids.New(), Role: extport.SessionRoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := authz.NewSession("foreign", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero := authz.Session{}
	missing, malformed := owner.WithPayload(nil), owner.WithPayload([]byte("{"))
	principal := func(s *authz.Session) *authz.Principal {
		return &authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: tokenOrg, Roles: []string{"platform_admin", "kn_officer"}, MFA: true, SelectedSession: s}
	}
	legacyOnly := principal(nil)
	legacyOnly.KN = &authz.KNContext{Role: authz.KNAdmin, UnitRef: unitA}
	legacyBroader := principal(&owner)
	legacyBroader.KN = &authz.KNContext{Role: authz.KNAdmin, UnitRef: unitA, Scope: authz.Scope{All: true}}
	otherTenant := principal(&owner)
	otherTenant.TenantID = other
	cases := []struct {
		name      string
		p         *authz.Principal
		query     string
		status    int
		wantUnit  uuid.UUID
		wantTitle string
		native    bool
	}{
		{"no identity", nil, "", 401, uuid.Nil, "", false},
		{"native ancestor", principal(nil), "", 200, unitA, "Native unit", true},
		{"owner without legacy", principal(&owner), "", 200, unitB, "Owned unit", false},
		{"owner overrides legacy unit", legacyBroader, "", 200, unitB, "Owned unit", false},
		{"legacy only cannot fallback", legacyOnly, "", 428, uuid.Nil, "", false},
		{"zero owner", principal(&zero), "", 403, uuid.Nil, "", false},
		{"foreign owner", principal(&foreign), "", 403, uuid.Nil, "", false},
		{"missing payload", principal(&missing), "", 403, uuid.Nil, "", false},
		{"malformed payload", principal(&malformed), "", 403, uuid.Nil, "", false},
		{"empty owner cannot use token org", principal(&empty), "", 422, uuid.Nil, "", false},
		{"explicit query cannot bypass invalid owner", principal(&malformed), "?org_ref=" + unitA.String(), 403, uuid.Nil, "", false},
		{"explicit query with valid empty owner", principal(&empty), "?org_ref=" + unitB.String(), 200, unitB, "Owned unit", false},
		{"native explicit query", principal(nil), "?org_ref=" + unitB.String(), 200, unitB, "Owned unit", false},
		{"raw tenant remains authoritative", otherTenant, "", 200, unitB, "Other tenant unit", false},
		{"admin without unit uses authenticated home node", principal(&adminOwner), "", 200, unitA, "Native unit", true},
		{"admin explicit query still wins", principal(&adminOwner), "?org_ref=" + unitB.String(), 200, unitB, "Owned unit", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counted.calls, deps.calls = 0, 0
			engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
			group := engine.Group("/v1", func(c *gin.Context) {
				if tc.p != nil {
					c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), *tc.p))
				}
			})
			mod.RegisterApp(group)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal-units"+tc.query, nil))
			if response.Code != tc.status {
				t.Fatalf("status=%d want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.status != 200 && counted.calls != 0 {
				t.Fatalf("denied after %d transactions", counted.calls)
			}
			if tc.native {
				if deps.calls != 1 {
					t.Fatal("native ancestor resolver not used")
				}
			} else if deps.calls != 0 {
				t.Fatal("owner or explicit query fell back to native ancestor")
			}
			if tc.status == 200 {
				var body struct {
					OrgRef string `json:"org_ref"`
					Items  []struct {
						Name string `json:"name"`
					}
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.OrgRef != tc.wantUnit.String() || len(body.Items) != 1 || body.Items[0].Name != tc.wantTitle || counted.calls == 0 {
					t.Fatalf("unexpected unit/RLS result: %s", response.Body.String())
				}
			}
		})
	}
}

// Actual JWT authentication + accounts middleware resolve the server-side
// support context, project through the real owner registry, enforce the reason
// and append audit rows. The handler removes legacy state before completion.
func TestAccountSupportAuditUsesOwnerThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	tid, actor, onBehalf, unit, selected, support := ids.New(), ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	now := time.Now().UTC()
	_, err := admin.Exec(context.Background(), `INSERT INTO pf_accounts.support_session (tenant_id,support_session_id,admin_ref,acted_role,department_code,org_ref,on_behalf_of,context_id,started_at,expires_at,last_event_id,last_seq) VALUES($1,$2,$3,'kn_tinh','trong_trot',$4,$5,$6,$7,$8,$9,1)`, tid, support, actor, unit, onBehalf, selected, now, now.Add(time.Hour), ids.New())
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(context.Background(), `INSERT INTO pf_accounts.session_context (tenant_id,context_id,user_ref,role,department_code,org_ref,unit_ref,support_session_id,on_behalf_of,idem_key,created_at,expires_at) VALUES($1,$2,$3,'kn_tinh','trong_trot',$4,$4,$5,$6,'account-owner-support',$7,$8)`, tid, selected, actor, unit, support, onBehalf, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer := authztest.New(t)
	reason := "Hỗ trợ nhập liệu theo cuộc gọi được xác nhận"
	makeRegistry := func(mode string) *authz.SessionRegistry {
		registration := extension.SessionProjection()
		if mode != "valid" {
			original := registration.Project
			if mode == "foreign" {
				registration.Profile = "foreign"
			}
			registration.Project = func(ctx context.Context, p authz.Principal) (authz.Session, bool, error) {
				s, ok, err := original(ctx, p)
				if err != nil {
					return s, ok, err
				}
				switch mode {
				case "zero":
					return authz.Session{}, true, nil
				case "missing":
					return s.WithPayload(nil), true, nil
				case "malformed":
					return s.WithPayload([]byte("{")), true, nil
				case "foreign":
					v, err := authz.NewSession("foreign", s.ID(), s.Scope())
					return v.WithPayload(s.Payload()), true, err
				case "no support":
					p.SelectedSession = &s
					k, err := extport.ReadSessionContext(p)
					if err != nil {
						return s, true, err
					}
					k.Support = nil
					v, err := extport.NewSessionContext(k)
					return v, true, err
				}
				return s, ok, nil
			}
		}
		registry, err := authz.NewSessionRegistry(registration)
		if err != nil {
			t.Fatal(err)
		}
		return registry
	}
	rowsForSession := func() int {
		var n int
		if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_accounts.support_action_log WHERE tenant_id=$1 AND support_session_id=$2`, tid, support).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	cases := []struct {
		name, mode      string
		subject, tenant uuid.UUID
		mfa, withReason bool
		status          int
		executed        bool
	}{
		{"valid owner completion without legacy", "valid", actor, tid, true, true, 200, true},
		{"missing owner payload", "missing", actor, tid, true, true, 403, false},
		{"malformed owner payload", "malformed", actor, tid, true, true, 403, false},
		{"foreign owner cannot use legacy support", "foreign", actor, tid, true, true, 403, false},
		{"zero owner", "zero", actor, tid, true, true, 403, false},
		{"owner without support cannot use legacy support", "no support", actor, tid, true, true, 403, false},
		{"wrong actor", "valid", ids.New(), tid, true, true, 403, false},
		{"wrong tenant", "valid", actor, ids.New(), true, true, 403, false},
		{"missing MFA", "valid", actor, tid, false, true, 403, false},
		{"missing reason", "valid", actor, tid, true, false, 422, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := rowsForSession()
			executed := false
			mod := accounts.New(pool, clock.System{}, accounts.Deps{Scope: accountPolicyScope{}, Departments: &accountPolicyDepartments{}, Sessions: makeRegistry(tc.mode), SessionPolicies: extension.AccountSessionPolicies(&accountPolicyDepartments{})})
			engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
			group := engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), mod.Middleware())
			group.POST("/audit-probe/:id", func(c *gin.Context) {
				p, _ := authz.FromContext(c.Request.Context())
				if p.ActorRef != actor || p.TenantID != tid || !p.MFA || !p.Has("platform_admin") {
					t.Error("owner changed authenticated identity")
				}
				p.KN = nil
				c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
				executed = true
				c.JSON(200, gin.H{"ok": true})
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/audit-probe/resource", strings.NewReader(`{"secret":"body-must-not-be-logged"}`))
			req.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: tc.subject, TenantID: tc.tenant, OrgRef: ids.New(), Roles: []string{"platform_admin"}, MFA: tc.mfa}))
			req.Header.Set(authz.ContextHeader, selected.String())
			if tc.withReason {
				req.Header.Set(authz.ReasonHeader, url.PathEscape(reason))
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if response.Code != tc.status || executed != tc.executed {
				t.Fatalf("status=%d want %d executed=%v want %v: %s", response.Code, tc.status, executed, tc.executed, response.Body.String())
			}
			want := before
			if tc.executed {
				want += 2
			}
			if got := rowsForSession(); got != want {
				t.Fatalf("audit rows=%d want %d", got, want)
			}
		})
	}
	rows, err := admin.Query(context.Background(), `SELECT actor_ref,on_behalf_of,context_id,acted_role,department_code,reason,phase,coalesce(status_code,0),content::text,request_id FROM pf_accounts.support_action_log WHERE tenant_id=$1 AND support_session_id=$2 ORDER BY seq`, tid, support)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	var requestID uuid.UUID
	for rows.Next() {
		var a, b, c, r uuid.UUID
		var role, dept, why, phase, content string
		var status int
		if err := rows.Scan(&a, &b, &c, &role, &dept, &why, &phase, &status, &content, &r); err != nil {
			t.Fatal(err)
		}
		if a != actor || b != onBehalf || c != selected || role != "kn_tinh" || dept != "trong_trot" || why != reason || strings.Contains(content, "body-must-not-be-logged") {
			t.Fatalf("audit owner/identity/PII mismatch: %s %s %s %s", a, b, role, content)
		}
		if n == 0 {
			requestID = r
			if phase != "attempted" || status != 0 {
				t.Fatal("missing attempted row")
			}
		} else if phase != "completed" || status != 200 || r != requestID {
			t.Fatal("invalid completed row")
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("audit rows=%d", n)
	}
	// Tenant RLS on the public read endpoint, retaining the raw authenticated tenant.
	mod := accounts.New(pool, clock.System{}, accounts.Deps{})
	engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
	mod.RegisterAdmin(engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), httpx.RequireMFA()))
	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/support-log?support_session_id="+support.String(), nil)
	req.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: actor, TenantID: ids.New(), Roles: []string{"platform_admin"}, MFA: true}))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	if response.Code != 200 || !bytes.Contains(response.Body.Bytes(), []byte(`"items":[]`)) {
		t.Fatalf("cross-tenant support log: %d %s", response.Code, response.Body.String())
	}
}
