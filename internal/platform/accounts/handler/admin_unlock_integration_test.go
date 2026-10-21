package handler_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// Keycloak permanentLockout does not change enabled. The admin must see the
// runtime lock before deciding to call the existing audited unlock operation.
func TestAdminUnlockShowsRuntimeLoginLock(t *testing.T) {
	f := newFixture(t)
	admin := f.nationalAdmin()
	id := admin.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String()
	detail := admin.do(http.MethodGet, path, nil)
	if detail.status != http.StatusOK || detail.body["login_locked"] != false || detail.body["enabled"] != true {
		t.Fatalf("initial detail: %d %v", detail.status, detail.body)
	}
	f.dir.LockByBruteForce(id)
	detail = admin.do(http.MethodGet, path, nil)
	if detail.status != http.StatusOK || detail.body["login_locked"] != true || detail.body["enabled"] != true {
		t.Fatalf("runtime lock must not overwrite enabled: %d %v", detail.status, detail.body)
	}
	if r := admin.do(http.MethodPost, path+"/unlock", map[string]any{"reason": goodReason}); r.status != http.StatusOK {
		t.Fatalf("unlock: %d %v", r.status, r.body)
	}
	detail = admin.do(http.MethodGet, path, nil)
	if detail.status != http.StatusOK || detail.body["login_locked"] != false {
		t.Fatalf("after unlock: %d %v", detail.status, detail.body)
	}
}

func TestAdminLoginLockLookupFailsClosedAndStaysDetailOnly(t *testing.T) {
	f := newFixture(t)
	admin := f.nationalAdmin()
	id := admin.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String()
	f.dir.Fail("BruteForceLocked", errors.Join(port.ErrUnavailable, errors.New("synthetic attack detection unavailable")))
	if r := admin.do(http.MethodGet, path, nil); r.status != http.StatusServiceUnavailable || r.body["code"] != "accounts.dependency_unavailable" {
		t.Fatalf("unknown runtime lock cannot be reported as unlocked: %d %v", r.status, r.body)
	}
	// A list must not make per-account runtime lock calls, or imply false for an unqueried value.
	list := admin.do(http.MethodGet, "/v1/accounts/users", nil)
	if list.status != http.StatusOK {
		t.Fatalf("list: %d %v", list.status, list.body)
	}
	for _, item := range list.body["items"].([]any) {
		if _, exists := item.(map[string]any)["login_locked"]; exists {
			t.Fatal("list must omit unqueried runtime lock")
		}
	}
	// Scope authorization runs before the external lookup and does not disclose lock status.
	outside := f.as(f.tenant, f.p92, port.RoleAccountAdminProvince)
	if r := outside.do(http.MethodGet, path, nil); r.status != http.StatusForbidden || r.body["code"] != "accounts.out_of_scope" {
		t.Fatalf("foreign province: %d %v", r.status, r.body)
	}
}
