package app

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// W11.06: mục "Sắp có" trên dây (enabled = false, status coming_soon); Admin có nhóm "Dùng chung" cuối;
// mã tạm phong_ban_5 trỏ về hanh_chinh_tong_hop khi dòng cũ không còn, dòng cũ còn thì dùng dòng cũ.
func TestW1106_MenuViewComingSoonAndAlias(t *testing.T) {
	hcth := port.Department{Code: port.DeptHanhChinhTongHop, Name: "Phòng Hành chính - Tổng hợp", Status: port.DeptActive, MenuProfile: port.MenuProfileHanhChinhTongHop}
	v, err := DepartmentMenu(context.Background(), nil, hcth, port.AudienceTinh)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != "kn.menu/v3" || len(v.Groups) < 2 || v.Groups[1].Key != domain.GroupNghiepVu {
		t.Fatalf("menu HCTH: %+v", v.Groups)
	}
	it := v.Groups[1].Items[1] // W11.22: Items[0] "Hồ sơ nhân sự" đã mở; "Hồ sơ dữ liệu" còn Sắp có
	if it.Enabled || it.Status != domain.ItemComingSoon || it.StatusLabel != "Sắp có" {
		t.Fatalf("mục Sắp có: %+v", it)
	}
	// W11.23: Contact (pf/contact) đã mở — mục thường của nhóm "Dùng chung".
	if last := v.Groups[len(v.Groups)-1]; last.Key != domain.GroupDungChung || !last.Items[0].Enabled || last.Items[0].Status != "" {
		t.Fatalf("Dùng chung: %+v", last)
	}
	// Mục đang chạy của khung chung vẫn bấm được.
	if d := v.Groups[0].Items[0]; !d.Enabled || d.Status != "" {
		t.Fatalf("Dashboard: %+v", d)
	}

	admin := AdminMenu(uuid.Nil, []port.Department{hcth})
	if g := admin.Groups[len(admin.Groups)-1]; g.Key != domain.GroupDungChung {
		t.Fatalf("Admin: %v", g.Key)
	}

	legacy := port.Department{Code: port.DeptLegacyPhongBan5, Status: port.DeptNotOpen}
	if d, ok := findDepartment([]port.Department{hcth}, port.DeptLegacyPhongBan5); !ok || d.Code != port.DeptHanhChinhTongHop {
		t.Fatal("phong_ban_5 → hanh_chinh_tong_hop")
	}
	if d, ok := findDepartment([]port.Department{legacy, hcth}, port.DeptLegacyPhongBan5); !ok || d.Code != port.DeptLegacyPhongBan5 {
		t.Fatal("dòng cũ còn → dùng dòng cũ")
	}
	if _, ok := findDepartment([]port.Department{hcth}, port.DeptLegacyPhongBan6); ok {
		t.Fatal("phong_ban_6 không kế nhiệm")
	}
}
