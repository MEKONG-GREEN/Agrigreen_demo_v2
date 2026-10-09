package db

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// FD: ánh xạ vai/cấp → "phải có Nút" và lỗi 403 node_scope_required khi thiếu Nút.
func TestRequiresNode_FD(t *testing.T) {
	node := uuid.New()
	kn := func(role string, org uuid.UUID) *authz.KNContext { return &authz.KNContext{Role: role, OrgRef: org} }
	for _, c := range []struct {
		name     string
		p        authz.Principal
		requires bool
		deny     bool // thiếu Nút → chặn
	}{
		{"coop_manager không org_ref", authz.Principal{Roles: []string{"coop_manager"}}, true, true},
		{"coop_manager có org_ref", authz.Principal{Roles: []string{"coop_manager"}, OrgRef: node}, true, false},
		{"kn_officer không org_ref", authz.Principal{Roles: []string{"kn_officer", "plot_editor"}}, true, true},
		{"coop_manager + platform_admin", authz.Principal{Roles: []string{"coop_manager", authz.RolePlatformAdmin}}, false, false},
		{"kn_officer + kn_province_admin", authz.Principal{Roles: []string{"kn_officer", "kn_province_admin"}}, false, false},
		{"kn_officer + kn_national_admin", authz.Principal{Roles: []string{"kn_officer", "kn_national_admin"}}, false, false},
		{"vai khác không org_ref (plot_viewer, farmer)", authz.Principal{Roles: []string{"plot_viewer", "farmer"}}, false, false},
		{"KN kn_xa không Nút", authz.Principal{Roles: []string{"kn_user"}, KN: kn(authz.KNXa, uuid.Nil)}, true, true},
		{"KN kn_xa có Nút", authz.Principal{KN: kn(authz.KNXa, node)}, true, false},
		{"KN kn_tinh không Nút (cấp tỉnh giữ nhánh rỗng)", authz.Principal{KN: kn(authz.KNTinh, uuid.Nil)}, false, false},
		{"KN kn_tw không Nút", authz.Principal{KN: kn(authz.KNTW, uuid.Nil)}, false, false},
		{"KN admin không Nút", authz.Principal{KN: kn(authz.KNAdmin, uuid.Nil)}, false, false},
		{"KN admin + realm coop_manager: chỉ xét vai ngữ cảnh", authz.Principal{Roles: []string{"coop_manager"}, KN: kn(authz.KNAdmin, uuid.Nil)}, false, false},
		{"KN kn_xa, realm coop_manager có org_ref: Nút của ngữ cảnh", authz.Principal{Roles: []string{"coop_manager"}, OrgRef: node,
			KN: kn(authz.KNXa, uuid.Nil)}, true, true},
	} {
		if got := RequiresNode(c.p); got != c.requires {
			t.Errorf("%s: RequiresNode = %v, muốn %v", c.name, got, c.requires)
		}
		ctx := authz.WithPrincipal(context.Background(), c.p)
		err := checkNodeScope(ctx, ContextVarsFrom(ctx))
		if !c.deny {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		pb := problem.From(err)
		if !errors.Is(err, ErrNodeScopeRequired) || pb.Status != 403 || pb.Code != "node_scope_required" {
			t.Errorf("%s: lỗi %v, muốn 403 node_scope_required", c.name, err)
		}
	}
	// Không Principal (consumer, job, worker) → không chặn.
	if err := checkNodeScope(context.Background(), ContextVars{}); err != nil {
		t.Fatalf("không Principal: %v", err)
	}
}

// Vai trong ánh xạ phải còn khai ở app.yaml (nguồn: app/HTX, app/KN — roles và context_roles), để đổi tên vai
// không lặng lẽ tắt bảo vệ.
func TestNodeBoundRolesDeclared_FD(t *testing.T) {
	apps, err := modreg.ReadApps(os.DirFS("../../.."))
	if err != nil || len(apps) == 0 {
		t.Fatalf("đọc app/*/app.yaml: %d app, %v", len(apps), err)
	}
	var roles, ctxRoles []string
	for _, a := range apps {
		roles = append(roles, a.Roles...)
		ctxRoles = append(ctxRoles, a.ContextRoles...)
	}
	for _, r := range append(slices.Clone(NodeBoundRoles), NodeExemptRoles...) {
		if r == authz.RolePlatformAdmin {
			continue // vai dùng chung (authz), khai ở realm
		}
		if !slices.Contains(roles, r) {
			t.Errorf("vai %q không còn trong roles của app/*/app.yaml", r)
		}
	}
	for _, r := range NodeBoundContextRoles {
		if !slices.Contains(ctxRoles, r) {
			t.Errorf("vai ngữ cảnh %q không còn trong context_roles", r)
		}
	}
}
