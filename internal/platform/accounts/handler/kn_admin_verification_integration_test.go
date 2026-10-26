package handler_test

import (
	"context"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// TT-13 exercises real account creation, session selection and every KN account
// mutation as a province officer with MFA. Rejections must leave both the
// assignment ledger/projection and the identity directory unchanged.
func TestTT13ProvinceOfficerCannotManageCommuneAccounts(t *testing.T) {
	f := newKNFixture(t)
	ctx := context.Background()
	officerID, _, officerCreated := f.createKN("Cán bộ tỉnh kiểm TT-13", asg(port.KNTinh, f.p91, []string{deptTT}))
	realmRoles, err := f.dir.RealmRoles(ctx, officerID)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"platform_admin", port.RoleAccountAdmin, port.RoleAccountAdminProvince} {
		if slices.Contains(realmRoles, forbidden) {
			t.Fatalf("province account received administrator: %v", realmRoles)
		}
	}
	officer := f.tok(officerID, f.tenant, true, port.RoleKNUser)
	selected := f.ctxID(officer, asgIDs(officerCreated)[port.KNTinh], deptTT)
	if r := f.withCtx(officer, http.MethodGet, "/v1/probe", selected, nil); r.status != http.StatusOK {
		t.Fatalf("province context: %v", r.body)
	}
	principal := f.lastProbe()
	if principal.KN == nil || principal.KN.Role != port.KNTinh || !slices.Equal(principal.Roles, []string{port.RoleKNUser}) {
		t.Fatalf("selected business role changed realm privileges: %+v", principal)
	}
	targetID, _, targetCreated := f.createKN("Cán bộ xã kiểm TT-13", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	assignment := asgIDs(targetCreated)[port.KNXa]
	beforeUser, err := f.dir.GetUser(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRoles, err := f.dir.RealmRoles(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	counts := func() [3]int64 {
		t.Helper()
		var n [3]int64
		err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM pf_accounts.account_event),
    (SELECT count(*) FROM pf_accounts.role_assignment_event),
    (SELECT count(*) FROM pf_accounts.role_assignment_current)`).Scan(&n[0], &n[1], &n[2])
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := counts()
	path := "/v1/accounts/users/" + targetID.String()
	cases := []struct {
		name, method, path string
		body               any
	}{
		{"create", http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": "Không được tạo", "email": ids.New().String() + "@vi-du.vn", "assignments": []any{asg(port.KNXa, f.p91, []string{deptTT}, "30292")}}},
		{"grant", http.MethodPost, path + "/assignments", asg(port.KNXa, f.p91, []string{deptTT}, "30301")},
		{"change", http.MethodPut, path + "/assignments/" + assignment, asg(port.KNXa, f.p91, []string{deptTT}, "30301")},
		{"revoke", http.MethodPost, path + "/assignments/" + assignment + "/revoke", map[string]any{"reason": "Không được thu hồi"}},
		{"profile", http.MethodPatch, path + "/profile", map[string]any{"full_name": "Không được sửa"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.req(f.admin2, officer, tc.method, tc.path, tc.body, map[string]string{authz.ContextHeader: selected})
			wantCode(t, r, http.StatusForbidden, "forbidden", tc.name)
		})
	}
	if after := counts(); after != before {
		t.Fatalf("denied requests changed ledger/projection: %v -> %v", before, after)
	}
	afterUser, err := f.dir.GetUser(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	afterRoles, err := f.dir.RealmRoles(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeUser, afterUser) || !slices.Equal(beforeRoles, afterRoles) {
		t.Fatal("denied requests changed identity directory")
	}
	// A legitimate administrator still reads the target and MFA remains mandatory.
	wantCode(t, f.adminDo(f.accAdmin, http.MethodGet, path+"/assignments", nil), http.StatusOK, "", "native administrator")
	noMFA := f.tok(f.rootID, f.tenant, false, "platform_admin")
	wantCode(t, f.adminDo(noMFA, http.MethodGet, path+"/assignments", nil), http.StatusForbidden, "mfa_required", "administrator without MFA")
}
