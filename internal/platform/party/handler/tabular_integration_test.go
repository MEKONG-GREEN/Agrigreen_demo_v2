package handler_test

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

func xlsxBytes(t *testing.T, header []string, rows [][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Sheet1", header, rows); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// coopCodes trả mã HTX (hệ thống sinh) theo ref.
func (f *fixture) coopCodes(tr tree) (string, string) {
	var c1, c2 string
	national := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)
	for _, it := range items(national.appDo(http.MethodGet, "/v1/cooperatives", nil)) {
		switch it["ref"] {
		case tr.c1.String():
			c1 = it["code"].(string)
		case tr.c2.String():
			c2 = it["code"].(string)
		}
	}
	return c1, c2
}

// download gọi GET trả thân thô (tệp xuất).
func (c caller) download(path string) (int, http.Header, []byte) {
	c.f.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.f.admin.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, r.Header, b
}

func rowsOf(r resp) []map[string]any {
	raw, _ := r.body["rows"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

// KN US-HTX-03: nhập nông hộ bằng tệp Excel .xlsx (tiêu đề tiếng Việt có dấu), kết quả theo dòng.
func TestImportHouseholdsXLSX(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c1, c2 := f.coopCodes(tr)
	header := []string{"Họ tên", "Số điện thoại", "CCCD", "Địa chỉ", "Mã HTX", "Diện tích (ha)", "Ngày vào HTX"}
	body := xlsxBytes(t, header, [][]string{
		{"Trần Thị Bình", "0907000001", "", "Ấp 2", c1, "2.5", "2023-05-01"},
		{"Trần Thị Bình", "0907000001", "", "Ấp 2", c1, "2.5", "2023-05-01"},
		{"Lê Văn Cường", "", "", "Ấp 3", c1, "1", ""},
		{},
		{"Phạm Văn Dũng", "0907000004", "", "Ấp 4", "HTX999999", "1", ""},
		{"Võ Thị Em", "0907000005", "", "Ấp 5", c2, "1", ""},
	})
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), tabular.ContentTypeXLSX, body)
	if r.status != http.StatusOK || r.body["accepted"] != float64(1) || r.body["duplicate"] != float64(1) || r.body["rejected"] != float64(3) {
		t.Fatalf("kết quả nhập .xlsx: %d %v", r.status, r.body)
	}
	// Số dòng là dòng thật của trang tính (dòng trống thứ 5 bị bỏ qua).
	wantStatus := []string{"accepted", "duplicate", "rejected", "rejected", "rejected"}
	wantLine := []float64{2, 3, 4, 6, 7}
	for i, row := range rowsOf(r) {
		if row["status"] != wantStatus[i] || row["row"] != wantLine[i] {
			t.Fatalf("dòng %d: %v", i, row)
		}
		if reason, _ := row["reason"].(string); strings.Contains(reason, "0907") || strings.Contains(reason, "Trần") {
			t.Fatalf("lý do không được chứa dữ liệu cá nhân: %q", reason)
		}
	}
	if got := len(items(prov.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil))); got != 1 {
		t.Fatalf("HTX 1 có đúng 1 nông hộ: %d", got)
	}
}

// Ô công thức trong .xlsx: cả tệp bị từ chối, không tính công thức.
func TestImportHouseholdsXLSXFormulaRejected(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c1, _ := f.coopCodes(tr)
	x := excelize.NewFile()
	_ = x.SetSheetRow("Sheet1", "A1", &[]any{"ho_ten", "so_dien_thoai", "ma_htx", "dien_tich_ha"})
	_ = x.SetSheetRow("Sheet1", "A2", &[]any{"Ngô Văn Giàu", "0907000009", c1, "1"})
	_ = x.SetSheetRow("Sheet1", "A3", &[]any{"Ngô Thị Hoa", "0907000010", c1})
	_ = x.SetCellFormula("Sheet1", "D3", "D2*2")
	var buf bytes.Buffer
	if _, err := x.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), tabular.ContentTypeXLSX, buf.Bytes())
	if r.status != http.StatusUnprocessableEntity || r.body["code"] != "party.import_invalid" || !strings.Contains(str(r.body, "detail"), "D3") {
		t.Fatalf("ô công thức phải bị từ chối cả tệp: %d %v", r.status, r.body)
	}
	if got := len(items(prov.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil))); got != 0 {
		t.Fatalf("không nạp dòng nào khi tệp bị từ chối: %d", got)
	}
}

// KN US-ADM-08: xuất cây tổ chức ra Excel → nhập lại (khứ hồi) → sửa tệp để tạo, di chuyển, đổi tên.
func TestOrgTreeExportImportRoundTrip(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)

	status, hdr, data := root.download("/v1/orgs/export.xlsx")
	if status != http.StatusOK || hdr.Get("Content-Type") != tabular.ContentTypeXLSX {
		t.Fatalf("xuất: %d %s %s", status, hdr.Get("Content-Type"), data)
	}
	tb, err := tabular.ReadBytes(data, tabular.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tb.Header, []string{"Mã tổ chức", "Loại", "Tên", "Mã đơn vị hành chính", "Mã tổ chức cấp trên", "Trạng thái"}) || len(tb.Rows) != 7 {
		t.Fatalf("tệp xuất: %v, %d dòng", tb.Header, len(tb.Rows))
	}
	// Cha luôn đứng trước con.
	seen := map[string]bool{}
	byName := map[string][]string{}
	for _, r := range tb.Rows {
		if len(r.Cells) != 6 {
			t.Fatalf("dòng %d: %v", r.Line, r.Cells)
		}
		if p := r.Cells[4]; p != "" && !seen[p] {
			t.Fatalf("cấp trên %s phải đứng trước dòng %d", p, r.Line)
		}
		seen[r.Cells[0]] = true
		byName[r.Cells[2]] = r.Cells
	}
	if byName["Trung tâm Khuyến nông Quốc gia"][1] != "ttkn_national" || byName["HTX Lúa Xanh"][3] != "30001" || byName["HTX Lúa Xanh"][5] != "Hoạt động" {
		t.Fatalf("nội dung xuất: %v", byName)
	}

	// Nhập lại nguyên tệp: không đổi gì.
	r := root.call(f.admin.URL, http.MethodPost, "/v1/orgs/import", key(), tabular.ContentTypeXLSX, data)
	if r.status != http.StatusOK || r.body["unchanged"] != float64(7) || r.body["created"] != float64(0) || r.body["rejected"] != float64(0) {
		t.Fatalf("nhập lại tệp xuất: %d %v", r.status, r.body)
	}

	// Sửa tệp: tổ mới đặt TRƯỚC dòng cha (vẫn xử lý đúng), HTX mới dưới tổ mới,
	// HTX 1 chuyển sang xã B, đổi tên tổ xã B, và vài dòng lỗi.
	t2 := byName["Tổ KNCĐ Xã B"]
	c1 := byName["HTX Lúa Xanh"]
	p92 := byName["TTKN Cần Thơ"]
	var rows [][]string
	rows = append(rows, []string{"TKN-31001", "Tổ khuyến nông cộng đồng", "Tổ KNCĐ Xã C", "31001", p92[0], ""})
	for _, r := range tb.Rows {
		cells := slices.Clone(r.Cells)
		switch cells[0] {
		case c1[0]:
			cells[3], cells[4] = "30002", t2[0]
		case t2[0]:
			cells[2] = "Tổ KNCĐ Xã B (sáp nhập)"
		}
		rows = append(rows, cells)
	}
	rows = append(rows,
		[]string{"", "cooperative", "HTX Xã C Mới", "31001", "TKN-31001", ""},
		[]string{"X-1", "loai_la", "Sai loại", "", "", ""},
		[]string{"X-2", "extension_team", "Không có cha", "30001", "KHONG-CO", ""},
		[]string{"HTX000999", "cooperative", "HTX không có", "30001", "", ""},
		[]string{"TKN-31001", "extension_team", "Trùng mã", "31001", p92[0], ""},
	)
	body := xlsxBytes(t, tb.Header, rows) // người dùng sửa trên chính tệp xuất
	r = root.call(f.admin.URL, http.MethodPost, "/v1/orgs/import?reason="+"S%C3%A1p+nh%E1%BA%ADp+x%C3%A3+theo+NQ+1234", key(), tabular.ContentTypeXLSX, body)
	if r.status != http.StatusOK || r.body["created"] != float64(2) || r.body["updated"] != float64(2) || r.body["unchanged"] != float64(5) || r.body["rejected"] != float64(4) {
		t.Fatalf("nhập tệp đã sửa: %d %v", r.status, r.body)
	}
	res := rowsOf(r)
	if res[0]["status"] != "created" || res[0]["row"] != float64(2) {
		t.Fatalf("tổ mới đặt trước cha vẫn được tạo: %v", res[0])
	}
	newCoop := res[8]
	if newCoop["status"] != "created" || !strings.HasPrefix(newCoop["code"].(string), "HTX") {
		t.Fatalf("HTX mới được sinh mã: %v", newCoop)
	}
	for i, want := range map[int]string{9: "không hợp lệ", 10: "KHONG-CO", 11: "để trống", 12: "trùng với dòng 2"} {
		if res[i]["status"] != "rejected" || !strings.Contains(res[i]["reason"].(string), want) {
			t.Fatalf("dòng lỗi %d: %v", i, res[i])
		}
	}
	for _, row := range res {
		switch row["code"] {
		case c1[0]:
			if row["status"] != "updated" || !slices.Equal(anyStrings(row["changes"]), []string{"moved"}) {
				t.Fatalf("HTX 1 được di chuyển: %v", row)
			}
		case t2[0]:
			if row["status"] != "updated" || !slices.Equal(anyStrings(row["changes"]), []string{"renamed"}) {
				t.Fatalf("tổ xã B được đổi tên: %v", row)
			}
		}
	}
	// Di chuyển đi qua MoveOrg: sổ ghi lý do, vị trí cũ; phạm vi đổi theo vị trí mới.
	h := root.adminDo(http.MethodGet, "/v1/orgs/"+tr.c1.String()+"/events", nil)
	if ev := items(h); len(ev) != 2 || ev[1]["action"] != "moved" || ev[1]["reason"] != "Sáp nhập xã theo NQ 1234" || ev[1]["prev_parent_ref"] != tr.t30001.String() {
		t.Fatalf("sổ biến động HTX 1: %v", h.body)
	}
	verifyChain(t, f, tr.c1, 2)

	// Xuất lại: có tổ và HTX mới, cha trước con.
	_, _, data = root.download("/v1/orgs/export.xlsx")
	tb2, err := tabular.ReadBytes(data, tabular.Limits{})
	if err != nil || len(tb2.Rows) != 9 {
		t.Fatalf("xuất lại: %v %d", err, len(tb2.Rows))
	}
	// Sai mẫu: thiếu cột Loại → từ chối cả tệp.
	bad := root.call(f.admin.URL, http.MethodPost, "/v1/orgs/import", key(), "text/csv", []byte("Mã tổ chức,Tên\nA,B\n"))
	if bad.status != http.StatusUnprocessableEntity || bad.body["code"] != "party.import_invalid" {
		t.Fatalf("tệp sai mẫu: %d %v", bad.status, bad.body)
	}
	// Chỉ quản trị dữ liệu chủ; cán bộ xã không được xuất.
	if st, _, _ := f.as(f.tenant, tr.t30001, port.RoleKNOfficer).download("/v1/orgs/export.xlsx"); st != http.StatusForbidden {
		t.Fatalf("cán bộ xã xuất cây phải 403: %d", st)
	}
}

// TTKN tỉnh chỉ xuất nhánh trong tỉnh; mã cấp trên ngoài phạm vi vẫn có để nối cây.
func TestOrgTreeExportScoped(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	_, _, data := prov.download("/v1/orgs/export.xlsx")
	tb, err := tabular.ReadBytes(data, tabular.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range tb.Rows {
		names = append(names, r.Cells[2])
	}
	if !slices.Equal(names, []string{"TTKN An Giang", "Tổ KNCĐ Xã A", "HTX Lúa Xanh", "Tổ KNCĐ Xã B"}) || tb.Rows[0].Cells[4] == "" {
		t.Fatalf("phạm vi tỉnh 89: %v / %v", names, tb.Rows[0].Cells)
	}
	// Nhập lại bởi TTKN tỉnh: không đổi gì, không lỗi phạm vi.
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/orgs/import", key(), tabular.ContentTypeXLSX, data)
	if r.status != http.StatusOK || r.body["unchanged"] != float64(4) {
		t.Fatalf("TTKN tỉnh nhập lại: %d %v", r.status, r.body)
	}
}

func anyStrings(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(string))
	}
	return out
}
