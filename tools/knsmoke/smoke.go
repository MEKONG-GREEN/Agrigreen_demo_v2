package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// Identity cấp token cho hai danh tính của kiểm khói: quản trị gốc của tenant KN mới (platform_admin, có MFA —
// Admin API bắt buộc) và tài khoản KN vừa được tạo qua pf/accounts. Bản chạy thật lấy token ở Keycloak realm kn
// (keycloak.go); kiểm thử tích hợp dùng issuer giả của tests/e2e/harness.
type Identity interface {
	RootToken(ctx context.Context) (string, error)
	UserToken(ctx context.Context, userID string) (string, error)
}

// Config là cấu hình một lần chạy.
type Config struct {
	AppURL, AdminURL string
	ID               Identity
	HTTP             *http.Client
	Out              io.Writer
	Province         string // mã tỉnh trong danh mục hành chính đã seed, mặc định 91
}

// Năm phòng ban của TTKN (docs/kn/phong-ban-seed.md) — mã ổn định.
var departments = []string{"trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"}

// seedDisable là các module ngoài KN mà seed tắt cho tenant KN (PLAN-KN-STANDALONE mục 1, audit mục 3 "Cổng module").
// Module khai `enable: always` (pf/apiclient, pf/extdata, pf/kref, mkg/*) không tắt được theo đơn vị — xem
// docs/kn/gia-dinh/KN-STANDALONE-10B-PROFILE-CI.md.
var seedDisable = []string{"pf/cooperative", "pf/cropcycle", "pf/mechanization", "pf/mrv", "pf/waterway", "pf/simulation", "pf/metering", "pf/content"}

// gone là một route của module ngoài KN: tenant KN phải nhận 403 module_not_enabled (R-MOD-08).
type gone struct {
	module, path string
	admin        bool
}

var goneRoutes = []gone{
	{module: "pf/cooperative", path: "/v1/plots"},
	{module: "pf/cropcycle", path: "/v1/cropcycle/seasons"},
	{module: "pf/mechanization", path: "/v1/mechanization/machines"},
	{module: "pf/mrv", path: "/v1/mrv/catalog"},
	{module: "pf/waterway", path: "/v1/waterway/segments"},
	{module: "pf/simulation", path: "/v1/scenarios"},
	{module: "pf/metering", path: "/v1/metering/usage", admin: true},
	{module: "pf/content", path: "/v1/documents"},
}

// removed là bề mặt ngoài BRD đã gỡ khỏi KN bởi một gói gỡ ĐÃ có trên dev. Route gỡ hẳn trả 404; route còn dùng ngoài
// KN bị từ chối trong phiên KN (X-AG-Context) bằng 403 kèm mã. Gói gỡ gộp sau thêm dòng của mình ở đây
// (KN-STANDALONE-10B-PROFILE-CI-GATE).
var removed = []struct {
	pkg, path string
	status    int
	code      string // rỗng → không kiểm mã
}{
	{"KN-STANDALONE-5-TT-DATA (#271)", "/v1/kn/cooperatives", http.StatusNotFound, ""},
	{"KN-STANDALONE-1-ADMIN-TOOLS (#264)", "/v1/reports/templates", http.StatusForbidden, "extension.surface_removed"},
	{"KN-STANDALONE-1-ADMIN-TOOLS (#264)", "/v1/reports/schedules", http.StatusForbidden, "extension.surface_removed"},
	{"KN-STANDALONE-1-ADMIN-TOOLS (#264)", "/v1/geo/conflicts", http.StatusForbidden, "extension.surface_removed"},
}

// probe là một lời gọi đọc đại diện cho một FN BRD 2.0 của phòng ban, gọi bằng ngữ cảnh Cấp Tỉnh của phòng đó.
type probe struct{ dept, fn, path string }

var probes = []probe{
	{"trong_trot", "Trồng trọt FN-TI-02.01 Dashboard trồng trọt toàn tỉnh", "/v1/extension/kn-dashboard"},
	{"trong_trot", "Trồng trọt FN-TI-02.02/02.03 Phát hành biểu mẫu, theo dõi nộp", "/v1/extension/report-batches"},
	{"trong_trot", "Trồng trọt FN-XA-03.03 Quản lý HTX & nông hộ", "/v1/kn/producer-records?kind=cooperative"},
	{"trong_trot", "Trồng trọt FN-XA-03.05 Vẽ ranh giới thửa ruộng (kho thửa KN, pf/cooperative tắt)", "/v1/kn/plots"},
	{"chan_nuoi_thuy_san", "CNTS FN-13 Theo dõi công việc cấp xã (theo dõi nộp biểu kỳ)", "/v1/extension/cnts/period-tracking"},
	{"chan_nuoi_thuy_san", "CNTS FN-11 Báo cáo dịch bệnh & báo nghi dịch", "/v1/extension/cnts/outbreaks"},
	{"chan_nuoi_thuy_san", "CNTS FN-18 Bản đồ vùng nuôi, vùng dịch", "/v1/extension/cnts/map/layers"},
	{"ke_hoach_tai_chinh", "KHTC FN-TI-02.01 Trang chủ tổng hợp", "/v1/extension/khtc/dashboard"},
	{"ke_hoach_tai_chinh", "KHTC FN-TI-02.03 Kỳ báo cáo và tiến độ nộp", "/v1/extension/khtc/periods"},
	{"ke_hoach_tai_chinh", "KHTC FN-AD-04.01 Danh mục KHTC", "/v1/kn/khtc/catalogs"},
	{"thong_tin_truyen_thong", "Báo giá FN-AD-04.03 Danh mục mặt hàng, nhóm hàng theo thời kỳ", "/v1/price/items"},
	{"thong_tin_truyen_thong", "Báo giá FN-AD-04.02 Danh mục xã báo giá", "/v1/price/points"},
	{"thong_tin_truyen_thong", "Báo giá FN-TI-02.05 Duyệt bảng giá", "/v1/price/boards"},
	{"hanh_chinh_tong_hop", "HCTH FN-06 Dashboard nhân sự", "/v1/hrm/dashboard"},
	{"hanh_chinh_tong_hop", "HCTH FN-07 Quản lý hồ sơ nhân sự", "/v1/hrm/staff"},
	{"hanh_chinh_tong_hop", "HCTH FN-13 Contact", "/v1/contacts/units"},
}

type resp struct {
	status int
	body   map[string]any
	raw    []byte
}

func (r resp) code() string { s, _ := r.body["code"].(string); return s }

func (r resp) String() string {
	b := r.raw
	if len(b) > 240 {
		b = b[:240]
	}
	return fmt.Sprintf("HTTP %d %s", r.status, b)
}

type runner struct {
	cfg   Config
	fails []string
}

func (r *runner) logf(format string, a ...any) { _, _ = fmt.Fprintf(r.cfg.Out, format+"\n", a...) }

func (r *runner) check(ok bool, what string, got resp) {
	if ok {
		r.logf("--- PASS: %s", what)
		return
	}
	r.fails = append(r.fails, what)
	r.logf("--- FAIL: %s: %s", what, got)
}

func idem() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "knsmoke-" + hex.EncodeToString(b)
}

// do gửi một request JSON. kctx là X-AG-Context (rỗng → không gắn). POST luôn mang Idempotency-Key (R-51).
func (r *runner) do(ctx context.Context, admin bool, method, path, token, kctx string, body any) (resp, error) {
	base := r.cfg.AppURL
	if admin {
		base = r.cfg.AdminURL
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return resp{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, rd)
	if err != nil {
		return resp{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", idem())
	}
	if kctx != "" {
		req.Header.Set("X-AG-Context", kctx)
	}
	res, err := r.cfg.HTTP.Do(req)
	if err != nil {
		return resp{}, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return resp{}, err
	}
	out := resp{status: res.StatusCode, raw: raw}
	_ = json.Unmarshal(raw, &out.body)
	return out, nil
}

// must gọi một bước seed: sai mã HTTP mong đợi → dừng (các bước sau phụ thuộc bước này).
func (r *runner) must(ctx context.Context, admin bool, method, path, token string, body any, want ...int) (resp, error) {
	got, err := r.do(ctx, admin, method, path, token, "", body)
	if err != nil {
		return got, err
	}
	for _, w := range want {
		if got.status == w {
			return got, nil
		}
	}
	return got, fmt.Errorf("seed %s %s: %s", method, path, got)
}

func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

func items(m map[string]any, k string) []map[string]any {
	arr, _ := m[k].([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		if x, ok := v.(map[string]any); ok {
			out = append(out, x)
		}
	}
	return out
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// seed dựng tenant KN mới CHỈ qua Admin API (không SQL): danh mục nền, cây TTKN, 5 phòng ban đang áp dụng, tắt module
// ngoài KN, một tài khoản Cấp Tỉnh thuộc cả 5 phòng. Trả id tài khoản và id gán vai.
func (r *runner) seed(ctx context.Context, root string) (userID, assignment string, err error) {
	if _, err = r.must(ctx, true, http.MethodPost, "/v1/masterdata/seed", root, map[string]any{}, http.StatusOK); err != nil {
		return "", "", err
	}
	nat, err := r.must(ctx, true, http.MethodPost, "/v1/orgs", root,
		map[string]any{"kind": "ttkn_national", "code": "KN-TTKNQG", "name": "Trung tâm Khuyến nông Quốc gia"}, http.StatusCreated)
	if err != nil {
		return "", "", err
	}
	prov, err := r.must(ctx, true, http.MethodPost, "/v1/orgs", root, map[string]any{"kind": "ttkn_province", "code": "KN-TTKN-" + r.cfg.Province,
		"name": "Trung tâm Khuyến nông tỉnh", "parent_ref": str(obj(nat.body, "org"), "ref"), "admin_unit_code": r.cfg.Province}, http.StatusCreated)
	if err != nil {
		return "", "", err
	}
	provRef := str(obj(prov.body, "org"), "ref")
	if _, err = r.must(ctx, true, http.MethodPost, "/v1/orgs/"+provRef+"/departments/seed", root, nil, http.StatusOK); err != nil {
		return "", "", err
	}
	list, err := r.must(ctx, true, http.MethodGet, "/v1/departments?org_ref="+provRef, root, nil, http.StatusOK)
	if err != nil {
		return "", "", err
	}
	for _, d := range items(list.body, "items") {
		if str(d, "status") == "active" {
			continue
		}
		if _, err = r.must(ctx, true, http.MethodPost, "/v1/departments/"+str(d, "id")+"/status", root,
			map[string]any{"status": "active", "reason": "Mở phòng ban cho tenant KN độc lập"}, http.StatusOK, http.StatusCreated); err != nil {
			return "", "", err
		}
	}
	for _, m := range seedDisable {
		if _, err = r.must(ctx, true, http.MethodPost, "/v1/modules/"+m+"/enablement", root,
			map[string]any{"enabled": false, "reason": "Tenant KN độc lập: module ngoài BRD 2.0"}, http.StatusCreated); err != nil {
			return "", "", err
		}
	}
	tag := idem()[8:20]
	user, err := r.must(ctx, true, http.MethodPost, "/v1/accounts/kn-users", root, map[string]any{
		"full_name": "Cán bộ Kiểm Khói", "email": "kn-smoke-" + tag + "@example.test",
		"assignments": []any{map[string]any{"role": "kn_tinh", "org_ref": provRef, "department_codes": departments,
			"commune_codes": []string{}, "mgmt_level": "chuyen_vien"}}}, http.StatusCreated)
	if err != nil {
		return "", "", err
	}
	as := items(user.body, "assignments")
	if len(as) != 1 {
		return "", "", fmt.Errorf("seed tài khoản KN: muốn 1 gán vai, có %s", user)
	}
	return str(obj(user.body, "user"), "id"), str(as[0], "assignment_id"), nil
}

// Run dựng tenant KN mới rồi kiểm khói. Lỗi seed → trả ngay; lỗi kiểm → chạy hết rồi trả danh sách.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Province == "" {
		cfg.Province = "91"
	}
	r := &runner{cfg: cfg}
	root, err := cfg.ID.RootToken(ctx)
	if err != nil {
		return fmt.Errorf("đăng nhập quản trị gốc: %w", err)
	}
	userID, assignment, err := r.seed(ctx, root)
	if err != nil {
		return err
	}
	r.logf("--- PASS: seed tenant KN mới (cây TTKN, 5 phòng ban, tắt %d module ngoài KN, tài khoản Cấp Tỉnh)", len(seedDisable))
	user, err := cfg.ID.UserToken(ctx, userID)
	if err != nil {
		return fmt.Errorf("đăng nhập tài khoản KN: %w", err)
	}
	r.logf("--- PASS: đăng nhập bằng tài khoản KN của tenant mới")

	// Module: đúng tập đã seed đang tắt, không hơn không kém.
	mods, err := r.do(ctx, false, http.MethodGet, "/v1/me/modules", user, "", nil)
	if err != nil {
		return err
	}
	var off []string
	for _, m := range items(mods.body, "items") {
		if m["enabled"] == false {
			off = append(off, str(m, "module"))
		}
	}
	sort.Strings(off)
	want := make([]string, 0, len(goneRoutes)) // độc lập với danh sách seed: seed thiếu một module → đỏ
	for _, g := range goneRoutes {
		want = append(want, g.module)
	}
	sort.Strings(want)
	r.check(mods.status == http.StatusOK && strings.Join(off, ",") == strings.Join(want, ","), "module ngoài KN đã tắt cho tenant: "+strings.Join(want, ", "), mods)
	for _, g := range goneRoutes {
		tok := user
		if g.admin {
			tok = root
		}
		got, err := r.do(ctx, g.admin, http.MethodGet, g.path, tok, "", nil)
		if err != nil {
			return err
		}
		r.check(got.status == http.StatusForbidden && got.code() == "module_not_enabled", "GET "+g.path+" → 403 module_not_enabled ("+g.module+" tắt)", got)
	}

	// Mỗi phòng: chọn ngữ cảnh, menu của phòng trả về, vài FN đại diện đọc được.
	for _, dept := range departments {
		sel, err := r.do(ctx, false, http.MethodPost, "/v1/me/context", user, "", map[string]any{"assignment_id": assignment, "department_code": dept})
		if err != nil {
			return err
		}
		kctx := str(sel.body, "context_id")
		r.check(sel.status == http.StatusCreated && kctx != "", "chọn ngữ cảnh Cấp Tỉnh phòng "+dept, sel)
		if kctx == "" {
			continue
		}
		menu, err := r.do(ctx, false, http.MethodGet, "/v1/menu", user, kctx, nil)
		if err != nil {
			return err
		}
		r.check(menu.status == http.StatusOK && str(obj(menu.body, "department"), "code") == dept && len(items(menu.body, "groups")) > 0,
			"GET /v1/menu phòng "+dept+" trả menu có nhóm", menu)
		if dept == departments[0] { // bề mặt đã gỡ: kiểm một lần, trong phiên KN
			for _, x := range removed {
				got, err := r.do(ctx, false, http.MethodGet, x.path, user, kctx, nil)
				if err != nil {
					return err
				}
				r.check(got.status == x.status && (x.code == "" || got.code() == x.code),
					fmt.Sprintf("GET %s → %d %s (đã gỡ bởi %s)", x.path, x.status, x.code, x.pkg), got)
			}
		}
		for _, p := range probes {
			if p.dept != dept {
				continue
			}
			got, err := r.do(ctx, false, http.MethodGet, p.path, user, kctx, nil)
			if err != nil {
				return err
			}
			r.check(got.status == http.StatusOK, "GET "+p.path+" ("+p.fn+")", got)
		}
	}
	if len(r.fails) > 0 {
		return errors.New("kiểm khói KN độc lập hỏng " + fmt.Sprint(len(r.fails)) + " mục: " + strings.Join(r.fails, "; "))
	}
	return nil
}
