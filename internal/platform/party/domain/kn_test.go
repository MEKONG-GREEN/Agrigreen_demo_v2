package domain_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// UR-KT-02 / BRD v7.7 §6: mọi bộ menu × đối tượng có trang mặc định, khóa mục duy nhất, đường dẫn đã thay
// mã phòng ban; Cấp Xã không có Dashboard (chốt v7.4); bộ generic không có mục riêng của Trồng trọt.
func TestUR_KT_02_MenuTableConsistent(t *testing.T) {
	for _, profile := range domain.MenuProfiles {
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
					if profile != port.MenuProfileTrongTrot && (strings.Contains(it.Key, "thu_thap") || strings.Contains(it.Key, "chi_cuc")) {
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

// implementedMenu: mục nghiệp vụ của khuôn phòng ban đã có màn (gói đã bỏ ComingSoon).
// BEGIN GENERATED IMPLEMENTED MENU
var implementedMenu = map[string]bool{"admin.pb.bao_cao_khtc": true, "admin.pb.bao_gia_lich": true, "admin.pb.bao_gia_xa": true, "admin.pb.cnts_bieu_ky": true, "admin.pb.cnts_danh_muc": true, "admin.pb.cnts_lich_tiem": true, "admin.pb.cnts_lich_truc": true, "admin.pb.hcth_danh_muc": true, "admin.pb.hcth_phan_quyen": true, "admin.pb.hcth_thu_muc": true, "admin.pb.khtc_bao_cao": true, "admin.pb.khtc_danh_muc": true, "admin.pb.khtc_ke_hoach": true, "tinh.bao_cao.dot_xuat": true, "tinh.bao_cao.tong_hop": true, "tinh.bao_cao_khtc": true, "tinh.bao_cao_khtc.da_gui": true, "tinh.bao_cao_khtc.phieu_ky_nay": true, "tinh.bao_gia.bang_gia": true, "tinh.bao_gia.bien_dong": true, "tinh.bao_gia.cau_hinh": true, "tinh.bao_gia.dashboard": true, "tinh.bao_gia.kiem_duyet": true, "tinh.bao_gia.thong_ke": true, "tinh.bao_gia.tien_do": true, "tinh.cnts.ban_do_dich": true, "tinh.cnts.bieu_mau": true, "tinh.cnts.bieu_mau.cap_quyen": true, "tinh.cnts.bieu_mau.nhac_nho": true, "tinh.cnts.bieu_mau.phat_hanh": true, "tinh.cnts.bieu_mau.ra_soat_nhat_ky": true, "tinh.cnts.canh_bao": true, "tinh.cnts.chien_dich": true, "tinh.cnts.chien_dich.quan_ly": true, "tinh.cnts.dich_benh": true, "tinh.cnts.du_an_knqg": true, "tinh.cnts.du_an_knqg.du_an": true, "tinh.cnts.ksgm": true, "tinh.cnts.lich": true, "tinh.cnts.lich.cong_tac_phong": true, "tinh.cnts.lich.cong_tac_xa": true, "tinh.cnts.lich.tiem": true, "tinh.cnts.lich.truc": true, "tinh.cnts.phu_luc_2": true, "tinh.cnts.quy_trinh": true, "tinh.cnts.so_lieu_xa": true, "tinh.cnts.so_lieu_xa.lich_tiem": true, "tinh.cnts.trang_chu": true, "tinh.dashboard": true, "tinh.hcth.bao_cao_so": true, "tinh.hcth.danh_muc": true, "tinh.hcth.dashboard": true, "tinh.hcth.ho_so": true, "tinh.hcth.ke_hoach": true, "tinh.hcth.kho_tai_lieu": true, "tinh.hcth.nang_bac": true, "tinh.hcth.quyet_dinh_nang_bac": true, "tinh.khtc.bao_cao": true, "tinh.khtc.da_xuat": true, "tinh.khtc.ke_hoach_nam": true, "tinh.khtc.ky_bao_cao": true, "tinh.khtc.tien_do": true, "tinh.nhiem_vu": true, "tinh.nhiem_vu.phat_hanh": true, "tinh.nhiem_vu.ra_soat": true, "tinh.nhiem_vu.trang_thai_nop": true, "tinh.trang_chu": true, "tinh.van_ban": true, "tw.bao_cao.hang_vu": true, "tw.bao_cao.nam": true, "tw.bao_cao.thang": true, "tw.bao_cao.tuan": true, "tw.bao_gia.bang_gia": true, "tw.bao_gia.bien_dong": true, "tw.bao_gia.dashboard": true, "tw.cnts.du_an_knqg": true, "tw.dashboard": true, "xa.bao_cao.cua_xa": true, "xa.bao_cao.dot_xuat": true, "xa.bao_cao.hang_vu": true, "xa.bao_cao.thang": true, "xa.bao_cao.tuan": true, "xa.bao_gia.bang_gia": true, "xa.bao_gia.bien_dong": true, "xa.bao_gia.lich_su": true, "xa.bao_gia.nhap": true, "xa.bao_gia.xac_minh": true, "xa.cnts.ban_do_dich": true, "xa.cnts.cong_viec_tuan": true, "xa.cnts.dich_benh": true, "xa.cnts.diem_trinh_dien": true, "xa.cnts.ho_mo_hinh": true, "xa.cnts.khai_thac_thuy_san": true, "xa.cnts.ksgm_ngay": true, "xa.cnts.lich": true, "xa.cnts.lich_cong_tac": true, "xa.cnts.lich_tiem": true, "xa.cnts.nhap_lieu_tuan": true, "xa.cnts.nhat_ky": true, "xa.cnts.nhat_ky.chan_nuoi": true, "xa.cnts.nhat_ky.thuy_san": true, "xa.cnts.quy_trinh": true, "xa.cnts.viec_can_lam": true, "xa.nhiem_vu": true, "xa.nhiem_vu.cong_viec": true, "xa.van_ban": true}

// END GENERATED IMPLEMENTED MENU

// W11.06 (kn.menu/v3, D3 C6, D4 X2): khuôn theo phòng ban, nhóm "Dùng chung" (Contact) cuối mọi đối tượng; mục module
// chưa có là "Sắp có"; phòng ban seed cũ (menu_profile generic) nhận khuôn riêng theo mã. BF-MENU (#98, kn.menu/v6, R-A6):
// mục nghiệp vụ nằm trong 3 nhóm Điều hành / Báo cáo / Dữ liệu, không còn nhóm "Nghiệp vụ".
func TestW1106_MenuV3DepartmentTemplates(t *testing.T) {
	if domain.MenuVersion != "kn.menu/v6" {
		t.Fatal(domain.MenuVersion)
	}
	keys := func(gs []domain.MenuGroup) []string {
		out := []string{}
		for _, g := range gs {
			out = append(out, g.Key)
		}
		return out
	}
	tt := keys(domain.DepartmentMenu(port.MenuProfileTrongTrot, port.AudienceXa, port.DeptTrongTrot))
	if strings.Join(tt, ",") != "dieu_hanh,bao_cao,du_lieu,dung_chung" {
		t.Fatalf("Trồng trọt Cấp Xã: %v", tt)
	}
	for _, code := range []string{port.DeptChanNuoiThuySan, port.DeptKeHoachTaiChinh, port.DeptThongTinTruyenThong, port.DeptHanhChinhTongHop} {
		if p := domain.EffectiveMenuProfile(port.MenuProfileGeneric, code); p != code || !domain.KnownMenuProfile(p) {
			t.Fatalf("EffectiveMenuProfile(generic, %s) = %s", code, p)
		}
		gs := domain.DepartmentMenu(port.MenuProfileGeneric, port.AudienceTinh, code)
		if k := keys(gs); strings.Join(k, ",") != "dieu_hanh,bao_cao,du_lieu,dung_chung" {
			t.Fatalf("%s Cấp Tỉnh: %v", code, k)
		}
		var leaves func([]domain.MenuItem)
		leaves = func(items []domain.MenuItem) {
			for _, it := range items {
				if len(it.Children) > 0 {
					leaves(it.Children)
				} else if !it.ComingSoon && !implementedMenu[it.Key] && it.Key != domain.KHTCReportItem.Key {
					t.Fatalf("%s: mục nghiệp vụ chưa có module phải Sắp có: %s", code, it.Key)
				}
			}
		}
		for _, g := range gs[:len(gs)-1] {
			leaves(g.Items)
		}
		admin := domain.AdminDepartmentItems(port.MenuProfileGeneric, code)
		if code == port.DeptThongTinTruyenThong { // W11.72: Xã báo giá, Kỳ báo giá & giờ khóa đã mở; Mặt hàng Sắp có
			if n := len(admin); admin[n-3].ComingSoon || admin[n-2].Route != "/kn/phong-ban/"+code+"/bao-gia-lich" || !admin[n-1].ComingSoon {
				t.Fatalf("%s: mục Admin Báo giá: %+v", code, admin[n-3:])
			}
			continue
		}
		if last := admin[len(admin)-1]; (!last.ComingSoon && !implementedMenu[last.Key]) || !strings.HasPrefix(last.Route, "/kn/phong-ban/"+code+"/") {
			t.Fatalf("%s: mục Admin Sắp có: %+v", code, last)
		}
	}
	if p := domain.EffectiveMenuProfile(port.MenuProfileTrongTrot, port.DeptChanNuoiThuySan); p != port.MenuProfileTrongTrot {
		t.Fatal("bộ menu khác generic giữ nguyên")
	}
	// KHTC: Cấp Xã không có menu KHTC (BRD KHTC v2.2 §VI.3).
	if k := keys(domain.DepartmentMenu(port.MenuProfileKeHoachTaiChinh, port.AudienceXa, port.DeptKeHoachTaiChinh)); strings.Join(k, ",") != "dieu_hanh,bao_cao,dung_chung" {
		t.Fatalf("KHTC Cấp Xã: %v", k)
	}
	// W11.11 (D4 X5, CN11, BG10): "Báo cáo cho KHTC" cuối nhóm "Báo cáo" của Cấp Tỉnh 4 phòng chuyên môn; không ở KHTC, Cấp
	// Xã, Cấp TW; Admin có mục theo phòng ban.
	khtcItem := func(gs []domain.MenuGroup) *domain.MenuItem {
		for _, g := range gs {
			for i, it := range g.Items {
				if it.Key == domain.KHTCReportItem.Key {
					if g.Key != domain.GroupBaoCao || i != len(g.Items)-1 {
						t.Fatalf("Báo cáo cho KHTC phải cuối nhóm Báo cáo: %s[%d]", g.Key, i)
					}
					return &g.Items[i]
				}
			}
		}
		return nil
	}
	for _, code := range []string{port.DeptTrongTrot, port.DeptChanNuoiThuySan, port.DeptThongTinTruyenThong, port.DeptHanhChinhTongHop} {
		profile := port.MenuProfileGeneric
		if code == port.DeptTrongTrot {
			profile = port.MenuProfileTrongTrot
		}
		it := khtcItem(domain.DepartmentMenu(profile, port.AudienceTinh, code))
		if it == nil || len(it.Children) != 4 || it.Children[0].Route != "/kn/bao-cao-khtc" || it.Children[0].ComingSoon ||
			it.Children[1].Route != "/kn/bao-cao-khtc/da-gui" || !it.Children[2].ComingSoon || !it.Children[3].ComingSoon {
			t.Fatalf("%s: Báo cáo cho KHTC: %+v", code, it)
		}
		for _, aud := range []string{port.AudienceXa, port.AudienceTW} {
			if khtcItem(domain.DepartmentMenu(profile, aud, code)) != nil {
				t.Fatalf("%s %s không có Báo cáo cho KHTC", code, aud)
			}
		}
		admin := domain.AdminDepartmentItems(profile, code)
		if !slices.ContainsFunc(admin, func(m domain.MenuItem) bool {
			return m.Key == "admin.pb.bao_cao_khtc" && m.Route == "/kn/phong-ban/"+code+"/bao-cao-khtc"
		}) {
			t.Fatalf("%s: Admin thiếu Báo cáo cho KHTC", code)
		}
	}
	if khtcItem(domain.DepartmentMenu(port.MenuProfileKeHoachTaiChinh, port.AudienceTinh, port.DeptKeHoachTaiChinh)) != nil ||
		khtcItem(domain.DepartmentMenu(port.MenuProfileGeneric, port.AudienceTinh, "phong_moi")) != nil {
		t.Fatal("KHTC và phòng generic không có Báo cáo cho KHTC")
	}
	sh := domain.SharedMenuGroup()
	// W11.23: Contact (pf/contact, cap/directory) đã có màn — mục thường cho MỌI đối tượng, không "Sắp có".
	if sh.Key != domain.GroupDungChung || len(sh.Items) == 0 || sh.Items[0].Key != "chung.contact" || sh.Items[0].ComingSoon ||
		sh.Items[0].Needs != "" {
		t.Fatalf("Dùng chung: %+v", sh)
	}
	// Mã tạm cũ (Q-051).
	if to, ok := port.CanonicalDepartmentCode(port.DeptLegacyPhongBan5); !ok || to != port.DeptHanhChinhTongHop {
		t.Fatal("phong_ban_5 → hanh_chinh_tong_hop")
	}
	if to, ok := port.CanonicalDepartmentCode(port.DeptLegacyPhongBan6); !ok || to != "" {
		t.Fatal("phong_ban_6 bỏ, không kế nhiệm")
	}
	if _, ok := port.CanonicalDepartmentCode(port.DeptTrongTrot); ok {
		t.Fatal("mã thật không phải mã tạm")
	}
}
