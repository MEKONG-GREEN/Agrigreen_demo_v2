package extension

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func mustUnitDelegation(t *testing.T) accport.UnitDelegationPolicy {
	t.Helper()
	p, err := UnitDelegationPolicy()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Định nghĩa chủ KN giữ đúng giá trị bridge hiện hành của accounts (seed và thời hạn so thêm ở tests/knaccounts).
func TestUnitDelegationDefinitionsMatchCompatibilityValues(t *testing.T) {
	p := mustUnitDelegation(t)
	u, d := p.InternalUnitDefinition(), p.DelegationDefinition()
	if !reflect.DeepEqual(u.Kinds, accport.InternalUnitKinds) {
		t.Fatalf("kinds %v != %v", u.Kinds, accport.InternalUnitKinds)
	}
	if !reflect.DeepEqual(u.DepartmentKinds, []string{accport.UnitKindPhong}) {
		t.Fatalf("department kinds %v", u.DepartmentKinds)
	}
	if !reflect.DeepEqual(d.Powers, accport.Powers) {
		t.Fatalf("powers %v != %v", d.Powers, accport.Powers)
	}
	if d.MaxDuration != 30*24*time.Hour || !reflect.DeepEqual(d.GranteeRoles, []string{authz.KNTinh}) {
		t.Fatalf("delegation limits %v %v", d.MaxDuration, d.GranteeRoles)
	}
	// KN không khai báo đơn vị nội bộ cho HTX, MKG, DCRD hay Nút nền tảng.
	want := []string{string(partyport.OrgTTKNNational), string(partyport.OrgTTKNProvince)}
	if !reflect.DeepEqual(u.OrgKinds, want) {
		t.Fatalf("org kinds %v", u.OrgKinds)
	}
	for _, k := range []partyport.OrgKind{partyport.OrgCooperative, partyport.OrgMKG, partyport.OrgDCRD, partyport.OrgExtensionTeam} {
		if slices.Contains(u.OrgKinds, string(k)) {
			t.Fatalf("owner declares internal units for %s", k)
		}
	}
}

// Mỗi lần đọc là bản sao độc lập: sửa kết quả không đổi định nghĩa chủ hay instance khác.
func TestUnitDelegationDefinitionsAreIsolated(t *testing.T) {
	a, b := mustUnitDelegation(t), mustUnitDelegation(t)
	u, d := a.InternalUnitDefinition(), a.DelegationDefinition()
	u.Kinds[0].Code, u.Seed[0].Name, u.OrgKinds[0], u.DepartmentKinds[0] = "kho", "Khác", string(partyport.OrgCooperative), "tram"
	d.Powers[0].Code, d.GranteeRoles[0] = "toan_quyen", authz.KNAdmin
	for _, p := range []accport.UnitDelegationPolicy{a, b} {
		if !reflect.DeepEqual(p.InternalUnitDefinition(), knInternalUnitDefinition()) || !reflect.DeepEqual(p.DelegationDefinition(), knDelegationDefinition()) {
			t.Fatal("definition mutated through a returned copy")
		}
	}
}

func knProvinceSession() port.SessionContext {
	return port.SessionContext{ID: uuid.New(), AssignmentID: uuid.New(), Role: port.SessionRoleProvince, Department: partyport.DeptTrongTrot,
		OrgRef: uuid.New(), UnitRef: uuid.New(), Scope: authz.Scope{Units: []string{"91"}}, MgmtLevel: "truong_phong", Manager: true,
		InternalUnit: "trong_trot"}
}

func sessionCtx(t *testing.T, k port.SessionContext) context.Context {
	t.Helper()
	s, err := port.NewSessionContext(k)
	if err != nil {
		t.Fatal(err)
	}
	return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), MFA: true, SelectedSession: &s})
}

func problemStatus(err error) int {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Status
	}
	return 0
}

func TestDelegationGrantDecisionMatrix(t *testing.T) {
	policy := mustUnitDelegation(t)
	base := knProvinceSession()
	cases := []struct {
		name    string
		mutate  func(*port.SessionContext)
		dept    string
		allowed bool
		status  int // lỗi cuối (handled) — 0 = không lỗi
	}{
		{name: "manager own department", allowed: true},
		{name: "manager explicit department", dept: partyport.DeptTrongTrot, allowed: true},
		{name: "manager by group", mutate: func(k *port.SessionContext) { k.MgmtLevel, k.ProvinceGroup = "chuyen_vien", "lanh_dao_phong" }, allowed: true},
		{name: "other department", dept: partyport.DeptHanhChinhTongHop},
		{name: "department not trimmed", dept: " " + partyport.DeptTrongTrot},
		{name: "not manager", mutate: func(k *port.SessionContext) { k.Manager = false }},
		{name: "view only", mutate: func(k *port.SessionContext) { k.ViewOnly = true }},
		{name: "support", mutate: func(k *port.SessionContext) { k.Support = &port.SessionSupport{SessionID: uuid.New()} }},
		{name: "no unit", mutate: func(k *port.SessionContext) { k.UnitRef = uuid.Nil }},
		{name: "national", mutate: func(k *port.SessionContext) { k.Role = port.SessionRoleNational }, status: http.StatusForbidden},
		{name: "commune", mutate: func(k *port.SessionContext) { k.Role = port.SessionRoleCommune }, status: http.StatusForbidden},
		{name: "admin", mutate: func(k *port.SessionContext) { k.Role, k.Department = port.SessionRoleAdmin, "" }, status: http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := base
			if c.mutate != nil {
				c.mutate(&k)
			}
			got, handled, err := policy.AuthorizeDelegationGrant(sessionCtx(t, k), c.dept)
			if !handled || problemStatus(err) != c.status || (c.status == 0 && err != nil) {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			if got.Allowed != c.allowed {
				t.Fatalf("allowed=%v want %v", got.Allowed, c.allowed)
			}
			want := accport.DelegationGrantDecision{}
			if c.allowed {
				want = accport.DelegationGrantDecision{Allowed: true, UnitRef: k.UnitRef, Department: k.Department}
			}
			if got != want {
				t.Fatalf("decision %+v want %+v", got, want)
			}
		})
	}
}

// native / phiên lạ / ràng buộc dở dang: chỉ request native là unhandled, còn lại lỗi cuối — không bao giờ quyết định
// từ Principal.KN hay vai realm.
func TestUnitDelegationSessionSelectionFailsClosed(t *testing.T) {
	policy := mustUnitDelegation(t)
	native := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), MFA: true,
		Roles: []string{authz.RolePlatformAdmin}})
	leader := authz.KNContext{ID: uuid.New(), Role: authz.KNTinh, Department: partyport.DeptTrongTrot, UnitRef: uuid.New(), Manager: true}
	dangling := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), KN: &leader})
	foreign, err := authz.NewSession("htx", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	other := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), SelectedSession: &foreign, KN: &leader})
	forged, err := authz.NewSession("kn", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	forged = forged.WithPayload([]byte(`{"Version":1,"Context":{"Role":"kn_tinh","Manager":true}}`))
	bad := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), SelectedSession: &forged})

	for name, c := range map[string]struct {
		ctx     context.Context
		handled bool
		status  int
	}{
		"native":          {ctx: native},
		"unauthenticated": {ctx: context.Background(), handled: true, status: http.StatusUnauthorized},
		"dangling KN":     {ctx: dangling, handled: true, status: 428},
		"foreign profile": {ctx: other, handled: true, status: http.StatusForbidden},
		"forged payload":  {ctx: bad, handled: true, status: http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			g, h1, e1 := policy.AuthorizeDelegationGrant(c.ctx, "")
			h2, e2 := policy.AuthorizeDelegationRevoke(c.ctx)
			ps, h3, e3 := policy.PeopleScope(c.ctx)
			for i, r := range []struct {
				h bool
				e error
			}{{h1, e1}, {h2, e2}, {h3, e3}} {
				if r.h != c.handled || problemStatus(r.e) != c.status || (c.status == 0 && r.e != nil) {
					t.Fatalf("call %d: handled=%v err=%v", i, r.h, r.e)
				}
			}
			if g != (accport.DelegationGrantDecision{}) || ps != (accport.PeopleScopeDecision{}) {
				t.Fatalf("decision leaked: %+v %+v", g, ps)
			}
		})
	}
}

func TestDelegationRevokeRequiresProvinceSession(t *testing.T) {
	policy := mustUnitDelegation(t)
	for role, status := range map[string]int{port.SessionRoleProvince: 0, port.SessionRoleNational: http.StatusForbidden,
		port.SessionRoleCommune: http.StatusForbidden, port.SessionRoleAdmin: http.StatusForbidden} {
		k := knProvinceSession()
		k.Role, k.Manager = role, false // người giao không cần còn là lãnh đạo; người giao kiểm ở accounts
		handled, err := policy.AuthorizeDelegationRevoke(sessionCtx(t, k))
		if !handled || problemStatus(err) != status || (status == 0 && err != nil) {
			t.Fatalf("%s: handled=%v err=%v", role, handled, err)
		}
	}
}

func TestPeopleScopeDecisionMatrix(t *testing.T) {
	policy := mustUnitDelegation(t)
	base := knProvinceSession()
	unit := accport.PeopleScopeDecision{Mode: accport.PeopleScopeInternalUnit, UnitRef: base.UnitRef, InternalUnit: "trong_trot",
		MemberRole: authz.KNTinh, OfficerRole: authz.KNXa}
	all := accport.PeopleScopeDecision{Mode: accport.PeopleScopeAll, InternalUnit: "trong_trot"}
	self := accport.PeopleScopeDecision{Mode: accport.PeopleScopeSelf, InternalUnit: "trong_trot"}
	cases := []struct {
		name   string
		mutate func(*port.SessionContext)
		want   accport.PeopleScopeDecision
		err    error
	}{
		{name: "leader with internal unit", want: unit},
		{name: "leader by group", mutate: func(k *port.SessionContext) { k.MgmtLevel, k.ProvinceGroup = "chuyen_vien", "lanh_dao_phong" }, want: unit},
		// Cờ quyền tính lại từ catalog chủ: Manager của payload không tự cấp phạm vi.
		{name: "payload manager without catalog flag", mutate: func(k *port.SessionContext) { k.MgmtLevel = "chuyen_vien" }, want: self},
		{name: "leader without internal unit", mutate: func(k *port.SessionContext) { k.InternalUnit = "" },
			want: accport.PeopleScopeDecision{Mode: accport.PeopleScopeSelf}},
		{name: "staff", mutate: func(k *port.SessionContext) { k.MgmtLevel, k.Manager = "chuyen_vien", false }, want: self},
		{name: "hcth", mutate: func(k *port.SessionContext) {
			k.Department, k.MgmtLevel = partyport.DeptHanhChinhTongHop, "chuyen_vien"
		}, want: all},
		{name: "hcth view only", mutate: func(k *port.SessionContext) {
			k.Department, k.MgmtLevel, k.ViewOnly = partyport.DeptHanhChinhTongHop, "chuyen_vien", true
		}, want: self},
		{name: "hcth board group", mutate: func(k *port.SessionContext) {
			k.Department, k.MgmtLevel, k.ProvinceGroup = partyport.DeptHanhChinhTongHop, "chuyen_vien", "vien_chuc"
		}, want: all},
		{name: "unit leader reads all", mutate: func(k *port.SessionContext) { k.MgmtLevel, k.ViewOnly = "lanh_dao_don_vi", true }, want: all},
		{name: "board reads all", mutate: func(k *port.SessionContext) { k.MgmtLevel, k.ProvinceGroup = "chuyen_vien", "ban_giam_doc" }, want: all},
		{name: "admin", mutate: func(k *port.SessionContext) { k.Role, k.Department, k.MgmtLevel = port.SessionRoleAdmin, "", "" }, want: all},
		{name: "national", mutate: func(k *port.SessionContext) { k.Role, k.MgmtLevel = port.SessionRoleNational, "giam_doc" }, want: self},
		{name: "commune", mutate: func(k *port.SessionContext) { k.Role, k.MgmtLevel = port.SessionRoleCommune, "giam_doc" }, want: self},
		{name: "unknown level", mutate: func(k *port.SessionContext) { k.MgmtLevel = "tong_giam_doc" }, err: accport.ErrOwnerAttributesRevoked},
		{name: "unknown group", mutate: func(k *port.SessionContext) { k.ProvinceGroup = "nhom_la" }, err: accport.ErrOwnerAttributesRevoked},
		{name: "unknown duty", mutate: func(k *port.SessionContext) { k.Duties = []string{"toan_quyen"} }, err: accport.ErrOwnerAttributesRevoked},
		{name: "group on commune", mutate: func(k *port.SessionContext) {
			k.Role, k.MgmtLevel, k.ProvinceGroup = port.SessionRoleCommune, "", "vien_chuc"
		},
			err: accport.ErrOwnerAttributesRevoked},
		{name: "duty on admin", mutate: func(k *port.SessionContext) {
			k.Role, k.Department, k.MgmtLevel, k.Duties = port.SessionRoleAdmin, "", "", []string{authz.DutyFormOwner}
		}, err: accport.ErrOwnerAttributesRevoked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := base
			if c.mutate != nil {
				c.mutate(&k)
			}
			got, handled, err := policy.PeopleScope(sessionCtx(t, k))
			if !handled || !errors.Is(err, c.err) || (c.err == nil && err != nil) {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			if c.err != nil {
				if got != (accport.PeopleScopeDecision{}) {
					t.Fatalf("revoked attributes leaked a scope: %+v", got)
				}
				return
			}
			if got != c.want {
				t.Fatalf("scope %+v want %+v", got, c.want)
			}
		})
	}
}
