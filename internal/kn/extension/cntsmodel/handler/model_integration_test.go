package handler_test

// W11.47 (D4 CN7, BRD CNTS v3.7 FN-10 phần hộ, FN-14, FN-19 phần CT/DA, UR-XA-04, UR-TINH-09, UR-TINH-19): CT/DA, điểm
// trình diễn, hộ mô hình, cấp quyền CNTS trên PostgreSQL thật qua module pf/extension (vai extension_app). Ngữ cảnh phiên KN
// giả lập bằng middleware X-AG-Context; danh mục CNTS, đơn vị hành chính, danh bạ Cấp Xã, kho định danh là bản giả.

import (
	"bytes"
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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
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

const cnts = "chan_nuoi_thuy_san"

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

var fac1 = uuid.MustParse("0192a000-0000-7000-8000-000000000001") // cơ sở giết mổ xã 30001

type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	switch code {
	case "30001", "30002":
		return partyport.AdminUnit{Code: code, Name: "Xã " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "31001":
		return partyport.AdminUnit{Code: code, Name: "Xã tỉnh khác", Level: partyport.LevelCommune, ParentCode: "92"}, nil
	case "89", "92":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

type catalogs struct{ knport.CNTSCatalogs }

var items = map[string]mdport.CatalogItem{
	"cnts_model_type:GA_TP": {Catalog: "cnts_model_type", Code: "GA_TP", Name: "Gà thương phẩm", Active: true},
}

func (catalogs) ItemAt(_ context.Context, catalog, code string, _ time.Time) (mdport.CatalogItem, error) {
	if it, ok := items[catalog+":"+code]; ok {
		return it, nil
	}
	return mdport.CatalogItem{}, fmt.Errorf("giả: %w", mdport.ErrCatalogItemNotFound)
}

func (catalogs) ListAt(_ context.Context, catalog string, _ time.Time) ([]mdport.CatalogItem, error) {
	out := []mdport.CatalogItem{}
	for _, it := range items {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

func (catalogs) FacilitiesAt(context.Context, time.Time, []string) ([]knport.SlaughterFacility, error) {
	return []knport.SlaughterFacility{{Ref: fac1, Code: "CS1", Name: "Lò mổ 1", CommuneCode: "30001", Kind: knport.FacilityConcentrated, Active: true}}, nil
}

type xaScopes struct {
	mu   sync.Mutex
	list []accport.XaScope
}

func (x *xaScopes) XaScopes(context.Context, uuid.UUID, string) ([]accport.XaScope, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.list, nil
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	pool   *pgxpool.Pool
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	xa     *xaScopes
	vault  *clienttest.Fake
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}, xa: &xaScopes{}, vault: clienttest.New()}
	f.pool = env.AdminPool(t, migrations.PF)
	mod := extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(t0), extension.Deps{Units: units{}})
	mod.UseCNTSCatalogs(catalogs{})
	mod.UseXaScopes(f.xa)
	mod.UseCNTSModelVault(f.vault)
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		f.t.Fatal(err)
	}
	return func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(c.GetHeader(authz.ContextHeader))
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
		scope, _ := json.Marshal(cp)
		ctx := idempotency.WithAuthorizationScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
	ref   uuid.UUID
}

func (f *fix) user(kc authz.KNContext) user {
	sub := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID, ref: sub}
}

func tinh(group string) authz.KNContext {
	return authz.KNContext{Role: authz.KNTinh, Department: cnts, ProvinceGroup: group, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}}
}

func xa(communes ...string) authz.KNContext {
	return authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: communes, Scope: authz.Scope{Units: communes}}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (u user) do(method, path, idem string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return resp{status: res.StatusCode, raw: raw, body: m}
}

func key() string                            { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp     { return u.do(http.MethodPost, p, key(), b) }
func (u user) postK(p, k string, b any) resp { return u.do(http.MethodPost, p, k, b) }
func (u user) get(p string) resp             { return u.do(http.MethodGet, p, "", nil) }
func str(m map[string]any, k string) string  { s, _ := m[k].(string); return s }
func num(m map[string]any, k string) string  { n, _ := m[k].(json.Number); return n.String() }
func list(m map[string]any, k string) []any  { l, _ := m[k].([]any); return l }

func want(t *testing.T, r resp, status int, code, what string) map[string]any {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
	return r.body
}

func (f *fix) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// seedProgram ghi một CT/DA chủ của KHTC (D20) để dự án CNTS gắn vào.
func (f *fix) seedProgram(code string) uuid.UUID {
	ref := ids.New()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO pf_extension.khtc_program (id, tenant_id, stream_key, program_ref,
	    code, name, kind, status, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, 'Chương trình KHTC', 'project', 'active', now(), $6, $7, 'test')`,
		ids.New(), f.tenant, "program:"+ref.String(), ref, code, ids.New(), "seed-"+ref.String()); err != nil {
		f.t.Fatal(err)
	}
	return ref
}

const base = "/v1/extension/cnts/"

func TestCNTSModel_W1147(t *testing.T) {
	f := newFix(t)
	chu := f.user(tinh("chu_nhiem"))
	chu2 := f.user(tinh("chu_nhiem"))
	leader := f.user(tinh("lanh_dao_phong"))
	viewer := f.user(tinh("vien_chuc"))
	tongHop := f.user(tinh("tong_hop"))
	officer := f.user(xa("30001"))  // cán bộ theo dõi điểm
	follower := f.user(xa("30001")) // được cấp quyền theo dõi mô hình
	heir := f.user(xa("30001"))     // nhận việc khi thu hồi
	other := f.user(xa("30002"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: follower.ref, Communes: []string{"30001"}},
		{ActorRef: heir.ref, Communes: []string{"30001"}}, {ActorRef: other.ref, Communes: []string{"30002"}}}

	// ----- CT/DA (UR-TINH-19, FN-19 BR-01, AC-04) -----
	prj := map[string]any{"code": "da-ga-2027", "name": "Mô hình gà an toàn sinh học", "kind": "knqg", "basis": "QĐ 123/QĐ-BNN",
		"scale_note": "3 điểm, 15 hộ", "start_date": "2027-03-01", "end_date": "2027-12-31", "budget": "450000000",
		"training_note": "2 lớp tập huấn", "supplies_note": "Giống gà, thức ăn, vaccine"}
	want(t, viewer.post(base+"projects", prj), 403, "", "viên chức không tạo dự án")
	want(t, chu.post(base+"projects", map[string]any{"code": "x", "name": "", "kind": "khac", "start_date": "2027-03-01"}), 422,
		"extension.cnts_project_invalid", "dự án sai")
	want(t, chu.post(base+"projects", merge(prj, map[string]any{"program_ref": ids.New().String()})), 422, "extension.cnts_project_invalid",
		"CT/DA KHTC không có")
	prog := f.seedProgram("CT-KNQG-01")
	k := key()
	p := want(t, chu.postK(base+"projects", k, merge(prj, map[string]any{"program_ref": prog.String()})), 201, "", "tạo dự án")
	if str(p, "code") != "DA-GA-2027" || str(p, "manager_ref") != chu.ref.String() || str(p, "budget") != "450000000" ||
		str(p, "province_code") != "89" || p["can_edit"] != true || str(p["program"].(map[string]any), "code") != "CT-KNQG-01" {
		t.Fatalf("dự án: %s", p)
	}
	want(t, chu.postK(base+"projects", k, merge(prj, map[string]any{"program_ref": prog.String()})), 201, "", "gọi lặp")
	want(t, chu2.post(base+"projects", prj), 409, "extension.cnts_project_code_taken", "trùng mã")
	pref := str(p, "ref")
	upd := merge(prj, map[string]any{"base_version": 1, "name": "Mô hình gà ATSH 2027"})
	want(t, chu2.post(base+"projects/"+pref, upd), 403, "", "AC-04: chủ nhiệm khác không sửa")
	want(t, chu.post(base+"projects/"+pref, merge(upd, map[string]any{"manager_ref": chu2.ref.String()})), 422,
		"extension.cnts_project_invalid", "chủ nhiệm không tự đổi chủ nhiệm")
	u := want(t, chu.post(base+"projects/"+pref, upd), 200, "", "chủ nhiệm sửa dự án mình")
	if num(u, "version") != "2" || len(list(u, "versions")) != 2 {
		t.Fatalf("sửa: %s", u)
	}
	want(t, leader.post(base+"projects/"+pref, upd), 409, "extension.cnts_project_version_conflict", "khóa lạc quan")
	want(t, leader.post(base+"projects/"+pref, merge(upd, map[string]any{"base_version": 2, "manager_ref": chu.ref.String()})), 200, "",
		"lãnh đạo phòng sửa")
	if l := list(want(t, chu2.get(base+"projects"), 200, "", "danh sách"), "items"); len(l) != 1 || l[0].(map[string]any)["can_edit"] != false {
		t.Fatalf("chủ nhiệm khác thấy nhưng không sửa: %v", l)
	}
	if l := list(want(t, officer.get(base+"projects"), 200, "", "xã chưa có điểm"), "items"); len(l) != 0 {
		t.Fatalf("Cấp Xã thấy dự án chưa có điểm ở xã: %v", l)
	}

	// ----- Điểm trình diễn -----
	site := map[string]any{"project_ref": pref, "commune_code": "30001", "name": "Điểm ấp Bình Hòa", "model_type_code": "GA_TP",
		"scale": "1500", "scale_unit": "con", "officer_ref": officer.ref.String(), "diary_form": "BM-08",
		"location": map[string]any{"lat": 10.38, "lng": 105.43, "source": "map"}}
	want(t, chu.post(base+"demo-sites", merge(site, map[string]any{"commune_code": "31001"})), 403, "extension.out_of_scope", "xã ngoài tỉnh")
	want(t, chu.post(base+"demo-sites", merge(site, map[string]any{"model_type_code": "BO"})), 422, "extension.cnts_site_invalid", "loại mô hình")
	want(t, chu.post(base+"demo-sites", merge(site, map[string]any{"cooperative_ref": ids.New().String()})), 422, "extension.cnts_site_invalid",
		"HTX không có trong công bố")
	want(t, chu.post(base+"demo-sites", merge(site, map[string]any{"officer_ref": other.ref.String()})), 422, "extension.cnts_site_invalid",
		"cán bộ không phụ trách xã")
	want(t, chu2.post(base+"demo-sites", site), 403, "", "chủ nhiệm khác không thêm điểm")
	s := want(t, chu.post(base+"demo-sites", site), 201, "", "thêm điểm")
	sref := str(s, "ref")
	if str(s, "model_type_name") != "Gà thương phẩm" || s["location"] == nil || s["can_add_households"] != true {
		t.Fatalf("điểm: %s", s)
	}
	if l := list(want(t, officer.get(base+"demo-sites"), 200, "", "điểm của xã"), "items"); len(l) != 1 || l[0].(map[string]any)["can_add_households"] != true {
		t.Fatalf("Cấp Xã thấy điểm của xã: %v", l)
	}
	if l := list(want(t, other.get(base+"demo-sites"), 200, "", "điểm xã khác"), "items"); len(l) != 0 {
		t.Fatalf("xã khác thấy điểm: %v", l)
	}
	if l := list(want(t, officer.get(base+"projects"), 200, "", "Cấp Xã thấy dự án có điểm ở xã"), "items"); len(l) != 1 {
		t.Fatalf("Cấp Xã: %v", l)
	}

	// ----- Hộ mô hình (UR-XA-04) — chủ hộ ở kho định danh -----
	hh := map[string]any{"site_ref": sref, "owner_name": "Nguyễn Văn Bình", "owner_phone": "0912345678", "scale": "500", "scale_unit": "con",
		"keeper_kind": "officer", "keeper_ref": officer.ref.String(), "joined_on": "2027-03-05",
		"location": map[string]any{"lat": 10.381, "lng": 105.431, "source": "gps"}}
	want(t, other.post(base+"model-households", hh), 404, "extension.cnts_site_not_found", "xã khác không thấy điểm")
	want(t, follower.post(base+"model-households", hh), 403, "", "chưa được cấp quyền theo dõi")
	want(t, officer.post(base+"model-households", merge(hh, map[string]any{"note": "SĐT 0912345678"})), 422,
		"extension.cnts_household_invalid", "SĐT trong ghi chú")
	h := want(t, officer.post(base+"model-households", hh), 201, "", "cán bộ theo dõi điểm thêm hộ")
	href := str(h, "ref")
	if str(h, "owner_label") == "" || !strings.Contains(str(h, "owner_phone_masked"), "•••") || h["location"] == nil {
		t.Fatalf("hộ: %s", h)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_model_household WHERE tenant_id = $1 AND household_ref = $2
	    AND person_ref IS NOT NULL AND coalesce(note, '') NOT LIKE '%Bình%'`, f.tenant, href); n != 1 {
		t.Fatalf("sổ hộ chỉ giữ person_ref: %d", n)
	}
	want(t, chu.post(base+"model-households", merge(hh, map[string]any{"owner_name": "Nguyễn V. Bình"})), 409, "extension.cnts_household_exists",
		"cùng SĐT = cùng chủ thể trong điểm")
	if l := list(want(t, chu.get(base+"model-households?site_ref="+sref), 200, "", "hộ theo điểm"), "items"); len(l) != 1 {
		t.Fatalf("hộ theo điểm: %v", l)
	}

	// ----- Cấp quyền (FN-14) -----
	g := map[string]any{"grantee_ref": follower.ref.String(), "role": "theo_doi_mo_hinh", "commune_codes": []string{"30001"},
		"site_refs": []string{sref}, "forms": []string{"BM-08", "bm-07"}}
	want(t, viewer.post(base+"grants", g), 403, "", "viên chức không cấp quyền")
	want(t, chu.post(base+"grants", map[string]any{"grantee_ref": follower.ref.String(), "role": "ksgm", "commune_codes": []string{"30001"},
		"facility_refs": []string{fac1.String()}}), 403, "", "BR-03: chủ nhiệm chỉ cấp theo dõi mô hình/nông hộ")
	want(t, tongHop.post(base+"grants", map[string]any{"grantee_ref": follower.ref.String(), "role": "ksgm", "commune_codes": []string{"31001"}}),
		403, "extension.out_of_scope", "địa bàn ngoài tỉnh")
	want(t, tongHop.post(base+"grants", map[string]any{"grantee_ref": ids.New().String(), "role": "ksgm", "commune_codes": []string{"30001"}}),
		422, "extension.cnts_grant_invalid", "không phải tài khoản Cấp Xã")
	want(t, chu2.post(base+"grants", g), 403, "extension.out_of_scope", "BR-03: điểm của dự án khác")
	gr := want(t, chu.post(base+"grants", g), 201, "", "cấp quyền theo dõi mô hình")
	gref := str(gr, "ref")
	if str(gr, "status") != "active" || len(list(gr, "forms")) != 2 || list(gr, "forms")[0] != "BM-07" || str(gr, "valid_from") != "2027-10-13" {
		t.Fatalf("quyền: %s", gr)
	}
	want(t, chu.post(base+"grants", g), 409, "extension.cnts_grant_exists", "trùng vai còn hiệu lực")
	ks := want(t, tongHop.post(base+"grants", map[string]any{"grantee_ref": officer.ref.String(), "role": "ksgm", "commune_codes": []string{"30001"},
		"facility_refs": []string{fac1.String()}, "forms": []string{"BM-01"}, "valid_to": "2027-12-31"}), 201, "", "tổng hợp cấp KSGM")
	if str(ks, "valid_to") != "2027-12-31" {
		t.Fatalf("KSGM: %s", ks)
	}
	h2 := want(t, follower.post(base+"model-households", merge(hh, map[string]any{"owner_name": "Trần Thị Cúc", "owner_phone": "0987654321",
		"keeper_kind": "self", "keeper_ref": nil, "self_entry": true})), 201, "", "UR-XA-04: cán bộ được cấp quyền thêm hộ")
	h2ref := str(h2, "ref")
	if l := list(want(t, follower.get(base+"grants?mine=true"), 200, "", "quyền của tôi"), "items"); len(l) != 1 {
		t.Fatalf("quyền của tôi: %v", l)
	}
	want(t, follower.get(base+"grants"), 403, "", "Cấp Xã không xem quyền người khác")
	upg := want(t, chu.post(base+"grants/"+gref, map[string]any{"base_version": 1, "commune_codes": []string{"30001"},
		"site_refs": []string{sref}, "forms": []string{"BM-08"}}), 200, "", "sửa quyền")
	if num(upg, "version") != "2" || len(list(upg, "versions")) != 2 {
		t.Fatalf("sửa quyền: %s", upg)
	}
	want(t, chu.post(base+"grants/"+gref+"/revoke", map[string]any{"base_version": 2, "reason": "x"}), 422, "", "lý do ngắn")
	rv := want(t, chu.post(base+"grants/"+gref+"/revoke", map[string]any{"base_version": 2, "reason": "Chuyển công tác",
		"successor_ref": heir.ref.String()}), 200, "", "FN-14 AC-02: thu hồi, chuyển cho người mới")
	if str(rv, "status") != "revoked" || str(rv, "successor_ref") != heir.ref.String() || str(rv, "successor_grant_ref") == "" || rv["can_edit"] != false {
		t.Fatalf("thu hồi: %s", rv)
	}
	if l := list(want(t, heir.get(base+"grants?mine=true"), 200, "", "người nhận"), "items"); len(l) != 1 ||
		str(l[0].(map[string]any), "status") != "active" || len(list(l[0].(map[string]any), "site_refs")) != 1 {
		t.Fatalf("quyền người nhận: %v", l)
	}
	want(t, follower.post(base+"model-households/"+h2ref, map[string]any{"base_version": 1, "keeper_kind": "self", "self_entry": true,
		"scale": "600", "scale_unit": "con"}), 403, "", "quyền đã thu hồi")
	hu := want(t, heir.post(base+"model-households/"+h2ref, map[string]any{"base_version": 1, "keeper_kind": "self", "self_entry": true,
		"scale": "600", "scale_unit": "con"}), 200, "", "người nhận việc sửa hộ")
	if str(hu, "scale") != "600" || hu["location"] == nil {
		t.Fatalf("sửa hộ: %s", hu)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_grant WHERE tenant_id = $1`, f.tenant); n != 5 {
		t.Fatalf("BR-03 log mọi thay đổi quyền: %d dòng", n)
	}

	// ----- Rút hộ: xóa tọa độ (QĐ-8), sổ giữ lịch sử -----
	want(t, officer.post(base+"model-households/"+href+"/withdraw", map[string]any{"base_version": 1, "reason": "Hộ ngừng nuôi"}), 200, "", "rút hộ")
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_model_point WHERE tenant_id = $1 AND kind = 'household' AND ref = $2`, f.tenant, href); n != 0 {
		t.Fatalf("tọa độ hộ đã rút còn: %d", n)
	}
	if l := list(want(t, chu.get(base+"model-households?status=active"), 200, "", "hộ đang tham gia"), "items"); len(l) != 1 {
		t.Fatalf("lọc trạng thái: %v", l)
	}

	// ----- Đóng dự án khóa sửa điểm, hộ -----
	d := want(t, chu.get(base+"projects/"+pref), 200, "", "chi tiết")
	if len(list(d, "sites")) != 1 || num(d, "household_count") != "1" {
		t.Fatalf("chi tiết dự án: %s", d)
	}
	want(t, chu.post(base+"projects/"+pref+"/close", map[string]any{"base_version": 3, "reason": "Đã nghiệm thu"}), 200, "", "kết thúc dự án")
	want(t, chu.post(base+"demo-sites/"+sref, merge(site, map[string]any{"base_version": 1})), 409, "extension.cnts_project_closed", "dự án đã kết thúc")
	want(t, heir.post(base+"model-households/"+h2ref, map[string]any{"base_version": 2, "keeper_kind": "self"}), 409,
		"extension.cnts_site_closed", "hộ của dự án đã kết thúc")

	// ADR-0055, Q-KNS-04: KN không ghi dữ liệu HTX.
	// Đọc qua công bố pf_cooperative_pub (knboundary: mã KN không nhắc bảng pf_coop).
	if n := f.count(`SELECT (SELECT count(*) FROM pf_cooperative_pub.v1_cooperatives WHERE tenant_id = $1) +
	    (SELECT count(*) FROM pf_cooperative_pub.v1_plots WHERE tenant_id = $1)`, f.tenant); n != 0 {
		t.Fatalf("KN đã ghi dữ liệu HTX: %d", n)
	}
}

func merge(a map[string]any, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
