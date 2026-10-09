package handler_test

// KN v7.7 FN-XA-03.05 "Vẽ ranh giới thửa ruộng": route /v1/kn/plots… theo ngữ cảnh phiên KN (X-AG-Context) và đồng bộ
// ngoại tuyến plot_boundary / plot của người gọi có ngữ cảnh KN. Ngữ cảnh phiên P1 giả lập bằng middleware gắn
// authz.KNContext theo header (như tests/kn77 knMiddleware).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port/farmerid"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const knDept = "trong_trot"

type knFix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	mod    plotMod
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
}

func newKNFix(t *testing.T) *knFix {
	env := pgtest.Get(t)
	f := &knFix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	ops := env.Pool(t, migrations.PF, "platform_api")
	tol := decimal.NewFromInt(1)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	f.mod = plotMod{svc: &app.Service{DB: shareddb.ForModule(pool, coopDB), Clock: clock.System{}, Scope: newFakeScope(), Params: fakeParams{tolerance: &tol},
		Units: newFakeUnits(), AdminGeom: adminGeometry{db: shareddb.ForModule(pool, geoDB)}}}
	f.mod.UseKN(farmerid.New(clienttest.New()), nil)
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	f.mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

// knMiddleware giả lập middleware X-AG-Context của P1.
func (f *knFix) knMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(authz.ContextHeader)
		if raw == "" {
			c.Next()
			return
		}
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

type knUser struct {
	f     *knFix
	tid   uuid.UUID
	actor uuid.UUID
	token string
	kc    *authz.KNContext
	noCtx bool
}

// user tạo người dùng KN (vai realm kn_user) với ngữ cảnh vai/phòng ban/xã.
func (f *knFix) user(tid uuid.UUID, role, dept string, units ...string) knUser {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: dept}
	switch role {
	case authz.KNXa:
		kc.Communes, kc.Scope = units, authz.Scope{Units: units}
		kc.OrgRef = ids.New() // như P1: lần gán Cấp Xã luôn có Nút (db.ForModule đòi Nút — FD)
	case authz.KNTW, authz.KNAdmin:
		kc.Scope = authz.Scope{All: true}
	default:
		kc.Scope = authz.Scope{Units: units}
	}
	f.mu.Lock()
	f.ctxs[kc.ID] = kc
	f.mu.Unlock()
	return knUser{f: f, tid: tid, actor: actor, token: tok, kc: kc}
}

func (u knUser) req(method, path, idem string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	if !u.noCtx {
		r.Header.Set(authz.ContextHeader, u.kc.ID.String())
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
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: res.StatusCode, hdr: res.Header, raw: raw, body: m}
}

func (u knUser) post(path string, body any) resp { return u.req(http.MethodPost, path, key(), body) }
func (u knUser) get(path string) resp            { return u.req(http.MethodGet, path, "", nil) }

// ctx là ngữ cảnh pf/sync của người dùng (Principal có KN).
func (u knUser) ctx() context.Context {
	cp := *u.kc
	p := authz.Principal{ActorRef: u.actor, TenantID: u.tid, Roles: []string{"kn_user"}, KN: &cp}
	return tenant.WithID(authz.WithPrincipal(context.Background(), p), u.tid)
}

func wantCode(t *testing.T, r resp, status int, c, what string) {
	t.Helper()
	if r.status != status || (c != "" && code(r) != c) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, c, r.status, r.raw)
	}
}

func knPlotBody(commune string, geom map[string]any) map[string]any {
	return map[string]any{"commune_code": commune, "lot_name": "Lô bờ kênh", "farmer_name": "Nguyễn Văn An", "note": "Lúa OM18",
		"declared_area_ha": "1.0", "shape": "square", "geometry": geom}
}

func TestFN_XA_03_05_CreateListGetUpdateCorrectLog(t *testing.T) {
	f := newKNFix(t)
	xa := f.user(f.tenant, authz.KNXa, knDept, "31117")
	k := key()
	r := xa.req(http.MethodPost, "/v1/kn/plots", k, knPlotBody("31117", rect(0, 0, 100, 100)))
	wantCode(t, r, http.StatusCreated, "", "vẽ lô mới")
	ref := str(r.body, "ref")
	if str(r.body, "lot_name") != "Lô bờ kênh" || str(r.body, "shape") != "square" || str(r.body, "department_code") != knDept {
		t.Fatalf("thuộc tính v7.7: %s", r.raw)
	}
	label := str(r.body, "farmer", "label")
	if label == "" || strings.Contains(string(r.raw), "Nguyễn Văn An") {
		t.Fatalf("tên nông dân phải được che (RB-3): %s", r.raw)
	}
	if a, _ := decimal.NewFromString(str(r.body, "area_ha")); a.Sub(decimal.NewFromInt(1)).Abs().GreaterThan(decimal.RequireFromString("0.01")) {
		t.Fatalf("diện tích tự tính ≈ 1 ha: %s", r.raw)
	}
	if str(r.body, "area_deviation_pct") == "" {
		t.Fatalf("phải đối chiếu diện tích khai báo: %s", r.raw)
	}
	// Idempotency: cùng khóa → cùng thửa.
	r2 := xa.req(http.MethodPost, "/v1/kn/plots", k, knPlotBody("31117", rect(0, 0, 100, 100)))
	if r2.status/100 != 2 || str(r2.body, "ref") != ref {
		t.Fatalf("POST lặp: %d %s", r2.status, r2.raw)
	}
	lst := xa.get("/v1/kn/plots?commune=31117")
	wantCode(t, lst, http.StatusOK, "", "danh sách")
	if items := list(lst.body, "items"); len(items) != 1 || str(items[0], "ref") != ref || get(items[0], "geometry") != nil {
		t.Fatalf("danh sách (không hình học): %s", lst.raw)
	}
	g := xa.get("/v1/kn/plots/" + ref)
	wantCode(t, g, http.StatusOK, "", "chi tiết")
	if str(g.body, "geometry", "type") != "MultiPolygon" {
		t.Fatalf("chi tiết kèm hình: %s", g.raw)
	}
	// Sửa thuộc tính = sự kiện bù (Log cũ → mới).
	u := xa.req(http.MethodPut, "/v1/kn/plots/"+ref, key(), map[string]any{"note": "Lúa ST25", "reason": "Đổi giống theo vụ mới"})
	wantCode(t, u, http.StatusOK, "", "sửa thuộc tính")
	if str(u.body, "note") != "Lúa ST25" || str(u.body, "lot_name") != "Lô bờ kênh" {
		t.Fatalf("sau sửa: %s", u.raw)
	}
	// Sửa ranh giới: phiên bản mới thay phiên bản hiện hành.
	c := xa.post("/v1/kn/plots/"+ref+"/boundary", map[string]any{"supersedes_boundary_id": str(g.body, "boundary_id"),
		"geometry": rect(0, 0, 90, 100), "shape": "polygon", "reason": "Đo lại ranh giới ngoài đồng"})
	wantCode(t, c, http.StatusCreated, "", "sửa ranh giới")
	if str(c.body, "shape") != "polygon" || str(c.body, "boundary_id") == str(g.body, "boundary_id") {
		t.Fatalf("ranh giới mới: %s", c.raw)
	}
	stale := xa.post("/v1/kn/plots/"+ref+"/boundary", map[string]any{"supersedes_boundary_id": str(g.body, "boundary_id"),
		"geometry": rect(0, 0, 80, 100), "shape": "polygon", "reason": "Sửa trên phiên bản cũ đã bị thay"})
	wantCode(t, stale, http.StatusConflict, "geo.version_conflict", "sửa trên phiên bản cũ")
	lg := xa.get("/v1/kn/plots/" + ref + "/log")
	wantCode(t, lg, http.StatusOK, "", "log")
	events := map[string]map[string]any{}
	for _, it := range list(lg.body, "items") {
		events[str(it, "event")] = it.(map[string]any)
	}
	for _, e := range []string{"created", "attributes_changed", "boundary_recorded", "boundary_corrected"} {
		if events[e] == nil {
			t.Fatalf("log thiếu %s: %s", e, lg.raw)
		}
	}
	ch := list(events["attributes_changed"], "changes")
	if len(ch) != 1 || str(ch[0], "field") != "note" || str(ch[0], "old") != "Lúa OM18" || str(ch[0], "new") != "Lúa ST25" {
		t.Fatalf("log sửa thuộc tính: %v", ch)
	}
	if str(events["attributes_changed"], "reason") != "Đổi giống theo vụ mới" || str(events["boundary_corrected"], "reason") == "" {
		t.Fatalf("log thiếu lý do: %s", lg.raw)
	}
	if strings.Contains(string(lg.raw), "Nguyễn Văn An") {
		t.Fatal("log không được chứa tên nông dân")
	}
	// Xuất tệp của xã. A5 (03/10/2026): tải tệp thửa là xuất NỘI BỘ — qua điểm kiểm soát dùng chung (ghi egress_event)
	// nhưng không bị chặn kể cả enforce + checkpoint báo vượt hạn mức.
	cp := &denyCP{}
	f.mod.UseEgress(cp, meter.ModeEnforce, nil)
	defer f.mod.UseEgress(nil, "", nil)
	ex := xa.get("/v1/kn/plots/export?commune=31117&format=geojson")
	if len(cp.got) != 1 || !cp.got[0].Internal || cp.got[0].Channel != meter.ChannelReportExport || cp.got[0].Product != "kn_plot_file" ||
		cp.got[0].Records < 1 || cp.got[0].DataTenantID == uuid.Nil {
		t.Fatalf("A5: tải tệp thửa phải qua checkpoint với Internal: %+v", cp.got)
	}
	if ex.status != http.StatusOK || !strings.Contains(string(ex.raw), ref) || !strings.Contains(ex.hdr.Get("Content-Disposition"), ".geojson") {
		t.Fatalf("xuất GeoJSON: %d %s", ex.status, ex.raw)
	}
	km := xa.get("/v1/kn/plots/export?commune=31117&format=kml")
	if km.status != http.StatusOK || !strings.Contains(string(km.raw), "<Placemark>") || !strings.Contains(string(km.raw), "<Polygon>") {
		t.Fatalf("xuất KML: %d %s", km.status, km.raw)
	}
}

func TestFN_XA_03_05_OverlapBlockedAndInvalidShape(t *testing.T) {
	f := newKNFix(t)
	xa := f.user(f.tenant, authz.KNXa, knDept, "31117")
	wantCode(t, xa.post("/v1/kn/plots", knPlotBody("31117", rect(0, 0, 100, 100))), http.StatusCreated, "", "lô 1")
	// v7.7: chặn MỌI chồng lấn khi trực tuyến (kể cả cùng HTX/thửa nháp), kèm phần chồng lấn.
	ov := xa.post("/v1/kn/plots", knPlotBody("31117", rect(50, 0, 100, 100)))
	wantCode(t, ov, http.StatusConflict, "geo.plot_overlap", "chồng lấn")
	ols := list(ov.body, "overlaps")
	if len(ols) != 1 || str(ols[0], "geometry", "type") == "" || str(ols[0], "area_ha") == "" {
		t.Fatalf("thông tin chồng lấn: %s", ov.raw)
	}
	// < 3 điểm / không khép được → 422.
	two := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.6, 10.05}, {105.601, 10.05}, {105.6, 10.05}}}}
	wantCode(t, xa.post("/v1/kn/plots", knPlotBody("31117", two)), http.StatusUnprocessableEntity, "geo.too_few_points", "2 điểm")
	// A2 (BRD v7.7, chốt 03/10/2026): tam giác 3 điểm hợp lệ; 3 điểm thẳng hàng vẫn chặn.
	tri := rect(600, 0, 50, 50) // 3 góc đầu của hình vuông
	tri["coordinates"] = [][][]float64{tri["coordinates"].([][][]float64)[0][:3]}
	wantCode(t, xa.post("/v1/kn/plots", knPlotBody("31117", tri)), http.StatusCreated, "", "tam giác 3 điểm")
	col := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.6, 10.05}, {105.601, 10.05}, {105.602, 10.05}}}}
	wantCode(t, xa.post("/v1/kn/plots", knPlotBody("31117", col)), http.StatusUnprocessableEntity, "geo.too_few_points", "3 điểm thẳng hàng")
	line := map[string]any{"type": "LineString", "coordinates": [][]float64{{105.6, 10.05}, {105.601, 10.05}}}
	wantCode(t, xa.post("/v1/kn/plots", knPlotBody("31117", line)), http.StatusUnprocessableEntity, "geo.invalid_geometry", "không phải vùng khép kín")
	b := knPlotBody("31117", rect(400, 0, 50, 50))
	b["note"] = "Gọi 0912345678"
	wantCode(t, xa.post("/v1/kn/plots", b), http.StatusUnprocessableEntity, "validation_failed", "ghi chú có SĐT")
	b = knPlotBody("31117", rect(400, 0, 50, 50))
	b["shape"] = "triangle"
	wantCode(t, xa.post("/v1/kn/plots", b), http.StatusUnprocessableEntity, "validation_failed", "cách vẽ lạ")
	b = knPlotBody("31117", rect(400, 0, 50, 50))
	delete(b, "lot_name")
	wantCode(t, xa.post("/v1/kn/plots", b), http.StatusUnprocessableEntity, "validation_failed", "thiếu tên lô")
}

func TestFN_XA_03_05_ScopeRolesTenantDepartment(t *testing.T) {
	f := newKNFix(t)
	xa := f.user(f.tenant, authz.KNXa, knDept, "31117")
	r := xa.post("/v1/kn/plots", knPlotBody("31117", rect(0, 0, 100, 100)))
	wantCode(t, r, http.StatusCreated, "", "vẽ")
	ref := str(r.body, "ref")

	other := f.user(f.tenant, authz.KNXa, knDept, "31120")
	wantCode(t, other.get("/v1/kn/plots/"+ref), http.StatusNotFound, "geo.plot_not_found", "xã khác đọc")
	wantCode(t, other.get("/v1/kn/plots?commune=31117"), http.StatusForbidden, "geo.out_of_scope", "xã khác lọc")
	wantCode(t, other.post("/v1/kn/plots", knPlotBody("31117", rect(300, 0, 50, 50))), http.StatusForbidden, "geo.out_of_scope", "vẽ ngoài địa bàn")
	wantCode(t, other.req(http.MethodPut, "/v1/kn/plots/"+ref, key(), map[string]any{"note": "x"}), http.StatusNotFound, "geo.plot_not_found", "xã khác sửa")
	wantCode(t, other.get("/v1/kn/plots/export?commune=31117"), http.StatusForbidden, "geo.out_of_scope", "xuất xã khác")

	tinh := f.user(f.tenant, authz.KNTinh, knDept, "92", "31117", "31120")
	if l := tinh.get("/v1/kn/plots"); l.status != http.StatusOK || len(list(l.body, "items")) != 1 {
		t.Fatalf("Cấp Tỉnh xem thửa trong tỉnh: %d %s", l.status, l.raw)
	}
	wantCode(t, tinh.post("/v1/kn/plots", knPlotBody("31117", rect(300, 0, 50, 50))), http.StatusForbidden, "forbidden", "Cấp Tỉnh ghi")
	tw := f.user(f.tenant, authz.KNTW, knDept)
	wantCode(t, tw.get("/v1/kn/plots/"+ref), http.StatusOK, "", "TW xem")
	wantCode(t, tw.post("/v1/kn/plots", knPlotBody("31117", rect(300, 0, 50, 50))), http.StatusForbidden, "geo.read_only", "TW ghi")
	wantCode(t, tw.req(http.MethodPut, "/v1/kn/plots/"+ref, key(), map[string]any{"note": "x"}), http.StatusForbidden, "geo.read_only", "TW sửa")

	dept := f.user(f.tenant, authz.KNXa, "bvtv", "31117")
	wantCode(t, dept.get("/v1/kn/plots/"+ref), http.StatusNotFound, "geo.plot_not_found", "phòng ban khác")
	admin := f.user(f.tenant, authz.KNAdmin, "")
	wantCode(t, admin.get("/v1/kn/plots/"+ref), http.StatusOK, "", "Admin xem mọi phòng ban")

	stranger := f.user(ids.New(), authz.KNXa, knDept, "31117")
	wantCode(t, stranger.get("/v1/kn/plots/"+ref), http.StatusNotFound, "geo.plot_not_found", "tenant khác")
	if l := stranger.get("/v1/kn/plots"); len(list(l.body, "items")) != 0 {
		t.Fatalf("tenant khác thấy thửa: %s", l.raw)
	}
	noCtx := xa
	noCtx.noCtx = true
	wantCode(t, noCtx.get("/v1/kn/plots"), 428, "accounts.context_required", "thiếu X-AG-Context")
}

func TestFN_XA_03_05_OfflineSyncKNCaller(t *testing.T) {
	f := newKNFix(t)
	xa := f.user(f.tenant, authz.KNXa, knDept, "31117")
	snk, src := f.mod.SyncSink(), f.mod.SyncSource()
	scope := xa.kc.Scope
	dev := ids.New()
	plot := ids.New()
	payload, _ := json.Marshal(map[string]any{"plot_ref": plot.String(), "commune_code": "31117", "geometry": rect(0, 0, 100, 100),
		"lot_name": "Lô ven sông", "note": "Vẽ ngoại tuyến", "farmer_name": "Trần Thị Bình", "shape": "circle", "declared_area_ha": "1.0"})
	id := ids.New()
	rec := syncRecord(id, "dev:"+dev.String()+":1", "create", payload, nil, nil, dev)
	// Người dùng KN chỉ có vai realm kn_user: vẫn được đẩy nhờ ngữ cảnh kn_xa.
	if res := snk.Accept(xa.ctx(), scope, rec); res.Status != "accepted" {
		t.Fatalf("KN kn_xa đẩy ranh giới: %+v", res)
	}
	if res := snk.Accept(xa.ctx(), scope, rec); res.Status != "duplicate" {
		t.Fatalf("gửi lặp: %+v", res)
	}
	ch, _, err := src.Changes(xa.ctx(), scope, "plot", 0, 100)
	if err != nil || len(ch) != 1 || ch[0].ID != plot || ch[0].Op != "upsert" {
		t.Fatalf("pull: %v %+v", err, ch)
	}
	var data map[string]any
	_ = json.Unmarshal(ch[0].Data, &data)
	if str(data, "lot_name") != "Lô ven sông" || str(data, "note") != "Vẽ ngoại tuyến" || str(data, "shape") != "circle" ||
		str(data, "department_code") != knDept || str(data, "declared_area_ha") == "" || str(data, "farmer_label") == "" {
		t.Fatalf("dữ liệu pull v7.7: %s", ch[0].Data)
	}
	if strings.Contains(string(ch[0].Data), "Trần Thị Bình") {
		t.Fatalf("pull không được chứa tên đầy đủ: %s", ch[0].Data)
	}
	// Ngoại tuyến không chặn chồng lấn: giữ bản ghi, mở xung đột (máy ngoại tuyến không thấy nhau).
	p2, _ := json.Marshal(map[string]any{"plot_ref": ids.New().String(), "commune_code": "31117", "geometry": rect(50, 0, 100, 100),
		"lot_name": "Lô chồng", "shape": "polygon"})
	if res := snk.Accept(xa.ctx(), scope, syncRecord(ids.New(), "dev:"+dev.String()+":2", "create", p2, nil, nil, dev)); res.Status != "conflict" || res.ConflictID == nil {
		t.Fatalf("chồng lấn ngoại tuyến: %+v", res)
	}
	// Phòng ban khác: thửa đi xuống thành tombstone; TW không đẩy được; ngoài địa bàn bị từ chối.
	other := f.user(f.tenant, authz.KNXa, "bvtv", "31117")
	och, _, _ := src.Changes(other.ctx(), other.kc.Scope, "plot", 0, 100)
	for _, c := range och {
		if c.Op != "tombstone" {
			t.Fatalf("phòng ban khác nhận thửa: %+v", c)
		}
	}
	tw := f.user(f.tenant, authz.KNTW, knDept)
	p3, _ := json.Marshal(map[string]any{"plot_ref": ids.New().String(), "commune_code": "31117", "geometry": rect(800, 0, 50, 50), "lot_name": "Lô TW"})
	if res := snk.Accept(tw.ctx(), tw.kc.Scope, syncRecord(ids.New(), "dev:"+dev.String()+":3", "create", p3, nil, nil, dev)); res.Status != "rejected" || res.Code != "geo.read_only" {
		t.Fatalf("TW đẩy: %+v", res)
	}
	p4, _ := json.Marshal(map[string]any{"plot_ref": ids.New().String(), "commune_code": "31120", "geometry": rect(800, 0, 50, 50), "lot_name": "Lô xã khác"})
	if res := snk.Accept(xa.ctx(), scope, syncRecord(ids.New(), "dev:"+dev.String()+":4", "create", p4, nil, nil, dev)); res.Status != "rejected" || res.Code != "geo.out_of_scope" {
		t.Fatalf("ngoài địa bàn: %+v", res)
	}
	// Sửa ngoại tuyến (op correct) của KN: phiên bản mới, giữ thuộc tính v7.7.
	reason := "Đo lại bằng GPS ngoài đồng"
	cp, _ := json.Marshal(map[string]any{"geometry": rect(0, 0, 40, 100), "shape": "polygon"})
	if res := snk.Accept(xa.ctx(), scope, syncRecord(ids.New(), "dev:"+dev.String()+":5", "correct", cp, &id, &reason, dev)); res.Status != "accepted" {
		t.Fatalf("sửa ngoại tuyến: %+v", res)
	}
	g := xa.get("/v1/kn/plots/" + plot.String())
	if str(g.body, "lot_name") != "Lô ven sông" || str(g.body, "shape") != "polygon" || get(g.body, "version") != float64(2) {
		t.Fatalf("sau sửa ngoại tuyến: %s", g.raw)
	}
}
