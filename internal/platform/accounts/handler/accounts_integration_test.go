package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	admin  *httptest.Server
	dir    *keycloak.Memory
	party  *fakeParty
	mod    *accounts.Module
	pool   *pgxpool.Pool
	tenant uuid.UUID
	// Cây tổ chức của tenant mặc định (mã ĐVHC chính thức 2025).
	national, p91, team30292, coop30292, coop30301, p92, coop31120 uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, accounts.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	dir := keycloak.NewMemory()
	pty := newFakeParty()
	mod := accounts.New(pool, clock.System{}, accounts.Deps{Directory: dir, Scope: pty, Orgs: pty})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	f := &fixture{t: t, env: env, iss: iss, admin: srv, dir: dir, party: pty, mod: mod, pool: pool, tenant: ids.New()}
	f.national = pty.add(f.tenant, partyport.OrgTTKNNational, "")
	f.p91 = pty.add(f.tenant, partyport.OrgTTKNProvince, "91")
	f.team30292 = pty.add(f.tenant, partyport.OrgExtensionTeam, "30292")
	f.coop30292 = pty.add(f.tenant, partyport.OrgCooperative, "30292")
	f.coop30301 = pty.add(f.tenant, partyport.OrgCooperative, "30301")
	f.p92 = pty.add(f.tenant, partyport.OrgTTKNProvince, "92")
	f.coop31120 = pty.add(f.tenant, partyport.OrgCooperative, "31120")
	return f
}

type caller struct {
	f     *fixture
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})}
}

func (f *fixture) nationalAdmin() caller { return f.as(f.tenant, f.national, port.RoleAccountAdmin) }
func (f *fixture) provinceAdmin() caller { return f.as(f.tenant, f.p91, port.RoleAccountAdminProvince) }

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (c caller) raw(method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, c.f.admin.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", ctype)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) do(method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	idem := ""
	if method == http.MethodPost {
		idem = key()
	}
	return c.raw(method, path, idem, "application/json", b)
}

func str(m map[string]any, path ...string) string {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	s, _ := cur.(string)
	return s
}

var seq int

func person(org uuid.UUID, roles ...string) map[string]any {
	seq++
	n := ids.New().String()[24:]
	return map[string]any{"email": "cb-" + n + "@vi-du.vn", "full_name": "Nguyễn Văn An", "org_ref": org.String(), "roles": roles}
}

// create tạo tài khoản và trả id Keycloak.
func (c caller) create(body map[string]any) uuid.UUID {
	c.f.t.Helper()
	r := c.do(http.MethodPost, "/v1/accounts/users", body)
	if r.status != http.StatusCreated {
		c.f.t.Fatalf("tạo tài khoản: %d %v", r.status, r.body)
	}
	return uuid.MustParse(str(r.body, "user", "id"))
}

func (f *fixture) events(tid, user uuid.UUID) []string {
	f.t.Helper()
	var out []string
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), "SELECT action || ':' || coalesce(lock_cause, '') FROM pf_accounts.account_event WHERE user_ref = $1 ORDER BY seq", user)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

const goodReason = "Cán bộ nghỉ việc theo quyết định số 12/QĐ"

// KN US-ADM-01, HTX US-ACC-01, CGH FN-01: tạo tài khoản — tenant từ token, org_ref
// trong phạm vi, required actions, liên kết một lần (không mật khẩu tạm, ADR-0010).
func TestCreateUserUSADM01(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	body := person(f.team30292, "kn_officer")
	body["phone"] = "0912 345 678"
	r := nat.do(http.MethodPost, "/v1/accounts/users", body)
	if r.status != http.StatusCreated || r.body["activation_email_sent"] != true {
		t.Fatalf("tạo: %d %v", r.status, r.body)
	}
	id := uuid.MustParse(str(r.body, "user", "id"))
	u, err := f.dir.GetUser(context.Background(), id)
	if err != nil || u.TenantID != f.tenant || u.OrgRef != f.team30292 || u.Phone != "0912345678" || u.FirstName != "An" || u.LastName != "Nguyễn Văn" {
		t.Fatalf("thuộc tính Keycloak: %+v %v", u.ID, err)
	}
	if !slices.Equal(u.RequiredActions, []string{port.ActionVerifyEmail, port.ActionUpdatePassword}) {
		t.Fatalf("required actions: %v", u.RequiredActions)
	}
	sent := f.dir.Sent()
	if len(sent) != 1 || sent[0].UserID != id || sent[0].Lifespan != 72*time.Hour || !slices.Equal(sent[0].Actions, u.RequiredActions) {
		t.Fatalf("liên kết kích hoạt: %+v", sent)
	}
	if got := f.events(f.tenant, id); !slices.Equal(got, []string{"created:", "actions_sent:"}) {
		t.Fatalf("sổ: %v", got)
	}
	// Vai [MFA] → CONFIGURE_TOTP.
	mfaID := nat.create(person(f.national, "registry_admin"))
	if u, _ := f.dir.GetUser(context.Background(), mfaID); !slices.Contains(u.RequiredActions, port.ActionConfigureTOTP) {
		t.Fatalf("vai [MFA] phải bắt TOTP: %v", u.RequiredActions)
	}
	// Trùng email / SĐT.
	dup := person(f.team30292, "kn_officer")
	dup["email"] = strings.ToUpper(body["email"].(string))
	if r := nat.do(http.MethodPost, "/v1/accounts/users", dup); r.status != http.StatusConflict || r.body["code"] != "accounts.user_exists" {
		t.Fatalf("trùng email: %d %v", r.status, r.body)
	}
	dupPhone := person(f.team30292, "kn_officer")
	dupPhone["phone"] = "0912345678"
	if r := nat.do(http.MethodPost, "/v1/accounts/users", dupPhone); r.status != http.StatusConflict || r.body["code"] != "accounts.phone_taken" {
		t.Fatalf("trùng SĐT: %d %v", r.status, r.body)
	}
	// HTX US-ACC-01 AC-7: quản lý HTX phải gắn HTX.
	if r := nat.do(http.MethodPost, "/v1/accounts/users", person(f.team30292, "coop_manager")); r.status != http.StatusUnprocessableEntity ||
		!strings.Contains(string(mustJSON(r.body["errors"])), "Vui lòng chọn HTX liên kết.") {
		t.Fatalf("coop_manager ngoài HTX: %d %v", r.status, r.body)
	}
	// Email lỗi → tài khoản vẫn tạo, báo chưa gửi; gửi lại được.
	f.dir.Fail("ExecuteActionsEmail", port.ErrEmailFailed)
	r = nat.do(http.MethodPost, "/v1/accounts/users", person(f.coop30292, "coop_manager"))
	f.dir.Fail("ExecuteActionsEmail", nil)
	if r.status != http.StatusCreated || r.body["activation_email_sent"] != false {
		t.Fatalf("email lỗi: %d %v", r.status, r.body)
	}
	coopID := str(r.body, "user", "id")
	if r := nat.do(http.MethodPost, "/v1/accounts/users/"+coopID+"/resend-actions", nil); r.status != http.StatusOK || r.body["activation_email_sent"] != true {
		t.Fatalf("gửi lại: %d %v", r.status, r.body)
	}
	// TTKN tỉnh không mở được quản lý tài khoản (KN US-ADM-01 AC-4).
	if r := f.as(f.tenant, f.p91, "kn_province_admin").do(http.MethodGet, "/v1/accounts/users", nil); r.status != http.StatusForbidden {
		t.Fatalf("kn_province_admin: %d", r.status)
	}
	// Thiếu MFA → chặn ở bề mặt Admin.
	noMFA := caller{f: f, token: f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{port.RoleAccountAdmin}, OrgRef: f.national})}
	if r := noMFA.do(http.MethodGet, "/v1/accounts/users", nil); r.status != http.StatusForbidden || r.body["code"] != "mfa_required" {
		t.Fatalf("thiếu MFA: %d %v", r.status, r.body)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu, không tạo thêm người dùng.
func TestIdempotentCreate(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	b, _ := json.Marshal(person(f.team30292, "kn_officer"))
	k := key()
	r1 := nat.raw(http.MethodPost, "/v1/accounts/users", k, "application/json", b)
	r2 := nat.raw(http.MethodPost, "/v1/accounts/users", k, "application/json", b)
	if r1.status != http.StatusCreated || r2.status != http.StatusCreated || r2.hdr.Get(idempotency.ReplayedHeader) != "true" || str(r1.body, "user", "id") != str(r2.body, "user", "id") {
		t.Fatalf("gọi lặp: %d %d %v", r1.status, r2.status, r2.hdr)
	}
	users, _ := f.dir.FindUsers(context.Background(), port.Query{Attributes: map[string]string{port.AttrTenantID: f.tenant.String()}})
	if len(users) != 1 || len(f.dir.Sent()) != 1 {
		t.Fatalf("chỉ một người dùng và một email: %d %d", len(users), len(f.dir.Sent()))
	}
	// Khóa lặp cũng trả phản hồi cũ.
	lb, _ := json.Marshal(map[string]any{"reason": goodReason})
	lk := key()
	path := "/v1/accounts/users/" + str(r1.body, "user", "id") + "/lock"
	l1 := nat.raw(http.MethodPost, path, lk, "application/json", lb)
	l2 := nat.raw(http.MethodPost, path, lk, "application/json", lb)
	if l1.status != http.StatusOK || l2.status != http.StatusOK || l2.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("khóa lặp: %d %d", l1.status, l2.status)
	}
}

// Ma trận cấp vai: quản trị tỉnh chỉ cấp kn_officer/coop_manager trong tỉnh của mình.
func TestProvinceAdminGrantMatrix(t *testing.T) {
	f := newFixture(t)
	prov := f.provinceAdmin()
	if id := prov.create(person(f.team30292, "kn_officer")); id == uuid.Nil {
		t.Fatal("tạo kn_officer trong tỉnh")
	}
	prov.create(person(f.coop30301, "coop_manager"))
	if r := prov.do(http.MethodPost, "/v1/accounts/users", person(f.national, "kn_national_admin")); r.status != http.StatusForbidden || r.body["code"] != "accounts.role_not_grantable" {
		t.Fatalf("cấp kn_national_admin: %d %v", r.status, r.body)
	}
	if r := prov.do(http.MethodPost, "/v1/accounts/users", person(f.p91, "kn_province_admin")); r.status != http.StatusForbidden || r.body["code"] != "accounts.role_not_grantable" {
		t.Fatalf("cấp kn_province_admin: %d %v", r.status, r.body)
	}
	if r := prov.do(http.MethodPost, "/v1/accounts/users", person(f.coop31120, "coop_manager")); r.status != http.StatusForbidden || r.body["code"] != "accounts.out_of_scope" {
		t.Fatalf("tổ chức tỉnh khác: %d %v", r.status, r.body)
	}
	if r := prov.do(http.MethodPost, "/v1/accounts/users", person(uuid.New(), "kn_officer")); r.status != http.StatusUnprocessableEntity || r.body["code"] != "accounts.org_invalid" {
		t.Fatalf("tổ chức không có: %d %v", r.status, r.body)
	}
	nat := f.nationalAdmin()
	boss := nat.create(person(f.p91, "kn_province_admin"))
	other := nat.create(person(f.coop31120, "coop_manager"))
	// Không thao tác trên tài khoản có vai vượt quyền, hay ngoài tỉnh.
	if r := prov.do(http.MethodPost, "/v1/accounts/users/"+boss.String()+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusForbidden || r.body["code"] != "accounts.role_not_grantable" {
		t.Fatalf("khóa cán bộ TTKN tỉnh: %d %v", r.status, r.body)
	}
	if r := prov.do(http.MethodGet, "/v1/accounts/users/"+other.String(), nil); r.status != http.StatusForbidden || r.body["code"] != "accounts.out_of_scope" {
		t.Fatalf("đọc tài khoản tỉnh khác: %d %v", r.status, r.body)
	}
	if r := prov.do(http.MethodPost, "/v1/accounts/users/"+other.String()+"/assignment", map[string]any{"org_ref": f.coop30301.String()}); r.status != http.StatusForbidden {
		t.Fatalf("chuyển tài khoản tỉnh khác về tỉnh mình: %d %v", r.status, r.body)
	}
	// Danh sách chỉ gồm người trong tỉnh 91, phân trang bằng con trỏ.
	seen := map[string]bool{}
	cursor := ""
	for range 10 {
		path := "/v1/accounts/users?limit=1"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		r := prov.do(http.MethodGet, path, nil)
		if r.status != http.StatusOK {
			t.Fatalf("danh sách: %d %v", r.status, r.body)
		}
		for _, it := range r.body["items"].([]any) {
			m := it.(map[string]any)
			seen[m["id"].(string)] = true
			if m["org_ref"] == f.coop31120.String() {
				t.Fatalf("danh sách lộ tài khoản tỉnh 92")
			}
		}
		next, _ := r.body["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 3 || !seen[boss.String()] {
		t.Fatalf("tỉnh 91 phải thấy 3 tài khoản (kể cả cán bộ TTKN tỉnh chỉ để xem): %v", seen)
	}
	grants := prov.do(http.MethodGet, "/v1/accounts/grants", nil)
	if g := grants.body["grantable"].([]any); len(g) != 2 {
		t.Fatalf("grants: %v", grants.body)
	}
}

// Khóa/mở khóa cần lý do ≥ 20 ký tự; khóa thu hồi phiên (KN US-ADM-01 AC-3).
func TestLockUnlockNeedsReason(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String()
	if r := nat.do(http.MethodPost, path+"/lock", map[string]any{"reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn: %d %v", r.status, r.body)
	}
	if r := nat.do(http.MethodPost, path+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusOK || str(r.body, "account", "lock_cause") != "manual" {
		t.Fatalf("khóa: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(context.Background(), id); u.Enabled || f.dir.Logouts(id) != 1 {
		t.Fatalf("Keycloak: enabled=%v logouts=%d", u.Enabled, f.dir.Logouts(id))
	}
	if r := nat.do(http.MethodPost, path+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusConflict || r.body["code"] != "accounts.state_conflict" {
		t.Fatalf("khóa lại: %d %v", r.status, r.body)
	}
	if r := nat.do(http.MethodPost, path+"/resend-actions", nil); r.status != http.StatusConflict {
		t.Fatalf("gửi liên kết khi đang khóa: %d", r.status)
	}
	if r := nat.do(http.MethodPost, path+"/unlock", map[string]any{"reason": "ok"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("mở khóa thiếu lý do: %d", r.status)
	}
	if r := nat.do(http.MethodPost, path+"/unlock", map[string]any{"reason": "Đã xác minh, cán bộ quay lại công tác"}); r.status != http.StatusOK || r.body["account"].(map[string]any)["enabled"] != true {
		t.Fatalf("mở khóa: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(context.Background(), id); !u.Enabled {
		t.Fatal("Keycloak phải bật lại")
	}
	// Không tự khóa mình.
	self := f.nationalAdmin()
	if r := self.do(http.MethodPost, "/v1/accounts/users/"+self.actor.String()+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusForbidden || r.body["code"] != "accounts.self_action" {
		t.Fatalf("tự khóa: %d %v", r.status, r.body)
	}
	ev := nat.do(http.MethodGet, path+"/events", nil)
	if items := ev.body["items"].([]any); len(items) != 4 {
		t.Fatalf("nhật ký: %v", ev.body)
	}
	// Sổ chỉ ghi thêm (US-HTXSTATUS-03 AC-2).
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE pf_accounts.account_event SET reason = 'sửa lý do cho đủ hai mươi ký tự' WHERE user_ref = $1", id)
		return err
	})
	if err == nil {
		t.Fatal("sổ account_event phải chặn UPDATE")
	}
	var vr ledger.VerifyResult
	_ = tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		vr, err = ledger.Verify(context.Background(), tx, "pf_accounts.account_event", f.tenant, "user:"+id.String())
		return err
	})
	if !vr.OK || vr.RowsChecked != 4 {
		t.Fatalf("chuỗi băm: %+v %v", vr, err)
	}
}

// CGH FN-01 luồng ngoại lệ "không khóa được Admin cuối cùng đang hoạt động": tài khoản
// quản trị nền tảng chỉ quản trị nền tảng thao tác được, và không ai tự khóa mình — nên
// luôn còn ít nhất một quản trị nền tảng đang hoạt động (người đang gọi).
func TestPlatformAdminCannotBeLockedByLowerAdminOrSelf(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.dir.Seed(port.NewUser{Username: "qtnt-1", Email: "qtnt-1@vi-du.vn", TenantID: f.tenant, OrgRef: f.national}, true)
	if err := f.dir.AddRealmRoles(ctx, root, []string{"platform_admin"}); err != nil {
		t.Fatal(err)
	}
	path := "/v1/accounts/users/" + root.String() + "/lock"
	for name, c := range map[string]caller{"quản trị tài khoản QG": f.nationalAdmin(), "quản trị tài khoản tỉnh": f.provinceAdmin()} {
		if r := c.do(http.MethodPost, path, map[string]any{"reason": goodReason}); r.status != http.StatusForbidden {
			t.Fatalf("%s khóa quản trị nền tảng: %d %v", name, r.status, r.body)
		}
	}
	// Chính quản trị nền tảng đó (token có sub = id Keycloak) không tự khóa được.
	self := caller{f: f, actor: root, token: f.iss.Sign(t, authztest.Token{Subject: root, TenantID: f.tenant, Roles: []string{"platform_admin"}, OrgRef: f.national, MFA: true})}
	if r := self.do(http.MethodPost, path, map[string]any{"reason": goodReason}); r.status != http.StatusForbidden || r.body["code"] != "accounts.self_action" {
		t.Fatalf("tự khóa: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(ctx, root); !u.Enabled {
		t.Fatal("quản trị nền tảng phải còn hoạt động")
	}
	// Có quản trị nền tảng thứ hai thì người này khóa được người kia (không phải người cuối cùng).
	second := f.as(f.tenant, f.national, "platform_admin")
	if r := second.do(http.MethodPost, path, map[string]any{"reason": goodReason}); r.status != http.StatusOK {
		t.Fatalf("quản trị nền tảng khác khóa: %d %v", r.status, r.body)
	}
}

func orgEvent(f *fixture, typ string, org uuid.UUID) outbox.Envelope {
	var payload any = partyport.OrgDeactivatedV1{OrgRef: org, Kind: "cooperative", Code: "HTX000001", AdminUnitCode: "30292", Reason: "HTX giải thể theo nghị quyết đại hội", ActorRef: ids.New(), EventTime: time.Now().UTC().Format(time.RFC3339), Seq: 2}
	if typ == partyport.EventOrgReactivated {
		payload = partyport.OrgReactivatedV1{OrgRef: org, Kind: "cooperative", Code: "HTX000001", AdminUnitCode: "30292", ActorRef: ids.New(), EventTime: time.Now().UTC().Format(time.RFC3339), Seq: 3}
	}
	b, _ := json.Marshal(payload)
	return outbox.Envelope{EventID: ids.New(), EventType: typ, SchemaVersion: 1, TenantID: f.tenant, OccurredAt: time.Now().UTC(), Producer: "pf/party", Payload: b}
}

// HTX US-HTXSTATUS-01 AC-2 / US-HTXSTATUS-02 AC-2: vô hiệu hóa HTX khóa mọi tài khoản
// của HTX đó; kích hoạt lại chỉ mở tài khoản bị khóa VÌ vô hiệu hóa. Idempotent qua inbox.
func TestOrgDeactivationLocksOnlyThatOrgAndReactivationRestoresOnlyThose(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	u1 := nat.create(person(f.coop30292, "coop_manager"))
	u2 := nat.create(person(f.coop30292, "plot_editor"))
	manual := nat.create(person(f.coop30292, "coop_manager"))
	if r := nat.do(http.MethodPost, "/v1/accounts/users/"+manual.String()+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusOK {
		t.Fatalf("khóa thủ công: %d", r.status)
	}
	otherOrg := nat.create(person(f.coop30301, "coop_manager"))
	// Người dùng tạo ngoài nền tảng (console Keycloak) cùng HTX; và người của tenant khác cùng org_ref.
	external := f.dir.Seed(port.NewUser{Username: "ngoai-nen-tang", Email: "ngoai@vi-du.vn", TenantID: f.tenant, OrgRef: f.coop30292}, true, "coop_manager")
	foreign := f.dir.Seed(port.NewUser{Username: "tenant-khac", Email: "khac@vi-du.vn", TenantID: ids.New(), OrgRef: f.coop30292}, true)

	h := f.mod.OrgStatusHandler()
	ctx := context.Background()
	deact := orgEvent(f, partyport.EventOrgDeactivated, f.coop30292)
	if ok, err := inbox.Process(ctx, f.pool, port.OrgStatusConsumer, deact, h); err != nil || !ok {
		t.Fatalf("xử lý deactivated: %v %v", ok, err)
	}
	f.party.setActive(f.tenant, f.coop30292, false)
	enabled := func(id uuid.UUID) bool { u, _ := f.dir.GetUser(ctx, id); return u.Enabled }
	for _, id := range []uuid.UUID{u1, u2, external} {
		if enabled(id) {
			t.Fatalf("%s phải bị khóa", id)
		}
		if ev := f.events(f.tenant, id); ev[len(ev)-1] != "locked:org_deactivated" {
			t.Fatalf("sổ %s: %v", id, ev)
		}
	}
	if !enabled(otherOrg) || !enabled(foreign) {
		t.Fatal("chỉ khóa tài khoản của đúng HTX trong đúng tenant")
	}
	if ev := f.events(f.tenant, manual); ev[len(ev)-1] != "locked:manual" {
		t.Fatalf("tài khoản khóa thủ công giữ nguyên nguyên nhân: %v", ev)
	}
	if f.dir.Logouts(u1) != 1 {
		t.Fatal("khóa theo HTX phải thu hồi phiên")
	}
	// Giao lặp cùng event_id → không xử lý lại.
	if ok, err := inbox.Process(ctx, f.pool, port.OrgStatusConsumer, deact, h); err != nil || ok {
		t.Fatalf("giao lặp: %v %v", ok, err)
	}
	if n := len(f.events(f.tenant, u1)); n != 3 {
		t.Fatalf("giao lặp không được ghi thêm: %d", n)
	}
	// HTX đang vô hiệu hóa → không mở khóa thủ công được.
	if r := nat.do(http.MethodPost, "/v1/accounts/users/"+u1.String()+"/unlock", map[string]any{"reason": goodReason}); r.status != http.StatusConflict || r.body["code"] != "accounts.org_inactive" {
		t.Fatalf("mở khóa khi HTX vô hiệu: %d %v", r.status, r.body)
	}
	// Không tạo tài khoản mới vào HTX đã vô hiệu hóa.
	if r := nat.do(http.MethodPost, "/v1/accounts/users", person(f.coop30292, "coop_manager")); r.status != http.StatusConflict || r.body["code"] != "accounts.org_inactive" {
		t.Fatalf("tạo vào HTX vô hiệu: %d %v", r.status, r.body)
	}

	f.party.setActive(f.tenant, f.coop30292, true)
	react := orgEvent(f, partyport.EventOrgReactivated, f.coop30292)
	if ok, err := inbox.Process(ctx, f.pool, port.OrgStatusConsumer, react, h); err != nil || !ok {
		t.Fatalf("xử lý reactivated: %v %v", ok, err)
	}
	for _, id := range []uuid.UUID{u1, u2, external} {
		if !enabled(id) {
			t.Fatalf("%s phải được mở lại", id)
		}
	}
	if enabled(manual) {
		t.Fatal("tài khoản khóa vì lý do khác không được mở nhầm (US-HTXSTATUS-02 AC-2)")
	}
	var outboxN int
	_ = tenant.WithTenantTx(ctx, f.env.AdminPool(t, migrations.PF), f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type IN ($2, $3)", f.tenant, port.EventUserLocked, port.EventUserUnlocked).Scan(&outboxN)
	})
	if outboxN != 1+3+3 {
		t.Fatalf("sự kiện locked/unlocked: %d", outboxN)
	}
}

func xlsxBody(t *testing.T, rows [][]any) []byte {
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	for i, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := x.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := x.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// KN US-ADM-02, HTX US-ACC-02 AC-2: nhập XLSX có dòng lỗi lẫn dòng hợp lệ.
func TestImportXLSXMixedRows(t *testing.T) {
	f := newFixture(t)
	prov := f.provinceAdmin()
	existing := person(f.team30292, "kn_officer")
	existing["phone"] = "0987000111"
	prov.create(existing)
	team, coop, far := f.team30292.String(), f.coop30292.String(), f.coop31120.String()
	body := xlsxBody(t, [][]any{
		{"Họ tên", "Email", "SĐT", "org_ref", "Vai trò"},
		{"Trần Thị Bình", "binh@vi-du.vn", "0987000222", team, "kn_officer"},      // 2 hợp lệ
		{"Lê Văn Cường", "", "0987000333", team, "kn_officer"},                    // 3 thiếu email
		{"Phạm Dũng", "sai-email", "", team, "kn_officer"},                        // 4 sai email
		{"Võ Em", "em@vi-du.vn", "", team, "kn_admin_tu_che"},                     // 5 sai vai
		{"Đỗ Giang", "BINH@vi-du.vn", "", team, "kn_officer"},                     // 6 trùng dòng 2
		{"Hồ Hạnh", "hanh@vi-du.vn", "0987000111", team, "kn_officer"},            // 7 SĐT đã có
		{"Ngô Ích", "ich@vi-du.vn", "", far, "coop_manager"},                      // 8 ngoài tỉnh
		{"Mai Kha", "kha@vi-du.vn", "", team, "kn_national_admin"},                // 9 vượt quyền
		{"Lý Lan", "lan@vi-du.vn", "0987000444", coop, "coop_manager;kn_officer"}, // 10 kn_officer cần tổ KN
		{"Bùi Minh", "minh@vi-du.vn", "", coop, "coop_manager"},                   // 11 hợp lệ
	})
	const xlsx = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	k := key()
	r := prov.raw(http.MethodPost, "/v1/accounts/users/import", k, xlsx, body)
	if r.status != http.StatusOK || r.body["created"] != float64(2) || r.body["rejected"] != float64(8) {
		t.Fatalf("nhập: %d %v", r.status, r.body)
	}
	want := map[int]string{
		3: "Thiếu Email", 4: "Sai định dạng Email", 5: "Sai định dạng vai trò", 6: "Trùng với dòng 2 trong tệp",
		7: "SĐT đã tồn tại", 8: "Tổ chức ngoài phạm vi phụ trách", 9: "Vai trò vượt quyền cấp của bạn",
	}
	for _, it := range r.body["rows"].([]any) {
		m := it.(map[string]any)
		row := int(m["row"].(float64))
		switch row {
		case 2, 11:
			if m["status"] != "created" || m["user_id"] == nil || m["activation_email_sent"] != true {
				t.Errorf("dòng %d: %v", row, m)
			}
		default:
			if m["status"] != "rejected" || (want[row] != "" && m["reason"] != want[row]) {
				t.Errorf("dòng %d: %v", row, m)
			}
		}
		if reason, _ := m["reason"].(string); strings.Contains(reason, "@") || strings.Contains(reason, "0987") {
			t.Errorf("lý do dòng %d chứa dữ liệu cá nhân: %s", row, reason)
		}
	}
	// Gửi lại cùng khóa → phản hồi cũ, không tạo thêm.
	again := prov.raw(http.MethodPost, "/v1/accounts/users/import", k, xlsx, body)
	users, _ := f.dir.FindUsers(context.Background(), port.Query{Attributes: map[string]string{port.AttrTenantID: f.tenant.String()}})
	if again.status != http.StatusOK || again.hdr.Get(idempotency.ReplayedHeader) != "true" || len(users) != 3 {
		t.Fatalf("nhập lặp: %d %d", again.status, len(users))
	}
	// CSV cùng mẫu; tệp thiếu cột bắt buộc bị từ chối cả tệp.
	csv := "ho_ten,email,org_ref,vai_tro\nTô Nga,nga@vi-du.vn," + team + ",kn_officer\n"
	if r := prov.raw(http.MethodPost, "/v1/accounts/users/import", key(), "text/csv", []byte(csv)); r.status != http.StatusOK || r.body["created"] != float64(1) {
		t.Fatalf("CSV: %d %v", r.status, r.body)
	}
	if r := prov.raw(http.MethodPost, "/v1/accounts/users/import", key(), "text/csv", []byte("ho_ten,email\nA,a@vi-du.vn\n")); r.status != http.StatusUnprocessableEntity || r.body["code"] != "accounts.import_invalid" {
		t.Fatalf("thiếu cột: %d %v", r.status, r.body)
	}
	if r := prov.raw(http.MethodPost, "/v1/accounts/users/import", key(), xlsx, []byte("không phải xlsx")); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("xlsx hỏng: %d", r.status)
	}
}

// Cách ly tenant: tài khoản của tenant khác coi như không tồn tại (R-16).
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	id := f.nationalAdmin().create(person(f.team30292, "kn_officer"))
	other := f.as(ids.New(), uuid.Nil, authz.RolePlatformAdmin)
	if r := other.do(http.MethodGet, "/v1/accounts/users/"+id.String(), nil); r.status != http.StatusNotFound || r.body["code"] != "accounts.user_not_found" {
		t.Fatalf("đọc chéo tenant: %d %v", r.status, r.body)
	}
	if r := other.do(http.MethodPost, "/v1/accounts/users/"+id.String()+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusNotFound {
		t.Fatalf("khóa chéo tenant: %d", r.status)
	}
	if r := other.do(http.MethodGet, "/v1/accounts/users", nil); r.status != http.StatusOK || len(r.body["items"].([]any)) != 0 {
		t.Fatalf("danh sách chéo tenant: %v", r.body)
	}
	// Tổ chức của tenant khác không gán được.
	if r := other.do(http.MethodPost, "/v1/accounts/users", person(f.team30292, "kn_officer")); r.status != http.StatusUnprocessableEntity || r.body["code"] != "accounts.org_invalid" {
		t.Fatalf("org của tenant khác: %d %v", r.status, r.body)
	}
	var n int
	_ = tenant.WithTenantTx(context.Background(), f.pool, uuid.New(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_accounts.account").Scan(&n)
	})
	if n != 0 {
		t.Fatalf("RLS: tenant khác thấy %d dòng", n)
	}
}

// KN US-ADM-03: thao tác loạt, kết quả theo từng tài khoản.
func TestBulkActions(t *testing.T) {
	f := newFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	a := nat.create(person(f.team30292, "kn_officer"))
	b := nat.create(person(f.coop30301, "coop_manager"))
	far := nat.create(person(f.coop31120, "coop_manager"))
	r := prov.do(http.MethodPost, "/v1/accounts/users/bulk", map[string]any{"action": "lock", "reason": goodReason, "user_ids": []string{a.String(), b.String(), far.String()}})
	if r.status != http.StatusOK || r.body["succeeded"] != float64(2) || r.body["failed"] != float64(1) {
		t.Fatalf("khóa loạt: %d %v", r.status, r.body)
	}
	if items := r.body["items"].([]any); items[2].(map[string]any)["code"] != "accounts.out_of_scope" {
		t.Fatalf("mục ngoài phạm vi: %v", items[2])
	}
	r = prov.do(http.MethodPost, "/v1/accounts/users/bulk", map[string]any{"action": "unlock", "reason": goodReason, "user_ids": []string{a.String(), b.String()}})
	if r.body["succeeded"] != float64(2) {
		t.Fatalf("mở loạt: %v", r.body)
	}
	sentBefore := len(f.dir.Sent())
	r = prov.do(http.MethodPost, "/v1/accounts/users/bulk", map[string]any{"action": "resend", "user_ids": []string{a.String(), b.String()}})
	if r.body["succeeded"] != float64(2) || len(f.dir.Sent()) != sentBefore+2 {
		t.Fatalf("gửi lại loạt: %v", r.body)
	}
	if r := prov.do(http.MethodPost, "/v1/accounts/users/bulk", map[string]any{"action": "xoa", "user_ids": []string{a.String()}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("hành động lạ: %d", r.status)
	}
}

// HTX US-ACC-01 AC-2: đổi đơn vị/vai áp dụng ngay; thêm vai [MFA] bắt cấu hình TOTP.
func TestAssignmentChange(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String() + "/assignment"
	r := nat.do(http.MethodPost, path, map[string]any{"org_ref": f.p91.String(), "roles": []string{"kn_province_admin", "registry_admin"}})
	if r.status != http.StatusOK || str(r.body, "account", "org_ref") != f.p91.String() {
		t.Fatalf("đổi: %d %v", r.status, r.body)
	}
	ctx := context.Background()
	u, _ := f.dir.GetUser(ctx, id)
	roles, _ := f.dir.RealmRoles(ctx, id)
	if u.OrgRef != f.p91 || !slices.Equal(roles, []string{"kn_province_admin", "registry_admin"}) || !slices.Contains(u.RequiredActions, port.ActionConfigureTOTP) {
		t.Fatalf("Keycloak: org=%v roles=%v actions=%v", u.OrgRef, roles, u.RequiredActions)
	}
	if f.dir.Logouts(id) != 0 {
		t.Fatal("đổi vai không bắt đăng nhập lại")
	}
	if r := nat.do(http.MethodPost, path, map[string]any{"roles": []string{"kn_officer"}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("kn_officer ở TTKN tỉnh: %d %v", r.status, r.body)
	}
	if r := f.provinceAdmin().do(http.MethodPost, path, map[string]any{"roles": []string{"kn_officer"}, "org_ref": f.team30292.String()}); r.status != http.StatusForbidden {
		t.Fatalf("quản trị tỉnh hạ vai cán bộ TTKN tỉnh: %d", r.status)
	}
}

// R-04 cho nhân sự: pf_accounts không có cột dữ liệu cá nhân.
func TestNoPIIColumns(t *testing.T) {
	f := newFixture(t)
	rows, err := f.env.AdminPool(t, migrations.PF).Query(context.Background(),
		"SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = 'pf_accounts'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	forbidden := []string{"name", "email", "phone", "mail", "username", "login", "password", "national_id", "address", "birth"}
	tables := map[string]bool{}
	for rows.Next() {
		var tbl, col string
		if err := rows.Scan(&tbl, &col); err != nil {
			t.Fatal(err)
		}
		tables[tbl] = true
		for _, bad := range forbidden {
			if strings.Contains(col, bad) {
				t.Errorf("pf_accounts.%s.%s trông như dữ liệu cá nhân", tbl, col)
			}
		}
	}
	if !tables["account"] || !tables["account_event"] {
		t.Fatalf("thiếu bảng: %v", tables)
	}
}

// Chưa cấu hình Keycloak → 503 có mã riêng.
func TestNoDirectoryIs503(t *testing.T) {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	mod := accounts.New(env.Pool(t, migrations.PF, accounts.DBRole), clock.System{}, accounts.Deps{})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA()))
	srv := httptest.NewServer(eng)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/accounts/users", nil)
	req.Header.Set("Authorization", "Bearer "+iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true}))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	if res.StatusCode != http.StatusServiceUnavailable || m["code"] != "accounts.dependency_unavailable" {
		t.Fatalf("%d %v", res.StatusCode, m)
	}
}

// GIS US-LOG-01 AC-3: sai mật khẩu 5 lần/15 phút → Keycloak khóa VĨNH VIỄN (realm
// permanentLockout, enabled vẫn true) tới khi Admin mở: API mở khóa nhận ra khóa chống dò,
// xóa trạng thái khóa ở Keycloak, ghi sự kiện mở khóa (prev_cause = brute_force) có lý do.
func TestUnlockClearsBruteForceLockout_GIS_US_LOG_01(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String()
	// Chưa bị khóa → mở khóa là xung đột trạng thái.
	if r := nat.do(http.MethodPost, path+"/unlock", map[string]any{"reason": "Mở thử khi đang hoạt động"}); r.status != http.StatusConflict {
		t.Fatalf("mở khóa tài khoản đang hoạt động: %d %v", r.status, r.body)
	}
	f.dir.LockByBruteForce(id)
	if locked, _ := f.dir.BruteForceLocked(context.Background(), id); !locked {
		t.Fatal("giả lập khóa chống dò")
	}
	if r := nat.do(http.MethodPost, path+"/unlock", map[string]any{"reason": "ok"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("mở khóa thiếu lý do: %d", r.status)
	}
	r := nat.do(http.MethodPost, path+"/unlock", map[string]any{"reason": "Người dùng đã xác minh danh tính qua điện thoại"})
	if r.status != http.StatusOK || r.body["account"].(map[string]any)["enabled"] != true {
		t.Fatalf("mở khóa: %d %v", r.status, r.body)
	}
	if locked, _ := f.dir.BruteForceLocked(context.Background(), id); locked {
		t.Fatal("Keycloak phải xóa trạng thái khóa chống dò")
	}
	ev := nat.do(http.MethodGet, path+"/events", nil)
	items := ev.body["items"].([]any)
	last := items[0].(map[string]any)
	if last["action"] != "unlocked" {
		last = items[len(items)-1].(map[string]any)
	}
	if last["action"] != "unlocked" || !strings.Contains(fmt.Sprint(last), "brute_force") {
		t.Fatalf("sự kiện mở khóa phải ghi nguyên nhân brute_force: %v", items)
	}
}
