package extension

import (
	"context"
	"testing"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestApplicationFormRegistrationPreservesPolicy(t *testing.T) {
	profiles := FormProfiles()
	if len(profiles) != 3 {
		t.Fatal("missing profile")
	}
	for _, p := range profiles {
		if p.ResolveActor == nil {
			t.Fatalf("missing adapter %s", p.Name)
		}
		if _, used, err := p.ResolveActor(context.Background(), authz.Principal{}, p); used || err != nil {
			t.Fatalf("realm session claimed: %v", err)
		}
	}
	planning := profiles[2]
	p := authz.Principal{KN: &authz.KNContext{Role: authz.KNTinh, Department: partyport.DeptKeHoachTaiChinh}}
	subject, used, err := planning.ResolveActor(context.Background(), bindCapabilityTestPrincipal(t, p), planning)
	if err != nil || !used || planning.LevelOf(subject.Role, subject.Department) != "TINH" {
		t.Fatalf("planning publisher denied: %v", err)
	}
	p.KN.Department = partyport.DeptChanNuoiThuySan
	subject, used, err = planning.ResolveActor(context.Background(), bindCapabilityTestPrincipal(t, p), planning)
	if err != nil || !used || planning.LevelOf(subject.Role, subject.Department) != "PHONG" {
		t.Fatalf("department leaf changed: %v", err)
	}
	livestock := profiles[1]
	p.KN.Department = "other"
	if _, _, err := livestock.ResolveActor(context.Background(), bindCapabilityTestPrincipal(t, p), livestock); err == nil {
		t.Fatal("cross-department access")
	}
	p.KN.Role = authz.KNAdmin
	if _, used, err := livestock.ResolveActor(context.Background(), bindCapabilityTestPrincipal(t, p), livestock); !used || err != nil {
		t.Fatalf("administrator definition denied: %v", err)
	}
	selected, other := ids.New(), ids.New()
	p.KN = &authz.KNContext{Role: authz.KNXa, Department: partyport.DeptChanNuoiThuySan, Facilities: []uuid.UUID{selected}}
	subject, used, err = livestock.ResolveActor(context.Background(), bindCapabilityTestPrincipal(t, p), livestock)
	if err != nil || !used || !subject.AllowsTarget(port.UnitFacility, selected) || subject.AllowsTarget(port.UnitFacility, other) || subject.AllowsTarget(port.UnitHousehold, other) {
		t.Fatalf("target restrictions lost: %v", err)
	}
	profiles[0].Roles[authz.KNTW] = "changed"
	if FormProfiles()[0].Roles[authz.KNTW] != "TW" {
		t.Fatal("shared mutable profile configuration")
	}
}

// Bind through the application projection, just as selected HTTP sessions are bound.
func bindCapabilityTestPrincipal(t *testing.T, p authz.Principal) authz.Principal {
	t.Helper()
	if p.KN == nil {
		return p
	}
	k := *p.KN
	if k.ID == uuid.Nil {
		k.ID = ids.New()
	}
	p.KN = &k
	registry, err := authz.NewSessionRegistry(SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.ResolveSelected(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelectedSession = &selected
	return p
}

// #71: biểu CNTS dùng phạm vi quyền FN-14 tường minh của payload chủ; profile khác và phiên chưa từng được cấp giữ nguyên.
func TestCNTSFormActorUsesOwnerGrantScope(t *testing.T) {
	profiles := FormProfiles()
	general, livestock := profiles[0], profiles[1]
	f1, f2, legacy := ids.New(), ids.New(), ids.New()
	bind := func(g *extport.CNTSGrantScope) authz.Principal {
		k := extport.SessionContext{ID: ids.New(), Role: extport.SessionRoleCommune, Department: partyport.DeptChanNuoiThuySan,
			Communes: []string{"30001", "30002"}, Scope: authz.Scope{Units: []string{"30001", "30002"}}, Facilities: []uuid.UUID{legacy},
			CNTSGrant: g}
		s, err := extport.NewSessionContext(k)
		if err != nil {
			t.Fatal(err)
		}
		return authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), SelectedSession: &s}
	}

	p := bind(nil)
	subject, used, err := livestock.ResolveActor(context.Background(), p, livestock)
	if err != nil || !used || subject.AllowsTemplate != nil || !subject.AllowsTarget(port.UnitFacility, legacy) || len(subject.Scope.Units) != 2 {
		t.Fatalf("chưa từng được cấp quyền phải giữ hành vi cũ: %v", err)
	}

	p = bind(&extport.CNTSGrantScope{})
	subject, _, err = livestock.ResolveActor(context.Background(), p, livestock)
	if err != nil || subject.AllowsTemplate == nil || subject.AllowsTemplate("CNTS-BM01") || subject.AllowsTarget(port.UnitFacility, legacy) ||
		!subject.Scope.Empty() {
		t.Fatalf("phạm vi tường minh rỗng phải chặn: %v", err)
	}

	p = bind(&extport.CNTSGrantScope{Grants: []extport.CNTSGrant{{Ref: ids.New(), Seq: 1, Role: "ksgm", Communes: []string{"30001"},
		Facilities: []uuid.UUID{f1}, Forms: []string{"BM-01"}}}})
	subject, _, err = livestock.ResolveActor(context.Background(), p, livestock)
	if err != nil || !subject.AllowsTemplate("CNTS-BM01") || subject.AllowsTemplate("CNTS-BM04") || !subject.AllowsTarget(port.UnitFacility, f1) ||
		subject.AllowsTarget(port.UnitFacility, f2) || subject.AllowsTarget(port.UnitFacility, legacy) ||
		!subject.Scope.Allows("30001") || subject.Scope.Allows("30002") {
		t.Fatalf("AC-03/BR-02: biểu, cơ sở, xã theo quyền FN-14: %v", err)
	}
	subject, _, err = general.ResolveActor(context.Background(), p, general)
	if err != nil || subject.AllowsTemplate != nil || !subject.Scope.Allows("30002") {
		t.Fatalf("profile kn không dùng quyền CNTS: %v", err)
	}
}
