package domain

// kn.menu/v3 (W11.06, D3 C6/H0, D4 X2): khuôn menu riêng của các phòng ban ngoài Trồng trọt và nhóm
// "Dùng chung". Nguồn: bảng "Menu theo vai trò" của BRD CNTS v3.7 §VI.3, KHTC v2.2 §VI.3, Báo giá v1.3
// §VI.3, HCTH v3.0 §VI.3. Mọi mục ở đây là ComingSoon (module chưa có — W11.2x, W11.4x, W11.6x, W11.7x);
// gói của module bỏ ComingSoon (và thêm ánh xạ menuModules nếu là module per_tenant) khi màn sẵn sàng.
// Khuôn riêng = khung báo cáo theo kỳ chung (genericMenus, giữ nguyên màn đang chạy và trang mặc định của
// đối tượng) + nhóm "Nghiệp vụ" ngay sau "Điều hành" (thứ tự C6: Điều hành / Nghiệp vụ / Báo cáo).

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"

// Khóa/nhãn nhóm mới của kn.menu/v3.
const (
	GroupNghiepVu       = "nghiep_vu"
	GroupNghiepVuLabel  = "Nghiệp vụ"
	GroupDungChung      = "dung_chung"
	GroupDungChungLabel = "Dùng chung"
)

// soon tạo một mục "Sắp có".
func soon(key, label, route, icon string) MenuItem {
	return MenuItem{Key: key, Label: label, Route: route, Icon: icon, ComingSoon: true}
}

// SharedGroup là nhóm "Dùng chung" cuối menu của MỌI đối tượng (HCTH v3.0 FN-13, UR-GEN-01: Contact là
// module dùng chung, cột hiển thị theo vai trò).
var SharedGroup = MenuGroup{Key: GroupDungChung, Label: GroupDungChungLabel, Items: []MenuItem{
	soon("chung.contact", "Contact", "/kn/dung-chung/contact", "phone"),
}}

// deptBusiness: nhóm "Nghiệp vụ" theo khuôn × đối tượng. Đối tượng vắng = không có nhóm này.
var deptBusiness = map[string]map[string][]MenuItem{
	// Chăn nuôi - Thủy sản (BRD CNTS v3.7 §VI.3; Cấp TW chỉ xem dự án KNQG).
	port.MenuProfileChanNuoiThuySan: {
		port.AudienceTW: {
			soon("tw.cnts.du_an_knqg", "Dự án KNQG", "/kn/cnts/du-an-knqg", "report"),
		},
		port.AudienceTinh: {
			soon("tinh.cnts.canh_bao", "Cảnh báo", "/kn/cnts/canh-bao", "zap"),
			soon("tinh.cnts.duyet_bao_cao", "Duyệt báo cáo", "/kn/cnts/duyet-bao-cao", "approve"),
			soon("tinh.cnts.bieu_mau", "Biểu mẫu & cấp quyền", "/kn/cnts/bieu-mau", "table"),
			soon("tinh.cnts.lich", "Lịch trực KSGM & lịch tiêm", "/kn/cnts/lich", "calendar"),
			soon("tinh.cnts.chien_dich", "Chiến dịch điều tra", "/kn/cnts/chien-dich", "megaphone"),
			soon("tinh.cnts.ban_do_dich", "Bản đồ vùng dịch", "/kn/cnts/ban-do-dich", "map"),
			soon("tinh.cnts.du_an_knqg", "Dự án KNQG / Điểm trình diễn", "/kn/cnts/du-an-knqg", "report"),
			soon("tinh.cnts.quy_trinh", "Quy trình, văn bản", "/kn/cnts/quy-trinh", "sop"),
		},
		port.AudienceXa: {
			soon("xa.cnts.lich", "Lịch trực của tôi", "/kn/cnts/lich", "calendar"),
			soon("xa.cnts.nhat_ky", "Hộ mô hình & nhật ký", "/kn/cnts/nhat-ky", "journal"),
			soon("xa.cnts.ban_do_dich", "Bản đồ vùng dịch", "/kn/cnts/ban-do-dich", "map"),
			soon("xa.cnts.diem_trinh_dien", "Điểm trình diễn", "/kn/cnts/du-an-knqg", "report"),
			soon("xa.cnts.quy_trinh", "Quy trình, văn bản", "/kn/cnts/quy-trinh", "sop"),
		},
	},
	// Kế hoạch - Tài chính (BRD KHTC v2.2 §VI.3; Cấp Xã không có menu KHTC).
	port.MenuProfileKeHoachTaiChinh: {
		port.AudienceTW: {
			soon("tw.khtc.bao_cao_tinh", "Báo cáo Tỉnh (KHTC)", "/kn/khtc/bao-cao-da-xuat", "report"),
		},
		port.AudienceTinh: {
			soon("tinh.khtc.ky_bao_cao", "Kỳ báo cáo & phiếu các phòng", "/kn/khtc/ky-bao-cao", "task"),
			soon("tinh.khtc.ke_hoach_nam", "Kế hoạch năm", "/kn/khtc/ke-hoach-nam", "calendar"),
			soon("tinh.khtc.so_lieu_ngoai", "Số liệu nguồn ngoài", "/kn/khtc/so-lieu-ngoai", "database"),
			soon("tinh.khtc.mau_bao_cao", "Mẫu báo cáo KHTC", "/kn/khtc/mau-bao-cao", "table"),
			soon("tinh.khtc.tien_do", "Tiến độ các chương trình", "/kn/khtc/tien-do", "chart"),
			soon("tinh.khtc.da_xuat", "Báo cáo đã xuất", "/kn/khtc/bao-cao-da-xuat", "report"),
		},
	},
	// Thông tin Tuyên truyền — module Báo giá nông sản (BRD Báo giá v1.3 §VI.3).
	port.MenuProfileThongTinTuyenTruyen: {
		port.AudienceTW: {
			soon("tw.bao_gia.bang_gia", "Bảng giá tổng hợp", "/kn/bao-gia/bang-gia", "chart"),
			soon("tw.bao_gia.bien_dong", "Báo cáo biến động giá", "/kn/bao-gia/bien-dong", "layers"),
			soon("tw.bao_gia.gia_thanh", "Giá thành sản xuất", "/kn/bao-gia/gia-thanh", "report"),
		},
		port.AudienceTinh: {
			soon("tinh.bao_gia.tien_do", "Tiến độ nhập giá", "/kn/bao-gia/tien-do", "bell"),
			soon("tinh.bao_gia.kiem_duyet", "Kiểm duyệt & công bố", "/kn/bao-gia/kiem-duyet", "approve"),
			soon("tinh.bao_gia.bang_gia", "Bảng giá tổng hợp", "/kn/bao-gia/bang-gia", "chart"),
			soon("tinh.bao_gia.bien_dong", "Báo cáo biến động giá", "/kn/bao-gia/bien-dong", "layers"),
			soon("tinh.bao_gia.dieu_tra", "Điều tra giá thành", "/kn/bao-gia/dieu-tra", "season"),
			soon("tinh.bao_gia.mat_hang", "Mặt hàng", "/kn/bao-gia/mat-hang", "package"),
		},
		port.AudienceXa: {
			soon("xa.bao_gia.nhap", "Nhập báo giá", "/kn/bao-gia/nhap", "upload"),
			soon("xa.bao_gia.xac_minh", "Yêu cầu xác minh", "/kn/bao-gia/xac-minh", "check"),
			soon("xa.bao_gia.lich_su", "Lịch sử báo giá", "/kn/bao-gia/lich-su", "journal"),
			soon("xa.bao_gia.bang_gia", "Bảng giá tổng hợp", "/kn/bao-gia/bang-gia", "chart"),
		},
	},
	// Hành chính - Tổng hợp (BRD HCTH v3.0 §VI.3; Cấp TW chỉ có Contact ở "Dùng chung").
	port.MenuProfileHanhChinhTongHop: {
		port.AudienceTinh: {
			soon("tinh.hcth.ho_so", "Hồ sơ nhân sự", "/kn/nhan-su/ho-so", "users"),
			soon("tinh.hcth.kho_tai_lieu", "Hồ sơ dữ liệu", "/kn/nhan-su/kho-tai-lieu", "library"),
			soon("tinh.hcth.danh_gia", "Đánh giá", "/kn/nhan-su/danh-gia", "approve"),
			soon("tinh.hcth.danh_muc", "Danh mục nhân sự", "/kn/nhan-su/danh-muc", "catalog"),
		},
		port.AudienceXa: {
			soon("xa.hcth.danh_gia", "Đánh giá của tôi", "/kn/nhan-su/danh-gia-cua-toi", "approve"),
		},
	},
}

// adminDeptBusiness: mục "Sắp có" thêm vào mục con của phòng ban trong nhóm "Phòng ban" của Admin.
var adminDeptBusiness = map[string][]MenuItem{
	port.MenuProfileChanNuoiThuySan: {
		soon("admin.pb.cnts_danh_muc", "Danh mục CNTS", adminDeptRoute("/danh-muc-cnts"), "catalog"),
	},
	port.MenuProfileKeHoachTaiChinh: {
		soon("admin.pb.khtc_ke_hoach", "Kế hoạch năm", adminDeptRoute("/ke-hoach-nam"), "calendar"),
	},
	port.MenuProfileThongTinTuyenTruyen: {
		soon("admin.pb.bao_gia_cau_hinh", "Xã báo giá & mặt hàng", adminDeptRoute("/bao-gia-cau-hinh"), "param"),
	},
	port.MenuProfileHanhChinhTongHop: {
		soon("admin.pb.hcth_phan_quyen", "Phân quyền hồ sơ", adminDeptRoute("/phan-quyen-ho-so"), "shield"),
		soon("admin.pb.hcth_thu_muc", "Thư mục tài liệu", adminDeptRoute("/thu-muc-tai-lieu"), "library"),
	},
}

// deptProfileByCode: mã phòng ban seed → khuôn riêng (EffectiveMenuProfile).
var deptProfileByCode = map[string]string{
	port.DeptChanNuoiThuySan:     port.MenuProfileChanNuoiThuySan,
	port.DeptKeHoachTaiChinh:     port.MenuProfileKeHoachTaiChinh,
	port.DeptThongTinTruyenThong: port.MenuProfileThongTinTuyenTruyen,
	port.DeptHanhChinhTongHop:    port.MenuProfileHanhChinhTongHop,
}

// EffectiveMenuProfile: phòng ban có menu_profile "generic" (seed v7.7 trước W11.06) mà mã có khuôn riêng
// dùng khuôn riêng — đơn vị đã nạp seed cũ nhận menu v3 không cần sửa dữ liệu. Bộ khác giữ nguyên.
func EffectiveMenuProfile(profile, code string) string {
	if profile == port.MenuProfileGeneric || profile == "" {
		if p, ok := deptProfileByCode[code]; ok {
			return p
		}
	}
	return profile
}

// withBusiness dựng khuôn riêng: khung chung (genericMenus) + nhóm "Nghiệp vụ" sau "Điều hành".
func withBusiness(profile string) map[string][]MenuGroup {
	out := map[string][]MenuGroup{}
	for aud, groups := range genericMenus {
		biz := deptBusiness[profile][aud]
		cp := make([]MenuGroup, 0, len(groups)+1)
		for _, g := range groups {
			cp = append(cp, g)
			if g.Key == "dieu_hanh" && len(biz) > 0 {
				cp = append(cp, MenuGroup{Key: GroupNghiepVu, Label: GroupNghiepVuLabel, Items: biz})
			}
		}
		out[aud] = cp
	}
	return out
}
