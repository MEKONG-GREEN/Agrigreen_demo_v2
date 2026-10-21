package handler_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type assignmentRulesHTTP struct {
	mu                    sync.Mutex
	values                []port.AssignmentRuleValues
	nodes                 []string
	valueError, nodeError error
	mutate                bool
}

func (r *assignmentRulesHTTP) ValidateAssignment(v port.AssignmentRuleValues) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := v
	seen.Departments, seen.Communes = slices.Clone(v.Departments), slices.Clone(v.Communes)
	r.values = append(r.values, seen)
	if r.mutate {
		if len(v.Departments) > 0 {
			v.Departments[0] = "provider_changed"
		}
		if len(v.Communes) > 0 {
			v.Communes[0] = "99999"
		}
	}
	return r.valueError
}
func (r *assignmentRulesHTTP) ValidateNodeKind(role, kind string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes = append(r.nodes, role+":"+kind)
	return r.nodeError
}
func (r *assignmentRulesHTTP) deny(value, node error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.valueError, r.nodeError = value, node
}
func (r *assignmentRulesHTTP) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.values), len(r.nodes)
}

func assignmentRulesServer(t *testing.T, f *knFix, rules port.AssignmentRules) *httptest.Server {
	t.Helper()
	mod := accounts.New(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, accounts.Deps{
		Directory: f.dir, Scope: f.party, Orgs: f.party, Departments: f.depts, AssignmentRules: rules})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(),
		idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api"))))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return srv
}

func storedRuleAssignment(t *testing.T, f *knFix, id uuid.UUID, role string, org uuid.UUID, depts, communes []string) {
	t.Helper()
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var gotRole string
		var gotOrg uuid.UUID
		var gotDepts, gotCommunes []string
		if err := tx.QueryRow(context.Background(), `SELECT role,org_ref,department_codes,commune_codes
          FROM pf_accounts.role_assignment_current WHERE assignment_id=$1`, id).Scan(&gotRole, &gotOrg, &gotDepts, &gotCommunes); err != nil {
			return err
		}
		if gotRole != role || gotOrg != org || !slices.Equal(gotDepts, depts) || !slices.Equal(gotCommunes, communes) {
			t.Fatalf("provider changed persisted scope: role%s org%s depts%v communes%v", gotRole, gotOrg, gotDepts, gotCommunes)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAssignmentRulesCreateGrantChangeThroughHTTP(t *testing.T) {
	f := newKNFixture(t)
	rules := &assignmentRulesHTTP{mutate: true}
	srv := assignmentRulesServer(t, f, rules)
	in := asg(port.KNTinh, f.p91, []string{" " + deptTT + " ", deptTT})
	in["role"], in["mgmt_level"] = " kn_tinh ", " chuyen_vien "
	r := f.req(srv, f.root, http.MethodPost, "/v1/accounts/kn-users", map[string]any{
		"full_name": "Cán bộ quy tắc", "email": "rules-" + ids.New().String() + "@vi-du.vn", "phone": "0987654321", "assignments": []any{in}}, nil)
	if r.status != http.StatusCreated {
		t.Fatalf("create: %d %v", r.status, r.body)
	}
	user := uuid.MustParse(str(r.body, "user", "id"))
	initial := uuid.MustParse(asgIDs(r)[port.KNTinh])
	storedRuleAssignment(t, f, initial, port.KNTinh, f.p91, []string{deptTT}, nil)
	u, err := f.dir.GetUser(context.Background(), user)
	if err != nil || !slices.Contains(u.RequiredActions, port.ActionConfigureTOTP) {
		t.Fatal("assignment policy removed D24", err)
	}
	path := "/v1/accounts/users/" + user.String() + "/assignments"
	grant := f.req(srv, f.root, http.MethodPost, path, asg(port.KNXa, f.p91, []string{deptTT}, "30301", "30292", "30301"), nil)
	if grant.status != http.StatusCreated {
		t.Fatalf("grant: %d %v", grant.status, grant.body)
	}
	aid := uuid.MustParse(str(grant.body, "assignment", "assignment_id"))
	storedRuleAssignment(t, f, aid, port.KNXa, f.p91, []string{deptTT}, []string{"30292", "30301"})
	change := asg(port.KNXa, f.p91, []string{deptBVTV}, "30292")
	change["reason"] = "Điều chỉnh phân công phòng ban"
	changed := f.req(srv, f.root, http.MethodPut, path+"/"+aid.String(), change, nil)
	if changed.status != http.StatusOK {
		t.Fatalf("change: %d %v", changed.status, changed.body)
	}
	storedRuleAssignment(t, f, aid, port.KNXa, f.p91, []string{deptBVTV}, []string{"30292"})
	values, nodes := rules.counts()
	if values != 3 || nodes != 3 {
		t.Fatalf("not all write paths used rules: %d/%d", values, nodes)
	}
}

func TestAssignmentRulesConfiguredDenialsThroughHTTP(t *testing.T) {
	f := newKNFixture(t)
	user, _, cr := f.createKN("Người đã có vai", asg(port.KNTinh, f.p91, []string{deptTT}))
	rules := &assignmentRulesHTTP{}
	srv := assignmentRulesServer(t, f, rules)
	path := "/v1/accounts/users/" + user.String() + "/assignments"
	rules.deny(port.AssignmentRuleError{Field: "mgmt_level", Msg: "fixture owner rejected"}, nil)
	createEmail := "denied-" + ids.New().String() + "@vi-du.vn"
	wantCode(t, f.req(srv, f.root, http.MethodPost, "/v1/accounts/kn-users", map[string]any{
		"full_name": "Không tạo khi chủ từ chối", "email": createEmail, "phone": "0987654322",
		"assignments": []any{asg(port.KNTinh, f.p91, []string{deptTT})}}, nil), http.StatusUnprocessableEntity, "validation_failed", "owner rejected create")
	users, err := f.dir.FindUsers(context.Background(), port.Query{Email: createEmail})
	if err != nil || len(users) != 0 {
		t.Fatal("provider denial reached Keycloak creation", err)
	}
	wantCode(t, f.req(srv, f.root, http.MethodPost, path, asg(port.KNXa, f.p91, []string{deptTT}, "30292"), nil), http.StatusUnprocessableEntity, "validation_failed", "owner rejected valid legacy grant")
	rules.deny(nil, &port.AssignmentRuleError{Field: "org_ref", Msg: "fixture owner rejected kind"})
	change := asg(port.KNTinh, f.p91, []string{deptBVTV})
	change["reason"] = "Điều chỉnh phân công theo thử nghiệm"
	wantCode(t, f.req(srv, f.root, http.MethodPut, path+"/"+asgIDs(cr)[port.KNTinh], change, nil), http.StatusUnprocessableEntity, "validation_failed", "owner rejected valid legacy node")
	rules.deny(errors.New("fixture owner unavailable"), nil)
	wantCode(t, f.req(srv, f.root, http.MethodPost, path, asg(port.KNXa, f.p91, []string{deptTT}, "30292"), nil), http.StatusInternalServerError, "", "unavailable owner did not fallback")
	storedRuleAssignment(t, f, uuid.MustParse(asgIDs(cr)[port.KNTinh]), port.KNTinh, f.p91, []string{deptTT}, nil)
	listed := f.adminDo(f.root, http.MethodGet, path, nil)
	if listed.status != http.StatusOK || len(listed.body["items"].([]any)) != 1 {
		t.Fatalf("rejected provider wrote assignment: %d %v", listed.status, listed.body)
	}
}

func TestAssignmentRulesCannotBypassNativeGuardsThroughHTTP(t *testing.T) {
	f := newKNFixture(t)
	user, _, _ := f.createKN("Kiểm ràng buộc nền", asg(port.KNTinh, f.p91, []string{deptTT}))
	if err := f.dir.SetOrgRef(context.Background(), user, f.p91); err != nil {
		t.Fatal(err)
	}
	rules := &assignmentRulesHTTP{}
	srv := assignmentRulesServer(t, f, rules)
	path := "/v1/accounts/users/" + user.String() + "/assignments"
	grant := func(who caller, in map[string]any) resp { return f.req(srv, who, http.MethodPost, path, in, nil) }
	past := asg(port.KNXa, f.p91, []string{deptTT}, "30292")
	past["valid_from"], past["valid_to"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)
	wantCode(t, grant(f.root, past), http.StatusUnprocessableEntity, "validation_failed", "invalid validity with permissive provider")
	if count, _ := rules.counts(); count != 0 {
		t.Fatal("invalid time reached provider")
	}
	wantCode(t, grant(f.tok(f.rootID, f.tenant, false, "platform_admin"), asg(port.KNXa, f.p91, []string{deptTT}, "30292")), http.StatusForbidden, "mfa_required", "missing MFA")
	wantCode(t, grant(f.tok(f.rootID, ids.New(), true, "platform_admin"), asg(port.KNXa, f.p91, []string{deptTT}, "30292")), http.StatusNotFound, "accounts.user_not_found", "foreign tenant")
	wantCode(t, grant(f.root, asg(port.KNXa, ids.New(), []string{deptTT}, "30292")), http.StatusConflict, "accounts.prerequisite_missing", "unknown node")
	wantCode(t, grant(f.root, asg(port.KNXa, f.p91, []string{"missing_department"}, "30292")), http.StatusUnprocessableEntity, "accounts.department_invalid", "department outside node")
	wantCode(t, grant(f.root, asg(port.KNXa, f.p91, []string{deptTT}, "31120")), http.StatusUnprocessableEntity, "accounts.commune_invalid", "commune outside node")
	f.depts.mu.Lock()
	f.depts.depts[f.p91][2].Retired = true
	f.depts.mu.Unlock()
	wantCode(t, grant(f.root, asg(port.KNXa, f.p91, []string{deptBVTV}, "30292")), http.StatusUnprocessableEntity, "accounts.department_invalid", "retired department")
	f.depts.mu.Lock()
	f.depts.depts[f.p91][2].Retired = false
	f.depts.mu.Unlock()
	f.party.setActive(f.tenant, f.p91, false)
	wantCode(t, grant(f.root, asg(port.KNXa, f.p91, []string{deptTT}, "30292")), http.StatusConflict, "accounts.org_inactive", "inactive node")
	f.party.setActive(f.tenant, f.p91, true)
	prov := f.as(f.tenant, f.p91, port.RoleAccountAdmin)
	wantCode(t, grant(prov, asg(port.KNXa, f.p92, []string{deptTT}, "31120")), http.StatusForbidden, "accounts.out_of_scope", "node outside caller scope")
	wantCode(t, grant(f.root, asg(port.KNAdmin, uuid.Nil, nil)), http.StatusForbidden, "accounts.kn_admin_grant", "Admin requires target platform_admin")
}
