package acceptance

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Cơ giới hóa (pf/mechanization) + định mức/ngưỡng là tham số có phiên bản của pf/masterdata.

type cgh struct {
	*world
	admin, viewer *harness.User
	phone         int
}

func newCGH(t *testing.T) *cgh {
	w := newWorld(t)
	return &cgh{
		world:  w,
		admin:  w.user(w.orgNational, true, "cgh_admin", "vault_registrar"),
		viewer: w.user(w.orgNational, false, "cgh_viewer"),
		phone:  999000500,
	}
}

func (c *cgh) post(path string, body any) harness.Resp {
	return c.admin.Do(harness.Admin, http.MethodPost, "/v1/mechanization"+path, "", body)
}

// owner tạo chủ sở hữu "Thành viên HTX" (SĐT tổng hợp dải 09990xxxxx, mỗi lần một số mới).
func (c *cgh) owner(coop string) map[string]any {
	c.t.Helper()
	c.phone++
	r := c.want(c.post("/owners", map[string]any{
		"owner_type": "thanh_vien_htx", "cooperative_ref": coop, "name": "Võ Văn Chủ Máy", "phone": fmt.Sprintf("0%d", c.phone),
	}), 201, "tạo chủ máy")
	return obj(r.Body, "owner")
}

func (c *cgh) machineBody(coop, ownerID, typ, serial, ownedSince string) map[string]any {
	return map[string]any{
		"cooperative_ref": coop, "machine_type": typ, "owner_id": ownerID, "brand": "Kubota", "model": "L4018",
		"serial_no": serial, "owned_since": ownedSince, "status": "Hoạt động", "year_made": 2021,
	}
}

func (c *cgh) machine(coop, ownerID, typ, serial, ownedSince string) map[string]any {
	c.t.Helper()
	r := c.want(c.post("/machines", c.machineBody(coop, ownerID, typ, serial, ownedSince)), 201, "tạo máy "+serial)
	return obj(r.Body, "machine")
}

func (c *cgh) area(coop string, year int, ha string) {
	c.t.Helper()
	c.want(c.post("/season-areas", map[string]any{"cooperative_ref": coop, "season_code": "DX", "season_year": year, "area_ha": ha, "source": "manual"}), 201, "diện tích vụ")
}

// balance chạy cân đối vụ DX năm year, trả run và kết quả MLD theo HTX.
func (c *cgh) balance(year int) (map[string]any, map[string]map[string]any) {
	c.t.Helper()
	run := obj(c.want(c.post("/balance-runs", map[string]any{"season_code": "DX", "season_year": year}), 201, "chạy cân đối").Body, "run")
	res := c.want(c.viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/balance-runs/"+str(run, "id")+"?machine_type=MLD", "", nil), 200, "đọc kết quả").Items("results")
	out := map[string]map[string]any{}
	for _, r := range res {
		out[str(r, "cooperative_ref")] = r
	}
	return run, out
}

// approveParam đề xuất → gửi duyệt → phê duyệt (người duyệt khác người đặt) một phiên bản tham số.
func (w *world) approveParam(key, value, from string) {
	w.t.Helper()
	setter := w.user("", true, "param_setter")
	approver := w.user("", true, "param_approver")
	v := w.want(setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions", "", map[string]any{
		"key": key, "value": value, "unit": "%", "effective_from": from, "source_doc": "QĐ thử nghiệm UAT số 01/2027",
	}), 201, "đề xuất "+key)
	id := str(v.Body, "id")
	w.want(setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/submit", "", nil), 200, "gửi duyệt "+key)
	w.want(approver.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", "", map[string]any{}), 200, "phê duyệt "+key)
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Chủ sở hữu: mã CSH-<tỉnh>-xxxxx tự sinh, Thành viên HTX phải gắn HTX".
// Khác prototype: tên/SĐT chủ máy nằm ở kho định danh (person_ref), trùng SĐT → 409 owner_duplicate.
func TestPrototypeChuSoHuuMaCSH(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Chủ sở hữu: mã CSH-<tỉnh>-xxxxx tự sinh, Thành viên HTX phải gắn HTX")
	c := newCGH(t)
	wantProblem(t, c.post("/owners", map[string]any{"owner_type": "thanh_vien_htx", "name": "Không HTX", "phone": "0999000501"}),
		422, "validation_failed", "Thành viên HTX không gắn HTX")
	o := c.owner(c.coopNK)
	if !regexp.MustCompile(`^CSH-[A-Z]{2,3}-\d{5}$`).MatchString(str(o, "code")) || str(o, "person_ref") == "" {
		t.Fatalf("mã CSH-<tỉnh>-xxxxx và person_ref ở kho định danh: %v", o)
	}
	wantProblem(t, c.post("/owners", map[string]any{"owner_type": "khac", "name": "Tên khác", "phone": "0999 000 501"}),
		409, "mechanization.owner_duplicate", "trùng SĐT chủ máy")
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Máy: mã MAY-<tỉnh>-xxxxx, bắt buộc SN hoặc số khung, ngày sở hữu không ở tương lai, SN không trùng".
func TestPrototypeMayMaMAY(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Máy: mã MAY-<tỉnh>-xxxxx, bắt buộc SN hoặc số khung, ngày sở hữu không ở tương lai, SN không trùng")
	c := newCGH(t)
	o := str(c.owner(c.coopNK), "id")
	noSN := c.machineBody(c.coopNK, o, "MLD", "", "2025-06-01")
	delete(noSN, "serial_no")
	wantProblem(t, c.post("/machines", noSN), 422, "validation_failed", "thiếu SN và số khung")
	wantProblem(t, c.post("/machines", c.machineBody(c.coopNK, o, "MLD", "SN-FUT", "2099-01-01")), 422, "validation_failed", "ngày sở hữu ở tương lai")
	m := c.machine(c.coopNK, o, "MLD", "SN-TEST-001", "2026-01-15")
	if !regexp.MustCompile(`^MAY-[A-Z]{2,3}-\d{5}$`).MatchString(str(m, "code")) || str(m, "farm_stage") != "lam_dat" {
		t.Fatalf("mã MAY-<tỉnh>-xxxxx, khâu tự gán theo chủng loại: %v", m)
	}
	o2 := str(c.owner(c.coopBT), "id")
	wantProblem(t, c.post("/machines", c.machineBody(c.coopBT, o2, "MLD", "sn-test-001", "2026-01-15")), 409, "mechanization.serial_taken", "SN trùng (khác HTX, khác hoa thường)")
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "BR-09: số máy của HTX tại ngày T chỉ tính máy sở hữu ≤ T và chưa vô hiệu hoá tại T".
// Khác prototype: vô hiệu hóa là sự kiện trong sổ máy; máy vô hiệu hóa ngày D không còn tính từ chính ngày D.
func TestPrototypeSoMayTaiNgayT(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "BR-09: số máy của HTX tại ngày T chỉ tính máy sở hữu ≤ T và chưa vô hiệu hoá tại T")
	c := newCGH(t)
	m := c.machine(c.coopNK, str(c.owner(c.coopNK), "id"), "MLD", "SN-BR09", "2026-01-15")
	count := func(at string) float64 {
		r := c.want(c.viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/cooperatives/"+c.coopNK+"/machine-count?at="+at, "", nil), 200, "số máy tại "+at)
		return num(r.Body, "total")
	}
	if count("2026-01-14") != 0 || count("2026-01-15") != 1 {
		t.Fatal("trước ngày sở hữu 15/01 không tính, từ ngày sở hữu thì tính")
	}
	path := "/machines/" + str(m, "ref") + "/deactivate"
	wantProblem(t, c.post(path, map[string]any{"deactivated_on": "2026-01-01", "reason": "Ngày sai trước sở hữu"}), 422, "validation_failed", "vô hiệu hóa trước ngày sở hữu")
	c.want(c.post(path, map[string]any{"deactivated_on": "2026-03-01", "reason": "Bán máy cho HTX khác"}), 200, "vô hiệu hóa máy")
	if count("2026-02-15") != 1 || count("2026-03-02") != 0 {
		t.Fatal("trước ngày vô hiệu vẫn tính cho vụ cũ, sau ngày vô hiệu không tính")
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Nhập máy hàng loạt: chế độ Cập nhật giữ máy cũ, chế độ Thay thế vô hiệu hoá máy không có trong tệp".
// Khác prototype: "Thay thế" vô hiệu hóa bằng sự kiện kèm ngày (không xóa); danh sách kể cả ngừng vẫn đủ máy.
func TestPrototypeNhapMayCapNhatVaThayThe(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Nhập máy hàng loạt: chế độ Cập nhật giữ máy cũ, chế độ Thay thế vô hiệu hoá máy không có trong tệp")
	c := newCGH(t)
	oc := str(c.owner(c.coopNK), "code")
	header := "Chủng loại,Hãng,Model,Năm SX,Nhiên liệu,Công suất,Năng suất,Mã CSH,Số máy,Số khung,Ngày sở hữu,Tình trạng\n"
	row := func(serial, brand string) string {
		return "MGS," + brand + ",X1,2022,Dầu,30,10," + oc + "," + serial + ",,01/02/2026,Hoạt động\n"
	}
	imp := func(mode, csv string) harness.Resp {
		h := http.Header{"Content-Type": {"text/csv"}, "Idempotency-Key": {harness.NewIdem()}}
		return c.admin.DoRaw(harness.Admin, http.MethodPost, "/v1/mechanization/machines/import?cooperative_ref="+c.coopNK+"&mode="+mode, h, strings.NewReader(csv))
	}
	first := c.want(imp("update", header+row("IMP-001", "Kubota")+row("IMP-002", "Yanmar")), 200, "nhập lần đầu")
	if num(first.Body, "created") != 2 || num(first.Body, "rejected") != 0 {
		t.Fatalf("nhập lần đầu: %s", trim(first.Raw))
	}
	second := c.want(imp("update", header+row("IMP-001", "Kubota DC-70")), 200, "nhập Cập nhật")
	if num(second.Body, "updated") != 1 || len(list(second.Body, "deactivated")) != 0 {
		t.Fatalf("Cập nhật không đụng máy vắng mặt: %s", trim(second.Raw))
	}
	active := func() int {
		return len(c.want(c.viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/machines?cooperative_ref="+c.coopNK, "", nil), 200, "máy đang hoạt động").Items())
	}
	if active() != 2 {
		t.Fatal("chế độ Cập nhật không làm mất máy")
	}
	third := c.want(imp("replace", header+row("IMP-001", "Kubota DC-70")), 200, "nhập Thay thế")
	if len(list(third.Body, "deactivated")) != 1 || active() != 1 {
		t.Fatalf("Thay thế phải vô hiệu hóa máy không có trong tệp: %s", trim(third.Raw))
	}
	all := c.want(c.viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/machines?include_inactive=true&cooperative_ref="+c.coopNK, "", nil), 200, "kể cả ngừng")
	if len(all.Items()) != 2 {
		t.Fatal("Thay thế không được xóa máy")
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Định mức: phải có văn bản, không chồng khoảng hiệu lực cho cùng chủng loại × khâu".
// Khác prototype: định mức là tham số có phiên bản theo effective_from (không có ngày kết thúc để
// "đóng"): bản sau tự thay từ ngày hiệu lực của nó; trùng effective_from với bản đã duyệt phải nêu
// correction_reason (bản ghi bù); người duyệt phải khác người đặt (R-56).
func TestPrototypeDinhMucPhaiCoVanBan(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Định mức: phải có văn bản, không chồng khoảng hiệu lực cho cùng chủng loại × khâu")
	w := newWorld(t)
	key := "cgh.norm_ha_per_machine:MGD:thu_hoach"
	setter := w.user("", true, "param_setter", "param_approver")
	approver := w.user("", true, "param_approver")
	propose := func(from, doc string) harness.Resp {
		return setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions", "", map[string]any{
			"key": key, "value": "30", "unit": "ha/máy/vụ", "effective_from": from, "source_doc": doc,
		})
	}
	wantFieldError(t, propose("2027-01-01", ""), "source_doc", "định mức thiếu văn bản")
	wantProblem(t, propose("2026-01-01", "QĐ 1/2027"), 422, "masterdata.param_correction_required", "trùng ngày hiệu lực với định mức seed")
	id := str(w.want(propose("2027-01-01", "QĐ 1/2027"), 201, "định mức mới từ 2027").Body, "id")
	w.want(setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/submit", "", nil), 200, "gửi duyệt")
	wantProblem(t, setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", "", map[string]any{}), 403, "self_approval_forbidden", "người đặt tự duyệt")
	w.want(approver.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", "", map[string]any{}), 200, "duyệt")
	at := func(day string) string {
		r := w.want(w.root.Do(harness.App, http.MethodGet, "/v1/masterdata/params/value?key="+key+"&at="+day, "", nil), 200, "định mức tại "+day)
		return str(r.Body, "value")
	}
	if a, b := at("2026-06-01"), at("2027-03-01"); a != "90" || b != "30" {
		t.Fatalf("định mức hiện hành theo ngày: 2026 = %s (muốn 90), 2027 = %s (muốn 30)", a, b)
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Ngưỡng cảnh báo theo phiên bản: mốc phải tăng dần, áp đúng phiên bản theo ngày của vụ".
// Giống prototype: mốc phải tăng dần (Cần chú ý < Đủ < Thừa, so với hai mốc đã duyệt tại ngày hiệu lực —
// kiểm khi đề xuất và khi duyệt) và áp đúng phiên bản theo ngày của vụ (ngày tham chiếu = ngày bắt đầu vụ, 01/11).
func TestPrototypeNguongTheoPhienBan(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Ngưỡng cảnh báo theo phiên bản: mốc phải tăng dần, áp đúng phiên bản theo ngày của vụ")
	c := newCGH(t)
	o := str(c.owner(c.coopNK), "id")
	for i := 1; i <= 4; i++ {
		c.machine(c.coopNK, o, "MLD", fmt.Sprintf("NG-%d", i), "2025-06-01")
	}
	c.area(c.coopNK, 2026, "550") // 4 × 120 ÷ 550 ≈ 87,3 %
	c.area(c.coopNK, 2027, "550")
	c.approveParam("cgh.coverage.sufficient_min_pct", "90", "2027-01-01")
	setter := c.user("", true, "param_setter")
	for _, bad := range []struct{ key, value, from, why string }{
		{"cgh.coverage.sufficient_min_pct", "50", "2027-06-01", "Đủ 50 % dưới Cần chú ý 60 %"},
		{"cgh.coverage.surplus_min_pct", "90", "2027-06-01", "Thừa 90 % không lớn hơn Đủ 90 %"},
		{"cgh.coverage.attention_min_pct", "86", "2026-06-01", "Cần chú ý 86 % trên Đủ 85 % (bản trước 01/01/2027)"},
	} {
		wantProblem(t, setter.Do(harness.Admin, http.MethodPost, "/v1/masterdata/params/versions", "", map[string]any{
			"key": bad.key, "value": bad.value, "unit": "%", "effective_from": bad.from, "source_doc": "QĐ thử nghiệm UAT số 02/2027",
		}), 422, "masterdata.param_threshold_order", bad.why)
	}
	for _, tc := range []struct {
		year       int
		sufficient string
		status     string
	}{{2026, "85", "sufficient"}, {2027, "90", "attention"}} {
		run, res := c.balance(tc.year)
		if got := str(obj(obj(run, "thresholds"), "sufficient"), "value"); got != tc.sufficient {
			t.Fatalf("vụ DX-%d phải dùng ngưỡng Đủ %s, dùng %s", tc.year, tc.sufficient, got)
		}
		if r := res[c.coopNK]; str(r, "status") != tc.status {
			t.Fatalf("vụ DX-%d: 87,3 %% phải là %s, nhận %v", tc.year, tc.status, r)
		}
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Ngưỡng cảnh báo theo phiên bản…" (classifyCoverage) và
// "UAT DEF-CGH-01: bảng điều hành CGH tính lại nhu cầu, cần–có theo khâu …".
// Khác prototype: mức đáp ứng không cắt ở 100 % (Thừa ≥ 120 %); mốc đúng bằng ngưỡng thuộc mức cao hơn.
func TestPrototypePhanLoaiMucDapUng(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Ngưỡng cảnh báo theo phiên bản: mốc phải tăng dần, áp đúng phiên bản theo ngày của vụ")
	nguon(t, "brd-2026.test.ts", "UAT DEF-CGH-01: bảng điều hành CGH tính lại nhu cầu, cần–có theo khâu và theo tỉnh theo vụ được lọc")
	c := newCGH(t)
	cases := []struct {
		coop, area, pct, status, label string
	}{
		{c.coopNK, "240", "50", "shortage", "Thiếu"},
		{c.coopNK2, "200", "60", "attention", "Cần chú ý"},
		{c.coopBT, "120", "100", "sufficient", "Đủ"},
		{c.coopLX, "100", "120", "surplus", "Thừa"},
	}
	for i, tc := range cases {
		c.machine(tc.coop, str(c.owner(tc.coop), "id"), "MLD", fmt.Sprintf("PL-%d", i), "2025-06-01")
		c.area(tc.coop, 2026, tc.area)
	}
	_, res := c.balance(2026)
	for _, tc := range cases {
		r := res[tc.coop]
		if !dec(t, r["coverage_pct"]).Equal(decimal.RequireFromString(tc.pct)) || str(r, "status") != tc.status || str(r, "status_label") != tc.label {
			t.Fatalf("1 máy × 120 ha ÷ %s ha: muốn %s %% %s, nhận %v", tc.area, tc.pct, tc.status, r)
		}
	}
	dash := c.want(c.viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/dashboard?season_code=DX&season_year=2026", "", nil), 200, "dashboard CGH")
	if num(obj(dash.Body, "kpis"), "total_machines") != 4 || num(obj(dash.Body, "kpis"), "total_cooperatives") != 4 {
		t.Fatalf("dashboard theo vụ: %s", trim(dash.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Chủng loại máy còn máy tham chiếu thì không xoá được, chỉ ngừng sử dụng".
// Khác prototype: danh mục không có thao tác xóa (DELETE → 405); ngừng dùng cần lý do, máy cũ giữ
// nguyên, máy mới không nhận chủng loại đã ngừng; kích hoạt lại được.
func TestPrototypeChungLoaiMayChiNgungDung(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Chủng loại máy còn máy tham chiếu thì không xoá được, chỉ ngừng sử dụng")
	c := newCGH(t)
	o := str(c.owner(c.coopNK), "id")
	m := c.machine(c.coopNK, o, "MGS", "GS-1", "2025-06-01")
	item := "/v1/masterdata/catalogs/machine_type/items/MGS"
	wantProblem(t, c.root.Do(harness.Admin, http.MethodDelete, item, "", nil), 405, "method_not_allowed", "xóa chủng loại")
	c.want(c.root.Do(harness.Admin, http.MethodPost, item+"/deactivate", "", map[string]any{"reason": "Ngừng dùng thử nghiệm UAT"}), 200, "ngừng dùng chủng loại")
	got := c.want(c.viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/machines/"+str(m, "ref"), "", nil), 200, "máy cũ")
	if got.Body["active"] != true || str(got.Body, "machine_type") != "MGS" {
		t.Fatalf("máy đang tham chiếu chủng loại phải giữ nguyên: %s", trim(got.Raw))
	}
	wantFieldError(t, c.post("/machines", c.machineBody(c.coopNK, o, "MGS", "GS-2", "2025-06-01")), "machine_type", "máy mới với chủng loại đã ngừng")
	c.want(c.root.Do(harness.Admin, http.MethodPost, item+"/reactivate", "", map[string]any{"reason": "Dùng lại sau thử nghiệm"}), 200, "kích hoạt lại")
	c.machine(c.coopNK, o, "MGS", "GS-2", "2025-06-01")
}
