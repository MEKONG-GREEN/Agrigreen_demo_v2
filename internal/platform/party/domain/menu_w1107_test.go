package domain_test

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

func hasMenuKey(groups []domain.MenuGroup, key string) bool {
	for _, g := range groups {
		for _, it := range g.Items {
			if it.Key == key {
				return true
			}
		}
	}
	return false
}

// W11.07: mục có Needs chỉ hiện theo thuộc tính vai (Q-067 "Nhân sự của phòng", QTNV báo giá, Duyệt báo cáo CNTS).
func TestW1107_MenuNeeds(t *testing.T) {
	bg := domain.DepartmentMenu(port.MenuProfileThongTinTuyenTruyen, port.AudienceTinh, port.DeptThongTinTruyenThong)
	if !hasMenuKey(bg, "tinh.bao_gia.cau_hinh") || !hasMenuKey(bg, "tinh.chung.nhan_su_phong") {
		t.Fatal("menu đầy đủ (ca vàng) phải có mục mới")
	}
	staff := domain.FilterNeeds(bg, domain.MenuTraits{})
	if hasMenuKey(staff, "tinh.bao_gia.cau_hinh") || hasMenuKey(staff, "tinh.chung.nhan_su_phong") || !hasMenuKey(staff, "tinh.bao_gia.mat_hang") {
		t.Fatal("cán bộ thường: không thấy Cấu hình, Nhân sự của phòng")
	}
	qtnv := domain.FilterNeeds(bg, domain.MenuTraits{Duties: []string{domain.NeedsPriceAdmin}})
	if !hasMenuKey(qtnv, "tinh.bao_gia.cau_hinh") || hasMenuKey(qtnv, "tinh.chung.nhan_su_phong") {
		t.Fatal("QTNV")
	}
	// BF-MENU (#98): menu CNTS Cấp Tỉnh theo hình khách gửi 23/10 không còn "Duyệt báo cáo".
	cn := domain.DepartmentMenu(port.MenuProfileChanNuoiThuySan, port.AudienceTinh, port.DeptChanNuoiThuySan)
	if lead := domain.FilterNeeds(cn, domain.MenuTraits{Manager: true}); hasMenuKey(lead, "tinh.cnts.duyet_bao_cao") || !hasMenuKey(lead, "tinh.chung.nhan_su_phong") {
		t.Fatal("lãnh đạo")
	}
	if hasMenuKey(domain.FilterNeeds(cn, domain.MenuTraits{}), "tinh.chung.nhan_su_phong") {
		t.Fatal("chuyên viên không thấy Nhân sự của phòng")
	}
	if hasMenuKey(domain.DepartmentMenu(port.MenuProfileTrongTrot, port.AudienceXa, port.DeptTrongTrot), "tinh.chung.nhan_su_phong") {
		t.Fatal("Cấp Xã không có Nhân sự của phòng")
	}
}
