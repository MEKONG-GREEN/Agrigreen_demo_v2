package app

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func TestAttributeCatalogInvalidConfigurationFailsClosedBeforeDependencies(t *testing.T) {
	svc := Service{Attributes: &port.AttributeCatalog{}}
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	if svc.AttributeCatalog().Valid() {
		t.Fatal("configured invalid fell back")
	}
	if _, err := svc.knCaller(ctx); problem.From(err).Code != ErrDependency.Code {
		t.Fatalf("admin invalid catalog:%v", err)
	}
	if _, err := svc.knSelf(ctx); problem.From(err).Code != ErrDependency.Code {
		t.Fatalf("self invalid catalog:%v", err)
	}
	if _, err := svc.ResolveContext(ctx, ids.New()); problem.From(err).Code != ErrDependency.Code {
		t.Fatalf("resolve invalid catalog:%v", err)
	}
	if _, err := svc.attributeCapabilities(port.Assignment{Role: port.KNTinh}, port.AssignmentAttrs{}); problem.From(err).Code != ErrDependency.Code {
		t.Fatalf("capabilities invalid catalog:%v", err)
	}
}

func TestAttributeCatalogCSVAndReadOnlyProjectionUseSameSnapshot(t *testing.T) {
	old := port.LegacyAttributeCatalog()
	levels := old.Levels()
	for i := range levels {
		if levels[i].Code == port.MgmtTruongPhong {
			levels[i].Label = "Chức danh riêng"
			levels[i].Manager = false
		}
	}
	c, err := port.NewAttributeCatalog(port.AttributeCatalogSpec{Version: "test/1", Levels: levels, Groups: []port.AttributeGroup{{Code: "readonly_group", Label: "Chỉ đọc", Manager: true, ReadsAllDepartments: true, ReadOnly: true}}, Duties: old.Duties(), Roles: old.Roles()})
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Attributes: c}
	if got := svc.mgmtOf(" CHỨC-DANH RIÊNG "); got != port.MgmtTruongPhong {
		t.Fatalf("configured CSV label:%q", got)
	}
	// The unchanged code still normalizes to truong_phong even after its label changes.
	if got := svc.mgmtOf("Trưởng phòng"); got != port.MgmtTruongPhong {
		t.Fatal("configured CSV rejected normalized code")
	}
	if got := svc.AttributeCatalog().LevelLabel(port.MgmtTruongPhong); got != "Chức danh riêng" {
		t.Fatalf("configured label fell back: %q", got)
	}
	a := port.Assignment{Role: port.KNTinh, MgmtLevel: port.MgmtTruongPhong}
	k := &authz.KNContext{Role: port.KNTinh}
	if err := svc.applyAttrs(k, a, port.AssignmentAttrs{}, false, nil); err != nil || k.Manager {
		t.Fatalf("nonempty title inferred manager:%+v %v", k, err)
	}
	if err := svc.applyAttrs(k, a, port.AssignmentAttrs{ProvinceGroup: "readonly_group"}, false, []string{authz.PowerApprove}); err != nil || !k.ReadOnly() || k.Can(authz.PowerApprove) || len(k.Powers) != 0 {
		t.Fatalf("read-only lost to manager/delegation:%+v %v", k, err)
	}
	if _, err := svc.attributeCapabilities(a, port.AssignmentAttrs{Duties: []string{"retired_duty"}}); problem.From(err).Code != ErrContextRevoked.Code {
		t.Fatalf("retired duty accepted:%v", err)
	}
}

func TestAttributeCatalogAssignmentRetainsNativeErrorPrecedence(t *testing.T) {
	for name, in := range map[string]domain.AssignmentInput{
		"role":             {Role: "retired_role", MgmtLevel: "retired_level"},
		"department_codes": {Role: port.KNTinh, MgmtLevel: "retired_level", Departments: []string{"invalid code"}},
	} {
		t.Run(name, func(t *testing.T) {
			svc := Service{Clock: clock.System{}}
			_, err := svc.checkAssignment(context.Background(), nil, in, nil)
			got := problem.From(err)
			if got.Code != problem.Validation.Code || len(got.Errors) != 1 || got.Errors[0].Field != name {
				t.Fatalf("native error order changed: %+v", got)
			}
		})
	}
}
