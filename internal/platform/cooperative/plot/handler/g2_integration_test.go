package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/shptest"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
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
	ms []app.CoopMarker
}

func (f *fakeMarkers) MarkersInBBox(_ context.Context, minLon, minLat, maxLon, maxLat float64, _ int) ([]app.CoopMarker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []app.CoopMarker
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
	f := newFixture(t, func(d *app.Service) {
		d.Orgs, d.Markers, d.Params = orgs, markers, fakeParams{tolerance: &tol, values: params}
	})
	g := &g2{fixture: f, orgs: orgs, markers: markers, params: params, coopC: ids.New(), prov92: f.province92, prov89: ids.New()}
	seedSourceOrg(t, f.tenant, g.coopC, partyport.OrgCooperative, "HTXC", "31120", uuid.Nil)
	seedSourceOrg(t, f.tenant, g.prov89, partyport.OrgTTKNProvince, "TTKN89", "89", uuid.Nil)
	orgs.add(f.coopA, "HTXA", "31117")
	orgs.add(f.coopB, "HTXB", "31117")
	orgs.add(g.coopC, "HTXC", "31120")
	f.scope.set(g.coopC, authz.Scope{Units: []string{"31120"}})
	f.scope.set(g.prov92, authz.Scope{Units: []string{"92", "31117", "31120"}})
	f.scope.set(g.prov89, authz.Scope{Units: []string{"89", "30001"}})
	return g
}

func (g *g2) geoAdmin89() caller { return g.as(g.tenant, g.prov89, port.RoleGeoAdmin) }

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
	err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
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
	err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
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
	err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
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
	if n := outboxCount(t, g.fixture, coopport.EventCooperativeBoundaryRecorded, "cooperative_ref", g.coopA.String()); n != 4 {
		t.Fatalf("số sự kiện %s = %d, muốn 4", coopport.EventCooperativeBoundaryRecorded, n)
	}
	verifyChain(t, g.fixture, "pf_coop.cooperative_boundary", "cooperative:"+g.coopA.String())
	mustFailUpdate(t, g.fixture, "UPDATE pf_coop.cooperative_boundary SET area_ha = 1 WHERE cooperative_ref = $1", g.coopA)
	mustFailUpdate(t, g.fixture, "DELETE FROM pf_coop.cooperative_area WHERE cooperative_ref = $1", g.coopA)
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
	g.as(g.tenant, g.coopC, coopport.RolePlotEditor).createCoop(g.coopC, rect(4000, 0, 500, 500))

	// Tỉnh thấy cả ba; xã 31117 thấy A, B; quản lý HTX A chỉ thấy A; xã 31120 chỉ thấy C.
	for _, tc := range []struct {
		name string
		c    caller
		want []uuid.UUID
	}{
		{"tỉnh", g.as(g.tenant, g.prov92, coopport.RolePlotViewer), []uuid.UUID{g.coopA, g.coopB, g.coopC}},
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

// Cách ly tenant (RB-2) cho ranh giới HTX (kho/Hub, đường bộ: pf/geo).
func TestG2TenantIsolation(t *testing.T) {
	g := newG2(t)
	g.officerA().createCoop(g.coopA, rect(0, 0, 500, 500))
	other := g.as(ids.New(), uuid.Nil, coopport.RolePlotViewer, port.RoleGeoAdmin, authz.RolePlatformAdmin)
	for _, p := range []string{"/v1/geo/cooperative-boundaries"} {
		if n := len(featureRefs(other.appDo(http.MethodGet, g2BBox(p), nil))); n != 0 {
			t.Fatalf("tenant khác thấy %d đối tượng ở %s", n, p)
		}
	}
	expect(t, other.appDo(http.MethodGet, "/v1/geo/cooperative-boundaries/"+g.coopA.String(), nil), http.StatusNotFound, "tenant khác đọc HTX")
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu (ranh giới HTX; kho, đường: pf/geo).
func TestG2IdempotentReplay(t *testing.T) {
	g := newG2(t)
	for _, tc := range []struct {
		base, path string
		c          caller
		body       map[string]any
	}{
		{g.app.URL, "/v1/geo/cooperative-boundaries", g.officerA(), map[string]any{"cooperative_ref": g.coopA.String(), "geometry": rect(0, 0, 300, 300)}},
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
	if n := len(featureRefs(g.national().appDo(http.MethodGet, g2BBox("/v1/geo/cooperative-boundaries"), nil))); n != 1 {
		t.Fatalf("số ranh giới HTX sau 2 lần gọi = %d", n)
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
	g.markers.ms = []app.CoopMarker{
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

// ---------------------------------------------------------------------------
// Sức chứa, hub MKG qua van, widget
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Đường bộ
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Log GPS
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Lớp gộp
// ---------------------------------------------------------------------------
