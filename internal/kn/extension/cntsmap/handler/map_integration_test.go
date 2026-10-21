package handler_test

// W11.52 (D4 CN12, BRD CNTS v3.7 FN-18, UR-TINH-17): bản đồ vùng nuôi, vùng dịch trên PostgreSQL/PostGIS thật qua module
// pf/extension (vai extension_app). Ngữ cảnh phiên KN giả lập bằng middleware X-AG-Context (phiên do module sở hữu);
// đơn vị hành chính, danh mục CNTS là bản giả. Ổ dịch BM-10 (W11.49), điểm trình diễn, hộ mô hình có tọa độ (W11.47) được
// ghi thẳng vào sổ của pf_extension bằng vai quản trị kiểm thử.

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
	"cnts_species:HEO":   {Catalog: "cnts_species", Code: "HEO", Name: "Heo", Active: true},
	"cnts_disease:DTHCP": {Catalog: "cnts_disease", Code: "DTHCP", Name: "Dịch tả heo châu Phi", Active: true},
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
	pool   *pgxpool.Pool
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

func (f *fix) userIn(tenant uuid.UUID, kc authz.KNContext) user {
	sub := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID, ref: sub}
}

func (f *fix) user(kc authz.KNContext) user { return f.userIn(f.tenant, kc) }

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

func (f *fix) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *fix) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// seedOutbreak ghi một ổ dịch BM-10 (W11.49) tại (lat, lng) của xã commune, ngày phát hiện asOf.
func (f *fix) seedOutbreak(commune string, lat, lng float64, asOf string) uuid.UUID {
	ref := ids.New()
	f.exec(`INSERT INTO pf_extension.cnts_outbreak (id, tenant_id, stream_key, outbreak_ref, event, commune_code, disease_code,
	    species_code, received_at, source, locality, location, location_source, herd_size, dead, symptoms, as_of, event_time, actor_ref,
	    idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, 'reported', $5, 'DTHCP', 'HEO', now(), 'can_bo', 'Ấp 1', ST_SetSRID(ST_MakePoint($6, $7), 4326), 'gps',
	    120, 15, 'Sốt, bỏ ăn', $8::date, now(), $9, $10, 'test')`,
		ids.New(), f.tenant, "outbreak:"+ref.String(), ref, commune, lng, lat, asOf, ids.New(), "seed-"+ref.String())
	return ref
}

// seedSite ghi một điểm trình diễn (W11.47) có tọa độ.
func (f *fix) seedSite(commune, name string, lat, lng float64) uuid.UUID {
	ref := ids.New()
	f.exec(`INSERT INTO pf_extension.cnts_demo_site (id, tenant_id, stream_key, site_ref, event, project_ref, commune_code, name,
	    model_type_code, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, 'created', $5, $6, $7, 'HEO_TP', now(), $8, $9, 'test')`,
		ids.New(), f.tenant, "csite:"+ref.String(), ref, ids.New(), commune, name, ids.New(), "seed-"+ref.String())
	f.exec(`INSERT INTO pf_extension.cnts_model_point (tenant_id, kind, ref, point, source, actor_ref)
	  VALUES ($1, 'site', $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), 'map', $5)`, f.tenant, ref, lng, lat, ids.New())
	return ref
}

// seedHousehold ghi một hộ mô hình (W11.47) của điểm site có tọa độ.
func (f *fix) seedHousehold(site uuid.UUID, commune string, lat, lng float64) uuid.UUID {
	ref := ids.New()
	f.exec(`INSERT INTO pf_extension.cnts_model_household (id, tenant_id, stream_key, household_ref, event, site_ref, project_ref,
	    commune_code, person_ref, keeper_kind, self_entry, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, 'created', $5, $6, $7, $8, 'self', true, now(), $9, $10, 'test')`,
		ids.New(), f.tenant, "chh:"+ref.String(), ref, site, ids.New(), commune, ids.New(), ids.New(), "seed-"+ref.String())
	f.exec(`INSERT INTO pf_extension.cnts_model_point (tenant_id, kind, ref, point, source, actor_ref)
	  VALUES ($1, 'household', $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), 'gps', $5)`, f.tenant, ref, lng, lat, ids.New())
	return ref
}

func square(lng, lat, d float64) map[string]any {
	return map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{lng, lat}, {lng + d, lat}, {lng + d, lat + d}, {lng, lat + d}, {lng, lat}}}}
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

func refs(items []any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		out[str(m, "ref")] = m
	}
	return out
}

const base = "/v1/extension/cnts/map/"

func TestCNTSMap_W1152(t *testing.T) {
	f := newFix(t)
	officer := f.user(tinh("vien_chuc")) // cán bộ phụ trách dịch
	leader := f.user(tinh("lanh_dao_phong"))
	viewOnlyCtx := tinh("vien_chuc")
	viewOnlyCtx.ViewOnly = true
	viewOnly := f.user(viewOnlyCtx)
	xa1 := f.user(xa("30001"))
	xa2 := f.user(xa("30002"))

	// Ổ dịch tại (10.40, 105.10) xã 30001; điểm ~1,1 km, hộ ~5 km (cùng xã), hộ ~20 km (xã 30002).
	ob := f.seedOutbreak("30001", 10.40, 105.10, "2027-10-10")
	site := f.seedSite("30001", "Điểm heo ATSH", 10.41, 105.10)
	h1 := f.seedHousehold(site, "30001", 10.445, 105.10)
	h2 := f.seedHousehold(site, "30002", 10.58, 105.10)

	// ----- Quyền vẽ (ma trận FN-18: viên chức chuyên môn CRU; nhóm khác, Cấp Xã chỉ xem; ngữ cảnh chỉ xem không ghi) -----
	livestock := map[string]any{"kind": "vung_chan_nuoi", "name": "Vùng chăn nuôi heo tập trung", "communes": []string{"30002"},
		"draw_mode": "polygon", "geometry": square(105.20, 10.55, 0.02), "species_code": "HEO", "effective_on": "2027-10-01"}
	want(t, leader.post(base+"zones", livestock), 403, "", "lãnh đạo phòng chỉ xem (FN-18)")
	want(t, viewOnly.post(base+"zones", livestock), 403, "extension.read_only", "ngữ cảnh chỉ xem")
	want(t, xa1.post(base+"zones", livestock), 403, "", "Cấp Xã chỉ xem")
	want(t, officer.do(http.MethodPost, base+"zones", "", livestock), 400, "idempotency_key_required", "thiếu Idempotency-Key")

	// ----- Tính hợp lệ hình học -----
	bowtie := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.2, 10.5}, {105.3, 10.6}, {105.3, 10.5}, {105.2, 10.6}, {105.2, 10.5}}}}
	r := want(t, officer.post(base+"zones", merge(livestock, map[string]any{"geometry": bowtie})), 422, "extension.cnts_zone_invalid",
		"đa giác tự cắt (ST_IsValid)")
	if !strings.Contains(string(r["errors"].([]any)[0].(map[string]any)["reason"].(string)), "Self-intersection") {
		t.Fatalf("lý do hình không hợp lệ: %s", r)
	}
	open := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.2, 10.5}, {105.3, 10.5}, {105.3, 10.6}, {105.2, 10.6}}}}
	want(t, officer.post(base+"zones", merge(livestock, map[string]any{"geometry": open})), 422, "extension.cnts_zone_invalid", "vòng hở")
	want(t, officer.post(base+"zones", merge(livestock, map[string]any{"geometry": square(2.3, 48.8, 0.1)})), 422,
		"extension.cnts_zone_invalid", "ngoài Việt Nam")
	want(t, officer.post(base+"zones", merge(livestock, map[string]any{"communes": []string{"31001"}})), 403, "extension.out_of_scope",
		"xã tỉnh khác")
	want(t, officer.post(base+"zones", merge(livestock, map[string]any{"effective_on": "2027-10-20"})), 422,
		"extension.cnts_zone_invalid", "ngày áp dụng tương lai")
	want(t, officer.post(base+"zones", merge(livestock, map[string]any{"note": "Liên hệ 0912345678"})), 422,
		"extension.cnts_zone_invalid", "SĐT trong ghi chú")
	want(t, officer.post(base+"zones", merge(livestock, map[string]any{"outbreak_ref": ob.String()})), 422,
		"extension.cnts_zone_invalid", "vùng chăn nuôi không gắn ổ dịch")
	// Ràng buộc DB cũng chặn hình không hợp lệ (kể cả khi bỏ qua ứng dụng).
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO pf_extension.cnts_map_zone (id, tenant_id, stream_key, zone_ref, event,
	    province_code, communes, kind, name, draw_mode, geom, effective_on, event_time, actor_ref, idem_key, rule_version)
	  SELECT $1, $2, 'czone:' || $3::uuid::text, $3::uuid, 'created', '89', '{30002}', 'vung_chan_nuoi', 'x', 'polygon',
	    ST_GeomFromText('POLYGON((105.2 10.5,105.3 10.6,105.3 10.5,105.2 10.6,105.2 10.5))', 4326), '2027-10-01', now(), $4, 'bad', 'test'`,
		ids.New(), f.tenant, ids.New(), ids.New()); err == nil || !strings.Contains(err.Error(), "cnts_map_zone_valid") {
		t.Fatalf("DB phải chặn hình tự cắt: %v", err)
	}

	// ----- Vẽ vùng chăn nuôi (đa giác) -----
	k := key()
	z := want(t, officer.postK(base+"zones", k, livestock), 201, "", "vẽ vùng chăn nuôi")
	zref := str(z, "ref")
	if num(z, "version") != "1" || str(z, "province_code") != "89" || str(z, "status") != "active" || z["can_edit"] != true ||
		str(z, "area_ha") == "" || str(z["geometry"].(map[string]any), "type") != "Polygon" || str(z, "effective_on") != "2027-10-01" {
		t.Fatalf("vùng: %s", z)
	}
	if z2 := want(t, officer.postK(base+"zones", k, livestock), 201, "", "gọi lặp (middleware phát lại)"); str(z2, "ref") != zref {
		t.Fatalf("gọi lặp phải trả lại vùng cũ: %s", z2)
	}

	// ----- Khoanh vùng từ ổ dịch BM-10 theo bán kính (500 m / 3 km / 10 km) -----
	want(t, officer.post(base+"zones/from-outbreak", map[string]any{"outbreak_ref": ob.String(),
		"radii": map[string]any{"vung_dich": 3000, "vung_uy_hiep": 1000}}), 422, "extension.cnts_zone_invalid", "bán kính không tăng dần")
	want(t, officer.post(base+"zones/from-outbreak", map[string]any{"outbreak_ref": ids.New().String()}), 422,
		"extension.cnts_zone_invalid", "ổ dịch không có")
	ko := key()
	fo := want(t, officer.postK(base+"zones/from-outbreak", ko, map[string]any{"outbreak_ref": ob.String()}), 201, "", "khoanh từ ổ dịch")
	zs := list(fo, "items")
	if len(zs) != 3 {
		t.Fatalf("muốn 3 vùng: %s", fo)
	}
	byKind := map[string]map[string]any{}
	for _, it := range zs {
		m := it.(map[string]any)
		byKind[str(m, "kind")] = m
	}
	outbreakZone, threat, buffer := byKind["vung_dich"], byKind["vung_uy_hiep"], byKind["vung_dem"]
	if str(outbreakZone, "outbreak_ref") != ob.String() || str(threat, "parent_zone_ref") != str(outbreakZone, "ref") ||
		str(buffer, "parent_zone_ref") != str(outbreakZone, "ref") || num(threat, "radius_m") != "3000" || num(buffer, "radius_m") != "10000" ||
		str(buffer, "draw_mode") != "radius" || str(buffer, "disease_code") != "DTHCP" || str(buffer, "effective_on") != "2027-10-13" {
		t.Fatalf("vùng từ ổ dịch: %s", fo)
	}
	if again := want(t, officer.postK(base+"zones/from-outbreak", ko, map[string]any{"outbreak_ref": ob.String()}), 201, "", "lặp"); len(list(again, "items")) != 3 {
		t.Fatalf("gọi lặp khoanh vùng: %s", again)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_map_zone WHERE tenant_id = $1 AND outbreak_ref = $2`, f.tenant, ob); n != 3 {
		t.Fatalf("gọi lặp không được ghi thêm: %d", n)
	}

	// ----- Truy vấn đối tượng trong vùng đệm (ST_Covers) -----
	in3 := want(t, xa1.get(base+"zones/"+str(threat, "ref")+"/contents"), 200, "", "trong vùng uy hiếp 3 km")
	places := map[string]bool{}
	for _, p := range list(in3, "places") {
		places[str(p.(map[string]any), "ref")] = true
	}
	if !places[site.String()] || places[h1.String()] || places[h2.String()] || num(in3, "outbreak_count") != "1" {
		t.Fatalf("vùng 3 km: %s", in3)
	}
	in10 := want(t, officer.get(base+"zones/"+str(buffer, "ref")+"/contents"), 200, "", "trong vùng đệm 10 km")
	places = map[string]bool{}
	for _, p := range list(in10, "places") {
		pm := p.(map[string]any)
		places[str(pm, "ref")] = true
		if _, err := pm["distance_m"].(json.Number).Float64(); err != nil {
			t.Fatalf("khoảng cách: %s", pm)
		}
	}
	if !places[site.String()] || !places[h1.String()] || places[h2.String()] || num(in10, "household_count") != "1" {
		t.Fatalf("vùng 10 km: %s", in10)
	}

	// ----- Phiên bản: sửa hình có lý do, theo ngày (BR-01, AC-03) -----
	upd := merge(livestock, map[string]any{"base_version": 1, "geometry": square(105.20, 10.55, 0.04), "effective_on": "2027-10-12",
		"reason": "Mở rộng vùng theo số liệu điều tra"})
	want(t, officer.post(base+"zones/"+zref, merge(upd, map[string]any{"reason": ""})), 422, "", "thiếu lý do")
	want(t, officer.post(base+"zones/"+zref, merge(upd, map[string]any{"base_version": 7})), 409, "extension.cnts_zone_version_conflict", "lệch phiên bản")
	want(t, officer.post(base+"zones/"+zref, merge(upd, map[string]any{"effective_on": "2027-09-01"})), 422, "extension.cnts_zone_invalid",
		"ngày áp dụng sớm hơn bản trước")
	want(t, leader.post(base+"zones/"+zref, upd), 403, "", "lãnh đạo phòng không sửa")
	u := want(t, officer.post(base+"zones/"+zref, upd), 200, "", "sửa vùng")
	if num(u, "version") != "2" || str(u, "reason") != "Mở rộng vùng theo số liệu điều tra" || str(u, "area_ha") == str(z, "area_ha") {
		t.Fatalf("phiên bản 2: %s", u)
	}
	at := func(day string) map[string]map[string]any {
		return refs(list(want(t, officer.get(base+"zones?date="+day), 200, "", "vùng ngày "+day), "items"))
	}
	if v := at("2027-10-05"); v[zref] == nil || num(v[zref], "version") != "1" || v[str(buffer, "ref")] != nil {
		t.Fatalf("ngày 05/10 thấy bản 1, chưa có vùng dịch: %v", v)
	}
	if v := at("2027-10-12"); num(v[zref], "version") != "2" {
		t.Fatalf("ngày 12/10 thấy bản 2: %v", v)
	}
	if v := at("2027-09-30"); len(v) != 0 {
		t.Fatalf("trước ngày áp dụng không có vùng: %v", v)
	}

	// ----- Ngưng hiển thị (không xóa cứng), khôi phục -----
	want(t, officer.post(base+"zones/"+zref+"/retire", map[string]any{"base_version": 2, "reason": "x"}), 422, "", "lý do ngắn")
	ret := want(t, officer.post(base+"zones/"+zref+"/retire", map[string]any{"base_version": 2, "reason": "Giải thể vùng nuôi"}), 200, "", "ngưng")
	if str(ret, "status") != "retired" || num(ret, "version") != "3" || str(ret, "area_ha") != str(u, "area_ha") {
		t.Fatalf("ngưng: %s", ret)
	}
	want(t, officer.post(base+"zones/"+zref, merge(upd, map[string]any{"base_version": 3, "effective_on": "2027-10-13"})), 409,
		"extension.cnts_zone_retired", "sửa vùng đã ngưng")
	if v := at("2027-10-13"); v[zref] != nil {
		t.Fatal("hôm nay không còn vùng đã ngưng")
	}
	if v := at("2027-10-12"); v[zref] == nil {
		t.Fatal("ngày cũ vẫn thấy vùng (AC-03)")
	}
	full := want(t, officer.get(base+"zones/"+zref), 200, "", "chi tiết")
	if vs := list(full, "versions"); len(vs) != 3 || str(vs[0].(map[string]any), "event") != "retired" {
		t.Fatalf("lịch sử phiên bản: %s", full)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_map_zone WHERE zone_ref = $1`, uuid.MustParse(zref)); n != 3 {
		t.Fatalf("sổ chỉ ghi thêm: %d dòng", n)
	}
	want(t, officer.post(base+"zones/"+zref+"/restore", map[string]any{"base_version": 3, "reason": "Khôi phục theo chỉ đạo"}), 200, "", "khôi phục")

	// ----- Phạm vi Cấp Xã (FN-18 AC-02) -----
	l1 := want(t, xa1.get(base+"layers"), 200, "", "lớp Cấp Xã 30001")
	z1 := refs(list(l1, "zones"))
	if z1[zref] != nil || len(z1) != 3 || l1["can_draw"] != false || len(list(l1, "outbreaks")) != 1 || len(list(l1, "places")) != 2 {
		t.Fatalf("xã 30001: %s", l1)
	}
	want(t, xa1.get(base+"zones/"+zref), 404, "extension.cnts_zone_not_found", "xã khác không thấy vùng")
	if l2 := want(t, xa2.get(base+"layers"), 200, "", "lớp Cấp Xã 30002"); len(list(l2, "zones")) != 1 || len(list(l2, "outbreaks")) != 0 ||
		len(list(l2, "places")) != 1 {
		t.Fatalf("xã 30002: %s", l2)
	}
	lt := want(t, officer.get(base+"layers"), 200, "", "lớp Cấp Tỉnh")
	if len(list(lt, "zones")) != 4 || len(list(lt, "places")) != 3 || lt["can_draw"] != true {
		t.Fatalf("Cấp Tỉnh: %s", lt)
	}
	if l := want(t, leader.get(base+"layers?date=2027-10-11"), 200, "", "lớp ngày cũ"); len(list(l, "zones")) != 1 || l["can_draw"] != false {
		t.Fatalf("ngày 11/10: %s", l)
	}

	// ----- Cách ly tenant (RLS) -----
	other := f.userIn(ids.New(), tinh("vien_chuc"))
	if l := want(t, other.get(base+"layers"), 200, "", "tenant khác"); len(list(l, "zones")) != 0 || len(list(l, "outbreaks")) != 0 ||
		len(list(l, "places")) != 0 {
		t.Fatalf("tenant khác thấy dữ liệu: %s", l)
	}
	want(t, other.get(base+"zones/"+zref), 404, "extension.cnts_zone_not_found", "tenant khác đọc vùng")
	want(t, other.post(base+"zones/from-outbreak", map[string]any{"outbreak_ref": ob.String()}), 422, "extension.cnts_zone_invalid",
		"tenant khác không thấy ổ dịch")
}
