package handler_test

// W11.49 (D4 CN9, BRD CNTS v3.7 FN-11, UR-XA-10): Báo cáo dịch bệnh BM-10 có phiên bản diễn biến và báo nghi dịch trên
// PostgreSQL thật qua module pf/extension (vai extension_app). Ngữ cảnh phiên KN giả lập bằng middleware X-AG-Context
// (như extension/khtc); danh mục CNTS và đơn vị hành chính là bản giả.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const cnts = "chan_nuoi_thuy_san"

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

// units: tỉnh 89, xã 30001, 30002 (cấp xã), 89 không phải xã.
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

// catalogs: bệnh dtlcp (heo), cum (gà), vật nuôi heo, ga.
type catalogs struct{ mdport.CNTSCatalogs }

var items = map[string]mdport.CatalogItem{
	"cnts_disease:dtlcp": {Catalog: "cnts_disease", Code: "dtlcp", Name: "Dịch tả lợn châu Phi", Active: true,
		Attrs: map[string]string{"species": "heo", "notifiable": "true"}},
	"cnts_disease:cum": {Catalog: "cnts_disease", Code: "cum", Name: "Cúm gia cầm", Active: true, Attrs: map[string]string{"species": "ga"}},
	"cnts_species:heo": {Catalog: "cnts_species", Code: "heo", Name: "Heo", Active: true},
	"cnts_species:ga":  {Catalog: "cnts_species", Code: "ga", Name: "Gà", Active: true},
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

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	pool   *pgxpool.Pool // vai quản trị — đọc shared.outbox
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	f.pool = env.AdminPool(t, migrations.PF)
	mod := extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(t0), extension.Deps{Units: units{}})
	mod.UseCNTSCatalogs(catalogs{})
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(authz.ContextHeader)
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
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
}

func (f *fix) user(kc authz.KNContext) user {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID}
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

func key() string                                   { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp            { return u.do(http.MethodPost, p, key(), b) }
func (u user) postK(p, k string, b any) resp        { return u.do(http.MethodPost, p, k, b) }
func (u user) get(p string) resp                    { return u.do(http.MethodGet, p, "", nil) }
func str(m map[string]any, k string) string         { s, _ := m[k].(string); return s }
func num(m map[string]any, k string) json.Number    { n, _ := m[k].(json.Number); return n }
func list(m map[string]any, k string) []any         { l, _ := m[k].([]any); return l }
func obj(m map[string]any, k string) map[string]any { o, _ := m[k].(map[string]any); return o }

func want(t *testing.T, r resp, status int, code, what string) map[string]any {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
	return r.body
}

func (f *fix) outbox(eventType string) []map[string]any {
	f.t.Helper()
	var out []map[string]any
	rows, err := f.pool.Query(context.Background(), `SELECT payload FROM shared.outbox WHERE event_type = $1 AND tenant_id = $2`, eventType, f.tenant)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var m map[string]any
		if err := rows.Scan(&m); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func report() map[string]any {
	return map[string]any{
		"commune_code": "30001", "disease_code": "dtlcp", "species_code": "heo", "received_at": "2027-10-12T08:30:00+07:00",
		"source": "can_bo", "locality": "Ấp Hòa Thuận A", "location": map[string]any{"lat": "10.2534100", "lon": "105.9721000", "accuracy_m": "8", "source": "gps"},
		"herd_size": 120, "herd_age_days": 90, "dead": 5, "symptoms": "Sốt cao, bỏ ăn, xuất huyết da",
		"culled_head": 0, "chemical_liters": "20.5",
	}
}

func with(m map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// UR-XA-10 quy tắc chặn, FN-11 AC-02/AC-03, BR-01, BR-02; vòng đời Đã gửi → Đang theo dõi → Đã kết thúc (Cấp Tỉnh).
func TestFN11_OutbreakVersions(t *testing.T) {
	f := newFix(t)
	xa1 := f.user(xa("30001"))
	xa2 := f.user(xa("30002"))
	tinh := f.user(authz.KNContext{Role: authz.KNTinh, Department: cnts, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})
	bgd := f.user(authz.KNContext{Role: authz.KNTinh, Department: cnts, Scope: authz.Scope{All: true}, ViewOnly: true})
	tw := f.user(authz.KNContext{Role: authz.KNTW, Department: cnts, Scope: authz.Scope{All: true}})
	khac := f.user(authz.KNContext{Role: authz.KNXa, Department: "trong_trot", Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}})
	const p = "/v1/extension/cnts/outbreaks"

	want(t, xa1.post(p, with(report(), "location", nil)), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "AC-02 thiếu vị trí")
	want(t, xa1.post(p, with(report(), "dead", 121)), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "DB-07 > DB-06")
	want(t, xa1.post(p, with(report(), "culled_head", 200)), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "DB-10 > DB-06")
	want(t, xa1.post(p, with(report(), "location", map[string]any{"lat": "0", "lon": "0", "source": "gps"})), http.StatusUnprocessableEntity,
		"extension.cnts_outbreak_invalid", "tọa độ 0,0")
	want(t, xa1.post(p, with(report(), "disease_code", "khong_co")), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "bệnh ngoài danh mục")
	want(t, xa1.post(p, with(report(), "species_code", "ga")), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "bệnh không áp dụng cho loài")
	want(t, xa1.post(p, with(report(), "symptoms", "Gọi 0912345678")), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "PII")
	want(t, xa1.post(p, with(report(), "locality", "", "household_ref", nil)), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "DB-04")
	want(t, xa1.post(p, with(report(), "commune_code", "30002")), http.StatusForbidden, "extension.out_of_scope", "xã ngoài phân công")
	want(t, tinh.post(p, report()), http.StatusForbidden, "", "Cấp Tỉnh chỉ xem")
	want(t, khac.post(p, report()), http.StatusForbidden, "", "phòng khác")
	want(t, tw.get(p), http.StatusForbidden, "", "Cấp TW không có FN-11")

	k := key()
	ob := want(t, xa1.postK(p, k, report()), http.StatusCreated, "", "lập BM-10")
	ref := str(ob, "ref")
	if str(ob, "status") != "reported" || num(ob, "version") != "1" || str(ob, "as_of") != "2027-10-12" || str(ob, "disease_name") != "Dịch tả lợn châu Phi" ||
		str(obj(ob, "location"), "lat") != "10.25341" || str(ob, "chemical_liters") != "20.5" {
		t.Fatalf("bản 1: %v", ob)
	}
	again := xa1.postK(p, k, report()) // middleware Idempotency-Key phát lại phản hồi đã lưu
	if again.status/100 != 2 || str(again.body, "ref") != ref {
		t.Fatalf("gửi lại phải trả cùng ổ dịch: %v", again)
	}
	ev := f.outbox(port.EventOutbreakReported)
	if len(ev) != 1 || ev[0]["outbreak_ref"] != ref || ev[0]["notifiable"] != true || ev[0]["province_code"] != "89" {
		t.Fatalf("BR-01: đúng một sự kiện ổ dịch mới: %v", ev)
	}

	// Phạm vi: xã khác không thấy; Cấp Tỉnh thấy, có thống kê.
	if n := len(list(want(t, xa2.get(p), http.StatusOK, "", "xã khác"), "items")); n != 0 {
		t.Fatalf("xã khác thấy %d ổ dịch", n)
	}
	want(t, xa2.get(p+"/"+ref), http.StatusNotFound, "extension.cnts_outbreak_not_found", "xã khác xem chi tiết")
	tl := want(t, tinh.get(p+"?status=reported&from=2027-10-01&to=2027-10-31"), http.StatusOK, "", "Cấp Tỉnh danh sách")
	if len(list(tl, "items")) != 1 || num(list(tl, "by_status")[0].(map[string]any), "count") != "1" {
		t.Fatalf("Cấp Tỉnh: %v", tl)
	}

	// Diễn biến: khóa lạc quan, ngày không lùi, đủ trường mỗi phiên bản.
	up := with(report(), "dead", 30, "culled_head", 90, "culled_kg", "6300.5", "as_of", "2027-10-13", "progress_note", "Thêm 25 con chết, tiêu hủy 90 con")
	want(t, xa1.post(p+"/"+ref+"/progress", up), http.StatusUnprocessableEntity, "validation_failed", "thiếu base_version")
	want(t, xa1.post(p+"/"+ref+"/progress", with(up, "base_version", 2)), http.StatusConflict, "extension.cnts_outbreak_version_conflict", "sai phiên bản")
	want(t, xa1.post(p+"/"+ref+"/progress", with(up, "base_version", 1, "as_of", "2027-10-11")), http.StatusUnprocessableEntity,
		"extension.cnts_outbreak_invalid", "ngày diễn biến lùi")
	want(t, xa1.post(p+"/"+ref+"/progress", with(up, "base_version", 1, "commune_code", "30002")), http.StatusUnprocessableEntity,
		"extension.cnts_outbreak_invalid", "đổi xã")
	v2 := want(t, xa1.post(p+"/"+ref+"/progress", with(up, "base_version", 1)), http.StatusCreated, "", "cập nhật diễn biến")
	if num(v2, "version") != "2" || str(v2, "status") != "monitoring" || num(v2, "dead") != "30" || str(v2, "culled_kg") != "6300.5" {
		t.Fatalf("bản 2: %v", v2)
	}

	// Kết thúc: Cấp Tỉnh; BGĐ chỉ xem; Xã không cập nhật ổ dịch đã kết thúc; mở lại.
	want(t, xa1.post(p+"/"+ref+"/close", map[string]any{"base_version": 2, "reason": "Hết 21 ngày"}), http.StatusForbidden, "", "Xã không kết thúc")
	want(t, bgd.post(p+"/"+ref+"/close", map[string]any{"base_version": 2, "reason": "Hết 21 ngày"}), http.StatusForbidden, "extension.read_only", "BGĐ chỉ xem")
	want(t, tinh.post(p+"/"+ref+"/reopen", map[string]any{"base_version": 2, "reason": "x"}), http.StatusConflict, "extension.cnts_outbreak_not_closed", "mở lại khi chưa kết thúc")
	cl := want(t, tinh.post(p+"/"+ref+"/close", map[string]any{"base_version": 2, "reason": "Hết 21 ngày không phát sinh"}), http.StatusCreated, "", "kết thúc")
	if str(cl, "status") != "closed" || num(cl, "dead") != "30" || str(cl, "status_reason") != "Hết 21 ngày không phát sinh" {
		t.Fatalf("bản kết thúc chụp nội dung hiện hành: %v", cl)
	}
	want(t, xa1.post(p+"/"+ref+"/progress", with(up, "base_version", 3)), http.StatusConflict, "extension.cnts_outbreak_closed", "cập nhật ổ dịch đã kết thúc")
	pts := want(t, tinh.get("/v1/extension/cnts/outbreak-points"), http.StatusOK, "", "lớp bản đồ")
	if len(list(pts, "features")) != 0 {
		t.Fatalf("mặc định bỏ ổ dịch đã kết thúc: %v", pts)
	}
	want(t, tinh.post(p+"/"+ref+"/reopen", map[string]any{"base_version": 3, "reason": "Tái phát"}), http.StatusCreated, "", "mở lại")

	d := want(t, tinh.get(p+"/"+ref), http.StatusOK, "", "chi tiết")
	vs := list(d, "versions")
	if len(vs) != 4 || str(obj(d, "current"), "status") != "monitoring" || str(vs[3].(map[string]any), "event") != "reported" {
		t.Fatalf("BR-02 mỗi lần cập nhật một phiên bản: %v", d)
	}
	pts = want(t, tinh.get("/v1/extension/cnts/outbreak-points"), http.StatusOK, "", "lớp bản đồ")
	fs := list(pts, "features")
	if len(fs) != 1 {
		t.Fatalf("điểm ổ dịch: %v", pts)
	}
	geom := obj(fs[0].(map[string]any), "geometry")
	if c := list(geom, "coordinates"); len(c) != 2 || c[0].(json.Number) != "105.9721" || c[1].(json.Number) != "10.25341" {
		t.Fatalf("GeoJSON [kinh độ, vĩ độ]: %v", geom)
	}
}

// FN-11: Cấp Xã ghi nhận tin báo nghi dịch, xác nhận thành ổ dịch (một giao dịch) hoặc bác bỏ.
func TestFN11_SuspectConfirmDismiss(t *testing.T) {
	f := newFix(t)
	xa1 := f.user(xa("30001"))
	tinh := f.user(authz.KNContext{Role: authz.KNTinh, Department: cnts, Scope: authz.Scope{All: true}})
	const p = "/v1/extension/cnts/suspects"
	tin := map[string]any{"commune_code": "30001", "reporter_kind": "nong_ho", "observed_at": "2027-10-13T07:00:00+07:00",
		"species_code": "heo", "locality": "Ấp 3", "affected": 10, "dead": 2, "description": "Heo bỏ ăn, chết rải rác"}

	want(t, tinh.post(p, tin), http.StatusForbidden, "", "Cấp Tỉnh không ghi tin báo")
	want(t, xa1.post(p, with(tin, "description", "")), http.StatusUnprocessableEntity, "extension.cnts_suspect_invalid", "thiếu mô tả")
	s1 := want(t, xa1.post(p, tin), http.StatusCreated, "", "tin báo không vị trí")
	s2 := want(t, xa1.post(p, with(tin, "location", map[string]any{"lat": "10.1", "lon": "105.8", "accuracy_m": "12"})), http.StatusCreated, "", "tin báo có vị trí")
	if n := len(list(want(t, tinh.get(p+"?status=reported"), http.StatusOK, "", "Cấp Tỉnh xem tin báo"), "items")); n != 2 {
		t.Fatalf("Cấp Tỉnh thấy %d tin báo", n)
	}

	body := map[string]any{"base_version": 1, "disease_code": "dtlcp", "species_code": "heo", "herd_size": 40, "dead": 2,
		"symptoms": "Sốt, xuất huyết", "locality": "Ấp 3"}
	want(t, xa1.post(p+"/"+str(s1, "ref")+"/confirm", body), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "xác nhận thiếu vị trí")
	ob := want(t, xa1.post(p+"/"+str(s2, "ref")+"/confirm", body), http.StatusCreated, "", "xác nhận → ổ dịch")
	if str(ob, "suspect_ref") != str(s2, "ref") || str(ob, "source") != "nong_ho" || str(obj(ob, "location"), "lat") != "10.1" ||
		str(ob, "received_at") != "2027-10-13T00:00:00Z" {
		t.Fatalf("ổ dịch từ tin báo: %v", ob)
	}
	sd := want(t, xa1.get(p+"/"+str(s2, "ref")), http.StatusOK, "", "tin báo đã xác nhận")
	if cur := obj(sd, "current"); str(cur, "status") != "confirmed" || str(cur, "outbreak_ref") != str(ob, "ref") {
		t.Fatalf("tin báo: %v", sd)
	}
	want(t, xa1.post(p+"/"+str(s2, "ref")+"/confirm", body), http.StatusConflict, "extension.cnts_suspect_resolved", "xác nhận lần hai")
	if n := len(f.outbox(port.EventOutbreakReported)); n != 1 {
		t.Fatalf("xác nhận phát một sự kiện ổ dịch mới, có %d", n)
	}

	want(t, xa1.post(p+"/"+str(s1, "ref")+"/dismiss", map[string]any{"base_version": 1}), http.StatusUnprocessableEntity, "extension.cnts_suspect_invalid", "thiếu lý do")
	ds := want(t, xa1.post(p+"/"+str(s1, "ref")+"/dismiss", map[string]any{"base_version": 1, "reason": "Heo bị ngộ độc thức ăn"}), http.StatusCreated, "", "bác bỏ")
	if str(ds, "status") != "dismissed" || num(ds, "version") != "2" {
		t.Fatalf("bác bỏ: %v", ds)
	}
}
