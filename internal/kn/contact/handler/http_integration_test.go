package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// staffStub là nguồn cán bộ thuộc tỉnh giả (pf/hrm) — off = cổng AG_HRM_RECORDS tắt.
type staffStub struct {
	off     bool
	entries []port.StaffEntry
}

func (s *staffStub) Staff(context.Context) ([]port.StaffEntry, error) {
	if s.off {
		return nil, port.ErrStaffDisabled
	}
	return s.entries, nil
}

type labelsStub struct{}

func (labelsStub) UnitNames(context.Context) (map[string]string, error) {
	return map[string]string{"trong_trot": "Phòng Trồng trọt", "hcth": "Phòng Hành chính – Tổng hợp"}, nil
}

func (labelsStub) CommuneNames(_ context.Context, codes []string) (map[string]string, error) {
	return map[string]string{"30292": "Xã Mỹ Hòa Hưng"}, nil
}

var testAccess = port.Access{
	Session: testOwnerSession,
	FullBy:  []string{authz.KNTinh, authz.KNAdmin}, BasicBy: []string{authz.KNTW, authz.KNXa},
	ManageBy: []string{authz.KNAdmin}, ManageDeptBy: []string{authz.KNTinh}, ManageDepartment: "hanh_chinh_tong_hop",
	RealmFull: []string{"platform_admin"}, RealmBasic: []string{"kn_officer"}, RealmManage: []string{"platform_admin"},
}

type fixture struct {
	t     *testing.T
	r     *gin.Engine
	svc   *app.Service
	vault *clienttest.Fake
	staff *staffStub
	env   *pgtest.Env
	tid   uuid.UUID
	who   map[string]authz.Principal
	logs  *bytes.Buffer
}

func (f *fixture) call(as, method, path string, body any, want int) map[string]any {
	f.t.Helper()
	raw := f.raw(as, method, path, body, want)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return m
}

func (f *fixture) raw(as, method, path string, body any, want int) []byte {
	f.t.Helper()
	var rd *bytes.Reader
	if b, ok := body.([]byte); ok {
		rd = bytes.NewReader(b)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("X-Test-As", as)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Code != want {
		f.t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
	}
	return w.Body.Bytes()
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule),
		clock.NewFixed(time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)), testAccess, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, svc: svc, vault: clienttest.New(), staff: &staffStub{}, env: env, tid: ids.New(), logs: &bytes.Buffer{}}
	svc.Vault, svc.Staff, svc.Labels = f.vault, f.staff, labelsStub{}
	kn := func(role, dept string) *authz.KNContext { return &authz.KNContext{Role: role, Department: dept} }
	f.who = map[string]authz.Principal{
		"admin":     {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}},
		"hcth":      {ActorRef: ids.New(), TenantID: f.tid, KN: kn(authz.KNTinh, "hanh_chinh_tong_hop")},
		"tinh_tt":   {ActorRef: ids.New(), TenantID: f.tid, KN: kn(authz.KNTinh, "trong_trot")},
		"viewonly":  {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: authz.KNTinh, Department: "hanh_chinh_tong_hop", ViewOnly: true}},
		"tw":        {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: authz.KNTW}},
		"xa":        {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: authz.KNXa, Department: "trong_trot", OrgRef: ids.New()}},
		"other":     {ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}},
		"realm":     {ActorRef: ids.New(), TenantID: f.tid, OrgRef: ids.New(), Roles: []string{"kn_officer"}},
		"farmer":    {ActorRef: ids.New(), TenantID: f.tid, Roles: []string{"farmer"}},
		"hrm_clerk": {ActorRef: ids.New(), TenantID: f.tid, KN: kn(authz.KNTinh, "hanh_chinh_tong_hop")},
	}
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	// Ghi log mọi request (như middleware truy cập thật: phương thức, đường dẫn có query, mã) vào bộ đệm để kiểm R-14.
	lg := slog.New(slog.NewJSONHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f.r.Use(func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
		c.Next()
		lg.Info("request", "method", c.Request.Method, "path", c.FullPath(), "status", c.Writer.Status(), "errors", c.Errors.String())
	})
	Handler{Svc: svc}.RegisterApp(f.r.Group("/v1"))
	return f
}

// addStaff tạo chủ thể cán bộ ở kho giả như pf/hrm (mục đích hrm.profile, ngăn hrm.profile) và thêm vào nguồn.
func (f *fixture) addStaff(name, phone, birth, email string, e port.StaffEntry) uuid.UUID {
	f.t.Helper()
	ctx := authz.WithPrincipal(context.Background(), f.who["hcth"])
	c, err := f.vault.Create(ctx, client.PurposeHRMProfile, client.Identity{Kind: client.SubjectPerson, FullName: name, Phone: phone,
		Address: "Số 1 đường bí mật", AreaCode: "hrm"})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.vault.PutSection(ctx, client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile,
		client.Fields{"birth_date": birth, "birth_precision": "day", "gender": "nu", "email": email, "note": "ghi chú hồ sơ"}); err != nil {
		f.t.Fatal(err)
	}
	e.SubjectRef = c.Ref
	f.staff.entries = append(f.staff.entries, e)
	return c.Ref
}

func items(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, x := range m["items"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func keys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func (f *fixture) seed() (mai, binh uuid.UUID) {
	mai = f.addStaff("Trần Thị Mai", "0912000111", "1990-03-15", "mai@ttkn.vn",
		port.StaffEntry{Level: port.LevelTinh, Active: true, Unit: "trong_trot", Title: "Trưởng phòng", AccountRef: ids.New()})
	binh = f.addStaff("Lê Văn Bình", "0912000222", "1985-07-01", "binh@ttkn.vn",
		port.StaffEntry{Level: port.LevelXa, Active: true, Unit: "trong_trot", Commune: "30292", Title: "Cán bộ khuyến nông xã"})
	f.addStaff("Phạm Văn Nghỉ", "0912000999", "1960", "nghi@ttkn.vn", port.StaffEntry{Level: port.LevelTinh, Active: false, Unit: "hcth"})
	return mai, binh
}

// FN-13 B3, BR-03, AC-01, AC-03, AC-04: lọc trường ở API theo vai.
func TestContact_FieldFiltering(t *testing.T) {
	f := newFixture(t)
	f.call("realm", http.MethodGet, "/v1/contacts/access", nil, http.StatusPreconditionRequired)
	mai, binh := f.seed()
	f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Nguyễn Văn Trung Ương", "phone": "0243000999",
		"email": "tw@khuyennong.gov.vn", "birth_year": "1975", "level": "tw", "organization": "Trung tâm Khuyến nông Quốc gia",
		"title": "Phó Giám đốc"}, http.StatusCreated)

	// Cấp TW, Cấp Xã: đúng 4 khóa (id + 3 trường); không email, địa chỉ, chức danh, đơn vị, cấp, nguồn — kể cả ở mức byte.
	for _, as := range []string{"tw", "xa"} {
		raw := f.raw(as, http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["field_set"] != "basic" || m["total"].(float64) != 3 {
			t.Fatalf("%s: %v", as, m)
		}
		for _, it := range items(m) {
			if got := keys(it); !slices.Equal(got, []string{"birth_year", "full_name", "id", "phone"}) {
				t.Fatalf("%s: khóa %v", as, got)
			}
		}
		for _, leak := range []string{"mai@ttkn.vn", "tw@khuyennong", "Trưởng phòng", "Phòng Trồng trọt", "Số 1 đường bí mật",
			"Trung tâm Khuyến nông Quốc gia", "ghi chú", "\"source\"", "\"email\"", "\"title\"", "\"unit\"", "\"level\""} {
			if bytes.Contains(raw, []byte(leak)) {
				t.Fatalf("%s: phản hồi lộ %q", as, leak)
			}
		}
		// Không xem chi tiết, không cập nhật (AC-01).
		f.call(as, http.MethodGet, "/v1/contacts/persons/"+mai.String(), nil, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "X", "phone": "0912000555", "level": "tw"}, http.StatusForbidden)
		// Lọc theo trường bị ẩn bị bỏ qua (không suy ra đơn vị/cấp); tìm theo email không khớp.
		if m := f.call(as, http.MethodGet, "/v1/contacts/persons?level=xa&unit=trong_trot", nil, http.StatusOK); m["total"].(json.Number) != "3" {
			t.Fatalf("%s: bộ lọc ẩn phải bị bỏ qua: %v", as, m)
		}
		if m := f.call(as, http.MethodGet, "/v1/contacts/persons?q=ttkn.vn", nil, http.StatusOK); m["total"].(json.Number) != "0" {
			t.Fatalf("%s: tìm theo email không được khớp: %v", as, m)
		}
		a := f.call(as, http.MethodGet, "/v1/contacts/access", nil, http.StatusOK)
		if a["field_set"] != "basic" || a["can_manage"] != false || a["can_detail"] != false {
			t.Fatalf("%s: access %v", as, a)
		}
	}
	// Kho chỉ được hỏi họ tên, năm sinh, SĐT cho bộ basic: không có lời gọi nào ngoài op card; không Resolve.
	for _, a := range f.vault.Accesses() {
		if a.Op == "resolve" || a.Op == "section_read" {
			t.Fatalf("Contact không được đọc định danh đầy đủ/ngăn: %+v", a)
		}
	}

	// Cấp Tỉnh (mọi phòng ban), Admin: đủ trường; AC-04 năm sinh 1990; AC-03 lọc cấp Xã ra cán bộ xã kèm đơn vị.
	for _, as := range []string{"tinh_tt", "admin", "hcth"} {
		m := f.call(as, http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK)
		if m["field_set"] != "full" || m["total"] != json.Number("3") {
			t.Fatalf("%s: %v", as, m)
		}
		var maiRow map[string]any
		for _, it := range items(m) {
			if it["id"] == mai.String() {
				maiRow = it
			}
		}
		if maiRow["birth_year"] != "1990" || maiRow["email"] != "mai@ttkn.vn" || maiRow["unit"] != "Phòng Trồng trọt" ||
			maiRow["title"] != "Trưởng phòng" || maiRow["level"] != "tinh" || maiRow["phone"] != "0912000111" {
			t.Fatalf("%s: dòng Mai %v", as, maiRow)
		}
		xa := items(f.call(as, http.MethodGet, "/v1/contacts/persons?level=xa", nil, http.StatusOK))
		if len(xa) != 1 || xa[0]["id"] != binh.String() || !strings.Contains(xa[0]["unit"].(string), "Xã Mỹ Hòa Hưng") {
			t.Fatalf("%s: lọc cấp Xã %v", as, xa)
		}
	}
	// Tìm theo họ tên không dấu, theo SĐT, theo email (chỉ bộ full).
	for q, want := range map[string]int{"tran thi": 1, "0912000222": 1, "binh@": 1, "000": 3, "0912 000": 2, "khong-co": 0} {
		if m := f.call("tinh_tt", http.MethodGet, "/v1/contacts/persons?q="+url.QueryEscape(q), nil, http.StatusOK); m["total"] != json.Number(itoa(want)) {
			t.Fatalf("q=%s: %v", q, m)
		}
	}
	if m := f.call("tw", http.MethodGet, "/v1/contacts/persons?q="+url.QueryEscape("Lê Văn"), nil, http.StatusOK); m["total"] != json.Number("1") {
		t.Fatalf("Cấp TW tìm theo họ tên: %v", m)
	}
	// Cán bộ đã nghỉ việc không có trong danh bạ.
	if m := f.call("admin", http.MethodGet, "/v1/contacts/persons?q="+url.QueryEscape("Nghỉ"), nil, http.StatusOK); m["total"] != json.Number("0") {
		t.Fatalf("cán bộ đã nghỉ: %v", m)
	}
	// Vai không có quyền danh bạ.
	f.call("farmer", http.MethodGet, "/v1/contacts/persons", nil, http.StatusForbidden)
	f.call("", http.MethodGet, "/v1/contacts/persons", nil, http.StatusUnauthorized)
}

func itoa(n int) string { return string(rune('0' + n)) }

// FN-13 B4, BR-01, BR-05, BR-06: liên hệ ngoài tỉnh — HCTH/Admin ghi; cán bộ thuộc tỉnh chỉ đọc; lịch sử tên trường; trùng
// SĐT; ngừng hiển thị; thẻ Đơn vị.
func TestContact_ExternalAndUnits(t *testing.T) {
	f := newFixture(t)
	mai, _ := f.seed()
	f.call("tinh_tt", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "A", "phone": "0912000555", "level": "tw"}, http.StatusForbidden)
	f.call("viewonly", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "A", "phone": "0912000555", "level": "tw"}, http.StatusForbidden)
	f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Thiếu SĐT", "level": "tw"}, http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Sai cấp", "phone": "0912000556", "level": "huyen"}, http.StatusUnprocessableEntity)
	// BR-01: SĐT của cán bộ thuộc tỉnh → trùng người.
	if m := f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Trùng", "phone": "0912000111", "level": "tw"},
		http.StatusConflict); m["code"] != "contact.duplicate_person" {
		t.Fatal(m)
	}
	d := f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Đỗ Thị Sở", "phone": "0296000123",
		"level": "tinh", "organization": "Sở NN&MT tỉnh Đồng Tháp", "title": "Chuyên viên", "email": "so@dongthap.gov.vn"}, http.StatusCreated)
	id := d["id"].(string)
	if d["source"] != "external" || d["can_edit"] != true || len(d["history"].([]any)) != 1 {
		t.Fatalf("tạo: %v", d)
	}
	// Sửa: SĐT (kho), chức danh (sổ); lịch sử chỉ có TÊN trường.
	r := f.call("admin", http.MethodPost, "/v1/contacts/persons/"+id+"/revise", map[string]any{"phone": "0296000124", "title": "Phó phòng"}, http.StatusOK)
	h := r["history"].([]any)
	last := h[len(h)-1].(map[string]any)
	if r["phone"] != "0296000124" || r["title"] != "Phó phòng" || len(h) != 2 {
		t.Fatalf("sửa: %v", r)
	}
	if cf := last["changed_fields"].([]any); len(cf) != 2 || !slices.Contains(cf, any("phone")) || !slices.Contains(cf, any("title")) {
		t.Fatalf("lịch sử: %v", last)
	}
	// Ngừng hiển thị: biến khỏi danh sách của mọi vai; HCTH xem lại bằng include_hidden; hiện lại.
	f.call("hcth", http.MethodPost, "/v1/contacts/persons/"+id+"/revise", map[string]any{"hidden": true}, http.StatusOK)
	if m := f.call("tinh_tt", http.MethodGet, "/v1/contacts/persons?include_hidden=true", nil, http.StatusOK); m["total"] != json.Number("2") {
		t.Fatalf("ẩn với Cấp Tỉnh khác: %v", m)
	}
	if m := f.call("hcth", http.MethodGet, "/v1/contacts/persons?include_hidden=true", nil, http.StatusOK); m["total"] != json.Number("3") {
		t.Fatalf("HCTH xem cả mục ẩn: %v", m)
	}
	f.call("hcth", http.MethodPost, "/v1/contacts/persons/"+id+"/revise", map[string]any{"hidden": false}, http.StatusOK)
	// Cán bộ thuộc tỉnh: sửa tại hồ sơ (FN-07).
	if m := f.call("admin", http.MethodPost, "/v1/contacts/persons/"+mai.String()+"/revise", map[string]any{"title": "X"}, http.StatusUnprocessableEntity); m["code"] != "contact.staff_read_only" {
		t.Fatal(m)
	}
	det := f.call("tinh_tt", http.MethodGet, "/v1/contacts/persons/"+mai.String(), nil, http.StatusOK)
	if det["source"] != "staff" || det["can_edit"] != false || det["email"] != "mai@ttkn.vn" {
		t.Fatalf("chi tiết cán bộ: %v", det)
	}

	// Thẻ Đơn vị: basic chỉ tên, cấp, SĐT.
	u := f.call("hcth", http.MethodPost, "/v1/contacts/units", map[string]any{"name": "Trung tâm Khuyến nông Quốc gia", "level": "tw",
		"phone": "024 3869 4444", "email": "vp@khuyennong.gov.vn", "address": "16 Thụy Khuê, Hà Nội"}, http.StatusCreated)
	if u["phone"] != "02438694444" {
		t.Fatalf("đơn vị: %v", u)
	}
	f.call("xa", http.MethodPost, "/v1/contacts/units", map[string]any{"name": "X", "level": "tw"}, http.StatusForbidden)
	for as, want := range map[string][]string{"tw": {"id", "level", "level_label", "name", "phone"},
		"tinh_tt": {"address", "email", "hidden", "id", "level", "level_label", "name", "org_ref", "phone"}} {
		got := items(f.call(as, http.MethodGet, "/v1/contacts/units", nil, http.StatusOK))
		if len(got) != 1 || !slices.Equal(keys(got[0]), want) {
			t.Fatalf("%s: thẻ Đơn vị %v", as, got)
		}
	}
	f.call("admin", http.MethodPost, "/v1/contacts/units/"+u["id"].(string)+"/revise", map[string]any{"name": "Trung tâm Khuyến nông Quốc gia",
		"level": "tw", "hidden": true}, http.StatusOK)
	if got := items(f.call("tw", http.MethodGet, "/v1/contacts/units", nil, http.StatusOK)); len(got) != 0 {
		t.Fatalf("đơn vị ẩn: %v", got)
	}
}

// AG_HRM_RECORDS tắt: chỉ còn liên hệ ngoài tỉnh — cổng chỉ áp cho dữ liệu chiếu từ hồ sơ nhân sự.
func TestContact_StaffGate(t *testing.T) {
	f := newFixture(t)
	f.seed()
	f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Liên hệ ngoài", "phone": "0243000777", "level": "khac"}, http.StatusCreated)
	f.staff.off = true
	m := f.call("tinh_tt", http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK)
	if m["staff_source"] != "disabled" || m["total"] != json.Number("1") || items(m)[0]["source"] != "external" {
		t.Fatalf("cổng tắt: %v", m)
	}
	if a := f.call("tw", http.MethodGet, "/v1/contacts/access", nil, http.StatusOK); a["staff_source"] != "disabled" {
		t.Fatalf("access: %v", a)
	}
	names, err := f.svc.DisplayNames(authz.WithPrincipal(context.Background(), f.who["hcth"]), []uuid.UUID{ids.New()})
	if err != nil || len(names) != 0 {
		t.Fatalf("tên khi cổng tắt: %v %v", names, err)
	}
}

// RLS theo tenant; sổ không có PII; log không có PII (R-14); tên theo tài khoản (thay pf_extension.staff).
func TestContact_RLSNoPII(t *testing.T) {
	f := newFixture(t)
	f.seed()
	d := f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Hoàng Văn Riêng", "phone": "0987654321",
		"level": "tw", "email": "rieng@example.vn", "birth_year": "1970", "organization": "Bộ NN&MT"}, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/contacts/persons/"+d["id"].(string)+"/revise", map[string]any{"full_name": "Hoàng Văn Riêng Tư"}, http.StatusOK)
	f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Lỗi", "phone": "0987654321", "level": "tw"}, http.StatusConflict)
	f.call("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Email Hỏng", "phone": "0987654000", "level": "tw",
		"email": "khong-hop-le@@x"}, http.StatusUnprocessableEntity)
	f.call("tw", http.MethodGet, "/v1/contacts/persons?q=0987654321", nil, http.StatusOK)
	f.call("tw", http.MethodGet, "/v1/contacts/persons/"+d["id"].(string), nil, http.StatusForbidden)

	// Tenant khác không thấy liên hệ ngoài tỉnh (RLS) — kể cả biết mã.
	o := f.call("other", http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK)
	for _, it := range items(o) {
		if it["source"] == "external" {
			t.Fatalf("tenant khác thấy liên hệ ngoài tỉnh: %v", it)
		}
	}
	f.call("other", http.MethodPost, "/v1/contacts/persons/"+d["id"].(string)+"/revise", map[string]any{"title": "X"}, http.StatusNotFound)
	ctx := context.Background()
	admin := f.env.AdminPool(t, migrations.PF)
	var n int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pf_contact.person WHERE tenant_id = $1`, f.tid).Scan(&n); err != nil || n != 2 {
		t.Fatalf("số phiên bản: %d %v", n, err)
	}
	app := f.env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	tx, err := app.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE r_contact`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, ids.New().String()); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_contact.person`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("RLS: tenant khác đọc được %d dòng (%v)", n, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE pf_contact.person SET title = 'x'`); err == nil {
		t.Fatal("sổ phải chặn UPDATE")
	}

	// Không PII trong bảng: mọi cột chữ của pf_contact không chứa họ tên, SĐT, email, năm sinh.
	rows, err := admin.Query(ctx, `SELECT row_to_json(p)::text FROM pf_contact.person p UNION ALL SELECT row_to_json(u)::text FROM pf_contact.unit u`)
	if err != nil {
		t.Fatal(err)
	}
	var dump strings.Builder
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		dump.WriteString(s)
	}
	rows.Close()
	pii := []string{"Hoàng Văn", "0987654321", "84987654321", "rieng@example.vn", "1970", "Trần Thị Mai", "0912000111", "mai@ttkn.vn"}
	for _, x := range pii {
		if strings.Contains(dump.String(), x) {
			t.Fatalf("pf_contact chứa PII %q", x)
		}
	}
	// Log của mọi request (kể cả lỗi 409/422/403) không chứa PII.
	for _, x := range pii {
		if strings.Contains(f.logs.String(), x) {
			t.Fatalf("log chứa PII %q: %s", x, f.logs.String())
		}
	}
	// Thông báo lỗi kiểm dữ liệu không lặp lại giá trị nhập.
	raw := f.raw("hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Giá Trị Bí Mật", "phone": "0987654000",
		"level": "tw", "email": "bi-mat@@x"}, http.StatusUnprocessableEntity)
	if bytes.Contains(raw, []byte("bi-mat")) || bytes.Contains(raw, []byte("Bí Mật")) {
		t.Fatalf("lỗi lặp giá trị nhập: %s", raw)
	}
	raw = f.raw("hcth", http.MethodPost, "/v1/contacts/persons", []byte(`{"full_name":"Giá Trị Bí Mật","phone":1}`), http.StatusUnprocessableEntity)
	if bytes.Contains(raw, []byte("Bí Mật")) {
		t.Fatalf("lỗi giải mã lặp giá trị: %s", raw)
	}

	// Tên theo tài khoản (thay display_name dạng rõ của pf_extension.staff).
	acc := f.staff.entries[0].AccountRef
	names, err := f.svc.DisplayNames(authz.WithPrincipal(ctx, f.who["xa"]), []uuid.UUID{acc, ids.New()})
	if err != nil || len(names) != 1 || names[acc] != "Trần Thị Mai" {
		t.Fatalf("DisplayNames: %v %v", names, err)
	}
}
