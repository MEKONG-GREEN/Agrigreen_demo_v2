package acceptance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Thửa ruộng và quy tắc chồng lấn (pf/geo).

const bboxNK = "/v1/plots?bbox=105.75,10.01,105.80,10.06"

func props(r harness.Resp) map[string]any { return obj(obj(r.Body, "plot"), "properties") }

// polygon dựng Polygon GeoJSON từ các đỉnh [lon, lat] (không tự khép vòng).
func polygon(pts ...[]float64) map[string]any {
	return map[string]any{"type": "Polygon", "coordinates": [][][]float64{pts}}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Thửa cần tối thiểu 4 điểm; hệ thống tự tính diện tích"
// và "UAT DEF-KN-PLOT-01: … tam giác có điểm đóng vòng vẫn là 3 điểm";
// htx-operations.test.ts — "Đường bao dưới 3 đỉnh bị từ chối".
// Khác prototype (htx-operations): mốc tối thiểu là 4 điểm theo BRD (KN US-PLOT-01), không phải 3.
func TestPrototypeThuaToiThieuBonDiem(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Thửa cần tối thiểu 4 điểm; hệ thống tự tính diện tích")
	nguon(t, "htx-operations.test.ts", "Đường bao dưới 3 đỉnh bị từ chối")
	w := newWorld(t)
	post := func(geom map[string]any) harness.Resp {
		return w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{"commune_code": communeNK, "geometry": geom})
	}
	triClosed := polygon([]float64{105.77, 10.03}, []float64{105.771, 10.03}, []float64{105.77, 10.031}, []float64{105.77, 10.03})
	wantProblem(t, post(triClosed), 422, "geo.too_few_points", "tam giác có điểm đóng vòng")
	two := polygon([]float64{105.77, 10.03}, []float64{105.771, 10.03})
	if r := post(two); r.Status != 422 || (r.Code() != "geo.too_few_points" && r.Code() != "geo.invalid_geometry") {
		t.Fatalf("đường bao 2 đỉnh phải bị từ chối 422: %d %s", r.Status, trim(r.Raw))
	}
	// Hình vuông 4 đỉnh + điểm đóng vòng trùng đỉnh đầu: hợp lệ, diện tích tự tính ≈ 1 ha.
	r := w.want(w.plot(w.officer, w.coopNK, 0, 0, 100, 100), 201, "thửa 4 điểm")
	area := dec(t, props(r)["area_ha"])
	if area.LessThan(decimal.RequireFromString("0.95")) || area.GreaterThan(decimal.RequireFromString("1.05")) {
		t.Fatalf("diện tích tự tính phải ≈ 1 ha, nhận %s", area)
	}
	if n := num(obj(r.Body, "boundary"), "point_count"); n != 4 {
		t.Fatalf("điểm đóng vòng không được tính là điểm thứ 5: %v", n)
	}
}

// Nguồn: mekong-green/tests/htx-operations.test.ts — "Diện tích thửa ruộng do hệ thống TÍNH từ đường bao, không nhận từ người dùng".
// Khác prototype: diện tích khai báo (declared_area_ha) được nhận để ĐỐI CHIẾU — lệch > 5 % thì cần
// xác nhận (HTX US-PLOT-02 AC-3), nhưng diện tích lưu vẫn là diện tích đo từ đường bao.
func TestPrototypeDienTichDoHeThongTinh(t *testing.T) {
	nguon(t, "htx-operations.test.ts", "Diện tích thửa ruộng do hệ thống TÍNH từ đường bao, không nhận từ người dùng")
	w := newWorld(t)
	body := func(east float64, extra map[string]any) map[string]any {
		b := map[string]any{"commune_code": communeNK, "cooperative_ref": w.coopNK, "geometry": harness.Rect(nkLat, nkLon, east, 0, 100, 100)}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	r := w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", body(0, map[string]any{"area_ha": "9999"})), 201, "gửi kèm area_ha giả")
	if a := dec(t, props(r)["area_ha"]); a.GreaterThan(decimal.RequireFromString("1.05")) {
		t.Fatalf("area_ha do người dùng gửi phải bị bỏ qua, nhận %s", a)
	}
	dev := wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", body(300, map[string]any{"declared_area_ha": "1.5"})),
		409, "geo.area_deviation_needs_confirm", "lệch diện tích khai báo")
	if dec(t, dev.Body["deviation_pct"]).LessThan(decimal.NewFromInt(5)) {
		t.Fatalf("phải báo mức lệch > 5 %%: %s", trim(dev.Raw))
	}
	ok := w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", body(300, map[string]any{"declared_area_ha": "1.5", "confirm_overlap": true})), 201, "xác nhận lệch diện tích")
	if a := dec(t, props(ok)["area_ha"]); a.GreaterThan(decimal.RequireFromString("1.05")) {
		t.Fatalf("diện tích lưu phải là diện tích đo, nhận %s", a)
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-KN-PLOT-01: đa giác tự cắt (nơ bướm) và điểm đóng vòng trùng đầu bị từ chối".
func TestPrototypeThuaTuCatBiTuChoi(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-KN-PLOT-01: đa giác tự cắt (nơ bướm) và điểm đóng vòng trùng đầu bị từ chối")
	w := newWorld(t)
	o := []float64{nkLon, nkLat}
	k := 0.003
	bowtie := polygon(o, []float64{o[0] + k, o[1] + k}, []float64{o[0] + k, o[1]}, []float64{o[0], o[1] + k}, o)
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{"commune_code": communeNK, "geometry": bowtie}),
		422, "geo.invalid_geometry", "hình nơ bướm tự cắt")
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Chồng lấn cùng HTX → cần xác nhận rồi mới lưu; chồng lấn HTX khác → chặn hẳn".
// Khác prototype: lưu sau xác nhận vẫn mở một xung đột same_coop_overlap chờ xem xét (cờ needs_review,
// SLA 72 giờ) thay vì chỉ lưu im lặng.
func TestPrototypeChongLanCungHTXVaKhacHTX(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Chồng lấn cùng HTX → cần xác nhận rồi mới lưu; chồng lấn HTX khác → chặn hẳn")
	w := newWorld(t)
	a := str(props(w.want(w.plot(w.officer, w.coopNK, 0, 0, 100, 100), 201, "thửa A")), "ref")
	overlap := func(coop string, confirm bool) harness.Resp {
		return w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{
			"commune_code": communeNK, "cooperative_ref": coop, "confirm_overlap": confirm,
			"geometry": harness.Rect(nkLat, nkLon, 50, 0, 100, 100),
		})
	}
	r := wantProblem(t, overlap(w.coopNK, false), 409, "geo.overlap_needs_confirm", "chồng lấn cùng HTX chưa xác nhận")
	if ov := list(r.Body, "overlaps"); r.Body["needs_confirm"] != true || len(ov) != 1 || str(ov[0], "plot_ref") != a {
		t.Fatalf("phải trả danh sách thửa bị chồng và cờ needs_confirm: %s", trim(r.Raw))
	}
	saved := w.want(overlap(w.coopNK, true), 201, "chồng lấn cùng HTX đã xác nhận")
	cs := list(saved.Body, "conflicts")
	if len(cs) != 1 || str(cs[0], "kind") != "same_coop_overlap" || props(saved)["needs_review"] != true {
		t.Fatalf("lưu sau xác nhận phải mở xung đột chờ xem xét: %s", trim(saved.Raw))
	}
	for _, confirm := range []bool{false, true} {
		wantProblem(t, overlap(w.coopNK2, confirm), 409, "geo.overlap_cross_coop", "chồng lấn HTX khác (kể cả khi xác nhận)")
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-KN-PLOT-02: hai thửa cắt nhau mà không nuốt đỉnh hay tâm của nhau vẫn bị phát hiện chồng lấn"
// và "UAT DEF-HTX-01/02" (kiểm trước khi lưu).
// Khác prototype: thửa liền kề chung cạnh (phần giao < ngưỡng geo.overlap_tolerance_pct = 1 %) không bị coi là chồng lấn.
func TestPrototypeChongLanQuaGiaoCanh(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-KN-PLOT-02: hai thửa cắt nhau mà không nuốt đỉnh hay tâm của nhau vẫn bị phát hiện chồng lấn")
	w := newWorld(t)
	// A rộng-thấp, B cao-hẹp: không đỉnh/tâm nào nằm trong thửa kia, chỉ các cạnh cắt nhau.
	rectA := harness.Rect(nkLat, nkLon, 0, 1000, 400, 80)
	rectB := harness.Rect(nkLat, nkLon, 250, 920, 100, 400)
	a := str(props(w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{
		"commune_code": communeNK, "cooperative_ref": w.coopNK, "geometry": rectA,
	}), 201, "thửa A")), "ref")
	check := func(coop string) harness.Resp {
		return w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/plots/overlap-check", "", map[string]any{
			"commune_code": communeNK, "cooperative_ref": coop, "geometry": rectB,
		}), 200, "kiểm chồng lấn trước khi lưu")
	}
	other := check(w.coopNK2)
	if other.Body["can_save"] != false || !refsOf(list(other.Body, "blocking"), "plot_ref")[a] {
		t.Fatalf("HTX khác: phải chặn và chỉ ra thửa A: %s", trim(other.Raw))
	}
	same := check(w.coopNK)
	if same.Body["requires_confirm"] != true || !refsOf(list(same.Body, "needs_confirm"), "plot_ref")[a] {
		t.Fatalf("cùng HTX: phải cần xác nhận: %s", trim(same.Raw))
	}
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{
		"commune_code": communeNK, "cooperative_ref": w.coopNK2, "geometry": rectB, "confirm_overlap": true,
	}), 409, "geo.overlap_cross_coop", "lưu B cho HTX khác")
	// Liền kề chung cạnh trái của A (HTX khác): nhận, không cờ.
	adj := w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{
		"commune_code": communeNK, "cooperative_ref": w.coopNK2, "geometry": harness.Rect(nkLat, nkLon, -100, 1000, 100, 80),
	}), 201, "thửa liền kề")
	if len(list(adj.Body, "flagged_overlaps")) != 0 || props(adj)["needs_review"] != false {
		t.Fatalf("thửa liền kề không được bị cờ chồng lấn: %s", trim(adj.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Xoá mềm ẩn thửa khỏi danh sách vận hành nhưng khôi phục được, có lý do và lịch sử".
// Khác prototype: G1 không xóa — "ngừng dùng" (retire) là sự kiện mới trong sổ kèm lý do ≥ 10 ký tự;
// thửa ẩn khỏi bản đồ và không còn chặn chồng lấn, lịch sử giữ nguyên. Khôi phục cũng là sự kiện mới
// ("restored", lý do ≥ 10 ký tự) và kiểm lại chồng lấn: thửa khác đã vẽ đè trong lúc ngừng dùng thì bị chặn.
func TestPrototypeNgungDungThuaCoLyDo(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Xoá mềm ẩn thửa khỏi danh sách vận hành nhưng khôi phục được, có lý do và lịch sử")
	w := newWorld(t)
	ref := str(props(w.want(w.plot(w.officer, w.coopNK, 0, 0, 100, 100), 201, "thửa")), "ref")
	path := "/v1/plots/" + ref + "/retire"
	wantFieldError(t, w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"reason": "nhầm"}), "reason", "lý do quá ngắn")
	r := w.want(w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"reason": "Nhập nhầm ranh giới"}), 200, "ngừng dùng thửa")
	if str(props(r), "status") != "retired" || str(obj(r.Body, "event"), "action") != "retired" || str(obj(r.Body, "event"), "reason") != "Nhập nhầm ranh giới" {
		t.Fatalf("sự kiện ngừng dùng phải ghi lý do: %s", trim(r.Raw))
	}
	if refsOf(w.want(w.provAdmin.Do(harness.App, http.MethodGet, bboxNK, "", nil), 200, "bản đồ").Items("features"), "id")[ref] {
		t.Fatal("thửa đã ngừng dùng không được còn trên bản đồ vận hành")
	}
	got := w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/plots/"+ref, "", nil), 200, "tra cứu thửa đã ngừng")
	if str(props(got), "status") != "retired" || str(props(got), "status_reason") != "Nhập nhầm ranh giới" || len(list(got.Body, "versions")) != 1 {
		t.Fatalf("thửa đã ngừng vẫn tra cứu được kèm lý do và lịch sử: %s", trim(got.Raw))
	}
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"reason": "Ngừng dùng lần thứ hai"}), 409, "geo.plot_state_conflict", "ngừng dùng lặp")
	// Khôi phục: bắt buộc lý do; thửa chưa ngừng dùng thì không khôi phục được.
	restore := "/v1/plots/" + ref + "/restore"
	wantFieldError(t, w.officer.Do(harness.App, http.MethodPost, restore, "", map[string]any{"reason": "nhầm"}), "reason", "lý do khôi phục quá ngắn")
	back := w.want(w.officer.Do(harness.App, http.MethodPost, restore, "", map[string]any{"reason": "Ngừng dùng nhầm thửa, khôi phục"}), 200, "khôi phục thửa")
	if str(props(back), "status") != "active" || str(obj(back.Body, "event"), "action") != "restored" {
		t.Fatalf("khôi phục phải đưa thửa về hoạt động bằng sự kiện restored: %s", trim(back.Raw))
	}
	if !refsOf(w.want(w.provAdmin.Do(harness.App, http.MethodGet, bboxNK, "", nil), 200, "bản đồ").Items("features"), "id")[ref] {
		t.Fatal("thửa đã khôi phục phải hiện lại trên bản đồ")
	}
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, restore, "", map[string]any{"reason": "Khôi phục lần thứ hai"}), 409, "geo.plot_state_conflict", "khôi phục thửa đang hoạt động")

	// Ngừng dùng lại → thửa đã ngừng không còn chặn chồng lấn với HTX khác …
	w.want(w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"reason": "Ngừng dùng lần hai sau khôi phục"}), 200, "ngừng dùng lại")
	w.want(w.plot(w.officer, w.coopNK2, 0, 0, 100, 100), 201, "vẽ lại đúng chỗ cho HTX khác")
	// … và khi đã có thửa HTX khác đè lên thì không khôi phục được (chồng lấn khác HTX luôn chặn).
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, restore, "", map[string]any{"reason": "Khôi phục khi đã bị vẽ đè", "confirm_overlap": true}),
		409, "geo.overlap_cross_coop", "khôi phục thửa đã bị HTX khác vẽ đè")
}

// Nguồn: mekong-green/tests/htx-operations.test.ts — "Vẽ lại đường bao thì diện tích tính lại và báo mức chênh lệch".
// Khác prototype: vẽ lại = phiên bản ranh giới mới trỏ về phiên bản cũ (supersedes_id), bắt buộc lý do;
// phiên bản cũ giữ nguyên nên mức chênh lệch tính được từ lịch sử phiên bản.
func TestPrototypeVeLaiRanhGioiTinhLaiDienTich(t *testing.T) {
	nguon(t, "htx-operations.test.ts", "Vẽ lại đường bao thì diện tích tính lại và báo mức chênh lệch")
	w := newWorld(t)
	r := w.want(w.plot(w.officer, w.coopNK, 0, 0, 100, 100), 201, "thửa")
	ref, v1 := str(props(r), "ref"), str(obj(r.Body, "boundary"), "boundary_id")
	path := "/v1/plots/" + ref + "/boundaries"
	small := harness.Rect(nkLat, nkLon, 0, 0, 100, 40)
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"supersedes_id": v1, "geometry": small}), 422, "validation_failed", "vẽ lại thiếu lý do")
	c := w.want(w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{
		"supersedes_id": v1, "correction_reason": "Đo lại bằng GPS ngoài đồng", "geometry": small,
	}), 201, "vẽ lại ranh giới")
	if num(obj(c.Body, "boundary"), "version") != 2 || str(obj(c.Body, "boundary"), "supersedes_id") != v1 {
		t.Fatalf("phải tạo phiên bản 2 trỏ về phiên bản 1: %s", trim(c.Raw))
	}
	got := w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/plots/"+ref, "", nil), 200, "đọc thửa")
	vs := list(got.Body, "versions")
	if len(vs) != 2 {
		t.Fatalf("phải giữ cả 2 phiên bản: %s", trim(got.Raw))
	}
	before, after := dec(t, vs[0]["area_ha"]), dec(t, vs[1]["area_ha"])
	deltaPct := after.Sub(before).Div(before).Mul(decimal.NewFromInt(100))
	if !after.Equal(dec(t, props(got)["area_ha"])) || deltaPct.GreaterThan(decimal.NewFromInt(-50)) {
		t.Fatalf("diện tích tính lại (%s → %s, %s %%) phải giảm hơn một nửa", before, after, deltaPct.StringFixed(1))
	}
}

// Nguồn: mekong-green/tests/htx-operations.test.ts — "Khuyến nông vẽ thửa rồi gán cho HTX và thành viên".
// Khác prototype: gán là sự kiện "assigned" trong sổ thửa; gán nông hộ bắt buộc đã có HTX.
// Giống prototype: nông hộ phải là thành viên của đúng HTX được gán ("Không gán được thửa cho thành viên
// của HTX khác") → 422 geo.household_invalid.
func TestPrototypeGanThuaChoHTXVaNongHo(t *testing.T) {
	nguon(t, "htx-operations.test.ts", "Khuyến nông vẽ thửa rồi gán cho HTX và thành viên")
	w := newWorld(t)
	hh := w.household(w.officer, w.coopNK, "Nguyễn Văn Tám", "0999000131")
	r := w.want(w.plot(w.officer, "", 0, 0, 100, 100), 201, "cán bộ KN vẽ thửa chưa gán")
	p := props(r)
	if p["cooperative_ref"] != nil || p["household_ref"] != nil || str(p, "source") != "kn_web" {
		t.Fatalf("thửa mới vẽ chưa gán chủ, ghi nguồn: %v", p)
	}
	path := "/v1/plots/" + str(p, "ref") + "/assign"
	wantFieldError(t, w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"household_ref": hh}), "household_ref", "gán nông hộ khi chưa có HTX")
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"cooperative_ref": w.coopNK2, "household_ref": hh}),
		422, "geo.household_invalid", "gán thửa cho HTX này với nông hộ của HTX khác")
	a := w.want(w.officer.Do(harness.App, http.MethodPost, path, "", map[string]any{"cooperative_ref": w.coopNK, "household_ref": hh}), 200, "gán HTX và nông hộ")
	if str(props(a), "cooperative_ref") != w.coopNK || str(props(a), "household_ref") != hh || str(obj(a.Body, "event"), "action") != "assigned" {
		t.Fatalf("gán thửa: %s", trim(a.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-HTX-01/02: nạp thửa từ tệp cũng phải ≥ 4 điểm và bị chặn/cần xác nhận khi chồng lấn"
// và "Đọc GeoJSON và KML thành danh sách đối tượng; Shapefile bị từ chối kèm hướng dẫn".
// Khác prototype: nhập tệp chỉ qua Admin API (MFA, vai geo_admin); kết quả theo từng đối tượng.
func TestPrototypeNapThuaTuTep(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-HTX-01/02: nạp thửa từ tệp cũng phải ≥ 4 điểm và bị chặn/cần xác nhận khi chồng lấn")
	nguon(t, "brd-2026.test.ts", "Đọc GeoJSON và KML thành danh sách đối tượng; Shapefile bị từ chối kèm hướng dẫn")
	// Khác prototype: Shapefile (.zip, .prj WGS84) nay được nhận; chỉ hệ toạ độ khác EPSG:4326 bị từ chối kèm hướng dẫn.
	w := newWorld(t)
	adm := w.user(w.orgNational, true, "geo_admin", "plot_viewer")
	feat := func(name string, geom map[string]any) map[string]any {
		return map[string]any{"type": "Feature", "properties": map[string]any{"name": name, "commune_code": communeNK}, "geometry": geom}
	}
	upload := func(query, ctype string, body []byte) harness.Resp {
		h := http.Header{"Content-Type": {ctype}, "Idempotency-Key": {harness.NewIdem()}}
		return adm.DoRaw(harness.Admin, http.MethodPost, "/v1/geo/plots/import?"+query, h, bytes.NewReader(body))
	}
	fc := func(fs ...any) []byte {
		b, err := json.Marshal(map[string]any{"type": "FeatureCollection", "features": fs})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	tri := polygon([]float64{nkLon, nkLat + 0.01}, []float64{nkLon + 0.001, nkLat + 0.01}, []float64{nkLon, nkLat + 0.011}, []float64{nkLon, nkLat + 0.01})
	q := "file_name=thua.geojson&cooperative_ref=" + w.coopNK
	r := w.want(upload(q, "application/geo+json", fc(
		feat("Vuông 1", harness.Rect(nkLat, nkLon, 0, 0, 100, 100)),
		feat("Tam giác", tri),
		feat("Vuông trùng", harness.Rect(nkLat, nkLon, 30, 30, 100, 100)),
	)), 200, "nạp GeoJSON")
	fs := list(r.Body, "features")
	if num(r.Body, "accepted") != 1 || len(fs) != 3 {
		t.Fatalf("chỉ thửa vuông 1 được nhận: %s", trim(r.Raw))
	}
	for i, code := range []string{"", "geo.too_few_points", "geo.overlap_needs_confirm"} {
		if str(fs[i], "code") != code {
			t.Fatalf("đối tượng %d: mã %q, muốn %q (%s)", i+1, str(fs[i], "code"), code, trim(r.Raw))
		}
	}
	confirmed := w.want(upload(q+"&confirm_overlap=true", "application/geo+json", fc(feat("Vuông trùng", harness.Rect(nkLat, nkLon, 30, 30, 100, 100)))), 200, "nạp lại có xác nhận")
	if num(confirmed.Body, "accepted")+num(confirmed.Body, "flagged") < 1 || num(confirmed.Body, "rejected") != 0 {
		t.Fatalf("chồng lấn cùng HTX đã xác nhận phải được lưu: %s", trim(confirmed.Raw))
	}
	foreign := w.want(upload("file_name=thua.geojson&confirm_overlap=true&cooperative_ref="+w.coopNK2, "application/geo+json",
		fc(feat("Của HTX khác", harness.Rect(nkLat, nkLon, 10, 10, 50, 50)))), 200, "nạp thửa của HTX khác")
	if num(foreign.Body, "rejected") != 1 || str(list(foreign.Body, "features")[0], "code") != "geo.overlap_cross_coop" {
		t.Fatalf("chồng lấn HTX khác luôn bị chặn: %s", trim(foreign.Raw))
	}
	// Shapefile (.zip kèm .prj WGS84) được nhận như GeoJSON; hệ toạ độ khác (VN-2000 UTM) bị từ chối
	// kèm hướng dẫn chuyển về EPSG:4326; .shp rời (không nén .zip) không đọc được.
	ring := func(east, north, side float64) [][2]float64 {
		var out [][2]float64
		for _, p := range harness.Rect(nkLat, nkLon, east, north, side, side)["coordinates"].([][][]float64)[0] {
			out = append(out, [2]float64{p[0], p[1]})
		}
		return out
	}
	fields := []string{"TEN_THUA", "MA_XA"}
	shpFeats := []shpFeature{{Ring: ring(600, 600, 80), Attrs: map[string]string{"TEN_THUA": "Thua Shapefile", "MA_XA": communeNK}}}
	shp := w.want(upload("file_name=thua_vu_dx.zip&cooperative_ref="+w.coopNK, "application/zip", shapefileZip(t, fields, shpFeats, prjWGS84)), 200, "nạp Shapefile WGS84")
	if num(shp.Body, "accepted") != 1 || num(shp.Body, "rejected") != 0 {
		t.Fatalf("Shapefile WGS84 phải được nhận: %s", trim(shp.Raw))
	}
	wantProblem(t, upload("file_name=thua_vn2000.zip&cooperative_ref="+w.coopNK, "application/zip", shapefileZip(t, fields, shpFeats, prjVN2000UTM48)),
		422, "geo.crs_unsupported", "Shapefile VN-2000 UTM")
	wantProblem(t, upload("file_name=ranh_gioi.shp", "application/octet-stream", []byte{0x00, 0x00, 0x27, 0x0a, 0x00}), 422, "geo.format_invalid", ".shp rời (phải nén .zip)")
}
