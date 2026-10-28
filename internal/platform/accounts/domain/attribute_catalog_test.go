package domain_test

import (
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/google/uuid"
)

func TestAttributeCatalogLegacyRoleLevelGroupDepartmentParity(t *testing.T) {
	c := port.LegacyAttributeCatalog()
	levels := append([]port.MgmtLevel{{}}, port.MgmtLevels...)
	groups := append([]port.CatalogItem{{}}, port.ProvinceGroups...)
	for _, role := range port.KNRolesAll {
		for _, level := range levels {
			for _, group := range groups {
				for _, dept := range []string{"own", "other", ""} {
					a := port.Assignment{Role: role, MgmtLevel: level.Code, Departments: []string{"own"}}
					if group.Code != "" && role != port.KNTinh {
						if _, ok, err := domain.ViewOnlyDepartmentWithCatalog(a, group.Code, dept, c); err == nil || ok {
							t.Fatal("invalid stored role/group accepted")
						}
						continue
					}
					readsAll := role == port.KNTinh && (level.ReadsAllDepartments || group.Code == port.GroupBanGiamDoc)
					wantOK := dept == "own" || (readsAll && dept != "")
					wantRO := wantOK && ((role == port.KNTinh && group.Code == port.GroupBanGiamDoc) || dept != "own")
					ro, ok, err := domain.ViewOnlyDepartmentWithCatalog(a, group.Code, dept, c)
					if err != nil || ro != wantRO || ok != wantOK {
						t.Fatalf("%s/%s/%s/%s: (%v,%v) want (%v,%v):%v", role, level.Code, group.Code, dept, ro, ok, wantRO, wantOK, err)
					}
					caps, err := c.Capabilities(role, level.Code, group.Code)
					manager := role == port.KNTinh && (level.Manager || group.Code == port.GroupLanhDaoPhong)
					if err != nil || caps.Manager != manager || caps.ReadsAllDepartments != readsAll {
						t.Fatalf("capability parity: %+v %v", caps, err)
					}
				}
			}
		}
	}
}

func TestAttributeCatalogNormalizationKeepsNativeTargetsAndRoleGuards(t *testing.T) {
	base := port.LegacyAttributeCatalog()
	c, err := port.NewAttributeCatalog(port.AttributeCatalogSpec{Version: "custom/1", Levels: base.Levels(), Groups: []port.AttributeGroup{{Code: "custom_group", Label: "Nhóm riêng"}},
		Duties: []port.CatalogItem{{Code: "custom_duty", Label: "Cờ riêng"}}, Roles: base.Roles()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := domain.NormalizeAttrsWithCatalog(port.KNTinh, domain.AttrsInput{ProvinceGroup: " custom_group ", Duties: []string{" custom_duty ", "custom_duty"}}, c)
	if err != nil || got.ProvinceGroup != "custom_group" || !slices.Equal(got.Duties, []string{"custom_duty"}) {
		t.Fatalf("configured membership:%+v %v", got, err)
	}
	ref := uuid.New()
	for name, in := range map[string]domain.AttrsInput{
		"legacy group":          {ProvinceGroup: port.GroupVienChuc},
		"legacy duty":           {Duties: []string{"qtnv_bao_gia"}},
		"wrong role target":     {Facilities: []uuid.UUID{ref}},
		"nil target":            {Households: []uuid.UUID{uuid.Nil}},
		"invalid internal unit": {InternalUnit: "Đơn vị sai"},
	} {
		if _, err := domain.NormalizeAttrsWithCatalog(port.KNTinh, in, c); err == nil {
			t.Errorf("%s escaped native validation", name)
		}
	}
	if _, err := domain.NormalizeAttrsWithCatalog(port.KNXa, domain.AttrsInput{ProvinceGroup: "custom_group"}, c); err == nil {
		t.Fatal("group allowed outside configured role")
	}
	refs := make([]uuid.UUID, domain.MaxScopeTargets+1)
	for i := range refs {
		refs[i] = uuid.New()
	}
	if _, err := domain.NormalizeAttrsWithCatalog(port.KNXa, domain.AttrsInput{Facilities: refs}, c); err == nil {
		t.Fatal("native target limit removed")
	}
	dedup, err := domain.NormalizeAttrsWithCatalog(port.KNXa, domain.AttrsInput{Facilities: []uuid.UUID{ref, ref}}, c)
	if err != nil || len(dedup.Facilities) != 1 {
		t.Fatalf("native reference dedup:%+v %v", dedup, err)
	}
	for _, bad := range []struct{ level, group string }{{"retired_level", ""}, {"", "retired_group"}} {
		if _, ok, err := domain.ViewOnlyDepartmentWithCatalog(port.Assignment{Role: port.KNTinh, MgmtLevel: bad.level, Departments: []string{"own"}}, bad.group, "own", c); err == nil || ok {
			t.Fatal("unknown stored code granted own-department access")
		}
	}
}

func TestAttributeCatalogCustomRoleReadOnlyWinsWithoutManagement(t *testing.T) {
	base := port.LegacyAttributeCatalog()
	roles := base.Roles()
	for i := range roles {
		if roles[i].Role == port.KNTinh {
			roles[i].ManagementPermissions = false
		}
	}
	c, err := port.NewAttributeCatalog(port.AttributeCatalogSpec{Version: "readonly/1", Levels: base.Levels(), Groups: []port.AttributeGroup{{Code: "restricted", Label: "Hạn chế", Manager: true, ReadsAllDepartments: true, ReadOnly: true}}, Roles: roles})
	if err != nil {
		t.Fatal(err)
	}
	a := port.Assignment{Role: port.KNTinh, MgmtLevel: port.MgmtLanhDaoDonVi, Departments: []string{"own"}}
	if ro, ok, err := domain.ViewOnlyDepartmentWithCatalog(a, "restricted", "own", c); err != nil || !ok || !ro {
		t.Fatalf("own department lost read-only: %v %v %v", ro, ok, err)
	}
	if _, ok, err := domain.ViewOnlyDepartmentWithCatalog(a, "restricted", "other", c); err != nil || ok {
		t.Fatalf("management-disabled role obtained other department: %v %v", ok, err)
	}
}
