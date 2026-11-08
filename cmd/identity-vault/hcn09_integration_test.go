package main

// #276: synthetic native current-state fixtures, real account owner middleware,
// production platform assembly and real vault HTTP/client (two separate PG databases).
// Local keys/signed test tokens prove this seam; they do not certify deployed OIDC/KMS.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHCN09NativeCurrentSession_276(t *testing.T) {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 11, 8, 5, 0, 0, 0, time.UTC))
	issuer := authztest.New(t)
	logs := &syncBuffer{}
	logger := telemetry.NewLogger("test", logs)
	vaultPool := env.SizedPool(t, migrations.Vault, identity.DBRole, 1)
	key, err := identity.NewLocalKMS("hcn09-276", bytes.Repeat([]byte{0x27}, 32))
	if err != nil {
		t.Fatal(err)
	}
	vm, err := identity.New(identity.Config{DB: vaultPool, Clock: clk, KMS: key, BlindKey: bytes.Repeat([]byte{0x76}, 32), RequireMFASensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	vaultServer := httptest.NewServer(NewRouter(Deps{Log: logger, Verifier: issuer.Verifier(), Module: vm, DB: vaultPool}))
	t.Cleanup(vaultServer.Close)
	mods, _, err := platformasm.Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.SizedPool(t, migrations.PF, role, 1), nil
	}, clk, platformasm.External{Vault: client.NewHTTP(vaultServer.URL, nil)})
	if err != nil {
		t.Fatal(err)
	}
	eng := platformasm.NewRouter(platformasm.Deps{Log: logger, Verifier: issuer.Verifier(), Ops: env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), Modules: mods.List()})
	server := httptest.NewServer(eng)
	t.Cleanup(server.Close)
	adminDB := env.AdminPool(t, migrations.PF)
	vaultDB := env.AdminPool(t, migrations.Vault)
	tid, actor, node, otherNode, unit := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := adminDB.Exec(context.Background(), query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Source projections are synthetic and private to this test's PG database.
	for _, x := range []struct{ code, level, parent string }{{"89", "province", ""}, {"30001", "commune", "89"}, {"30002", "commune", "89"}} {
		exec(`INSERT INTO pf_masterdata.admin_unit(tenant_id,code,valid_from,name,level,parent_code,source,actor_ref) VALUES($1,$2,'2020-01-01','Địa bàn kiểm thử',$3,nullif($4,''),'synthetic #276',$5)`, tid, x.code, x.level, x.parent, actor)
	}
	for _, x := range []struct {
		ref              uuid.UUID
		kind, code, area string
	}{{unit, "ttkn_province", "HCN276-P", "89"}, {node, "extension_team", "HCN276-A", "30001"}, {otherNode, "extension_team", "HCN276-B", "30002"}} {
		exec(`INSERT INTO pf_party.org(tenant_id,ref,kind,code,name,admin_unit_code,last_event_id,last_seq) VALUES($1,$2,$3,$4,'Nút kiểm thử',$5,$6,1)`, tid, x.ref, x.kind, x.code, x.area, ids.New())
	}
	for _, dept := range []string{"chan_nuoi_thuy_san", "trong_trot"} {
		exec(`INSERT INTO pf_party.department(tenant_id,id,org_ref,code,name,status,last_event_id,last_seq,updated_by) VALUES($1,$2,$3,$4,'Phòng kiểm thử','active',$5,1,$6)`, tid, ids.New(), unit, dept, ids.New(), actor)
	}
	exec(`INSERT INTO pf_masterdata.catalog_item(tenant_id,catalog,code,name,attrs,last_change_id,updated_by) VALUES($1,'cnts_species','GA','Gà','{"group":"gia_cam"}',$2,$3)`, tid, ids.New(), actor)
	type selected struct{ actor, assignment, id uuid.UUID }
	seed := func(role, dept, group string, org uuid.UUID, communes []string) selected {
		t.Helper()
		u := selected{ids.New(), ids.New(), ids.New()}
		ownerUnit := unit
		depts := []string{dept}
		if role == "admin" {
			ownerUnit = uuid.Nil
			depts = []string{}
		}
		exec(`INSERT INTO pf_accounts.role_assignment_current(tenant_id,assignment_id,user_ref,role,org_ref,unit_ref,department_codes,commune_codes,valid_from,active,last_event_id,last_seq,created_by,created_at,updated_by,updated_at)
          VALUES($1,$2,$3,$4,nullif($5,'00000000-0000-0000-0000-000000000000')::uuid,nullif($6,'00000000-0000-0000-0000-000000000000')::uuid,$7,$8,$9,true,$10,1,$3,$9,$3,$9)`, tid, u.assignment, u.actor, role, org.String(), ownerUnit.String(), depts, communes, clk.Now().Add(-time.Hour), ids.New())
		if group != "" {
			exec(`INSERT INTO pf_accounts.assignment_attr_current(tenant_id,assignment_id,user_ref,role,province_group,last_event_id,last_seq,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,1,$3,$7)`, tid, u.assignment, u.actor, role, group, ids.New(), clk.Now())
		}
		exec(`INSERT INTO pf_accounts.session_context(tenant_id,context_id,user_ref,assignment_id,role,department_code,org_ref,unit_ref,commune_codes,idem_key,created_at,expires_at)
          VALUES($1,$2,$3,$4,$5,nullif($6,''),nullif($7,'00000000-0000-0000-0000-000000000000')::uuid,nullif($8,'00000000-0000-0000-0000-000000000000')::uuid,$9,($2::uuid)::text,$10,$11)`, tid, u.id, u.actor, u.assignment, role, dept, org.String(), ownerUnit.String(), communes, clk.Now(), clk.Now().Add(time.Hour))
		return u
	}
	a := seed("admin", "", "", uuid.Nil, []string{})
	x := seed("kn_xa", "chan_nuoi_thuy_san", "", node, []string{"30001"})
	y := seed("kn_xa", "chan_nuoi_thuy_san", "", otherNode, []string{"30002"})
	v := seed("kn_tinh", "chan_nuoi_thuy_san", "vien_chuc", unit, []string{})
	b := seed("kn_tinh", "chan_nuoi_thuy_san", "ban_giam_doc", unit, []string{})
	th := seed("kn_tinh", "chan_nuoi_thuy_san", "tong_hop", unit, []string{})
	chu := seed("kn_tinh", "chan_nuoi_thuy_san", "chu_nhiem", unit, []string{})
	ld := seed("kn_tinh", "chan_nuoi_thuy_san", "lanh_dao_phong", unit, []string{})
	d := seed("kn_xa", "trong_trot", "", node, []string{"30001"})
	type response struct {
		status int
		body   map[string]any
		raw    string
		cache  string
	}
	request := func(u selected, tenantID uuid.UUID, mfa bool, roles []string, method, path, key string, body any) response {
		t.Helper()
		var rd io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, server.URL+path, rd)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: u.actor, TenantID: tenantID, OrgRef: unit, Roles: roles, MFA: mfa}))
		req.Header.Set(authz.ContextHeader, u.id.String())
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		raw, _ := io.ReadAll(res.Body)
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return response{res.StatusCode, out, string(raw), res.Header.Get("Cache-Control")}
	}
	roles := []string{"kn_user", client.RoleRegistrar}
	call := func(u selected, method, path, key string, body any) response {
		return request(u, tid, true, roles, method, path, key, body)
	}
	want := func(r response, status int) map[string]any {
		t.Helper()
		if r.status != status {
			t.Fatalf("native HTTP status=%d want=%d: %s", r.status, status, r.raw)
		}
		return r.body
	}
	const base = "/v1/extension/cnts/livestock-households"
	create := map[string]any{"owner_name": "Hộ kiểm thử An", "owner_phone": "0990000123", "commune_code": "30001", "hamlet": "Ấp kiểm thử", "livestock_type": "nong_ho", "animals": []any{map[string]any{"species_code": "GA", "head_count": 12}}, "checkin": map[string]any{"lat": 10.3, "lon": 105.4, "accuracy_m": "9", "mocked": false, "captured_at": clk.Now().Format(time.RFC3339)}}
	h := want(call(a, "POST", base, "hcn276-create", create), 201)
	path := base + "/" + h["id"].(string)
	update := map[string]any{"base_version": 1, "owner_name": "Hộ kiểm thử Bình", "owner_phone": "0991000456", "commune_code": "30001", "hamlet": "Ấp kiểm thử", "livestock_type": "nong_ho", "animals": create["animals"]}
	want(call(a, "POST", path, "hcn276-rename1", update), 200)
	update["base_version"], update["owner_name"], update["owner_phone"] = 2, "Hộ kiểm thử Cường", "0992000789"
	want(call(x, "POST", path, "hcn276-rename2", update), 200)
	history := func(m map[string]any, version float64) string {
		var values []string
		for _, raw := range m["history"].([]any) {
			r := raw.(map[string]any)
			if r["version"] != version {
				continue
			}
			for _, raw := range r["changes"].([]any) {
				c := raw.(map[string]any)
				values = append(values, c["field"].(string)+":"+c["old"].(string)+"→"+c["new"].(string))
			}
		}
		return strings.Join(values, "|")
	}
	const exact = "owner_name:Hộ kiểm thử An→Hộ kiểm thử Bình|owner_phone:0990•••123→0991•••456"
	for _, u := range []selected{a, x, v, b, th, chu, ld} {
		r := call(u, "GET", path, "", nil)
		got := want(r, 200)
		if history(got, 2) != exact || r.cache != "no-store" {
			t.Fatal("native HCN09 revision/phone/no-store mismatch")
		}
		if strings.Contains(r.raw, "0990000123") || strings.Contains(r.raw, "0991000456") || strings.Contains(r.raw, "0992000789") {
			t.Fatal("native plaintext phone leaked")
		}
	}
	if got := want(call(b, "GET", path, "", nil), 200); got["can_edit"] != false || got["owner_name"] != nil || history(got, 2) != exact {
		t.Fatal("read-only cohort lost HCN09 read or gained write")
	}
	want(call(b, "POST", path, "hcn276-readonly", update), 403)
	// Each failure is checked before any new full-name lookup; warmed history must not replay.
	countAudit := func() int {
		t.Helper()
		var n int
		if err := vaultDB.QueryRow(context.Background(), `SELECT count(*) FROM vault.access_log WHERE tenant_id=$1 AND op='resolve'`, tid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// The metadata replay must not persist plaintext names outside the vault or bypass an audit.
	var cachedBody string
	if err := adminDB.QueryRow(context.Background(), `SELECT convert_from(response_body,'UTF8') FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key='hcn276-rename2'`, tid).Scan(&cachedBody); err != nil {
		t.Fatal(err)
	}
	for _, pii := range []string{"Hộ kiểm thử An", "Hộ kiểm thử Bình", "Hộ kiểm thử Cường", "0990000123", "0991000456", "0992000789"} {
		if strings.Contains(cachedBody, pii) {
			t.Fatal("household idempotency cache stored plaintext names or phones")
		}
	}
	beforeReplay := countAudit()
	replayed := want(call(x, "POST", path, "hcn276-rename2", update), 200)
	if replayed["owner_name"] != nil || strings.Contains(history(replayed, 2), "Hộ kiểm thử") || !strings.Contains(history(replayed, 2), "không đọc được lịch sử định danh") || countAudit() != beforeReplay {
		t.Fatal("metadata replay leaked or freshly resolved names")
	}
	if got := want(call(a, "GET", path, "", nil), 200); history(got, 2) != exact || countAudit() <= beforeReplay {
		t.Fatal("fresh detail did not audit and resolve names after metadata replay")
	}
	deny := func(name string, fn func() response, status int) {
		t.Helper()
		before := countAudit()
		r := fn()
		want(r, status)
		if countAudit() != before || strings.Contains(r.raw, "Hộ kiểm thử") {
			t.Fatalf("%s reached vault or leaked history", name)
		}
	}
	deny("wrong node", func() response { return call(y, "GET", path, "", nil) }, 404)
	deny("wrong department", func() response { return call(d, "GET", path, "", nil) }, 403)
	deny("wrong tenant", func() response { return request(x, ids.New(), true, roles, "GET", path, "", nil) }, 403)
	wrongActor := x
	wrongActor.actor = ids.New()
	deny("wrong actor", func() response { return call(wrongActor, "GET", path, "", nil) }, 403)
	exec(`UPDATE pf_accounts.role_assignment_current SET active=false WHERE tenant_id=$1 AND assignment_id=$2`, tid, x.assignment)
	deny("revoked current assignment", func() response { return call(x, "GET", path, "", nil) }, 403)
	deny("revoked cached commune mutation", func() response { return call(x, "POST", path, "hcn276-rename2", update) }, 403)
	exec(`UPDATE pf_accounts.role_assignment_current SET active=true WHERE tenant_id=$1 AND assignment_id=$2`, tid, x.assignment)
	exec(`UPDATE pf_party.org SET active=false WHERE tenant_id=$1 AND ref=$2`, tid, node)
	deny("inactive current node", func() response { return call(x, "GET", path, "", nil) }, 404)
	exec(`UPDATE pf_party.org SET active=true WHERE tenant_id=$1 AND ref=$2`, tid, node)
	exec(`UPDATE pf_party.department SET status='suspended' WHERE tenant_id=$1 AND org_ref=$2 AND code='chan_nuoi_thuy_san'`, tid, unit)
	deny("suspended current department", func() response { return call(x, "GET", path, "", nil) }, 403)
	exec(`UPDATE pf_party.department SET status='active' WHERE tenant_id=$1 AND org_ref=$2 AND code='chan_nuoi_thuy_san'`, tid, unit)
	exec(`UPDATE pf_accounts.role_assignment_current SET commune_codes=ARRAY['30002'] WHERE tenant_id=$1 AND assignment_id=$2`, tid, x.assignment)
	deny("narrowed current scope", func() response { return call(x, "GET", path, "", nil) }, 404)
	deny("narrowed scope cannot replay old mutation", func() response { return call(x, "POST", path, "hcn276-rename2", update) }, 422)
	exec(`UPDATE pf_accounts.role_assignment_current SET commune_codes=ARRAY['30001'] WHERE tenant_id=$1 AND assignment_id=$2`, tid, x.assignment)
	exec(`UPDATE pf_accounts.role_assignment_current SET active=false WHERE tenant_id=$1 AND assignment_id=$2`, tid, a.assignment)
	deny("revoked cached mutation", func() response { return call(a, "POST", base, "hcn276-create", create) }, 403)
	exec(`UPDATE pf_accounts.role_assignment_current SET active=true WHERE tenant_id=$1 AND assignment_id=$2`, tid, a.assignment)
	// Vault capability is separately enforced; no realm-role fabrication by the KN service.
	unavailable := func(r response) {
		t.Helper()
		got := want(r, 200)
		c := history(got, 2)
		if strings.Contains(c, "Hộ kiểm thử") || !strings.Contains(c, "không đọc được lịch sử định danh") || !strings.Contains(c, "0990•••123→0991•••456") {
			t.Fatal("failed Resolve did not remain explicitly unavailable")
		}
	}
	unavailable(request(v, tid, false, roles, "GET", path, "", nil))
	unavailable(request(v, tid, true, []string{"kn_user", client.RoleDisplay}, "GET", path, "", nil))
	// Actual audit storage fails through both HTTP services; partial plaintext must not reach KN.
	fn := fmt.Sprintf(`CREATE FUNCTION vault.hcn09_native_audit276() RETURNS trigger LANGUAGE plpgsql AS $f$ BEGIN
      IF NEW.tenant_id='%s'::uuid AND NEW.op='resolve' THEN RAISE EXCEPTION 'synthetic required audit unavailable'; END IF;
      RETURN NEW; END $f$`, tid)
	if _, err := vaultDB.Exec(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
	if _, err := vaultDB.Exec(context.Background(), `CREATE TRIGGER hcn09_native_audit276 BEFORE INSERT ON vault.access_log FOR EACH ROW EXECUTE FUNCTION vault.hcn09_native_audit276()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = vaultDB.Exec(context.Background(), `DROP TRIGGER IF EXISTS hcn09_native_audit276 ON vault.access_log; DROP FUNCTION IF EXISTS vault.hcn09_native_audit276()`)
	})
	beforeFailure := countAudit()
	unavailable(call(v, "GET", path, "", nil))
	if countAudit() != beforeFailure {
		t.Fatal("failed native audit committed an OK entry")
	}
	if _, err := vaultDB.Exec(context.Background(), `DROP TRIGGER hcn09_native_audit276 ON vault.access_log; DROP FUNCTION vault.hcn09_native_audit276()`); err != nil {
		t.Fatal(err)
	}
	// Reuse the same sized pools after denial and confirm the genuine old version still survives.
	if got := want(call(x, "GET", path, "", nil), 200); history(got, 2) != exact {
		t.Fatal("authorization/RLS state leaked across pool reuse")
	}
	clk.Advance(2 * time.Hour)
	deny("expired session", func() response { return call(x, "GET", path, "", nil) }, 403)
	clk.Advance(-2 * time.Hour)
	for _, pii := range []string{"Hộ kiểm thử An", "Hộ kiểm thử Bình", "Hộ kiểm thử Cường", "0990000123", "0991000456", "0992000789"} {
		if strings.Contains(logs.String(), pii) {
			t.Fatal("PII leaked to native HTTP logs")
		}
	}
}
