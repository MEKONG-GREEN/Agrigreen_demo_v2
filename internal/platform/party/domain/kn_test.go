package domain_test

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// UR-KT-02 / BRD v7.7 §6: mọi bộ menu × đối tượng có trang mặc định, khóa mục duy nhất, đường dẫn đã thay
// mã phòng ban; Cấp Xã không có Dashboard (chốt v7.4); bộ generic không có mục riêng của Trồng trọt.
func TestUR_KT_02_MenuTableConsistent(t *testing.T) {
	for _, profile := range []string{port.MenuProfileTrongTrot, port.MenuProfileGeneric} {
		for _, aud := range []string{port.AudienceTW, port.AudienceTinh, port.AudienceXa} {
			groups := domain.DepartmentMenu(profile, aud, "chan_nuoi")
			if len(groups) == 0 {
				t.Fatalf("%s/%s: không có menu", profile, aud)
			}
			seen := map[string]bool{}
			var walk func(items []domain.MenuItem)
			walk = func(items []domain.MenuItem) {
				for _, it := range items {
					if seen[it.Key] {
						t.Fatalf("%s/%s: trùng khóa %s", profile, aud, it.Key)
					}
					seen[it.Key] = true
					if it.Route == "" && len(it.Children) == 0 {
						t.Fatalf("%s/%s: mục %s không có đường dẫn lẫn mục con", profile, aud, it.Key)
					}
					// Hợp đồng đường dẫn KN-fix: vai + phòng ban ở ngữ cảnh phiên, không ở URL.
					if it.Route != "" && (!strings.HasPrefix(it.Route, "/kn/") || strings.Contains(it.Route, "{") || strings.Contains(it.Route, "chan_nuoi")) {
						t.Fatalf("%s/%s: đường dẫn %s", profile, aud, it.Route)
					}
					if aud == port.AudienceXa && strings.Contains(it.Key, "dashboard") {
						t.Fatalf("Cấp Xã không có Dashboard: %s", it.Key)
					}
					if profile == port.MenuProfileGeneric && (strings.Contains(it.Key, "thu_thap") || strings.Contains(it.Key, "chi_cuc")) {
						t.Fatalf("bộ generic có mục riêng Trồng trọt: %s", it.Key)
					}
					walk(it.Children)
				}
			}
			for _, g := range groups {
				walk(g.Items)
			}
			if r := domain.DefaultRoute(aud); !strings.HasPrefix(r, "/kn/") {
				t.Fatalf("trang mặc định %s: %s", aud, r)
			}
		}
		items := domain.AdminDepartmentItems(profile, "tt")
		if len(items) < 5 {
			t.Fatalf("mục Admin của phòng ban (%s): %d", profile, len(items))
		}
		for _, it := range items {
			if strings.Contains(it.Route, "{") || (strings.HasPrefix(it.Route, "/kn/phong-ban/") && !strings.HasPrefix(it.Route, "/kn/phong-ban/tt/")) {
				t.Fatalf("mục Admin của phòng ban (%s): đường dẫn %s", profile, it.Route)
			}
		}
	}
	// Bản sao sâu: sửa kết quả không đổi bảng tĩnh.
	g := domain.DepartmentMenu(port.MenuProfileTrongTrot, port.AudienceXa, "trong_trot")
	g[0].Items[0].Children[0].Label = "đổi"
	if domain.DepartmentMenu(port.MenuProfileTrongTrot, port.AudienceXa, "trong_trot")[0].Items[0].Children[0].Label == "đổi" {
		t.Fatal("DepartmentMenu phải trả bản sao")
	}
	if domain.KnownMenuProfile("khong_co") || !domain.KnownAudience(port.AudienceAdmin) {
		t.Fatal("bộ menu / đối tượng")
	}
}

// AC-CHUNG: Log "giá trị cũ → mới" từ hai ảnh chụp.
func TestFN_AD_04_03_DiffOldNew(t *testing.T) {
	cs := domain.Diff([]byte(`{"name":"A","sort_order":10,"retired":false,"x":"y"}`), []byte(`{"name":"B","sort_order":10,"retired":true}`))
	if len(cs) != 3 || cs[0].Field != "name" || *cs[0].Old != "A" || *cs[0].New != "B" || cs[1].Field != "retired" || *cs[1].New != "true" ||
		cs[2].Field != "x" || cs[2].New != nil {
		t.Fatalf("diff: %+v", cs)
	}
	if cs := domain.Diff(nil, []byte(`{"sort_order":7}`)); len(cs) != 1 || cs[0].Old != nil || *cs[0].New != "7" {
		t.Fatalf("diff tạo mới: %+v", cs)
	}
}

// FN-AD-04.02 / P4: dòng tổng Khu vực + Toàn tỉnh (decimal), xã chưa gán vào nhóm riêng.
func TestFN_AD_04_02_SumByRegion(t *testing.T) {
	assign := map[string]port.CommuneRegion{"x1": {RegionCode: "KV1"}, "x2": {RegionCode: "KV1"}, "x3": {RegionCode: "KV2"}}
	vals := map[string]decimal.Decimal{"x1": decimal.RequireFromString("1.1"), "x2": decimal.RequireFromString("2.2"),
		"x3": decimal.RequireFromString("3.3"), "x4": decimal.RequireFromString("0.4")}
	tot := port.SumByRegion(vals, assign)
	if !tot.ByRegion["KV1"].Equal(decimal.RequireFromString("3.3")) || !tot.ByRegion["KV2"].Equal(decimal.RequireFromString("3.3")) ||
		!tot.ByRegion[port.Unassigned].Equal(decimal.RequireFromString("0.4")) || !tot.Province.Equal(decimal.RequireFromString("7")) ||
		tot.Communes["KV1"] != 2 {
		t.Fatalf("tổng: %+v", tot)
	}
	if rs := tot.Regions(); len(rs) != 3 || rs[2] != port.Unassigned {
		t.Fatalf("thứ tự khu vực: %v", rs)
	}
}

func TestFN_AD_04_03_NormalizeDepartment(t *testing.T) {
	d, err := domain.NormalizeDepartment(port.Department{Code: "trong_trot", Name: " Phòng Trồng trọt "})
	if err != nil || d.Status != port.DeptNotOpen || d.MenuProfile != port.MenuProfileGeneric || d.Name != "Phòng Trồng trọt" {
		t.Fatalf("mặc định: %+v %v", d, err)
	}
	for _, bad := range []port.Department{{Code: "TT", Name: "x"}, {Code: "tt", Name: ""}, {Code: "tt", Name: "x", Status: "open"},
		{Code: "tt", Name: "x", SortOrder: -1}, {Code: "tt", Name: "x", MenuProfile: "abc"}} {
		if _, err := domain.NormalizeDepartment(bad); err == nil {
			t.Fatalf("phải lỗi: %+v", bad)
		}
	}
}
