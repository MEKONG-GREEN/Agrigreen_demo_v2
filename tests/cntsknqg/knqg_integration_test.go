package cntsknqg_test

// W11.53 (D4 CN13, BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01): mẫu biểu dự án KNQG Mẫu 01, 03, 04, 05, 07 trên
// PostgreSQL thật qua module pf/extension (vai extension_app), pf/export THẬT (Word, số văn bản) và kho đối tượng cục bộ
// (bản scan đã ký). Dự án, điểm, hộ (W11.47) và nhật ký đã xác thực (W11.48) đi qua API của chính các gói đó.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const cnts = "chan_nuoi_thuy_san"

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	switch code {
	case "30001", "30002":
		return partyport.AdminUnit{Code: code, Name: "Xã Bình Hòa " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "89":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh An Giang", Level: partyport.LevelProvince}, nil
	case "92":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh khác", Level: partyport.LevelProvince}, nil
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
	var out []mdport.CatalogItem
	for _, it := range items {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

func (catalogs) FacilitiesAt(context.Context, time.Time, []string) ([]knport.SlaughterFacility, error) {
	return nil, nil
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
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}, xa: &xaScopes{}}
	f.pool = env.AdminPool(t, migrations.PF)
	clk := clock.NewFixed(t0)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := extension.New(pool, clk, extension.Deps{Units: units{}})
	mod.UseCNTSCatalogs(catalogs{})
	mod.UseXaScopes(f.xa)
	mod.UseCNTSModelVault(clienttest.New())
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mod.UseDeptLibrary(st)
	ex, err := export.New(pool, clk, export.Deps{Access: exportport.AccessFunc(func(context.Context) bool { return false }),
		Templates: []exportport.TemplateRegistration{extension.ExportTemplates()}})
	if err != nil {
		t.Fatal(err)
	}
	mod.UseExporter(ex.Exporter())
	registry, err := authz.NewSessionRegistry(mod.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mw := func(c *gin.Context) {
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
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		p.SelectedSession = &selected
		scope, _ := json.Marshal(cp)
		ctx := idempotency.WithAuthorizationScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		c.Next()
	}
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), mw, idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
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

func key() string { return "k-" + ids.New().String() }

func (u user) req(method, path, idem, ct string, body io.Reader) (int, []byte, http.Header) {
	r, _ := http.NewRequest(method, u.f.srv.URL+path, body)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw, res.Header
}

type resp struct {
	code int
	raw  []byte
	body map[string]any
}

func (u user) do(method, path, idem string, body any) resp {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	code, raw, _ := u.req(method, path, idem, "application/json", rd)
	out := resp{code: code, raw: raw}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (u user) post(path string, body any) resp { return u.do(http.MethodPost, path, key(), body) }
func (u user) get(path string) resp            { return u.do(http.MethodGet, path, "", nil) }

func (u user) upload(path string, base int64, name string, data []byte) resp {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("meta", fmt.Sprintf(`{"base_version": %d}`, base))
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()
	code, raw, _ := u.req(http.MethodPost, path, key(), w.FormDataContentType(), &buf)
	out := resp{code: code, raw: raw}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func want(t *testing.T, r resp, code int, problemCode, what string) map[string]any {
	t.Helper()
	if r.code != code {
		t.Fatalf("%s: mong %d, nhận %d %s", what, code, r.code, r.raw)
	}
	if problemCode != "" && r.body["code"] != problemCode {
		t.Fatalf("%s: mã lỗi %q, nhận %s", what, problemCode, r.raw)
	}
	return r.body
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprint(int64(v))
	}
	return ""
}

func ver(m map[string]any) int64 { return int64(m["version"].(float64)) }

func list(m map[string]any, k string) []any {
	l, _ := m[k].([]any)
	return l
}

func strsOf(m map[string]any, k string) []string {
	var out []string
	for _, x := range list(m, k) {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func has(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

var pdf = []byte("%PDF-1.4\n% bien ban da ky\n%%EOF\n")

const base = "/v1/extension/cnts/"

func TestCNTSKNQG_W1153(t *testing.T) {
	f := newFix(t)
	chu := f.user(tinh("chu_nhiem"))
	chu2 := f.user(tinh("chu_nhiem"))
	ld := f.user(tinh("lanh_dao_phong"))
	vc := f.user(tinh("vien_chuc"))
	bgdCtx := tinh("ban_giam_doc")
	bgdCtx.ViewOnly = true
	bgd := f.user(bgdCtx)
	tw := f.user(authz.KNContext{Role: authz.KNTW, Department: cnts, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})
	twOther := f.user(authz.KNContext{Role: authz.KNTW, Department: cnts, Scope: authz.Scope{Units: []string{"92"}}})
	officer := f.user(xa("30001"))
	xa2 := f.user(xa("30002"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: xa2.ref, Communes: []string{"30002"}}}

	// ----- Dự án KNQG, 2 điểm, hộ (W11.47) -----
	p := want(t, chu.post(base+"projects", map[string]any{"code": "KNQG-GA-2027", "name": "Mô hình gà an toàn sinh học", "kind": "knqg",
		"basis": "Hợp đồng 12/HĐ-KNQG", "start_date": "2027-03-01", "end_date": "2027-12-31", "budget": "450000000"}), 201, "", "dự án KNQG")
	pref := str(p, "ref")
	pt := want(t, chu.post(base+"projects", map[string]any{"code": "TINH-1", "name": "Dự án tỉnh", "kind": "tinh", "start_date": "2027-03-01"}),
		201, "", "dự án tỉnh")
	site := func(commune, name string) string {
		s := want(t, chu.post(base+"demo-sites", map[string]any{"project_ref": pref, "commune_code": commune, "name": name, "model_type_code": "GA_TP",
			"scale": "1500", "scale_unit": "con", "officer_ref": map[string]string{"30001": officer.ref.String(), "30002": xa2.ref.String()}[commune],
			"diary_form": "BM-08", "location": map[string]any{"lat": 10.38, "lng": 105.43, "source": "map"}}), 201, "", "điểm "+name)
		return str(s, "ref")
	}
	sA, sB := site("30001", "Điểm A"), site("30002", "Điểm B")
	hA := str(want(t, officer.post(base+"model-households", map[string]any{"site_ref": sA, "owner_name": "Nguyễn Văn Bình", "owner_phone": "0912345678",
		"scale": "500", "scale_unit": "con", "keeper_kind": "officer", "keeper_ref": officer.ref.String(),
		"location": map[string]any{"lat": 10.381, "lng": 105.431, "source": "gps"}}), 201, "", "hộ A"), "ref")

	// Nhật ký: một lứa 500 con, một lần ghi đã xác thực (UR-TINH-12) → thực tế Mẫu 05.
	cy := want(t, officer.post(base+"diary-cycles", map[string]any{"household_ref": hA, "breed": "Gà lai Mía", "received_date": "2027-10-01",
		"initial_count": 500, "age_days": 1}), 201, "", "lứa")
	en := want(t, officer.post(base+"diary-entries", map[string]any{"cycle_ref": str(cy, "ref"), "entry_kind": "daily",
		"captured_at": t0.Add(-24 * time.Hour).Format(time.RFC3339), "metrics": map[string]any{"alive": "500", "dead": "0", "feed_kg": "12.5"},
		"location": map[string]any{"lat": 10.3811, "lng": 105.4311, "accuracy_m": "8"}, "submit": true}), 201, "", "lần ghi")
	want(t, chu.post(base+"diary-entries/"+str(en, "ref")+"/verify", map[string]any{"base_version": ver(en)}), 200, "", "xác thực lần ghi")

	// ----- Mẫu 01: lập (điền sẵn), mỗi dự án × tháng một bản, quyền chủ nhiệm (AC-04) -----
	newDoc := func(u user, form string, extra map[string]any) resp {
		body := map[string]any{"form": form, "project_ref": pref}
		for k, v := range extra {
			body[k] = v
		}
		return u.post(base+"knqg-docs", body)
	}
	want(t, newDoc(chu2, "01", map[string]any{"period": "2027-10"}), 403, "", "AC-04: chủ nhiệm dự án khác không lập được")
	want(t, newDoc(vc, "01", map[string]any{"period": "2027-10"}), 403, "", "viên chức chỉ xem")
	want(t, newDoc(bgd, "01", map[string]any{"period": "2027-10"}), 403, "extension.read_only", "Ban Giám đốc chỉ xem")
	want(t, newDoc(chu, "01", map[string]any{"period": "2027-13"}), 422, "extension.cnts_knqg_invalid", "tháng sai")
	want(t, chu.post(base+"knqg-docs", map[string]any{"form": "01", "project_ref": str(pt, "ref"), "period": "2027-10"}), 422,
		"extension.cnts_knqg_project_kind", "chỉ dự án KNQG")
	k01 := key()
	d1 := want(t, chu.do(http.MethodPost, base+"knqg-docs", k01, map[string]any{"form": "01", "project_ref": pref, "period": "2027-10"}),
		201, "", "lập Mẫu 01")
	r1 := str(d1, "ref")
	c1 := d1["content"].(map[string]any)
	f1 := c1["fields"].(map[string]any)
	if f1["ten_du_an"] != "Mô hình gà an toàn sinh học" || f1["kinh_phi"] != "450000000" || len(c1["tables"].(map[string]any)["mo_hinh"].([]any)) != 2 ||
		str(d1, "status") != "draft" || !has(strsOf(d1, "actions"), "submit") {
		t.Fatalf("Mẫu 01 điền sẵn: %s", d1)
	}
	if again := want(t, chu.do(http.MethodPost, base+"knqg-docs", k01, map[string]any{"form": "01", "project_ref": pref, "period": "2027-10"}),
		201, "", "phát lại cùng khóa"); str(again, "ref") != r1 {
		t.Fatalf("phát lại: %v", again)
	}
	want(t, newDoc(chu, "01", map[string]any{"period": "2027-10"}), 409, "extension.cnts_knqg_exists", "mỗi dự án mỗi tháng một bản")
	// Sửa: khóa lạ, SĐT trong văn bản, vật tư vượt kế hoạch (cảnh báo quy tắc 11).
	upd := func(u user, ref string, base int64, content map[string]any) resp {
		return u.post("/v1/extension/cnts/knqg-docs/"+ref, map[string]any{"base_version": base, "content": content})
	}
	want(t, upd(chu, r1, ver(d1), map[string]any{"fields": map[string]any{"la": "x"}}), 422, "extension.cnts_knqg_invalid", "ô lạ")
	want(t, upd(chu, r1, ver(d1), map[string]any{"fields": map[string]any{"kien_nghi": "Gọi 0912345678"}}), 422, "extension.cnts_knqg_invalid", "SĐT")
	want(t, upd(chu2, r1, ver(d1), map[string]any{"fields": map[string]any{"danh_gia": "x"}}), 403, "", "chủ nhiệm khác không sửa")
	content1 := map[string]any{"fields": map[string]any{"danh_gia": "Đúng tiến độ", "ten_du_an": "giả mạo"},
		"tables": map[string]any{"vat_tu": []map[string]any{{"ten": "Gà giống", "dvt": "con", "ke_hoach": "1500", "da_cap": "1600"}}}}
	u1 := want(t, upd(chu, r1, ver(d1), content1), 200, "", "sửa Mẫu 01")
	if u1["content"].(map[string]any)["fields"].(map[string]any)["ten_du_an"] != "Mô hình gà an toàn sinh học" || len(strsOf(u1, "warnings")) != 1 {
		t.Fatalf("ô tự điền giữ nguyên, cảnh báo vật tư vượt: %s", u1)
	}
	want(t, upd(chu, r1, ver(d1), content1), 409, "extension.cnts_knqg_version_conflict", "khóa lạc quan")
	act := func(u user, ref, a string, base int64, reason string) resp {
		return u.post(base_+ref+"/actions/"+a, map[string]any{"base_version": base, "reason": reason})
	}
	s1 := want(t, act(chu, r1, "submit", ver(u1), ""), 200, "", "trình duyệt")
	want(t, act(chu, r1, "approve", ver(s1), ""), 403, "", "chủ nhiệm không tự duyệt")
	want(t, act(bgd, r1, "approve", ver(s1), ""), 403, "extension.read_only", "Ban Giám đốc không duyệt")
	want(t, act(ld, r1, "return", ver(s1), ""), 422, "", "FN-19: trả lại không ý kiến → chặn")
	rt := want(t, act(ld, r1, "return", ver(s1), "Bổ sung kết quả điểm B"), 200, "", "trả lại có ý kiến")
	if str(rt, "status") != "returned" || str(rt, "status_reason") != "Bổ sung kết quả điểm B" || rt["can_edit"] != true {
		t.Fatalf("trả lại: %s", rt)
	}
	s1 = want(t, act(chu, r1, "submit", ver(rt), ""), 200, "", "trình lại")
	ap := want(t, act(ld, r1, "approve", ver(s1), ""), 200, "", "lãnh đạo phòng duyệt")
	if str(ap, "status") != "approved" || ap["can_edit"] != false {
		t.Fatalf("đã duyệt: %s", ap)
	}
	want(t, upd(chu, r1, ver(ap), content1), 409, "extension.cnts_knqg_locked", "BR-03: đã duyệt không sửa")
	want(t, act(chu, r1, "adjust", ver(ap), ""), 422, "", "điều chỉnh cần lý do")
	adj := want(t, act(chu, r1, "adjust", ver(ap), "Cập nhật số hộ điểm B"), 200, "", "bản điều chỉnh")
	if str(adj, "status") != "draft" || str(adj, "revision") != "2" || len(list(adj, "versions")) != 7 {
		t.Fatalf("BR-03: điều chỉnh tạo bản mới, trình lại: %s", adj)
	}

	// ----- Mẫu 05: điểm A, thực tế từ nhật ký (quy tắc 9), ký có scan; Cấp Xã xem điểm xã mình -----
	want(t, newDoc(chu, "05", nil), 422, "extension.cnts_knqg_invalid", "Mẫu 05 cần điểm")
	d5 := want(t, newDoc(chu, "05", map[string]any{"site_ref": sA}), 201, "", "lập Mẫu 05 điểm A")
	r5 := str(d5, "ref")
	var chiTieu []any
	chiTieu = d5["content"].(map[string]any)["tables"].(map[string]any)["chi_tieu"].([]any)
	row := func(rows []any, ma string) map[string]any {
		for _, r := range rows {
			if m := r.(map[string]any); m["ma"] == ma {
				return m
			}
		}
		return nil
	}
	if r := row(chiTieu, "so_ho"); r["thuc_te_nhat_ky"] != "1" || r["thuc_te"] != "1" || r["yeu_cau"] != "1" {
		t.Fatalf("số hộ thực tế từ nhật ký đã xác thực: %v", r)
	}
	if r := row(chiTieu, "quy_mo"); r["thuc_te_nhat_ky"] != "500 con" || r["yeu_cau"] != "1500 con" {
		t.Fatalf("quy mô: %v", r)
	}
	want(t, newDoc(chu, "05", map[string]any{"site_ref": sA}), 409, "extension.cnts_knqg_exists", "một Mẫu 05 mỗi điểm")
	if l := list(want(t, officer.get(base+"knqg-docs"), 200, "", "Cấp Xã"), "items"); len(l) != 1 || str(l[0].(map[string]any), "form") != "05" {
		t.Fatalf("Cấp Xã chỉ thấy Mẫu 05 của xã mình: %v", l)
	}
	if l := list(want(t, xa2.get(base+"knqg-docs"), 200, "", "xã khác"), "items"); len(l) != 0 {
		t.Fatalf("xã khác không thấy (#108 lọc trước khi trả): %v", l)
	}
	want(t, xa2.get(base+"knqg-docs/"+r5), 404, "", "xã khác không mở được")
	want(t, officer.post(base+"knqg-docs/"+r5, map[string]any{"base_version": ver(d5), "content": map[string]any{}}), 403, "", "Cấp Xã chỉ xem")
	// Sửa thực tế khác nhật ký, không ghi chú → chặn trình ký.
	edited := withFields(d5, map[string]any{"don_vi_thuc_hien": "Trung tâm Khuyến nông An Giang", "danh_gia": "Đạt yêu cầu"})
	so := row(edited["tables"].(map[string]any)["chi_tieu"].([]any), "so_ho")
	so["thuc_te"], so["thuc_te_nhat_ky"] = "2", "2"
	e5 := want(t, upd(chu, r5, ver(d5), edited), 200, "", "sửa Mẫu 05")
	if r := row(e5["content"].(map[string]any)["tables"].(map[string]any)["chi_tieu"].([]any), "so_ho"); r["thuc_te_nhat_ky"] != "1" {
		t.Fatalf("cột thực tế từ nhật ký do máy chủ đặt: %v", r)
	}
	want(t, act(chu, r5, "request_sign", ver(e5), ""), 422, "extension.cnts_knqg_incomplete", "quy tắc 9: sửa thực tế phải có ghi chú")
	so["ghi_chu"] = "Thêm 1 hộ nhân rộng"
	e5 = want(t, upd(chu, r5, ver(e5), edited), 200, "", "ghi chú")
	w5 := want(t, act(chu, r5, "request_sign", ver(e5), ""), 200, "", "trình ký")
	want(t, upd(chu, r5, ver(w5), edited), 409, "extension.cnts_knqg_locked", "chờ ký không sửa")
	want(t, chu.upload(base+"knqg-docs/"+r5+"/signed", ver(w5), "bb.exe", []byte("MZ")), 422, "extension.cnts_knqg_scan_invalid", "tệp không phải scan")
	want(t, chu2.upload(base+"knqg-docs/"+r5+"/signed", ver(w5), "bb.pdf", pdf), 403, "", "chủ nhiệm khác không ký")
	sg := want(t, chu.upload(base+"knqg-docs/"+r5+"/signed", ver(w5), "bien-ban-05.pdf", pdf), 200, "", "tải bản đã ký")
	if str(sg, "status") != "signed" || sg["signed"] == nil {
		t.Fatalf("đã ký: %s", sg)
	}
	code, body, hdr := officer.req(http.MethodGet, base+"knqg-docs/"+r5+"/signed", "", "", nil)
	if code != 200 || !bytes.Equal(body, pdf) || hdr.Get("X-Content-SHA256") == "" {
		t.Fatalf("Cấp Xã tải bản scan điểm xã mình: %d %s", code, body)
	}

	// ----- Mẫu 07: chặn gửi khi điểm B chưa có Mẫu 05 đã ký (AC-03, quy tắc 10) -----
	d7 := want(t, newDoc(chu, "07", map[string]any{"period": "2027"}), 201, "", "lập Mẫu 07")
	r7 := str(d7, "ref")
	if d := d7["content"].(map[string]any)["tables"].(map[string]any)["diem"].([]any); len(d) != 1 {
		t.Fatalf("Mẫu 07 tổng hợp từ Mẫu 05 đã ký: %v", d)
	}
	if b := strsOf(d7, "blockers"); len(b) != 1 || !strings.Contains(b[0], "Điểm B") {
		t.Fatalf("điều kiện gửi: %v", b)
	}
	if l := list(want(t, tw.get(base+"knqg-docs?form=07"), 200, "", "TW"), "items"); len(l) != 0 {
		t.Fatalf("TW không thấy Mẫu 07 chưa gửi: %v", l)
	}
	c7 := withFields(d7, map[string]any{"hop_dong": "12/HĐ-KN ngày 01/03/2027", "nhan_xet": "Dự án đạt yêu cầu"})
	e7 := want(t, upd(chu, r7, ver(d7), c7), 200, "", "sửa Mẫu 07")
	want(t, act(chu, r7, "send", ver(e7), ""), 409, "extension.cnts_knqg_needs_signed_05", "AC-03: thiếu Mẫu 05 đã ký của điểm B")
	d5b := want(t, newDoc(chu, "05", map[string]any{"site_ref": sB}), 201, "", "Mẫu 05 điểm B")
	e5b := want(t, upd(chu, str(d5b, "ref"), ver(d5b), withFields(d5b, map[string]any{"don_vi_thuc_hien": "TTKN", "danh_gia": "Đạt"})), 200, "", "sửa 05 B")
	w5b := want(t, act(chu, str(d5b, "ref"), "request_sign", ver(e5b), ""), 200, "", "trình ký 05 B")
	want(t, chu.upload(base+"knqg-docs/"+str(d5b, "ref")+"/signed", ver(w5b), "b.png", []byte("\x89PNG\r\n\x1a\n....")), 200, "", "ký 05 B (ảnh)")
	// Mẫu 05 điểm A có bản điều chỉnh chưa ký → Mẫu 07 vẫn không gửi được.
	a5 := want(t, act(chu, r5, "adjust", ver(sg), "Sửa số hộ theo biên bản giấy"), 200, "", "điều chỉnh 05 A")
	if a5["signed"] != nil || str(a5, "revision") != "2" {
		t.Fatalf("bản điều chỉnh chưa ký, phải ký lại: %s", a5)
	}
	want(t, act(chu, r7, "send", ver(e7), ""), 409, "extension.cnts_knqg_needs_signed_05", "Mẫu 05 có bản điều chỉnh chưa ký")
	w5 = want(t, act(chu, r5, "request_sign", ver(a5), ""), 200, "", "trình ký lại 05 A")
	want(t, chu.upload(base+"knqg-docs/"+r5+"/signed", ver(w5), "bb2.pdf", pdf), 200, "", "ký lại 05 A")
	sent := want(t, act(chu, r7, "send", ver(e7), ""), 200, "", "gửi Quốc gia")
	if str(sent, "status") != "sent" || len(sent["content"].(map[string]any)["tables"].(map[string]any)["diem"].([]any)) != 2 {
		t.Fatalf("đã gửi, tổng hợp 2 điểm: %s", sent)
	}

	// ----- Cấp TW (UR-QG-01): thấy ngay, chỉ dự án KNQG, xác nhận / trả lại Mẫu 07 -----
	tl := list(want(t, tw.get(base+"knqg-docs"), 200, "", "TW danh sách"), "items")
	for _, x := range tl {
		if str(x.(map[string]any), "project_ref") != pref {
			t.Fatalf("TW chỉ thấy dự án KNQG: %v", x)
		}
	}
	if l := list(want(t, tw.get(base+"knqg-docs?form=07"), 200, "", "TW Mẫu 07"), "items"); len(l) != 1 || !has(strsOf(l[0].(map[string]any), "actions"), "confirm") {
		t.Fatalf("AC-02: TW thấy Mẫu 07 ngay sau khi gửi, có nút xác nhận: %v", l)
	}
	if l := list(want(t, twOther.get(base+"knqg-docs"), 200, "", "TW ngoài tỉnh"), "items"); len(l) != 0 {
		t.Fatalf("TW ngoài phạm vi: %v", l)
	}
	want(t, tw.post(base+"knqg-docs/"+r1, map[string]any{"base_version": ver(adj), "content": map[string]any{}}), 403, "extension.read_only", "TW chỉ xem")
	want(t, act(tw, r1, "approve", ver(adj), ""), 403, "", "TW không duyệt Mẫu 01")
	want(t, act(chu, r7, "confirm", ver(sent), ""), 403, "", "chủ nhiệm không tự xác nhận")
	want(t, act(tw, r7, "return", ver(sent), ""), 422, "", "trả lại cần ý kiến")
	rr := want(t, act(tw, r7, "return", ver(sent), "Bổ sung chứng từ quyết toán"), 200, "", "TW trả lại")
	if str(rr, "status") != "returned" || rr["can_edit"] != false {
		t.Fatalf("trả lại (TW không sửa): %s", rr)
	}
	if v := want(t, chu.get(base+"knqg-docs/"+r5), 200, "", "05 sau trả lại"); str(v, "status") != "signed" {
		t.Fatalf("trả lại Mẫu 07 không mở khóa Mẫu 05: %s", v)
	}
	sent = want(t, act(chu, r7, "send", ver(rr), ""), 200, "", "gửi lại")
	cf := want(t, act(tw, r7, "confirm", ver(sent), ""), 200, "", "TW xác nhận")
	if str(cf, "status") != "confirmed" || len(strsOf(cf, "actions")) != 0 {
		t.Fatalf("BR-02: đã xác nhận khóa hẳn: %s", cf)
	}
	want(t, act(chu, r7, "adjust", ver(cf), "Sửa lại số liệu năm"), 409, "extension.cnts_knqg_state", "đã xác nhận không điều chỉnh")
	want(t, upd(chu, r7, ver(cf), c7), 409, "extension.cnts_knqg_locked", "đã xác nhận không sửa")

	// ----- Mẫu 03 (số văn bản), Mẫu 04; Word đúng mẫu qua pf/export -----
	d3 := want(t, newDoc(chu, "03", nil), 201, "", "lập Mẫu 03")
	if hs := d3["content"].(map[string]any)["tables"].(map[string]any)["ho_so"].([]any); len(hs) < 5 {
		t.Fatalf("Mẫu 03: danh mục hồ sơ theo mẫu: %v", hs)
	}
	d4 := want(t, newDoc(ld, "04", nil), 201, "", "lãnh đạo phòng lập Mẫu 04")
	word := func(u user, ref string) (int, map[string]any) {
		r := u.post(base+"knqg-docs/"+ref+"/word", map[string]any{})
		return r.code, r.body
	}
	code, w3 := word(chu, str(d3, "ref"))
	if code != 201 || str(w3, "doc_number") != "1/BC-TTKN" || str(w3, "template") != "cnts.knqg_03" {
		t.Fatalf("Word Mẫu 03 có số: %d %v", code, w3)
	}
	for _, ref := range []string{r1, str(d4, "ref"), r5, r7} {
		if code, w := word(chu, ref); code != 201 || str(w, "doc_number") != "" {
			t.Fatalf("Word biên bản (không số): %d %v", code, w)
		}
	}
	if code, _ := word(xa2, r5); code != 404 {
		t.Fatalf("xã khác không xuất Word: %d", code)
	}
	code, w5w := word(officer, r5)
	if code != 201 {
		t.Fatalf("Cấp Xã xuất Word Mẫu 05 điểm xã mình: %d", code)
	}
	code, raw, _ := officer.req(http.MethodGet, base+"knqg-docs/"+r5+"/words/"+str(w5w, "id")+"/download", "", "", nil)
	if code != 200 {
		t.Fatalf("tải Word: %d %s", code, raw)
	}
	content, err := docx.Extract(raw)
	if err != nil {
		t.Fatal(err)
	}
	text := content.Text()
	for _, s := range []string{"Mẫu 05/DA-NTMH", "KẾT QUẢ THỰC HIỆN MÔ HÌNH KHUYẾN NÔNG", "Mô hình gà an toàn sinh học", "Điểm A, Xã Bình Hòa 30001",
		"4. Kết quả thực hiện", "Thêm 1 hộ nhân rộng", "PHỤ LỤC I", "(Bản điều chỉnh lần 1)"} {
		if !strings.Contains(text, s) {
			t.Fatalf("AC-01: Word thiếu %q:\n%s", s, text)
		}
	}
	if l := list(want(t, chu.get(base+"knqg-docs/"+r5+"/words"), 200, "", "lịch sử Word"), "items"); len(l) != 2 {
		t.Fatalf("lịch sử Word: %v", l)
	}
	if l := list(want(t, chu.get(base+"knqg-specs"), 200, "", "đặc tả"), "items"); len(l) != 5 {
		t.Fatalf("5 mẫu: %v", l)
	}

	// Dự án kết thúc → khóa lập/sửa biểu; RLS: tenant khác không thấy.
	want(t, chu.post(base+"projects/"+pref+"/close", map[string]any{"base_version": ver(p), "reason": "Đã nghiệm thu năm"}), 200, "", "kết thúc dự án")
	want(t, newDoc(chu, "03", nil), 409, "extension.cnts_project_closed", "dự án đã kết thúc")
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension.cnts_knqg_doc WHERE tenant_id = $1`, f.tenant).Scan(&n); err != nil || n == 0 {
		t.Fatalf("sổ: %d %v", n, err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE pf_extension.cnts_knqg_doc SET status = 'draft' WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("sổ chỉ ghi thêm: UPDATE phải bị chặn")
	}
}

const base_ = "/v1/extension/cnts/knqg-docs/"

// withFields sao chép sâu nội dung của biểu doc và đặt thêm ô.
func withFields(doc map[string]any, fields map[string]any) map[string]any {
	raw, _ := json.Marshal(doc["content"])
	var c map[string]any
	_ = json.Unmarshal(raw, &c)
	fs, _ := c["fields"].(map[string]any)
	if fs == nil {
		fs = map[string]any{}
		c["fields"] = fs
	}
	for k, v := range fields {
		fs[k] = v
	}
	return c
}
