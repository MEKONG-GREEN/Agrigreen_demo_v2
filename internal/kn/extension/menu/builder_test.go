package menu

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

func department(unit uuid.UUID, code, profile string) partyport.Department {
	return partyport.Department{OrgRef: unit, Code: code, Name: "Phòng " + code, Status: partyport.DeptActive, MenuProfile: profile}
}

func leaves(groups []partyport.MenuGroup) map[string]partyport.MenuItem {
	out := map[string]partyport.MenuItem{}
	var visit func([]partyport.MenuItem)
	visit = func(items []partyport.MenuItem) {
		for _, it := range items {
			if len(it.Children) > 0 {
				visit(it.Children)
				continue
			}
			out[it.Key] = it
		}
	}
	for _, g := range groups {
		visit(g.Items)
	}
	return out
}

// Hai mục trình bày khác khóa cùng trỏ dashboard nhân sự HCTH được giữ (không khử trùng lặp); route chỉ đăng ký
// sở hữu một lần; tắt pf/hrm ẩn cả hai.
func TestPresentationAliasesKeptAndOwnedOnce(t *testing.T) {
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.New()
	request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceTinh, UnitRef: unit,
		Department: department(unit, "hanh_chinh_tong_hop", partyport.MenuProfileHanhChinhTongHop), FilterTraits: true, Manager: true}, Mode: partyport.MenuDepartment}
	def, err := reg.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	items := leaves(def.Groups)
	for _, key := range []string{"tinh.hcth.dashboard", "tinh.chung.dashboard_nhan_su"} {
		if it := items[key]; it.Route != "/kn/nhan-su/dashboard" || it.Module != "pf/hrm" || !it.Enabled {
			t.Fatalf("%s: %+v", key, it)
		}
	}
	owned := 0
	for _, r := range c.Routes() {
		if r.Pattern == "/kn/nhan-su/dashboard" {
			owned++
		}
	}
	if owned != 1 {
		t.Fatalf("dashboard registrations = %d", owned)
	}
	v, err := sharedView(def, request, map[string]bool{"pf/hrm": true}, c.NavigationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range v.Groups {
		for _, it := range g.Items {
			if strings.HasPrefix(it.Route, "/kn/nhan-su") {
				t.Fatalf("disabled pf/hrm still shows %s", it.Key)
			}
		}
	}
	// Không phải lãnh đạo: bỏ mục Needs=manager (alias dùng chung), giữ mục nghiệp vụ của phòng.
	request.Manager = false
	def, err = reg.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	items = leaves(def.Groups)
	if _, ok := items["tinh.chung.dashboard_nhan_su"]; ok {
		t.Fatal("manager-only alias shown to non-manager")
	}
	if _, ok := items["tinh.hcth.dashboard"]; !ok {
		t.Fatal("department dashboard missing")
	}
}

// Trang đích do KN sở hữu: bộ menu và alias "generic"/rỗng + mã có khuôn riêng; module tắt → trang lá kế tiếp.
func TestDefaultRouteSelection(t *testing.T) {
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.New()
	for _, tc := range []struct {
		audience, code, profile string
		disabled                map[string]bool
		want                    string
	}{
		{partyport.AudienceTinh, "ke_hoach_tai_chinh", partyport.MenuProfileKeHoachTaiChinh, nil, "/kn/khtc/ky-bao-cao"},
		{partyport.AudienceTinh, "ke_hoach_tai_chinh", partyport.MenuProfileGeneric, nil, "/kn/khtc/ky-bao-cao"},
		{partyport.AudienceTinh, "ke_hoach_tai_chinh", "", nil, "/kn/khtc/ky-bao-cao"},
		{partyport.AudienceTinh, "trong_trot", partyport.MenuProfileTrongTrot, nil, "/kn/bao-cao-khtc"},
		{partyport.AudienceTinh, "phong_moi", partyport.MenuProfileGeneric, nil, "/kn/dashboard"},
		{partyport.AudienceTW, "ke_hoach_tai_chinh", partyport.MenuProfileKeHoachTaiChinh, nil, "/kn/khtc/bao-cao-da-xuat"},
		// BF-MENU (#98): menu CNTS Cấp Xã không có "Công việc cần làm" của Trồng trọt → mục bật đầu tiên "Việc cần làm".
		{partyport.AudienceXa, "chan_nuoi_thuy_san", partyport.MenuProfileGeneric, nil, "/kn/cnts/bao-cao-ky"},
		{partyport.AudienceTinh, "thong_tin_truyen_thong", partyport.MenuProfileThongTinTuyenTruyen, map[string]bool{"pf/extension": true}, "/kn/bao-gia/tien-do"},
	} {
		request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: tc.audience, UnitRef: unit,
			Department: department(unit, tc.code, tc.profile)}, Mode: partyport.MenuDepartment}
		def, err := reg.Build(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if tc.disabled == nil && def.DefaultRoute != tc.want {
			t.Errorf("%s/%s/%s builder default %s, want %s", tc.audience, tc.code, tc.profile, def.DefaultRoute, tc.want)
		}
		v, err := sharedView(def, request, tc.disabled, c.NavigationPolicy())
		if err != nil || v.DefaultRoute != tc.want {
			t.Errorf("%s/%s/%s landing %s %v, want %s", tc.audience, tc.code, tc.profile, v.DefaultRoute, err, tc.want)
		}
	}
	def, err := reg.Build(context.Background(), partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceAdmin, UnitRef: unit},
		Mode: partyport.MenuAdmin, Departments: []partyport.Department{department(unit, "trong_trot", partyport.MenuProfileTrongTrot)}})
	if err != nil || def.DefaultRoute != "/kn/tong-quan" {
		t.Fatalf("admin default %s %v", def.DefaultRoute, err)
	}
}

// Phiên/hồ sơ cấu hình sai bị từ chối (lỗi cuối), không fallback sang cây, phiên hay đơn vị khác.
func TestBuilderDeniesInvalidSessionOrProfile(t *testing.T) {
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit, other := uuid.New(), uuid.New()
	ok := department(unit, "trong_trot", partyport.MenuProfileTrongTrot)
	notOpen, retired := ok, ok
	notOpen.Status = partyport.DeptNotOpen
	retired.Retired = true
	foreign := ok
	foreign.OrgRef = other
	nav := func(audience string, d partyport.Department) partyport.NavigationRequest {
		return partyport.NavigationRequest{Audience: audience, UnitRef: unit, Department: d}
	}
	for name, request := range map[string]partyport.MenuBuildRequest{
		"unknown audience":           {NavigationRequest: nav("kn_huyen", ok), Mode: partyport.MenuDepartment},
		"empty audience":             {NavigationRequest: nav("", ok), Mode: partyport.MenuDepartment},
		"unknown profile":            {NavigationRequest: nav(partyport.AudienceTinh, department(unit, "lua_gao", "lua_gao")), Mode: partyport.MenuDepartment},
		"empty profile unmapped":     {NavigationRequest: nav(partyport.AudienceTinh, department(unit, "phong_moi", "")), Mode: partyport.MenuDepartment},
		"missing department":         {NavigationRequest: nav(partyport.AudienceTinh, partyport.Department{}), Mode: partyport.MenuDepartment},
		"missing unit":               {NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceTinh, Department: ok}, Mode: partyport.MenuDepartment},
		"department of another unit": {NavigationRequest: nav(partyport.AudienceTinh, foreign), Mode: partyport.MenuDepartment},
		"department not open":        {NavigationRequest: nav(partyport.AudienceXa, notOpen), Mode: partyport.MenuDepartment},
		"department retired":         {NavigationRequest: nav(partyport.AudienceXa, retired), Mode: partyport.MenuDepartment},
		"admin in department mode":   {NavigationRequest: nav(partyport.AudienceAdmin, ok), Mode: partyport.MenuDepartment},
		"officer in admin mode":      {NavigationRequest: nav(partyport.AudienceTinh, ok), Mode: partyport.MenuAdmin, Departments: []partyport.Department{ok}},
		"officer in modules mode":    {NavigationRequest: nav(partyport.AudienceTinh, ok), Mode: partyport.MenuDepartmentModules, Departments: []partyport.Department{ok}},
		"admin foreign department":   {NavigationRequest: nav(partyport.AudienceAdmin, partyport.Department{}), Mode: partyport.MenuAdmin, Departments: []partyport.Department{foreign}},
		"admin unknown profile":      {NavigationRequest: nav(partyport.AudienceAdmin, partyport.Department{}), Mode: partyport.MenuAdmin, Departments: []partyport.Department{department(unit, "x", "lua_gao")}},
		"modules unknown department": {NavigationRequest: nav(partyport.AudienceAdmin, ok), Mode: partyport.MenuDepartmentModules, Departments: []partyport.Department{department(unit, "khac", partyport.MenuProfileGeneric)}},
		"modules without metadata":   {NavigationRequest: nav(partyport.AudienceAdmin, ok), Mode: partyport.MenuDepartmentModules},
		"modules empty department":   {NavigationRequest: nav(partyport.AudienceAdmin, partyport.Department{}), Mode: partyport.MenuDepartmentModules, Departments: []partyport.Department{{}}},
		"modules unknown profile": {NavigationRequest: nav(partyport.AudienceAdmin, department(unit, "x", "lua_gao")), Mode: partyport.MenuDepartmentModules,
			Departments: []partyport.Department{department(unit, "x", "lua_gao")}},
	} {
		if _, err := c.Build(context.Background(), request); !errors.Is(err, ErrDenied) {
			t.Errorf("%s: builder err %v", name, err)
		}
		if def, err := reg.Build(context.Background(), request); err == nil {
			t.Errorf("%s: registry built %+v", name, def.DefaultRoute)
		}
	}
	policy := c.NavigationPolicy()
	if _, err := policy(nav("kn_huyen", ok)); err == nil {
		t.Error("policy accepted unknown audience")
	}
	if _, err := policy(nav(partyport.AudienceTinh, department(unit, "lua_gao", "lua_gao"))); err == nil {
		t.Error("policy accepted unknown profile")
	}
}

type failingForms struct{}

func (failingForms) HasForms(context.Context, string, string, string) (bool, error) {
	return false, errors.New("forms down")
}

// Lỗi nguồn form availability là lỗi cuối (không ẩn mục âm thầm); nil = chưa có mẫu → ẩn mục Requires.
func TestFormAvailabilityErrorsAreFinal(t *testing.T) {
	c := catalog(t)
	unit := uuid.New()
	request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceXa, UnitRef: unit,
		Department: department(unit, "trong_trot", partyport.MenuProfileTrongTrot)}, Mode: partyport.MenuDepartment}
	c.UseFormAvailability(failingForms{})
	if _, err := c.Build(context.Background(), request); err == nil || err.Error() != "forms down" {
		t.Fatalf("err %v", err)
	}
	c.UseFormAvailability(nil)
	def, err := c.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := leaves(def.Groups)["xa.bao_cao.dot_xuat"]; ok {
		t.Fatal("ad hoc report shown without forms")
	}
	c.UseFormAvailability(forms(true))
	def, err = c.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if it, ok := leaves(def.Groups)["xa.bao_cao.dot_xuat"]; !ok || it.Module != "pf/extension" {
		t.Fatalf("ad hoc report %+v", it)
	}
}

// Quyền sở hữu route: module có ánh xạ tenant giữ nguyên ánh xạ của nguồn; route chưa ánh xạ thuộc lõi luôn bật
// (pf/party, Contact → pf/contact) như compatibility không lọc chúng; mỗi mẫu một lần, NewMenus chấp nhận.
func TestRouteOwnership(t *testing.T) {
	c := catalog(t)
	routes := c.Routes()
	if _, err := partyport.NewMenus(c.Build, routes...); err != nil {
		t.Fatal(err)
	}
	owner := map[string]string{}
	for _, r := range routes {
		if _, dup := owner[r.Pattern]; dup {
			t.Fatalf("duplicate %s", r.Pattern)
		}
		owner[r.Pattern] = r.Module
	}
	for pattern, want := range map[string]string{
		"/kn/dashboard":                            "pf/extension",
		"/kn/khtc/ky-bao-cao":                      "pf/extension",
		"/kn/nhan-su/dashboard":                    "pf/hrm",
		"/kn/bao-gia/nhap":                         "pf/price",
		"/kn/phong-ban/{department}/danh-muc-cnts": "pf/masterdata",
		"/kn/du-lieu/may-moc":                      "pf/mechanization",
		"/kn/phong-ban/{department}/mau-bao-cao":   "pf/reports",
		"/kn/phong-ban/{department}/bao-gia-lich":  "pf/price",
		"/kn/phong-ban/{department}/ke-hoach-nam":  "pf/party",
		"/kn/dung-chung/contact":                   "pf/contact",
		"/kn/du-lieu/htx":                          "pf/party",
		"/kn/tong-quan":                            "pf/party",
		"/dong-bo":                                 "pf/sync",
	} {
		if owner[pattern] != want {
			t.Errorf("%s owned by %q, want %q", pattern, owner[pattern], want)
		}
	}
	if !slices.IsSortedFunc(routes, func(a, b partyport.MenuRoute) int { return strings.Compare(a.Pattern, b.Pattern) }) {
		t.Error("routes not sorted")
	}
	routes[0].Module = "pf/x"
	if c.Routes()[0].Module == "pf/x" {
		t.Error("Routes leaked internal slice")
	}
}

// Nguồn hỏng bị từ chối như bộ sinh (không cây một phần).
func TestCompileRejectsInvalidSources(t *testing.T) {
	base := `{"id":"a","order":0,"tables":{"MenuProfiles":["trong_trot"],"defaultRoutes":{"kn_tinh":"/kn/dashboard"}}}`
	for name, docs := range map[string][]string{
		"duplicate id":        {base, base},
		"fractional order":    {`{"id":"a","order":1.5,"tables":{}}`},
		"missing order":       {`{"id":"a","tables":{}}`},
		"unknown table":       {`{"id":"a","order":0,"tables":{"other":{}}}`},
		"duplicate item":      {`{"id":"a","order":0,"tables":{"MenuProfiles":["x"]}}`, `{"id":"b","order":1,"tables":{"MenuProfiles":["x"]}}`},
		"duplicate value":     {base, `{"id":"b","order":1,"tables":{"defaultRoutes":{"kn_tinh":"/x"}}}`},
		"stale replacement":   {base, `{"id":"b","order":1,"replacements":[{"path":["defaultRoutes","kn_tinh"],"before":"/old","after":"/new"}]}`},
		"missing replacement": {base, `{"id":"b","order":1,"replacements":[{"path":["nope","kn_tinh"],"before":null,"after":"/new"}]}`},
		"profile no business": {`{"id":"a","order":0,"tables":{"MenuProfiles":["lua_gao"],"deptBusiness":{}}}`},
		"navigation profile":  {base, `{"id":"b","order":1,"tables":{"contextDefaultRoutes":{"lua_gao":{"kn_tinh":"/x"}}}}`},
		"navigation route":    {base, `{"id":"b","order":1,"tables":{"contextDefaultRoutes":{"trong_trot":{"kn_tinh":"//x"}}}}`},
	} {
		raw := make([][]byte, 0, len(docs))
		for _, d := range docs {
			raw = append(raw, []byte(d))
		}
		if _, err := Compile(raw...); !errors.Is(err, ErrSource) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok, err := Compile([]byte(base), []byte(`{"id":"b","order":1,"replacements":[{"path":["MenuProfiles",0],"before":"trong_trot","after":"trong_trot"}]}`))
	if err != nil || ok["MenuProfiles"].([]any)[0] != "trong_trot" {
		t.Fatalf("index replacement: %v %v", ok, err)
	}
	if _, err := New([]byte(base)); !errors.Is(err, ErrSource) {
		t.Errorf("missing tables accepted: %v", err)
	}
}

// Review Codex #76: Admin không gắn nút (UnitRef nil) thấy phòng của mọi đơn vị; hai đơn vị cùng mã phòng
// phải dựng được menu (khóa mục cha thêm OrgRef), mã không trùng giữ khóa cũ, nhãn/route/trạng thái giữ nguyên.
func TestAdminWithoutUnitAcceptsSameCodeAcrossUnits(t *testing.T) {
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	a, b, other := uuid.New(), uuid.New(), uuid.New()
	request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceAdmin}, Mode: partyport.MenuAdmin,
		Departments: []partyport.Department{
			department(a, "hanh_chinh_tong_hop", partyport.MenuProfileHanhChinhTongHop),
			department(b, "hanh_chinh_tong_hop", partyport.MenuProfileHanhChinhTongHop),
			department(other, "trong_trot", partyport.MenuProfileTrongTrot),
		}}
	def, err := reg.Build(context.Background(), request)
	if err != nil {
		t.Fatalf("hai đơn vị cùng mã phòng: %v", err)
	}
	var keys []string
	for _, g := range def.Groups {
		if g.Key != groupDepartments {
			continue
		}
		for _, it := range g.Items {
			keys = append(keys, it.Key)
			if it.Label != "Phòng "+strings.Split(strings.TrimPrefix(it.Key, groupDepartments+"."), ".")[0] || len(it.Children) == 0 {
				t.Errorf("mục %s mất nhãn hoặc mục con: %+v", it.Key, it)
			}
		}
	}
	want := []string{"phong_ban.hanh_chinh_tong_hop." + a.String(), "phong_ban.hanh_chinh_tong_hop." + b.String(), "phong_ban.trong_trot"}
	if !slices.Equal(keys, want) {
		t.Fatalf("khóa mục Phòng ban %v, muốn %v", keys, want)
	}
}

// BF-ACC-KN-MENU (#120, TT-13): menu Admin KN không còn liên kết màn tài khoản native cũ /quan-tri/tai-khoan
// (Công cụ nền tảng (tạm) › Tài khoản cán bộ (G1)); quản lý người dùng của Admin KN đi qua /kn/quan-tri/nguoi-dung.
// Màn/API native giữ nguyên, chỉ mở theo vai realm (platform_admin/account_admin), không theo menu KN.
func TestAdminMenuDropsNativeAccountLink(t *testing.T) {
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.New()
	request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceAdmin, UnitRef: unit}, Mode: partyport.MenuAdmin,
		Departments: []partyport.Department{department(unit, "trong_trot", partyport.MenuProfileTrongTrot)}}
	def, err := reg.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	items := leaves(def.Groups)
	if _, ok := items["admin.qt.cong_cu.tai_khoan"]; ok {
		t.Fatal("menu Admin KN còn mục admin.qt.cong_cu.tai_khoan")
	}
	for key, it := range items {
		if it.Route == "/quan-tri/tai-khoan" || strings.HasPrefix(it.Route, "/quan-tri/tai-khoan/") {
			t.Fatalf("menu Admin KN còn liên kết native %s: %+v", key, it)
		}
	}
	if it := items["admin.qt.nguoi_dung"]; it.Route != "/kn/quan-tri/nguoi-dung" || !it.Enabled || it.Status == "coming_soon" {
		t.Fatalf("Admin KN mất màn Người dùng mới: %+v", it)
	}
	for _, key := range []string{"admin.qt.cong_cu.tham_so", "admin.qt.cong_cu.to_chuc"} {
		if _, ok := items[key]; !ok {
			t.Fatalf("gỡ nhầm mục công cụ nền tảng khác %s", key)
		}
	}
}
