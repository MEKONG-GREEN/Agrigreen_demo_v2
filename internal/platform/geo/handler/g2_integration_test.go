package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ---------------------------------------------------------------------------
// Giả lập pf/cooperative cho G2 (W3.3): lớp HTX / lớp thửa của bản đồ gộp (geoport.MapLayers) và dữ kiện thửa cho
// log GPS (geoport.PlotGeometry). Hiện thực thật nằm ở pf/cooperative (kiểm thử riêng ở cooperative/plot/handler).
// ---------------------------------------------------------------------------

type fakeMapLayers struct {
	mu     sync.Mutex
	tenant uuid.UUID // tenant của đối tượng giả — tenant khác đọc rỗng (như hiện thực thật)
	coops  []map[string]any
	plots  []map[string]any
}

func feature(ref uuid.UUID, layer string) map[string]any {
	return map[string]any{"type": "Feature", "id": ref.String(), "geometry": map[string]any{"type": "Point", "coordinates": []float64{105.6, 10.05}},
		"properties": map[string]any{"ref": ref.String(), "layer": layer}}
}

func (f *fakeMapLayers) CooperativeLayer(ctx context.Context, _ port.MapLayerQuery) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	feats := []map[string]any{}
	if tid, err := tenant.FromContext(ctx); err == nil && tid == f.tenant {
		feats = append(feats, f.coops...)
	}
	return map[string]any{"type": "FeatureCollection", "features": feats, "truncated": false, "limit": 1000}, nil
}

func (f *fakeMapLayers) PlotLayer(ctx context.Context, _ port.MapLayerQuery) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	feats := []map[string]any{}
	if tid, err := tenant.FromContext(ctx); err == nil && tid == f.tenant {
		feats = append(feats, f.plots...)
	}
	return map[string]any{"type": "FeatureCollection", "features": feats, "truncated": false}, nil
}

// fakePlotGeometry: thửa giả cho log GPS — xã, HTX, khoảng cách cấu hình được.
type fakePlotGeometry struct {
	mu     sync.Mutex
	places map[uuid.UUID]port.PlotPlace
	dist   map[uuid.UUID]func(lon, lat decimal.Decimal) decimal.Decimal
	owners map[uuid.UUID][]uuid.UUID // nông hộ → thửa
}

func (f *fakePlotGeometry) put(ref, household uuid.UUID, p port.PlotPlace) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.places[ref] = p
	if household != uuid.Nil {
		f.owners[household] = append(f.owners[household], ref)
	}
}

func (f *fakePlotGeometry) PlotPlace(_ context.Context, ref uuid.UUID, lon, lat *decimal.Decimal) (port.PlotPlace, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.places[ref]
	if !ok {
		return port.PlotPlace{}, false, nil
	}
	p.DistanceM = nil
	if d := f.dist[ref]; d != nil && lon != nil && lat != nil {
		v := d(*lon, *lat).Round(1)
		p.DistanceM = &v
	}
	return p, true, nil
}

func (f *fakePlotGeometry) PlotsOfHouseholds(_ context.Context, hh []uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []uuid.UUID{}
	for _, h := range hh {
		out = append(out, f.owners[h]...)
	}
	return out, nil
}

type g2 struct {
	*fixture
	layers *fakeMapLayers
	plots  *fakePlotGeometry
	params *paramValues
	coopC  uuid.UUID // HTX ở xã 31120
	prov92 uuid.UUID // tổ chức cấp tỉnh Cần Thơ (92)
	prov89 uuid.UUID // tổ chức cấp tỉnh An Giang (89)
}

func newG2(t *testing.T) *g2 {
	params := &paramValues{m: map[string]decimal.Decimal{}}
	tol := decimal.NewFromInt(1)
	f := newFixture(t, func(d *geo.Deps) { d.Params = fakeParams{tolerance: &tol, values: params} })
	g := &g2{fixture: f, params: params, coopC: ids.New(), prov92: ids.New(), prov89: ids.New(),
		layers: &fakeMapLayers{tenant: f.tenant}, plots: &fakePlotGeometry{places: map[uuid.UUID]port.PlotPlace{}, owners: map[uuid.UUID][]uuid.UUID{},
			dist: map[uuid.UUID]func(lon, lat decimal.Decimal) decimal.Decimal{}}}
	f.mod.UseMapLayers(g.layers)
	f.mod.UsePlots(g.plots)
	f.scope.set(g.coopC, authz.Scope{Units: []string{"31120"}})
	f.scope.set(g.prov92, authz.Scope{Units: []string{"92", "31117", "31120"}})
	f.scope.set(g.prov89, authz.Scope{Units: []string{"89", "30001"}})
	return g
}

// geoAdmin92 là quản trị GIS cấp tỉnh Cần Thơ.
func (g *g2) geoAdmin92() caller { return g.as(g.tenant, g.prov92, port.RoleGeoAdmin) }
func (g *g2) geoAdmin89() caller { return g.as(g.tenant, g.prov89, port.RoleGeoAdmin) }

func (c caller) adminJSON(path string, body any) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.adminPost(path, "application/json", b)
}

func num(m any, path ...string) float64 {
	v, _ := get(m, path...).(float64)
	return v
}

func dec(t *testing.T, m any, path ...string) decimal.Decimal {
	t.Helper()
	s := str(m, path...)
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("%v không phải số: %q", path, s)
	}
	return d
}

func near(a, b decimal.Decimal, tol string) bool {
	return a.Sub(b).Abs().LessThanOrEqual(decimal.RequireFromString(tol))
}

// point dựng GeoJSON Point cách gốc (east, north) mét.
func point(east, north float64) map[string]any {
	dLat, dLon := degPerMetre(baseLat)
	return map[string]any{"type": "Point", "coordinates": []float64{baseLon + east*dLon, baseLat + north*dLat}}
}

// line dựng GeoJSON LineString qua các điểm (east, north) mét.
func line(pts ...[2]float64) map[string]any {
	dLat, dLon := degPerMetre(baseLat)
	cs := make([][]float64, 0, len(pts))
	for _, p := range pts {
		cs = append(cs, []float64{baseLon + p[0]*dLon, baseLat + p[1]*dLat})
	}
	return map[string]any{"type": "LineString", "coordinates": cs}
}

func g2BBox(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%sbbox=%f,%f,%f,%f", path, sep, baseLon-0.05, baseLat-0.05, baseLon+0.1, baseLat+0.1)
}

func featureRefs(r resp) []string {
	var out []string
	for _, ft := range list(r.body, "features") {
		out = append(out, str(ft, "id"))
	}
	return out
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func verifyChain(t *testing.T, f *fixture, table, stream string) {
	t.Helper()
	ctx := context.Background()
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		res, err := ledger.Verify(ctx, tx, table, f.tenant, stream)
		if err != nil {
			return err
		}
		if !res.OK || res.RowsChecked < 1 {
			return fmt.Errorf("%s %s: chuỗi không hợp lệ %+v", table, stream, res)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mustFailUpdate(t *testing.T, f *fixture, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	})
	if err == nil {
		t.Fatalf("phải thất bại (sổ chỉ ghi thêm): %s", sql)
	}
}

func outboxCount(t *testing.T, f *fixture, eventType, key, val string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND payload->>$2 = $3", eventType, key, val).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Ranh giới HTX
// ---------------------------------------------------------------------------

// Cách ly tenant (RB-2) cho ranh giới HTX, kho/Hub, đường bộ.
func TestG2TenantIsolation(t *testing.T) {
	g := newG2(t)
	admin := g.national()
	expect(t, admin.adminJSON("/v1/geo/facilities", map[string]any{"kind": "kho", "name": "Kho A",
		"point": point(100, 100), "province_code": "92", "commune_code": "31117", "capacity": "500"}), http.StatusCreated, "tạo kho")
	expect(t, admin.adminJSON("/v1/geo/roads", map[string]any{"name": "ĐT 922", "road_class": "tinh_lo", "max_load_tons": "30",
		"geometry": line([2]float64{0, 0}, [2]float64{1000, 0}), "province_code": "92"}), http.StatusCreated, "tạo đường")

	other := g.as(ids.New(), uuid.Nil, port.RoleGeoAdmin, authz.RolePlatformAdmin)
	for _, p := range []string{"/v1/geo/facilities", "/v1/geo/roads"} {
		if n := len(featureRefs(other.appDo(http.MethodGet, g2BBox(p), nil))); n != 0 {
			t.Fatalf("tenant khác thấy %d đối tượng ở %s", n, p)
		}
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu (kho, đường; ranh giới HTX ở pf/cooperative).
func TestG2IdempotentReplay(t *testing.T) {
	g := newG2(t)
	for _, tc := range []struct {
		base, path string
		c          caller
		body       map[string]any
	}{
		{g.admin.URL, "/v1/geo/facilities", g.national(), map[string]any{"kind": "hub", "name": "Hub Thới Lai", "point": point(10, 10), "province_code": "92", "commune_code": "31117", "capacity": 1000}},
		{g.admin.URL, "/v1/geo/roads", g.national(), map[string]any{"name": "Bờ kênh 1", "road_class": "bo_ruong", "max_load_tons": 2, "geometry": line([2]float64{0, 0}, [2]float64{0, 300}), "province_code": "92"}},
	} {
		b, _ := json.Marshal(tc.body)
		k := key()
		r1 := tc.c.do(tc.base, http.MethodPost, tc.path, k, "application/json", b)
		r2 := tc.c.do(tc.base, http.MethodPost, tc.path, k, "application/json", b)
		expect(t, r1, http.StatusCreated, tc.path+" lần 1")
		if r2.status != r1.status || r2.hdr.Get("Idempotent-Replayed") != "true" || !bytes.Equal(r1.raw, r2.raw) {
			t.Fatalf("%s phát lại khác lần đầu:\n%s\n%s", tc.path, r1.raw, r2.raw)
		}
	}
	if n := len(featureRefs(g.national().appDo(http.MethodGet, g2BBox("/v1/geo/facilities"), nil))); n != 1 {
		t.Fatalf("số kho sau 2 lần gọi = %d", n)
	}
}

// ---------------------------------------------------------------------------
// Kho / Hub
// ---------------------------------------------------------------------------

// GIS US-BND-05 AC-1/AC-2, US-BND-06 AC-1/AC-2/AC-3 (BR-14/19): thêm, sửa (bản ghi
// bù), xóa mềm kho/Hub; thông báo lỗi đúng câu chữ; phạm vi tỉnh.
func TestFacilityLedger_GIS_US_BND_05_06(t *testing.T) {
	g := newG2(t)
	admin := g.geoAdmin92()
	base := map[string]any{"kind": "kho", "name": "Kho lúa Thới Lai", "point": point(200, 200),
		"province_code": "92", "commune_code": "31117", "capacity": "1500.5", "unit": "tan"}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range base {
			m[kk] = vv
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	for _, tc := range []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"thiếu tên", with("name", ""), domain.MsgFacilityRequired},
		{"thiếu địa chỉ (xã)", with("commune_code", nil), domain.MsgFacilityRequired},
		{"sức chứa 0", with("capacity", 0), domain.MsgCapacityPositive},
		{"sức chứa âm", with("capacity", "-5"), domain.MsgCapacityPositive},
		{"sức chứa sai định dạng", with("capacity", "một nghìn"), domain.MsgCapacityPositive},
	} {
		r := admin.adminJSON("/v1/geo/facilities", tc.body)
		if r.status != http.StatusUnprocessableEntity || str(r.body, "detail") != tc.msg {
			t.Fatalf("%s: %d %s", tc.name, r.status, r.raw)
		}
	}
	// Ngoài phạm vi tỉnh.
	expect(t, g.geoAdmin89().adminJSON("/v1/geo/facilities", base), http.StatusForbidden, "admin tỉnh khác")

	r := admin.adminJSON("/v1/geo/facilities", with("code", "KHO-TL-01"))
	expect(t, r, http.StatusCreated, "tạo kho")
	ref := str(r.body, "facility", "id")
	ev1 := str(r.body, "event", "id")
	if str(r.body, "facility", "properties", "nominal_capacity") != "1500.500" || num(r.body, "facility", "properties", "version") != 1 {
		t.Fatalf("kho: %s", r.raw)
	}
	expect(t, admin.adminJSON("/v1/geo/facilities", with("code", "KHO-TL-01")), http.StatusConflict, "trùng mã")
	// Chỉ vẽ khuôn viên → marker là điểm trong vùng; diện tích trắc địa.
	r = admin.adminJSON("/v1/geo/facilities", with("point", nil))
	expect(t, r, http.StatusUnprocessableEntity, "không có vị trí")
	poly := with("point", nil)
	poly["polygon"], poly["kind"], poly["name"] = rect(1000, 1000, 100, 100), "hub", "Hub Trường Xuân"
	r = admin.adminJSON("/v1/geo/facilities", poly)
	expect(t, r, http.StatusCreated, "tạo hub bằng khuôn viên")
	if get(r.body, "facility", "properties", "footprint") == nil || !near(dec(t, r.body, "facility", "properties", "area_ha"), decimal.NewFromInt(1), "0.01") ||
		!strings.HasPrefix(str(r.body, "facility", "properties", "code"), "HUB-") {
		t.Fatalf("hub khuôn viên: %s", r.raw)
	}

	// US-BND-06 AC-1: sửa = phiên bản mới kèm lý do; AC-3: xóa trắng tên bị chặn.
	vpath := "/v1/geo/facilities/" + ref + "/versions"
	r = admin.adminJSON(vpath, map[string]any{"supersedes_id": ev1, "correction_reason": "Cập nhật sức chứa sau nâng cấp", "name": ""})
	if r.status != http.StatusUnprocessableEntity || str(r.body, "detail") != domain.MsgFacilityRequired {
		t.Fatalf("sửa xóa tên: %d %s", r.status, r.raw)
	}
	r = admin.adminJSON(vpath, map[string]any{"supersedes_id": ev1, "correction_reason": "Cập nhật sức chứa sau nâng cấp", "capacity": 2000, "point": point(210, 205)})
	expect(t, r, http.StatusCreated, "sửa kho")
	if num(r.body, "facility", "properties", "version") != 2 || str(r.body, "facility", "properties", "nominal_capacity") != "2000.000" ||
		str(r.body, "event", "supersedes_id") != ev1 || str(r.body, "facility", "properties", "name") != "Kho lúa Thới Lai" {
		t.Fatalf("phiên bản 2: %s", r.raw)
	}
	r = admin.adminJSON(vpath, map[string]any{"supersedes_id": ev1, "correction_reason": "Sửa dựa trên phiên bản cũ", "capacity": 10})
	if r.status != http.StatusConflict || code(r) != "geo.version_conflict" {
		t.Fatalf("sửa trên bản cũ: %d %s", r.status, r.raw)
	}
	// AC-2: xóa mềm — ẩn khỏi bản đồ, lịch sử + người xóa còn đủ.
	r = admin.adminJSON("/v1/geo/facilities/"+ref+"/retire", map[string]any{"reason": "Kho ngừng hợp đồng thuê"})
	expect(t, r, http.StatusOK, "ngừng dùng kho")
	viewer := g.as(g.tenant, g.orgA, rolePlotViewer)
	if has(featureRefs(viewer.appDo(http.MethodGet, g2BBox("/v1/geo/facilities"), nil)), ref) {
		t.Fatal("kho đã ngừng dùng vẫn hiện")
	}
	if !has(featureRefs(viewer.appDo(http.MethodGet, g2BBox("/v1/geo/facilities?include_retired=true"), nil)), ref) {
		t.Fatal("include_retired phải thấy kho đã ngừng dùng")
	}
	r = viewer.appDo(http.MethodGet, "/v1/geo/facilities/"+ref, nil)
	evs := list(r.body, "events")
	if len(evs) != 3 || str(evs[2], "action") != "retired" || str(evs[2], "actor_ref") != admin.actor.String() {
		t.Fatalf("Event Log kho: %s", r.raw)
	}
	expect(t, admin.adminJSON("/v1/geo/facilities/"+ref+"/restore", map[string]any{"reason": "Ký lại hợp đồng thuê kho"}), http.StatusOK, "khôi phục kho")
	if n := outboxCount(t, g.fixture, port.EventFacilityRecorded, "facility_ref", ref); n != 4 {
		t.Fatalf("số sự kiện kho = %d, muốn 4", n)
	}
	verifyChain(t, g.fixture, "pf_geo.facility_event", "facility:"+ref)
	mustFailUpdate(t, g.fixture, "UPDATE pf_geo.facility_event SET capacity = 1 WHERE facility_ref = $1", uuid.MustParse(ref))
	mustFailUpdate(t, g.fixture, "DELETE FROM pf_geo.facility WHERE ref = $1", uuid.MustParse(ref))
	// Admin API bắt buộc vai geo_admin.
	expect(t, g.officerA().adminJSON("/v1/geo/facilities", base), http.StatusForbidden, "không vai geo_admin")
}

// GIS US-BND-08: nhập hàng loạt vị trí kho/Hub (GeoJSON điểm, KML Point).
func TestFacilityImport_GIS_US_BND_08(t *testing.T) {
	g := newG2(t)
	admin := g.geoAdmin92()
	pt := point(100, 100)
	data, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": []any{
		map[string]any{"type": "Feature", "geometry": pt, "properties": map[string]any{"ten": "Hub A", "suc_chua": "800", "ma": "HUB-A"}},
		map[string]any{"type": "Feature", "geometry": pt, "properties": map[string]any{"ten": "Hub B", "suc_chua": "0"}},
		map[string]any{"type": "Feature", "geometry": pt, "properties": map[string]any{"ten": "", "suc_chua": "100"}},
	}})
	r := admin.adminPost("/v1/geo/facilities/import?province_code=92&commune_code=31117", "application/geo+json", data)
	expect(t, r, http.StatusOK, "nhập GeoJSON")
	fs := list(r.body, "features")
	if num(r.body, "accepted") != 1 || num(r.body, "rejected") != 2 || !strings.Contains(str(fs[1], "reason"), domain.MsgCapacityPositive) ||
		str(fs[0], "facility_code") != "HUB-A" {
		t.Fatalf("kết quả nhập: %s", r.raw)
	}
	kml := `<?xml version="1.0"?><kml xmlns="http://www.opengis.net/kml/2.2"><Document><Placemark><name>Kho KML</name>
	<ExtendedData><Data name="ma_xa"><value>31120</value></Data><Data name="suc_chua"><value>250</value></Data>
	<Data name="loai"><value>kho</value></Data></ExtendedData><Point><coordinates>105.61,10.06,0</coordinates></Point></Placemark></Document></kml>`
	r = admin.adminPost("/v1/geo/facilities/import?province_code=92&file_name=kho.kml", "application/vnd.google-earth.kml+xml", []byte(kml))
	if r.status != http.StatusOK || num(r.body, "accepted") != 1 {
		t.Fatalf("nhập KML: %d %s", r.status, r.raw)
	}
	items := list(g.national().appDo(http.MethodGet, g2BBox("/v1/geo/facilities?kind=kho"), nil).body, "features")
	if len(items) != 1 || str(items[0], "properties", "name") != "Kho KML" || str(items[0], "properties", "unit") != "tan" {
		t.Fatalf("kho từ KML: %v", items)
	}
}

// ---------------------------------------------------------------------------
// Sức chứa, hub MKG qua van, widget
// ---------------------------------------------------------------------------

func hubCtx(f *fixture) context.Context { return tenant.WithID(context.Background(), f.tenant) }

// GIS US-WH-01 + BRD BR-52: số Admin nhập tay không bị lần đồng bộ sau ghi đè —
// hệ thống báo sync_differs cho tới khi Admin xác nhận (release) áp dụng số đồng bộ.
// HubSink idempotent theo (hub_ref, version) và (hub_ref, as_of).
func TestCapacityManualNotOverwritten_BR52_GIS_US_WH_01(t *testing.T) {
	g := newG2(t)
	sink := g.mod.HubSink()
	ctx := hubCtx(g.fixture)
	hub := ids.New()
	dLat, dLon := degPerMetre(baseLat)
	h := port.HubFacility{HubRef: hub, Version: 1, Code: "MKG-HUB-01", Name: "Hub MKG Cờ Đỏ", Lon: decimal.NewFromFloat(baseLon + 500*dLon),
		Lat: decimal.NewFromFloat(baseLat + 500*dLat), ProvinceCode: "92", PublishedAt: time.Now().UTC()}
	for i, tc := range []struct {
		version int64
		name    string
		want    bool
	}{{1, "Hub MKG Cờ Đỏ", true}, {1, "Hub MKG Cờ Đỏ", false}, {3, "Hub MKG Cờ Đỏ (mở rộng)", true}, {2, "Hub cũ", false}} {
		h.Version, h.Name = tc.version, tc.name
		applied, err := sink.UpsertHubFacility(ctx, h)
		if err != nil || applied != tc.want {
			t.Fatalf("upsert %d (v%d): applied=%v err=%v", i, tc.version, applied, err)
		}
	}
	t1 := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	c := port.HubCapacity{HubRef: hub, AsOf: t1, CapacityTons: decimal.NewFromInt(100), UsedTons: decimal.NewFromInt(50)}
	if ok, err := sink.AppendHubCapacity(ctx, c); err != nil || !ok {
		t.Fatalf("van lần 1: %v %v", ok, err)
	}
	if ok, err := sink.AppendHubCapacity(ctx, c); err != nil || ok {
		t.Fatalf("van lặp cùng as_of phải bỏ qua: %v %v", ok, err)
	}
	viewer := g.as(g.tenant, g.orgA, rolePlotViewer)
	widget := func() map[string]any {
		t.Helper()
		r := viewer.appDo(http.MethodGet, g2BBox("/v1/geo/capacity-widgets"), nil)
		expect(t, r, http.StatusOK, "widget")
		for _, it := range list(r.body, "items") {
			if str(it, "ref") == hub.String() {
				return it.(map[string]any)
			}
		}
		t.Fatalf("không thấy hub: %s", r.raw)
		return nil
	}
	if w := widget(); str(w, "origin") != "mkg" || str(w, "name") != "Hub MKG Cờ Đỏ (mở rộng)" || str(w, "fill_pct") != "50.0" ||
		str(w, "class") != domain.CapacityAvailable || str(w, "source") != "valve" {
		t.Fatalf("widget van: %v", w)
	}
	// Admin nhập tay khi tích hợp chưa đúng (BR-52).
	admin := g.geoAdmin92()
	capPath := "/v1/geo/hubs/" + hub.String() + "/capacity"
	r := admin.adminJSON(capPath, map[string]any{"capacity": "100", "used": "90", "as_of": time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)})
	expect(t, r, http.StatusCreated, "nhập tay hub")
	if str(r.body, "capacity", "source") != "manual" || str(r.body, "capacity", "class") != domain.CapacityFull {
		t.Fatalf("sau nhập tay: %s", r.raw)
	}
	expect(t, admin.adminJSON(capPath, map[string]any{"capacity": 0, "used": 1}), http.StatusUnprocessableEntity, "sức chứa 0")
	// Van có số mới hơn → KHÔNG ghi đè; cảnh báo sync_differs cho Admin.
	if ok, err := sink.AppendHubCapacity(ctx, port.HubCapacity{HubRef: hub, AsOf: time.Now().UTC().Add(-time.Hour), CapacityTons: decimal.NewFromInt(100), UsedTons: decimal.NewFromInt(30)}); err != nil || !ok {
		t.Fatalf("van lần 2: %v %v", ok, err)
	}
	if w := widget(); str(w, "source") != "manual" || str(w, "fill_pct") != "90.0" || w["sync_differs"] != true {
		t.Fatalf("số nhập tay bị ghi đè: %v", w)
	}
	r = viewer.appDo(http.MethodGet, capPath, nil)
	if str(r.body, "capacity", "pending_sync", "used") != "30.000" || len(list(r.body, "capacity_history")) != 3 {
		t.Fatalf("chi tiết hub: %s", r.raw)
	}
	// Admin xác nhận áp dụng số đồng bộ.
	expect(t, admin.adminJSON(capPath+"/release", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity, "lý do ngắn")
	r = admin.adminJSON(capPath+"/release", map[string]any{"reason": "Đã đối chiếu với ERP, áp dụng số đồng bộ"})
	expect(t, r, http.StatusOK, "nhả số nhập tay")
	if str(r.body, "capacity", "source") != "valve" || str(r.body, "capacity", "fill_pct") != "30.0" || get(r.body, "capacity", "sync_differs") != false {
		t.Fatalf("sau nhả: %s", r.raw)
	}
	r = admin.adminJSON(capPath+"/release", map[string]any{"reason": "Nhả lần hai không còn gì"})
	if r.status != http.StatusConflict || code(r) != "geo.capacity_nothing_to_release" {
		t.Fatalf("nhả lần 2: %d %s", r.status, r.raw)
	}
	// Hub chưa nhận qua van → 404.
	expect(t, admin.adminJSON("/v1/geo/hubs/"+ids.New().String()+"/capacity", map[string]any{"capacity": 10, "used": 1}), http.StatusNotFound, "hub lạ")

	// Kho của tenant: nhập tay, sửa bằng bản ghi bù, nhập tệp theo mã.
	r = admin.adminJSON("/v1/geo/facilities", map[string]any{"kind": "kho", "code": "KHO-X", "name": "Kho X",
		"point": point(50, 50), "province_code": "92", "commune_code": "31117", "capacity": 400})
	expect(t, r, http.StatusCreated, "tạo kho")
	fref := str(r.body, "facility", "id")
	fPath := "/v1/geo/facilities/" + fref + "/capacity"
	r = admin.adminJSON(fPath, map[string]any{"capacity": 400, "used": 100})
	expect(t, r, http.StatusCreated, "nhập tay kho")
	snap := str(r.body, "snapshot", "id")
	r = admin.adminJSON(fPath, map[string]any{"capacity": 400, "used": 120, "supersedes_id": snap})
	expect(t, r, http.StatusUnprocessableEntity, "sửa không lý do")
	r = admin.adminJSON(fPath, map[string]any{"capacity": 400, "used": 120, "supersedes_id": snap, "correction_reason": "Nhập nhầm số tồn đầu ngày"})
	if r.status != http.StatusCreated || str(r.body, "snapshot", "supersedes_id") != snap || str(r.body, "capacity", "fill_pct") != "30.0" {
		t.Fatalf("sửa ảnh chụp: %d %s", r.status, r.raw)
	}
	r = admin.adminJSON("/v1/geo/capacity/import", map[string]any{"items": []any{
		map[string]any{"facility_code": "KHO-X", "capacity": 400, "used": 380},
		map[string]any{"facility_code": "KHONGCO", "capacity": 10, "used": 1},
		map[string]any{"hub_ref": hub.String(), "capacity": "abc", "used": 1},
	}})
	if r.status != http.StatusOK || num(r.body, "accepted") != 1 || num(r.body, "rejected") != 2 {
		t.Fatalf("nhập tệp sức chứa: %d %s", r.status, r.raw)
	}
	r = viewer.appDo(http.MethodGet, "/v1/geo/facilities/"+fref, nil)
	if str(r.body, "capacity", "source") != "import" || str(r.body, "capacity", "class") != domain.CapacityFull {
		t.Fatalf("kho sau nhập tệp: %s", r.raw)
	}
	verifyChain(t, g.fixture, "pf_geo.facility_capacity", "capacity:hub:"+hub.String())
	mustFailUpdate(t, g.fixture, "UPDATE pf_geo.facility_capacity SET used = 0 WHERE subject_ref = $1", hub)
	mustFailUpdate(t, g.fixture, "DELETE FROM pf_geo.hub_view WHERE hub_ref = $1", hub)
}

// GIS US-HUB-01 (BR-50): % và lớp màu theo ngưỡng tham số FN-03
// (geo.hub_capacity_warn_pct / geo.hub_capacity_full_pct; mặc định 60/85).
func TestHubCapacityWidgetThresholds_GIS_US_HUB_01(t *testing.T) {
	g := newG2(t)
	admin := g.geoAdmin92()
	mk := func(code string, used int64) {
		r := admin.adminJSON("/v1/geo/facilities", map[string]any{"kind": "hub", "code": code, "name": "Hub " + code,
			"point": point(float64(len(code))*10, 0), "province_code": "92", "commune_code": "31117", "capacity": 100})
		expect(t, r, http.StatusCreated, "tạo hub "+code)
		expect(t, admin.adminJSON("/v1/geo/facilities/"+str(r.body, "facility", "id")+"/capacity", map[string]any{"capacity": 100, "used": used}),
			http.StatusCreated, "sức chứa "+code)
	}
	mk("H59", 59)
	mk("H60X", 60)
	mk("H85XX", 85)
	mk("H75XXX", 75)
	classes := func() map[string]string {
		r := g.national().appDo(http.MethodGet, g2BBox("/v1/geo/capacity-widgets"), nil)
		out := map[string]string{}
		for _, it := range list(r.body, "items") {
			out[str(it, "code")] = str(it, "class")
		}
		return out
	}
	want := map[string]string{"H59": "available", "H60X": "near_full", "H85XX": "full", "H75XXX": "near_full"}
	if got := classes(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("mặc định 60/85: %v", got)
	}
	g.params.set(port.ParamHubCapacityWarnPct, decimal.NewFromInt(50))
	g.params.set(port.ParamHubCapacityFullPct, decimal.NewFromInt(70))
	want = map[string]string{"H59": "near_full", "H60X": "near_full", "H85XX": "full", "H75XXX": "full"}
	if got := classes(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ngưỡng 50/70: %v", got)
	}
	// Cặp ngưỡng không tăng dần → quay về mặc định.
	g.params.set(port.ParamHubCapacityWarnPct, decimal.NewFromInt(90))
	if got := classes(); got["H75XXX"] != "near_full" {
		t.Fatalf("ngưỡng sai phải về mặc định: %v", got)
	}
}

// ---------------------------------------------------------------------------
// Đường bộ
// ---------------------------------------------------------------------------

// GIS US-ROAD-01 AC-1/AC-2, US-ROAD-02/03 AC-1/AC-2 (BR-29/30): tuyến có phân loại và
// tải trọng; chiều dài trắc địa tự tính; lọc tải trọng ≥ tải trọng nhập; sửa là
// phiên bản mới; thông báo lỗi đúng câu chữ.
func TestRoadLedger_GIS_US_ROAD_01_02_03(t *testing.T) {
	g := newG2(t)
	admin := g.geoAdmin92()
	ln := line([2]float64{0, 0}, [2]float64{1000, 0})
	for _, tc := range []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"thiếu tên", map[string]any{"road_class": "tinh_lo", "max_load_tons": 30, "geometry": ln, "province_code": "92"}, domain.MsgRoadRequired},
		{"thiếu phân loại", map[string]any{"name": "ĐT 922", "max_load_tons": 30, "geometry": ln, "province_code": "92"}, domain.MsgRoadRequired},
		{"tải trọng 0", map[string]any{"name": "ĐT 922", "road_class": "tinh_lo", "max_load_tons": 0, "geometry": ln, "province_code": "92"}, domain.MsgRoadLoadPositive},
		{"tải trọng sai định dạng", map[string]any{"name": "ĐT 922", "road_class": "tinh_lo", "max_load_tons": "ba mươi", "geometry": ln, "province_code": "92"}, domain.MsgRoadLoadPositive},
	} {
		r := admin.adminJSON("/v1/geo/roads", tc.body)
		if r.status != http.StatusUnprocessableEntity || str(r.body, "detail") != tc.msg {
			t.Fatalf("%s: %d %s", tc.name, r.status, r.raw)
		}
	}
	expect(t, g.geoAdmin89().adminJSON("/v1/geo/roads", map[string]any{"name": "ĐT 922", "road_class": "tinh_lo", "max_load_tons": 30,
		"geometry": ln, "province_code": "92"}), http.StatusForbidden, "admin tỉnh khác")
	r := admin.adminJSON("/v1/geo/roads", map[string]any{"name": "ĐT 922", "road_class": "tinh_lo", "max_load_tons": 30, "geometry": ln, "province_code": "92"})
	expect(t, r, http.StatusCreated, "vẽ tuyến tỉnh lộ")
	road := str(r.body, "road", "id")
	seg1 := str(r.body, "segment", "segment_id")
	if l := dec(t, r.body, "road", "properties", "length_m"); !near(l, decimal.NewFromInt(1000), "1") {
		t.Fatalf("chiều dài trắc địa = %s, muốn 1000 m", l)
	}
	r = admin.adminJSON("/v1/geo/roads", map[string]any{"name": "Bờ ruộng ấp 3", "road_class": "bo_ruong", "max_load_tons": "2.5",
		"geometry": line([2]float64{0, 100}, [2]float64{0, 300}), "province_code": "92"})
	expect(t, r, http.StatusCreated, "vẽ bờ ruộng")

	viewer := g.as(g.tenant, g.orgA, rolePlotViewer)
	r = viewer.appDo(http.MethodGet, g2BBox("/v1/geo/roads?min_load_tons=10"), nil)
	if ids := featureRefs(r); len(ids) != 1 || ids[0] != road || r.body["none_matched"] != false {
		t.Fatalf("lọc ≥ 10 t: %s", r.raw)
	}
	if len(featureRefs(viewer.appDo(http.MethodGet, g2BBox("/v1/geo/roads?min_load_tons=2.5"), nil))) != 2 {
		t.Fatal("lọc ≥ 2,5 t phải gồm cả bờ ruộng (tải trọng cho phép = tải trọng nhập)")
	}
	// AC-2: không tuyến nào đủ tải trọng → không đánh dấu, có thông báo.
	r = viewer.appDo(http.MethodGet, g2BBox("/v1/geo/roads?min_load_tons=40"), nil)
	if len(featureRefs(r)) != 0 || r.body["none_matched"] != true || str(r.body, "notice") != domain.MsgRoadNoneMatched {
		t.Fatalf("không tuyến phù hợp: %s", r.raw)
	}
	// US-ROAD-03: kéo/thêm điểm → phiên bản mới, chiều dài tính lại.
	vpath := "/v1/geo/roads/" + road + "/versions"
	r = admin.adminJSON(vpath, map[string]any{"supersedes_id": seg1, "correction_reason": "Nối dài tuyến theo thực địa",
		"geometry": line([2]float64{0, 0}, [2]float64{1000, 0}, [2]float64{1000, 500})})
	expect(t, r, http.StatusCreated, "sửa tuyến")
	if l := dec(t, r.body, "road", "properties", "length_m"); !near(l, decimal.NewFromInt(1500), "1.5") || num(r.body, "road", "properties", "version") != 2 {
		t.Fatalf("sau sửa: %s", r.raw)
	}
	r = admin.adminJSON(vpath, map[string]any{"supersedes_id": seg1, "correction_reason": "Sửa trên phiên bản cũ", "max_load_tons": 10})
	if r.status != http.StatusConflict || code(r) != "geo.version_conflict" {
		t.Fatalf("sửa trên bản cũ: %d %s", r.status, r.raw)
	}
	r = admin.adminJSON(vpath, map[string]any{"supersedes_id": str(r.body, "current_segment_id"), "correction_reason": "Hạ tải trọng do cầu yếu", "max_load_tons": -1})
	if r.status != http.StatusUnprocessableEntity || str(r.body, "detail") != domain.MsgRoadLoadPositive {
		t.Fatalf("sửa tải trọng âm: %d %s", r.status, r.raw)
	}
	expect(t, admin.adminJSON("/v1/geo/roads/"+road+"/retire", map[string]any{"reason": "Tuyến bị sạt lở, đóng"}), http.StatusOK, "ngừng dùng tuyến")
	if has(featureRefs(viewer.appDo(http.MethodGet, g2BBox("/v1/geo/roads"), nil)), road) {
		t.Fatal("tuyến đã ngừng dùng vẫn hiện")
	}
	r = viewer.appDo(http.MethodGet, "/v1/geo/roads/"+road, nil)
	if vs := list(r.body, "versions"); len(vs) != 3 || str(vs[2], "action") != "retired" || get(vs[0], "geometry") == nil {
		t.Fatalf("lịch sử tuyến: %s", r.raw)
	}
	verifyChain(t, g.fixture, "pf_geo.road_segment", "road:"+road)
	mustFailUpdate(t, g.fixture, "UPDATE pf_geo.road_segment SET max_load_tons = 99 WHERE road_ref = $1", uuid.MustParse(road))
}

// ---------------------------------------------------------------------------
// Log GPS
// ---------------------------------------------------------------------------

// HTX US-GPS-03 AC-1/AC-2/AC-3: log GPS ghi qua port (giao dịch riêng của geo,
// gọi lặp an toàn), chỉ đọc (sửa/xóa bị từ chối kèm thông báo, kể cả Admin), đọc
// theo phạm vi + cursor; khoảng cách tới lô tính trắc địa (HTX US-GPS-01).
func TestGpsFixLog_HTX_US_GPS_03(t *testing.T) {
	g := newG2(t)
	// Thửa 100 m × 100 m ở góc gốc (pf/cooperative giả): khoảng cách theo trục đông khi điểm ở giữa theo trục bắc.
	plot := ids.New()
	g.plots.put(plot, uuid.Nil, port.PlotPlace{CommuneCode: "31117", CooperativeRef: g.coopA})
	g.plots.dist[plot] = func(lon, _ decimal.Decimal) decimal.Decimal {
		_, dLon := degPerMetre(baseLat)
		e := (lon.InexactFloat64() - baseLon) / dLon
		return decimal.NewFromFloat(math.Max(0, e-100))
	}
	log := g.mod.GpsLog()
	ctx := hubCtx(g.fixture)
	farmer := ids.New()
	lonlat := func(e, n float64) (*decimal.Decimal, *decimal.Decimal) {
		dLat, dLon := degPerMetre(baseLat)
		lo, la := decimal.NewFromFloat(baseLon+e*dLon), decimal.NewFromFloat(baseLat+n*dLat)
		return &lo, &la
	}
	acc := decimal.RequireFromString("4.5")
	in := func(e, n float64) port.GpsFix {
		lo, la := lonlat(e, n)
		return port.GpsFix{ID: ids.New(), ActorRef: farmer, DeviceID: "dev-1", Purpose: port.GpsPurposeFarmLog, TargetRef: ids.New(),
			PlotRef: plot, Lon: lo, Lat: la, AccuracyM: &acc, Source: port.GpsSourceDevice, CapturedAt: time.Now().Add(-time.Minute)}
	}
	inside := in(50, 50)
	r1, err := log.RecordFix(ctx, inside)
	if err != nil || r1.Duplicate || r1.DistanceToPlotM == nil || !r1.DistanceToPlotM.IsZero() {
		t.Fatalf("điểm trong lô: %+v %v", r1, err)
	}
	// AC-3: gửi lại cùng ID (sau mất kết nối) → không trùng.
	r2, err := log.RecordFix(ctx, inside)
	if err != nil || !r2.Duplicate || r2.Seq != r1.Seq || r2.ID != r1.ID {
		t.Fatalf("gửi lại: %+v %v", r2, err)
	}
	far, err := log.RecordFix(ctx, in(200, 50))
	if err != nil || far.DistanceToPlotM == nil || !near(*far.DistanceToPlotM, decimal.NewFromInt(100), "0.5") {
		t.Fatalf("điểm cách lô 100 m: %+v %v", far, err)
	}
	// HTX US-GPS-02: chọn lô thủ công, không có toạ độ.
	if _, err := log.RecordFix(ctx, port.GpsFix{ID: ids.New(), ActorRef: farmer, Purpose: port.GpsPurposeHarvest, PlotRef: plot, Source: port.GpsSourceManualPick}); err != nil {
		t.Fatalf("chọn lô thủ công: %v", err)
	}
	if _, err := log.RecordFix(ctx, port.GpsFix{ID: ids.New(), ActorRef: farmer, Purpose: port.GpsPurposeHarvest, Source: port.GpsSourceDevice}); !errors.Is(err, port.ErrGpsFixInvalid) {
		t.Fatalf("thiếu toạ độ phải ErrGpsFixInvalid: %v", err)
	}
	// Log không gắn lô, không xác định được xã → chỉ toàn quốc thấy.
	lo, la := lonlat(9000, 9000)
	if _, err := log.RecordFix(ctx, port.GpsFix{IdemKey: "support-1", ActorRef: farmer, Purpose: port.GpsPurposeSupport, Lon: lo, Lat: la, Source: port.GpsSourcePhotoEXIF}); err != nil {
		t.Fatal(err)
	}

	count := func(c caller, q string) int {
		t.Helper()
		r := c.appDo(http.MethodGet, "/v1/geo/gps-fixes"+q, nil)
		expect(t, r, http.StatusOK, "đọc log")
		return len(list(r.body, "items"))
	}
	if n := count(g.national(), ""); n != 4 {
		t.Fatalf("toàn quốc thấy %d, muốn 4", n)
	}
	if n := count(g.officerA(), ""); n != 3 {
		t.Fatalf("xã 31117 thấy %d, muốn 3", n)
	}
	if n := count(g.officerB(), ""); n != 0 {
		t.Fatalf("xã 31120 thấy %d", n)
	}
	if n := count(g.as(g.tenant, g.coopB, rolePlotEditor, port.RoleCoopManager), ""); n != 0 {
		t.Fatalf("HTX B thấy log của HTX A: %d", n)
	}
	if n := count(g.managerA(), "?purpose=harvest"); n != 1 {
		t.Fatalf("lọc mục đích: %d", n)
	}
	if n := count(g.as(ids.New(), uuid.Nil, authz.RolePlatformAdmin), ""); n != 0 {
		t.Fatalf("tenant khác thấy %d", n)
	}
	// Cursor: 2 + 1 (mới nhất trước).
	r := g.officerA().appDo(http.MethodGet, "/v1/geo/gps-fixes?limit=2", nil)
	next := str(r.body, "next_cursor")
	items := list(r.body, "items")
	if len(items) != 2 || next == "" || str(items[0], "id") <= str(items[1], "id") {
		t.Fatalf("trang 1: %s", r.raw)
	}
	r = g.officerA().appDo(http.MethodGet, "/v1/geo/gps-fixes?limit=2&cursor="+next, nil)
	if len(list(r.body, "items")) != 1 || get(r.body, "next_cursor") != nil {
		t.Fatalf("trang 2: %s", r.raw)
	}
	// AC-1: sửa/xóa bị từ chối, kể cả Admin.
	for _, m := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		r := g.national().do(g.app.URL, m, "/v1/geo/gps-fixes/"+r1.ID.String(), "", "application/json", []byte(`{}`))
		if r.status != http.StatusMethodNotAllowed || code(r) != "geo.gps_log_read_only" || str(r.body, "detail") != domain.MsgGpsReadOnly {
			t.Fatalf("%s log: %d %s", m, r.status, r.raw)
		}
	}
	verifyChain(t, g.fixture, "pf_geo.gps_fix", "gps:"+farmer.String())
	mustFailUpdate(t, g.fixture, "UPDATE pf_geo.gps_fix SET accuracy_m = 0 WHERE actor_ref = $1", farmer)
	mustFailUpdate(t, g.fixture, "DELETE FROM pf_geo.gps_fix WHERE actor_ref = $1", farmer)
}

// ---------------------------------------------------------------------------
// Lớp gộp
// ---------------------------------------------------------------------------

// GIS BR-24/25 (FN-01): lớp gộp theo khung nhìn; polygon thửa chỉ khi zoom ≥
// geo.plot_layer_min_zoom (mặc định 13).
func TestLayersZoomGate_GIS_BR24(t *testing.T) {
	g := newG2(t)
	// Lớp thửa, lớp HTX do pf/cooperative dựng (geoport.MapLayers giả, W3.3) — pf/geo chỉ ghép và chặn theo zoom.
	g.layers.plots = append(g.layers.plots, feature(ids.New(), "plots"))
	g.layers.coops = append(g.layers.coops, feature(g.coopA, "cooperatives"))
	c := g.national()
	r := c.appDo(http.MethodGet, g2BBox("/v1/geo/layers?zoom=12"), nil)
	expect(t, r, http.StatusOK, "lớp zoom 12")
	if r.body["plots"] != nil || str(r.body, "plots_notice") == "" || len(list(r.body, "cooperatives", "features")) != 1 ||
		get(r.body, "facilities") == nil || get(r.body, "roads") == nil {
		t.Fatalf("zoom 12: %s", r.raw)
	}
	r = c.appDo(http.MethodGet, g2BBox("/v1/geo/layers?zoom=13&layers=plots"), nil)
	if len(list(r.body, "plots", "features")) != 1 || r.body["cooperatives"] != nil {
		t.Fatalf("zoom 13: %s", r.raw)
	}
	g.params.set(port.ParamPlotLayerMinZoom, decimal.NewFromInt(15))
	if r = c.appDo(http.MethodGet, g2BBox("/v1/geo/layers?zoom=14&layers=plots"), nil); r.body["plots"] != nil {
		t.Fatalf("ngưỡng 15, zoom 14: %s", r.raw)
	}
	expect(t, c.appDo(http.MethodGet, g2BBox("/v1/geo/layers?layers=weather"), nil), http.StatusUnprocessableEntity, "lớp lạ")
}
