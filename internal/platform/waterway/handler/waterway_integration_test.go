package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// GIS US-WATER-02 AC-1/AC-2: vẽ tuyến mới; chiều dài do hệ thống tính (không nhận
// từ client); thiếu tên/phân loại hoặc số đo ≤ 0/sai định dạng bị chặn với đúng thông báo.
func TestGISUSWater02CreateSegmentValidation(t *testing.T) {
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor)

	body := segBody("Kênh Xà No", "draft", "40", 3.5, line(10.00, 105.00, 10.00, 105.01))
	body["length_m"] = "1" // bị bỏ qua
	r := ed.adm(http.MethodPost, "/v1/waterway/segments", body)
	expect(t, r, 201, "")
	length, _ := decimal.NewFromString(r.str("length_m"))
	if length.LessThan(decimal.NewFromInt(1090)) || length.GreaterThan(decimal.NewFromInt(1100)) {
		t.Fatalf("chiều dài geodesic ~1096 m do CSDL tính, nhận %s", r.str("length_m"))
	}
	if r.num("revision") != 1 || r.str("status") != "draft" || r.str("width_m") != "40" || r.str("depth_m") != "3.5" {
		t.Fatalf("bản ghi: %v", r.body)
	}
	if r.hdr.Get("Location") != "/v1/waterway/segments/"+r.str("segment_ref") {
		t.Fatalf("Location: %v", r.hdr)
	}

	missing := segBody("", "draft", "40", "3", line(10, 105, 10, 105.01))
	missing["waterway_class"] = ""
	r = ed.adm(http.MethodPost, "/v1/waterway/segments", missing)
	expect(t, r, 422, "validation_failed")
	if r.fieldReason("name") != domain.MsgNameClassRequired || r.fieldReason("waterway_class") != domain.MsgNameClassRequired {
		t.Fatalf("AC-2 thiếu tên/phân loại: %v", r.body)
	}
	for _, bad := range []any{"0", -2, "abc"} {
		r = ed.adm(http.MethodPost, "/v1/waterway/segments", segBody("Kênh B", "draft", bad, "3", line(10, 105, 10, 105.01)))
		expect(t, r, 422, "validation_failed")
		if r.fieldReason("width_m") != domain.MsgPositiveNumbers {
			t.Fatalf("AC-2 số đo %v: %v", bad, r.body)
		}
	}
	r = ed.adm(http.MethodPost, "/v1/waterway/segments", segBody("Kênh C", "draft", "4", "3", line(10, 105)))
	expect(t, r, 422, "validation_failed")

	// Không có vai waterway_editor → 403; không MFA → 403 ở Admin API.
	viewer := f.as(f.tenant, port.RolePublisher)
	expect(t, viewer.adm(http.MethodPost, "/v1/waterway/segments", segBody("Kênh D", "draft", "4", "3", line(10, 105, 10, 105.01))), 403, "forbidden")
	noMFA := f.iss.Sign(t, authztestToken(ids.New(), f.tenant, port.RoleEditor))
	c := caller{f: f, tid: f.tenant, token: noMFA}
	expect(t, c.adm(http.MethodGet, "/v1/waterway/segments", nil), 403, "mfa_required")
}

// GIS US-WATER-03 + RB-1: sửa tuyến là bản ghi bù trỏ về bản trước, có lý do; sửa
// trên bản cũ bị chặn; sổ chỉ ghi thêm (UPDATE/DELETE bị chặn).
func TestGISUSWater03ReviseByCompensatingRecord(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor)
	ref := ed.segment("Kênh Mới", "draft", "30", "3", line(10.00, 105.00, 10.00, 105.01))

	rev := segBody("Kênh Mới (kéo dài)", "confirmed", "30", "3", line(10.00, 105.00, 10.001, 105.005, 10.00, 105.02))
	rev["expected_revision"] = 1
	r := ed.adm(http.MethodPost, "/v1/waterway/segments/"+ref+"/revisions", rev)
	expect(t, r, 422, "validation_failed") // thiếu lý do
	rev["reason"] = "Kéo dài theo khảo sát 09/2026"
	r = ed.adm(http.MethodPost, "/v1/waterway/segments/"+ref+"/revisions", rev)
	expect(t, r, 201, "")
	if r.num("revision") != 2 || r.str("supersedes_id") == "" || r.str("correction_reason") == "" || r.str("status") != "confirmed" {
		t.Fatalf("bản sửa: %v", r.body)
	}
	l2, _ := decimal.NewFromString(r.str("length_m"))
	if l2.LessThan(decimal.NewFromInt(2100)) {
		t.Fatalf("chiều dài phải tính lại theo hình học mới: %s", r.str("length_m"))
	}
	// Sửa trên bản cũ → 409.
	expect(t, ed.adm(http.MethodPost, "/v1/waterway/segments/"+ref+"/revisions", rev), 409, "waterway.revision_conflict")
	expect(t, ed.adm(http.MethodPost, "/v1/waterway/segments/"+ids.New().String()+"/revisions", rev), 404, "waterway.segment_not_found")

	d := ed.adm(http.MethodGet, "/v1/waterway/segments/"+ref, nil)
	expect(t, d, 200, "")
	hist, _ := d.body["history"].([]any)
	if len(hist) != 2 {
		t.Fatalf("lịch sử phải giữ cả hai bản: %v", d.body)
	}
	// Sổ chỉ ghi thêm.
	for _, sql := range []string{"UPDATE pf_waterway.segment_revision SET name = 'x'", "DELETE FROM pf_waterway.segment_revision"} {
		err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err })
		if err == nil {
			t.Fatalf("%s phải bị chặn", sql)
		}
		if ledgerErr := ledger.Classify(err); ledgerErr == nil {
			t.Fatal("lỗi rỗng")
		}
	}
	var ok bool
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		res, err := ledger.Verify(ctx, tx, "pf_waterway.segment_revision", f.tenant, "segment:"+ref)
		ok = res.OK && res.RowsChecked == 2
		return err
	}); err != nil || !ok {
		t.Fatalf("chuỗi băm của đoạn: %v %v", ok, err)
	}
}

// POST lặp cùng Idempotency-Key → một bản ghi, phản hồi phát lại (R-51).
func TestIdempotentPostReplay(t *testing.T) {
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor)
	k := "k-" + ids.New().String()
	body := segBody("Kênh Lặp", "confirmed", "30", "3", line(10, 105, 10, 105.01))
	a := ed.do(f.admin.URL, http.MethodPost, "/v1/waterway/segments", k, body)
	b := ed.do(f.admin.URL, http.MethodPost, "/v1/waterway/segments", k, body)
	expect(t, a, 201, "")
	if b.status != 201 || b.str("segment_ref") != a.str("segment_ref") || b.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("phát lại: %d %v", b.status, b.body)
	}
	if n := f.count(f.tenant, "SELECT count(*) FROM pf_waterway.segment_revision"); n != 1 {
		t.Fatalf("mong 1 bản ghi, có %d", n)
	}
	kb := "k-" + ids.New().String()
	j1 := ed.do(f.admin.URL, http.MethodPost, "/v1/waterway/networks/builds", kb, nil)
	j2 := ed.do(f.admin.URL, http.MethodPost, "/v1/waterway/networks/builds", kb, nil)
	expect(t, j1, 202, "")
	if j2.status != 202 || j2.str("id") != j1.str("id") || j1.hdr.Get("Location") != "/v1/waterway/networks/builds/"+j1.str("id") {
		t.Fatalf("yêu cầu dựng lặp: %d %v", j2.status, j2.body)
	}
	if n := f.count(f.tenant, "SELECT count(*) FROM pf_waterway.build_job"); n != 1 {
		t.Fatalf("mong 1 job, có %d", n)
	}
}

// mạng mẫu (GIS US-WATER-01, prototype tests/waterway.test.ts): từ A(14.00,106.00) tới
// B(14.20,106.00) có đường tắt hẹp (chỉ ghe) và đường vòng rộng (sà lan 1.000 t);
// một đoạn nháp rộng nối thẳng A–B không được vào đồ thị; một tuyến cắt ngang đường vòng.
type network struct{ narrow, wide, draft, cross string }

func (c caller) seedNetwork() network {
	var n network
	n.narrow = c.segment("Tắt hẹp", "confirmed", "12", "2.0", line(14.00, 106.00, 14.10, 106.00, 14.20, 106.00))
	n.wide = c.segment("Vòng rộng", "confirmed", "60", "5", line(14.00, 106.00, 14.10, 106.12, 14.20, 106.00))
	n.draft = c.segment("Nháp thẳng", "draft", "100", "6", line(14.00, 106.001, 14.20, 106.001))
	n.cross = c.segment("Ngang", "confirmed", "40", "4", line(14.05, 106.03, 14.05, 106.09))
	return n
}

func routeKm(t *testing.T, r resp) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(r.str("distance_km"), 64)
	if err != nil {
		t.Fatalf("distance_km: %v", r.body)
	}
	return v
}

func refs(r resp) []string {
	raw, _ := r.body["segment_refs"].([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(string))
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// GIS US-WATER-01 + mục 16: dựng bất đồng bộ (202 + job, worker, giao lặp an toàn),
// tuyến nháp không vào đồ thị, phát hành cần waterway_publisher khác người dựng,
// lớp bản đồ đánh dấu tuyến phù hợp tải trọng, định tuyến lọc tải trọng.
func TestGISUSWater01BuildPublishRoute(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor, port.RolePublisher)
	n := ed.seedNetwork()

	// Chưa phát hành: định tuyến rơi về Haversine hiệu chỉnh, nói rõ lý do.
	user := f.as(f.tenant)
	r := user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0")
	expect(t, r, 200, "")
	if r.body["found"] != false || r.str("source") != port.SourceHaversine || r.body["version_no"] != nil || r.str("reason") == "" {
		t.Fatalf("chưa có mạng: %v", r.body)
	}

	no, st := ed.build()
	if no != 1 || st.body["reused"] != false {
		t.Fatalf("phiên bản đầu: %v", st.body)
	}
	// Worker giao lặp → không tạo phiên bản thứ hai.
	env := f.requestedEvent(f.tenant, st.str("id"))
	env.EventID = ids.New()
	if _, err := inboxProcess(ctx, f, env); err != nil {
		t.Fatal(err)
	}
	if c := f.count(f.tenant, "SELECT count(*) FROM pf_waterway.network_version"); c != 1 {
		t.Fatalf("mong 1 phiên bản, có %d", c)
	}
	// Tuyến nháp không vào đồ thị.
	if c := f.count(f.tenant, "SELECT count(*) FROM pf_waterway.edge WHERE segment_ref = $1", uuid.MustParse(n.draft)); c != 0 {
		t.Fatalf("tuyến nháp vào đồ thị: %d cạnh", c)
	}
	v := ed.adm(http.MethodGet, "/v1/waterway/networks/1", nil)
	expect(t, v, 200, "")
	if v.str("status") != "built" || v.num("segment_count") != 3 || v.num("component_count") != 1 || len(v.str("graph_sha256")) != 64 {
		t.Fatalf("phiên bản: %v", v.body)
	}
	fd := ed.adm(http.MethodGet, "/v1/waterway/networks/1/findings?kind=crossing", nil)
	expect(t, fd, 200, "")
	if items := fd.items(); len(items) != 1 || items[0]["auto_fixed"] != true || len(items[0]["segment_refs"].([]any)) != 2 {
		t.Fatalf("tuyến Ngang cắt đường vòng giữa đoạn → chèn đỉnh: %v", fd.body)
	}

	// Chưa phát hành → Application API chưa thấy.
	lay := user.app(http.MethodGet, "/v1/waterway/segments")
	if lay.body["version_no"] != nil || len(lay.items()) != 0 {
		t.Fatalf("chưa phát hành mà lộ lớp: %v", lay.body)
	}
	// R-56: người dựng không tự phát hành; thiếu vai publisher → 403.
	expect(t, ed.adm(http.MethodPost, "/v1/waterway/networks/1/publish", nil), 403, "self_approval_forbidden")
	expect(t, f.as(f.tenant, port.RoleEditor).adm(http.MethodPost, "/v1/waterway/networks/1/publish", nil), 403, "forbidden")
	pub := f.as(f.tenant, port.RolePublisher)
	p := pub.adm(http.MethodPost, "/v1/waterway/networks/1/publish", nil)
	expect(t, p, 200, "")
	if p.str("status") != "published" || p.str("published_by") != pub.actor.String() || p.str("effective_from") == "" {
		t.Fatalf("phát hành: %v", p.body)
	}
	expect(t, pub.adm(http.MethodPost, "/v1/waterway/networks/1/publish", nil), 409, "waterway.version_not_publishable")
	if c := f.count(f.tenant, "SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND (payload->>'version_no')::int = 1", port.EventNetworkPublished); c != 1 {
		t.Fatalf("sự kiện phát hành: %d", c)
	}

	// Lớp bản đồ: sà lan 1.000 t chỉ đánh dấu đường vòng (và tuyến Ngang? 40 m / 4 m → qua được).
	lay = user.app(http.MethodGet, "/v1/waterway/segments?min_load_tons=1000&bbox=105.9,13.9,106.2,14.3")
	expect(t, lay, 200, "")
	passable := map[string]bool{}
	for _, it := range lay.items() {
		passable[it["segment_ref"].(string)] = it["passable"].(bool)
		if it["segment_ref"] == n.draft {
			t.Fatal("tuyến nháp lộ ra lớp bản đồ")
		}
	}
	if len(passable) != 3 || passable[n.narrow] || !passable[n.wide] || !passable[n.cross] || lay.num("passable_count") != 2 || lay.body["message"] != nil {
		t.Fatalf("đánh dấu tuyến phù hợp (AC-1): %v", lay.body)
	}
	// AC-2: tải trọng vượt mọi tuyến → không đánh dấu tuyến nào, có thông báo.
	none := user.app(http.MethodGet, "/v1/waterway/segments?min_load_tons=5000")
	if none.num("passable_count") != 0 || none.str("message") == "" {
		t.Fatalf("AC-2: %v", none.body)
	}
	for _, it := range none.items() {
		if it["passable"] == true {
			t.Fatalf("AC-2 không được đánh dấu: %v", it)
		}
	}

	// Định tuyến: ghe đi đường tắt; sà lan 1.000 t phải đi vòng, không đi qua tuyến hẹp;
	// ràng buộc bất lợi nhất được nêu.
	ghe := user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&min_load_tons=100")
	barge := user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&min_load_tons=1000")
	expect(t, ghe, 200, "")
	expect(t, barge, 200, "")
	if ghe.body["found"] != true || barge.body["found"] != true || ghe.num("version_no") != 1 {
		t.Fatalf("định tuyến: %v / %v", ghe.body, barge.body)
	}
	if gk, bk := routeKm(t, ghe), routeKm(t, barge); gk > 23 || bk < 30 || gk >= bk {
		t.Fatalf("ghe %.3f km phải ngắn hơn sà lan %.3f km (đường nháp không được dùng)", gk, bk)
	}
	if !contains(refs(ghe), n.narrow) || contains(refs(barge), n.narrow) || !contains(refs(barge), n.wide) {
		t.Fatalf("tuyến đi qua: ghe %v, sà lan %v", refs(ghe), refs(barge))
	}
	lim, _ := ghe.body["limiting"].(map[string]any)
	if lim == nil || lim["max_load_tons"] != "100" || lim["segment_ref"] != n.narrow || lim["reason"] != domain.DimWidth {
		t.Fatalf("ràng buộc bất lợi nhất của đường ghe: %v", ghe.body["limiting"])
	}
	if ghe.str("source") != port.SourceRouted || ghe.str("source_label") == "" {
		t.Fatalf("nhãn nguồn: %v", ghe.body)
	}
	geom, _ := ghe.body["geometry"].(map[string]any)
	if coords, _ := geom["coordinates"].([]any); len(coords) < 2 {
		t.Fatalf("phải trả hình học đường đi: %v", ghe.body)
	}
	// Không phương tiện nào đủ → Haversine hiệu chỉnh kèm lý do.
	huge := user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&min_load_tons=3000")
	if huge.body["found"] != false || huge.str("source") != port.SourceHaversine || huge.str("reason") == "" {
		t.Fatalf("không đủ tải trọng: %v", huge.body)
	}
	expect(t, user.app(http.MethodGet, "/v1/waterway/routes?from=abc&to=14.2,106.0"), 422, "validation_failed")

	// Dựng lại khi không có gì đổi → tái dùng phiên bản (tất định, cùng băm).
	no2, st2 := ed.build()
	if no2 != 1 || st2.body["reused"] != true {
		t.Fatalf("dựng lại không đổi: %v", st2.body)
	}
}

// Mục 16: định tuyến ghim phiên bản cũ vẫn chạy sau khi phát hành phiên bản mới;
// chỉ MỘT phiên bản có hiệu lực; phiên bản chưa phát hành không định tuyến được.
func TestVersionPinningAfterNewPublish(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor)
	pub := f.as(f.tenant, port.RolePublisher)
	n := ed.seedNetwork()
	no, _ := ed.build()
	expect(t, pub.adm(http.MethodPost, fmt.Sprintf("/v1/waterway/networks/%d/publish", no), nil), 200, "")
	user := f.as(f.tenant)
	v1 := user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&min_load_tons=100")
	km1 := routeKm(t, v1)

	// Ngừng dùng đường tắt (bản ghi bù), dựng và phát hành phiên bản 2.
	rev := segBody("Tắt hẹp", "retired", "12", "2.0", line(14.00, 106.00, 14.10, 106.00, 14.20, 106.00))
	rev["expected_revision"], rev["reason"] = 1, "Bồi lắng, ngừng khai thác"
	expect(t, ed.adm(http.MethodPost, "/v1/waterway/segments/"+n.narrow+"/revisions", rev), 201, "")
	no2, _ := ed.build()
	if no2 != 2 {
		t.Fatalf("mong phiên bản 2, nhận %d", no2)
	}
	expect(t, pub.adm(http.MethodPost, "/v1/waterway/networks/2/publish", nil), 200, "")
	old := ed.adm(http.MethodGet, "/v1/waterway/networks/1", nil)
	if old.str("status") != "superseded" || old.str("superseded_at") == "" {
		t.Fatalf("phiên bản 1 phải chuyển superseded và vẫn giữ: %v", old.body)
	}
	if c := f.count(f.tenant, "SELECT count(*) FROM pf_waterway.network_version WHERE status = 'published'"); c != 1 {
		t.Fatalf("chỉ một phiên bản có hiệu lực, có %d", c)
	}

	cur := user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&min_load_tons=100")
	pinned := user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&min_load_tons=100&version=1")
	if cur.num("version_no") != 2 || routeKm(t, cur) <= km1 || contains(refs(cur), n.narrow) {
		t.Fatalf("phiên bản 2 không còn đường tắt: %v", cur.body)
	}
	if pinned.num("version_no") != 1 || routeKm(t, pinned) != km1 || !contains(refs(pinned), n.narrow) {
		t.Fatalf("ghim phiên bản 1 phải cho đúng kết quả cũ: %v", pinned.body)
	}
	// Cổng Router cho pf/simulation: ghim phiên bản như Application API.
	rctx := tenant.WithID(ctx, f.tenant)
	rt, err := f.mod.Router().RouteBetween(rctx, []port.LatLng{{Lat: 14.0, Lng: 106.0}, {Lat: 14.2, Lng: 106.0}}, decimal.NewFromInt(100), 1)
	if err != nil || rt.VersionNo != 1 || !rt.Found || rt.DistanceKm.String() != strconv.FormatFloat(km1, 'f', -1, 64) {
		t.Fatalf("Router ghim phiên bản 1: %+v %v", rt, err)
	}
	if cv, err := f.mod.Router().CurrentVersion(rctx); err != nil || cv != 2 {
		t.Fatalf("CurrentVersion: %d %v", cv, err)
	}
	// Phiên bản không có / chưa phát hành → 404.
	expect(t, user.app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&version=9"), 404, "waterway.network_version_not_found")
	ed.segment("Nhánh mới", "confirmed", "20", "2", line(14.20, 106.00, 14.30, 106.00))
	no3, _ := ed.build()
	expect(t, user.app(http.MethodGet, fmt.Sprintf("/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0&version=%d", no3)), 404, "waterway.network_version_not_found")
	if _, err := f.mod.Router().RouteBetween(rctx, []port.LatLng{{Lat: 14.0, Lng: 106.0}, {Lat: 14.2, Lng: 106.0}}, decimal.Zero, no3); err == nil {
		t.Fatal("Router không được định tuyến phiên bản chưa phát hành")
	}
	// Đồ thị đã dựng bất biến.
	err = tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE pf_waterway.edge SET cost_m = 1")
		return err
	})
	if err == nil {
		t.Fatal("UPDATE cạnh phải bị chặn")
	}
}

// GIS BR-37: công trình vượt sông ràng buộc cạnh chứa nó — cầu thấp chặn sà lan;
// công trình đặt xa tuyến bị từ chối; ràng buộc bất lợi nhất nêu mã công trình.
func TestStructureConstrainsRoute(t *testing.T) {
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor)
	pub := f.as(f.tenant, port.RolePublisher)
	wide := ed.segment("Sông lớn", "confirmed", "200", "8", line(10.10, 105.10, 10.20, 105.20))

	far := map[string]any{"kind": "cau", "name": "Cầu lạc", "segment_ref": wide, "location": map[string]any{"lat": 9.0, "lng": 104.0},
		"clearance_height_m": "5"}
	expect(t, ed.adm(http.MethodPost, "/v1/waterway/structures", far), 422, "waterway.structure_off_segment")
	bridge := map[string]any{"kind": "cau", "name": "Cầu thấp", "segment_ref": wide, "location": map[string]any{"lat": 10.15, "lng": 105.15},
		"clearance_height_m": "5", "clearance_width_m": "40", "depth_m": "6", "survey_date": "2026-09-01"}
	br := ed.adm(http.MethodPost, "/v1/waterway/structures", bridge)
	expect(t, br, 201, "")

	no, _ := ed.build()
	expect(t, pub.adm(http.MethodPost, fmt.Sprintf("/v1/waterway/networks/%d/publish", no), nil), 200, "")
	user := f.as(f.tenant)
	lay := user.app(http.MethodGet, "/v1/waterway/segments")
	it := lay.items()
	if len(it) != 1 || it[0]["max_load_tons"] != "100" || it[0]["limiting"] != domain.DimClearance || it[0]["limiting_structure_ref"] != br.str("structure_ref") {
		t.Fatalf("cầu thấp hạ tuyến xuống mức ghe: %v", lay.body)
	}
	r := user.app(http.MethodGet, "/v1/waterway/routes?from=10.10,105.10&to=10.20,105.20&min_load_tons=1000")
	if r.body["found"] != false {
		t.Fatalf("sà lan 1.000 t không qua cầu 5 m: %v", r.body)
	}
	g := user.app(http.MethodGet, "/v1/waterway/routes?from=10.10,105.10&to=10.20,105.20&min_load_tons=100")
	lim, _ := g.body["limiting"].(map[string]any)
	if g.body["found"] != true || lim["structure_ref"] != br.str("structure_ref") {
		t.Fatalf("ràng buộc phải nêu công trình: %v", g.body)
	}

	// Ngừng dùng cây cầu (bản ghi bù) → dựng lại: sà lan 2.000 t qua được.
	bridge["status"], bridge["expected_revision"], bridge["reason"] = "retired", 1, "Cầu đã tháo dỡ"
	expect(t, ed.adm(http.MethodPost, "/v1/waterway/structures/"+br.str("structure_ref")+"/revisions", bridge), 201, "")
	no2, _ := ed.build()
	expect(t, pub.adm(http.MethodPost, fmt.Sprintf("/v1/waterway/networks/%d/publish", no2), nil), 200, "")
	lay = user.app(http.MethodGet, "/v1/waterway/segments")
	if it := lay.items(); it[0]["max_load_tons"] != "2000" {
		t.Fatalf("bỏ cầu phải nới ràng buộc: %v", lay.body)
	}
	list := ed.adm(http.MethodGet, "/v1/waterway/structures?segment_ref="+wide, nil)
	if items := list.items(); len(items) != 1 || items[0]["status"] != "retired" || items[0]["revision"] != float64(2) {
		t.Fatalf("danh sách công trình: %v", list.body)
	}
}

// Cách ly tenant (RB-2): tenant khác không thấy đoạn, job, phiên bản hay lớp bản đồ.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor)
	pub := f.as(f.tenant, port.RolePublisher)
	ref := ed.segment("Kênh tenant A", "confirmed", "30", "3", line(10, 105, 10, 105.01))
	no, st := ed.build()
	expect(t, pub.adm(http.MethodPost, fmt.Sprintf("/v1/waterway/networks/%d/publish", no), nil), 200, "")

	other := ids.New()
	ed2 := f.as(other, port.RoleEditor, port.RolePublisher)
	expect(t, ed2.adm(http.MethodGet, "/v1/waterway/segments/"+ref, nil), 404, "waterway.segment_not_found")
	if items := ed2.adm(http.MethodGet, "/v1/waterway/segments", nil).items(); len(items) != 0 {
		t.Fatalf("tenant khác thấy đoạn: %v", items)
	}
	expect(t, ed2.adm(http.MethodGet, "/v1/waterway/networks/builds/"+st.str("id"), nil), 404, "waterway.build_not_found")
	expect(t, ed2.adm(http.MethodGet, "/v1/waterway/networks/1", nil), 404, "waterway.network_version_not_found")
	expect(t, ed2.adm(http.MethodPost, "/v1/waterway/networks/1/publish", nil), 404, "waterway.network_version_not_found")
	u2 := f.as(other)
	lay := u2.app(http.MethodGet, "/v1/waterway/segments")
	if lay.body["version_no"] != nil || len(lay.items()) != 0 {
		t.Fatalf("tenant khác thấy lớp: %v", lay.body)
	}
	expect(t, u2.app(http.MethodGet, "/v1/waterway/networks/current"), 404, "waterway.network_version_not_found")
	expect(t, f.as(f.tenant).app(http.MethodGet, "/v1/waterway/networks/current"), 200, "")
	// Revise đoạn của tenant khác → không thấy.
	rev := segBody("x", "confirmed", "30", "3", line(10, 105, 10, 105.01))
	rev["expected_revision"], rev["reason"] = 1, "thử sửa chéo tenant"
	expect(t, ed2.adm(http.MethodPost, "/v1/waterway/segments/"+ref+"/revisions", rev), 404, "waterway.segment_not_found")
}
