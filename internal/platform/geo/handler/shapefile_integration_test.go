package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/shptest"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// fakeOrgs thay party port.Orgs + port.OrgCodes.
type fakeOrgs struct{ orgs []partyport.Org }

func (f fakeOrgs) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	for _, o := range f.orgs {
		if o.Ref == ref {
			return o, nil
		}
	}
	return partyport.Org{}, partyport.ErrOrgNotFound
}

func (f fakeOrgs) ByCode(_ context.Context, code string) (partyport.Org, error) {
	for _, o := range f.orgs {
		if o.Code == code {
			return o, nil
		}
	}
	return partyport.Org{}, partyport.ErrOrgNotFound
}

// newFixtureOrgs như newFixture nhưng nối tra cứu HTX (Mã HTX trong tệp nhập).
func newFixtureOrgs(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, geo.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	sc := newFakeScope()
	tol := decimal.NewFromInt(1)
	f := &fixture{t: t, iss: iss, pool: pool, scope: sc, tenant: ids.New(), orgA: ids.New(), orgB: ids.New(), coopA: ids.New(), coopB: ids.New()}
	orgs := fakeOrgs{orgs: []partyport.Org{
		{Ref: f.coopA, Kind: partyport.OrgCooperative, Code: "HTX000001", Name: "HTX A", AdminUnitCode: "31117", Active: true},
		{Ref: f.orgA, Kind: partyport.OrgExtensionTeam, Code: "TKN-A", Name: "Tổ A", AdminUnitCode: "31117", Active: true},
	}}
	mod := geo.New(pool, clock.System{}, geo.Deps{Scope: sc, Params: fakeParams{tolerance: &tol}, AdminUnits: newFakeUnits(), Orgs: orgs})
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.app, f.admin, f.mod = httptest.NewServer(appEng), httptest.NewServer(adminEng), mod
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	sc.set(f.orgA, authz.Scope{Units: []string{"31117"}})
	sc.set(f.coopA, authz.Scope{Units: []string{"31117"}})
	return f
}

func sqDeg(east, north, side float64) []shptest.Point {
	dLat, dLon := degPerMetre(baseLat)
	x, y := baseLon+east*dLon, baseLat+north*dLat
	w, h := side*dLon, side*dLat
	return []shptest.Point{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
}

// KN US-PLOT-02, GIS FN-05: nhập Shapefile (.zip) — kết quả theo từng đối tượng như
// GeoJSON; trường MA_HTX của .dbf gán HTX theo Mã HTX; .prj phải là WGS84.
func TestImportShapefileZip(t *testing.T) {
	f := newFixtureOrgs(t)
	adm := f.national()
	fields := []string{"TEN_THUA", "MA_XA", "MA_HTX"}
	feats := []shptest.Feature{
		{Parts: []shptest.Polygon{{Outer: sqDeg(0, 0, 100)}}, Attrs: map[string]string{"TEN_THUA": "Thửa Ông Năm", "MA_XA": "31117", "MA_HTX": "HTX000001"}},
		{Parts: []shptest.Polygon{{Outer: sqDeg(300, 0, 60)}, {Outer: sqDeg(400, 0, 60)}}, Attrs: map[string]string{"TEN_THUA": "Hai mảnh", "MA_XA": "31117"}},
		{Parts: []shptest.Polygon{{Outer: sqDeg(600, 0, 80)}}, Attrs: map[string]string{"TEN_THUA": "HTX lạ", "MA_XA": "31117", "MA_HTX": "HTX999999"}},
		{Parts: nil, Attrs: map[string]string{"TEN_THUA": "Không hình", "MA_XA": "31117"}},
		{Parts: []shptest.Polygon{{Outer: sqDeg(30, 30, 100)}}, Attrs: map[string]string{"TEN_THUA": "Đè thửa 1", "MA_XA": "31117", "MA_HTX": "HTX000001"}},
	}
	zipData := shptest.Zip(fields, feats, shptest.WGS84, shptest.Options{})
	r := adm.adminPost("/v1/geo/plots/import?file_name=thua_vu_dx.zip", "application/zip", zipData)
	expect(t, r, http.StatusOK, "nhập Shapefile")
	fs := list(r.body, "features")
	if str(r.body, "format") != "shapefile" || get(r.body, "accepted") != 2.0 || get(r.body, "rejected") != 3.0 || len(fs) != 5 {
		t.Fatalf("kết quả nhập Shapefile: %s", r.raw)
	}
	for i, want := range []string{"", "", "geo.cooperative_invalid", "geo.invalid_geometry", "geo.overlap_needs_confirm"} {
		if got := str(fs[i], "code"); got != want {
			t.Fatalf("đối tượng %d: mã %q, muốn %q (%s)", i+1, got, want, r.raw)
		}
	}
	if str(fs[0], "name") != "Thửa Ông Năm" || !strings.Contains(str(fs[2], "reason"), "HTX999999") {
		t.Fatalf("tên/lý do: %s", r.raw)
	}
	p1 := adm.appDo(http.MethodGet, "/v1/plots/"+str(fs[0], "plot_ref"), nil)
	if str(p1.body, "plot", "properties", "cooperative_ref") != f.coopA.String() || str(p1.body, "plot", "properties", "source") != "import_file" {
		t.Fatalf("MA_HTX gán HTX theo Mã HTX: %s", p1.raw)
	}
	p2 := adm.appDo(http.MethodGet, "/v1/plots/"+str(fs[1], "plot_ref"), nil)
	if str(p2.body, "plot", "properties", "status") != "draft" || str(p2.body, "plot", "geometry", "type") != "MultiPolygon" {
		t.Fatalf("thửa hai mảnh, chưa gán HTX: %s", p2.raw)
	}

	// VN-2000 (.prj) → từ chối cả tệp, nêu cần chuyển sang EPSG:4326.
	vn := shptest.Zip(fields, feats[:1], shptest.VN2000UTM48, shptest.Options{})
	r = adm.adminPost("/v1/geo/plots/import?file_name=thua_vn2000.zip", "application/zip", vn)
	if r.status != http.StatusUnprocessableEntity || code(r) != "geo.crs_unsupported" || !strings.Contains(str(r.body, "detail"), "EPSG:4326") {
		t.Fatalf("VN-2000: %d %s", r.status, r.raw)
	}
	// Thiếu .prj → từ chối.
	r = adm.adminPost("/v1/geo/plots/import?format=shapefile", "application/zip", shptest.Zip(fields, feats[:1], "", shptest.Options{}))
	if r.status != http.StatusUnprocessableEntity || code(r) != "geo.crs_unsupported" {
		t.Fatalf("thiếu .prj: %d %s", r.status, r.raw)
	}
}
