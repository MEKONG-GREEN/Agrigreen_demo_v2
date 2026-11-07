package knaccounts_test

import (
	"context"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

type rolloutPolicy struct {
	accport.UnitDelegationPolicy
	unitDefinition  *accport.InternalUnitDefinition
	delegDefinition *accport.DelegationDefinition
	grants          atomic.Int64
	revokes         atomic.Int64
	people          atomic.Int64
}

func newRolloutPolicy(t *testing.T) *rolloutPolicy {
	t.Helper()
	p, err := extension.UnitDelegationPolicy()
	if err != nil {
		t.Fatal(err)
	}
	return &rolloutPolicy{UnitDelegationPolicy: p}
}

func (p *rolloutPolicy) InternalUnitDefinition() accport.InternalUnitDefinition {
	if p.unitDefinition != nil {
		return *p.unitDefinition
	}
	return p.UnitDelegationPolicy.InternalUnitDefinition()
}

func (p *rolloutPolicy) DelegationDefinition() accport.DelegationDefinition {
	if p.delegDefinition != nil {
		return *p.delegDefinition
	}
	return p.UnitDelegationPolicy.DelegationDefinition()
}

func (p *rolloutPolicy) AuthorizeDelegationGrant(ctx context.Context, department string) (accport.DelegationGrantDecision, bool, error) {
	p.grants.Add(1)
	return p.UnitDelegationPolicy.AuthorizeDelegationGrant(ctx, department)
}

func (p *rolloutPolicy) AuthorizeDelegationRevoke(ctx context.Context) (bool, error) {
	p.revokes.Add(1)
	return p.UnitDelegationPolicy.AuthorizeDelegationRevoke(ctx)
}

func (p *rolloutPolicy) PeopleScope(ctx context.Context) (accport.PeopleScopeDecision, bool, error) {
	p.people.Add(1)
	return p.UnitDelegationPolicy.PeopleScope(ctx)
}

// A synthetic definition is deliberately different from compatibility data.
// This proves that the shared consumers use every field of the owner contract,
// while the production owner rules themselves remain unchanged.
func TestUnitDelegationDefinitionsDriveHTTPAndLedgerOnPostgres(t *testing.T) {
	policy := newRolloutPolicy(t)
	policy.unitDefinition = &accport.InternalUnitDefinition{
		Kinds: []accport.CatalogItem{{Code: "doi", Label: "Đội công tác"}}, DepartmentKinds: []string{"doi"},
		OrgKinds: []string{"ttkn_province"}, SeedStatus: "owner test seed",
		Seed: []accport.InternalUnitSeedItem{{Code: "doi_tt", Kind: "doi", Name: "Đội Trồng trọt", DepartmentCode: "trong_trot", SortOrder: 12}},
	}
	policy.delegDefinition = &accport.DelegationDefinition{
		Powers: []accport.CatalogItem{{Code: "duyet_bao_cao", Label: "Quyền do chủ khai báo"}}, MaxDuration: 2 * 24 * time.Hour,
		GranteeRoles: []string{accport.KNXa},
	}
	clk := clock.NewFixed(time.Now())
	f := newFixture(t, fixtureDeps{UnitDelegations: policy, Clock: clk})
	d := &delegFlow{t: t, f: f, clk: clk, root: f.as(f.national, authz.RolePlatformAdmin)}
	root := d.root
	cat := root.do(http.MethodGet, "/v1/accounts/mgmt-levels", nil)
	if cat.status != http.StatusOK || !sameCatalog(cat.body["powers"], policy.delegDefinition.Powers) || cat.body["delegation_max_days"] != float64(2) {
		t.Fatalf("owner delegation catalog ignored: %d %v", cat.status, cat.body)
	}
	seed := root.do(http.MethodPost, "/v1/accounts/internal-units/seed", map[string]any{"org_ref": f.p91.String()})
	if seed.status != http.StatusOK || seed.body["status"] != policy.unitDefinition.SeedStatus || len(seed.body["created"].([]any)) != 1 {
		t.Fatalf("owner seed ignored: %d %v", seed.status, seed.body)
	}
	u := seed.body["created"].([]any)[0].(map[string]any)
	if u["code"] != "doi_tt" || u["kind_label"] != "Đội công tác" || u["department_code"] != "trong_trot" || u["sort_order"] != float64(12) {
		t.Fatalf("owner seed fields ignored: %v", u)
	}
	for _, org := range []string{f.national.String(), f.coop30292.String()} {
		r := root.do(http.MethodPost, "/v1/accounts/internal-units/seed", map[string]any{"org_ref": org})
		if r.status != http.StatusUnprocessableEntity || codeOf(r) != "accounts.internal_unit_org_invalid" {
			t.Fatalf("owner organization kinds ignored: %d %v", r.status, r.body)
		}
	}
	body := map[string]any{"org_ref": f.p91.String(), "code": "doi_moi", "kind": "doi", "name": "Đội mới", "department_code": "trong_trot"}
	created := root.do(http.MethodPost, "/v1/accounts/internal-units", body)
	if created.status != http.StatusCreated || created.body["kind_label"] != "Đội công tác" {
		t.Fatalf("owner kind or department-kind rejected: %d %v", created.status, created.body)
	}
	id := created.body["id"].(string)
	body["name"] = "Đội đã sửa"
	if r := root.do(http.MethodPut, "/v1/accounts/internal-units/"+id, body); r.status != http.StatusOK || r.body["name"] != "Đội đã sửa" || r.body["kind_label"] != "Đội công tác" {
		t.Fatalf("owner update: %d %v", r.status, r.body)
	}
	if r := root.do(http.MethodPost, "/v1/accounts/internal-units/"+id+"/deactivate", map[string]any{"reason": "Hợp nhất vào đơn vị khác"}); r.status != http.StatusOK || r.body["active"] != false {
		t.Fatalf("deactivate: %d %v", r.status, r.body)
	}
	log := root.do(http.MethodGet, "/v1/accounts/internal-units/"+id+"/log", nil)
	if log.status != http.StatusOK || len(log.body["items"].([]any)) != 3 {
		t.Fatalf("unit ledger lost changes: %d %v", log.status, log.body)
	}
	body["code"], body["kind"], body["department_code"] = "legacy_tram", "tram", ""
	if r := root.do(http.MethodPost, "/v1/accounts/internal-units", body); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("legacy kind bypassed owner definition: %d %v", r.status, r.body)
	}
	list := root.do(http.MethodGet, "/v1/accounts/internal-units?org_ref="+f.p91.String(), nil)
	if list.status != http.StatusOK || !sameCatalog(list.body["kinds"], policy.unitDefinition.Kinds) || len(list.body["items"].([]any)) != 1 {
		t.Fatalf("owner unit list ignored: %d %v", list.status, list.body)
	}
	mgr := d.createUser(asgCase{role: accport.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: "truong_phong"})
	staff := d.createUser(asgCase{role: accport.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: "chuyen_vien"})
	xa := d.createUser(asgCase{role: accport.KNXa, org: "p91", depts: []string{"trong_trot"}, communes: []string{"30292"}, mgmt: "chuyen_vien"})
	ctx := d.selectContext(mgr, "trong_trot")
	grant := func(in map[string]any) resp {
		r, _ := d.app(mgr.id, f.tenant, http.MethodPost, "/v1/me/delegations", ctx, "", "", in)
		return r
	}
	for name, in := range map[string]map[string]any{
		"legacy grantee role": d.delegBody(staff, "", "duyet_bao_cao", time.Hour),
		"owner duration":      d.delegBody(xa, "", "duyet_bao_cao", 3*24*time.Hour),
		"legacy power":        d.delegBody(xa, "", "mo_lai_ky", time.Hour),
	} {
		if r := grant(in); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("%s bypassed owner definition: %d %v", name, r.status, r.body)
		}
	}
	r := grant(d.delegBody(xa, "", "duyet_bao_cao", time.Hour))
	if r.status != http.StatusCreated || r.body["power_label"] != "Quyền do chủ khai báo" {
		t.Fatalf("owner grantee role or label ignored: %d %v", r.status, r.body)
	}
	mine, _ := d.app(mgr.id, f.tenant, http.MethodGet, "/v1/me/delegations", ctx, "", "", nil)
	if mine.status != http.StatusOK || mine.body["items"].([]any)[0].(map[string]any)["power_label"] != "Quyền do chủ khai báo" {
		t.Fatalf("list label ignored: %d %v", mine.status, mine.body)
	}
	revoked, _ := d.app(mgr.id, f.tenant, http.MethodPost, "/v1/me/delegations/"+r.body["delegation_id"].(string)+"/revoke", ctx, "", "",
		map[string]any{"reason": "Thu hồi theo quyết định mới"})
	if revoked.status != http.StatusOK || revoked.body["power_label"] != "Quyền do chủ khai báo" {
		t.Fatalf("revoke label ignored: %d %v", revoked.status, revoked.body)
	}
	// A configured instance must not mutate a separate compatibility instance.
	other := newFixture(t, fixtureDeps{})
	otherCat := other.as(other.national, authz.RolePlatformAdmin).do(http.MethodGet, "/v1/accounts/mgmt-levels", nil)
	if otherCat.body["delegation_max_days"] != float64(30) || !sameCatalog(otherCat.body["powers"], accport.Powers) {
		t.Fatalf("owner configuration escaped its instance: %v", otherCat.body)
	}
}

func TestUnitDelegationInvalidConfigurationStopsStartup(t *testing.T) {
	for _, which := range []string{"units", "delegations"} {
		t.Run(which, func(t *testing.T) {
			policy := newRolloutPolicy(t)
			if which == "units" {
				policy.unitDefinition = &accport.InternalUnitDefinition{}
			} else {
				policy.delegDefinition = &accport.DelegationDefinition{}
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid configured owner fell back at startup")
				}
			}()
			accounts.New(nil, clock.System{}, accounts.Deps{UnitDelegations: policy})
		})
	}
}

func TestUnitDelegationRolloutRetainsNativeHTXAndMKGOnPostgres(t *testing.T) {
	observe := func(policy accport.UnitDelegationPolicy) map[string]any {
		f := newFixture(t, fixtureDeps{UnitDelegations: policy})
		native := f.as(f.mkg, accport.RoleAccountAdmin)
		out := map[string]any{}
		for _, c := range []struct {
			name, org, role string
		}{
			{"htx", f.coop30292.String(), "coop_manager"},
			{"mkg", f.mkg.String(), "param_setter"},
			{"mkg-admin", f.mkg.String(), "registry_admin"},
		} {
			out[c.name] = f.createObserved(native, c.org, c.role)
			if out[c.name].(map[string]any)["status"] != http.StatusCreated {
				t.Fatalf("native %s not exercised: %v", c.name, out[c.name])
			}
		}
		id := f.mustCreate(native, f.mkg.String(), "param_setter")
		changed := native.do(http.MethodPost, "/v1/accounts/users/"+id+"/assignment", map[string]any{"roles": []string{"param_setter", "masterdata_admin"}})
		out["change"] = map[string]any{"status": changed.status, "state": f.userState(native, id)}
		csv := "ho_ten,email,org_ref,vai_tro\n" +
			"Cán bộ HTX,htx-" + f.tenant.String()[:8] + "@vi-du.vn," + f.coop30292.String() + ",coop_manager\n" +
			"Cán bộ MKG,mkg-" + f.tenant.String()[:8] + "@vi-du.vn," + f.mkg.String() + ",param_setter\n"
		imported := native.raw(http.MethodPost, "/v1/accounts/users/import", "native-import-"+ids.New().String(), "text/csv", []byte(csv))
		out["import"] = map[string]any{"status": imported.status, "created": imported.body["created"], "rejected": imported.body["rejected"]}
		if changed.status != http.StatusOK || imported.status != http.StatusOK || imported.body["created"] != float64(2) {
			t.Fatalf("native change/import not exercised: %v", out)
		}
		return out
	}
	before, after := observe(nil), observe(newRolloutPolicy(t))
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("unit/delegation rollout changed native accounts: before=%v after=%v", before, after)
	}
}

// Identities and MFA still belong to authentication, regardless of owner config.
func TestUnitDelegationOwnerDoesNotBypassAdminMFAOnPostgres(t *testing.T) {
	f := newFixture(t, fixtureDeps{UnitDelegations: newRolloutPolicy(t)})
	c := caller{f: f, token: f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, OrgRef: f.national, Roles: []string{authz.RolePlatformAdmin}})}
	r := c.do(http.MethodPost, "/v1/accounts/internal-units/seed", map[string]any{"org_ref": f.p91.String()})
	if r.status != http.StatusForbidden || codeOf(r) != "mfa_required" {
		t.Fatalf("owner bypassed Admin MFA: %d %v", r.status, r.body)
	}
}
