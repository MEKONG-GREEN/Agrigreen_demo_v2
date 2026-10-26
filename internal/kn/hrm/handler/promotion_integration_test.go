package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func promoRules(quotaPct string) map[string]any {
	return map[string]any{"valid_from": "2026-06-01", "note": "W11.25 kiểm thử: chỉ tiêu NLTH",
		"payload": map[string]any{"min_rating": "HTNV", "rating_years": 3, "achievement_years": 5,
			"discipline_extension_months": map[string]any{"khien_trach": 6, "canh_cao": 6, "cach_chuc": 12}, "nlth_quota_pct": quotaPct,
			"nlth_retirement_months": 12, "nlth_retirement_within_months": 12, "tnvk_start_pct": "5", "tnvk_step_pct": "1", "tnvk_cycle_months": 12,
			"tnvk_first_months": 36, "remind_months": []int{1, 3, 6}, "retirement_age_male_months": 738, "retirement_age_female_months": 684}}
}

func items(m map[string]any, key string) []map[string]any {
	var out []map[string]any
	if xs, ok := m[key].([]any); ok {
		for _, x := range xs {
			out = append(out, x.(map[string]any))
		}
	}
	return out
}

func byStaff(xs []map[string]any, staff, kind string) map[string]any {
	for _, x := range xs {
		if x["staff_id"] == staff && (kind == "" || x["kind"] == kind) {
			return x
		}
	}
	return nil
}

func (f *recFixture) multipartFile(as, path, name string, content []byte, want int) map[string]any {
	f.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(content)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Test-As", as)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != want {
		f.t.Fatalf("%s POST %s: %d (muốn %d) %s", as, path, rec.Code, want, rec.Body.String())
	}
	return m
}

// W11.25 (FN-09, FN-10) qua HTTP trên PostgreSQL thật: tính kỳ NLTX/TNVK/NLTH từ mốc xếp bậc + bảng hệ số + tham số, điều
// kiện (AC-01 KHTNV → Chưa đủ kèm lý do), lọc theo khung 1/3 tháng (AC-05), đưa vào danh sách bắt buộc lý do, NLTH từ
// thành tích + chỉ tiêu (AC-03), xuất tự chia Mẫu số 1/2/4 (AC-02: 20 cột, cột 19 trống, hệ số chênh lệch), tải lại có
// ngày/người xuất + SHA-256 (AC-04), trạng thái xử lý (AC-06), sổ quyết định cập nhật bậc cùng giao dịch (FN-10 AC-01),
// người không có tên trở lại bảng theo dõi (BR-02), tìm theo số/năm/tên (AC-02), xuất sổ Excel (AC-03), hủy = sự kiện
// bù khôi phục bậc, tệp vào Kho tài liệu, job tính mốc hằng ngày, quyền.
func TestHRMPromotionHTTP_W1125(t *testing.T) {
	off := newRecFixture(t, false)
	if p := off.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusForbidden); p["code"] != "hrm.records_disabled" {
		t.Fatalf("tắt: %v", p)
	}

	f := newRecFixture(t, true)
	withDocs(t, f)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusCreated)
	f.call("admin", http.MethodPost, "/v1/hrm/rules/promotion", promoRules("20"), http.StatusCreated) // 5 viên chức × 20 % = 1

	tt := ev("unit", "2015-03-01", map[string]any{"unit": "trong_trot"})
	vc := ev("position", "2015-03-01", map[string]any{"position": "vien_chuc"})
	start := ev("work_start", "2015-03-01", map[string]any{})
	grade := func(step, since string) map[string]any {
		return ev("grade", "2015-03-01", map[string]any{"grade": "01.003", "step": step, "step_since": since})
	}
	mk := func(name, phone, gender string, extra ...map[string]any) string {
		return f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("vien_chuc", name, phone, gender, "1980-03-04", nil,
			append([]map[string]any{tt, vc, start}, extra...)...), http.StatusCreated)["id"].(string)
	}
	a := mk("Trần Văn An", "0913000001", "nam", grade("5", "2023-11-01"))
	b := mk("Lê Thị Bé", "0913000002", "nu", grade("4", "2023-12-15"))
	cc := mk("Phạm Văn Cuối", "0913000003", "nam", grade("9", "2023-06-01"))
	d := mk("Võ Thị Dung", "0913000004", "nu", grade("3", "2024-02-01"),
		ev("award", "2024", map[string]any{"form": "bang_khen_ubnd_tinh", "title": "Bằng khen UBND tỉnh"}))
	e := mk("Ngô Văn Em", "0913000005", "nam", grade("2", "2024-06-01"), ev("award", "2025", map[string]any{"form": "bang_khen_bo"}))
	rate := func(id string, ys map[int]string) {
		for y, r := range ys {
			f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id+"/ratings", map[string]any{"year": y, "rating": r, "source": "conversion"}, http.StatusCreated)
		}
	}
	ok3 := map[int]string{2023: "HTNV", 2024: "HTTNV", 2025: "HTXSNV"}
	rate(a, ok3)
	rate(cc, ok3)
	rate(d, ok3)
	rate(b, map[int]string{2023: "HTNV", 2024: "KHTNV", 2025: "HTNV"})
	// Sửa xếp loại đã có cần lý do; lịch sử giữ dòng cũ.
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+a+"/ratings", map[string]any{"year": 2025, "rating": "HTTNV"}, http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+a+"/ratings", map[string]any{"year": 2025, "rating": "HTTNV",
		"reason": "Nhập nhầm từ dữ liệu chuyển đổi"}, http.StatusCreated)
	if xs := items(f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+a+"/ratings", nil, http.StatusOK), "items"); len(xs) != 4 {
		t.Fatalf("lịch sử xếp loại: %v", xs)
	}

	// Quyền: lãnh đạo phòng, viên chức, Cấp Xã/TW không có màn nâng bậc; ngữ cảnh chỉ xem không ghi.
	for _, as := range []string{"ld_tt", "vc_tt", "xa", "tw"} {
		f.call(as, http.MethodGet, "/v1/hrm/promotion", nil, http.StatusForbidden)
	}
	f.call("viewonly", http.MethodPost, "/v1/hrm/promotion/cases/include", map[string]any{"keys": []string{a + ":nltx:b6-20261101"}},
		http.StatusForbidden)

	// AC-05: khung 1 tháng (đến 12/11/2026): A (01/11/2026) + C TNVK quá hạn (01/06/2026); B (15/12/2026) chỉ có ở khung 3 tháng.
	l1 := f.call("hcth", http.MethodGet, "/v1/hrm/promotion?window=1", nil, http.StatusOK)
	x1 := items(l1, "items")
	if len(x1) != 2 || byStaff(x1, a, "nltx") == nil || byStaff(x1, cc, "tnvk") == nil {
		t.Fatalf("khung 1 tháng: %v", x1)
	}
	l3 := f.call("hcth", http.MethodGet, "/v1/hrm/promotion?window=3", nil, http.StatusOK)
	x3 := items(l3, "items")
	ra, rb, rc := byStaff(x3, a, "nltx"), byStaff(x3, b, "nltx"), byStaff(x3, cc, "tnvk")
	if ra == nil || rb == nil || rc == nil || len(x3) != 3 {
		t.Fatalf("khung 3 tháng: %v", x3)
	}
	if ra["due"] != "2026-11-01" || ra["from_coeff"] != "3.66" || ra["to_coeff"] != "3.99" || ra["diff"] != "0.33" || ra["cond"] != "ok" ||
		ra["status"] != "pending" || ra["name"] != "Trần Văn An" || ra["months_enjoyed"] != "1" {
		t.Fatalf("A: %v", ra)
	}
	// AC-01: KHTNV trong thời gian giữ bậc → Chưa đủ kèm lý do.
	if rb["cond"] != "fail" || !strings.Contains(strings.Join(func() []string {
		var s []string
		for _, w := range rb["why"].([]any) {
			s = append(s, w.(string))
		}
		return s
	}(), ","), "KHTNV 2024") {
		t.Fatalf("B: %v", rb)
	}
	if rc["due"] != "2026-06-01" || rc["pct_to"] != "5" || rc["pct_from"] != nil || rc["diff"] != "0.25" {
		t.Fatalf("C TNVK: %v", rc)
	}
	sum := l3["summary"].(map[string]any)
	if sum["total"] != json.Number("3") || sum["nltx"] != json.Number("2") || sum["tnvk"] != json.Number("1") ||
		sum["quota"].(map[string]any)["total"] != json.Number("1") {
		t.Fatalf("khối chỉ số: %v", sum)
	}
	det := f.call("hcth", http.MethodGet, "/v1/hrm/promotion/cases/"+rb["key"].(string), nil, http.StatusOK)
	if len(det["basis"].([]any)) != 4 || len(det["ratings"].([]any)) != 3 || det["_cache"] != nil {
		t.Fatalf("chi tiết B: %v", det)
	}
	f.call("hcth", http.MethodGet, "/v1/hrm/promotion/cases/"+uuid.NewString()+":nltx:b2-20260101", nil, http.StatusNotFound)

	// Đưa vào danh sách: "Chưa đủ" bắt buộc lý do (luồng ngoại lệ FN-09).
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/cases/include", map[string]any{"keys": []any{rb["key"]}}, http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/cases/include", map[string]any{"keys": []any{rb["key"]},
		"reason": "Xếp loại 2024 đang được xem xét lại"}, http.StatusOK)
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/cases/include", map[string]any{"keys": []any{ra["key"]}}, http.StatusOK)
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/cases/exclude", map[string]any{"keys": []any{ra["key"]}}, http.StatusOK)

	// NLTH: danh sách chọn có D (bằng khen UBND tỉnh 2024 → 12 tháng: 01/02/2027 − 12 = 01/02/2026) và E.
	pool := f.call("hcth", http.MethodGet, "/v1/hrm/promotion/nlth-pool", nil, http.StatusOK)
	var dAward, eAward string
	for _, r := range items(pool, "rows") {
		for _, o := range items(r, "options") {
			aw := o["award"].(map[string]any)
			switch r["staff_id"] {
			case d:
				if o["due"] != "2026-02-01" || aw["months"] != json.Number("12") || o["cond"] != "ok" {
					t.Fatalf("NLTH D: %v", o)
				}
				dAward = aw["event_id"].(string)
			case e:
				eAward = aw["event_id"].(string)
			}
		}
		if r["staff_id"] == a || r["staff_id"] == b {
			t.Fatalf("người đang đến kỳ thường xuyên không vào danh sách chọn NLTH: %v", r)
		}
	}
	if dAward == "" || eAward == "" {
		t.Fatalf("pool: %v", pool)
	}
	dk := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/proposals", map[string]any{"items": []any{map[string]any{"staff_id": d,
		"award_event_id": dAward}}}, http.StatusCreated)["keys"].([]any)[0].(string)
	// AC-03: E (01/06/2026) vượt chỉ tiêu 1 người của năm 2026.
	if p := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/proposals", map[string]any{"items": []any{map[string]any{"staff_id": e,
		"award_event_id": eAward}}, "reason": "Đề xuất theo thành tích Bộ"}, http.StatusUnprocessableEntity); p["code"] != "hrm.nlth_quota_exceeded" {
		t.Fatalf("chỉ tiêu: %v", p)
	}

	// Xuất: hệ thống tự chia 4 kỳ thành Mẫu số 1 (A, B), 2 (D), 4 (C — % TNVK lần đầu = khởi điểm).
	exp := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/exports", map[string]any{"keys": []string{ra["key"].(string), rb["key"].(string),
		rc["key"].(string), dk}, "review_month": "2026-11", "preparer_id": a, "signer_title": "Phó Giám đốc",
		"notes": map[string]any{ra["key"].(string): "Đủ điều kiện"}}, http.StatusCreated)
	files := map[string]map[string]any{}
	for _, x := range items(exp, "items") {
		files[x["form"].(string)] = x
	}
	if len(files) != 3 || files["mau1"]["count"] != json.Number("2") || files["mau1"]["file_name"] != "MauSo1_T11_2026.xlsx" {
		t.Fatalf("xuất: %v", exp)
	}
	w, body := f.raw("hcth", "/v1/hrm/promotion/exports/"+files["mau1"]["id"].(string)+"/file")
	if w.Code != http.StatusOK || w.Header().Get("X-Export-Drift") != "false" || w.Header().Get("X-Export-Sha256") != files["mau1"]["sha256"] {
		t.Fatalf("tải lại Mẫu số 1: %d %v", w.Code, w.Header())
	}
	txt := xlsxText(t, body, "mau1")
	for _, want := range []string{"MẪU SỐ 1", "THÁNG 11/2026", "Trần Văn An - ", "|3,66|", "|3,99|", "|0,33|", "|01/11/2026|", "Đủ điều kiện",
		"Lê Thị Bé", "PHÓ GIÁM ĐỐC", "|20\n"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("Mẫu số 1 thiếu %q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "|21|") || !strings.Contains(txt, "|1||Đủ điều kiện") { // cột 19 trống trước Ghi chú
		t.Fatalf("Mẫu số 1 cột 19:\n%s", txt)
	}
	_, b4 := f.raw("hcth", "/v1/hrm/promotion/exports/"+files["mau4"]["id"].(string)+"/file")
	if t4 := xlsxText(t, b4, "mau4"); !strings.Contains(t4, "Phạm Văn Cuối") || !strings.Contains(t4, "|5|01/06/2027|0,25|") {
		t.Fatalf("Mẫu số 4:\n%s", t4)
	}
	_, b2 := f.raw("hcth", "/v1/hrm/promotion/exports/"+files["mau2"]["id"].(string)+"/file")
	if t2 := xlsxText(t, b2, "mau2"); !strings.Contains(t2, "Bằng khen UBND tỉnh 2024|12") || !strings.Contains(t2, "|01/02/2026|") {
		t.Fatalf("Mẫu số 2:\n%s", t2)
	}
	// AC-04: lịch sử xuất có ngày + người xuất; AC-06: trạng thái "Đã xuất danh sách".
	hist := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion/exports", nil, http.StatusOK), "items")
	if len(hist) != 3 || hist[0]["actor"] != f.who["hcth"].ActorRef.String() || hist[0]["recorded_at"] == "" {
		t.Fatalf("file đã xuất: %v", hist)
	}
	x3 = items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?window=3", nil, http.StatusOK), "items")
	for _, x := range x3 {
		if x["status"] != "exported" {
			t.Fatalf("trạng thái sau xuất: %v", x)
		}
	}

	// FN-10: quyết định từ Mẫu số 1 chỉ có A (B không có tên → trở lại bảng theo dõi kèm ghi chú).
	m1 := files["mau1"]["id"].(string)
	src := f.call("hcth", http.MethodGet, "/v1/hrm/promotion/exports/"+m1, nil, http.StatusOK)
	if len(items(src, "items")) != 2 || src["_cache"] != nil {
		t.Fatalf("danh sách gốc: %v", src)
	}
	dec := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{"export_id": m1, "decision_no": "540/QĐ-SNNMT",
		"decided_on": "2026-10-10", "issuer": "Sở NN&MT", "people": []any{map[string]any{"case_key": ra["key"]}}}, http.StatusCreated)
	did := dec["id"].(string)
	if dec["updated"] != json.Number("1") || dec["returned"] != json.Number("1") {
		t.Fatalf("lưu quyết định: %v", dec)
	}
	cur := func(id, at string) map[string]any {
		return f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id+"?at="+at, nil, http.StatusOK)["current"].(map[string]any)
	}
	if c := cur(a, "2026-11-02"); c["step"] != json.Number("6") || c["coefficient"] != "3.99" || c["step_since"] != "2026-11-01" {
		t.Fatalf("A sau quyết định: %v", c)
	}
	if c := cur(a, "2026-10-31"); c["step"] != json.Number("5") {
		t.Fatalf("A trước ngày hưởng: %v", c)
	}
	// Danh sách gốc đã có quyết định → không lập quyết định thứ hai; B trở lại "Chưa xử lý" kèm ghi chú.
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{"export_id": m1, "decision_no": "541/QĐ-SNNMT",
		"decided_on": "2026-10-10", "issuer": "Sở NN&MT", "people": []any{map[string]any{"case_key": rb["key"]}}}, http.StatusConflict)
	x3 = items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?window=3", nil, http.StatusOK), "items")
	if xb := byStaff(x3, b, "nltx"); xb["status"] != "pending" || !strings.Contains(xb["note"].(string), "540/QĐ-SNNMT") {
		t.Fatalf("B sau quyết định: %v", xb)
	}
	if xa := byStaff(x3, a, "nltx"); xa == nil || xa["status"] != "decided" || xa["decision_no"] != "540/QĐ-SNNMT" {
		t.Fatalf("A đã có quyết định: %v", x3)
	}

	// Quyết định NLTH (D) — thành tích đánh dấu đã dùng; TNVK (C) — lần sau đọc % từ sổ quyết định.
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{"export_id": files["mau2"]["id"], "decision_no": "541/QĐ-SNNMT",
		"decided_on": "2026-10-11", "issuer": "Sở NN&MT", "people": []any{map[string]any{"case_key": dk}}}, http.StatusCreated)
	if c := cur(d, "2026-10-12"); c["step"] != json.Number("4") || c["step_since"] != "2026-02-01" {
		t.Fatalf("D sau NLTH: %v", c)
	}
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{"export_id": files["mau4"]["id"], "decision_no": "542/QĐ-SNNMT",
		"decided_on": "2026-10-11", "issuer": "Sở NN&MT", "people": []any{map[string]any{"case_key": rc["key"]}}}, http.StatusCreated)
	x12 := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?window=12", nil, http.StatusOK), "items")
	if xc := byStaff(x12, cc, "tnvk"); xc == nil || xc["status"] != "pending" || xc["due"] != "2027-06-01" || xc["pct_from"] != "5" || xc["pct_to"] != "6" {
		var all []any
		for _, x := range x12 {
			all = append(all, x["staff_id"], x["kind"], x["status"], x["due"])
		}
		t.Fatalf("TNVK lần sau: %v", all)
	}
	pool = f.call("hcth", http.MethodGet, "/v1/hrm/promotion/nlth-pool", nil, http.StatusOK)
	for _, r := range items(pool, "rows") {
		if r["staff_id"] == d && len(items(r, "options")) != 0 {
			t.Fatalf("thành tích đã dùng vẫn chọn được: %v", r)
		}
	}

	// Nhập quyết định đã ban hành: số trùng → cảnh báo, gửi lại với acknowledge; bậc vượt ngạch → chặn.
	old := map[string]any{"kind": "nltx", "decision_no": "540/qđ-snnmt", "decided_on": "2026-03-01", "issuer": "UBND tỉnh An Giang",
		"people": []any{map[string]any{"staff_id": e, "to_step": 3, "effective_on": "2026-03-01"}}}
	if p := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", old, http.StatusConflict); p["code"] != "hrm.decision_warnings" {
		t.Fatalf("cảnh báo số trùng: %v", p)
	}
	old["people"] = []any{map[string]any{"staff_id": e, "to_step": 10, "effective_on": "2026-03-01"}}
	old["acknowledge_warnings"] = true
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", old, http.StatusUnprocessableEntity)
	old["people"] = []any{map[string]any{"staff_id": e, "to_step": 3, "effective_on": "2026-03-01"}}
	oldID := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", old, http.StatusCreated)["id"].(string)
	if c := cur(e, "2026-10-12"); c["step"] != json.Number("3") {
		t.Fatalf("E sau quyết định nhập: %v", c)
	}

	// Sổ quyết định: tìm theo số, năm, tên viên chức (AC-02); xuất Excel (AC-03).
	if xs := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion/decisions?year=2026", nil, http.StatusOK), "items"); len(xs) != 4 {
		t.Fatalf("sổ 2026: %d", len(xs))
	}
	if xs := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion/decisions?q=dung", nil, http.StatusOK), "items"); len(xs) != 1 ||
		xs[0]["decision_no"] != "541/QĐ-SNNMT" {
		t.Fatalf("tìm theo tên: %v", xs)
	}
	if xs := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion/decisions?q=542", nil, http.StatusOK), "items"); len(xs) != 1 {
		t.Fatalf("tìm theo số: %v", xs)
	}
	f.call("hcth", http.MethodGet, "/v1/hrm/promotion/decisions?year=2025", nil, http.StatusOK)
	wb, bb := f.raw("hcth", "/v1/hrm/promotion/decision-book?year=2026")
	if wb.Code != http.StatusOK || !strings.Contains(xlsxText(t, bb, "SO_QD"), "542/QĐ-SNNMT") {
		t.Fatalf("sổ Excel: %d", wb.Code)
	}

	// Tệp quyết định → Kho tài liệu (thư mục "Quyết định nâng bậc", gắn người có tên).
	doc := f.multipartFile("hcth", "/v1/hrm/promotion/decisions/"+did+"/file", "540.pdf", pdf, http.StatusCreated)["doc_id"].(string)
	dv := f.call("hcth", http.MethodGet, "/v1/hrm/promotion/decisions/"+did, nil, http.StatusOK)
	if dv["doc_id"] != doc || dv["status"] != "applied" || len(dv["history"].([]any)) != 2 {
		t.Fatalf("quyết định có tệp: %v", dv)
	}
	if xs := items(f.call("hcth", http.MethodGet, "/v1/hrm/docs?staff="+a, nil, http.StatusOK), "items"); len(xs) != 1 {
		t.Fatalf("kho tài liệu: %v", xs)
	}

	// Hủy = sự kiện bù: lý do ≥ 10 ký tự; bậc A về 5; kỳ A về "Đã xuất danh sách"; hủy lại → 409. Hủy quyết định D trước
	// quyết định nhập sau cho cùng người? (E có một quyết định — không chặn.)
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions/"+did+"/cancel", map[string]any{"reason": "sai"}, http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions/"+did+"/cancel", map[string]any{"reason": "Nhập sai số quyết định"}, http.StatusOK)
	if c := cur(a, "2026-11-02"); c["step"] != json.Number("5") || c["coefficient"] != "3.66" {
		t.Fatalf("A sau hủy: %v", c)
	}
	if p := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions/"+did+"/cancel", map[string]any{"reason": "Hủy lần thứ hai"},
		http.StatusConflict); p["code"] != "hrm.decision_cancelled" {
		t.Fatalf("hủy lại: %v", p)
	}
	x3 = items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?window=3", nil, http.StatusOK), "items")
	if xa := byStaff(x3, a, "nltx"); xa == nil || xa["status"] != "exported" {
		t.Fatalf("A sau hủy: %v", xa)
	}
	if dv := f.call("hcth", http.MethodGet, "/v1/hrm/promotion/decisions/"+did, nil, http.StatusOK); dv["status"] != "cancelled" ||
		dv["cancel_reason"] != "Nhập sai số quyết định" {
		t.Fatalf("quyết định đã hủy: %v", dv)
	}
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions/"+oldID+"/cancel", map[string]any{"reason": "Quyết định nhập nhầm người"}, http.StatusOK)

	// Job tính mốc hằng ngày: một dòng mỗi ngày; chạy lại cùng ngày không ghi thêm.
	r1, err := f.svc.PromotionScan(t.Context(), f.tid, time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC), nil)
	if err != nil || !r1.Fresh || r1.Counts["due_3"] != 2 || r1.Counts["fail"] != 1 {
		t.Fatalf("job: %+v %v", r1, err)
	}
	if r2, err := f.svc.PromotionScan(t.Context(), f.tid, time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), nil); err != nil || r2.Fresh {
		t.Fatalf("job lặp: %+v %v", r2, err)
	}
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusOK); l["last_scan"].(map[string]any)["day"] != "2026-10-12" {
		t.Fatalf("lần tính gần nhất: %v", l["last_scan"])
	}

	// Sổ chỉ ghi thêm; không PII trong sổ nâng bậc (RB-3); tenant khác không thấy.
	for _, q := range []string{"UPDATE pf_hrm.promotion_decision SET issuer = 'x'", "DELETE FROM pf_hrm.promotion_event",
		"UPDATE pf_hrm.promotion_export SET file_name = 'x.xlsx'", "DELETE FROM pf_hrm.annual_rating", "DELETE FROM pf_hrm.promotion_scan"} {
		err := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
			_, e := tx.Exec(context.Background(), q)
			return e
		})
		if err == nil {
			t.Fatalf("%s phải thất bại", q)
		}
	}
	var dump string
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT concat_ws(' ',
	    (SELECT string_agg(to_jsonb(x)::text, ' ') FROM pf_hrm.promotion_export x WHERE tenant_id = $1),
	    (SELECT string_agg(to_jsonb(x)::text, ' ') FROM pf_hrm.promotion_decision x WHERE tenant_id = $1),
	    (SELECT string_agg(to_jsonb(x)::text, ' ') FROM pf_hrm.promotion_event x WHERE tenant_id = $1))`, f.tid).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	for _, pii := range []string{"Trần Văn An", "Lê Thị Bé", "1980", "0913000001"} {
		if strings.Contains(dump, pii) {
			t.Fatalf("PII %q trong sổ nâng bậc", pii)
		}
	}
	if xs := items(f.call("other", http.MethodGet, "/v1/hrm/promotion/decisions", nil, http.StatusOK), "items"); len(xs) != 0 {
		t.Fatalf("tenant khác: %v", xs)
	}
}
