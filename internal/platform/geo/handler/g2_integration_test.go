package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/shptest"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ---------------------------------------------------------------------------
// Giả lập pf/party cho G2: sổ HTX (Orgs + OrgCodes) và marker vị trí HTX.
// ---------------------------------------------------------------------------

type coopRegistry struct {
	mu   sync.Mutex
	orgs map[uuid.UUID]partyport.Org
}

func (f *coopRegistry) add(ref uuid.UUID, code, unit string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orgs[ref] = partyport.Org{Ref: ref, Kind: partyport.OrgCooperative, Code: code, Name: "HTX " + code, AdminUnitCode: unit, Active: true}
}

func (f *coopRegistry) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orgs[ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return o, nil
}

func (f *coopRegistry) ByCode(_ context.Context, code string) (partyport.Org, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.orgs {
		if o.Code == code {
			return o, nil
		}
	}
	return partyport.Org{}, partyport.ErrOrgNotFound
}

type fakeMarkers struct {
	mu sync.Mutex
	ms []partyport.CoopMarker
}

func (f *fakeMarkers) MarkersInBBox(_ context.Context, minLon, minLat, maxLon, maxLat float64, _ int) ([]partyport.CoopMarker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []partyport.CoopMarker
	for _, m := range f.ms {
		lon, lat := m.Lon.InexactFloat64(), m.Lat.InexactFloat64()
		if lon >= minLon && lon <= maxLon && lat >= minLat && lat <= maxLat {
			out = append(out, m)
		}
	}
	return out, nil
}

type g2 struct {
	*fixture
	orgs    *coopRegistry
	markers *fakeMarkers
	params  *paramValues
	coopC   uuid.UUID // HTX ở xã 31120
	prov92  uuid.UUID // tổ chức cấp tỉnh Cần Thơ (92)
	prov89  uuid.UUID // tổ chức cấp tỉnh An Giang (89)
}

func newG2(t *testing.T) *g2 {
	orgs := &coopRegistry{orgs: map[uuid.UUID]partyport.Org{}}
	markers := &fakeMarkers{}
	params := &paramValues{m: map[string]decimal.Decimal{}}
	tol := decimal.NewFromInt(1)
	f := newFixture(t, func(d *geo.Deps) {
		d.Orgs, d.CoopMarkers, d.Params = orgs, markers, fakeParams{tolerance: &tol, values: params}
	})
	g := &g2{fixture: f, orgs: orgs, markers: markers, params: params, coopC: ids.New(), prov92: ids.New(), prov89: ids.New()}
	orgs.add(f.coopA, "HTXA", "31117")
	orgs.add(f.coopB, "HTXB", "31117")
	orgs.add(g.coopC, "HTXC", "31120")
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

func (c caller) createCoop(coop uuid.UUID, geom map[string]any) resp {
	c.f.t.Helper()
	r := c.appDo(http.MethodPost, "/v1/geo/cooperative-boundaries", map[string]any{"cooperative_ref": coop.String(), "geometry": geom})
	expect(c.f.t, r, http.StatusCreated, "tạo ranh giới HTX")
	return r
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

// GIS US-BND-02 AC-1/AC-2, US-BND-03 AC-1/AC-2 (BRD FN-04 BR-13/14): ranh giới HTX
// là sổ phiên bản — diện tích trắc địa tính trong DB, sửa bằng bản ghi bù có lý
// do, xóa mềm ẩn khỏi bản đồ nhưng giữ lịch sử và người xóa; chuỗi băm hợp lệ.
func TestCoopBoundaryLedger_GIS_US_BND_02_03(t *testing.T) {
	g := newG2(t)
	c := g.officerA()

	// AC-2: thiếu HTX → chặn lưu kèm thông báo.
	r := c.appDo(http.MethodPost, "/v1/geo/cooperative-boundaries", map[string]any{"geometry": rect(0, 0, 1000, 1000)})
	if r.status != http.StatusUnprocessableEntity || str(r.body, "detail") != domain.MsgCoopRequired {
		t.Fatalf("thiếu HTX: %d %s", r.status, r.raw)
	}
	r = c.createCoop(g.coopA, rect(0, 0, 1000, 1000))
	if a := dec(t, r.body, "cooperative", "properties", "area_ha"); !near(a, decimal.NewFromInt(100), "0.05") {
		t.Fatalf("diện tích trắc địa = %s, muốn 100 ha", a)
	}
	v1 := str(r.body, "boundary", "boundary_id")
	if str(r.body, "cooperative", "properties", "commune_code") != "31117" || str(r.body, "boundary", "source") != domain.CoopSourceGeoWeb {
		t.Fatalf("xã/nguồn sai: %s", r.raw)
	}
	// Đã có ranh giới → phải gửi phiên bản mới.
	r = c.appDo(http.MethodPost, "/v1/geo/cooperative-boundaries", map[string]any{"cooperative_ref": g.coopA.String(), "geometry": rect(0, 0, 900, 900)})
	if r.status != http.StatusConflict || code(r) != "geo.cooperative_boundary_exists" {
		t.Fatalf("tạo lần 2: %d %s", r.status, r.raw)
	}
	path := "/v1/geo/cooperative-boundaries/" + g.coopA.String()
	r = c.appDo(http.MethodPost, path+"/versions", map[string]any{"supersedes_id": v1, "correction_reason": "ngắn", "geometry": rect(0, 0, 1000, 1200)})
	expect(t, r, http.StatusUnprocessableEntity, "lý do ngắn")
	r = c.appDo(http.MethodPost, path+"/versions", map[string]any{"supersedes_id": v1, "correction_reason": "Đo lại ranh giới theo bản đồ địa chính", "geometry": rect(0, 0, 1000, 1200)})
	expect(t, r, http.StatusCreated, "sửa ranh giới HTX")
	v2 := str(r.body, "boundary", "boundary_id")
	if num(r.body, "boundary", "version") != 2 || str(r.body, "boundary", "supersedes_id") != v1 ||
		!near(dec(t, r.body, "cooperative", "properties", "area_ha"), decimal.NewFromInt(120), "0.06") {
		t.Fatalf("phiên bản 2 sai: %s", r.raw)
	}
	r = c.appDo(http.MethodPost, path+"/versions", map[string]any{"supersedes_id": v1, "correction_reason": "Sửa dựa trên bản cũ đã bị thay", "geometry": rect(0, 0, 500, 500)})
	if r.status != http.StatusConflict || code(r) != "geo.version_conflict" || str(r.body, "current_boundary_id") != v2 {
		t.Fatalf("sửa trên bản cũ: %d %s", r.status, r.raw)
	}

	// AC-2 (US-BND-03): xóa mềm — ẩn khỏi bản đồ dùng chung, lịch sử + người xóa còn đủ.
	r = c.appDo(http.MethodPost, path+"/retire", map[string]any{"reason": "HTX giải thể theo quyết định của xã"})
	expect(t, r, http.StatusOK, "ngừng dùng")
	if str(r.body, "cooperative", "properties", "status") != "retired" || str(r.body, "boundary", "actor_ref") != c.actor.String() {
		t.Fatalf("ngừng dùng: %s", r.raw)
	}
	if has(featureRefs(c.appDo(http.MethodGet, g2BBox("/v1/geo/cooperative-boundaries"), nil)), g.coopA.String()) {
		t.Fatal("ranh giới đã ngừng dùng vẫn hiện trên bản đồ")
	}
	r = c.appDo(http.MethodGet, path, nil)
	expect(t, r, http.StatusOK, "đọc lịch sử")
	vs := list(r.body, "versions")
	if len(vs) != 3 || str(vs[2], "action") != "retired" || str(vs[2], "correction_reason") == "" || get(vs[0], "geometry") == nil {
		t.Fatalf("lịch sử = %s", r.raw)
	}
	r = c.appDo(http.MethodPost, path+"/versions", map[string]any{"supersedes_id": str(vs[2], "boundary_id"), "correction_reason": "Sửa khi đã ngừng dùng thì phải chặn", "geometry": rect(0, 0, 500, 500)})
	if r.status != http.StatusConflict || code(r) != "geo.feature_state_conflict" {
		t.Fatalf("sửa khi đã ngừng dùng: %d %s", r.status, r.raw)
	}
	r = c.appDo(http.MethodPost, path+"/restore", map[string]any{"reason": "Khôi phục do xóa nhầm HTX"})
	expect(t, r, http.StatusOK, "khôi phục")
	if !has(featureRefs(c.appDo(http.MethodGet, g2BBox("/v1/geo/cooperative-boundaries"), nil)), g.coopA.String()) {
		t.Fatal("ranh giới khôi phục không hiện lại")
	}
	if n := outboxCount(t, g.fixture, port.EventCooperativeBoundaryRecorded, "cooperative_ref", g.coopA.String()); n != 4 {
		t.Fatalf("số sự kiện %s = %d, muốn 4", port.EventCooperativeBoundaryRecorded, n)
	}
	verifyChain(t, g.fixture, "pf_geo.cooperative_boundary", "cooperative:"+g.coopA.String())
	mustFailUpdate(t, g.fixture, "UPDATE pf_geo.cooperative_boundary SET area_ha = 1 WHERE cooperative_ref = $1", g.coopA)
	mustFailUpdate(t, g.fixture, "DELETE FROM pf_geo.cooperative_area WHERE cooperative_ref = $1", g.coopA)
}

// Phạm vi cây phân cấp (ADR-0009) + quản lý HTX chỉ HTX mình (GIS US-BND-02).
func TestCoopBoundaryScope_GIS_US_BND_02(t *testing.T) {
	g := newG2(t)
	// Cán bộ xã 31120 không vẽ được cho HTX ở xã 31117.
	r := g.officerB().appDo(http.MethodPost, "/v1/geo/cooperative-boundaries", map[string]any{"cooperative_ref": g.coopA.String(), "geometry": rect(0, 0, 500, 500)})
	if r.status != http.StatusForbidden || code(r) != "geo.out_of_scope" {
		t.Fatalf("cán bộ xã khác: %d %s", r.status, r.raw)
	}
	// Quản lý HTX A không vẽ cho HTX B; vẽ cho HTX mình (mặc định HTX mình, nguồn htx_app).
	m := g.managerA()
	r = m.appDo(http.MethodPost, "/v1/geo/cooperative-boundaries", map[string]any{"cooperative_ref": g.coopB.String(), "geometry": rect(0, 0, 500, 500)})
	expect(t, r, http.StatusForbidden, "quản lý HTX vẽ cho HTX khác")
	r = m.appDo(http.MethodPost, "/v1/geo/cooperative-boundaries", map[string]any{"geometry": rect(0, 0, 500, 500)})
	expect(t, r, http.StatusCreated, "quản lý HTX vẽ cho HTX mình")
	if str(r.body, "boundary", "cooperative_ref") != g.coopA.String() || str(r.body, "boundary", "source") != domain.CoopSourceHTXApp {
		t.Fatalf("mặc định HTX mình/nguồn: %s", r.raw)
	}
	g.officerA().createCoop(g.coopB, rect(2000, 0, 500, 500))
	g.as(g.tenant, g.coopC, port.RolePlotEditor).createCoop(g.coopC, rect(4000, 0, 500, 500))

	// Tỉnh thấy cả ba; xã 31117 thấy A, B; quản lý HTX A chỉ thấy A; xã 31120 chỉ thấy C.
	for _, tc := range []struct {
		name string
		c    caller
		want []uuid.UUID
	}{
		{"tỉnh", g.as(g.tenant, g.prov92, port.RolePlotViewer), []uuid.UUID{g.coopA, g.coopB, g.coopC}},
		{"xã 31117", g.officerA(), []uuid.UUID{g.coopA, g.coopB}},
		{"quản lý HTX A", m, []uuid.UUID{g.coopA}},
		{"xã 31120", g.officerB(), []uuid.UUID{g.coopC}},
	} {
		got := featureRefs(tc.c.appDo(http.MethodGet, g2BBox("/v1/geo/cooperative-boundaries"), nil))
		if len(got) != len(tc.want) {
			t.Fatalf("%s thấy %v, muốn %v", tc.name, got, tc.want)
		}
		for _, w := range tc.want {
			if !has(got, w.String()) {
				t.Fatalf("%s không thấy %s", tc.name, w)
			}
		}
	}
	expect(t, m.appDo(http.MethodGet, "/v1/geo/cooperative-boundaries/"+g.coopB.String(), nil), http.StatusNotFound, "quản lý HTX đọc HTX khác")
	r = m.appDo(http.MethodPost, "/v1/geo/cooperative-boundaries/"+g.coopB.String()+"/retire", map[string]any{"reason": "Quản lý HTX khác không được xóa"})
	expect(t, r, http.StatusNotFound, "quản lý HTX xóa HTX khác")
	// Không có vai → 403.
	expect(t, g.as(g.tenant, g.orgA, "mrv_viewer").appDo(http.MethodGet, g2BBox("/v1/geo/cooperative-boundaries"), nil), http.StatusForbidden, "không vai")
}

// Cách ly tenant (RB-2) cho ranh giới HTX, kho/Hub, đường bộ.
func TestG2TenantIsolation(t *testing.T) {
	g := newG2(t)
	g.officerA().createCoop(g.coopA, rect(0, 0, 500, 500))
	admin := g.national()
	expect(t, admin.adminJSON("/v1/geo/facilities", map[string]any{"kind": "kho", "name": "Kho A",
		"point": point(100, 100), "province_code": "92", "commune_code": "31117", "capacity": "500"}), http.StatusCreated, "tạo kho")
	expect(t, admin.adminJSON("/v1/geo/roads", map[string]any{"name": "ĐT 922", "road_class": "tinh_lo", "max_load_tons": "30",
		"geometry": line([2]float64{0, 0}, [2]float64{1000, 0}), "province_code": "92"}), http.StatusCreated, "tạo đường")

	other := g.as(ids.New(), uuid.Nil, port.RolePlotViewer, port.RoleGeoAdmin, authz.RolePlatformAdmin)
	for _, p := range []string{"/v1/geo/cooperative-boundaries", "/v1/geo/facilities", "/v1/geo/roads"} {
		if n := len(featureRefs(other.appDo(http.MethodGet, g2BBox(p), nil))); n != 0 {
			t.Fatalf("tenant khác thấy %d đối tượng ở %s", n, p)
		}
	}
	expect(t, other.appDo(http.MethodGet, "/v1/geo/cooperative-boundaries/"+g.coopA.String(), nil), http.StatusNotFound, "tenant khác đọc HTX")
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu (ranh giới HTX, kho, đường).
func TestG2IdempotentReplay(t *testing.T) {
	g := newG2(t)
	for _, tc := range []struct {
		base, path string
		c          caller
		body       map[string]any
	}{
		{g.app.URL, "/v1/geo/cooperative-boundaries", g.officerA(), map[string]any{"cooperative_ref": g.coopA.String(), "geometry": rect(0, 0, 300, 300)}},
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

// GIS US-BND-07: nhập hàng loạt ranh giới HTX (GeoJSON, Shapefile) — HTX theo
// cooperative_ref hoặc Mã HTX; HTX đã có ranh giới → phiên bản mới; hệ toạ độ
// khác WGS84 → geo.crs_unsupported cho cả tệp.
func TestCoopBoundaryImport_GIS_US_BND_07(t *testing.T) {
	g := newG2(t)
	admin := g.as(g.tenant, g.prov92, port.RoleGeoAdmin)
	fc := func(feats ...map[string]any) []byte {
		b, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
		return b
	}
	feat := func(props map[string]any, geom map[string]any) map[string]any {
		return map[string]any{"type": "Feature", "properties": props, "geometry": geom}
	}
	data := fc(
		feat(map[string]any{"cooperative_ref": g.coopA.String(), "name": "HTX A"}, rect(0, 0, 500, 500)),
		feat(map[string]any{"ma_htx": "HTXB"}, rect(1000, 0, 500, 500)),
		feat(map[string]any{"name": "Không rõ HTX"}, rect(2000, 0, 500, 500)),
		feat(map[string]any{"cooperative_ref": g.coopA.String()}, map[string]any{"type": "Point", "coordinates": []float64{105.6, 10.05}}),
	)
	r := admin.adminPost("/v1/geo/cooperative-boundaries/import?format=geojson&file_name=htx.geojson", "application/geo+json", data)
	expect(t, r, http.StatusOK, "nhập GeoJSON")
	if num(r.body, "accepted") != 2 || num(r.body, "rejected") != 2 {
		t.Fatalf("kết quả nhập: %s", r.raw)
	}
	fs := list(r.body, "features")
	if str(fs[1], "cooperative_ref") != g.coopB.String() || str(fs[2], "code") != "validation_failed" || str(fs[3], "code") != "geo.invalid_geometry" {
		t.Fatalf("từng đối tượng: %s", r.raw)
	}
	// Nhập lại HTX A → phiên bản 2 thay phiên bản 1, lý do mặc định nêu tên tệp.
	r = admin.adminPost("/v1/geo/cooperative-boundaries/import?file_name=htx_v2.geojson", "application/geo+json",
		fc(feat(map[string]any{"cooperative_ref": g.coopA.String()}, rect(0, 0, 600, 600))))
	expect(t, r, http.StatusOK, "nhập lại")
	if f0 := list(r.body, "features")[0]; str(f0, "status") != "accepted" || get(f0, "replaced") != true || num(f0, "version") != 2 {
		t.Fatalf("nhập đè: %s", r.raw)
	}
	d := g.officerA().appDo(http.MethodGet, "/v1/geo/cooperative-boundaries/"+g.coopA.String(), nil)
	if vs := list(d.body, "versions"); len(vs) != 2 || !strings.Contains(str(vs[1], "correction_reason"), "htx_v2.geojson") || str(vs[1], "source") != "import_file" {
		t.Fatalf("lịch sử sau nhập: %s", d.raw)
	}
	// Shapefile WGS84 có trường MA_HTX; VN-2000 → từ chối cả tệp.
	sq := func(e, n, w float64) []shptest.Point {
		dLat, dLon := degPerMetre(baseLat)
		x0, y0 := baseLon+e*dLon, baseLat+n*dLat
		x1, y1 := x0+w*dLon, y0+w*dLat
		return []shptest.Point{{x0, y0}, {x0, y1}, {x1, y1}, {x1, y0}, {x0, y0}}
	}
	shp := []shptest.Feature{{Parts: []shptest.Polygon{{Outer: sq(5000, 0, 400)}}, Attrs: map[string]string{"MA_HTX": "HTXC"}}}
	r = admin.adminPost("/v1/geo/cooperative-boundaries/import?file_name=htx.zip", "application/zip", shptest.Zip([]string{"MA_HTX"}, shp, shptest.WGS84, shptest.Options{Base: "htx"}))
	if r.status != http.StatusOK || num(r.body, "accepted") != 1 {
		t.Fatalf("Shapefile WGS84: %d %s", r.status, r.raw)
	}
	r = admin.adminPost("/v1/geo/cooperative-boundaries/import?file_name=htx.zip", "application/zip", shptest.Zip([]string{"MA_HTX"}, shp, shptest.VN2000UTM48, shptest.Options{Base: "htx"}))
	if r.status != http.StatusUnprocessableEntity || code(r) != "geo.crs_unsupported" {
		t.Fatalf("Shapefile VN-2000: %d %s", r.status, r.raw)
	}
	// Admin tỉnh An Giang nhập cho HTX Cần Thơ → ngoài phạm vi (từng đối tượng).
	r = g.geoAdmin89().adminPost("/v1/geo/cooperative-boundaries/import", "application/geo+json",
		fc(feat(map[string]any{"cooperative_ref": g.coopB.String()}, rect(0, 3000, 300, 300))))
	if r.status != http.StatusOK || str(list(r.body, "features")[0], "code") != "geo.out_of_scope" {
		t.Fatalf("nhập ngoài phạm vi: %s", r.raw)
	}
	// Không có vai quản trị GIS → 403.
	expect(t, g.officerA().adminPost("/v1/geo/cooperative-boundaries/import", "application/geo+json", data), http.StatusForbidden, "không vai geo_admin")
}

// Marker HTX: điểm pf_party.cooperative.location được ưu tiên cho HTX có ranh giới,
// HTX chưa có ranh giới hiện bằng điểm (GIS US-BND-02 "gắn Marker").
func TestCoopMarkersFallback_GIS_US_BND_02(t *testing.T) {
	g := newG2(t)
	g.officerA().createCoop(g.coopA, rect(0, 0, 500, 500))
	dLat, dLon := degPerMetre(baseLat)
	at := func(e, n float64) (decimal.Decimal, decimal.Decimal) {
		return decimal.NewFromFloat(baseLon + e*dLon), decimal.NewFromFloat(baseLat + n*dLat)
	}
	lonA, latA := at(250, 250)
	lonB, latB := at(3000, 0)
	g.markers.mu.Lock()
	g.markers.ms = []partyport.CoopMarker{
		{Ref: g.coopA, Code: "HTXA", Name: "HTX A", CommuneCode: "31117", Lon: lonA, Lat: latA},
		{Ref: g.coopB, Code: "HTXB", Name: "HTX B", CommuneCode: "31117", Lon: lonB, Lat: latB},
		{Ref: g.coopC, Code: "HTXC", Name: "HTX C", CommuneCode: "31120", Lon: lonB, Lat: latB},
	}
	g.markers.mu.Unlock()
	r := g.officerA().appDo(http.MethodGet, g2BBox("/v1/geo/cooperative-boundaries"), nil)
	fs := list(r.body, "features")
	if len(fs) != 2 {
		t.Fatalf("muốn 1 vùng + 1 marker (HTX C ngoài phạm vi): %s", r.raw)
	}
	if str(fs[0], "properties", "marker_source") != "party_location" || str(fs[0], "geometry", "type") != "MultiPolygon" {
		t.Fatalf("vùng HTX A: %s", r.raw)
	}
	if str(fs[1], "id") != g.coopB.String() || str(fs[1], "geometry", "type") != "Point" {
		t.Fatalf("marker HTX B: %s", r.raw)
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
	viewer := g.as(g.tenant, g.orgA, port.RolePlotViewer)
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
	viewer := g.as(g.tenant, g.orgA, port.RolePlotViewer)
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

	viewer := g.as(g.tenant, g.orgA, port.RolePlotViewer)
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
	plot, _ := g.managerA().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
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
	if n := count(g.as(g.tenant, g.coopB, port.RolePlotEditor, port.RoleCoopManager), ""); n != 0 {
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
	g.officerA().createPlot("31117", uuid.Nil, rect(0, 0, 100, 100))
	g.officerA().createCoop(g.coopA, rect(0, 0, 500, 500))
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
