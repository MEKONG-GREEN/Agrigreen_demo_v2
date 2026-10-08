package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// fakeHouseholds thực hiện party/port.Households (pf/party thật có kiểm thử riêng).
type fakeHouseholds struct {
	mu sync.Mutex
	m  map[uuid.UUID]map[uuid.UUID]partyport.HouseholdMembership // tenant → ref
}

func (f *fakeHouseholds) add(tid uuid.UUID, m partyport.HouseholdMembership) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m[tid] == nil {
		f.m[tid] = map[uuid.UUID]partyport.HouseholdMembership{}
	}
	f.m[tid][m.Ref] = m
}

func (f *fakeHouseholds) Membership(ctx context.Context, ref uuid.UUID) (partyport.HouseholdMembership, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return partyport.HouseholdMembership{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.m[tid][ref]
	if !ok {
		return m, partyport.ErrHouseholdNotFound
	}
	return m, nil
}

type fixedParams map[string]string

func (p fixedParams) At(_ context.Context, key string, at time.Time) (params.ParamValue, error) {
	v, ok := p[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Value: decimal.RequireFromString(v), Version: 1, EffectiveFrom: at}, nil
}

// syncBuffer gom log của engine để kiểm token không bao giờ vào log.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type farmerFixture struct {
	t                                                          *testing.T
	env                                                        *pgtest.Env
	iss                                                        *authztest.FakeIssuer
	srv                                                        *httptest.Server
	dir                                                        *keycloak.Memory
	party                                                      *fakeParty
	hh                                                         *fakeHouseholds
	vault                                                      *clienttest.Fake
	mod                                                        *accounts.Module
	pool                                                       *pgxpool.Pool
	logs                                                       *syncBuffer
	tenant                                                     uuid.UUID
	p91, team30292, team31120, coop30292, coop30301, coop31120 uuid.UUID
}

func newFarmerFixture(t *testing.T) *farmerFixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, accounts.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	f := &farmerFixture{t: t, env: env, iss: iss, dir: keycloak.NewMemory(), party: newFakeParty(),
		hh: &fakeHouseholds{m: map[uuid.UUID]map[uuid.UUID]partyport.HouseholdMembership{}}, vault: clienttest.New(),
		pool: pool, logs: &syncBuffer{}, tenant: ids.New()}
	f.mod = accounts.New(pool, clock.System{}, accounts.Deps{Directory: f.dir, Scope: f.party, Orgs: f.party,
		Households: f.hh, Vault: f.vault, Params: fixedParams{mdport.ParamActivationGrantLifespan: "48"}})
	eng := httpx.NewEngine(telemetry.NewLogger("test", f.logs), 1<<20)
	f.mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	f.p91 = f.party.add(f.tenant, partyport.OrgTTKNProvince, "91")
	f.team30292 = f.party.add(f.tenant, partyport.OrgExtensionTeam, "30292")
	f.team31120 = f.party.add(f.tenant, partyport.OrgExtensionTeam, "31120")
	f.coop30292 = f.party.add(f.tenant, partyport.OrgCooperative, "30292")
	f.coop30301 = f.party.add(f.tenant, partyport.OrgCooperative, "30301")
	f.coop31120 = f.party.add(f.tenant, partyport.OrgCooperative, "31120")
	return f
}

// household tạo chủ hộ trong kho định danh giả (chỉ mục mù SĐT) và thành viên HTX.
func (f *farmerFixture) household(tid, coop uuid.UUID, commune, phone string) uuid.UUID {
	f.t.Helper()
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, ActorRef: ids.New()})
	c, err := f.vault.Create(ctx, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold, FullName: "Trần Thị Bé", Phone: phone, AreaCode: commune})
	if err != nil {
		f.t.Fatal(err)
	}
	f.hh.add(tid, partyport.HouseholdMembership{Ref: c.Ref, CooperativeRef: coop, CommuneCode: commune, Active: true})
	return c.Ref
}

type fcaller struct {
	f     *farmerFixture
	token string
	actor uuid.UUID
}

func (f *farmerFixture) as(tid, org uuid.UUID, roles ...string) fcaller {
	actor := ids.New()
	return fcaller{f: f, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org})}
}

func (f *farmerFixture) asUser(tid, user uuid.UUID, roles ...string) fcaller {
	return fcaller{f: f, actor: user, token: f.iss.Sign(f.t, authztest.Token{Subject: user, TenantID: tid, Roles: roles})}
}

func (c fcaller) raw(method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, c.f.srv.URL+path, rd)
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

func (c fcaller) post(path, idem string, body any) resp {
	b, _ := json.Marshal(body)
	return c.raw(http.MethodPost, path, idem, "application/json", b)
}

func (c fcaller) get(path string) resp { return c.raw(http.MethodGet, path, "", "", nil) }

// HTX US-ACC-01, US-LOGIN-01: quản lý HTX tạo tài khoản nông dân cho nông hộ của HTX
// mình — tên đăng nhập là SĐT chuẩn hóa, vai farmer, không org_ref, không mật khẩu;
// không tạo được cho HTX khác; cán bộ KN theo phạm vi xã; SĐT phải khớp nông hộ.
func TestFarmerAccountCreateScopeHTXUSACC01(t *testing.T) {
	f := newFarmerFixture(t)
	own := f.household(f.tenant, f.coop30292, "30292", "0912000001")
	other := f.household(f.tenant, f.coop30301, "30301", "0912000002")
	far := f.household(f.tenant, f.coop31120, "31120", "0912000003")
	mgr := f.as(f.tenant, f.coop30292, partyport.RoleCoopManager)

	r := mgr.post("/v1/farmer-accounts", key(), map[string]any{"household_ref": own.String(), "phone": "+84 912 000 001", "full_name": "Trần Thị Bé"})
	if r.status != http.StatusCreated || r.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("tạo: %d %v", r.status, r.body)
	}
	user := uuid.MustParse(str(r.body, "farmer", "user_ref"))
	link := str(r.body, "activation", "link")
	if link == "" || str(r.body, "activation", "qr", "data") != link || str(r.body, "activation", "status") != "active" {
		t.Fatalf("lượt kích hoạt: %v", r.body)
	}
	u, err := f.dir.GetUser(context.Background(), user)
	if err != nil || u.Username != "0912000001" || u.Phone != "0912000001" || u.OrgRef != uuid.Nil || u.Email != "" ||
		!slices.Equal(u.RequiredActions, []string{port.ActionUpdatePassword}) {
		t.Fatalf("Keycloak: %+v %v", u.RequiredActions, err)
	}
	if roles, _ := f.dir.RealmRoles(context.Background(), user); !slices.Equal(roles, []string{port.RoleFarmer}) {
		t.Fatalf("vai: %v", roles)
	}
	if len(f.dir.Sent()) != 0 {
		t.Fatal("không được gửi email kích hoạt cho nông dân")
	}
	issued := f.dir.Links()
	if len(issued) != 1 || issued[0].Lifespan != 48*time.Hour || issued[0].GrantID.String() != str(r.body, "activation", "grant_id") {
		t.Fatalf("liên kết Keycloak: %+v", issued)
	}
	// Cổng FarmerHouseholds cho cropcycle/extension.
	ctx := tenant.WithID(context.Background(), f.tenant)
	links, err := f.mod.FarmerHouseholds().HouseholdsForSubject(ctx, user)
	if err != nil || len(links) != 1 || links[0].HouseholdRef != own || links[0].CooperativeRef != f.coop30292 || links[0].CommuneCode != "30292" {
		t.Fatalf("HouseholdsForSubject: %+v %v", links, err)
	}
	if other, _ := f.mod.FarmerHouseholds().HouseholdsForSubject(tenant.WithID(context.Background(), ids.New()), user); len(other) != 0 {
		t.Fatal("tenant khác không thấy liên kết")
	}
	// Chiều ngược FarmerAccounts: nông hộ → tài khoản (cảnh báo khẩn theo địa bàn, HTX US-NEWS-03).
	if subs, err := f.mod.FarmerAccounts().SubjectsForHouseholds(ctx, []uuid.UUID{own, ids.New()}); err != nil || len(subs) != 1 || subs[0] != user {
		t.Fatalf("SubjectsForHouseholds: %v %v", subs, err)
	}
	if subs, _ := f.mod.FarmerAccounts().SubjectsForHouseholds(tenant.WithID(context.Background(), ids.New()), []uuid.UUID{own}); len(subs) != 0 {
		t.Fatal("tenant khác không thấy tài khoản")
	}
	// Nông dân tự xem nông hộ của mình.
	farmer := f.asUser(f.tenant, user, port.RoleFarmer)
	if mh := farmer.get("/v1/farmer/households"); mh.status != http.StatusOK || len(mh.body["items"].([]any)) != 1 {
		t.Fatalf("nông hộ của tôi: %d %v", mh.status, mh.body)
	}

	// Quản lý HTX không tạo được cho nông hộ của HTX khác (kể cả cùng tỉnh).
	if r := mgr.post("/v1/farmer-accounts", key(), map[string]any{"household_ref": other.String(), "phone": "0912000002"}); r.status != http.StatusForbidden || str(r.body, "code") != "accounts.out_of_scope" {
		t.Fatalf("HTX khác: %d %v", r.status, r.body)
	}
	// SĐT không khớp nông hộ đã chọn.
	if r := mgr.post("/v1/farmer-accounts", key(), map[string]any{"household_ref": own.String(), "phone": "0912000002"}); r.status != http.StatusUnprocessableEntity || str(r.body, "code") != "accounts.phone_household_mismatch" {
		t.Fatalf("SĐT lệch: %d %v", r.status, r.body)
	}
	// Nông hộ đã có tài khoản.
	if r := mgr.post("/v1/farmer-accounts", key(), map[string]any{"phone": "0912000001"}); r.status != http.StatusConflict {
		t.Fatalf("trùng: %d %v", r.status, r.body)
	}
	// Cán bộ KN xã 30292 không tạo được cho xã 31120; cán bộ xã 31120 thì được (tra nông hộ theo SĐT).
	kn := f.as(f.tenant, f.team30292, partyport.RoleKNOfficer)
	if r := kn.post("/v1/farmer-accounts", key(), map[string]any{"phone": "0912000003"}); r.status != http.StatusForbidden {
		t.Fatalf("KN ngoài xã: %d %v", r.status, r.body)
	}
	kn2 := f.as(f.tenant, f.team31120, partyport.RoleKNOfficer)
	if r := kn2.post("/v1/farmer-accounts", key(), map[string]any{"phone": "0912000003"}); r.status != http.StatusCreated || str(r.body, "farmer", "household_ref") != far.String() {
		t.Fatalf("KN trong xã: %d %v", r.status, r.body)
	}
	// Vai không được phép.
	if r := farmer.post("/v1/farmer-accounts", key(), map[string]any{"phone": "0912000002"}); r.status != http.StatusForbidden {
		t.Fatalf("nông dân tự tạo: %d", r.status)
	}
	// Cách ly tenant: quản lý HTX tenant khác (cùng org_ref giả) không thấy nông hộ.
	mgrB := f.as(ids.New(), f.coop30292, partyport.RoleCoopManager)
	if r := mgrB.post("/v1/farmer-accounts", key(), map[string]any{"household_ref": own.String(), "phone": "0912000001"}); r.status < 400 {
		t.Fatalf("tenant khác: %d %v", r.status, r.body)
	}
	// Danh sách tài khoản nông dân của HTX (không PII).
	lst := mgr.get("/v1/farmer-accounts")
	if lst.status != http.StatusOK || len(lst.body["items"].([]any)) != 1 {
		t.Fatalf("danh sách: %d %v", lst.status, lst.body)
	}
	if mgr.get("/v1/farmer-accounts?cooperative_ref="+f.coop30301.String()).status != http.StatusForbidden {
		t.Fatal("danh sách HTX khác phải bị chặn")
	}
}

func (f *farmerFixture) grants(user uuid.UUID) []map[string]any {
	f.t.Helper()
	r := f.as(f.tenant, f.coop30292, partyport.RoleCoopManager).get("/v1/farmer-accounts/" + user.String() + "/activation")
	if r.status != http.StatusOK {
		f.t.Fatalf("lượt kích hoạt: %d %v", r.status, r.body)
	}
	var out []map[string]any
	for _, it := range r.body["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

// g2-conventions §1: phát lại liên kết/QR thu hồi lượt trước (reissued), lượt mới
// có token mới gắn mã lượt mới; hoàn tất liên kết → lượt ghi "đã dùng".
func TestActivationReissueRevokesPrevious(t *testing.T) {
	f := newFarmerFixture(t)
	f.household(f.tenant, f.coop30292, "30292", "0912000011")
	mgr := f.as(f.tenant, f.coop30292, partyport.RoleCoopManager)
	r := mgr.post("/v1/farmer-accounts", key(), map[string]any{"phone": "0912000011"})
	if r.status != http.StatusCreated {
		t.Fatalf("tạo: %d %v", r.status, r.body)
	}
	user := uuid.MustParse(str(r.body, "farmer", "user_ref"))
	first := str(r.body, "activation", "grant_id")
	firstLink := str(r.body, "activation", "link")

	r2 := mgr.post("/v1/farmer-accounts/"+user.String()+"/activation", key(), nil)
	if r2.status != http.StatusCreated || str(r2.body, "activation", "link") == "" || str(r2.body, "activation", "link") == firstLink {
		t.Fatalf("phát lại: %d %v", r2.status, r2.body)
	}
	second := str(r2.body, "activation", "grant_id")
	gs := f.grants(user)
	if len(gs) != 2 || gs[0]["grant_id"] != second || gs[0]["status"] != "active" ||
		gs[1]["grant_id"] != first || gs[1]["status"] != "revoked" || gs[1]["revoke_reason"] != "reissued" || gs[1]["replaced_by"] != second {
		t.Fatalf("lượt sau phát lại: %v", gs)
	}
	if l := f.dir.Links(); len(l) != 2 || l[1].GrantID.String() != second {
		t.Fatalf("Keycloak nhận mã lượt mới: %+v", l)
	}
	// Nông dân hoàn tất liên kết (đặt mật khẩu) → lượt đã dùng.
	f.dir.CompleteActions(user, port.ActionUpdatePassword)
	if gs := f.grants(user); gs[0]["status"] != "used" || gs[0]["used_at"] == nil {
		t.Fatalf("đã dùng: %v", gs)
	}
	// Quên mật khẩu: phát lại thêm UPDATE_PASSWORD (không cấp mật khẩu tạm, HTX US-ACC-01 AC-3).
	if r3 := mgr.post("/v1/farmer-accounts/"+user.String()+"/activation", key(), nil); r3.status != http.StatusCreated {
		t.Fatalf("đặt lại mật khẩu: %d %v", r3.status, r3.body)
	}
	if u, _ := f.dir.GetUser(context.Background(), user); !slices.Contains(u.RequiredActions, port.ActionUpdatePassword) {
		t.Fatalf("required action: %v", u.RequiredActions)
	}
	// Quản lý HTX khác không phát được (không lộ tài khoản).
	if r4 := f.as(f.tenant, f.coop30301, partyport.RoleCoopManager).post("/v1/farmer-accounts/"+user.String()+"/activation", key(), nil); r4.status != http.StatusNotFound {
		t.Fatalf("HTX khác: %d", r4.status)
	}
	// Tài khoản bị khóa → không phát.
	_ = f.dir.SetEnabled(context.Background(), user, false)
	if r5 := mgr.post("/v1/farmer-accounts/"+user.String()+"/activation", key(), nil); r5.status != http.StatusConflict {
		t.Fatalf("đang khóa: %d %v", r5.status, r5.body)
	}
}

// Token của liên kết kích hoạt không bao giờ vào log, sổ, bảng lượt hay bản lưu
// phát lại (shared.idempotency_key); gọi lặp cùng Idempotency-Key trả bản đã lược.
func TestActivationTokenNeverLoggedOrStored(t *testing.T) {
	f := newFarmerFixture(t)
	f.household(f.tenant, f.coop30292, "30292", "0912000021")
	mgr := f.as(f.tenant, f.coop30292, partyport.RoleCoopManager)
	k := key()
	r := mgr.post("/v1/farmer-accounts", k, map[string]any{"phone": "0912000021"})
	link := str(r.body, "activation", "link")
	if r.status != http.StatusCreated || link == "" {
		t.Fatalf("tạo: %d %v", r.status, r.body)
	}
	token := link[strings.Index(link, "key=")+4:]
	// Gọi lặp: phát lại phản hồi đã lược liên kết.
	again := mgr.post("/v1/farmer-accounts", k, map[string]any{"phone": "0912000021"})
	if again.hdr.Get(idempotency.ReplayedHeader) != "true" || str(again.body, "activation", "link") != "" || str(again.body, "activation", "grant_id") != str(r.body, "activation", "grant_id") {
		t.Fatalf("phát lại: %v %v", again.hdr, again.body)
	}
	user := str(r.body, "farmer", "user_ref")
	r2 := mgr.post("/v1/farmer-accounts/"+user+"/activation", key(), nil)
	token2 := str(r2.body, "activation", "link")
	token2 = token2[strings.Index(token2, "key=")+4:]
	if strings.Contains(f.logs.String(), token) || strings.Contains(f.logs.String(), token2) {
		t.Fatal("token nằm trong log")
	}
	ops := f.env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	for _, q := range []string{
		`SELECT count(*) FROM pf_accounts.activation_grant g WHERE row_to_json(g)::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM pf_accounts.account_event e WHERE row_to_json(e)::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM shared.outbox o WHERE row_to_json(o)::text LIKE '%' || $1 || '%'`,
	} {
		for _, tk := range []string{token, token2} {
			var n int
			err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(context.Background(), q, tk).Scan(&n)
			})
			if err != nil && strings.Contains(q, "shared.outbox") {
				err = ops.QueryRow(context.Background(), q, tk).Scan(&n)
			}
			if err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("token nằm trong DB: %s", q)
			}
		}
	}
	idem := f.env.Pool(t, migrations.PF, "platform_api")
	for _, tk := range []string{token, token2} {
		var n int
		if err := tenant.WithTenantTx(context.Background(), idem, f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.idempotency_key WHERE convert_from(response_body, 'UTF8') LIKE '%' || $1 || '%'`, tk).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("token nằm trong bản lưu phát lại idempotency")
		}
	}
}

// HTX US-ACC-02: nhập tài khoản nông dân hàng loạt — dòng lỗi có lý do, dòng hợp lệ
// vẫn được tạo và nhận liên kết kích hoạt riêng.
func TestFarmerImportPerRowHTXUSACC02(t *testing.T) {
	f := newFarmerFixture(t)
	f.household(f.tenant, f.coop30292, "30292", "0912000031")
	f.household(f.tenant, f.coop30292, "30292", "0912000032")
	f.household(f.tenant, f.coop30301, "30301", "0912000033")
	mgr := f.as(f.tenant, f.coop30292, partyport.RoleCoopManager)
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Nông dân", []string{"Số điện thoại", "Họ tên"}, [][]string{
		{"0912000031", "Lê Văn A"}, // 2: tạo
		{"", "Thiếu SĐT"},          // 3
		{"09120", "SĐT sai"},       // 4
		{"0912 000 031", "Lặp"},    // 5: trùng dòng 2
		{"0912000033", "HTX khác"}, // 6: ngoài HTX
		{"0999999999", "Không hộ"}, // 7: không có nông hộ
		{"0912000032", "Lê Văn B"}, // 8: tạo
	}); err != nil {
		t.Fatal(err)
	}
	r := mgr.raw(http.MethodPost, "/v1/farmer-accounts/import", key(), tabular.ContentTypeXLSX, buf.Bytes())
	if r.status != http.StatusOK || r.body["created"].(float64) != 2 || r.body["rejected"].(float64) != 5 {
		t.Fatalf("nhập: %d %v", r.status, r.body)
	}
	reasons := map[int]string{}
	for _, it := range r.body["rows"].([]any) {
		m := it.(map[string]any)
		row := int(m["row"].(float64))
		if m["status"] == "created" {
			if str(m, "activation", "link") == "" {
				t.Fatalf("dòng %d thiếu liên kết kích hoạt", row)
			}
			continue
		}
		reasons[row], _ = m["reason"].(string)
	}
	want := map[int]string{3: "Thiếu SĐT", 4: "Sai định dạng SĐT", 5: "trùng với dòng 2", 6: "ngoài phạm vi", 7: "Thiếu mã nông hộ"}
	for row, w := range want {
		if !strings.Contains(strings.ToLower(reasons[row]), strings.ToLower(w)) {
			t.Fatalf("dòng %d: %q, muốn chứa %q", row, reasons[row], w)
		}
	}
}

// HTX US-SUPPORT-02: nông dân tra danh bạ cán bộ HTX của mình theo tên/chức vụ; SĐT
// chỉ đọc lúc bấm (no-store); cán bộ không có số → ẩn gọi nhanh; HTX khác → không thấy.
func TestCoopStaffDirectoryHTXUSSUPPORT02(t *testing.T) {
	f := newFarmerFixture(t)
	f.household(f.tenant, f.coop30292, "30292", "0912000041")
	mgr := f.as(f.tenant, f.coop30292, partyport.RoleCoopManager)
	r := mgr.post("/v1/farmer-accounts", key(), map[string]any{"phone": "0912000041"})
	user := uuid.MustParse(str(r.body, "farmer", "user_ref"))
	withPhone := f.dir.Seed(port.NewUser{Username: "ql-htx", FirstName: "Hùng", LastName: "Phạm Văn", Phone: "0987654321", TenantID: f.tenant, OrgRef: f.coop30292}, true, "coop_manager")
	noPhone := f.dir.Seed(port.NewUser{Username: "ke-toan", FirstName: "Lan", LastName: "Võ Thị", TenantID: f.tenant, OrgRef: f.coop30292}, true)
	otherCoop := f.dir.Seed(port.NewUser{Username: "ql-khac", FirstName: "Tư", LastName: "Ngô", Phone: "0987000000", TenantID: f.tenant, OrgRef: f.coop30301}, true)

	farmer := f.asUser(f.tenant, user, port.RoleFarmer)
	d := farmer.get("/v1/farmer/coop-staff")
	if d.status != http.StatusOK {
		t.Fatalf("danh bạ: %d %v", d.status, d.body)
	}
	got := map[string]map[string]any{}
	for _, it := range d.body["items"].([]any) {
		m := it.(map[string]any)
		got[m["user_ref"].(string)] = m
		if _, leaked := m["phone"]; leaked {
			t.Fatal("danh sách không được chứa SĐT")
		}
	}
	if len(got) != 2 || got[withPhone.String()]["has_phone"] != true || got[noPhone.String()]["has_phone"] != false {
		t.Fatalf("danh bạ: %v", d.body)
	}
	if _, self := got[user.String()]; self {
		t.Fatal("tài khoản nông dân không thuộc danh bạ cán bộ")
	}
	if q := farmer.get("/v1/farmer/coop-staff?q=hung"); len(q.body["items"].([]any)) != 1 {
		t.Fatalf("tìm theo tên không dấu: %v", q.body)
	}
	c := farmer.get("/v1/farmer/coop-staff/" + withPhone.String() + "/contact")
	if c.status != http.StatusOK || str(c.body, "phone") != "0987654321" || c.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("liên hệ: %d %v %v", c.status, c.body, c.hdr)
	}
	if c := farmer.get("/v1/farmer/coop-staff/" + noPhone.String() + "/contact"); c.status != http.StatusNotFound || str(c.body, "code") != "accounts.contact_unavailable" {
		t.Fatalf("không có số: %d %v", c.status, c.body)
	}
	if c := farmer.get("/v1/farmer/coop-staff/" + otherCoop.String() + "/contact"); c.status != http.StatusNotFound || str(c.body, "code") != "accounts.staff_not_found" {
		t.Fatalf("HTX khác: %d %v", c.status, c.body)
	}
	if mgr.get("/v1/farmer/coop-staff").status != http.StatusForbidden {
		t.Fatal("chỉ vai farmer")
	}
}
