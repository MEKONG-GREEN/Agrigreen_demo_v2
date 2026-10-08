package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	app    *httptest.Server
	admin  *httptest.Server
	mod    *geo.Module
	pool   *pgxpool.Pool
	scope  *fakeScope
	tenant uuid.UUID
	// Tổ chức giả: cán bộ xã 31117, cán bộ xã 31120, hai HTX ở xã 31117.
	orgA, orgB, coopA, coopB uuid.UUID
}

func newFixture(t *testing.T, opts ...func(*geo.Deps)) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, geo.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	sc := newFakeScope()
	tol := decimal.NewFromInt(1)
	deps := geo.Deps{Scope: sc, Params: fakeParams{tolerance: &tol}, AdminUnits: newFakeUnits()}
	for _, o := range opts {
		o(&deps)
	}
	mod := geo.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, deps)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 4<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 4<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appSrv, adminSrv := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(appSrv.Close)
	t.Cleanup(adminSrv.Close)
	f := &fixture{t: t, iss: iss, app: appSrv, admin: adminSrv, mod: mod, pool: pool, scope: sc, tenant: ids.New(),
		orgA: ids.New(), orgB: ids.New(), coopA: ids.New(), coopB: ids.New()}
	sc.set(f.orgA, authz.Scope{Units: []string{"31117"}})
	sc.set(f.orgB, authz.Scope{Units: []string{"31120"}})
	sc.set(f.coopA, authz.Scope{Units: []string{"31117"}})
	sc.set(f.coopB, authz.Scope{Units: []string{"31117"}})
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	roles []string
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, roles: roles, token: tok, actor: actor}
}

// national: quản trị toàn quốc (phạm vi All).
func (f *fixture) national() caller {
	return f.as(f.tenant, uuid.Nil, port.RolePlotEditor, port.RoleGeoAdmin, authz.RolePlatformAdmin)
}

func (f *fixture) officerA() caller { return f.as(f.tenant, f.orgA, port.RolePlotEditor) }
func (f *fixture) officerB() caller { return f.as(f.tenant, f.orgB, port.RolePlotEditor) }
func (f *fixture) managerA() caller {
	return f.as(f.tenant, f.coopA, port.RolePlotEditor, port.RoleCoopManager)
}

type resp struct {
	status int
	hdr    http.Header
	raw    []byte
	body   map[string]any
}

func (c caller) do(base, method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", ctype)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, raw: raw, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) appDo(method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	idem := ""
	if method == http.MethodPost {
		idem = key()
	}
	return c.do(c.f.app.URL, method, path, idem, "application/json", b)
}

func (c caller) adminPost(path, ctype string, body []byte) resp {
	c.f.t.Helper()
	return c.do(c.f.admin.URL, http.MethodPost, path, key(), ctype, body)
}

func get(m any, path ...string) any {
	cur := m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m any, path ...string) string {
	s, _ := get(m, path...).(string)
	return s
}

func list(m any, path ...string) []any {
	l, _ := get(m, path...).([]any)
	return l
}

func code(r resp) string { return str(r.body, "code") }

func expect(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: HTTP %d, muốn %d: %s", what, r.status, status, r.raw)
	}
}

// createPlot tạo thửa và trả ref.
func (c caller) createPlot(commune string, coop uuid.UUID, geom map[string]any) (uuid.UUID, resp) {
	c.f.t.Helper()
	body := map[string]any{"commune_code": commune, "geometry": geom}
	if coop != uuid.Nil {
		body["cooperative_ref"] = coop.String()
	}
	r := c.appDo(http.MethodPost, "/v1/plots", body)
	expect(c.f.t, r, http.StatusCreated, "tạo thửa")
	return uuid.MustParse(str(r.body, "plot", "id")), r
}

func bboxPath(limit int) string {
	return fmt.Sprintf("/v1/plots?bbox=%f,%f,%f,%f&limit=%d", baseLon-0.05, baseLat-0.05, baseLon+0.1, baseLat+0.1, limit)
}

func featureIDs(r resp) []string {
	var out []string
	for _, f := range list(r.body, "features") {
		out = append(out, str(f, "id"))
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------

// KN US-PLOT-01 AC-2: diện tích tự tính từ polygon — trắc địa (ellipsoid WGS84), sai số ≤ 0,5 %.
func TestGeodesicAreaNearCanTho(t *testing.T) {
	f := newFixture(t)
	c := f.national()
	ref, r := c.createPlot("31117", uuid.Nil, rect(0, 0, 200, 250)) // 5 ha
	area := decimal.RequireFromString(str(r.body, "plot", "properties", "area_ha"))
	if diff := area.Sub(decimal.NewFromInt(5)).Abs(); diff.GreaterThan(decimal.RequireFromString("0.025")) {
		t.Fatalf("diện tích = %s ha, muốn 5 ha ± 0,5%%", area)
	}
	if got := str(r.body, "plot", "properties", "status"); got != port.StatusDraft {
		t.Fatalf("thửa KN vẽ trước khi gán HTX phải là draft, được %s", got)
	}
	if n, _ := get(r.body, "boundary", "point_count").(float64); n != 4 {
		t.Fatalf("point_count = %v", n)
	}
	if v, _ := get(r.body, "boundary", "version").(float64); v != 1 {
		t.Fatalf("phiên bản đầu = %v", v)
	}
	// Hai sự kiện trong outbox cùng giao dịch.
	var types []string
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), "SELECT event_type FROM shared.outbox WHERE payload->>'plot_ref' = $1 ORDER BY event_type", ref.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			types = append(types, s)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	// Hai sự kiện cùng một giao dịch: thứ tự id UUIDv7 trong cùng mili giây không bảo đảm.
	sort.Strings(types)
	want := []string{port.EventPlotBoundaryRecorded, port.EventPlotCreated}
	sort.Strings(want)
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("sự kiện = %v", types)
	}
}

// FN-XA-03.05 / HTX US-PLOT-02 AC-2: < 3 điểm phân biệt hoặc thẳng hàng, tự cắt, hệ toạ độ phẳng, diện tích vô lý → chặn
// lưu; tam giác (3 điểm) hợp lệ — quyết định A2 03/10/2026 (CHECK point_count >= 3, pf 000084).
func TestBoundaryValidationRules(t *testing.T) {
	f := newFixture(t)
	c := f.national()
	dLat, dLon := degPerMetre(baseLat)
	tri := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{baseLon, baseLat}, {baseLon + 100*dLon, baseLat}, {baseLon, baseLat + 100*dLat}}}}
	dup := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{baseLon, baseLat}, {baseLon + 100*dLon, baseLat}, {baseLon + 100*dLon, baseLat}}}}
	col := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{baseLon, baseLat}, {baseLon + 50*dLon, baseLat}, {baseLon + 100*dLon, baseLat}}}}
	bow := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{
		{baseLon, baseLat}, {baseLon + 100*dLon, baseLat + 100*dLat}, {baseLon + 100*dLon, baseLat}, {baseLon, baseLat + 100*dLat}}}}
	vn2000 := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{585000, 1108000}, {585100, 1108000}, {585100, 1108100}, {585000, 1108100}}}}
	cases := []struct {
		name string
		geom map[string]any
		want string
	}{
		{"2 điểm phân biệt", dup, "geo.too_few_points"},
		{"3 điểm thẳng hàng", col, "geo.too_few_points"},
		{"hình nơ bướm tự cắt", bow, "geo.invalid_geometry"},
		{"toạ độ VN-2000", vn2000, "geo.crs_unsupported"},
		{"quá nhỏ (1 m²)", rect(0, 0, 1, 1), "geo.area_out_of_range"},
	}
	for _, cs := range cases {
		r := c.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "geometry": cs.geom})
		if r.status != http.StatusUnprocessableEntity || code(r) != cs.want {
			t.Fatalf("%s: %d %s, muốn 422 %s", cs.name, r.status, r.raw, cs.want)
		}
	}
	r := c.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "99999", "geometry": rect(0, 0, 50, 50)})
	expect(t, r, http.StatusUnprocessableEntity, "mã xã không tồn tại")
	// A2: tam giác lưu được, point_count = 3 qua CHECK của DB.
	r = c.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "geometry": tri})
	expect(t, r, http.StatusCreated, "tam giác 3 điểm")
}

// KN US-PLOT-03, HTX US-PLOT-04 AC-1/AC-2, nguyên mẫu mdm/lifecycle.ts: chồng lấn thửa của HTX KHÁC
// vượt ngưỡng → chặn (geo.overlap_cross_coop, không bỏ qua được); chồng lấn cùng HTX hoặc với thửa
// nháp → 409 geo.overlap_needs_confirm, chỉ lưu khi confirm_overlap = true (mở xung đột xem xét).
// Thửa liền kề chung cạnh và phần chồng dưới ngưỡng 1 % không bị coi là chồng lấn.
func TestOverlapRules(t *testing.T) {
	f := newFixture(t)
	c := f.national()
	p1, _ := c.createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	body := func(coop uuid.UUID, geom map[string]any, confirm bool) map[string]any {
		b := map[string]any{"commune_code": "31117", "geometry": geom, "confirm_overlap": confirm}
		if coop != uuid.Nil {
			b["cooperative_ref"] = coop.String()
		}
		return b
	}

	// HTX khác: chặn, kể cả khi xác nhận.
	for _, confirm := range []bool{false, true} {
		r := c.appDo(http.MethodPost, "/v1/plots", body(f.coopB, rect(50, 0, 100, 100), confirm))
		if r.status != http.StatusConflict || code(r) != "geo.overlap_cross_coop" {
			t.Fatalf("khác HTX (confirm=%v): %d %s", confirm, r.status, r.raw)
		}
		if ov := list(r.body, "overlaps"); len(ov) != 1 || str(ov[0], "plot_ref") != p1.String() || str(ov[0], "overlap_pct") != "50.0000" {
			t.Fatalf("danh sách chồng lấn = %v", ov)
		}
	}

	// Cùng HTX: cần xác nhận.
	r := c.appDo(http.MethodPost, "/v1/plots", body(f.coopA, rect(50, 0, 100, 100), false))
	if r.status != http.StatusConflict || code(r) != "geo.overlap_needs_confirm" || get(r.body, "needs_confirm") != true {
		t.Fatalf("cùng HTX chưa xác nhận: %d %s", r.status, r.raw)
	}
	// Thửa nháp (chưa gán HTX) đè thửa HTX A: cũng chỉ cần xác nhận.
	r = c.appDo(http.MethodPost, "/v1/plots", body(uuid.Nil, rect(50, 0, 100, 100), false))
	if r.status != http.StatusConflict || code(r) != "geo.overlap_needs_confirm" {
		t.Fatalf("thửa nháp chưa xác nhận: %d %s", r.status, r.raw)
	}
	// Xác nhận → lưu, mở xung đột same_coop_overlap, chờ xem xét.
	r = c.appDo(http.MethodPost, "/v1/plots", body(f.coopA, rect(50, 0, 100, 100), true))
	expect(t, r, http.StatusCreated, "cùng HTX đã xác nhận")
	p2 := uuid.MustParse(str(r.body, "plot", "id"))
	cs := list(r.body, "conflicts")
	if len(cs) != 1 || str(cs[0], "kind") != "same_coop_overlap" || str(cs[0], "owner_ref") != f.coopA.String() ||
		get(r.body, "plot", "properties", "needs_review") != true || len(list(r.body, "flagged_overlaps")) != 1 {
		t.Fatalf("xung đột sau xác nhận: %s", r.raw)
	}
	due, _ := time.Parse(time.RFC3339Nano, str(cs[0], "due_at"))
	det, _ := time.Parse(time.RFC3339Nano, str(cs[0], "detected_at"))
	if due.Sub(det) != 72*time.Hour {
		t.Fatalf("SLA = %v, muốn 72 giờ", due.Sub(det))
	}

	// Liền kề chung cạnh: nhận, không cờ. Chồng 0,5 % (dưới ngưỡng 1 %): nhận.
	_, r = c.createPlot("31117", f.coopB, rect(0, 100, 100, 100))
	if len(list(r.body, "flagged_overlaps")) != 0 || get(r.body, "plot", "properties", "needs_review") != false {
		t.Fatalf("thửa liền kề bị cờ: %s", r.raw)
	}
	_, r = c.createPlot("31117", f.coopA, rect(0, -99.5, 100, 100))
	if len(list(r.body, "flagged_overlaps")) != 0 {
		t.Fatalf("chồng dưới ngưỡng bị cờ: %s", r.raw)
	}

	// HTX US-PLOT-02 AC-3: lệch > 5 % so với diện tích khai báo → cần xác nhận.
	r = c.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "geometry": rect(1000, 0, 100, 100), "declared_area_ha": "1.2"})
	if r.status != http.StatusConflict || code(r) != "geo.area_deviation_needs_confirm" || str(r.body, "deviation_pct") < "16" {
		t.Fatalf("lệch diện tích: %d %s", r.status, r.raw)
	}
	c.createPlot("31117", uuid.Nil, rect(1000, 0, 100, 100)) // không khai báo: không kiểm
	r = c.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "geometry": rect(1300, 0, 100, 100), "declared_area_ha": "1.03"})
	expect(t, r, http.StatusCreated, "lệch 3 % (dưới ngưỡng)")
	r = c.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "geometry": rect(1600, 0, 100, 100), "declared_area_ha": "1.2", "confirm_overlap": true})
	expect(t, r, http.StatusCreated, "lệch diện tích đã xác nhận")

	// Kiểm trước khi lưu: HTX khác → blocking; cùng HTX → needs_confirm.
	r = c.appDo(http.MethodPost, "/v1/plots/overlap-check", map[string]any{"commune_code": "31117", "cooperative_ref": f.coopB.String(), "geometry": rect(20, 20, 30, 30)})
	expect(t, r, http.StatusOK, "overlap-check HTX B")
	if get(r.body, "can_save") != false || len(list(r.body, "blocking")) != 1 {
		t.Fatalf("overlap-check HTX B: %s", r.raw)
	}
	r = c.appDo(http.MethodPost, "/v1/plots/overlap-check", map[string]any{"commune_code": "31117", "cooperative_ref": f.coopA.String(), "geometry": rect(20, 20, 30, 30)})
	if get(r.body, "can_save") != true || get(r.body, "requires_confirm") != true || len(list(r.body, "needs_confirm")) != 1 {
		t.Fatalf("overlap-check HTX A: %s", r.raw)
	}

	// Gán lại HTX kiểm lại chồng lấn: thửa nháp D đè p1 (HTX A).
	r = c.appDo(http.MethodPost, "/v1/plots", body(uuid.Nil, rect(0, 60, 40, 30), true))
	expect(t, r, http.StatusCreated, "thửa nháp D đã xác nhận")
	d := str(r.body, "plot", "id")
	r = c.appDo(http.MethodPost, "/v1/plots/"+d+"/assign", map[string]any{"cooperative_ref": f.coopB.String()})
	if r.status != http.StatusConflict || code(r) != "geo.overlap_cross_coop" {
		t.Fatalf("gán D cho HTX B: %d %s", r.status, r.raw)
	}
	r = c.appDo(http.MethodPost, "/v1/plots/"+d+"/assign", map[string]any{"cooperative_ref": f.coopA.String()})
	if r.status != http.StatusConflict || code(r) != "geo.overlap_needs_confirm" {
		t.Fatalf("gán D cho HTX A chưa xác nhận: %d %s", r.status, r.raw)
	}
	r = c.appDo(http.MethodPost, "/v1/plots/"+d+"/assign", map[string]any{"cooperative_ref": f.coopA.String(), "confirm_overlap": true})
	expect(t, r, http.StatusOK, "gán D cho HTX A đã xác nhận")
	if len(list(r.body, "conflicts")) != 1 || get(r.body, "plot", "properties", "needs_review") != true {
		t.Fatalf("gán có xác nhận: %s", r.raw)
	}

	// Xử lý xung đột của p2 → gỡ cờ.
	r = c.appDo(http.MethodGet, "/v1/plots/"+p2.String(), nil)
	cid := str(list(r.body, "conflicts")[0], "id")
	r = c.appDo(http.MethodPost, "/v1/geo/conflicts/"+cid+"/resolve", map[string]any{"note": "ngắn"})
	expect(t, r, http.StatusUnprocessableEntity, "ghi chú quá ngắn")
	r = c.appDo(http.MethodPost, "/v1/geo/conflicts/"+cid+"/resolve", map[string]any{"note": "Đã đối chiếu thực địa, HTX thống nhất ranh giới"})
	expect(t, r, http.StatusOK, "xử lý xung đột")
	r = c.appDo(http.MethodGet, "/v1/plots/"+p2.String(), nil)
	if get(r.body, "plot", "properties", "needs_review") != false {
		t.Fatalf("cờ chờ xem xét phải được gỡ: %s", r.raw)
	}
	r = c.appDo(http.MethodPost, "/v1/geo/conflicts/"+cid+"/resolve", map[string]any{"note": "Xử lý lần hai không được phép"})
	if r.status != http.StatusConflict || code(r) != "geo.conflict_state" {
		t.Fatalf("xử lý lại: %d %s", r.status, r.raw)
	}
	r = c.appDo(http.MethodGet, "/v1/geo/conflicts?status=open", nil)
	if len(list(r.body, "items")) != 2 {
		t.Fatalf("còn đúng 2 xung đột mở (thửa D: lúc tạo và lúc gán): %s", r.raw)
	}
}

// HTX US-PLOT-01 AC-2, ADR-0011: sửa ranh giới = phiên bản mới trỏ về phiên bản cũ; phiên bản cũ
// giữ nguyên, chuỗi băm của sổ xác minh được, sổ không UPDATE/DELETE được.
func TestCorrectionCreatesNewVersion(t *testing.T) {
	f := newFixture(t)
	c := f.officerA()
	ref, r := c.createPlot("31117", uuid.Nil, rect(0, 0, 100, 100))
	v1 := str(r.body, "boundary", "boundary_id")
	path := "/v1/plots/" + ref.String() + "/boundaries"

	r = c.appDo(http.MethodPost, path, map[string]any{"supersedes_id": v1, "geometry": rect(0, 0, 100, 120)})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu lý do phải 422: %d %s", r.status, r.raw)
	}
	r = c.appDo(http.MethodPost, path, map[string]any{"supersedes_id": v1, "correction_reason": "Đo lại bằng GPS ngoài đồng", "geometry": rect(0, 0, 100, 120), "gps_accuracy_m": "2.5", "source": "kn_field_app"})
	expect(t, r, http.StatusCreated, "sửa ranh giới")
	v2 := str(r.body, "boundary", "boundary_id")
	if v, _ := get(r.body, "boundary", "version").(float64); v != 2 || str(r.body, "boundary", "supersedes_id") != v1 {
		t.Fatalf("phiên bản 2 sai: %s", r.raw)
	}
	if a := decimal.RequireFromString(str(r.body, "plot", "properties", "area_ha")); a.Sub(decimal.RequireFromString("1.2")).Abs().GreaterThan(decimal.RequireFromString("0.006")) {
		t.Fatalf("diện tích sau sửa = %s, muốn 1,2 ha", a)
	}
	// Sửa trên phiên bản cũ (người khác đã sửa) → 409.
	r = c.appDo(http.MethodPost, path, map[string]any{"supersedes_id": v1, "correction_reason": "Sửa dựa trên bản cũ", "geometry": rect(0, 0, 90, 90)})
	if r.status != http.StatusConflict || code(r) != "geo.version_conflict" || str(r.body, "current_boundary_id") != v2 {
		t.Fatalf("sửa trên bản cũ: %d %s", r.status, r.raw)
	}

	r = c.appDo(http.MethodGet, "/v1/plots/"+ref.String(), nil)
	expect(t, r, http.StatusOK, "đọc thửa")
	vs := list(r.body, "versions")
	if len(vs) != 2 || str(vs[0], "boundary_id") != v1 || str(vs[1], "boundary_id") != v2 || get(vs[0], "geometry") == nil {
		t.Fatalf("lịch sử phiên bản = %s", r.raw)
	}
	if v, _ := get(r.body, "plot", "properties", "version").(float64); v != 2 {
		t.Fatalf("phiên bản hiện hành = %v", v)
	}

	ctx := context.Background()
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		for _, tbl := range []string{"pf_geo.plot_boundary", "pf_geo.plot_event"} {
			res, err := ledger.Verify(ctx, tx, tbl, f.tenant, "plot:"+ref.String())
			if err != nil {
				return err
			}
			if !res.OK || res.RowsChecked < 1 {
				return fmt.Errorf("%s: chuỗi không hợp lệ %+v", tbl, res)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sổ chỉ ghi thêm: vai ứng dụng không sửa được hình.
	err = tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE pf_geo.plot_boundary SET area_ha = 1 WHERE plot_ref = $1", ref)
		return err
	})
	if err == nil {
		t.Fatal("UPDATE sổ ranh giới phải thất bại")
	}
}

// GIS FN-07 BR-25: đọc theo khung nhìn, tối đa 1.000 polygon; phạm vi xã (ADR-0009):
// cán bộ xã A không thấy/ghi thửa xã B; quản lý HTX chỉ thấy HTX mình và thửa nháp.
func TestBBoxLimitAndScope(t *testing.T) {
	f := newFixture(t)
	nat := f.national()
	a1, _ := nat.createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	a2, _ := nat.createPlot("31117", uuid.Nil, rect(300, 0, 100, 100))
	a3, _ := nat.createPlot("31117", f.coopB, rect(600, 0, 100, 100))
	b1, _ := nat.createPlot("31120", uuid.Nil, rect(0, 300, 100, 100))

	r := nat.appDo(http.MethodGet, bboxPath(1000), nil)
	expect(t, r, http.StatusOK, "bbox toàn quốc")
	if len(featureIDs(r)) != 4 || get(r.body, "truncated") != false || str(r.body, "type") != "FeatureCollection" {
		t.Fatalf("bbox toàn quốc = %s", r.raw)
	}
	r = nat.appDo(http.MethodGet, bboxPath(2), nil)
	if len(featureIDs(r)) != 2 || get(r.body, "truncated") != true {
		t.Fatalf("limit=2 phải cắt và báo truncated: %s", r.raw)
	}
	r = nat.appDo(http.MethodGet, bboxPath(1001), nil)
	expect(t, r, http.StatusUnprocessableEntity, "limit > 1000")
	r = nat.appDo(http.MethodGet, "/v1/plots?bbox=1,2,3", nil)
	expect(t, r, http.StatusUnprocessableEntity, "bbox thiếu số")
	// Zoom xa: hình được đơn giản hóa nhưng vẫn là Feature hợp lệ.
	r = nat.appDo(http.MethodGet, bboxPath(1000)+"&zoom=10", nil)
	if len(featureIDs(r)) != 4 || str(list(r.body, "features")[0], "geometry", "type") == "" {
		t.Fatalf("zoom=10: %s", r.raw)
	}

	oa := f.officerA()
	seen := featureIDs(oa.appDo(http.MethodGet, bboxPath(1000), nil))
	if len(seen) != 3 || contains(seen, b1.String()) {
		t.Fatalf("cán bộ xã A thấy %v", seen)
	}
	seen = featureIDs(f.officerB().appDo(http.MethodGet, bboxPath(1000), nil))
	if len(seen) != 1 || seen[0] != b1.String() {
		t.Fatalf("cán bộ xã B thấy %v", seen)
	}
	r = oa.appDo(http.MethodGet, "/v1/plots/"+b1.String(), nil)
	if r.status != http.StatusNotFound || code(r) != "geo.plot_not_found" {
		t.Fatalf("xã A đọc thửa xã B: %d %s", r.status, r.raw)
	}
	r = oa.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31120", "geometry": rect(0, 600, 50, 50)})
	if r.status != http.StatusForbidden || code(r) != "geo.out_of_scope" {
		t.Fatalf("xã A vẽ ở xã B: %d %s", r.status, r.raw)
	}

	// Quản lý HTX A: thửa của HTX A + thửa nháp, không thấy HTX B (HTX US-PLOT-01 AC-1, AC-4).
	ma := f.managerA()
	seen = featureIDs(ma.appDo(http.MethodGet, bboxPath(1000), nil))
	if len(seen) != 2 || !contains(seen, a1.String()) || !contains(seen, a2.String()) {
		t.Fatalf("quản lý HTX A thấy %v", seen)
	}
	r = ma.appDo(http.MethodGet, "/v1/plots/"+a3.String(), nil)
	expect(t, r, http.StatusNotFound, "quản lý HTX A đọc thửa HTX B")
	r = ma.appDo(http.MethodGet, "/v1/cooperatives/"+f.coopB.String()+"/plots", nil)
	expect(t, r, http.StatusForbidden, "quản lý HTX A liệt kê HTX B")
	r = ma.appDo(http.MethodGet, "/v1/cooperatives/"+f.coopA.String()+"/plots", nil)
	if r.status != http.StatusOK || len(list(r.body, "features")) != 1 {
		t.Fatalf("thửa HTX A: %d %s", r.status, r.raw)
	}
	// Quản lý HTX nhận thửa nháp về HTX mình; không gán được cho HTX khác.
	r = ma.appDo(http.MethodPost, "/v1/plots/"+a2.String()+"/assign", map[string]any{"cooperative_ref": f.coopB.String()})
	expect(t, r, http.StatusForbidden, "quản lý HTX A gán cho HTX B")
	r = ma.appDo(http.MethodPost, "/v1/plots/"+a2.String()+"/assign", map[string]any{"cooperative_ref": f.coopA.String()})
	expect(t, r, http.StatusOK, "nhận thửa nháp")
	if str(r.body, "plot", "properties", "status") != port.StatusActive {
		t.Fatalf("thửa đã gán phải active: %s", r.raw)
	}
	// Không có vai → 403.
	r = f.as(f.tenant, f.orgA, "consent_reader").appDo(http.MethodGet, bboxPath(10), nil)
	expect(t, r, http.StatusForbidden, "không có vai geo")
}

// R-16, RB-2: tenant khác không thấy gì của tenant này.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.national().createPlot("31117", uuid.Nil, rect(0, 0, 100, 100))
	other := f.as(ids.New(), uuid.Nil, port.RolePlotEditor, authz.RolePlatformAdmin)
	r := other.appDo(http.MethodGet, "/v1/plots/"+ref.String(), nil)
	expect(t, r, http.StatusNotFound, "tenant khác đọc thửa")
	if n := len(featureIDs(other.appDo(http.MethodGet, bboxPath(1000), nil))); n != 0 {
		t.Fatalf("tenant khác thấy %d thửa", n)
	}
	// Tenant khác vẽ đè đúng chỗ đó: không bị coi là chồng lấn (dữ liệu tách biệt).
	_, r = other.createPlot("31117", uuid.Nil, rect(0, 0, 100, 100))
	if len(list(r.body, "flagged_overlaps")) != 0 {
		t.Fatalf("chồng lấn xuyên tenant: %s", r.raw)
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu, không tạo thửa thứ hai.
func TestIdempotentCreate(t *testing.T) {
	f := newFixture(t)
	c := f.national()
	body, _ := json.Marshal(map[string]any{"commune_code": "31117", "geometry": rect(0, 0, 80, 80), "name": "Thửa bờ kênh"})
	k := key()
	r1 := c.do(f.app.URL, http.MethodPost, "/v1/plots", k, "application/json", body)
	r2 := c.do(f.app.URL, http.MethodPost, "/v1/plots", k, "application/json", body)
	expect(t, r1, http.StatusCreated, "lần 1")
	expect(t, r2, http.StatusCreated, "lần 2")
	if r2.hdr.Get("Idempotent-Replayed") != "true" || !bytes.Equal(r1.raw, r2.raw) {
		t.Fatalf("phát lại khác lần đầu:\n%s\n%s", r1.raw, r2.raw)
	}
	if n := len(featureIDs(c.appDo(http.MethodGet, bboxPath(1000), nil))); n != 1 {
		t.Fatalf("số thửa sau 2 lần gọi = %d", n)
	}
	// Cùng khóa, thân khác → 422.
	other, _ := json.Marshal(map[string]any{"commune_code": "31117", "geometry": rect(500, 0, 80, 80)})
	r3 := c.do(f.app.URL, http.MethodPost, "/v1/plots", k, "application/json", other)
	expect(t, r3, http.StatusUnprocessableEntity, "cùng khóa khác thân")
}

func syncCtx(f *fixture, org uuid.UUID, roles ...string) context.Context {
	p := authz.Principal{ActorRef: ids.New(), TenantID: f.tenant, OrgRef: org, Roles: roles}
	return tenant.WithID(authz.WithPrincipal(context.Background(), p), f.tenant)
}

// docs/dev/sync-contract.md: chiều xuống theo con trỏ sync_seq, tombstone khi ngừng dùng hoặc ra khỏi phạm vi.
func TestSyncSourceCursorAndTombstone(t *testing.T) {
	f := newFixture(t)
	nat := f.national()
	p1, _ := nat.createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	p2, _ := nat.createPlot("31117", f.coopA, rect(300, 0, 100, 100))
	nat.createPlot("31120", uuid.Nil, rect(0, 300, 100, 100)) // ngoài phạm vi xã A

	src := f.mod.SyncSource()
	if got := src.Entities(); len(got) != 1 || got[0] != "plot" {
		t.Fatalf("Entities = %v", got)
	}
	ctx := syncCtx(f, f.orgA, port.RolePlotEditor)
	scA := authz.Scope{Units: []string{"31117"}}
	ch, last, err := src.Changes(ctx, scA, "plot", 0, 1)
	if err != nil || len(ch) != 1 || ch[0].ID != p1 || ch[0].Op != "upsert" {
		t.Fatalf("trang 1: %v %+v", err, ch)
	}
	var data map[string]any
	if err := json.Unmarshal(ch[0].Data, &data); err != nil || str(data, "geometry", "type") != "MultiPolygon" || str(data, "area_ha") == "" {
		t.Fatalf("dữ liệu thửa: %v %s", err, ch[0].Data)
	}
	ch, last, err = src.Changes(ctx, scA, "plot", last, 10)
	if err != nil || len(ch) != 1 || ch[0].ID != p2 {
		t.Fatalf("trang 2: %v %+v", err, ch)
	}
	ch, last2, err := src.Changes(ctx, scA, "plot", last, 10)
	if err != nil || len(ch) != 0 || last2 != last {
		t.Fatalf("không còn thay đổi: %v %+v", err, ch)
	}

	r := nat.appDo(http.MethodPost, "/v1/plots/"+p1.String()+"/retire", map[string]any{"reason": "Thửa đã chuyển đổi sang đất ở"})
	expect(t, r, http.StatusOK, "ngừng dùng")
	ch, last, err = src.Changes(ctx, scA, "plot", last, 10)
	if err != nil || len(ch) != 1 || ch[0].ID != p1 || ch[0].Op != "tombstone" || ch[0].Data != nil {
		t.Fatalf("tombstone sau ngừng dùng: %v %+v", err, ch)
	}
	// Thửa đã ngừng dùng ẩn khỏi bản đồ (GIS BR-17) nhưng vẫn đọc được lịch sử.
	if seen := featureIDs(nat.appDo(http.MethodGet, bboxPath(1000), nil)); contains(seen, p1.String()) {
		t.Fatal("thửa ngừng dùng vẫn hiện trên bản đồ")
	}
	expect(t, nat.appDo(http.MethodGet, "/v1/plots/"+p1.String(), nil), http.StatusOK, "đọc thửa ngừng dùng")

	// Quản lý HTX A: thửa chuyển sang HTX B → tombstone trên máy của HTX A.
	mctx := syncCtx(f, f.coopA, port.RolePlotEditor, port.RoleCoopManager)
	_, mlast, err := src.Changes(mctx, scA, "plot", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, nat.appDo(http.MethodPost, "/v1/plots/"+p2.String()+"/assign", map[string]any{"cooperative_ref": f.coopB.String()}), http.StatusOK, "chuyển HTX")
	ch, _, err = src.Changes(mctx, scA, "plot", mlast, 10)
	if err != nil || len(ch) != 1 || ch[0].ID != p2 || ch[0].Op != "tombstone" {
		t.Fatalf("tombstone khi chuyển HTX: %v %+v", err, ch)
	}
	// Cán bộ xã vẫn thấy (upsert).
	ch, _, err = src.Changes(ctx, scA, "plot", last, 10)
	if err != nil || len(ch) != 1 || ch[0].Op != "upsert" {
		t.Fatalf("cán bộ xã sau chuyển HTX: %v %+v", err, ch)
	}
	if _, _, err := src.Changes(ctx, scA, "task", 0, 10); err == nil {
		t.Fatal("entity lạ phải lỗi")
	}
}

func boundaryRecord(entityOp string, plotRef uuid.UUID, commune string, coop uuid.UUID, geom map[string]any, supersedes *uuid.UUID) (uuid.UUID, []byte, string, *uuid.UUID) {
	payload := map[string]any{"plot_ref": plotRef.String(), "commune_code": commune, "geometry": geom, "gps_accuracy_m": "4.0"}
	if coop != uuid.Nil {
		payload["cooperative_ref"] = coop.String()
	}
	b, _ := json.Marshal(payload)
	return ids.New(), b, entityOp, supersedes
}

// Mục 15: ranh giới vẽ ngoại tuyến — idempotent theo idem_key; chồng lấn cùng HTX hoặc
// sửa đồng thời → giữ cả hai, mở xung đột (StatusConflict) chứ không từ chối.
func TestSyncSinkIdempotentAndConflict(t *testing.T) {
	f := newFixture(t)
	snk := f.mod.SyncSink()
	if got := snk.Entities(); len(got) != 1 || got[0] != "plot_boundary" {
		t.Fatalf("Entities = %v", got)
	}
	ctx := syncCtx(f, f.orgA, port.RolePlotEditor)
	scA := authz.Scope{Units: []string{"31117"}}
	dev1, dev2 := ids.New(), ids.New()
	type rec = struct {
		id      uuid.UUID
		payload []byte
		op      string
		sup     *uuid.UUID
	}
	mk := func(op string, plot uuid.UUID, geom map[string]any, sup *uuid.UUID) rec {
		id, p, o, s := boundaryRecord(op, plot, "31117", f.coopA, geom, sup)
		return rec{id, p, o, s}
	}
	send := func(r rec, dev uuid.UUID, idem, reason string) (string, *uuid.UUID, string) {
		var cr *string
		if reason != "" {
			cr = &reason
		}
		res := snk.Accept(ctx, scA, syncRecord(r.id, idem, r.op, r.payload, r.sup, cr, dev))
		return res.Status, res.ConflictID, res.Code
	}

	plot1 := ids.New()
	r1 := mk("create", plot1, rect(0, 0, 100, 100), nil)
	idem1 := "dev:" + dev1.String() + ":1"
	if st, _, c := send(r1, dev1, idem1, ""); st != "accepted" {
		t.Fatalf("tạo ngoại tuyến: %s %s", st, c)
	}
	if st, _, _ := send(r1, dev1, idem1, ""); st != "duplicate" {
		t.Fatalf("gửi lặp phải duplicate, được %s", st)
	}

	// Máy 2 (không thấy máy 1) vẽ đè thửa cùng HTX → conflict, vẫn lưu.
	plot2 := ids.New()
	st, cid, c := send(mk("create", plot2, rect(40, 0, 100, 100), nil), dev2, "dev:"+dev2.String()+":1", "")
	if st != "conflict" || cid == nil {
		t.Fatalf("chồng lấn cùng HTX ngoại tuyến: %s %v %s", st, cid, c)
	}
	r := f.national().appDo(http.MethodGet, "/v1/plots/"+plot2.String(), nil)
	expect(t, r, http.StatusOK, "thửa ngoại tuyến bị xung đột vẫn được giữ")
	cs := list(r.body, "conflicts")
	if get(r.body, "plot", "properties", "needs_review") != true || len(cs) != 1 || str(cs[0], "kind") != "same_coop_overlap" ||
		str(cs[0], "id") != cid.String() || str(cs[0], "owner_ref") != f.coopA.String() {
		t.Fatalf("xung đột cùng HTX: %s", r.raw)
	}

	// Hai bản sửa cùng dựa trên phiên bản 1 (thu hẹp, hết đè thửa 2): bản đầu thành hiện hành,
	// bản sau giữ lại + xung đột sửa đồng thời.
	v1 := r1.id
	st, _, c = send(mk("correct", plot1, rect(0, 0, 30, 100), &v1), dev1, "dev:"+dev1.String()+":2", "Đo lại bằng GPS ngoài đồng")
	if st != "accepted" {
		t.Fatalf("sửa ngoại tuyến 1: %s %s", st, c)
	}
	late := mk("correct", plot1, rect(0, 0, 35, 100), &v1)
	st, cid, c = send(late, dev2, "dev:"+dev2.String()+":2", "Máy 2 sửa trên bản cũ")
	if st != "conflict" || cid == nil {
		t.Fatalf("sửa đồng thời: %s %v %s", st, cid, c)
	}
	r = f.national().appDo(http.MethodGet, "/v1/plots/"+plot1.String(), nil)
	vs := list(r.body, "versions")
	if v, _ := get(r.body, "plot", "properties", "version").(float64); v != 2 || len(vs) != 3 || get(vs[2], "became_current") != false ||
		str(vs[2], "boundary_id") != late.id.String() {
		t.Fatalf("sửa đồng thời phải giữ cả hai, bản muộn không thành hiện hành: %s", r.raw)
	}

	// Máy đã xác nhận giữ nguyên chồng lấn cùng HTX (confirm_overlap) → accepted, xung đột vẫn mở.
	cp, cpl, _, _ := boundaryRecord("create", ids.New(), "31117", f.coopA, rect(40, 50, 100, 100), nil)
	var m map[string]any
	_ = json.Unmarshal(cpl, &m)
	m["confirm_overlap"] = true
	cpl, _ = json.Marshal(m)
	if res := snk.Accept(ctx, scA, syncRecord(cp, "dev:"+dev2.String()+":3", "create", cpl, nil, nil, dev2)); res.Status != "accepted" {
		t.Fatalf("cùng HTX đã xác nhận: %+v", res)
	}
	// HTX khác vẽ đè ngoại tuyến: không chặn được máy → giữ cả hai, conflict cross_coop_overlap.
	xp, xpl, _, _ := boundaryRecord("create", ids.New(), "31117", f.coopB, rect(0, 0, 50, 50), nil)
	res := snk.Accept(ctx, scA, syncRecord(xp, "dev:"+dev2.String()+":4", "create", xpl, nil, nil, dev2))
	if res.Status != "conflict" || res.ConflictID == nil {
		t.Fatalf("khác HTX ngoại tuyến: %+v", res)
	}
	r = f.national().appDo(http.MethodGet, "/v1/geo/conflicts?status=open&limit=200", nil)
	kinds := map[string]int{}
	for _, it := range list(r.body, "items") {
		kinds[str(it, "kind")]++
	}
	if kinds["cross_coop_overlap"] != 1 {
		t.Fatalf("xung đột khác HTX: %v", kinds)
	}

	// Dữ liệu sai → rejected kèm mã.
	bad := mk("create", ids.New(), map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.6, 10.05}, {105.601, 10.05}, {105.602, 10.05}}}}, nil)
	if st, _, c := send(bad, dev1, "dev:"+dev1.String()+":3", ""); st != "rejected" || c != "geo.too_few_points" {
		t.Fatalf("3 điểm thẳng hàng: %s %s", st, c)
	}
	// Ngoài phạm vi → rejected.
	out, p, _, _ := boundaryRecord("create", ids.New(), "31120", uuid.Nil, rect(0, 900, 50, 50), nil)
	res = snk.Accept(ctx, scA, syncRecord(out, "dev:"+dev1.String()+":4", "create", p, nil, nil, dev1))
	if res.Status != "rejected" || res.Code != "geo.out_of_scope" {
		t.Fatalf("ngoài phạm vi: %+v", res)
	}
}

// KN US-PLOT-02, GIS FN-05: nhập GeoJSON/KML, kết quả theo từng đối tượng; chỉ WGS84; .shp rời bị từ chối.
func TestImportGeoJSONAndKML(t *testing.T) {
	f := newFixture(t)
	adm := f.national()
	dLat, dLon := degPerMetre(baseLat)
	feat := func(name string, geom map[string]any) map[string]any {
		return map[string]any{"type": "Feature", "properties": map[string]any{"name": name, "commune_code": "31117"}, "geometry": geom}
	}
	fc, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": []any{
		feat("Thửa 1", rect(0, 0, 100, 100)),
		feat("Thửa 3 điểm thẳng hàng", map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{baseLon, baseLat}, {baseLon + 50*dLon, baseLat}, {baseLon + 100*dLon, baseLat}}}}),
		feat("Thửa đè thửa 1", rect(30, 30, 100, 100)),
		map[string]any{"type": "Feature", "properties": map[string]any{"name": "Cọc"}, "geometry": map[string]any{"type": "Point", "coordinates": []float64{baseLon, baseLat}}},
	}})
	r := adm.adminPost("/v1/geo/plots/import?file_name=thua.geojson", "application/geo+json", fc)
	expect(t, r, http.StatusOK, "nhập GeoJSON")
	fs := list(r.body, "features")
	if get(r.body, "accepted") != 1.0 || get(r.body, "rejected") != 3.0 || len(fs) != 4 {
		t.Fatalf("kết quả nhập: %s", r.raw)
	}
	for i, want := range []string{"", "geo.too_few_points", "geo.overlap_needs_confirm", "geo.invalid_geometry"} {
		if got := str(fs[i], "code"); got != want {
			t.Fatalf("đối tượng %d: mã %q, muốn %q (%s)", i+1, got, want, r.raw)
		}
	}
	if str(fs[0], "plot_ref") == "" || str(fs[2], "reason") == "" {
		t.Fatalf("thiếu plot_ref/lý do: %s", r.raw)
	}

	x0, y0 := baseLon+1000*dLon, baseLat
	kml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><kml xmlns="http://www.opengis.net/kml/2.2"><Document>
<Placemark><name>Thửa KML</name><Polygon><outerBoundaryIs><LinearRing><coordinates>%f,%f %f,%f %f,%f %f,%f %f,%f</coordinates></LinearRing></outerBoundaryIs></Polygon></Placemark>
</Document></kml>`, x0, y0, x0+100*dLon, y0, x0+100*dLon, y0+100*dLat, x0, y0+100*dLat, x0, y0)
	r = adm.adminPost("/v1/geo/plots/import?format=kml&commune_code=31117&cooperative_ref="+f.coopA.String(), "application/vnd.google-earth.kml+xml", []byte(kml))
	expect(t, r, http.StatusOK, "nhập KML")
	if get(r.body, "accepted") != 1.0 || str(r.body, "format") != "kml" {
		t.Fatalf("nhập KML: %s", r.raw)
	}
	pref := str(list(r.body, "features")[0], "plot_ref")
	pr := adm.appDo(http.MethodGet, "/v1/plots/"+pref, nil)
	if str(pr.body, "plot", "properties", "source") != "import_file" || str(pr.body, "plot", "properties", "cooperative_ref") != f.coopA.String() {
		t.Fatalf("thửa từ KML: %s", pr.raw)
	}

	r = adm.adminPost("/v1/geo/plots/import?file_name=ranh_gioi.shp", "application/octet-stream", []byte{0x00, 0x00, 0x27, 0x0a, 0x00})
	if r.status != http.StatusUnprocessableEntity || code(r) != "geo.format_invalid" {
		t.Fatalf("Shapefile .shp rời (không phải gói .zip): %d %s", r.status, r.raw)
	}
	utm, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "crs": map[string]any{"type": "name", "properties": map[string]any{"name": "EPSG:32648"}}, "features": []any{}})
	r = adm.adminPost("/v1/geo/plots/import", "application/geo+json", utm)
	if r.status != http.StatusUnprocessableEntity || code(r) != "geo.crs_unsupported" {
		t.Fatalf("UTM: %d %s", r.status, r.raw)
	}
	// Admin API chỉ cho vai geo_admin.
	r = f.officerA().adminPost("/v1/geo/plots/import", "application/geo+json", fc)
	expect(t, r, http.StatusForbidden, "cán bộ xã gọi Admin API nhập tệp")
}

// GIS FN-07 BR-26/27: nhập ranh giới xã (xem trước rồi áp dụng); thửa phải nằm trong địa giới xã.
func TestAdminBoundaryAndCommuneContainment(t *testing.T) {
	f := newFixture(t)
	adm := f.national()
	commune := rect(-500, -500, 2000, 2000)
	fc, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": []any{
		map[string]any{"type": "Feature", "properties": map[string]any{"code": "31117"}, "geometry": commune},
		map[string]any{"type": "Feature", "properties": map[string]any{"code": "00000"}, "geometry": commune},
	}})
	r := adm.adminPost("/v1/geo/admin-boundaries/import?dry_run=true&source=CloudMap%202026-09", "application/geo+json", fc)
	expect(t, r, http.StatusOK, "xem trước")
	if get(r.body, "dry_run") != true || get(r.body, "accepted") != 1.0 || get(r.body, "rejected") != 1.0 {
		t.Fatalf("xem trước: %s", r.raw)
	}
	expect(t, adm.appDo(http.MethodGet, "/v1/geo/admin-boundaries/31117", nil), http.StatusNotFound, "xem trước không áp dụng")
	r = adm.adminPost("/v1/geo/admin-boundaries/import?source=CloudMap%202026-09", "application/geo+json", fc)
	expect(t, r, http.StatusOK, "áp dụng")
	r = adm.appDo(http.MethodGet, "/v1/geo/admin-boundaries/31117", nil)
	if r.status != http.StatusOK || str(r.body, "properties", "name") != "Xã Thới Lai" {
		t.Fatalf("ranh giới xã: %d %s", r.status, r.raw)
	}

	adm.createPlot("31117", uuid.Nil, rect(0, 0, 100, 100))
	r = adm.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "geometry": rect(1450, 0, 100, 100)})
	if r.status != http.StatusUnprocessableEntity || code(r) != "geo.outside_commune" {
		t.Fatalf("thửa ra ngoài xã: %d %s", r.status, r.raw)
	}
	// Nhập lại: hình cũ được đóng (không xóa), đánh dấu replaced.
	r = adm.adminPost("/v1/geo/admin-boundaries/import?source=CloudMap%202026-10", "application/geo+json", fc)
	if fs := list(r.body, "features"); len(fs) != 2 || get(fs[0], "replaced") != true {
		t.Fatalf("nhập lại: %s", r.raw)
	}
	expect(t, adm.adminPost("/v1/geo/admin-boundaries/import", "application/geo+json", fc), http.StatusUnprocessableEntity, "thiếu nguồn")
}

func syncRecord(id uuid.UUID, idem, op string, payload []byte, sup *uuid.UUID, reason *string, dev uuid.UUID) syncport.Record {
	return syncport.Record{
		ID: id, IdemKey: idem, Entity: "plot_boundary", Op: op, EventTime: time.Now().Add(-time.Hour),
		SupersedesID: sup, CorrectionReason: reason, DeviceID: dev, Payload: payload,
	}
}
