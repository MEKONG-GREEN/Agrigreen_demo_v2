// Package support274_test exercises the actual Application API, account session owner and HTTP vault on PostgreSQL.
package support274_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type response274 struct {
	status int
	raw    []byte
	body   map[string]any
	header http.Header
}
type fixture274 struct {
	t                                    *testing.T
	st                                   *harness.Stack
	tid, actor                           uuid.UUID
	admin, context, assignment, operator string
}

func call274(base, method, path, token, session, key string, body any) response274 {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if session != "" {
		req.Header.Set("X-AG-Context", session)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return response274{status: 0, raw: []byte(err.Error())}
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var bodyOut map[string]any
	_ = json.Unmarshal(raw, &bodyOut)
	return response274{status: res.StatusCode, raw: raw, body: bodyOut, header: res.Header}
}
func (f *fixture274) want(r response274, status int) {
	f.t.Helper()
	if r.status != status {
		f.t.Fatalf("want %d got %d %s", status, r.status, r.raw)
	}
}
func (f *fixture274) api(method, path, key string, body any) response274 {
	return call274(f.st.App.URL, method, path, f.admin, f.context, key, body)
}
func (f *fixture274) token(actor, tid uuid.UUID, mfa bool, roles ...string) string {
	return f.st.Issuer.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, MFA: mfa, Roles: roles, TTL: time.Hour})
}
func setup274(t *testing.T) *fixture274 {
	st := harness.New(t)
	f := &fixture274{t: t, st: st, tid: ids.New()}
	f.actor = st.Directory.Seed(accport.NewUser{Username: "support-admin-274", Email: "admin274@example.test", FirstName: "Admin", LastName: "274", TenantID: f.tid}, true, authz.RolePlatformAdmin, client.RoleRegistrar, client.RoleEvidenceViewer)
	operator := st.Directory.Seed(accport.NewUser{Username: "support-operator-274", Email: "operator274@example.test", FirstName: "Operator", LastName: "274", TenantID: f.tid}, true, authz.RolePlatformAdmin, client.RoleRegistrar)
	f.operator = f.token(operator, f.tid, true, authz.RolePlatformAdmin, client.RoleRegistrar)
	f.admin = f.token(f.actor, f.tid, true, authz.RolePlatformAdmin)
	grant := call274(st.Admin.URL, http.MethodPost, "/v1/accounts/users/"+f.actor.String()+"/assignments", f.operator, "", ids.New().String(), map[string]any{"role": "admin", "department_codes": []string{}, "commune_codes": []string{}})
	f.want(grant, 201)
	a := grant.body["assignment"].(map[string]any)
	f.assignment = a["assignment_id"].(string)
	selected := call274(st.App.URL, http.MethodPost, "/v1/me/context", f.admin, "", ids.New().String(), map[string]any{"assignment_id": f.assignment})
	f.want(selected, 201)
	f.context = selected.body["context_id"].(string)
	// This is the documented bootstrap: a real Admin token creates the vault subject; only its public data is configured.
	created := call274(st.Vault.URL, http.MethodPost, "/v1/subjects", f.operator, "", ids.New().String(), map[string]any{"purpose": "registry.admin", "kind": "person", "full_name": "Kho hỗ trợ tài khoản KN", "area_code": "kn_support"})
	f.want(created, 201)
	var keyData struct {
		Ref       uuid.UUID `json:"ref"`
		PublicKey []byte    `json:"public_key"`
	}
	if err := json.Unmarshal(created.raw, &keyData); err != nil {
		t.Fatal(err)
	}
	st.Mods.Contact.UseSupport(contact.SupportConfig{TenantID: f.tid, SubjectRef: keyData.Ref, PublicKey: keyData.PublicKey})
	return f
}
func supportInput274() map[string]any {
	return map[string]any{"name": "Fixture hỗ trợ 274", "contact": "support274@example.test", "message": "Tài khoản chưa có vai trò đang áp dụng <script>alert(274)</script>"}
}
func (f *fixture274) submit(key, token string) response274 {
	return call274(f.st.App.URL, http.MethodPost, "/v1/public/contacts/support-requests", token, "", key, supportInput274())
}
func (f *fixture274) first() map[string]any {
	r := f.api(http.MethodGet, "/v1/contacts/support-requests", "", nil)
	f.want(r, 200)
	items := r.body["items"].([]any)
	if len(items) != 1 {
		f.t.Fatalf("queue: %s", r.raw)
	}
	return items[0].(map[string]any)
}

func TestSupport274ActualPublicAdminVaultIsolation(t *testing.T) {
	f := setup274(t)
	key := ids.New().String()
	f.want(f.submit(key, ""), 202)
	// Lost ACK and retry, including a token with no assignment, cannot create a second record or expose an account.
	noAssignment := f.token(ids.New(), ids.New(), false, accport.RoleKNUser)
	r := f.submit(key, noAssignment)
	f.want(r, 202)
	if string(r.raw) != `{"status":"accepted"}` {
		t.Fatalf("public response %s", r.raw)
	}
	row := f.first()
	id := row["id"].(string)
	version := row["version"]
	if strings.Contains(string(mustJSON274(row)), "support274@example.test") {
		t.Fatal("PII in queue metadata")
	}
	detail := f.api(http.MethodGet, "/v1/contacts/support-requests/"+id, "", nil)
	f.want(detail, 200)
	if detail.body["message"] != supportInput274()["message"] || detail.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("detail: %s", detail.raw)
	}
	// Ordinary users and realm-only Admins cannot open the queue; a tenant cannot select another tenant in the request.
	plain := f.token(ids.New(), f.tid, false, accport.RoleKNUser)
	for _, path := range []string{"/v1/contacts/support-requests", "/v1/contacts/support-requests/" + id} {
		f.want(call274(f.st.App.URL, http.MethodGet, path, plain, "", "", nil), 403)
	}
	f.want(call274(f.st.App.URL, http.MethodGet, "/v1/contacts/support-requests", f.admin, "", "", nil), 428)
	foreign := f.token(f.actor, ids.New(), true, authz.RolePlatformAdmin, client.RoleEvidenceViewer)
	f.want(call274(f.st.App.URL, http.MethodGet, "/v1/contacts/support-requests/"+id, foreign, f.context, "", nil), 403)
	noMFA := f.token(f.actor, f.tid, false, authz.RolePlatformAdmin, client.RoleEvidenceViewer)
	f.want(call274(f.st.App.URL, http.MethodGet, "/v1/contacts/support-requests/"+id, noMFA, f.context, "", nil), 403)
	// Actual vault permission checks: a normal evidence viewer cannot unwrap with this purpose, even with MFA.
	var subject uuid.UUID
	var wrapped []byte
	if err := f.st.AdminPF().QueryRow(context.Background(), `SELECT subject_ref,wrapped_dek FROM pf_contact.support_request WHERE tenant_id=$1 AND id=$2`, f.tid, id).Scan(&subject, &wrapped); err != nil {
		t.Fatal(err)
	}
	f.want(call274(f.st.Vault.URL, http.MethodPost, "/v1/subjects/"+subject.String()+"/unwrap", f.token(ids.New(), f.tid, true, client.RoleEvidenceViewer), "", ids.New().String(), map[string]any{"purpose": "kn.account_support", "wrapped_dek": wrapped}), 403)
	// An existing evidence viewer cannot substitute a legacy purpose for the dedicated support purpose.
	f.want(call274(f.st.Vault.URL, http.MethodPost, "/v1/subjects/"+subject.String()+"/unwrap", f.token(ids.New(), f.tid, true, client.RoleEvidenceViewer), "", ids.New().String(), map[string]any{"purpose": "extension.field_support", "wrapped_dek": wrapped}), 403)
	resolveKey := ids.New().String()
	payload := map[string]any{"expected_version": version}
	f.want(f.api(http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", resolveKey, payload), 200)
	f.want(f.api(http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", resolveKey, payload), 200)
	f.want(f.api(http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", ids.New().String(), payload), 409)
	// Changing/removing the selected owner context cannot replay a previously authorized success.
	f.want(call274(f.st.App.URL, http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", f.admin, "", resolveKey, payload), 428)
	f.want(call274(f.st.App.URL, http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", f.admin, ids.New().String(), resolveKey, payload), 403)

	// Replaying a previously successful operation must recheck MFA/current account authority first.
	f.want(call274(f.st.App.URL, http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", noMFA, f.context, resolveKey, payload), 403)
	roleLost := f.token(f.actor, f.tid, true, client.RoleEvidenceViewer)
	f.want(call274(f.st.App.URL, http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", roleLost, f.context, resolveKey, payload), 403)
	f.want(call274(f.st.Admin.URL, http.MethodPost, "/v1/accounts/users/"+f.actor.String()+"/assignments/"+f.assignment+"/revoke", f.operator, "", ids.New().String(), map[string]any{"reason": "Thu hồi quyền của fixture hỗ trợ 274"}), 200)
	f.want(f.api(http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", resolveKey, payload), 403)
	var n, anon int
	var raw string
	if err := f.st.AdminPF().QueryRow(context.Background(), `SELECT count(*),string_agg(row_to_json(r)::text,'') FROM pf_contact.support_request r WHERE tenant_id=$1`, f.tid).Scan(&n, &raw); err != nil {
		t.Fatal(err)
	}
	if n != 1 || strings.Contains(raw, "support274@example.test") || strings.Contains(raw, "alert(274)") {
		t.Fatal("duplicate request or plaintext stored")
	}
	if err := f.st.AdminPF().QueryRow(context.Background(), `SELECT count(*) FROM pf_contact.support_event WHERE tenant_id=$1 AND event='submitted' AND actor_ref IS NULL`, f.tid).Scan(&anon); err != nil || anon != 1 {
		t.Fatalf("anonymous provenance %d %v", anon, err)
	}
	// The actual app role cannot mutate an immutable request; direct SQL remains tenant isolated.
	pool := db.ForModule(f.st.PF("app_platform"), db.Module("pf/contact"))
	if err := tenant.WithTenantTx(context.Background(), pool, f.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE pf_contact.support_request SET created_at=now() WHERE id=$1`, id)
		return err
	}); err == nil {
		t.Fatal("app role mutated immutable support")
	}
	foreignTenant := ids.New()
	if err := tenant.WithTenantTx(context.Background(), pool, foreignTenant, func(tx pgx.Tx) error {
		var visible int
		err := tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_contact.support_request WHERE id=$1`, id).Scan(&visible)
		if err == nil && visible != 0 {
			t.Fatal("direct module SQL leaked another tenant")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.st.Logs.String(), "support274@example.test") || strings.Contains(f.st.Logs.String(), "alert(274)") {
		t.Fatal("PII in logs")
	}
}

func mustJSON274(v any) []byte { b, _ := json.Marshal(v); return b }

func TestSupport274ConcurrentCommitCASAuditFailure(t *testing.T) {
	f := setup274(t)
	key := ids.New().String()
	var wg sync.WaitGroup
	results := make(chan response274, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.submit(key, "") }()
	}
	wg.Wait()
	close(results)
	for r := range results {
		f.want(r, 202)
	}
	row := f.first()
	id := row["id"].(string)
	version := row["version"]
	cas := make(chan response274, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cas <- f.api(http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", ids.New().String(), map[string]any{"expected_version": version})
		}()
	}
	wg.Wait()
	close(cas)
	statuses := map[int]int{}
	for r := range cas {
		statuses[r.status]++
	}
	if statuses[200] != 1 || statuses[409] != 1 {
		t.Fatalf("CAS: %v", statuses)
	}
	admin := f.st.AdminPF()
	ctx := context.Background()
	// Fail the actual append-only audit at DB boundary. No plaintext or false commit success may escape.
	_, err := admin.Exec(ctx, `CREATE FUNCTION pf_contact.reject_support274() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'support audit fixture failure'; END $$; CREATE TRIGGER reject_support274 BEFORE INSERT ON pf_contact.support_event FOR EACH ROW EXECUTE FUNCTION pf_contact.reject_support274()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, `DROP TRIGGER IF EXISTS reject_support274 ON pf_contact.support_event; DROP FUNCTION IF EXISTS pf_contact.reject_support274()`)
	})
	r := f.api(http.MethodGet, "/v1/contacts/support-requests/"+id, "", nil)
	f.want(r, 500)
	if strings.Contains(string(r.raw), "support274@example.test") {
		t.Fatal("plaintext escaped audit failure")
	}
	f.want(f.submit(ids.New().String(), ""), 500)
	var n int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pf_contact.support_request WHERE tenant_id=$1`, f.tid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("false commit: %d %v", n, err)
	}
	_, err = admin.Exec(ctx, `DROP TRIGGER reject_support274 ON pf_contact.support_event; DROP FUNCTION pf_contact.reject_support274()`)
	if err != nil {
		t.Fatal(err)
	}
	// Each chain verifies after the failed transaction rolled back.
	if err := tenant.WithTenantTx(ctx, f.st.PF("app_platform"), f.tid, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, `SELECT 1`); return e }); err != nil {
		t.Fatal(err)
	}
	// Verification uses the owner test pool; no product query crosses schemas.
	if err := tenant.WithTenantTx(ctx, admin, f.tid, func(tx pgx.Tx) error {
		v, e := ledger.Verify(ctx, tx, "pf_contact.support_event", f.tid, "support:"+id)
		if e == nil && !v.OK {
			t.Fatal("support audit chain invalid")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSupport274ActualVaultAuditFailure(t *testing.T) {
	f := setup274(t)
	f.want(f.submit(ids.New().String(), ""), 202)
	row := f.first()
	id := row["id"].(string)
	vault := f.st.Env.AdminPool(t, migrations.Vault)
	ctx := context.Background()
	_, err := vault.Exec(ctx, `CREATE FUNCTION vault.reject_support274() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.purpose='kn.account_support' THEN RAISE EXCEPTION 'vault support audit fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_support274 BEFORE INSERT ON vault.access_log FOR EACH ROW EXECUTE FUNCTION vault.reject_support274()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = vault.Exec(ctx, `DROP TRIGGER IF EXISTS reject_support274 ON vault.access_log; DROP FUNCTION IF EXISTS vault.reject_support274()`)
	})
	r := f.api(http.MethodGet, "/v1/contacts/support-requests/"+id, "", nil)
	if r.status < 500 || strings.Contains(string(r.raw), "support274@example.test") {
		t.Fatalf("vault audit failure: %d %s", r.status, r.raw)
	}
}

func TestSupport274RejectsCiphertextMovedToAnotherRequest(t *testing.T) {
	f := setup274(t)
	f.want(f.submit(ids.New().String(), ""), 202)
	f.want(f.submit(ids.New().String(), ""), 202)
	list := f.api(http.MethodGet, "/v1/contacts/support-requests", "", nil)
	f.want(list, 200)
	rows := list.body["items"].([]any)
	first := rows[0].(map[string]any)["id"].(string)
	second := rows[1].(map[string]any)["id"].(string)
	var fingerprints int
	if err := f.st.AdminPF().QueryRow(context.Background(), `SELECT count(DISTINCT payload_hash) FROM pf_contact.support_request WHERE tenant_id=$1`, f.tid).Scan(&fingerprints); err != nil || fingerprints != 2 {
		t.Fatalf("identical input must not have an unkeyed PII fingerprint: %d %v", fingerprints, err)
	}
	// Deliberately bypass immutable storage as a privileged test attacker; restore the trigger in the same transaction.
	admin := f.st.AdminPF()
	ctx := context.Background()
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `ALTER TABLE pf_contact.support_request DISABLE TRIGGER trg_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE pf_contact.support_request a SET (nonce,wrapped_dek,ciphertext)=(SELECT b.nonce,b.wrapped_dek,b.ciphertext FROM pf_contact.support_request b WHERE b.tenant_id=$1 AND b.id=$3) WHERE a.tenant_id=$1 AND a.id=$2`, f.tid, first, second); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE pf_contact.support_request ENABLE TRIGGER trg_immutable`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := f.api(http.MethodGet, "/v1/contacts/support-requests/"+first, "", nil)
	f.want(r, 500)
	if strings.Contains(string(r.raw), "support274@example.test") {
		t.Fatal("moved envelope returned plaintext")
	}
}

func TestSupport274ForeignAdminCannotListReadOrResolve(t *testing.T) {
	f := setup274(t)
	f.want(f.submit(ids.New().String(), ""), 202)
	row := f.first()
	id := row["id"].(string)
	foreignTenant := ids.New()
	operator := f.st.Directory.Seed(accport.NewUser{Username: "foreign-operator274", Email: "foreign-operator274@example.test", TenantID: foreignTenant}, true, authz.RolePlatformAdmin)
	actor := f.st.Directory.Seed(accport.NewUser{Username: "foreign-admin274", Email: "foreign-admin274@example.test", TenantID: foreignTenant}, true, authz.RolePlatformAdmin)
	token := f.token(actor, foreignTenant, true, authz.RolePlatformAdmin)
	granted := call274(f.st.Admin.URL, http.MethodPost, "/v1/accounts/users/"+actor.String()+"/assignments", f.token(operator, foreignTenant, true, authz.RolePlatformAdmin), "", ids.New().String(), map[string]any{"role": "admin", "department_codes": []string{}, "commune_codes": []string{}})
	f.want(granted, 201)
	assignment := granted.body["assignment"].(map[string]any)["assignment_id"].(string)
	selected := call274(f.st.App.URL, http.MethodPost, "/v1/me/context", token, "", ids.New().String(), map[string]any{"assignment_id": assignment})
	f.want(selected, 201)
	session := selected.body["context_id"].(string)
	list := call274(f.st.App.URL, http.MethodGet, "/v1/contacts/support-requests", token, session, "", nil)
	f.want(list, 200)
	if len(list.body["items"].([]any)) != 0 {
		t.Fatal("foreign Admin listed another tenant's requests")
	}
	f.want(call274(f.st.App.URL, http.MethodGet, "/v1/contacts/support-requests/"+id, token, session, "", nil), 404)
	f.want(call274(f.st.App.URL, http.MethodPut, "/v1/contacts/support-requests/"+id+"/resolution", token, session, ids.New().String(), map[string]any{"expected_version": row["version"]}), 404)
}

func TestSupport274VaultPurposeCannotOpenOtherModuleSubject(t *testing.T) {
	f := setup274(t)
	other := call274(f.st.Vault.URL, http.MethodPost, "/v1/subjects", f.operator, "", ids.New().String(), map[string]any{"purpose": "registry.admin", "kind": "person", "full_name": "Fixture subject khác", "area_code": "other_module274"})
	f.want(other, 201)
	var created struct {
		Ref       uuid.UUID `json:"ref"`
		PublicKey []byte    `json:"public_key"`
	}
	if err := json.Unmarshal(other.raw, &created); err != nil {
		t.Fatal(err)
	}
	pub, err := envelope.Key32(created.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := envelope.Seal([]byte("synthetic other module content"), pub)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/subjects/" + created.Ref.String() + "/unwrap"
	f.want(call274(f.st.Vault.URL, http.MethodPost, path, f.admin, "", ids.New().String(), map[string]any{"purpose": "kn.account_support", "wrapped_dek": sealed.WrappedDEK}), 403)
	// Existing evidence behavior for another module's subject remains intact.
	f.want(call274(f.st.Vault.URL, http.MethodPost, path, f.token(ids.New(), f.tid, true, client.RoleEvidenceViewer), "", ids.New().String(), map[string]any{"purpose": "extension.field_support", "wrapped_dek": sealed.WrappedDEK}), 200)
}

func TestSupport274NoAssignmentCanCreateIndependently(t *testing.T) {
	f := setup274(t)
	actor := f.st.Directory.Seed(accport.NewUser{Username: "no-assignment274", Email: "no-assignment274@example.test", TenantID: f.tid}, true, accport.RoleKNUser)
	f.want(f.submit(ids.New().String(), f.token(actor, f.tid, false, accport.RoleKNUser)), 202)
	row := f.first()
	if row["state"] != "new" {
		t.Fatalf("no assignment create: %v", row)
	}
}
