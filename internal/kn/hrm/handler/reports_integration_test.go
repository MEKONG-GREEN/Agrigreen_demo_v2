package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (f *recFixture) raw(as, path string) (*httptest.ResponseRecorder, []byte) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-As", as)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w, w.Body.Bytes()
}

func staffBody(typ, name, phone, gender, birth string, sens map[string]any, events ...map[string]any) map[string]any {
	b := newStaff(typ, name, phone, events...)
	p := b["profile"].(map[string]any)
	p["gender"], p["birth_date"] = gender, birth
	if sens != nil {
		b["sensitive"] = sens
	}
	return b
}

func xlsxText(t *testing.T, data []byte, sheet string) string {
	t.Helper()
	x, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	rows, err := x.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, r := range rows {
		sb.WriteString(strings.Join(r, "|") + "\n")
	}
	return sb.String()
}

// W11.28 (FN-12, FN-06) qua HTTP: cờ AG_HRM_RECORDS, quyền (HCTH/Admin xuất toàn Trung tâm; lãnh đạo phòng không xuất biểu
// Sở, dashboard phòng mình), as-of tại ngày chốt (AC-02), quy tắc kiểm chéo, nhấn ô (NS05/06), sổ xuất + nhật ký trường
// nhạy cảm, tệp dựng lại được (SHA-256, drift khi định danh đổi), đọc kho theo lô.
func TestHRMReportsHTTP_W1128(t *testing.T) {
	off := newRecFixture(t, false)
	if p := off.call("hcth", http.MethodGet, "/v1/hrm/reports", nil, http.StatusForbidden); p["code"] != "hrm.records_disabled" {
		t.Fatalf("tắt: %v", p)
	}
	off.call("hcth", http.MethodGet, "/v1/hrm/dashboard", nil, http.StatusForbidden)

	f := newRecFixture(t, true)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	full := map[string]any{"ethnicity": "kinh", "religion": "khong", "hometown": "Xã Mỹ Hòa Hưng", "revolution_date": "2010-05",
		"party_date": "2012-03-04"}
	start := ev("work_start", "2015-03-01", map[string]any{})
	tt := ev("unit", "2015-03-01", map[string]any{"unit": "trong_trot"})
	vc := ev("position", "2015-03-01", map[string]any{"position": "vien_chuc"})
	a := f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("vien_chuc", "Trần Thị Mai", "0912000111", "nu", "1988-05-01", full, tt, start,
		ev("position", "2018-01-01", map[string]any{"position": "truong_phong", "appointed_on": "2018-01-01"}),
		ev("grade", "2015-03-01", map[string]any{"grade": "01.003", "step": "5"}),
		ev("degree", "2012-06-01", map[string]any{"level": "thac_si", "major": "trong_trot", "form": "chinh_quy"}),
		ev("training", "2016-01-01", map[string]any{"type": "political_theory", "code": "trung_cap"})), http.StatusCreated)["id"].(string)
	b := f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("vien_chuc", "Lê Văn Bình", "0912000222", "nam", "1964-02-01", full, tt, vc, start,
		ev("degree", "1988-06-01", map[string]any{"level": "dai_hoc", "major": "chan_nuoi"})), http.StatusCreated)["id"].(string)
	d := f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("vien_chuc", "Phạm Văn Đông", "0912000333", "nam", "1970-01-01", full,
		ev("unit", "2015-03-01", map[string]any{"unit": "chan_nuoi"}), vc, start), http.StatusCreated)["id"].(string)
	e := f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("vien_chuc", "Võ Thị Em", "0912000444", "nu", "1995-01-01", nil, tt, vc, start),
		http.StatusCreated)["id"].(string)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("hop_dong", "Ngô Văn Hợp", "0912000555", "nam", "1990-01-01", full,
		ev("unit", "2020-01-01", map[string]any{"unit": "hcth"}), vc, ev("work_start", "2020-01-01", map[string]any{})), http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("can_bo_xa", "Nguyễn Văn Xã", "0912000666", "nam", "1985-01-01", nil,
		ev("unit", "2022-01-01", map[string]any{"commune": "30292", "mgmt_unit": "trong_trot"}), vc, ev("work_start", "2022-01-01", map[string]any{})),
		http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+d+"/events", ev("work_end", "2026-10-05", map[string]any{"reason": "nghi_huu"}), http.StatusCreated)

	// Quyền: lãnh đạo phòng, viên chức, Cấp Xã/TW không có biểu Sở; mã biểu lạ 404.
	for _, as := range []string{"ld_tt", "vc_tt", "xa", "tw"} {
		f.call(as, http.MethodGet, "/v1/hrm/reports", nil, http.StatusForbidden)
		f.call(as, http.MethodGet, "/v1/hrm/reports/NS03", nil, http.StatusForbidden)
	}
	f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS09", nil, http.StatusNotFound)
	if xs := f.call("hcth", http.MethodGet, "/v1/hrm/reports", nil, http.StatusOK)["items"].([]any); len(xs) != 6 {
		t.Fatalf("danh sách biểu: %v", xs)
	}
	// Thẻ TK chỉ cấp cho người: bỏ sensitive.view của nhóm HCTH → biểu có trường TK 403, NS03 vẫn được.
	f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "user", "grantee": f.who["hcth2"].ActorRef.String(),
		"perms": map[string]any{"sensitive.view": "none"}}, http.StatusCreated)
	f.call("hcth2", http.MethodGet, "/v1/hrm/reports/NS01", nil, http.StatusForbidden)
	f.call("hcth2", http.MethodGet, "/v1/hrm/reports/NS03", nil, http.StatusOK)

	// NS01 tại 12/10/2026: A (trưởng phòng trước), E, B theo tên; D đã nghỉ. R3 đạt (4 + 0 − 1 = 3); TK cảnh báo E.
	before := len(f.vault.Accesses())
	v := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS01?at=2026-10-12", nil, http.StatusOK)
	if v["status"] != "warning" || v["period"] != "THÁNG 10/2026" || len(v["columns"].([]any)) != 19 {
		t.Fatalf("NS01: %v %v", v["status"], v["period"])
	}
	var names []string
	for _, r := range v["rows"].([]any) {
		row := r.(map[string]any)
		if row["kind"] == "person" {
			names = append(names, row["cells"].([]any)[1].(string))
		}
	}
	if strings.Join(names, ",") != "Trần Thị Mai,Lê Văn Bình,Võ Thị Em" {
		t.Fatalf("thứ tự NS01: %v", names)
	}
	for _, c := range v["checks"].([]any) {
		ch := c.(map[string]any)
		if ch["rule"] == "R3" && ch["ok"] != true || ch["rule"] == "TK" && (ch["ok"] != false || len(ch["people"].([]any)) != 1) {
			t.Fatalf("kiểm tra: %v", ch)
		}
	}
	// Đọc kho THEO LÔ: một lần đọc ngăn hồ sơ + một lần ngăn nhạy cảm cho cả lô (fake ghi từng chủ thể).
	ops := map[string]int{}
	for _, x := range f.vault.Accesses()[before:] {
		ops[x.Op+"/"+string(x.Purpose)]++
	}
	if ops["section_read/hrm.sensitive"] != 5 || ops["resolve/hrm.profile"] != 5 {
		t.Fatalf("đọc kho: %v", ops)
	}
	// AC-02: NS01 tháng 9 vẫn có D; NS02 tháng 10 có D với lý do nghỉ.
	v9 := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS01?at=2026-09-30", nil, http.StatusOK)
	if !strings.Contains(fmt.Sprint(v9["rows"]), "Phạm Văn Đông") {
		t.Fatal("NS01 tháng 9 phải có người nghỉ 05/10")
	}
	v2 := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS02?at=2026-10-12", nil, http.StatusOK)
	if s := fmt.Sprint(v2["rows"]); !strings.Contains(s, "Phạm Văn Đông") || !strings.Contains(s, "từ 05/10/2026") {
		t.Fatalf("NS02: %s", s)
	}

	// NS06: tổng 4 (3 VC + 1 HĐ), B trên tuổi nghỉ hưu (R6 cảnh báo), không chặn; nhấn ô Nữ dòng Tổng cộng.
	v6 := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS06?at=2026-10-12", nil, http.StatusOK)
	if v6["status"] == "blocked" || len(v6["columns"].([]any)) != 39 {
		t.Fatalf("NS06: %v", v6["checks"])
	}
	cell := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS06/cells?at=2026-10-12&row="+domain.RowKeyCenter+"&col=nu", nil, http.StatusOK)
	if items := cell["items"].([]any); len(items) != 2 {
		t.Fatalf("ô Nữ: %v", cell)
	}
	f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS06/cells?at=2026-10-12&row=_center&col=don_vi", nil, http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS01/cells?row=_center&col=nu", nil, http.StatusUnprocessableEntity)
	v5 := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS05?at=2026-10-12", nil, http.StatusOK)
	if v5["status"] == "blocked" {
		t.Fatalf("NS05: %v", v5["checks"])
	}
	if c := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS05/cells?at=2026-10-12&row=trong_trot&col=g:ths_nong_hoc", nil, http.StatusOK); len(c["items"].([]any)) != 1 {
		t.Fatalf("ô ThS Nông học: %v", c)
	}

	// Xuất NS01: tóm tắt không PII; lịch sử; tải lại đúng SHA-256; tệp có tiêu đề kỳ, họ tên, người lập/ký, chân trang.
	ex := f.call("hcth", http.MethodPost, "/v1/hrm/reports/NS01/export", map[string]any{"as_of": "2026-10-12", "preparer_id": e, "signer_id": a},
		http.StatusCreated)
	if strings.Contains(fmt.Sprint(ex), "Trần") || ex["file_name"] != "NS01_2026-10-12.xlsx" || ex["status"] != "warning" {
		t.Fatalf("xuất: %v", ex)
	}
	f.call("hcth", http.MethodPost, "/v1/hrm/reports/NS01/export", map[string]any{"as_of": "2027-01-01"}, http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/reports/NS01/export", map[string]any{"preparer_id": "018f0000-0000-7000-8000-000000000000"},
		http.StatusUnprocessableEntity)
	w, data := f.raw("hcth", "/v1/hrm/report-exports/"+ex["id"].(string)+"/file")
	if w.Code != http.StatusOK || w.Header().Get("X-Export-Drift") != "false" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("tải: %d %v %s", w.Code, w.Header(), data)
	}
	txt := xlsxText(t, data, "NS01")
	for _, want := range []string{"DANH SÁCH VIÊN CHỨC HIỆN CÓ MẶT", "THÁNG 10/2026", "Trần Thị Mai", "Phòng", "NGƯỜI LẬP BIỂU", "Võ Thị Em",
		"Xuất từ hệ thống AgriGreen", "Xã Mỹ Hòa Hưng", "Thạc sĩ Trồng trọt"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("tệp thiếu %q:\n%s", want, txt)
		}
	}
	if w, _ := f.raw("ld_tt", "/v1/hrm/report-exports/"+ex["id"].(string)+"/file"); w.Code != http.StatusForbidden {
		t.Fatalf("lãnh đạo phòng tải: %d", w.Code)
	}
	if w, _ := f.raw("hcth", "/v1/hrm/report-exports/018f0000-0000-7000-8000-000000000000/file"); w.Code != http.StatusNotFound {
		t.Fatalf("lần xuất lạ: %d", w.Code)
	}
	// Định danh đổi sau lần xuất → tải lại báo drift; nhật ký hồ sơ có lần xuất báo cáo (trường nhạy cảm).
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+a+"/profile", map[string]any{"full_name": "Trần Thị Mai Anh"}, http.StatusOK)
	if w, _ := f.raw("hcth", "/v1/hrm/report-exports/"+ex["id"].(string)+"/file"); w.Header().Get("X-Export-Drift") != "true" {
		t.Fatal("phải báo drift khi định danh đã đổi")
	}
	hist := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS01/history", nil, http.StatusOK)["items"].([]any)
	if len(hist) != 3 || hist[2].(map[string]any)["action"] != "export" || hist[0].(map[string]any)["drift"] != true ||
		len(hist[2].(map[string]any)["sensitive_fields"].([]any)) != 4 {
		t.Fatalf("lịch sử: %v", hist)
	}
	if last := f.call("hcth", http.MethodGet, "/v1/hrm/reports", nil, http.StatusOK)["items"].([]any)[0].(map[string]any); last["exports"] != jsonNum(1) {
		t.Fatalf("lần xuất gần nhất: %v", last)
	}
	logs := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+b+"/access-log", nil, http.StatusOK)["items"].([]any)
	if !strings.Contains(fmt.Sprint(logs), "NS01") {
		t.Fatalf("nhật ký hồ sơ thiếu lần xuất báo cáo: %v", logs)
	}

	// Dashboard: HCTH toàn Trung tâm — tổng = số dòng NS01 cùng ngày (AC-02); lãnh đạo Phòng TT chỉ phòng mình (BR-02).
	db := f.call("hcth", http.MethodGet, "/v1/hrm/dashboard?at=2026-10-12", nil, http.StatusOK)
	bt := db["by_type"].(map[string]any)
	if db["total"] != jsonNum(3) || db["staff"] != jsonNum(4) || bt["can_bo_xa"] != jsonNum(1) || db["left"] != jsonNum(1) ||
		db["over_retire"] != jsonNum(1) || db["pii"] != true {
		t.Fatalf("dashboard HCTH: %v", db)
	}
	dl := f.call("ld_tt", http.MethodGet, "/v1/hrm/dashboard?at=2026-10-12", nil, http.StatusOK)
	if dl["total"] != jsonNum(3) || dl["scope"] != "unit" || len(dl["by_unit"].([]any)) != 1 {
		t.Fatalf("dashboard lãnh đạo phòng: %v", dl)
	}
	f.call("vc_tt", http.MethodGet, "/v1/hrm/dashboard", nil, http.StatusForbidden)
	m := f.call("hcth", http.MethodGet, "/v1/hrm/dashboard/members?at=2026-10-12&metric=over_retire", nil, http.StatusOK)
	if m["total"] != jsonNum(1) || m["items"].([]any)[0].(map[string]any)["name"] != "Lê Văn Bình" {
		t.Fatalf("thành viên chỉ số: %v", m)
	}
	f.call("hcth", http.MethodGet, "/v1/hrm/dashboard/members?metric=bogus", nil, http.StatusUnprocessableEntity)
	// Người xem không có vai kho (403): dashboard vẫn chạy, pii = false; biểu Sở báo lỗi của kho.
	f.vault.Fail = func(op string) error {
		if op == "batch_read" {
			return problem.Forbidden
		}
		return nil
	}
	if p := f.call("hcth", http.MethodGet, "/v1/hrm/dashboard", nil, http.StatusOK); p["pii"] != false || p["total"] != jsonNum(3) {
		t.Fatalf("dashboard không vai kho: %v", p)
	}
	f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS03", nil, http.StatusForbidden)
	f.vault.Fail = nil
}

func jsonNum(n int) any { return json.Number(fmt.Sprint(n)) }

// W11.28 AC-01 FN-06: dashboard dưới 5 giây với 1.000 hồ sơ (BRD AC-01 FN-07 dùng cùng cỡ dữ liệu) — kiểm thử hiệu năng
// đơn giản (đo một lần gọi HTTP sau khi nạp dữ liệu; kho giả trong bộ nhớ).
func TestHRMDashboardBenchmark_W1128(t *testing.T) {
	if testing.Short() {
		t.Skip("kiểm thử hiệu năng")
	}
	f := newRecFixture(t, true)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	ctx := authz.WithPrincipal(context.Background(), f.who["hcth"])
	units := []string{"trong_trot", "chan_nuoi", "hcth", "tram_cho_moi"}
	const n = 1000
	for i := 0; i < n; i++ {
		gender, birth := "nam", fmt.Sprintf("%d-03-01", 1965+i%35)
		if i%2 == 1 {
			gender = "nu"
		}
		name, phone := "Cán Bộ "+string(rune('A'+i/26%26))+string(rune('a'+i%26))+"nh "+string(rune('A'+i/676)), fmt.Sprintf("09130%05d", i)
		typ := "vien_chuc"
		if i%10 == 9 {
			typ = "hop_dong"
		}
		_, err := f.svc.CreateStaff(ctx, app.CreateInput{Type: typ,
			Profile: app.ProfileInput{FullName: &name, Phone: &phone, Gender: &gender, BirthDate: &birth},
			Events: []domain.EventInput{
				{Kind: domain.EventUnit, ValidFrom: "2015-01-01", Payload: map[string]string{"unit": units[i%len(units)]}},
				{Kind: domain.EventPosition, ValidFrom: "2015-01-01", Payload: map[string]string{"position": "vien_chuc"}},
				{Kind: domain.EventWorkStart, ValidFrom: "2015-01-01", Payload: map[string]string{}},
				{Kind: domain.EventGrade, ValidFrom: "2015-01-01", Payload: map[string]string{"grade": "01.003", "step": "3"}},
				{Kind: domain.EventDegree, ValidFrom: "2010-06-01", Payload: map[string]string{"level": "dai_hoc", "major": "trong_trot"}},
			}}, "")
		if err != nil {
			t.Fatal(i, err)
		}
	}
	start := time.Now()
	d := f.call("hcth", http.MethodGet, "/v1/hrm/dashboard?at=2026-10-12", nil, http.StatusOK)
	el := time.Since(start)
	t.Logf("dashboard %d hồ sơ: %v (máy chủ báo %v ms)", n, el, d["elapsed_ms"])
	if d["staff"] != jsonNum(n) || el > 5*time.Second {
		t.Fatalf("dashboard: %v trong %v (yêu cầu < 5 s)", d["staff"], el)
	}
	start = time.Now()
	v := f.call("hcth", http.MethodGet, "/v1/hrm/reports/NS06?at=2026-10-12", nil, http.StatusOK)
	t.Logf("NS06 %d hồ sơ: %v, trạng thái %v", n, time.Since(start), v["status"])
}
