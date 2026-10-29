package knrecords_test

// KNSPLIT-2-KN-RECORDS (#67): hồ sơ KN riêng HTX / Nông hộ / Tổ hợp tác trên PostgreSQL thật qua module pf/extension
// (vai r_extension). Kiểm thử đặt ở tests/ (ngoài module KN) vì phải tạo HTX nguồn bằng SQL và đếm bảng HTX — mã KN
// không được nhắc bảng HTX. HTX nguồn tạo ở pf_party bằng vai quản trị (như HTX có sẵn), KN chỉ đọc qua publication
// pf_party_pub.v1_org_tree. Ngữ cảnh phiên KN đi qua middleware X-AG-Context + projection phiên của KN (như
// tests/knattrs) — không đặt Principal thẳng vào handler.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

var t0 = time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)

// units là danh mục đơn vị hành chính giả: 31117, 31120 là xã; 89 là tỉnh.
type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	switch code {
	case "31117", "31120", "31123":
		return partyport.AdminUnit{Code: code, Name: "Xã " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "89":
		return partyport.AdminUnit{Code: code, Name: "An Giang", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	// HTX đã công bố ở xã 31117 (trong phạm vi), 31120 (ngoài phạm vi Cấp Xã), HTX ngừng hoạt động; tổ khuyến nông.
	htx, htxOther, htxInactive, team uuid.UUID
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := extension.New(pool, clock.NewFixed(t0), extension.Deps{Units: units{}})
	mod.UseKNRecordsVault(clienttest.New())
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	f.seed()
	return f
}

func (f *fix) admin(sql string, args ...any) {
	f.t.Helper()
	if _, err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("seed: %v\n%s", err, sql)
	}
}

func (f *fix) org(ref uuid.UUID, kind partyport.OrgKind, unit, name string, active bool) {
	f.admin(`INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, active, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1)`, f.tenant, ref, string(kind), "O"+ref.String()[24:], name, unit, active, ids.New())
}

func (f *fix) seed() {
	f.htx, f.htxOther, f.htxInactive, f.team = ids.New(), ids.New(), ids.New(), ids.New()
	f.org(f.htx, partyport.OrgCooperative, "31117", "HTX Nông nghiệp Bình Hòa", true)
	f.org(f.htxOther, partyport.OrgCooperative, "31120", "HTX Xã Khác", true)
	f.org(f.htxInactive, partyport.OrgCooperative, "31117", "HTX Đã Ngừng", false)
	f.org(f.team, partyport.OrgExtensionTeam, "31117", "Tổ khuyến nông 31117", true)
	f.admin(`INSERT INTO pf_coop.cooperative (tenant_id, org_ref) VALUES ($1, $2), ($1, $3)`, f.tenant, f.htx, f.htxOther)
}

func (f *fix) count() int {
	f.t.Helper()
	var n int
	if err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		"SELECT count(*) FROM pf_extension.kn_producer WHERE tenant_id = $1", f.tenant).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// htxSnapshot đếm dòng của MỌI bảng thuộc schema HTX (pf_party, pf_coop, pf_mechanization) — theo tenant khi bảng có
// tenant_id. Dùng chứng minh luồng KN không ghi dữ liệu HTX.
func (f *fix) htxSnapshot() map[string]int {
	f.t.Helper()
	ctx := context.Background()
	adm := pgtest.Get(f.t).AdminPool(f.t, migrations.PF)
	rows, err := adm.Query(ctx, `SELECT c.relnamespace::regnamespace::text, c.relname,
	    EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
	  FROM pg_class c
	  WHERE c.relnamespace::regnamespace::text IN ('pf_party', 'pf_coop', 'pf_mechanization')
	    AND c.relkind IN ('r', 'p') AND NOT c.relispartition
	  ORDER BY 1, 2`)
	if err != nil {
		f.t.Fatal(err)
	}
	type tbl struct {
		schema, name string
		tenant       bool
	}
	var ts []tbl
	for rows.Next() {
		var x tbl
		if err := rows.Scan(&x.schema, &x.name, &x.tenant); err != nil {
			f.t.Fatal(err)
		}
		ts = append(ts, x)
	}
	rows.Close()
	if len(ts) < 5 {
		f.t.Fatalf("không thấy bảng HTX để đối chiếu: %v", ts)
	}
	out := map[string]int{}
	for _, x := range ts {
		q := fmt.Sprintf(`SELECT count(*) FROM %q.%q`, x.schema, x.name)
		var args []any
		if x.tenant {
			q += " WHERE tenant_id = $1"
			args = append(args, f.tenant)
		}
		var n int
		if err := adm.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			f.t.Fatalf("%s.%s: %v", x.schema, x.name, err)
		}
		out[x.schema+"."+x.name] = n
	}
	return out
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		f.t.Fatal(err)
	}
	return func(c *gin.Context) {
		raw := c.GetHeader(authz.ContextHeader)
		if raw == "" {
			c.Next()
			return
		}
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(raw)
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		selected, err := registry.ResolveSelected(c.Request.Context(), p)
		if err != nil {
			f.t.Errorf("owner session projection: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		p.SelectedSession = &selected
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
}

func (f *fix) user(tid uuid.UUID, role string, units ...string) user {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: "trong_trot"}
	switch role {
	case authz.KNXa:
		kc.Communes, kc.Scope = units, authz.Scope{Units: units}
		kc.OrgRef = f.team
	case authz.KNTW:
		kc.Scope = authz.Scope{All: true}
	case authz.KNAdmin:
		kc.Scope, kc.Department = authz.Scope{All: true}, ""
	default:
		kc.Scope = authz.Scope{Units: units}
	}
	f.mu.Lock()
	f.ctxs[kc.ID] = kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID}
}

func (u user) revoke() {
	u.f.mu.Lock()
	delete(u.f.ctxs, u.ctxID)
	u.f.mu.Unlock()
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (u user) do(method, path, key string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	if method == http.MethodPost {
		if key == "" {
			key = "k-" + ids.New().String()
		}
		r.Header.Set(idempotency.Header, key)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: res.StatusCode, raw: raw, body: m}
}

func (u user) get(p string) resp         { return u.do(http.MethodGet, p, "", nil) }
func (u user) post(p string, b any) resp { return u.do(http.MethodPost, p, "", b) }
func str(m any, k string) string         { mm, _ := m.(map[string]any); s, _ := mm[k].(string); return s }
func num(m any, k string) float64        { mm, _ := m.(map[string]any); n, _ := mm[k].(float64); return n }
func obj(m any, k string) map[string]any {
	mm, _ := m.(map[string]any)
	o, _ := mm[k].(map[string]any)
	return o
}
func items(r resp) []map[string]any {
	l, _ := r.body["items"].([]any)
	out := []map[string]any{}
	for _, x := range l {
		if o, ok := x.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}

func want(t *testing.T, r resp, status int, c, what string) {
	t.Helper()
	if r.status != status || (c != "" && str(r.body, "code") != c) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, c, r.status, r.raw)
	}
}

func fieldErr(t *testing.T, r resp, field string) {
	t.Helper()
	errs, _ := r.body["errors"].([]any)
	for _, e := range errs {
		if str(e, "field") == field {
			return
		}
	}
	t.Fatalf("thiếu lỗi trường %s: %s", field, r.raw)
}

const base = "/v1/kn/producer-records"

func coop(name, commune, phone string) map[string]any {
	return map[string]any{"kind": "cooperative", "commune_code": commune, "name": name,
		"representative": map[string]any{"full_name": "Nguyễn Văn An", "phone": phone}}
}

func hh(commune, phone string, parent string) map[string]any {
	b := map[string]any{"kind": "household", "commune_code": commune, "holder": map[string]any{"full_name": "Trần Thị Bích", "phone": phone}}
	if parent != "" {
		b["parent_ref"] = parent
	}
	return b
}

// TT-08: bắt buộc Xã/phường, Loại, Tên, Người đại diện, Điện thoại; hồ sơ lưu ở sổ KN, đọc lại thật; SĐT che.
func TestKNRecords_CreateValidateReadBack(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, "31117")

	r := xa.post(base, map[string]any{})
	want(t, r, http.StatusUnprocessableEntity, "validation_failed", "thiếu loại")
	fieldErr(t, r, "kind")
	r = xa.post(base, map[string]any{"kind": "cooperative"})
	want(t, r, http.StatusUnprocessableEntity, "validation_failed", "thiếu trường")
	for _, fld := range []string{"commune_code", "name", "representative.full_name", "representative.phone"} {
		fieldErr(t, r, fld)
	}
	r = xa.post(base, map[string]any{"kind": "cooperative", "commune_code": "31117", "name": "HTX A",
		"representative": map[string]any{"full_name": "Lê Văn C", "phone": "12345"}})
	want(t, r, http.StatusUnprocessableEntity, "validation_failed", "SĐT sai")
	fieldErr(t, r, "representative.phone")
	// Số do người dùng nhập (§4): số mũ, âm, rất dài, quá lớn — chặn trước decimal.
	for _, a := range []any{"1e100000", "1e-100000", "-1", strings.Repeat("9", 5000), "1000001", "1.23456", json.Number("1e5")} {
		b := coop("HTX Số", "31117", "0912345678")
		b["area_ha"] = a
		r = xa.post(base, b)
		want(t, r, http.StatusUnprocessableEntity, "validation_failed", fmt.Sprintf("diện tích %.20v", a))
		fieldErr(t, r, "area_ha")
	}
	if n := f.count(); n != 0 {
		t.Fatalf("lỗi vẫn ghi sổ: %d", n)
	}

	b := coop("HTX Thuận Phát", "31117", "0912345678")
	b["area_ha"], b["tax_code"], b["address"] = json.Number("125.5"), "1801234567", "Ấp 3"
	r = xa.post(base, b)
	want(t, r, http.StatusCreated, "", "tạo HTX")
	ref := str(r.body, "ref")
	if str(r.body, "kind") != "cooperative" || str(r.body, "kind_label") != "HTX" || str(r.body, "area_ha") != "125.5" ||
		str(r.body, "status") != "active" || num(r.body, "version") != 1 || r.body["can_edit"] != true {
		t.Fatalf("HTX sau tạo: %s", r.raw)
	}
	rep := obj(r.body, "representative")
	if str(rep, "label") == "" || !strings.Contains(str(rep, "masked_phone"), "•••") || strings.Contains(string(r.raw), "0912345678") {
		t.Fatalf("người đại diện phải che: %s", r.raw)
	}
	g := xa.get(base + "/" + ref)
	want(t, g, http.StatusOK, "", "đọc lại")
	if str(g.body, "name") != "HTX Thuận Phát" || str(g.body, "tax_code") != "1801234567" || len(g.body["log"].([]any)) != 1 {
		t.Fatalf("đọc lại: %s", g.raw)
	}
	// Trùng tên trong xã (khác hoa thường, thừa khoảng trắng) → 409; ngoài phạm vi → 403.
	want(t, xa.post(base, coop("  htx  THUẬN phát ", "31117", "0912000111")), http.StatusConflict, "extension.kn_record_name_taken", "trùng tên")
	want(t, xa.post(base, coop("HTX Xã Khác", "31120", "0912000222")), http.StatusForbidden, "extension.out_of_scope", "ngoài phạm vi")
	if n := f.count(); n != 1 {
		t.Fatalf("sổ: %d", n)
	}
	// Idempotency: lặp cùng khóa → cùng hồ sơ, không ghi thêm.
	key := "same-key-" + ids.New().String()
	r1 := xa.do(http.MethodPost, base, key, coop("THT Ấp 5", "31117", "0912000333"))
	r2 := xa.do(http.MethodPost, base, key, coop("THT Ấp 5", "31117", "0912000333"))
	if r1.status != http.StatusCreated || str(r1.body, "ref") == "" || str(r2.body, "ref") != str(r1.body, "ref") {
		t.Fatalf("idempotent: %d %s / %d %s", r1.status, r1.raw, r2.status, r2.raw)
	}
	if n := f.count(); n != 2 {
		t.Fatalf("idempotent ghi thêm: %d", n)
	}
}

// TT-09: ba danh sách riêng (HTX, Nông hộ, Tổ hợp tác), mỗi danh sách có bộ lọc riêng; Nông hộ thuộc HTX/THT.
func TestKNRecords_ThreeListsAndMembership(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, "31117", "31123")
	c := xa.post(base, coop("HTX Bình Minh", "31117", "0913000001"))
	want(t, c, http.StatusCreated, "", "HTX")
	g := xa.post(base, map[string]any{"kind": "coop_group", "commune_code": "31123", "name": "Tổ hợp tác Rau sạch",
		"representative": map[string]any{"full_name": "Phạm Văn Dũng", "phone": "0913000002"}})
	want(t, g, http.StatusCreated, "", "THT")
	if str(g.body, "kind_label") != "Tổ hợp tác" {
		t.Fatalf("THT: %s", g.raw)
	}
	want(t, xa.post(base, map[string]any{"kind": "coop_group", "commune_code": "31123", "name": "THT", "tax_code": "1801234567",
		"representative": map[string]any{"full_name": "Phạm Văn E", "phone": "0913000009"}}), http.StatusUnprocessableEntity, "validation_failed", "MST cho THT")
	h1 := xa.post(base, hh("31117", "0913000003", str(c.body, "ref")))
	want(t, h1, http.StatusCreated, "", "nông hộ thuộc HTX")
	if str(h1.body, "parent_name") != "HTX Bình Minh" || h1.body["name"] != nil || str(h1.body, "display_name") == "" || obj(h1.body, "holder") == nil {
		t.Fatalf("nông hộ: %s", h1.raw)
	}
	want(t, xa.post(base, hh("31123", "0913000004", str(g.body, "ref"))), http.StatusCreated, "", "nông hộ thuộc THT")
	want(t, xa.post(base, hh("31117", "0913000005", "")), http.StatusCreated, "", "nông hộ độc lập")
	// Nông hộ: tên ở kho định danh (không nhận name), cùng chủ hộ → 409, cha phải là HTX/THT.
	bad := hh("31117", "0913000006", "")
	bad["name"] = "Hộ ông Ba"
	want(t, xa.post(base, bad), http.StatusUnprocessableEntity, "validation_failed", "nông hộ có name")
	want(t, xa.post(base, hh("31117", "0913000003", "")), http.StatusConflict, "extension.kn_record_household_exists", "trùng chủ hộ")
	r := xa.post(base, hh("31117", "0913000007", str(h1.body, "ref")))
	want(t, r, http.StatusUnprocessableEntity, "validation_failed", "cha là nông hộ")
	fieldErr(t, r, "parent_ref")

	if l := items(xa.get(base + "?kind=cooperative")); len(l) != 1 || num(l[0], "member_count") != 1 {
		t.Fatalf("danh sách HTX: %v", l)
	}
	if l := items(xa.get(base + "?kind=coop_group")); len(l) != 1 || num(l[0], "member_count") != 1 {
		t.Fatalf("danh sách THT: %v", l)
	}
	if l := items(xa.get(base + "?kind=household")); len(l) != 3 {
		t.Fatalf("danh sách Nông hộ: %v", l)
	}
	if l := items(xa.get(base + "?kind=household&commune=31123")); len(l) != 1 {
		t.Fatalf("lọc xã: %v", l)
	}
	if l := items(xa.get(base + "?kind=household&parent_ref=" + str(c.body, "ref"))); len(l) != 1 || str(l[0], "ref") != str(h1.body, "ref") {
		t.Fatalf("lọc HTX/THT: %v", l)
	}
	if l := items(xa.get(base + "?kind=cooperative&q=bình")); len(l) != 1 {
		t.Fatalf("tìm tên (không phân biệt hoa thường): %v", l)
	}
	if l := items(xa.get(base + "?kind=cooperative&q=khong-co")); len(l) != 0 {
		t.Fatalf("tìm tên: %v", l)
	}
	want(t, xa.get(base+"?kind=household&q=Bích"), http.StatusUnprocessableEntity, "validation_failed", "tìm tên nông hộ")
	want(t, xa.get(base), http.StatusUnprocessableEntity, "validation_failed", "thiếu kind")
	want(t, xa.get(base+"?kind=cooperative&commune=31120"), http.StatusForbidden, "extension.out_of_scope", "lọc xã ngoài phạm vi")
	want(t, xa.get(base+"?kind=cooperative&q="+strings.Repeat("a", 101)), http.StatusUnprocessableEntity, "validation_failed", "q dài")
	want(t, xa.get(base+"?kind=cooperative&limit=100000"), http.StatusUnprocessableEntity, "validation_failed", "limit lớn")
}

// TT-08 "có sửa": khóa lạc quan, không đổi loại, không đổi → không ghi; vô hiệu hóa thay xóa, kích hoạt lại.
func TestKNRecords_UpdateAndStatus(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, "31117")
	c := xa.post(base, coop("HTX Phú Lợi", "31117", "0914000001"))
	want(t, c, http.StatusCreated, "", "tạo")
	ref := str(c.body, "ref")
	path := base + "/" + ref

	upd := coop("HTX Phú Lợi Mới", "31117", "0914000001")
	delete(upd, "representative")
	upd["base_version"] = 5
	want(t, xa.post(path, upd), http.StatusConflict, "extension.kn_record_version_conflict", "sai phiên bản")
	upd["base_version"] = 1
	upd["kind"] = "coop_group"
	want(t, xa.post(path, upd), http.StatusUnprocessableEntity, "validation_failed", "đổi loại")
	delete(upd, "kind")
	upd["area_ha"] = "1e3"
	want(t, xa.post(path, upd), http.StatusUnprocessableEntity, "validation_failed", "diện tích số mũ")
	upd["area_ha"] = "80.25"
	r := xa.post(path, upd)
	want(t, r, http.StatusOK, "", "sửa")
	if str(r.body, "name") != "HTX Phú Lợi Mới" || str(r.body, "area_ha") != "80.25" || num(r.body, "version") != 2 {
		t.Fatalf("sau sửa: %s", r.raw)
	}
	repBefore := str(obj(r.body, "representative"), "masked_phone")
	// Gửi lại y nguyên (khóa mới) → không ghi.
	upd["base_version"] = 2
	r = xa.post(path, upd)
	want(t, r, http.StatusOK, "", "gửi lại")
	if num(r.body, "version") != 2 || f.count() != 2 {
		t.Fatalf("không đổi vẫn ghi: %s (sổ %d)", r.raw, f.count())
	}
	// Đổi người đại diện → chủ thể mới ở kho định danh.
	upd["representative"] = map[string]any{"full_name": "Võ Thị Hoa", "phone": "0914999888"}
	r = xa.post(path, upd)
	want(t, r, http.StatusOK, "", "đổi người đại diện")
	if str(obj(r.body, "representative"), "masked_phone") == repBefore || num(r.body, "version") != 3 {
		t.Fatalf("đổi người đại diện: %s", r.raw)
	}

	want(t, xa.post(path+"/deactivate", map[string]any{"reason": "ngắn", "base_version": 3}), http.StatusUnprocessableEntity, "validation_failed", "lý do ngắn")
	want(t, xa.post(path+"/deactivate", map[string]any{"reason": "gọi 0914999888 để hỏi", "base_version": 3}), http.StatusUnprocessableEntity, "validation_failed", "lý do có SĐT")
	r = xa.post(path+"/deactivate", map[string]any{"reason": "HTX giải thể theo quyết định của xã", "base_version": 3})
	want(t, r, http.StatusOK, "", "vô hiệu hóa")
	if str(r.body, "status") != "inactive" || str(r.body, "status_reason") == "" {
		t.Fatalf("vô hiệu hóa: %s", r.raw)
	}
	if l := items(xa.get(base + "?kind=cooperative")); len(l) != 0 {
		t.Fatalf("mặc định chỉ hoạt động: %v", l)
	}
	if l := items(xa.get(base + "?kind=cooperative&status=inactive")); len(l) != 1 {
		t.Fatalf("lọc ngừng: %v", l)
	}
	upd["base_version"] = 4
	want(t, xa.post(path, upd), http.StatusConflict, "extension.kn_record_state_conflict", "sửa khi ngừng")
	want(t, xa.post(path+"/deactivate", map[string]any{"reason": "HTX giải thể theo quyết định của xã", "base_version": 4}), http.StatusConflict, "extension.kn_record_state_conflict", "vô hiệu hai lần")
	r = xa.post(path+"/reactivate", map[string]any{"reason": "HTX hoạt động lại theo đề nghị", "base_version": 4})
	want(t, r, http.StatusOK, "", "kích hoạt lại")
	if str(r.body, "status") != "active" || len(r.body["log"].([]any)) != 5 {
		t.Fatalf("kích hoạt lại: %s", r.raw)
	}
}

// Quyền, phạm vi, tenant, phiên: Cấp Tỉnh/Quốc gia chỉ xem; ngoài phạm vi 404; tenant khác không thấy; phiên thu hồi 403.
func TestKNRecords_RolesScopeTenantSession(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, "31117")
	c := xa.post(base, coop("HTX Quyền", "31117", "0915000001"))
	want(t, c, http.StatusCreated, "", "tạo")
	path := base + "/" + str(c.body, "ref")

	tinh := f.user(f.tenant, authz.KNTinh, "31117", "31120")
	if l := items(tinh.get(base + "?kind=cooperative")); len(l) != 1 || l[0]["can_edit"] != false {
		t.Fatalf("Tỉnh xem: %v", l)
	}
	want(t, tinh.post(base, coop("HTX Tỉnh", "31117", "0915000002")), http.StatusForbidden, "", "Tỉnh không ghi")
	tw := f.user(f.tenant, authz.KNTW)
	want(t, tw.get(path), http.StatusOK, "", "TW xem")
	want(t, tw.post(base, coop("HTX TW", "31117", "0915000003")), http.StatusForbidden, "extension.read_only", "TW chỉ xem")

	other := f.user(f.tenant, authz.KNXa, "31120")
	want(t, other.get(path), http.StatusNotFound, "extension.kn_record_not_found", "xã khác không thấy")
	want(t, other.post(path+"/deactivate", map[string]any{"reason": "không phải xã của tôi", "base_version": 1}), http.StatusNotFound, "extension.kn_record_not_found", "xã khác không ghi")
	if l := items(other.get(base + "?kind=cooperative")); len(l) != 0 {
		t.Fatalf("xã khác lộ dữ liệu: %v", l)
	}
	foreign := f.user(ids.New(), authz.KNXa, "31117")
	want(t, foreign.get(path), http.StatusNotFound, "extension.kn_record_not_found", "tenant khác")
	if l := items(foreign.get(base + "?kind=cooperative")); len(l) != 0 {
		t.Fatalf("tenant khác lộ dữ liệu: %v", l)
	}
	adm := f.user(f.tenant, authz.KNAdmin)
	want(t, adm.post(base, coop("HTX Admin", "99999", "0915000004")), http.StatusUnprocessableEntity, "validation_failed", "Admin mã xã lạ")
	want(t, adm.post(base, coop("HTX Admin", "89", "0915000004")), http.StatusUnprocessableEntity, "validation_failed", "Admin mã tỉnh")
	want(t, adm.post(base, coop("HTX Admin", "31120", "0915000004")), http.StatusCreated, "", "Admin tạo")
	xa.revoke()
	want(t, xa.get(path), http.StatusForbidden, "", "phiên thu hồi")
}

// HTX đã công bố chỉ ĐỌC qua publication: danh sách ứng viên liên kết, liên kết đối chiếu htx_ref.
func TestKNRecords_PublishedHTXLink(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, "31117")
	r := xa.get("/v1/kn/producer-htx-candidates")
	want(t, r, http.StatusOK, "", "ứng viên")
	if l := items(r); len(l) != 1 || str(l[0], "htx_ref") != f.htx.String() || l[0]["linked_ref"] != nil {
		t.Fatalf("ứng viên (chỉ HTX đang hoạt động trong phạm vi): %s", r.raw)
	}
	want(t, xa.get("/v1/kn/producer-htx-candidates?commune=31120"), http.StatusForbidden, "extension.out_of_scope", "ứng viên ngoài phạm vi")
	b := coop("HTX Nông nghiệp Bình Hòa", "31117", "0916000001")
	b["htx_ref"] = f.htxOther.String()
	want(t, xa.post(base, b), http.StatusUnprocessableEntity, "validation_failed", "HTX xã khác")
	b["htx_ref"] = f.htxInactive.String()
	want(t, xa.post(base, b), http.StatusUnprocessableEntity, "validation_failed", "HTX ngừng")
	b["htx_ref"] = f.team.String()
	want(t, xa.post(base, b), http.StatusUnprocessableEntity, "validation_failed", "không phải HTX")
	b["htx_ref"] = f.htx.String()
	c := xa.post(base, b)
	want(t, c, http.StatusCreated, "", "liên kết HTX đã công bố")
	if str(c.body, "htx_ref") != f.htx.String() || str(c.body, "htx_name") != "HTX Nông nghiệp Bình Hòa" {
		t.Fatalf("liên kết: %s", c.raw)
	}
	if l := items(xa.get("/v1/kn/producer-htx-candidates")); len(l) != 1 || str(l[0], "linked_ref") != str(c.body, "ref") {
		t.Fatalf("ứng viên đã liên kết: %v", l)
	}
	if l := items(xa.get(base + "?kind=cooperative&htx_ref=" + f.htx.String())); len(l) != 1 {
		t.Fatalf("lọc theo htx_ref: %v", l)
	}
	hb := hh("31117", "0916000002", "")
	hb["htx_ref"] = f.htx.String()
	want(t, xa.post(base, hb), http.StatusUnprocessableEntity, "validation_failed", "htx_ref cho nông hộ")
}

// Hồi quy CORE RULE: mọi thao tác hồ sơ KN (tạo, sửa, vô hiệu, kích hoạt, liên kết HTX đã công bố, đọc) KHÔNG ghi một
// dòng nào vào bảng HTX; và vai r_extension không có quyền ghi bảng HTX nào.
func TestKNRecords_NoWriteToHTXTables(t *testing.T) {
	f := newFix(t)
	before := f.htxSnapshot()
	xa := f.user(f.tenant, authz.KNXa, "31117")
	b := coop("HTX Ranh Giới", "31117", "0917000001")
	b["htx_ref"] = f.htx.String()
	c := xa.post(base, b)
	want(t, c, http.StatusCreated, "", "tạo HTX liên kết")
	ref := str(c.body, "ref")
	want(t, xa.post(base, hh("31117", "0917000002", ref)), http.StatusCreated, "", "tạo nông hộ")
	want(t, xa.post(base, map[string]any{"kind": "coop_group", "commune_code": "31117", "name": "THT Ranh Giới",
		"representative": map[string]any{"full_name": "Đỗ Văn Khang", "phone": "0917000003"}}), http.StatusCreated, "", "tạo THT")
	upd := coop("HTX Ranh Giới 2", "31117", "0917000004")
	upd["base_version"], upd["htx_ref"] = 1, f.htx.String()
	want(t, xa.post(base+"/"+ref, upd), http.StatusOK, "", "sửa")
	want(t, xa.post(base+"/"+ref+"/deactivate", map[string]any{"reason": "kiểm ranh giới không ghi HTX", "base_version": 2}), http.StatusOK, "", "vô hiệu")
	want(t, xa.post(base+"/"+ref+"/reactivate", map[string]any{"reason": "kiểm ranh giới không ghi HTX", "base_version": 3}), http.StatusOK, "", "kích hoạt")
	want(t, xa.get(base+"/"+ref), http.StatusOK, "", "đọc")
	want(t, xa.get("/v1/kn/producer-htx-candidates"), http.StatusOK, "", "ứng viên")
	if n := f.count(); n != 6 {
		t.Fatalf("sổ KN: %d", n)
	}
	after := f.htxSnapshot()
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("luồng KN đã ghi bảng HTX %s: %d → %d", k, v, after[k])
		}
	}
	if len(after) != len(before) {
		t.Fatalf("số bảng HTX đổi: %d → %d", len(before), len(after))
	}
	var bad []string
	rows, err := pgtest.Get(t).AdminPool(t, migrations.PF).Query(context.Background(), `SELECT n.nspname || '.' || c.relname
	  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	  WHERE n.nspname IN ('pf_party', 'pf_coop', 'pf_mechanization') AND c.relkind IN ('r', 'p')
	    AND (has_table_privilege('r_extension', c.oid, 'INSERT') OR has_table_privilege('r_extension', c.oid, 'UPDATE')
	      OR has_table_privilege('r_extension', c.oid, 'DELETE'))`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		bad = append(bad, s)
	}
	rows.Close()
	if len(bad) > 0 {
		t.Fatalf("r_extension có quyền ghi bảng HTX: %v", bad)
	}
}
