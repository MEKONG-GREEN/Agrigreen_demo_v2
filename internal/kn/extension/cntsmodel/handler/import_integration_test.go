package handler_test

// W11.53c (#176, BRD CNTS v3.7 FN-19 luồng phụ "Nạp Excel điểm, hộ") trên PostgreSQL thật: tệp mẫu, xem trước không ghi,
// lỗi theo dòng/cột không ghi nửa chừng, số mũ/chuỗi dài bị chặn, xã ngoài tỉnh, trùng trong tệp và với dữ liệu đã có, chủ
// hộ vào kho định danh (sổ KN và bản lưu idempotency không có họ tên/SĐT), phát lại theo Idempotency-Key.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

type actorNames struct {
	mu sync.Mutex
	m  map[uuid.UUID]string
}

func (a *actorNames) DisplayNames(_ context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[uuid.UUID]string{}
	for _, r := range refs {
		if n, ok := a.m[r]; ok {
			out[r] = n
		}
	}
	return out, nil
}

// book dựng tệp nạp: sheet điểm, sheet hộ (tiêu đề mẫu + dòng). nil → không có sheet đó.
func book(t *testing.T, sites, households [][]any) []byte {
	t.Helper()
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	first := true
	put := func(name string, head []string, rows [][]any) {
		if rows == nil {
			return
		}
		if first {
			if err := x.SetSheetName("Sheet1", name); err != nil {
				t.Fatal(err)
			}
			first = false
		} else if _, err := x.NewSheet(name); err != nil {
			t.Fatal(err)
		}
		h := make([]any, len(head))
		for i, s := range head {
			h[i] = s
		}
		all := append([][]any{h}, rows...)
		for i, r := range all {
			cell, _ := excelize.CoordinatesToCellName(1, i+1)
			if err := x.SetSheetRow(name, cell, &r); err != nil {
				t.Fatal(err)
			}
		}
	}
	put(domain.SheetSites, domain.SiteHeaders, sites)
	put(domain.SheetHouseholds, domain.HouseholdHeaders, households)
	buf, err := x.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (u user) upload(path, idem string, data []byte) resp {
	u.f.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.SetBoundary("agtest176boundary") // thân tất định như web (stableMultipart): gọi lặp cùng khóa = cùng từng byte
	fw, _ := w.CreateFormFile("file", "nap.xlsx")
	_, _ = fw.Write(data)
	_ = w.Close()
	r, _ := http.NewRequest(http.MethodPost, u.f.srv.URL+path, &body)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	r.Header.Set("Idempotency-Key", idem)
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

// issueAt tìm lỗi của sheet/dòng/cột.
func issueAt(l []any, sheet string, row int, col string) bool {
	for _, x := range l {
		m := x.(map[string]any)
		if str(m, "sheet") == sheet && num(m, "row") == itoa(row) && str(m, "column") == col {
			return true
		}
	}
	return false
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestCNTSModelImport_W1153c(t *testing.T) {
	f := newFix(t)
	chu := f.user(tinh("chu_nhiem"))
	chu2 := f.user(tinh("chu_nhiem"))
	viewer := f.user(tinh("vien_chuc"))
	officer := f.user(xa("30001"))
	twin1, twin2 := f.user(xa("30002")), f.user(xa("30002"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: twin1.ref, Communes: []string{"30002"}},
		{ActorRef: twin2.ref, Communes: []string{"30002"}}}
	f.names.m[officer.ref], f.names.m[twin1.ref], f.names.m[twin2.ref] = "Lê Văn Theo", "Trần Văn Trùng", "Trần Văn Trùng"

	p := want(t, chu.post(base+"projects", map[string]any{"code": "DA-NAP-2027", "name": "Dự án nạp Excel", "kind": "knqg",
		"start_date": "2027-03-01"}), 201, "", "tạo dự án")
	pref := str(p, "ref")
	imp := base + "projects/" + pref + "/import"

	// ----- Tệp mẫu -----
	want(t, viewer.get(base+"projects/"+pref+"/import-template"), 403, "", "viên chức không tải mẫu nạp")
	tr := chu.get(base + "projects/" + pref + "/import-template")
	if tr.status != 200 || !bytes.HasPrefix(tr.raw, []byte("PK")) {
		t.Fatalf("tệp mẫu: %d", tr.status)
	}
	tpl, err := excelize.OpenReader(bytes.NewReader(tr.raw))
	if err != nil {
		t.Fatal(err)
	}
	if l := tpl.GetSheetList(); len(l) != 3 || l[0] != domain.SheetSites || l[1] != domain.SheetHouseholds {
		t.Fatalf("sheet mẫu: %v", l)
	}
	if v, _ := tpl.GetCellValue(domain.SheetHouseholds, "C1"); v != domain.HouseholdHeaders[2] {
		t.Fatalf("tiêu đề mẫu: %q", v)
	}
	_ = tpl.Close()
	// Tệp mẫu trống nạp lại: không có dòng nào → không hợp lệ (không ghi gì).
	if r := want(t, chu.upload(imp+"/preview", key(), tr.raw), 200, "", "xem trước tệp mẫu trống"); r["valid"] != false {
		t.Fatalf("tệp trống: %v", r)
	}

	// ----- Lỗi theo dòng/cột: không ghi nửa chừng -----
	badSites := [][]any{
		{"D1", "Điểm ấp Bình Hòa", "30001", "GA_TP", "1500", "con", "BM-08", "Lê Văn Theo", ""},
		{"D2", "Điểm ngoài tỉnh", "31001", "GA_TP", "", "", "", "", ""},                    // FN-14: xã ngoài tỉnh
		{"D3", "Điểm số mũ", "30001", "GA_TP", "1e100000", "con", "", "", ""},              // AUTO-REVIEW §4
		{"D4", "Điểm dài", "30001", "GA_TP", strings.Repeat("9", 5000), "con", "", "", ""}, // chuỗi rất dài
		{"D5", "Điểm loại lạ", "30002", "BO", "", "", "", "Trần Văn Trùng", ""},            // loại mô hình, cán bộ trùng tên
		{"D1", "Điểm trùng mã", "30001", "GA_TP", "", "", "", "", ""},
		{"D6", "Điểm SĐT", "30001", "GA_TP", "", "", "", "", "gọi 0912345678"},
	}
	badHH := [][]any{
		{"D1", "Nguyễn Văn Bình", "0912345678", "500", "con", "", "05/03/2027", ""},
		{"D1", "Nguyễn V. Bình", "912345678", "", "", "", "", ""}, // trùng SĐT cùng điểm (ô số mất số 0)
		{"D9", "Trần Thị Cúc", "0987654321", "", "", "", "", ""},  // mã điểm không có
		{"D1", "Phạm Văn Đạt", "0123", "1,5", "ha", "", "", ""},   // SĐT sai
		{"D1", "Hộ số mũ", "0934111222", "1E+400", "con", "", "", ""},
		{"D1", "Hộ chuỗi dài", "0934111223", strings.Repeat("9", 5000), "con", "", "", ""},
		{"D1", "Hộ số âm", "0934111224", "-5", "con", "", "", ""},
		{"D1", "Hộ quá lớn", "0934111225", "1000000000000", "con", "", "", ""},
	}
	data := book(t, badSites, badHH)
	k := key()
	r := want(t, chu.upload(imp, k, data), 422, "extension.cnts_import_invalid", "tệp còn lỗi")
	is := list(r, "issues")
	for _, w := range []struct {
		sheet string
		row   int
		col   string
	}{{domain.SheetSites, 3, "C"}, {domain.SheetSites, 4, "E"}, {domain.SheetSites, 5, "E"}, {domain.SheetSites, 6, "D"},
		{domain.SheetSites, 6, "H"}, {domain.SheetSites, 7, "A"}, {domain.SheetSites, 8, "I"}, {domain.SheetHouseholds, 3, "C"},
		{domain.SheetHouseholds, 4, "A"}, {domain.SheetHouseholds, 5, "C"}, {domain.SheetHouseholds, 6, "D"},
		{domain.SheetHouseholds, 7, "D"}, {domain.SheetHouseholds, 8, "D"}, {domain.SheetHouseholds, 9, "D"}} {
		if !issueAt(is, w.sheet, w.row, w.col) {
			t.Errorf("thiếu lỗi %s dòng %d cột %s: %s", w.sheet, w.row, w.col, r)
		}
	}
	if rr := chu.upload(imp, k, data); rr.status != 422 || bytes.Contains(rr.raw, []byte("0912345678")) {
		t.Fatalf("phát lại lỗi / lộ SĐT: %d %s", rr.status, rr.raw)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_demo_site WHERE tenant_id = $1`, f.tenant); n != 0 {
		t.Fatalf("ghi nửa chừng %d điểm", n)
	}
	if len(f.vault.Accesses()) != 0 {
		t.Fatalf("tệp lỗi đã gọi kho định danh: %d", len(f.vault.Accesses()))
	}

	// ----- Xem trước tệp đúng: không ghi -----
	goodSites := [][]any{
		{"d1", "Điểm ấp Bình Hòa", "30001", "GA_TP", "1.500,5", "con", "bm-08", "lê văn theo", ""},
		{"D2", "Điểm ấp Phú Hữu", "30002", "GA_TP", 2500, "con", "", "", ""},
	}
	goodHH := [][]any{
		{"D1", "Nguyễn Văn Bình", "0912345678", 500, "con", "", "05/03/2027", ""},
		{"D1", "Trần Thị Cúc", 987654321, "", "", "Hộ tự ghi", 46451, ""},
		{"D2", "Lê Văn Dũng", "0901234567", "", "", "Hộ tự ghi", "", ""},
	}
	good := book(t, goodSites, goodHH)
	want(t, chu2.upload(imp+"/preview", key(), good), 403, "", "AC-04: chủ nhiệm khác không nạp")
	pv := want(t, chu.upload(imp+"/preview", key(), good), 200, "", "xem trước")
	if pv["valid"] != true || num(pv, "site_count") != "2" || num(pv, "household_count") != "3" || pv["committed"] != false {
		t.Fatalf("xem trước: %s", pv)
	}
	s0 := list(pv, "sites")[0].(map[string]any)
	h0 := list(pv, "households")[0].(map[string]any)
	if str(s0, "scale") != "1500.5" || str(s0, "officer_ref") != officer.ref.String() || str(s0, "diary_form") != "BM-08" ||
		str(h0, "owner_label") != "N. V. Bình" || str(h0, "owner_phone_masked") != "091•••678" || str(h0, "joined_on") != "2027-03-05" {
		t.Fatalf("dòng xem trước: %v %v", s0, h0)
	}
	h1 := list(pv, "households")[1].(map[string]any)
	if str(h1, "joined_on") != "2027-03-05" || str(h1, "keeper_kind") != "self" {
		t.Fatalf("ngày Excel/hộ tự ghi: %v", h1)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_demo_site WHERE tenant_id = $1`, f.tenant); n != 0 {
		t.Fatalf("xem trước đã ghi %d điểm", n)
	}

	// ----- Ghi: một giao dịch, chủ hộ ở kho định danh -----
	k = key()
	c1 := want(t, chu.upload(imp, k, good), 201, "", "nạp tệp")
	if c1["committed"] != true || c1["replayed"] != false || len(list(c1, "site_refs")) != 2 {
		t.Fatalf("nạp: %s", c1)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_demo_site WHERE tenant_id = $1`, f.tenant); n != 2 {
		t.Fatalf("điểm: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_model_household WHERE tenant_id = $1 AND person_ref IS NOT NULL
	    AND idem_key LIKE 'chh:imp:%'`, f.tenant); n != 3 {
		t.Fatalf("hộ: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_model_household WHERE tenant_id = $1 AND keeper_kind = 'officer' AND keeper_ref = $2`,
		f.tenant, officer.ref); n != 1 {
		t.Fatalf("cán bộ theo dõi của điểm ghi nhật ký: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM shared.idempotency_key WHERE tenant_id = $1 AND (convert_from(response_body, 'UTF8') LIKE '%0912345678%'
	    OR convert_from(response_body, 'UTF8') LIKE '%Nguyễn Văn%')`, f.tenant); n != 0 {
		t.Fatalf("bản lưu idempotency có họ tên/SĐT: %d", n)
	}
	d := want(t, chu.get(base+"projects/"+pref), 200, "", "chi tiết dự án")
	if num(d, "site_count") != "2" || num(d, "household_count") != "3" {
		t.Fatalf("chi tiết: %s", d)
	}

	// ----- Phát lại cùng khóa: không bản sao -----
	c2 := chu.upload(imp, k, good)
	if c2.status != 201 || c2.body["committed"] != true {
		t.Fatalf("phát lại: %d %s", c2.status, c2.raw)
	}
	// Mất bản lưu phản hồi (tiến trình chết sau commit): sổ vẫn chống trùng theo khóa idem của từng dòng.
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM shared.idempotency_key WHERE tenant_id = $1 AND idem_key = $2`, f.tenant, k); err != nil {
		t.Fatal(err)
	}
	c3 := want(t, chu.upload(imp, k, good), 200, "", "phát lại theo sổ")
	if c3["replayed"] != true {
		t.Fatalf("phát lại theo sổ: %s", c3)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_demo_site WHERE tenant_id = $1`, f.tenant); n != 2 {
		t.Fatalf("phát lại tạo bản sao: %d điểm", n)
	}
	if fmt.Sprint(list(c3, "site_refs")) != fmt.Sprint(list(c1, "site_refs")) || num(c3, "site_count") != "2" || num(c3, "household_count") != "3" {
		t.Fatalf("phát lại theo sổ phải trả kết quả gốc: %v / %v", c3, c1)
	}
	// #176 review P2: mất bản lưu phản hồi, gửi tệp B KHÁC nội dung cùng khóa (cùng dự án, cùng dòng đầu) → từ chối,
	// không báo đã ghi; gửi lại đúng tệp A → kết quả gốc.
	dropReceipt := func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM shared.idempotency_key WHERE tenant_id = $1 AND idem_key = $2`, f.tenant, k); err != nil {
			t.Fatal(err)
		}
	}
	other := book(t, [][]any{goodSites[0], {"D9", "Điểm ấp Mỹ Lợi", "30002", "GA_TP", "", "", "", "", ""}}, goodHH)
	dropReceipt()
	if rb := chu.upload(imp, k, other); rb.status != 422 || str(rb.body, "code") != "idempotency_key_reused" || rb.body["committed"] == true {
		t.Fatalf("tệp khác cùng khóa sau mất bản lưu: %d %s", rb.status, rb.raw)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_demo_site WHERE tenant_id = $1`, f.tenant); n != 2 {
		t.Fatalf("tệp B đã ghi: %d điểm", n)
	}
	dropReceipt()
	c4 := want(t, chu.upload(imp, k, good), 200, "", "gửi lại tệp A sau tệp B")
	if c4["replayed"] != true || fmt.Sprint(list(c4, "site_refs")) != fmt.Sprint(list(c1, "site_refs")) {
		t.Fatalf("tệp A phải nhận kết quả gốc: %s", c4)
	}
	// Nạp lại cùng tệp bằng khóa khác: trùng điểm đã có → chặn, không ghi.
	again := want(t, chu.upload(imp, key(), good), 422, "extension.cnts_import_invalid", "nạp lại cùng tệp")
	if !issueAt(list(again, "issues"), domain.SheetSites, 2, "B") {
		t.Fatalf("trùng điểm đã có: %s", again)
	}

	// ----- Thêm hộ vào điểm đã có (mã UUID), trùng chủ hộ đã có → chặn -----
	var site1 string
	if err := f.pool.QueryRow(context.Background(), `SELECT site_ref::text FROM pf_extension.cnts_demo_site WHERE tenant_id = $1
	    AND name = 'Điểm ấp Bình Hòa'`, f.tenant).Scan(&site1); err != nil {
		t.Fatal(err)
	}
	dupHH := book(t, nil, [][]any{{site1, "Nguyễn Văn Bình", "0912 345 678", "", "", "", "", ""}})
	dh := want(t, chu.upload(imp, key(), dupHH), 422, "extension.cnts_import_invalid", "chủ hộ đã có ở điểm")
	if !issueAt(list(dh, "issues"), domain.SheetHouseholds, 2, "C") {
		t.Fatalf("trùng hộ đã có: %s", dh)
	}
	add := book(t, nil, [][]any{{site1, "Võ Thị Em", "0934567890", "200", "con", "", "", ""}})
	want(t, chu.upload(imp, key(), add), 201, "", "thêm hộ vào điểm đã có")
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_model_household WHERE tenant_id = $1 AND site_ref = $2::uuid`, f.tenant, site1); n != 3 {
		t.Fatalf("hộ của điểm 1: %d", n)
	}

	// ----- Tệp sai: không phải .xlsx, sai cột, dự án đã kết thúc -----
	want(t, chu.upload(imp+"/preview", key(), []byte("STT,Tên\n1,x")), 422, "extension.cnts_import_invalid", "không phải xlsx")
	x := excelize.NewFile()
	_ = x.SetSheetName("Sheet1", domain.SheetSites)
	_ = x.SetSheetRow(domain.SheetSites, "A1", &[]any{"Mã điểm", "Mã xã", "Tên điểm"})
	buf, _ := x.WriteToBuffer()
	_ = x.Close()
	bl := want(t, chu.upload(imp+"/preview", key(), buf.Bytes()), 422, "extension.cnts_import_invalid", "sai thứ tự cột")
	if !strings.Contains(str(bl, "detail"), "cột B") {
		t.Fatalf("lỗi bố cục: %s", bl)
	}
	want(t, chu.post(base+"projects/"+pref+"/close", map[string]any{"base_version": 1, "reason": "Đã nghiệm thu"}), 200, "", "kết thúc dự án")
	want(t, chu.upload(imp+"/preview", key(), good), 409, "extension.cnts_project_closed", "dự án đã kết thúc")

	// ADR-0055: KN không ghi dữ liệu HTX.
	if n := f.count(`SELECT (SELECT count(*) FROM pf_cooperative_pub.v1_cooperatives WHERE tenant_id = $1)`, f.tenant); n != 0 {
		t.Fatalf("KN đã ghi dữ liệu HTX: %d", n)
	}
}
