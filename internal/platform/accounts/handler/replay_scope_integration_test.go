package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// BF-ACC-REPLAY-SCOPE (#154): phát lại bền theo sổ account_event (sau khi dòng cache
// shared.idempotency_key hết hạn) phải kiểm lại phạm vi/quyền HIỆN HÀNH của người gọi và
// khớp thao tác + tài khoản đích; không trả ảnh chụp tài khoản ngoài phạm vi.

// expireCache giả lập dòng cache idempotency hết hạn bình thường; sổ account_event giữ nguyên.
func (f *fixture) expireCache(idem string) {
	f.t.Helper()
	tag, err := f.env.AdminPool(f.t, migrations.PF).Exec(context.Background(),
		"UPDATE shared.idempotency_key SET expires_at = now() - interval '1 hour' WHERE tenant_id=$1 AND idem_key=$2", f.tenant, idem)
	if err != nil || tag.RowsAffected() != 1 {
		f.t.Fatalf("hết hạn cache: affected=%d err=%v", tag.RowsAffected(), err)
	}
}

// movedTo là cùng actor/realm role nhưng token nay phục vụ tổ chức org.
func (f *fixture) movedTo(c caller, org uuid.UUID, role string) caller {
	return caller{f: f, actor: c.actor, token: f.iss.Sign(f.t, authztest.Token{
		Subject: c.actor, TenantID: f.tenant, OrgRef: org, Roles: []string{role}, MFA: true,
	})}
}

func reasonBody(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"reason": goodReason})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func denied(r resp) bool {
	return r.status == http.StatusForbidden || r.status == http.StatusNotFound || r.status == http.StatusUnprocessableEntity
}

func TestBFAccReplayScope_UnlockAfterCacheExpiryRechecksScope(t *testing.T) {
	f := newFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String() + "/unlock"
	f.dir.LockByBruteForce(id)
	body := reasonBody(t)
	idem := key()
	if r := prov.raw(http.MethodPost, path, idem, "application/json", body); r.status != http.StatusOK {
		t.Fatalf("mở khóa hợp lệ: %d %v", r.status, r.body)
	}
	moved := f.movedTo(prov, f.p92, port.RoleAccountAdminProvince)
	if r := moved.raw(http.MethodPost, path, key(), "application/json", body); r.status != http.StatusForbidden || r.body["code"] != "accounts.out_of_scope" {
		t.Fatalf("đối chứng khóa mới ngoài phạm vi: %d %v", r.status, r.body)
	}
	f.expireCache(idem)
	r := moved.raw(http.MethodPost, path, idem, "application/json", body)
	if r.status == http.StatusOK || !denied(r) {
		t.Fatalf("phát lại sau hết hạn cache bỏ qua phạm vi tỉnh: %d replayed=%v account=%v", r.status, r.body["replayed"], r.body["account"])
	}
	if _, leak := r.body["account"]; leak {
		t.Fatalf("lộ ảnh chụp tài khoản ngoài phạm vi: %v", r.body)
	}
}

func TestBFAccReplayScope_LockAfterCacheExpiryRechecksScope(t *testing.T) {
	f := newFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String() + "/lock"
	body := reasonBody(t)
	idem := key()
	if r := prov.raw(http.MethodPost, path, idem, "application/json", body); r.status != http.StatusOK {
		t.Fatalf("khóa hợp lệ: %d %v", r.status, r.body)
	}
	moved := f.movedTo(prov, f.p92, port.RoleAccountAdminProvince)
	f.expireCache(idem)
	r := moved.raw(http.MethodPost, path, idem, "application/json", body)
	if r.status == http.StatusOK || !denied(r) {
		t.Fatalf("phát lại khóa sau hết hạn cache bỏ qua phạm vi: %d %v", r.status, r.body)
	}
}

// Phát lại hợp lệ (cùng actor, phạm vi, đích, thân) sau hết hạn cache vẫn trả kết quả cũ, không ghi thêm sổ.
func TestBFAccReplayScope_SameCallerReplayStillIdempotent(t *testing.T) {
	f := newFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String() + "/unlock"
	f.dir.LockByBruteForce(id)
	body := reasonBody(t)
	idem := key()
	first := prov.raw(http.MethodPost, path, idem, "application/json", body)
	if first.status != http.StatusOK {
		t.Fatalf("mở khóa: %d %v", first.status, first.body)
	}
	before := len(f.events(f.tenant, id))
	f.expireCache(idem)
	again := prov.raw(http.MethodPost, path, idem, "application/json", body)
	if again.status != http.StatusOK || again.body["replayed"] != true {
		t.Fatalf("phát lại hợp lệ phải 200 replayed=true: %d %v", again.status, again.body)
	}
	if after := len(f.events(f.tenant, id)); after != before {
		t.Fatalf("phát lại không được ghi thêm sổ: %d → %d", before, after)
	}
}

// Cùng khóa nhưng tài khoản đích khác (cùng tỉnh) → 422 xung đột khóa, không trả kết quả của đích cũ.
func TestBFAccReplayScope_KeyReusedForOtherTargetConflicts(t *testing.T) {
	f := newFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	a := nat.create(person(f.team30292, "kn_officer"))
	b := nat.create(person(f.team30292, "kn_officer"))
	body := reasonBody(t)
	idem := key()
	if r := prov.raw(http.MethodPost, "/v1/accounts/users/"+a.String()+"/lock", idem, "application/json", body); r.status != http.StatusOK {
		t.Fatalf("khóa a: %d %v", r.status, r.body)
	}
	f.expireCache(idem)
	r := prov.raw(http.MethodPost, "/v1/accounts/users/"+b.String()+"/lock", idem, "application/json", body)
	if r.status != http.StatusUnprocessableEntity || r.body["code"] != "idempotency_key_reused" {
		t.Fatalf("khóa dùng lại cho đích khác phải 422 idempotency_key_reused: %d %v", r.status, r.body)
	}
	// Cùng khóa, cùng đích nhưng thao tác khác (mở khóa) → cũng là xung đột.
	r = prov.raw(http.MethodPost, "/v1/accounts/users/"+a.String()+"/unlock", idem, "application/json", body)
	if r.status != http.StatusUnprocessableEntity || r.body["code"] != "idempotency_key_reused" {
		t.Fatalf("khóa dùng lại cho thao tác khác phải 422: %d %v", r.status, r.body)
	}
}

// Actor khác (cùng tenant, cùng phạm vi) dùng lại khóa của người khác → 422, không nhận kết quả của người kia.
func TestBFAccReplayScope_OtherActorConflicts(t *testing.T) {
	f := newFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String() + "/lock"
	body := reasonBody(t)
	idem := key()
	if r := prov.raw(http.MethodPost, path, idem, "application/json", body); r.status != http.StatusOK {
		t.Fatalf("khóa: %d %v", r.status, r.body)
	}
	f.expireCache(idem)
	other := f.provinceAdmin()
	if r := other.raw(http.MethodPost, path, idem, "application/json", body); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("actor khác dùng lại khóa phải 422: %d %v", r.status, r.body)
	}
}
