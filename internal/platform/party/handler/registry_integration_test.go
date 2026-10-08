package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
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
	app    *httptest.Server
	admin  *httptest.Server
	vault  *fakeVault
	units  *fakeUnits
	mod    *party.Module
	pool   *pgxpool.Pool
	tenant uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, party.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	vault := newFakeVault()
	units := newFakeUnits()
	mod := party.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, party.Deps{AdminUnits: units, Vault: vault})
	log := telemetry.NewLogger("test", io.Discard)

	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appSrv, adminSrv := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(appSrv.Close)
	t.Cleanup(adminSrv.Close)
	return &fixture{t: t, env: env, iss: iss, app: appSrv, admin: adminSrv, vault: vault, units: units, mod: mod, pool: pool, tenant: ids.New()}
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	roles []string
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, roles: roles, token: tok, actor: actor}
}

// root là quản trị toàn quốc của tenant mặc định.
func (f *fixture) root() caller {
	return f.as(f.tenant, uuid.Nil, port.RoleRegistryAdmin, authz.RolePlatformAdmin)
}

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (c caller) call(base, method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
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
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) appDo(method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	idem := ""
	if body != nil {
		b, _ = json.Marshal(body)
	}
	if method == http.MethodPost {
		idem = key()
	}
	return c.call(c.f.app.URL, method, path, idem, "application/json", b)
}

func (c caller) adminDo(method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	idem := ""
	if body != nil {
		b, _ = json.Marshal(body)
	}
	if method == http.MethodPost {
		idem = key()
	}
	return c.call(c.f.admin.URL, method, path, idem, "application/json", b)
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

// createOrg tạo tổ chức qua Admin API và trả ref.
func (c caller) createOrg(body map[string]any) uuid.UUID {
	c.f.t.Helper()
	r := c.adminDo(http.MethodPost, "/v1/orgs", body)
	if r.status != http.StatusCreated {
		c.f.t.Fatalf("tạo tổ chức %v: %d %v", body["name"], r.status, r.body)
	}
	return uuid.MustParse(str(r.body, "org", "ref"))
}

// tree dựng cây: TW → tỉnh 89 (tổ xã 30001, 30002) và tỉnh 92; HTX ở 30001 và 31001.
type tree struct {
	national, p89, p92, t30001, t30002, c1, c2 uuid.UUID
}

func (f *fixture) seedTree(root caller) tree {
	var tr tree
	tr.national = root.createOrg(map[string]any{"kind": "ttkn_national", "code": "TTKNQG-" + ids.New().String()[:8], "name": "Trung tâm Khuyến nông Quốc gia"})
	tr.p89 = root.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN89-" + ids.New().String()[:8], "name": "TTKN An Giang", "admin_unit_code": "89", "parent_ref": tr.national.String()})
	tr.p92 = root.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN92-" + ids.New().String()[:8], "name": "TTKN Cần Thơ", "admin_unit_code": "92", "parent_ref": tr.national.String()})
	tr.t30001 = root.createOrg(map[string]any{"kind": "extension_team", "code": "TKN30001-" + ids.New().String()[:6], "name": "Tổ KNCĐ Xã A", "admin_unit_code": "30001", "parent_ref": tr.p89.String()})
	tr.t30002 = root.createOrg(map[string]any{"kind": "extension_team", "code": "TKN30002-" + ids.New().String()[:6], "name": "Tổ KNCĐ Xã B", "admin_unit_code": "30002", "parent_ref": tr.p89.String()})
	tr.c1 = root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Lúa Xanh", "admin_unit_code": "30001", "parent_ref": tr.t30001.String(),
		"cooperative": map[string]any{"area_ha": "120.5", "established_on": "2019-03-01", "location": map[string]any{"lat": "10.3861", "lon": "105.4352"}}})
	tr.c2 = root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Cần Thơ Xanh", "admin_unit_code": "31001", "parent_ref": tr.p92.String()})
	return tr
}

func items(r resp) []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func refs(r resp, field string) []string {
	var out []string
	for _, it := range items(r) {
		out = append(out, it[field].(string))
	}
	return out
}

func farmer(coop uuid.UUID, phone string) map[string]any {
	return map[string]any{
		"cooperative_ref": coop.String(), "cultivated_area_ha": "1.25", "member_since": "2024-01-15",
		"identity": map[string]any{"full_name": "Nguyễn Văn An", "phone": phone, "address": "Ấp 1"},
	}
}

// ---------------------------------------------------------------------------

// KN US-HTX-01, ADR-0009: TTKN tỉnh chỉ thấy HTX của các xã trong tỉnh; TTKN QG thấy toàn quốc.
func TestScopeProvinceSeesOnlyItsCommunes(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())

	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	got := refs(prov.appDo(http.MethodGet, "/v1/cooperatives", nil), "ref")
	if !slices.Equal(got, []string{tr.c1.String()}) {
		t.Fatalf("TTKN tỉnh 89 chỉ thấy HTX xã 30001, nhận %v", got)
	}
	if r := prov.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil); r.status != http.StatusNotFound {
		t.Fatalf("HTX tỉnh khác phải 404, nhận %d", r.status)
	}
	orgs := refs(prov.appDo(http.MethodGet, "/v1/orgs", nil), "ref")
	if slices.Contains(orgs, tr.p92.String()) || slices.Contains(orgs, tr.national.String()) || !slices.Contains(orgs, tr.t30002.String()) {
		t.Fatalf("GET /v1/orgs theo phạm vi tỉnh sai: %v", orgs)
	}

	national := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)
	if got := refs(national.appDo(http.MethodGet, "/v1/cooperatives", nil), "ref"); len(got) != 2 {
		t.Fatalf("TTKN QG thấy toàn quốc: %v", got)
	}
	// TTKN QG chỉ đọc (KN US-HTX-01): không tạo được nông hộ.
	if r := national.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0901000001")); r.status != http.StatusForbidden {
		t.Fatalf("TTKN QG chỉ đọc, nhận %d", r.status)
	}

	// Chi tiết HTX: toạ độ và cờ "vị trí tạm" (CGH FN-03 AC-03).
	c1 := prov.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String(), nil)
	if c1.body["location_provisional"] != false || str(c1.body, "area_ha") != "120.5" || !strings.HasPrefix(str(c1.body, "code"), "HTX") {
		t.Fatalf("hồ sơ HTX: %v", c1.body)
	}
	c2 := national.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil)
	if c2.body["location_provisional"] != true || c2.body["location"] != nil {
		t.Fatalf("HTX không toạ độ phải gắn cờ vị trí tạm: %v", c2.body)
	}

	// Tìm theo tên, phân trang cursor (R-53).
	if got := refs(national.appDo(http.MethodGet, "/v1/cooperatives?q=c%E1%BA%A7n", nil), "ref"); !slices.Equal(got, []string{tr.c2.String()}) {
		t.Fatalf("tìm theo tên: %v", got)
	}
	p1 := national.appDo(http.MethodGet, "/v1/cooperatives?limit=1", nil)
	cur, _ := p1.body["next_cursor"].(string)
	if len(items(p1)) != 1 || cur == "" {
		t.Fatalf("trang 1: %v", p1.body)
	}
	p2 := national.appDo(http.MethodGet, "/v1/cooperatives?limit=1&cursor="+cur, nil)
	if len(items(p2)) != 1 || p2.body["next_cursor"] != nil || refs(p2, "ref")[0] == refs(p1, "ref")[0] {
		t.Fatalf("trang 2: %v", p2.body)
	}

	// Port ScopeResolver trực tiếp.
	ctx := authz.WithPrincipal(tenant.WithID(context.Background(), f.tenant), authz.Principal{TenantID: f.tenant, OrgRef: tr.p89, ActorRef: ids.New()})
	sc, err := f.mod.ScopeResolver().Scope(ctx)
	if err != nil || sc.All || !sc.Allows("89") || !sc.Allows("30001") || !sc.Allows("30002") || sc.Allows("31001") {
		t.Fatalf("scope tỉnh: %+v %v", sc, err)
	}
	o, err := f.mod.Orgs().Get(ctx, tr.c1)
	if err != nil || o.Kind != port.OrgCooperative || o.AdminUnitCode != "30001" || o.ParentRef != tr.t30001 {
		t.Fatalf("port.Orgs.Get: %+v %v", o, err)
	}
	if _, err := f.mod.Orgs().Get(ctx, ids.New()); !errors.Is(err, port.ErrOrgNotFound) {
		t.Fatalf("muốn ErrOrgNotFound, nhận %v", err)
	}
}

// KN US-HTX-02: cán bộ xã chỉ tạo nông hộ trong xã phụ trách.
func TestCommuneOfficerCannotCreateOutsideCommune(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	officer := f.as(f.tenant, tr.t30001, port.RoleKNOfficer)

	if r := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c2, "0902000001")); r.status != http.StatusForbidden || r.body["code"] != "party.out_of_scope" {
		t.Fatalf("HTX ngoài xã phải 403 party.out_of_scope: %d %v", r.status, r.body)
	}
	other := farmer(tr.c1, "0902000002")
	other["commune_code"] = "30002"
	if r := officer.appDo(http.MethodPost, "/v1/households", other); r.status != http.StatusForbidden {
		t.Fatalf("xã của hộ ngoài phạm vi phải 403: %d %v", r.status, r.body)
	}
	if r := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0902000003")); r.status != http.StatusCreated {
		t.Fatalf("tạo trong xã: %d %v", r.status, r.body)
	}
	// Quản lý HTX chỉ thao tác nông hộ của HTX mình (HTX US-HH-01 AC-3).
	mgrOther := f.as(f.tenant, tr.c2, port.RoleCoopManager)
	if r := mgrOther.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0902000004")); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX khác phải 403: %d %v", r.status, r.body)
	}
	mgr := f.as(f.tenant, tr.c1, port.RoleCoopManager)
	if r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0902000005")); r.status != http.StatusCreated {
		t.Fatalf("quản lý HTX tạo hộ của HTX mình: %d %v", r.status, r.body)
	}
}

// ADR-0009: không có org_ref → phạm vi rỗng, mặc định chặn.
func TestNoOrgSeesNothing(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	anon := f.as(f.tenant, uuid.Nil, port.RoleKNOfficer, port.RoleKNProvinceAdmin)
	if r := anon.appDo(http.MethodGet, "/v1/cooperatives", nil); r.status != http.StatusOK || len(items(r)) != 0 {
		t.Fatalf("không org_ref phải thấy rỗng: %d %v", r.status, r.body)
	}
	if r := anon.appDo(http.MethodGet, "/v1/orgs", nil); len(items(r)) != 0 {
		t.Fatalf("không org_ref: %v", r.body)
	}
	if r := anon.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String(), nil); r.status != http.StatusNotFound {
		t.Fatalf("không org_ref đọc chi tiết phải 404: %d", r.status)
	}
	if r := anon.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0903000001")); r.status != http.StatusForbidden {
		t.Fatalf("không org_ref ghi phải 403: %d %v", r.status, r.body)
	}
	unknown := f.as(f.tenant, ids.New(), port.RoleKNOfficer)
	if r := unknown.appDo(http.MethodGet, "/v1/cooperatives", nil); len(items(r)) != 0 {
		t.Fatalf("org_ref không tồn tại phải thấy rỗng: %v", r.body)
	}
}

// HTX US-HTXSTATUS-01/02/03: lý do ≥ 20 ký tự, sự kiện khóa/mở tài khoản, nhật ký chỉ đọc có chuỗi băm.
func TestDeactivateReactivateCooperative(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	path := "/v1/cooperatives/" + tr.c1.String()
	mgr := f.as(f.tenant, tr.c1, port.RoleCoopManager)
	if r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0904000001")); r.status != http.StatusCreated {
		t.Fatalf("tạo hộ: %v", r.body)
	}

	short := "Giải thể theo QĐ 12" // 19 ký tự có dấu
	if r := root.adminDo(http.MethodPost, path+"/deactivate", map[string]any{"reason": short}); r.status != http.StatusUnprocessableEntity || r.body["code"] != "validation_failed" {
		t.Fatalf("lý do 19 ký tự phải 422: %d %v", r.status, r.body)
	}
	if r := f.as(f.tenant, tr.p89, port.RoleKNOfficer).adminDo(http.MethodPost, path+"/deactivate", map[string]any{"reason": "Giải thể theo quyết định số 12/QĐ-UBND"}); r.status != http.StatusForbidden {
		t.Fatalf("không có vai quản trị phải 403: %d", r.status)
	}
	reason := "Giải thể theo quyết định số 12/QĐ-UBND"
	r := root.adminDo(http.MethodPost, path+"/deactivate", map[string]any{"reason": reason})
	if r.status != http.StatusOK || str(r.body, "org", "ref") != tr.c1.String() || r.body["active_households"] != float64(1) {
		t.Fatalf("vô hiệu hóa: %d %v", r.status, r.body)
	}
	if r := root.adminDo(http.MethodPost, path+"/deactivate", map[string]any{"reason": reason}); r.status != http.StatusConflict {
		t.Fatalf("vô hiệu hóa lần hai phải 409: %d", r.status)
	}

	// Ẩn khỏi danh sách hoạt động, vẫn tra cứu được (HTX US-HTX-01 AC-2).
	national := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)
	if got := refs(national.appDo(http.MethodGet, "/v1/cooperatives", nil), "ref"); slices.Contains(got, tr.c1.String()) {
		t.Fatalf("HTX vô hiệu hóa phải ẩn: %v", got)
	}
	if got := refs(national.appDo(http.MethodGet, "/v1/cooperatives?include_inactive=true", nil), "ref"); !slices.Contains(got, tr.c1.String()) {
		t.Fatalf("include_inactive phải thấy: %v", got)
	}
	// Tài khoản thuộc HTX đã vô hiệu hóa không còn phạm vi.
	if r := mgr.appDo(http.MethodGet, "/v1/cooperatives", nil); len(items(r)) != 0 {
		t.Fatalf("HTX bị vô hiệu hóa → phạm vi rỗng: %v", r.body)
	}
	if r := root.adminDo(http.MethodPost, path+"/reactivate", map[string]any{"reason": "Tái tham gia đề án"}); r.status != http.StatusOK {
		t.Fatalf("kích hoạt lại: %d %v", r.status, r.body)
	}

	// Sự kiện outbox.
	pub := f.env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	for typ, want := range map[string]int{port.EventOrgDeactivated: 1, port.EventOrgReactivated: 1} {
		var n int
		var payloadReason string
		if err := pub.QueryRow(context.Background(), `SELECT count(*), coalesce(max(payload->>'reason'), '') FROM shared.outbox
		    WHERE tenant_id = $1 AND event_type = $2 AND payload->>'org_ref' = $3`, f.tenant, typ, tr.c1.String()).Scan(&n, &payloadReason); err != nil {
			t.Fatal(err)
		}
		if n != want || (typ == port.EventOrgDeactivated && payloadReason != reason) {
			t.Fatalf("%s: %d sự kiện, lý do %q", typ, n, payloadReason)
		}
	}

	// Nhật ký chỉ đọc: created → deactivated → reactivated, chuỗi băm nguyên vẹn.
	h := root.adminDo(http.MethodGet, "/v1/orgs/"+tr.c1.String()+"/events", nil)
	var actions []string
	for _, it := range items(h) {
		actions = append(actions, it["action"].(string))
	}
	if !slices.Equal(actions, []string{"created", "deactivated", "reactivated"}) || str(items(h)[1], "reason") != reason {
		t.Fatalf("nhật ký: %v", h.body)
	}
	verifyChain(t, f, tr.c1, 3)
	// Sổ chỉ ghi thêm: party_app không sửa được (US-HTXSTATUS-03 AC-2).
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "UPDATE pf_party.org_event SET reason = 'x' WHERE org_ref = $1", tr.c1)
		return e
	})
	if err == nil {
		t.Fatal("UPDATE sổ org_event phải bị từ chối")
	}
}

func verifyChain(t *testing.T, f *fixture, ref uuid.UUID, rows int64) {
	t.Helper()
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		res, err := ledger.Verify(context.Background(), tx, "pf_party.org_event", f.tenant, "org:"+ref.String())
		if err != nil {
			return err
		}
		if !res.OK || res.RowsChecked != rows {
			t.Fatalf("chuỗi băm: %+v", res)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// CGH FN-03 / MST-01: mã số thuế duy nhất trong tenant, chuẩn hóa định dạng.
func TestTaxCodeUniquePerTenant(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	coop := func(c caller, tax string) resp {
		return c.adminDo(http.MethodPost, "/v1/orgs", map[string]any{"kind": "cooperative", "name": "HTX " + tax, "admin_unit_code": "30001",
			"cooperative": map[string]any{"tax_code": tax}})
	}
	if r := coop(root, "0301234567"); r.status != http.StatusCreated {
		t.Fatalf("tạo HTX: %d %v", r.status, r.body)
	}
	if r := coop(root, "030-123-4567"); r.status != http.StatusConflict || r.body["code"] != "party.tax_code_taken" {
		t.Fatalf("trùng MST trong tenant phải 409: %d %v", r.status, r.body)
	}
	if r := coop(root, "12345"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("MST sai định dạng phải 422: %d", r.status)
	}
	other := f.as(ids.New(), uuid.Nil, port.RoleRegistryAdmin, authz.RolePlatformAdmin)
	if r := coop(other, "0301234567"); r.status != http.StatusCreated {
		t.Fatalf("tenant khác được dùng cùng MST: %d %v", r.status, r.body)
	}
	// Mã HTX do hệ thống tự sinh (CGH BR-01).
	r := root.adminDo(http.MethodPost, "/v1/orgs", map[string]any{"kind": "cooperative", "code": "TUNHAP", "name": "HTX tự đặt mã", "admin_unit_code": "30001"})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("nhập tay Mã HTX phải 422: %d", r.status)
	}
}

// KN US-HTX-05, HTX US-HH-01: tạo chủ hộ qua kho định danh, trùng → dùng lại mã cũ, báo "trùng".
func TestHouseholdDedupeViaVault(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	officer := f.as(f.tenant, tr.t30001, port.RoleKNOfficer)

	r1 := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0905000001"))
	if r1.status != http.StatusCreated || r1.body["duplicate"] != false {
		t.Fatalf("tạo hộ: %d %v", r1.status, r1.body)
	}
	ref := str(r1.body, "household", "household_ref")
	r2 := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0905000001"))
	if r2.status != http.StatusOK || r2.body["duplicate"] != true || r2.body["duplicate_label"] != "trùng" || str(r2.body, "household", "household_ref") != ref {
		t.Fatalf("trùng SĐT phải dùng lại mã cũ: %d %v", r2.status, r2.body)
	}
	lst := officer.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil)
	it := items(lst)
	if len(it) != 1 || it[0]["household_ref"] != ref || it[0]["label"] != "Nguyễn A." || it[0]["masked_phone"] != "xxx 001" {
		t.Fatalf("danh sách nông hộ kèm nhãn che: %v", lst.body)
	}
	if _, ok := it[0]["phone"]; ok {
		t.Fatal("danh sách không được có SĐT đầy đủ")
	}
	p := f.vault.usedPurposes()
	if !slices.Contains(p, "registry.admin") || !slices.Contains(p, "extension.field_support") {
		t.Fatalf("mục đích gọi kho định danh: %v", p)
	}
	// Cục KTHT không xem nông hộ.
	if r := f.as(f.tenant, tr.national, port.RoleCGHViewer).appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil); r.status != http.StatusForbidden {
		t.Fatalf("cgh_viewer xem nông hộ phải 403: %d", r.status)
	}
}

// KN US-ADM-07/08: di chuyển nhánh khi sáp nhập, giữ lịch sử, chuỗi băm nguyên vẹn, chặn vòng.
func TestOrgMoveHistoryAndChain(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	path := "/v1/orgs/" + tr.c1.String() + "/move"

	if r := root.adminDo(http.MethodPost, path, map[string]any{"new_parent_ref": tr.t30002.String(), "new_admin_unit_code": "30002", "reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn phải 422: %d", r.status)
	}
	if r := root.adminDo(http.MethodPost, "/v1/orgs/"+tr.p89.String()+"/move", map[string]any{"new_parent_ref": tr.t30001.String(), "reason": "Thử tạo vòng trong cây"}); r.status != http.StatusUnprocessableEntity || r.body["code"] != "party.invalid_hierarchy" {
		t.Fatalf("di chuyển vào nhánh con phải 422 invalid_hierarchy: %d %v", r.status, r.body)
	}
	if r := root.adminDo(http.MethodPost, path, map[string]any{"new_parent_ref": tr.t30002.String(), "reason": "Xã khác tỉnh không hợp lệ"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("HTX xã 30001 dưới tổ xã 30002 phải 422: %d %v", r.status, r.body)
	}
	reason := "Sáp nhập xã A vào xã B theo NQ 1234"
	r := root.adminDo(http.MethodPost, path, map[string]any{"new_parent_ref": tr.t30002.String(), "new_admin_unit_code": "30002", "reason": reason})
	if r.status != http.StatusOK || str(r.body, "org", "parent_ref") != tr.t30002.String() || str(r.body, "org", "admin_unit_code") != "30002" {
		t.Fatalf("di chuyển: %d %v", r.status, r.body)
	}
	if str(r.body, "event", "prev_parent_ref") != tr.t30001.String() || str(r.body, "event", "prev_admin_unit_code") != "30001" {
		t.Fatalf("sổ phải giữ vị trí cũ: %v", r.body)
	}
	// Phạm vi đổi ngay theo vị trí mới (US-ADM-07 AC-2).
	if got := refs(f.as(f.tenant, tr.t30001, port.RoleKNOfficer).appDo(http.MethodGet, "/v1/cooperatives", nil), "ref"); len(got) != 0 {
		t.Fatalf("tổ xã cũ không còn thấy HTX: %v", got)
	}
	if got := refs(f.as(f.tenant, tr.t30002, port.RoleKNOfficer).appDo(http.MethodGet, "/v1/cooperatives", nil), "ref"); !slices.Equal(got, []string{tr.c1.String()}) {
		t.Fatalf("tổ xã mới thấy HTX: %v", got)
	}
	h := root.adminDo(http.MethodGet, "/v1/orgs/"+tr.c1.String()+"/events", nil)
	if len(items(h)) != 2 || items(h)[1]["action"] != "moved" || items(h)[1]["reason"] != reason {
		t.Fatalf("lịch sử di chuyển: %v", h.body)
	}
	verifyChain(t, f, tr.c1, 2)

	// Đổi tên cũng vào sổ.
	if r := root.adminDo(http.MethodPost, "/v1/orgs/"+tr.t30002.String()+"/rename", map[string]any{"name": "Tổ KNCĐ Xã B mới"}); r.status != http.StatusOK || str(r.body, "org", "name") != "Tổ KNCĐ Xã B mới" {
		t.Fatalf("đổi tên: %d %v", r.status, r.body)
	}
	// Không vô hiệu hóa được nút còn con đang hoạt động (dùng API chung cho HTX thì 404 vì khác loại).
	if r := root.adminDo(http.MethodPost, "/v1/cooperatives/"+tr.t30002.String()+"/deactivate", map[string]any{"reason": "Giải thể tổ khuyến nông xã B cũ"}); r.status != http.StatusNotFound {
		t.Fatalf("deactivate qua route HTX với tổ KN phải 404: %d", r.status)
	}
}

// RB-2: cách ly tenant.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	other := f.as(ids.New(), uuid.Nil, port.RoleRegistryAdmin, authz.RolePlatformAdmin)
	if r := other.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String(), nil); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác phải 404: %d", r.status)
	}
	if r := other.appDo(http.MethodGet, "/v1/cooperatives", nil); len(items(r)) != 0 {
		t.Fatalf("tenant khác thấy rỗng: %v", r.body)
	}
	if r := other.adminDo(http.MethodPost, "/v1/cooperatives/"+tr.c1.String()+"/deactivate", map[string]any{"reason": "Thử vô hiệu hóa HTX của tenant khác"}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác vô hiệu hóa phải 404: %d", r.status)
	}
	// org_ref trỏ sang tổ chức của tenant khác → không có phạm vi.
	cross := f.as(other.tid, tr.national, port.RoleKNNationalAdmin)
	if r := cross.appDo(http.MethodGet, "/v1/orgs", nil); len(items(r)) != 0 {
		t.Fatalf("org_ref của tenant khác không cho phạm vi: %v", r.body)
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu, sổ chỉ một dòng.
func TestIdempotentCreateOrg(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	body, _ := json.Marshal(map[string]any{"kind": "ttkn_national", "code": "QG-IDEM", "name": "TTKN QG"})
	k := key()
	r1 := root.call(f.admin.URL, http.MethodPost, "/v1/orgs", k, "application/json", body)
	r2 := root.call(f.admin.URL, http.MethodPost, "/v1/orgs", k, "application/json", body)
	if r1.status != http.StatusCreated || r2.status != http.StatusCreated || r2.hdr.Get("Idempotent-Replayed") != "true" ||
		str(r1.body, "org", "ref") != str(r2.body, "org", "ref") {
		t.Fatalf("gọi lặp: %d %v / %d %v", r1.status, r1.body, r2.status, r2.body)
	}
	var n int
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_party.org_event WHERE idem_key = $1", k).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sổ phải có đúng 1 dòng, có %d", n)
	}
	// Khóa mới, cùng mã → 409.
	if r := root.call(f.admin.URL, http.MethodPost, "/v1/orgs", key(), "application/json", body); r.status != http.StatusConflict || r.body["code"] != "party.org_code_taken" {
		t.Fatalf("trùng mã tổ chức: %d %v", r.status, r.body)
	}
	// Admin API bắt buộc MFA.
	noMFA := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{port.RoleRegistryAdmin}})
	c := caller{f: f, token: noMFA}
	if r := c.call(f.admin.URL, http.MethodGet, "/v1/orgs/"+str(r1.body, "org", "ref")+"/events", "", "application/json", nil); r.status != http.StatusForbidden || r.body["code"] != "mfa_required" {
		t.Fatalf("thiếu MFA: %d %v", r.status, r.body)
	}
}

// KN US-HTX-03, HTX US-HH-02: nhập CSV, kết quả theo dòng.
func TestImportHouseholdsCSV(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	var c1Code, c2Code string
	national := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)
	for _, it := range items(national.appDo(http.MethodGet, "/v1/cooperatives", nil)) {
		switch it["ref"] {
		case tr.c1.String():
			c1Code = it["code"].(string)
		case tr.c2.String():
			c2Code = it["code"].(string)
		}
	}
	csvBody := "ho_ten,so_dien_thoai,cccd,dia_chi,ma_htx,dien_tich_ha,ngay_vao_htx\n" +
		"Trần Thị Bình,0906000001,,Ấp 2," + c1Code + ",2.5,2023-05-01\n" +
		"Trần Thị Bình,0906000001,,Ấp 2," + c1Code + ",2.5,2023-05-01\n" +
		"Lê Văn Cường,,,Ấp 3," + c1Code + ",1,\n" +
		"Phạm Văn Dũng,0906000004,,Ấp 4,HTX999999,1,\n" +
		"Võ Thị Em,0906000005,,Ấp 5," + c2Code + ",1,\n" +
		"Đỗ Văn Phúc,0906000006,,Ấp 6," + c1Code + ",abc,\n"
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), "text/csv", []byte(csvBody))
	if r.status != http.StatusOK || r.body["accepted"] != float64(1) || r.body["duplicate"] != float64(1) || r.body["rejected"] != float64(4) {
		t.Fatalf("kết quả nhập: %d %v", r.status, r.body)
	}
	rows, _ := r.body["rows"].([]any)
	want := []string{"accepted", "duplicate", "rejected", "rejected", "rejected", "rejected"}
	for i, raw := range rows {
		row := raw.(map[string]any)
		if row["status"] != want[i] || row["row"] != float64(i+2) {
			t.Fatalf("dòng %d: %v", i+2, row)
		}
		if reason, _ := row["reason"].(string); strings.Contains(reason, "0906") || strings.Contains(reason, "Trần") {
			t.Fatalf("lý do không được chứa dữ liệu cá nhân: %q", reason)
		}
	}
	// Tệp thiếu cột bắt buộc → từ chối cả tệp.
	bad := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), "text/csv", []byte("ho_ten,dia_chi\nA,B\n"))
	if bad.status != http.StatusUnprocessableEntity || bad.body["code"] != "party.import_invalid" {
		t.Fatalf("tệp sai mẫu: %d %v", bad.status, bad.body)
	}
}

// R-04: không có dữ liệu cá nhân trong bảng pf_party.
func TestNoPIIColumns(t *testing.T) {
	f := newFixture(t)
	rows, err := f.env.AdminPool(t, migrations.PF).Query(context.Background(),
		"SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = 'pf_party'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	forbidden := []string{"full_name", "phone", "national_id", "cccd", "address", "label", "email", "birth", "dob", "holder", "owner_name"}
	cols := map[string][]string{}
	for rows.Next() {
		var tbl, col string
		if err := rows.Scan(&tbl, &col); err != nil {
			t.Fatal(err)
		}
		cols[tbl] = append(cols[tbl], col)
		for _, bad := range forbidden {
			if strings.Contains(col, bad) {
				t.Errorf("pf_party.%s.%s trông như dữ liệu cá nhân (R-04)", tbl, col)
			}
		}
	}
	hh := cols["household"]
	slices.Sort(hh)
	want := []string{"active", "commune_code", "created_at", "created_by", "cultivated_area_ha", "household_ref", "tenant_id", "updated_at"}
	if !slices.Equal(hh, want) {
		t.Fatalf("pf_party.household chỉ được giữ mã tham chiếu và thuộc tính không định danh: %v", hh)
	}
	if len(cols["org_event"]) == 0 || len(cols["cooperative"]) == 0 || len(cols["household_membership"]) == 0 {
		t.Fatalf("thiếu bảng: %v", cols)
	}
}
