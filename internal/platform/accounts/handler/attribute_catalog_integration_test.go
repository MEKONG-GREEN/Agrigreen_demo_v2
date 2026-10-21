package handler_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func TestAttributeCatalogHTTPLabelsContextAndLiveGuards(t *testing.T) {
	legacy := port.LegacyAttributeCatalog()
	levels := legacy.Levels()
	for i := range levels {
		if levels[i].Code == port.MgmtTruongPhong {
			levels[i].Label = "Chức danh thử nghiệm"
			levels[i].Manager = false
		}
	}
	catalog, err := port.NewAttributeCatalog(port.AttributeCatalogSpec{Version: "owner/test", Levels: levels,
		Groups: []port.AttributeGroup{{Code: "restricted", Label: "Nhóm chỉ xem", Manager: true, ReadsAllDepartments: true, ReadOnly: true}},
		Duties: []port.CatalogItem{{Code: "owner_duty", Label: "Nhiệm vụ thử nghiệm"}}, Roles: legacy.Roles()})
	if err != nil {
		t.Fatal(err)
	}
	f := newKNFixtureWithCatalog(t, catalog)
	r := f.adminDo(f.root, http.MethodGet, "/v1/accounts/mgmt-levels", nil)
	if r.status != http.StatusOK || r.body["version"] != "owner/test" || r.body["provisional"] != false {
		t.Fatalf("configured metadata: %d %v", r.status, r.body)
	}
	found := false
	for _, raw := range r.body["items"].([]any) {
		row := raw.(map[string]any)
		if row["code"] == port.MgmtTruongPhong {
			found = row["label"] == "Chức danh thử nghiệm" && row["manager"] == false
		}
	}
	if !found {
		t.Fatal("management labels/flags did not use configured catalog")
	}
	groups := r.body["province_groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["code"] != "restricted" {
		t.Fatalf("groups fell back: %v", groups)
	}
	duties := r.body["duties"].([]any)
	if len(duties) != 1 || duties[0].(map[string]any)["code"] != "owner_duty" {
		t.Fatalf("duties fell back: %v", duties)
	}

	id, user, created := f.createKN("Cán bộ kiểm danh mục", asgLevel(port.KNTinh, f.p91, []string{deptTT}, port.MgmtTruongPhong))
	assignment := asgIDs(created)[port.KNTinh]
	before := f.ctxID(user, assignment, deptTT)
	if k := f.probeKN(user, before).KN; k.Manager || k.ReadOnly() {
		t.Fatalf("title inferred privileges: %+v", k)
	}
	listed := f.req(f.app, user, http.MethodGet, "/v1/me/contexts", nil, nil)
	assignments := listed.body["assignments"].([]any)
	if len(assignments) != 1 || assignments[0].(map[string]any)["mgmt_level"] != "Chức danh thử nghiệm" {
		t.Fatalf("assignment label: %v", listed.body)
	}
	wantCode(t, f.setAttrs(id, assignment, map[string]any{"province_group": port.GroupBanGiamDoc}), http.StatusUnprocessableEntity, "validation_failed", "configured group membership")
	wantCode(t, f.setAttrs(id, assignment, map[string]any{"duties": []string{authz.DutyPriceAdmin}}), http.StatusUnprocessableEntity, "validation_failed", "configured duty membership")
	r = f.setAttrs(id, assignment, map[string]any{"province_group": "restricted", "duties": []string{"owner_duty"}})
	if r.status != http.StatusOK || str(r.body, "attributes", "province_group_label") != "Nhóm chỉ xem" {
		t.Fatalf("configured attributes: %d %v", r.status, r.body)
	}
	// Existing contexts must re-read the current attribute ledger on every request.
	if k := f.probeKN(user, before).KN; !k.Manager || !k.ReadOnly() || !k.HasDuty("owner_duty") || k.Can(authz.PowerApprove) || len(k.Powers) != 0 {
		t.Fatalf("live read-only capability: %+v", k)
	}
	wantCode(t, f.withCtx(user, http.MethodPost, "/v1/probe/x/write", before, map[string]any{}), http.StatusForbidden, "accounts.context_read_only", "own-department write")
	cross := f.ctxID(user, assignment, deptBVTV)
	if k := f.probeKN(user, cross).KN; !k.ReadOnly() {
		t.Fatal("cross-department access became writable")
	}
	wantCode(t, f.withCtx(user, http.MethodPost, "/v1/probe/x/write", cross, map[string]any{}), http.StatusForbidden, "accounts.context_read_only", "cross-department write")
	// Current organization data remains authoritative even when a catalog grants read-all.
	f.depts.setStatus(f.p91, deptBVTV, partyport.DeptNotOpen)
	wantCode(t, f.withCtx(user, http.MethodGet, "/v1/probe", cross, nil), http.StatusForbidden, "accounts.context_revoked", "retired department")
	r = f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+id.String()+"/assignments/"+assignment+"/revoke", map[string]any{"reason": "Kết thúc lần gán thử nghiệm"})
	if r.status != http.StatusOK {
		t.Fatalf("revoke: %d %v", r.status, r.body)
	}
	wantCode(t, f.withCtx(user, http.MethodGet, "/v1/probe", before, nil), http.StatusForbidden, "accounts.context_revoked", "revoked assignment")
}
