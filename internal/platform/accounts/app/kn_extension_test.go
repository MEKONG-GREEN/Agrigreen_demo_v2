package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type fakeAssignments struct {
	list    []port.Assignment
	tenants []uuid.UUID
}

func (f *fakeAssignments) ActiveAssignments(ctx context.Context, role, dept string) ([]port.Assignment, error) {
	if tid, err := tenant.FromContext(ctx); err == nil {
		f.tenants = append(f.tenants, tid)
	}
	var out []port.Assignment
	for _, a := range f.list {
		if (role == "" || a.Role == role) && (dept == "" || len(a.Departments) > 0 && a.Departments[0] == dept) {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeOrgs map[uuid.UUID]partyport.Org

func (f fakeOrgs) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	o, ok := f[ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return o, nil
}

// KN v7.7 P1 → P5 (FN-XA-03.08, FN-TI-02.07, Q-05): pf/extension nhận vai, phòng ban, tỉnh, xã phụ trách và CẤP
// QUẢN LÝ từ ngữ cảnh phiên thật + sổ gán vai trò (không suy từ vai realm); danh bạ cán bộ theo phòng ban × tỉnh.
func TestFN_TI_02_07_ExtensionKNContextFromP1(t *testing.T) {
	national, ag, ct, team := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	orgs := fakeOrgs{
		national: {Ref: national, Kind: partyport.OrgTTKNNational},
		ag:       {Ref: ag, Kind: partyport.OrgTTKNProvince, AdminUnitCode: "91", ParentRef: national},
		ct:       {Ref: ct, Kind: partyport.OrgTTKNProvince, AdminUnitCode: "92", ParentRef: national},
		team:     {Ref: team, Kind: partyport.OrgExtensionTeam, AdminUnitCode: "30301", ParentRef: ag},
	}
	head, xa1, other := uuid.New(), uuid.New(), uuid.New()
	asgHead, asgXa := uuid.New(), uuid.New()
	fa := &fakeAssignments{list: []port.Assignment{
		{ID: asgHead, UserRef: head, Role: port.KNTinh, OrgRef: ag, UnitRef: ag, Departments: []string{"trong_trot"}, MgmtLevel: "truong_phong", Active: true},
		{ID: asgXa, UserRef: xa1, Role: port.KNXa, OrgRef: team, UnitRef: ag, Departments: []string{"trong_trot"}, Communes: []string{"30301"}, MgmtLevel: "chuyen_vien", Active: true},
		{ID: uuid.New(), UserRef: xa1, Role: port.KNXa, OrgRef: team, UnitRef: ag, Departments: []string{"trong_trot"}, Communes: []string{"30292", "30301"}, Active: true},
		{ID: uuid.New(), UserRef: other, Role: port.KNXa, OrgRef: ct, UnitRef: ct, Departments: []string{"trong_trot"}, Communes: []string{"31001"}, Active: true},
		{ID: uuid.New(), UserRef: uuid.New(), Role: port.KNTW, OrgRef: national, UnitRef: national, Departments: []string{"trong_trot"}, Active: true},
	}}
	e := app.ExtensionKN{Assignments: fa, Orgs: orgs}
	tid := uuid.New()

	// Không có X-AG-Context → ok = false (extension giữ đường suy từ vai realm).
	if _, ok, err := e.KNContext(authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid})); ok || err != nil {
		t.Fatalf("không ngữ cảnh: ok=%v err=%v", ok, err)
	}
	// Cấp Tỉnh (Trưởng phòng): tỉnh từ đơn vị sở hữu phòng ban, cấp quản lý từ lần gán.
	ctx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, KN: &authz.KNContext{
		AssignmentID: asgHead, Role: port.KNTinh, Department: "trong_trot", OrgRef: ag, UnitRef: ag}}), tid)
	c, ok, err := e.KNContext(ctx)
	if err != nil || !ok || c.Role != extport.KNRoleTinh || c.Department != "trong_trot" || c.ProvinceCode != "91" || c.MgmtLevel != "truong_phong" {
		t.Fatalf("Cấp Tỉnh: %+v ok=%v err=%v", c, ok, err)
	}
	// Cấp Xã gắn Nút tổ khuyến nông (không có UnitRef): đi lên cây tới TTKN tỉnh; xã phụ trách từ ngữ cảnh.
	ctx = authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, KN: &authz.KNContext{
		AssignmentID: asgXa, Role: port.KNXa, Department: "trong_trot", OrgRef: team, Communes: []string{"30301"}}})
	if c, ok, err = e.KNContext(ctx); err != nil || !ok || c.ProvinceCode != "91" || len(c.Communes) != 1 || c.MgmtLevel != "chuyen_vien" {
		t.Fatalf("Cấp Xã: %+v ok=%v err=%v", c, ok, err)
	}
	// Ngữ cảnh hỗ trợ của Admin không gắn tài khoản: cấp quản lý không biết.
	ctx = authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, KN: &authz.KNContext{Role: port.KNAdmin}})
	if c, ok, err = e.KNContext(ctx); err != nil || !ok || c.Role != extport.KNRoleAdmin || c.ProvinceCode != "" || c.MgmtLevel != "" {
		t.Fatalf("Admin: %+v ok=%v err=%v", c, ok, err)
	}

	// Danh bạ: chỉ Cấp Xã/Tỉnh của phòng ban × tỉnh 91; một người nhiều lần gán cùng cấp → gộp xã; tenant
	// truyền tường minh (job worker không có Principal).
	offs, err := e.Officers(context.Background(), tid, "trong_trot", "91")
	if err != nil || len(offs) != 2 {
		t.Fatalf("danh bạ: %+v err=%v", offs, err)
	}
	if offs[0].ActorRef != head || offs[0].Level != port.ScheduleLevelProvince || offs[0].MgmtLevel != "truong_phong" {
		t.Fatalf("người duyệt: %+v", offs[0])
	}
	if offs[1].ActorRef != xa1 || offs[1].Level != port.ScheduleLevelCommune || len(offs[1].Communes) != 2 || offs[1].ProvinceCode != "91" {
		t.Fatalf("cán bộ xã (gộp xã): %+v", offs[1])
	}
	if len(fa.tenants) == 0 || fa.tenants[len(fa.tenants)-1] != tid {
		t.Fatalf("tenant của danh bạ: %v", fa.tenants)
	}
	if offs, _ := e.Officers(context.Background(), tid, "trong_trot", "92"); len(offs) != 1 || offs[0].ActorRef != other {
		t.Fatalf("tỉnh 92: %+v", offs)
	}
}
