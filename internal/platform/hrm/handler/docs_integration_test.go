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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// denyConsent: đồng ý của hộ luôn "chưa có" — tài liệu nhân sự (hr_records) không được phụ thuộc đồng ý.
type denyConsent struct{}

func (denyConsent) HasConsent(context.Context, uuid.UUID, string) (bool, error) { return false, nil }

// withDocs nối pf/evidence thật (kho đối tượng cục bộ, kho định danh giả) cho fixture hồ sơ; người dùng có vai kho.
func withDocs(t *testing.T, f *recFixture) {
	objs, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{PresignSecret: []byte("presign-secret-kiem-thu-0123456789"),
		BaseURL: "http://objstore.test"})
	if err != nil {
		t.Fatal(err)
	}
	ev := evidence.New(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, evidence.Deps{Store: objs, Vault: f.vault,
		Consent: denyConsent{}})
	ev.UseResolver(port.SubjectModule, app.FileResolver{S: f.svc})
	f.svc.UseDocs(ev.Files())
	for k, p := range f.who {
		if k != "norole" {
			p.Roles = append(p.Roles, client.RoleRegistrar, client.RoleEvidenceViewer)
		}
		f.who[k] = p
	}
	p := f.who["ld_tt"]
	p.ActorRef, p.Roles = ids.New(), nil
	f.who["norole"] = p
}

func (f *recFixture) upload(as string, meta map[string]any, name string, content []byte, want int) map[string]any {
	f.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	raw, _ := json.Marshal(meta)
	_ = w.WriteField("meta", string(raw))
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(content)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/hrm/docs", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Test-As", as)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != want {
		f.t.Fatalf("%s upload %s: %d (muốn %d) %s", as, name, rec.Code, want, rec.Body.String())
	}
	return m
}

func (f *recFixture) raw(as, path string, want int) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-As", as)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != want {
		f.t.Fatalf("%s GET %s: %d (muốn %d) %s", as, path, rec.Code, want, rec.Body.String())
	}
	return rec
}

func folderByKey(t *testing.T, l map[string]any, key string) string {
	t.Helper()
	for _, it := range l["items"].([]any) {
		m := it.(map[string]any)
		if m["system_key"] == key {
			return m["folder_id"].(string)
		}
	}
	t.Fatalf("không có thư mục %s: %v", key, l)
	return ""
}

func docTitles(l map[string]any) []string {
	var out []string
	for _, it := range l["items"].([]any) {
		out = append(out, it.(map[string]any)["title"].(string))
	}
	return out
}

var pdf = []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\ntrailer\n%%EOF\n")

// W11.24 (FN-14) qua HTTP: cây thư mục (gốc Admin, mặc định), phân loại, tệp qua pf/evidence (hr_records, mã hóa phong bì,
// không phụ thuộc đồng ý), AC-01 lọc theo nhân viên, AC-02 lãnh đạo phòng chỉ thấy phòng mình, AC-03 tải xuống ghi nhật ký,
// quyền thư mục ghi đè ma trận FN-20, xóa = ẩn (lý do, lưu vết), AG_HRM_RECORDS cho tài liệu gắn nhân viên, sổ chỉ ghi thêm.
func TestHRMDocsHTTP_W1124(t *testing.T) {
	// Hồ sơ TẮT: kho tài liệu vẫn chạy cho tài liệu không gắn nhân viên; gắn nhân viên → hrm.records_disabled.
	off := newRecFixture(t, false)
	withDocs(t, off)
	off.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusCreated)
	lo := off.call("hcth", http.MethodGet, "/v1/hrm/docs/folders", nil, http.StatusOK)
	if lo["records_enabled"] != false || lo["ready"] != true {
		t.Fatalf("tắt: %v", lo)
	}
	khacOff := folderByKey(t, lo, "khac")
	off.upload("hcth", map[string]any{"folder_id": khacOff, "title": "Nội quy cơ quan", "doc_type": "khac"}, "noi-quy.pdf", pdf, http.StatusCreated)
	if p := off.upload("hcth", map[string]any{"folder_id": khacOff, "title": "QĐ", "doc_type": "khac", "staff_ids": []string{ids.New().String()}},
		"qd.pdf", pdf, http.StatusForbidden); p["code"] != "hrm.records_disabled" {
		t.Fatalf("gắn nhân viên khi tắt: %v", p)
	}
	off.call("hcth", http.MethodGet, "/v1/hrm/docs?staff="+ids.New().String(), nil, http.StatusForbidden)

	f := newRecFixture(t, true)
	withDocs(t, f)
	f.call("", http.MethodGet, "/v1/hrm/docs/folders", nil, http.StatusUnauthorized)
	f.call("realmonly", http.MethodGet, "/v1/hrm/docs/folders", nil, http.StatusForbidden)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)

	// Cấu trúc gốc: chỉ Admin (FN-14 điều kiện tiên quyết); cài mặc định lặp lại không nhân đôi.
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusForbidden)
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders", map[string]any{"name": "Gốc của HCTH"}, http.StatusForbidden)
	if r := f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusCreated); r["created"] != json.Number("7") {
		t.Fatalf("mặc định: %v", r)
	}
	if r := f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusCreated); r["created"] != json.Number("0") {
		t.Fatalf("mặc định lặp: %v", r)
	}
	l := f.call("hcth", http.MethodGet, "/v1/hrm/docs/folders", nil, http.StatusOK)
	if l["can_create_root"] != false || len(l["items"].([]any)) != 7 {
		t.Fatalf("cây HCTH: %v", l)
	}
	nangBac, khac := folderByKey(t, l, "nang_bac"), folderByKey(t, l, "khac")
	// HCTH tạo, đổi tên thư mục con (B2); trùng tên cùng cấp → 409; lãnh đạo phòng không tạo được.
	sub := f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders", map[string]any{"parent_id": nangBac, "name": "Năm 2026"}, http.StatusCreated)
	subID := sub["folder_id"].(string)
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders", map[string]any{"parent_id": nangBac, "name": "năm 2026"}, http.StatusConflict)
	f.call("ld_tt", http.MethodPost, "/v1/hrm/docs/folders", map[string]any{"parent_id": nangBac, "name": "Khác"}, http.StatusForbidden)
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders/"+subID+"/rename", map[string]any{"name": "Đợt 1 năm 2026"}, http.StatusOK)
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders/"+nangBac+"/rename", map[string]any{"name": "QĐ nâng bậc"}, http.StatusForbidden)

	// Hồ sơ nhân viên (W11.22) để phân loại theo nhân viên.
	unitTT := ev("unit", "2020-01-01", map[string]any{"unit": "trong_trot"})
	vc := ev("position", "2020-01-01", map[string]any{"position": "vien_chuc"})
	s1 := f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Trần Thị Mai", "0912000111", unitTT, vc), http.StatusCreated)
	s2 := f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Lê Văn Bình", "0912000222",
		ev("unit", "2021-01-01", map[string]any{"unit": "chan_nuoi"}), vc), http.StatusCreated)
	id1, id2 := s1["id"].(string), s2["id"].(string)

	// Tải lên (B3): định dạng, dung lượng; phân loại; phòng ban mặc định = đơn vị công tác của nhân viên.
	if p := f.upload("hcth", map[string]any{"folder_id": subID, "title": "Virus", "doc_type": "nang_bac"}, "a.exe", []byte("MZ...."),
		http.StatusUnprocessableEntity); p["code"] != "hrm.doc_format" {
		t.Fatalf("định dạng: %v", p)
	}
	f.upload("hcth", map[string]any{"folder_id": subID, "title": "Giả PDF", "doc_type": "nang_bac"}, "a.pdf", []byte("PK\x03\x04"),
		http.StatusUnprocessableEntity)
	f.upload("hcth", map[string]any{"folder_id": subID, "title": "QĐ", "doc_type": "sai"}, "a.pdf", pdf, http.StatusUnprocessableEntity)
	f.upload("ld_tt", map[string]any{"folder_id": subID, "title": "QĐ", "doc_type": "nang_bac"}, "a.pdf", pdf, http.StatusForbidden)
	d1 := f.upload("hcth", map[string]any{"folder_id": subID, "title": "QĐ nâng bậc lương tháng 7/2026", "doc_type": "nang_bac",
		"staff_ids": []string{id1, id2}, "doc_no": "123/QĐ-TTKN", "issued_on": "2026-07-15"}, "QĐ 123.pdf", pdf, http.StatusCreated)
	doc1 := d1["doc_id"].(string)
	d2 := f.upload("hcth", map[string]any{"folder_id": khac, "title": "Biên bản họp phòng Chăn nuôi", "doc_type": "khac",
		"unit_code": "chan_nuoi", "year": 2026}, "bien-ban.docx", []byte("PK\x03\x04word"), http.StatusCreated)
	doc2 := d2["doc_id"].(string)
	f.upload("hcth", map[string]any{"folder_id": khac, "title": "Đơn vị lạ", "doc_type": "khac", "unit_code": "da_ngung"}, "x.pdf", pdf,
		http.StatusUnprocessableEntity)

	g := f.call("hcth", http.MethodGet, "/v1/hrm/docs/"+doc1, nil, http.StatusOK)
	dd := g["doc"].(map[string]any)
	if dd["unit_code"] != "trong_trot" || dd["year"] != json.Number("2026") || dd["content_type"] != "application/pdf" ||
		strings.Join(toStrs(dd["staff_codes"]), ",") != "VC-00001,VC-00002" || strings.Join(toStrs(dd["folder_path"]), "/") != "Quyết định nâng bậc/Đợt 1 năm 2026" {
		t.Fatalf("chi tiết: %v", dd)
	}

	// AC-01: lọc theo nhân viên; tra cứu theo tên/số QĐ, loại, năm.
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/docs?staff="+id2, nil, http.StatusOK); l["total"] != json.Number("1") {
		t.Fatalf("theo nhân viên: %v", l)
	}
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/docs?q=123/qđ", nil, http.StatusOK); l["total"] != json.Number("1") {
		t.Fatalf("theo số QĐ: %v", l)
	}
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/docs?folder="+nangBac+"&recursive=1&doc_type=nang_bac&year=2026", nil, http.StatusOK); l["total"] != json.Number("1") {
		t.Fatalf("thư mục đệ quy: %v", l)
	}
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/docs?folder="+nangBac, nil, http.StatusOK); l["total"] != json.Number("0") {
		t.Fatalf("thư mục không đệ quy: %v", l)
	}

	// AC-02: lãnh đạo phòng chỉ thấy tài liệu phòng mình; Cấp Xã, Cấp TW không thấy.
	if l := f.call("ld_tt", http.MethodGet, "/v1/hrm/docs", nil, http.StatusOK); strings.Join(docTitles(l), "|") != "QĐ nâng bậc lương tháng 7/2026" {
		t.Fatalf("lãnh đạo Trồng trọt: %v", l)
	}
	if l := f.call("ld_cn", http.MethodGet, "/v1/hrm/docs", nil, http.StatusOK); strings.Join(docTitles(l), "|") != "Biên bản họp phòng Chăn nuôi" {
		t.Fatalf("lãnh đạo Chăn nuôi: %v", l)
	}
	f.call("ld_cn", http.MethodGet, "/v1/hrm/docs/"+doc1, nil, http.StatusNotFound)
	for _, who := range []string{"xa", "tw", "vc_tt"} {
		if l := f.call(who, http.MethodGet, "/v1/hrm/docs", nil, http.StatusOK); l["total"] != json.Number("0") {
			t.Fatalf("%s: %v", who, l)
		}
		if l := f.call(who, http.MethodGet, "/v1/hrm/docs/folders", nil, http.StatusOK); len(l["items"].([]any)) != 0 {
			t.Fatalf("%s cây: %v", who, l)
		}
	}
	f.call("ld_tt", http.MethodPost, "/v1/hrm/docs/"+doc1+"/hide", map[string]any{"reason": "Lãnh đạo phòng thử ẩn"}, http.StatusForbidden)

	// Xem trước/tải xuống: giải mã qua pf/evidence (hr_records) — đúng nội dung; AC-03 nhật ký.
	rec := f.raw("ld_tt", "/v1/hrm/docs/"+doc1+"/content?download=1", http.StatusOK)
	if !bytes.Equal(rec.Body.Bytes(), pdf) || rec.Header().Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment; filename*=UTF-8''Q%C4%90%20123.pdf") {
		t.Fatalf("tải xuống: %q %v", rec.Body.String(), rec.Header())
	}
	if rec := f.raw("hcth", "/v1/hrm/docs/"+doc2+"/content", http.StatusOK); !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("xem trước: %v", rec.Header())
	}
	f.raw("norole", "/v1/hrm/docs/"+doc1+"/content", http.StatusForbidden) // thiếu vai kho vault_evidence_viewer
	f.raw("ld_cn", "/v1/hrm/docs/"+doc1+"/content", http.StatusNotFound)
	g = f.call("hcth", http.MethodGet, "/v1/hrm/docs/"+doc1, nil, http.StatusOK)
	acc := g["access"].([]any)
	if len(acc) != 2 || acc[1].(map[string]any)["action"] != "download" || acc[1].(map[string]any)["scope"] != "unit" {
		t.Fatalf("nhật ký: %v", acc) // ld_tt tải (unit) + norole (ghi trước khi pf/evidence từ chối vai)
	}
	if g := f.call("ld_tt", http.MethodGet, "/v1/hrm/docs/"+doc1, nil, http.StatusOK); g["access"] != nil {
		t.Fatalf("lãnh đạo phòng không xem nhật ký: %v", g)
	}

	// Sửa phân loại: lưu vết trường đã đổi.
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/"+doc1+"/classify", map[string]any{"title": "QĐ nâng bậc lương 7/2026",
		"doc_type": "nang_bac", "staff_ids": []string{id1}, "doc_no": "123/QĐ-TTKN", "issued_on": "2026-07-15", "unit_code": "trong_trot"},
		http.StatusOK)
	g = f.call("hcth", http.MethodGet, "/v1/hrm/docs/"+doc1, nil, http.StatusOK)
	h := g["history"].([]any)
	if len(h) != 2 || strings.Join(toStrs(h[1].(map[string]any)["changed_fields"]), ",") != "title,staff_ids" {
		t.Fatalf("lịch sử: %v", h)
	}

	// Quyền thư mục ghi đè ma trận FN-20: chặn lãnh đạo phòng ở "Quyết định nâng bậc" (kế thừa xuống thư mục con), mở cho
	// một người. Chỉ người quản lý toàn Trung tâm ở thư mục; Admin gốc.
	f.call("ld_tt", http.MethodPost, "/v1/hrm/docs/folders/"+subID+"/acl", map[string]any{"acl": map[string]any{}}, http.StatusForbidden)
	f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/"+nangBac+"/acl", map[string]any{"acl": map[string]any{"role:admin": map[string]any{}}},
		http.StatusBadRequest)
	f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/"+nangBac+"/acl", map[string]any{
		"acl": map[string]any{"role:manager": map[string]any{"docs.view": "none"}}, "note": "Chặn lãnh đạo phòng"}, http.StatusOK)
	if l := f.call("ld_tt", http.MethodGet, "/v1/hrm/docs", nil, http.StatusOK); l["total"] != json.Number("0") {
		t.Fatalf("thư mục chặn: %v", l)
	}
	f.raw("ld_tt", "/v1/hrm/docs/"+doc1+"/content", http.StatusNotFound)
	ldcn := f.who["ld_cn"].ActorRef.String()
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders/"+subID+"/acl", map[string]any{
		"acl": map[string]any{"user:" + ldcn: map[string]any{"docs.view": "all"}}}, http.StatusOK)
	if l := f.call("ld_cn", http.MethodGet, "/v1/hrm/docs?folder="+subID, nil, http.StatusOK); l["total"] != json.Number("1") {
		t.Fatalf("mở cho một người: %v", l)
	}
	if hst := f.call("admin", http.MethodGet, "/v1/hrm/docs/folders/"+nangBac+"/history", nil, http.StatusOK); len(hst["items"].([]any)) != 2 {
		t.Fatalf("nhật ký đổi quyền: %v", hst)
	}

	// Xóa = ẩn: lý do bắt buộc; ẩn khỏi danh sách; người quản lý xem được khi include_hidden; hiện lại.
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/"+doc2+"/hide", map[string]any{"reason": "ngắn"}, http.StatusBadRequest)
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/"+doc2+"/hide", map[string]any{"reason": "Tải nhầm tệp, thay bằng bản ký"}, http.StatusOK)
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/docs?folder="+khac, nil, http.StatusOK); l["total"] != json.Number("0") {
		t.Fatalf("ẩn: %v", l)
	}
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/docs?folder="+khac+"&include_hidden=1", nil, http.StatusOK); l["total"] != json.Number("1") {
		t.Fatalf("ẩn — người quản lý: %v", l)
	}
	f.call("ld_cn", http.MethodGet, "/v1/hrm/docs/"+doc2, nil, http.StatusNotFound)
	// Thư mục còn tài liệu đang hiện → không ẩn được; thư mục rỗng ẩn được.
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders/"+subID+"/hide", map[string]any{"reason": "Dọn thư mục thừa"}, http.StatusConflict)
	empty := f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders", map[string]any{"parent_id": khac, "name": "Tạm"}, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/folders/"+empty["folder_id"].(string)+"/hide", map[string]any{"reason": "Dọn thư mục thừa"},
		http.StatusOK)
	f.call("hcth", http.MethodPost, "/v1/hrm/docs/"+doc2+"/restore", map[string]any{"reason": "Khôi phục theo yêu cầu"}, http.StatusOK)

	// Tenant khác, ngữ cảnh chỉ xem.
	if l := f.call("other", http.MethodGet, "/v1/hrm/docs", nil, http.StatusOK); l["total"] != json.Number("0") {
		t.Fatalf("tenant khác: %v", l)
	}
	f.call("other", http.MethodGet, "/v1/hrm/docs/"+doc1, nil, http.StatusNotFound)
	f.call("viewonly", http.MethodPost, "/v1/hrm/docs/folders", map[string]any{"parent_id": khac, "name": "X"}, http.StatusForbidden)

	// Sổ chỉ ghi thêm; nội dung tệp không ở pf_hrm, tệp ở pf_files có bộ ba chủ thể pf/hrm.
	for _, q := range []string{"UPDATE pf_hrm.doc SET title = 'x'", "DELETE FROM pf_hrm.doc", "UPDATE pf_hrm.doc_folder SET name = 'x'",
		"DELETE FROM pf_hrm.doc_access"} {
		err := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
			_, e := tx.Exec(context.Background(), q)
			return e
		})
		if err == nil {
			t.Fatalf("%s phải thất bại", q)
		}
	}
	var n int
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT count(*) FROM pf_files.file
	  WHERE tenant_id = $1 AND subject_module = 'pf/hrm' AND subject_type = 'hr_folder'`, f.tid).Scan(&n); err != nil || n != 2 {
		t.Fatalf("tệp ở pf_files: %d %v", n, err)
	}
}

func toStrs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
