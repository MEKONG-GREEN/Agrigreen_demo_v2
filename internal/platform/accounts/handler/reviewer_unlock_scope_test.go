package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Reviewer-only adversarial HTTP test: no production code or ledger mutation.
func TestReviewerUnlockReplayAfterCacheExpiryRechecksScope(t *testing.T) {
	f := newFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String() + "/unlock"
	f.dir.LockByBruteForce(id)
	body, err := json.Marshal(map[string]any{"reason": goodReason})
	if err != nil {
		t.Fatal(err)
	}
	idem := key()
	first := prov.raw(http.MethodPost, path, idem, "application/json", body)
	if first.status != http.StatusOK {
		t.Fatalf("authorized seed: %d %v", first.status, first.body)
	}
	// Same actor and realm role, now serving province92 rather than91.
	moved := caller{f: f, actor: prov.actor, token: f.iss.Sign(t, authztest.Token{
		Subject: prov.actor, TenantID: f.tenant, OrgRef: f.p92,
		Roles: []string{port.RoleAccountAdminProvince}, MFA: true,
	})}
	fresh := moved.raw(http.MethodPost, path, key(), "application/json", body)
	if fresh.status != http.StatusForbidden || fresh.body["code"] != "accounts.out_of_scope" {
		t.Fatalf("fresh scope control: %d %v", fresh.status, fresh.body)
	}
	cached := moved.raw(http.MethodPost, path, idem, "application/json", body)
	if cached.status == http.StatusOK {
		t.Fatalf("cache authorization binding control failed: %v", cached.body)
	}
	// Simulate normal cache retention expiry; durable account_event remains intact.
	tag, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(),
		"UPDATE shared.idempotency_key SET expires_at = now() - interval '1 hour' WHERE tenant_id=$1 AND idem_key=$2", f.tenant, idem)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("expire only cache row: affected=%d err=%v", tag.RowsAffected(), err)
	}
	replay := moved.raw(http.MethodPost, path, idem, "application/json", body)
	if replay.status == http.StatusOK {
		t.Fatalf("expired-cache replay bypassed current province scope: status=%d replayed=%v account=%v", replay.status, replay.body["replayed"], replay.body["account"])
	}
	if replay.status != http.StatusForbidden && replay.status != http.StatusNotFound && replay.status != http.StatusUnprocessableEntity {
		t.Fatalf("expected scope denial or key-conflict, got %d %v", replay.status, replay.body)
	}
}
