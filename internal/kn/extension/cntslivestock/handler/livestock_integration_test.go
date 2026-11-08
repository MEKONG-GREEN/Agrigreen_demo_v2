package handler_test

// CNTS-V4-HOUSEHOLD-LIST (#251, ADR-0056, BRD CNTS v4.0 FN-10 H1–H4, BR-03…BR-08, AC-05…AC-09, HCN-01…HCN-09, ma trận
// quyền "Danh sách hộ chăn nuôi"): danh sách hộ chăn nuôi trên PostgreSQL thật qua module pf/extension (vai extension_app).
// Ngữ cảnh phiên KN giả lập bằng middleware X-AG-Context; danh mục CNTS, đơn vị hành chính, kho định danh là bản giả.

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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
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
		return partyport.AdminUnit{Code: code, Name: "Xã " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "89":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

type catalogs struct{ knport.CNTSCatalogs }

var species = []mdport.CatalogItem{
	{Catalog: "cnts_species", Code: "GA", Name: "Gà", Active: true},
	{Catalog: "cnts_species", Code: "VIT", Name: "Vịt", Active: true},
	{Catalog: "cnts_species", Code: "CU", Name: "Cút", Active: false},
}

func (catalogs) ItemAt(_ context.Context, catalog, code string, _ time.Time) (mdport.CatalogItem, error) {
	for _, it := range species {
		if it.Catalog == catalog && it.Code == code {
			return it, nil
		}
	}
	return mdport.CatalogItem{}, fmt.Errorf("giả: %w", mdport.ErrCatalogItemNotFound)
}

func (catalogs) ListAt(_ context.Context, catalog string, _ time.Time) ([]mdport.CatalogItem, error) {
	out := []mdport.CatalogItem{}
	for _, it := range species {
		if it.Catalog == catalog && it.Active {
			out = append(out, it)
		}
	}
	return out, nil
}

type names struct {
	mu sync.Mutex
	m  map[uuid.UUID]string
}

func (a *names) DisplayNames(_ context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[uuid.UUID]string{}
	for _, r := range refs {
		if n, ok := a.m[r]; ok {
			out[r] = n
		}
	}
	return out, nil
}

type fix struct {
	t        *testing.T
	iss      *authztest.FakeIssuer
	srv      *httptest.Server
	pool     *pgxpool.Pool
	tenant   uuid.UUID
	mu       sync.Mutex
	ctxs     map[uuid.UUID]*authz.KNContext
	vault    *clienttest.Fake
	names    *names
	sessions *authz.SessionRegistry
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}, vault: clienttest.New(),
		names: &names{m: map[uuid.UUID]string{}}}
	f.pool = env.AdminPool(t, migrations.PF)
	mod := extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(t0), extension.Deps{Units: units{}})
	registry, err := authz.NewSessionRegistry(mod.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	f.sessions = registry
	mod.UseCNTSCatalogs(catalogs{})
	mod.UseCNTSModelVault(f.vault)
	mod.UseReportCycle(extension.ReportCycleDeps{Names: f.names})
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
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
		selected, err := f.sessions.ResolveSelected(c.Request.Context(), p)
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

func (f *fix) user(name string, kc authz.KNContext) user {
	sub := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: f.tenant, Roles: []string{"kn_user", "vault_registrar"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	f.names.mu.Lock()
	f.names.m[sub] = name
	f.names.mu.Unlock()
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
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
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

const base = "/v1/extension/cnts/livestock-households"

func checkin(acc string) map[string]any {
	return map[string]any{"lat": 10.3712, "lon": 105.4321, "accuracy_m": acc, "mocked": false, "captured_at": t0.Format(time.RFC3339)}
}

func household(name, commune, hamlet string) map[string]any {
	return map[string]any{"owner_name": name, "owner_phone": "0912000123", "commune_code": commune, "hamlet": hamlet,
		"livestock_type": "nong_ho", "animals": []any{map[string]any{"species_code": "GA", "head_count": 120}}, "checkin": checkin("12.5")}
}

// changes trả "field:old→new" của dòng lịch sử version v.
func changes(m map[string]any, v string) string {
	var parts []string
	for _, h := range list(m, "history") {
		hm := h.(map[string]any)
		if num(hm, "version") != v {
			continue
		}
		for _, c := range list(hm, "changes") {
			cm := c.(map[string]any)
			parts = append(parts, str(cm, "field")+":"+str(cm, "old")+"→"+str(cm, "new"))
		}
	}
	return strings.Join(parts, "|")
}

func TestLivestockHouseholds_251(t *testing.T) {
	f := newFix(t)
	x1 := f.user("Cán bộ xã 1", xa("30001"))
	x2 := f.user("Cán bộ xã 2", xa("30002"))
	th := f.user("Cán bộ tổng hợp", tinh("tong_hop"))
	vc := f.user("Viên chức", tinh("vien_chuc"))
	tw := f.user("Cấp TW", authz.KNContext{Role: authz.KNTW, Department: cnts, Scope: authz.Scope{All: true}})

	// Cấp TW không có quyền (ma trận FN-10).
	want(t, tw.get(base), 403, "forbidden", "TW đọc")

	// AC-05: thêm hộ chưa check-in → không lưu.
	noCheckin := household("Nguyễn Văn An", "30001", "Ấp Mỹ Hòa")
	delete(noCheckin, "checkin")
	want(t, x1.post(base, noCheckin), 422, "extension.cnts_livestock_household_checkin_required", "thiếu check-in")

	// Kiểm dữ liệu: số con, độ chính xác 1e100000, vật nuôi ngoài danh mục / ngừng dùng, ấp chứa SĐT, loại sai, SĐT chữ.
	want(t, x1.post(base, `{"owner_name":"A","commune_code":"30001","livestock_type":"nong_ho","animals":[{"species_code":"GA","head_count":1e100000}]}`),
		422, "validation_failed", "head_count 1e100000")
	bad := household("Nguyễn Văn An", "30001", "Ấp Mỹ Hòa")
	bad["checkin"] = checkin("1e100000")
	want(t, x1.post(base, bad), 422, "extension.cnts_livestock_household_invalid", "accuracy 1e100000")
	bad = household("Nguyễn Văn An", "30001", "Ấp Mỹ Hòa")
	bad["animals"] = []any{map[string]any{"species_code": "CU", "head_count": 1}}
	want(t, x1.post(base, bad), 422, "extension.cnts_livestock_household_invalid", "vật nuôi ngừng dùng")
	bad = household("Nguyễn Văn An", "30001", "Ấp 0912345678")
	want(t, x1.post(base, bad), 422, "extension.cnts_livestock_household_invalid", "ấp chứa SĐT")
	bad = household("Nguyễn Văn An", "30001", "Ấp Mỹ Hòa")
	bad["owner_phone"] = "09a"
	want(t, x1.post(base, bad), 422, "extension.cnts_livestock_household_invalid", "SĐT chữ")
	bad = household("Nguyễn Văn An", "30001", "Ấp Mỹ Hòa")
	bad["animals"] = []any{map[string]any{"species_code": "GA", "head_count": -1}}
	want(t, x1.post(base, bad), 422, "extension.cnts_livestock_household_invalid", "số con âm")

	// AC-06: Cấp Xã chỉ thêm hộ xã mình.
	want(t, x1.post(base, household("Lê Văn Cường", "30002", "")), 403, "extension.out_of_scope", "thêm hộ xã khác")
	// Chỉ cán bộ tổng hợp được thêm ở Cấp Tỉnh.
	want(t, vc.post(base, household("Lê Văn Cường", "30002", "")), 403, "extension.out_of_scope", "viên chức thêm hộ")

	// H1–H3: thêm hộ có check-in → Đang dùng, mã HCN0000001; sai số GPS > 50 m → cảnh báo vẫn lưu.
	in := household("Nguyễn Văn An", "30001", "Ấp Mỹ Hòa")
	in["checkin"] = checkin("80")
	k1 := key()
	h1 := want(t, x1.postK(base, k1, in), 201, "", "thêm hộ")
	id1 := str(h1, "id")
	if str(h1, "code") != "HCN0000001" || str(h1, "status") != "active" || str(h1, "owner_label") != "Nguyễn V. A." ||
		str(h1, "owner_masked_phone") != "0912•••123" || str(h1, "owner_name") != "Nguyễn Văn An" ||
		!strings.Contains(string(mustJSON(h1["warnings"])), "gps_accuracy") || h1["location"] == nil {
		t.Fatalf("hộ mới: %v", h1)
	}
	// Gửi lại cùng khóa → cùng hộ, không cấp mã mới.
	if r := x1.postK(base, k1, in); (r.status != 200 && r.status != 201) || str(r.body, "id") != id1 {
		t.Fatalf("gửi lại tạo hộ mới: %d %s", r.status, r.raw)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_livestock_household_key WHERE tenant_id = $1`, f.tenant); n != 1 {
		t.Fatalf("số mã hộ: %d", n)
	}
	// Họ tên/SĐT không nằm ở sổ KN.
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_livestock_household WHERE tenant_id = $1 AND row_to_json(cnts_livestock_household)::text LIKE '%Nguy%'`, f.tenant); n != 0 {
		t.Fatal("sổ KN chứa họ tên")
	}

	// Cảnh báo trùng: cùng họ tên (khác hoa thường/khoảng trắng), ấp, xã → 409; xác nhận hộ khác → lưu, mã HCN0000002.
	dupIn := household(" nguyễn  văn an ", "30001", "ấp mỹ hòa")
	dupIn["owner_phone"] = "0912000999"
	r := x1.post(base, dupIn)
	want(t, r, 409, "extension.cnts_livestock_household_duplicate", "trùng")
	if !strings.Contains(string(r.raw), "HCN0000001") {
		t.Fatalf("409 phải kèm mã hộ trùng: %s", r.raw)
	}
	dupIn["confirm_distinct"] = true
	h2 := want(t, x1.post(base, dupIn), 201, "", "xác nhận hộ khác")
	if str(h2, "code") != "HCN0000002" {
		t.Fatalf("mã hộ 2: %v", h2)
	}
	id2 := str(h2, "id")

	// AC-06: xã khác không thấy, không đọc được.
	if l := want(t, x2.get(base), 200, "", "xã 2 liệt kê"); len(list(l, "items")) != 0 || num(l, "total") != "0" {
		t.Fatalf("xã 2 thấy hộ xã 1: %v", l)
	}
	want(t, x2.get(base+"/"+id1), 404, "extension.cnts_livestock_household_not_found", "xã 2 đọc hộ xã 1")
	if l := want(t, vc.get(base+"?commune=30001&status=active"), 200, "", "tỉnh liệt kê"); num(l, "total") != "2" {
		t.Fatalf("tỉnh liệt kê: %v", l)
	}
	// Viên chức Cấp Tỉnh chỉ xem: không có họ tên đầy đủ, không sửa.
	if v := want(t, vc.get(base+"/"+id1), 200, "", "viên chức đọc"); v["owner_name"] != nil || v["can_edit"] != false || v["can_deactivate"] != false {
		t.Fatalf("viên chức: %v", v)
	}

	// AC-08: sửa loại chăn nuôi → lịch sử người sửa, thời điểm, cũ → mới; sai phiên bản → 409.
	upd := map[string]any{"base_version": 1, "commune_code": "30001", "hamlet": "Ấp Mỹ Hòa", "livestock_type": "trang_trai_nho",
		"animals": []any{map[string]any{"species_code": "GA", "head_count": 120}, map[string]any{"species_code": "VIT", "head_count": 30}}}
	want(t, vc.post(base+"/"+id1, upd), 403, "forbidden", "viên chức sửa")
	v := want(t, x1.post(base+"/"+id1, upd), 200, "", "sửa loại")
	if got := changes(v, "2"); got != "animals:Gà: 120→Gà: 120; Vịt: 30|livestock_type:Nông hộ→Trang trại nhỏ" {
		t.Fatalf("lịch sử AC-08: %q", got)
	}
	if hm := list(v, "history")[0].(map[string]any); str(hm, "actor_name") != "Cán bộ xã 1" || str(hm, "at") == "" {
		t.Fatalf("người sửa: %v", hm)
	}
	want(t, x1.post(base+"/"+id1, upd), 409, "extension.cnts_livestock_household_version_conflict", "sai phiên bản")
	upd["base_version"] = 2
	want(t, x1.post(base+"/"+id1, upd), 422, "validation_failed", "không có thay đổi")

	// Q9 (#276): họ tên cũ → mới đầy đủ; SĐT vẫn che một phần.
	upd["owner_name"], upd["owner_phone"] = "Nguyễn Văn Bình", "0913000456"
	v = want(t, th.post(base+"/"+id1, upd), 200, "", "sửa họ tên")
	if got := changes(v, "3"); got != "owner_name:Nguyễn Văn An→Nguyễn Văn Bình|owner_phone:0912•••123→0913•••456" {
		t.Fatalf("lịch sử họ tên: %q", got)
	}
	if strings.Contains(string(mustJSON(list(v, "history"))), "0913000456") {
		t.Fatal("lịch sử lộ SĐT đầy đủ")
	}
	// SĐT đã thuộc chủ thể khác (hộ 2) → 409.
	upd["base_version"], upd["owner_phone"] = 3, "0912000999"
	want(t, th.post(base+"/"+id1, upd), 409, "extension.cnts_livestock_household_phone_taken", "SĐT trùng chủ thể khác")

	// H4: đổi vị trí phải check-in lại và có lý do.
	want(t, x1.post(base+"/"+id1+"/checkin", map[string]any{"base_version": 3, "checkin": checkin("8")}), 422,
		"extension.cnts_livestock_household_invalid", "đổi vị trí thiếu lý do")
	v = want(t, x1.post(base+"/"+id1+"/checkin", map[string]any{"base_version": 3, "checkin": checkin("8"), "reason": "Hộ chuyển chuồng sang thửa khác"}),
		200, "", "đổi vị trí")
	if str(v, "event") != "relocated" || !strings.HasPrefix(changes(v, "4"), "location:") {
		t.Fatalf("đổi vị trí: %v", v)
	}

	// BR-04: vô hiệu hóa chỉ cán bộ tổng hợp Cấp Tỉnh, bắt buộc lý do; sau đó không sửa được, không kích hoạt lại.
	want(t, x1.post(base+"/"+id2+"/deactivate", map[string]any{"base_version": 1, "reason": "Hộ ngừng chăn nuôi"}), 403, "forbidden", "xã vô hiệu hóa")
	want(t, vc.post(base+"/"+id2+"/deactivate", map[string]any{"base_version": 1, "reason": "Hộ ngừng chăn nuôi"}), 403, "forbidden", "viên chức vô hiệu hóa")
	want(t, th.post(base+"/"+id2+"/deactivate", map[string]any{"base_version": 1}), 422, "extension.cnts_livestock_household_invalid", "thiếu lý do")
	v = want(t, th.post(base+"/"+id2+"/deactivate", map[string]any{"base_version": 1, "reason": "Hộ ngừng chăn nuôi"}), 200, "", "vô hiệu hóa")
	if str(v, "status") != "deactivated" || changes(v, "2") != "status:Đang dùng→Vô hiệu hóa" || v["can_edit"] != false {
		t.Fatalf("vô hiệu hóa: %v", v)
	}
	want(t, x1.post(base+"/"+id2, map[string]any{"base_version": 2, "commune_code": "30001", "livestock_type": "nong_ho",
		"animals": []any{map[string]any{"species_code": "GA", "head_count": 1}}}), 409, "extension.cnts_livestock_household_deactivated", "sửa hộ đã vô hiệu hóa")
	// BR-07: hộ vô hiệu hóa không còn trong danh sách đang dùng.
	if l := want(t, x1.get(base+"?status=active"), 200, "", "lọc đang dùng"); num(l, "total") != "1" {
		t.Fatalf("lọc đang dùng: %v", l)
	}

	// BR-08, AC-09: hộ Chờ bổ sung vị trí (tạo từ BM-10 — #252) → cán bộ xã check-in → Đang dùng, lịch sử ghi chuyển trạng thái.
	pending := f.seedPending("30001")
	if l := want(t, x1.get(base+"?status=pending_location"), 200, "", "lọc chờ vị trí"); num(l, "total") != "1" {
		t.Fatalf("lọc chờ vị trí: %v", l)
	}
	v = want(t, x1.post(base+"/"+pending.String()+"/checkin", map[string]any{"base_version": 1, "checkin": checkin("5")}), 200, "", "check-in hộ chờ vị trí")
	if str(v, "status") != "active" || str(v, "event") != "checked_in" ||
		!strings.Contains(changes(v, "2"), "status:Chờ bổ sung vị trí→Đang dùng") {
		t.Fatalf("check-in hộ chờ vị trí: %v", v)
	}

	// Chuỗi băm của sổ sạch; mã không dùng lại; tenant khác bắt đầu lại từ HCN0000001 và không thấy hộ tenant này.
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_livestock_household_counter WHERE tenant_id = $1 AND last = 3`, f.tenant); n != 1 { // 2 hộ thêm + 1 hộ chờ vị trí
		t.Fatalf("bộ đếm: %d", n)
	}
	g := newFix(t)
	o := g.user("Xã tenant khác", xa("30001"))
	if h := want(t, o.post(base, household("Trần Văn Dũng", "30001", "")), 201, "", "tenant khác"); str(h, "code") != "HCN0000001" {
		t.Fatalf("mã tenant khác: %v", h)
	}
	want(t, o.get(base+"/"+id1), 404, "extension.cnts_livestock_household_not_found", "chéo tenant")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (f *fix) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// seedPending ghi một hộ Chờ bổ sung vị trí như BM-10 DB-04 sẽ ghi (#252): chủ thể ở kho, mã hộ, phiên bản created_pending.
func (f *fix) seedPending(commune string) uuid.UUID {
	f.t.Helper()
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: f.tenant, ActorRef: ids.New()})
	cr, err := f.vault.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectHousehold, FullName: "Phạm Thị Hoa", AreaCode: commune})
	if err != nil {
		f.t.Fatal(err)
	}
	ref := ids.New()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO pf_extension.cnts_livestock_household_counter (tenant_id, last) VALUES ($1, 1)
	  ON CONFLICT (tenant_id) DO UPDATE SET last = pf_extension.cnts_livestock_household_counter.last + 1`, f.tenant); err != nil {
		f.t.Fatal(err)
	}
	var code string
	if err := f.pool.QueryRow(context.Background(), `SELECT 'HCN' || lpad(last::text, 7, '0') FROM pf_extension.cnts_livestock_household_counter
	  WHERE tenant_id = $1`, f.tenant).Scan(&code); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO pf_extension.cnts_livestock_household_key (tenant_id, household_ref, code)
	  VALUES ($1, $2, $3)`, f.tenant, ref, code); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO pf_extension.cnts_livestock_household (id, tenant_id, stream_key,
	    household_ref, code, event, status, commune_code, person_ref, livestock_type, species_codes, head_counts, origin_outbreak_ref,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $10, $3, $4, 'created_pending', 'pending_location', $5, $6, 'nong_ho', '{GA}', '{10}', $7, $8, $9,
	    $11, 'kn.cnts.livestock_household/v1')`,
		ids.New(), f.tenant, ref, code, commune, cr.Ref, ids.New(), t0, ids.New(), "lhh:"+ref.String(), "seed:"+ref.String()); err != nil {
		f.t.Fatal(err)
	}
	return ref
}
