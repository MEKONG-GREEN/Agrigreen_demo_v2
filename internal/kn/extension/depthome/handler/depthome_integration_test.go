package handler_test

// W11.54 (D4 CN14; BRD CNTS v3.7 FN-17 UR-TINH-06, FN-20 UR-TINH-25): trang chủ phòng + kho văn bản phòng trên
// PostgreSQL thật qua module pf/extension (vai extension_app) và kho đối tượng cục bộ (objstore.Local). Ngữ cảnh phiên KN
// giả lập bằng middleware X-AG-Context như các gói CNTS khác.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const (
	cnts = "chan_nuoi_thuy_san"
	khtc = "ke_hoach_tai_chinh"
)

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	pool   *pgxpool.Pool
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	f.pool = env.AdminPool(t, migrations.PF)
	mod := extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(t0), extension.Deps{})
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mod.UseDeptLibrary(st)
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		f.t.Fatal(err)
	}
	return func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(c.GetHeader(authz.ContextHeader))
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		selected, err := registry.ResolveSelected(c.Request.Context(), p)
		if err != nil {
			f.t.Errorf("owner session projection: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		p.SelectedSession = &selected
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
}

func (f *fix) user(kc authz.KNContext) user {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID}
}

func tinh(dept string, manager bool) authz.KNContext {
	return authz.KNContext{Role: authz.KNTinh, Department: dept, Manager: manager, Scope: authz.Scope{All: true}}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
	header http.Header
}

func (u user) send(method, path, idem, ctype string, body io.Reader) resp {
	u.f.t.Helper()
	r, _ := http.NewRequest(method, u.f.srv.URL+path, body)
	r.Header.Set("Authorization", "Bearer "+u.token)
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
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
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return resp{status: res.StatusCode, raw: raw, body: m, header: res.Header}
}

func key() string { return "k-" + ids.New().String() }

func (u user) post(p string, b any) resp {
	raw, _ := json.Marshal(b)
	return u.send(http.MethodPost, p, key(), "application/json", bytes.NewReader(raw))
}
func (u user) get(p string) resp { return u.send(http.MethodGet, p, "", "", nil) }

// upload gửi multipart: phần meta (nếu khác nil), phần file, các trường thêm.
func (u user) upload(p, k string, meta any, name string, data []byte, extra map[string]string) resp {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if meta != nil {
		raw, _ := json.Marshal(meta)
		_ = w.WriteField("meta", string(raw))
	}
	for kk, v := range extra {
		_ = w.WriteField(kk, v)
	}
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()
	return u.send(http.MethodPost, p, k, w.FormDataContentType(), &buf)
}

func str(m map[string]any, k string) string         { s, _ := m[k].(string); return s }
func list(m map[string]any, k string) []any         { l, _ := m[k].([]any); return l }
func obj(m map[string]any, k string) map[string]any { o, _ := m[k].(map[string]any); return o }
func num(m map[string]any, k string) string {
	n, _ := m[k].(json.Number)
	return n.String()
}

func want(t *testing.T, r resp, status int, code, what string) map[string]any {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
	return r.body
}

func pdf(s string) []byte { return []byte("%PDF-1.7\n" + s + "\n%%EOF") }

func titles(m map[string]any) []string {
	var out []string
	for _, it := range list(m, "items") {
		out = append(out, str(it.(map[string]any), "title"))
	}
	return out
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// FN-20 (UR-TINH-25): thư mục, thêm văn bản (tệp ở kho đối tượng), quyền theo phòng × cấp, đối tượng Cấp Xã, tệp > 20 MB bị
// chặn (AC-02), hết hiệu lực vẫn tìm được và có nhãn (AC-03, BR-01), phiên bản tệp, ẩn = xóa (Lãnh đạo phòng).
func TestFN20_DeptLibrary(t *testing.T) {
	f := newFix(t)
	vc := f.user(tinh(cnts, false))
	ld := f.user(tinh(cnts, true))
	bgd := f.user(authz.KNContext{Role: authz.KNTinh, Department: cnts, Manager: true, ViewOnly: true, Scope: authz.Scope{All: true}})
	xa := f.user(authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}})
	tw := f.user(authz.KNContext{Role: authz.KNTW, Department: cnts, Scope: authz.Scope{All: true}})
	other := f.user(tinh(khtc, true))
	admin := f.user(authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}})

	// Thư mục: Cấp Tỉnh tạo; trùng tên cùng cấp → 409; Cấp Xã không tạo được.
	fo := want(t, vc.post("/v1/extension/dept-docs/folders", map[string]any{"name": "Quy trình kỹ thuật"}), 201, "", "tạo thư mục")
	folder := str(fo, "ref")
	want(t, vc.post("/v1/extension/dept-docs/folders", map[string]any{"name": "quy trình kỹ thuật"}), 409, "extension.dept_folder_exists", "trùng tên")
	want(t, xa.post("/v1/extension/dept-docs/folders", map[string]any{"name": "Xã"}), 403, "forbidden", "Cấp Xã tạo thư mục")
	sub := want(t, vc.post("/v1/extension/dept-docs/folders", map[string]any{"name": "Gia cầm", "parent_ref": folder}), 201, "", "thư mục con")
	if p := list(sub, "path"); len(p) != 1 {
		t.Logf("đường dẫn trả về khi tạo chỉ có tên: %v", p)
	}

	// Thêm văn bản: Nghị định 83/2018 cho cả Cấp Xã; quy trình nội bộ chỉ Cấp Tỉnh.
	content := pdf("Nghi dinh 83/2018/ND-CP")
	nd := want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "Nghị định 83/2018/NĐ-CP về khuyến nông",
		"kind": "van_ban_phap_quy", "number": "83/2018/NĐ-CP", "issuer": "Chính phủ", "issued_on": "2018-05-24",
		"effective_from": "2018-07-10", "audience": []string{"xa", "nong_ho"}, "folder_ref": folder, "field": "Khuyến nông"},
		"nd83.pdf", content, nil), 201, "", "thêm Nghị định")
	ndRef := str(nd, "ref")
	if str(nd, "validity") != "active" || num(obj(nd, "file"), "version") != "1" || str(obj(nd, "file"), "content_type") != "application/pdf" {
		t.Fatalf("văn bản mới: %v", nd)
	}
	qt := want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "Quy trình tiêm phòng gia cầm nội bộ", "kind": "quy_trinh_ky_thuat",
		"folder_ref": str(sub, "ref")}, "qt.pdf", pdf("noi bo"), nil), 201, "", "thêm quy trình")
	// Hết hiệu lực: vẫn lưu, tìm được, nhãn expired (AC-03).
	want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "Kế hoạch tiêm phòng 2025", "kind": "ke_hoach",
		"effective_from": "2025-01-01", "effective_to": "2025-12-31", "audience": []string{"xa"}}, "kh.docx",
		append([]byte("PK\x03\x04"), []byte("docx giả")...), nil), 201, "", "thêm kế hoạch cũ")

	// Chặn: tệp > 20 MB (AC-02), sai định dạng, thiếu tiêu đề, Cấp Xã/Ban Giám đốc/Cấp TW/khác phòng không thêm được.
	big := append(pdf("x"), bytes.Repeat([]byte{'a'}, 20<<20)...)
	want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "To", "kind": "khac"}, "to.pdf", big, nil), 413,
		"extension.dept_doc_too_large", "tệp > 20 MB")
	want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "Ảnh", "kind": "khac"}, "a.exe", []byte("MZ...."), nil), 422,
		"extension.dept_doc_file_type", "sai định dạng")
	want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "Giả PDF", "kind": "khac"}, "gia.pdf", []byte("không phải pdf"), nil),
		422, "extension.dept_doc_file_type", "đuôi pdf nhưng nội dung sai")
	want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": " ", "kind": "khac"}, "a.pdf", pdf("a"), nil), 422,
		"extension.dept_doc_invalid", "thiếu tiêu đề")
	want(t, xa.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "X", "kind": "khac"}, "a.pdf", pdf("a"), nil), 403, "forbidden", "Cấp Xã thêm")
	want(t, bgd.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "X", "kind": "khac"}, "a.pdf", pdf("a"), nil), 403,
		"extension.read_only", "Ban Giám đốc thêm")
	want(t, tw.get("/v1/extension/dept-docs"), 403, "forbidden", "Cấp TW xem kho")

	// Danh sách theo quyền.
	all := want(t, vc.get("/v1/extension/dept-docs"), 200, "", "Cấp Tỉnh xem")
	if n := num(all, "total"); n != "3" {
		t.Fatalf("Cấp Tỉnh thấy %s văn bản: %v", n, titles(all))
	}
	xs := want(t, xa.get("/v1/extension/dept-docs"), 200, "", "Cấp Xã xem")
	if got := titles(xs); len(got) != 2 || has(got, "Quy trình tiêm phòng gia cầm nội bộ") {
		t.Fatalf("Cấp Xã chỉ thấy văn bản có đối tượng xa: %v", got)
	}
	want(t, xa.get("/v1/extension/dept-docs/"+str(qt, "ref")), 404, "extension.dept_doc_not_found", "Cấp Xã mở văn bản nội bộ")
	if got := titles(want(t, other.get("/v1/extension/dept-docs"), 200, "", "phòng khác")); len(got) != 0 {
		t.Fatalf("phòng KHTC thấy văn bản CNTS: %v", got)
	}
	want(t, other.get("/v1/extension/dept-docs/"+ndRef), 404, "extension.dept_doc_not_found", "phòng khác mở văn bản")
	want(t, other.get("/v1/extension/dept-docs?department="+cnts), 403, "forbidden", "phòng khác chọn phòng CNTS")
	ex := want(t, vc.get("/v1/extension/dept-docs?validity=expired"), 200, "", "lọc hết hiệu lực")
	if got := titles(ex); len(got) != 1 || got[0] != "Kế hoạch tiêm phòng 2025" {
		t.Fatalf("lọc hết hiệu lực: %v", got)
	}
	if got := titles(want(t, vc.get("/v1/extension/dept-docs?q=83%2F2018"), 200, "", "tìm số hiệu")); len(got) != 1 {
		t.Fatalf("tìm theo số hiệu: %v", got)
	}
	if got := titles(want(t, vc.get("/v1/extension/dept-docs?folder="+folder), 200, "", "lọc thư mục")); len(got) != 1 {
		t.Fatalf("lọc thư mục: %v", got)
	}
	want(t, admin.get("/v1/extension/dept-docs"), 422, "validation_failed", "Admin không chọn phòng")
	if n := num(want(t, admin.get("/v1/extension/dept-docs?department="+cnts), 200, "", "Admin chọn phòng"), "total"); n != "3" {
		t.Fatalf("Admin thấy %s", n)
	}

	// Sửa thuộc tính có khóa lạc quan; tải tệp mới → phiên bản tệp 2, tệp cũ vẫn tải được.
	want(t, vc.post("/v1/extension/dept-docs/"+ndRef+"/update", map[string]any{"title": "Nghị định 83", "kind": "van_ban_phap_quy", "base_version": 9}),
		409, "extension.dept_doc_version_conflict", "sửa bản cũ")
	up := want(t, vc.post("/v1/extension/dept-docs/"+ndRef+"/update", map[string]any{"title": "Nghị định 83/2018/NĐ-CP (khuyến nông)",
		"kind": "van_ban_phap_quy", "number": "83/2018/NĐ-CP", "audience": []string{"xa"}, "folder_ref": folder, "base_version": 1}), 200, "", "sửa")
	if num(up, "version") != "2" || str(up, "event") != "updated" {
		t.Fatalf("sửa: %v", up)
	}
	newPDF := pdf("ban sua doi 2020")
	rf := want(t, vc.upload("/v1/extension/dept-docs/"+ndRef+"/file", key(), nil, "nd83-v2.pdf", newPDF, map[string]string{"base_version": "2"}),
		200, "", "tải tệp mới")
	if num(obj(rf, "file"), "version") != "2" || num(rf, "version") != "3" || len(list(rf, "versions")) != 3 {
		t.Fatalf("tải tệp mới: %v", rf)
	}
	cur := xa.get("/v1/extension/dept-docs/" + ndRef + "/content?download=1")
	if cur.status != 200 || !bytes.Equal(cur.raw, newPDF) || !strings.HasPrefix(cur.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("Cấp Xã tải bản hiện hành: %d %q", cur.status, cur.header.Get("Content-Disposition"))
	}
	old := vc.get("/v1/extension/dept-docs/" + ndRef + "/content?version=1")
	if old.status != 200 || !bytes.Equal(old.raw, content) || old.header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("tải phiên bản 1: %d %s", old.status, old.raw)
	}

	// Ẩn ("xóa"): viên chức không được; Lãnh đạo phòng ẩn có lý do; ẩn rồi không hiện ở danh sách, Cấp Xã 404; Lãnh đạo
	// xem được khi include_hidden; hiện lại.
	want(t, vc.post("/v1/extension/dept-docs/"+ndRef+"/hide", map[string]any{"reason": "Hết dùng", "base_version": 3}), 403, "forbidden", "viên chức ẩn")
	want(t, ld.post("/v1/extension/dept-docs/"+ndRef+"/hide", map[string]any{"reason": "", "base_version": 3}), 422, "extension.dept_doc_invalid", "thiếu lý do")
	hd := want(t, ld.post("/v1/extension/dept-docs/"+ndRef+"/hide", map[string]any{"reason": "Đã có văn bản thay thế", "base_version": 3}), 200, "", "ẩn")
	if hd["hidden"] != true {
		t.Fatalf("ẩn: %v", hd)
	}
	if has(titles(want(t, vc.get("/v1/extension/dept-docs"), 200, "", "sau ẩn")), "Nghị định 83/2018/NĐ-CP (khuyến nông)") {
		t.Fatal("văn bản ẩn vẫn hiện")
	}
	want(t, xa.get("/v1/extension/dept-docs/"+ndRef+"/content"), 404, "extension.dept_doc_not_found", "Cấp Xã tải văn bản ẩn")
	if !has(titles(want(t, ld.get("/v1/extension/dept-docs?include_hidden=1"), 200, "", "Lãnh đạo xem ẩn")), "Nghị định 83/2018/NĐ-CP (khuyến nông)") {
		t.Fatal("Lãnh đạo phòng không thấy văn bản ẩn")
	}
	want(t, ld.post("/v1/extension/dept-docs/"+ndRef+"/hide", map[string]any{"reason": "Ẩn lần nữa", "base_version": 4}), 409, "extension.dept_doc_state", "ẩn hai lần")
	want(t, ld.post("/v1/extension/dept-docs/"+ndRef+"/restore", map[string]any{"reason": "Khôi phục theo yêu cầu", "base_version": 4}), 200, "", "hiện lại")

	// Thư mục còn văn bản → không ẩn được; thư mục rỗng ẩn được.
	want(t, ld.post("/v1/extension/dept-docs/folders/"+folder+"/hide", map[string]any{"reason": "Dọn kho", "base_version": 1}), 409,
		"extension.dept_folder_not_empty", "ẩn thư mục còn văn bản")
	empty := want(t, vc.post("/v1/extension/dept-docs/folders", map[string]any{"name": "Tạm"}), 201, "", "thư mục tạm")
	want(t, vc.post("/v1/extension/dept-docs/folders/"+str(empty, "ref")+"/rename", map[string]any{"name": "Tạm thời", "base_version": 1}), 200, "", "đổi tên")
	want(t, ld.post("/v1/extension/dept-docs/folders/"+str(empty, "ref")+"/hide", map[string]any{"reason": "Không dùng", "base_version": 2}), 200, "", "ẩn thư mục")
	fl := want(t, vc.get("/v1/extension/dept-docs/folders"), 200, "", "danh sách thư mục")
	for _, it := range list(fl, "items") {
		if str(it.(map[string]any), "name") == "Tạm thời" {
			t.Fatal("thư mục ẩn vẫn hiện")
		}
	}

	// Sổ chỉ ghi thêm: UPDATE/DELETE bị chặn.
	if _, err := f.pool.Exec(context.Background(), `UPDATE pf_extension.dept_doc SET title = 'x' WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("UPDATE sổ văn bản không bị chặn")
	}
}

// FN-17 (UR-TINH-06): trang chủ phòng — quyền (Cấp Tỉnh, Admin chọn phòng; Cấp Xã/TW không), thẻ theo phòng, thẻ chờ duyệt
// chỉ Lãnh đạo phòng/Ban Giám đốc, giới thiệu + chức năng nhiệm vụ do Lãnh đạo phòng sửa (có phiên bản, khóa lạc quan).
func TestFN17_DeptHome(t *testing.T) {
	f := newFix(t)
	vc := f.user(tinh(cnts, false))
	ld := f.user(tinh(cnts, true))
	bgd := f.user(authz.KNContext{Role: authz.KNTinh, Department: cnts, Manager: true, ViewOnly: true, Scope: authz.Scope{All: true}})
	xa := f.user(authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}})
	admin := f.user(authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}})

	want(t, xa.get("/v1/extension/dept-home"), 403, "forbidden", "Cấp Xã mở trang chủ")
	want(t, admin.get("/v1/extension/dept-home"), 422, "validation_failed", "Admin không chọn phòng")
	want(t, admin.get("/v1/extension/dept-home?department=khong_co"), 422, "validation_failed", "phòng lạ")

	h := want(t, vc.get("/v1/extension/dept-home"), 200, "", "viên chức mở trang chủ")
	keys := map[string]bool{}
	for _, k := range list(h, "kpis") {
		keys[str(k.(map[string]any), "key")] = true
	}
	for _, k := range []string{"alerts_open", "alerts_overdue", "outbreaks_active", "schedules_week", "docs_total"} {
		if !keys[k] {
			t.Fatalf("trang chủ CNTS thiếu thẻ %s: %v", k, keys)
		}
	}
	if keys["schedules_pending"] || h["manager"] == true || h["can_edit_profile"] == true || h["profile"] != nil {
		t.Fatalf("viên chức thấy thẻ của Lãnh đạo phòng: %s", h)
	}
	hb := want(t, bgd.get("/v1/extension/dept-home"), 200, "", "Ban Giám đốc")
	if hb["manager"] != true || hb["can_edit_profile"] == true {
		t.Fatalf("Ban Giám đốc xem như Lãnh đạo phòng nhưng không sửa: %v", hb)
	}

	// Giới thiệu: viên chức, Ban Giám đốc không sửa; Lãnh đạo phòng lưu; bản cũ → 409.
	want(t, vc.post("/v1/extension/dept-home/profile", map[string]any{"intro": "x", "base_version": 0}), 403, "forbidden", "viên chức sửa giới thiệu")
	want(t, bgd.post("/v1/extension/dept-home/profile", map[string]any{"intro": "x", "base_version": 0}), 403, "extension.read_only", "Ban Giám đốc sửa")
	want(t, ld.post("/v1/extension/dept-home/profile", map[string]any{"intro": "", "duties": "", "base_version": 0}), 422,
		"extension.dept_profile_invalid", "giới thiệu rỗng")
	p1 := want(t, ld.post("/v1/extension/dept-home/profile", map[string]any{"intro": "Phòng Chăn nuôi – Thủy sản",
		"duties": "1. Theo dõi dịch bệnh\n2. Tổng hợp báo cáo", "base_version": 0}), 201, "", "lưu giới thiệu")
	if num(p1, "version") != "1" {
		t.Fatalf("giới thiệu: %v", p1)
	}
	want(t, ld.post("/v1/extension/dept-home/profile", map[string]any{"intro": "Mới", "base_version": 0}), 409,
		"extension.dept_profile_version_conflict", "bản cũ")
	want(t, ld.post("/v1/extension/dept-home/profile", map[string]any{"intro": "Phòng CNTS — Trung tâm Khuyến nông An Giang",
		"duties": "Theo dõi dịch bệnh", "base_version": 1}), 201, "", "lưu lần 2")
	hist := want(t, vc.get("/v1/extension/dept-home/profile/history"), 200, "", "lịch sử giới thiệu")
	if len(list(hist, "items")) != 2 {
		t.Fatalf("lịch sử: %v", hist)
	}

	// Văn bản mới hiện ở trang chủ (AC-01 FN-20: hiện ngay cho người được xem).
	want(t, vc.upload("/v1/extension/dept-docs", key(), map[string]any{"title": "Quy trình nuôi tôm", "kind": "quy_trinh_ky_thuat"},
		"tom.pdf", pdf("tom"), nil), 201, "", "thêm văn bản")
	hl := want(t, ld.get("/v1/extension/dept-home"), 200, "", "Lãnh đạo phòng mở trang chủ")
	if str(obj(hl, "profile"), "intro") != "Phòng CNTS — Trung tâm Khuyến nông An Giang" || hl["can_edit_profile"] != true {
		t.Fatalf("giới thiệu hiện hành: %v", hl["profile"])
	}
	if num(obj(hl, "library"), "total") != "1" || num(obj(hl, "library"), "new_30d") != "1" || len(list(hl, "latest_docs")) != 1 {
		t.Fatalf("thống kê kho: %v %v", hl["library"], hl["latest_docs"])
	}
	found := false
	for _, k := range list(hl, "kpis") {
		if str(k.(map[string]any), "key") == "schedules_pending" {
			found = true
		}
	}
	if !found {
		t.Fatal("Lãnh đạo phòng thiếu thẻ chờ duyệt")
	}

	// Mỗi phòng có trang chủ: Admin chọn từng phòng.
	for _, d := range []string{"trong_trot", khtc, "thong_tin_truyen_thong", "hanh_chinh_tong_hop"} {
		r := want(t, admin.get("/v1/extension/dept-home?department="+d), 200, "", "Admin trang chủ "+d)
		if str(r, "department_code") != d || len(list(r, "kpis")) == 0 {
			t.Fatalf("trang chủ %s: %v", d, r)
		}
	}
}
