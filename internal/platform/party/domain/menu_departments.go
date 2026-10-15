package domain

// kn.menu/v3 (W11.06, D3 C6/H0, D4 X2): khuôn menu riêng của các phòng ban ngoài Trồng trọt và nhóm
// "Dùng chung". Nguồn: bảng "Menu theo vai trò" của BRD CNTS v3.7 §VI.3, KHTC v2.2 §VI.3, Báo giá v1.3
// §VI.3, HCTH v3.0 §VI.3. Mục ở đây là ComingSoon (module chưa có — W11.2x, W11.4x, W11.6x, W11.7x) trừ mục đã có màn;
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

// GroupBaoCao là khóa nhóm "Báo cáo" của khung chung.
const GroupBaoCao = "bao_cao"

// W11.11 (D4 X5, CN11, BG10 — BRD KHTC v2.2 Mục IV.3 FN-PN-03.01…03, GD-01): "Báo cáo cho KHTC" — MỘT mã dùng chung
// (pf/extension khtcperiod, màn /kn/bao-cao-khtc đọc phòng từ ngữ cảnh phiên) hiện trong menu Cấp Tỉnh của mọi phòng chuyên
// môn nộp phiếu KHTC: Trồng trọt, CNTS, Thông tin tuyên truyền (Báo giá), HCTH — cuối nhóm "Báo cáo". Phòng KHTC không nộp
// phiếu cho chính mình; phòng mở bằng cấu hình (generic) chưa có trong BRD — thêm khuôn vào khtcReportProfiles khi cần.
// Cấp Xã, Cấp TW không có menu này. Form nhập phiếu (FN-PN-03.02) là màn của W11.63, mở từ dòng phiếu.
var khtcReportProfiles = map[string]bool{
	port.MenuProfileTrongTrot:           true,
	port.MenuProfileChanNuoiThuySan:     true,
	port.MenuProfileThongTinTuyenTruyen: true,
	port.MenuProfileHanhChinhTongHop:    true,
}

// KHTCReportItem là mục "Báo cáo cho KHTC" của Cấp Tỉnh phòng chuyên môn. Tiến độ các CT của phòng và Báo cáo KHTC đã xuất
// (FN-PN-03.03) chờ KH5/KH6 — "Sắp có".
var KHTCReportItem = MenuItem{Key: "tinh.bao_cao_khtc", Label: "Báo cáo cho KHTC", Icon: "send", Children: []MenuItem{
	{Key: "tinh.bao_cao_khtc.phieu_ky_nay", Label: "Phiếu kỳ này", Route: "/kn/bao-cao-khtc", Icon: "task"},
	{Key: "tinh.bao_cao_khtc.da_gui", Label: "Phiếu đã gửi", Route: "/kn/bao-cao-khtc/da-gui", Icon: "check"},
	soon("tinh.bao_cao_khtc.tien_do", "Tiến độ các CT của phòng", "/kn/bao-cao-khtc/tien-do", "chart"),
	soon("tinh.bao_cao_khtc.da_xuat", "Báo cáo KHTC đã xuất", "/kn/bao-cao-khtc/da-xuat", "report"),
}}

// KHTCReportAdminItem là mục "Báo cáo cho KHTC" của phòng ban trong nhóm "Phòng ban" của Admin (xem phiếu của phòng, nhập
// thay khi hỗ trợ — FN-PN-03.02 ma trận quyền).
var KHTCReportAdminItem = MenuItem{Key: "admin.pb.bao_cao_khtc", Label: "Báo cáo cho KHTC", Route: adminDeptRoute("/bao-cao-khtc"), Icon: "send"}

// SharedGroup là nhóm "Dùng chung" cuối menu của MỌI đối tượng (HCTH v3.0 FN-13, UR-GEN-01: Contact là
// module dùng chung, cột hiển thị theo vai trò). W11.23: Contact (pf/contact `cap/directory`, enable always — không cần ánh
// xạ menuModules) đã có màn danh sách, tìm kiếm, chi tiết — mở cho mọi vai; máy chủ lọc trường theo vai.
var SharedGroup = MenuGroup{Key: GroupDungChung, Label: GroupDungChungLabel, Items: []MenuItem{
	{Key: "chung.contact", Label: "Contact", Route: "/kn/dung-chung/contact", Icon: "phone"},
}}

// needs gắn điều kiện phân quyền trong vai (W11.07) cho một mục.
func needs(it MenuItem, n string) MenuItem {
	it.Needs = n
	return it
}

// sharedByAudience: mục thêm vào nhóm "Dùng chung" theo đối tượng (W11.07, Q-067 — đề xuất của dev: lãnh đạo
// các phòng làm việc nhân sự của phòng mình ngay trong phiên phòng ban của mình, không cần gán thêm phòng ban
// HCTH). Màn thuộc pf/hrm — W11.22 đã mở: danh sách hồ sơ trong phạm vi "đơn vị mình" của ma trận FN-20.
var sharedByAudience = map[string][]MenuItem{
	port.AudienceTinh: {
		needs(MenuItem{Key: "tinh.chung.nhan_su_phong", Label: "Nhân sự của phòng", Route: "/kn/nhan-su/cua-phong", Icon: "users"}, NeedsManager),
		// W11.28 (FN-06 BR-02): lãnh đạo phòng xem dashboard nhân sự của phòng mình (máy chủ giới hạn phạm vi).
		needs(MenuItem{Key: "tinh.chung.dashboard_nhan_su", Label: "Dashboard nhân sự", Route: "/kn/nhan-su/dashboard", Icon: "chart"}, NeedsManager),
		needs(MenuItem{Key: "tinh.chung.tai_lieu_phong", Label: "Tài liệu của phòng", Route: "/kn/nhan-su/tai-lieu-phong", Icon: "library"}, NeedsManager),
	},
}

// deptBusiness: nhóm "Nghiệp vụ" theo khuôn × đối tượng. Đối tượng vắng = không có nhóm này.
var deptBusiness = map[string]map[string][]MenuItem{
	// Chăn nuôi - Thủy sản (BRD CNTS v3.7 §VI.3; Cấp TW chỉ xem dự án KNQG).
	port.MenuProfileChanNuoiThuySan: {
		port.AudienceTW: {
			soon("tw.cnts.du_an_knqg", "Dự án KNQG", "/kn/cnts/du-an-knqg", "report"),
		},
		port.AudienceTinh: {
			// W11.43: cảnh báo (12 loại Mục III.5 + trễ liên tiếp), giao/đóng xử lý, cấu hình nhắc nhở theo biểu, ngưỡng.
			{Key: "tinh.cnts.canh_bao", Label: "Cảnh báo", Route: "/kn/cnts/canh-bao", Icon: "zap"},
			needs(soon("tinh.cnts.duyet_bao_cao", "Duyệt báo cáo", "/kn/cnts/duyet-bao-cao", "approve"), NeedsManager),
			// W11.42: phát hành biểu kỳ Cấp Xã BM-01/04/05/17 (kỳ native cap/forms), theo dõi nộp, chốt/mở lại.
			{Key: "tinh.cnts.bieu_mau", Label: "Biểu mẫu & cấp quyền", Route: "/kn/cnts/bieu-mau", Icon: "table"},
			// W11.49: Báo cáo dịch bệnh BM-10 — xem ổ dịch, diễn biến, tin báo nghi dịch; xác nhận kết thúc ổ dịch.
			{Key: "tinh.cnts.dich_benh", Label: "Báo cáo dịch bệnh", Route: "/kn/cnts/dich-benh", Icon: "shield"},
			// W11.44 (FN-16 — Báo cáo: KSGM tuần, tháng; Thủy sản (Phụ lục 2)): BM-02 + dòng điều chỉnh, BM-18 + nuôi trồng, lũy
			// kế, chốt tuần (số công bố cho KHTC), tệp chia sẻ Chi cục có phiên bản.
			{Key: "tinh.cnts.ksgm", Label: "KSGM tuần, tháng", Route: "/kn/cnts/ksgm", Icon: "chart"},
			{Key: "tinh.cnts.phu_luc_2", Label: "Thủy sản (Phụ lục 2)", Route: "/kn/cnts/phu-luc-2", Icon: "report"},
			// W11.46: lịch trực KSGM BM-03, lịch tiêm phòng BM-07, lịch công tác (BM-06, duyệt lịch cán bộ xã, check-in).
			{Key: "tinh.cnts.lich", Label: "Lịch trực KSGM", Route: "/kn/cnts/lich-truc", Icon: "calendar"},
			{Key: "tinh.cnts.lich_tiem", Label: "Lịch tiêm phòng", Route: "/kn/cnts/lich-tiem", Icon: "check"},
			{Key: "tinh.cnts.lich_cong_tac", Label: "Lịch công tác", Route: "/kn/nhiem-vu/lich-cong-tac", Icon: "calendar", BadgeSource: BadgeScheduleApprovals},
			soon("tinh.cnts.chien_dich", "Chiến dịch điều tra", "/kn/cnts/chien-dich", "megaphone"),
			soon("tinh.cnts.ban_do_dich", "Bản đồ vùng dịch", "/kn/cnts/ban-do-dich", "map"),
			soon("tinh.cnts.du_an_knqg", "Dự án KNQG / Điểm trình diễn", "/kn/cnts/du-an-knqg", "report"),
			soon("tinh.cnts.quy_trinh", "Quy trình, văn bản", "/kn/cnts/quy-trinh", "sop"),
			// W11.41: Cấp Tỉnh xem danh mục CNTS (FN-06: Admin CRUD, Cấp Tỉnh R).
			{Key: "tinh.cnts.danh_muc", Label: "Danh mục CNTS", Route: "/kn/cnts/danh-muc", Icon: "catalog"},
		},
		port.AudienceXa: {
			// W11.42: nhập biểu kỳ Cấp Xã (KSGM hằng ngày, nhập liệu tuần, khai thác thủy sản, công việc tuần).
			{Key: "xa.cnts.bao_cao_ky", Label: "Báo cáo kỳ CNTS", Route: "/kn/cnts/bao-cao-ky", Icon: "task"},
			// W11.49: lập BM-10, cập nhật diễn biến, ghi nhận / xác nhận báo nghi dịch.
			{Key: "xa.cnts.dich_benh", Label: "Báo cáo dịch bệnh", Route: "/kn/cnts/dich-benh", Icon: "shield"},
			// W11.43: cảnh báo của các xã phụ trách, xử lý cảnh báo được giao (FN-13 Cấp Xã).
			{Key: "xa.cnts.canh_bao", Label: "Cảnh báo", Route: "/kn/cnts/canh-bao", Icon: "zap"},
			// W11.46: lịch trực KSGM của tôi, lịch tiêm (ghi ngày tiêm thực tế), lịch công tác + check-in.
			{Key: "xa.cnts.lich", Label: "Lịch trực của tôi", Route: "/kn/cnts/lich-truc", Icon: "calendar"},
			{Key: "xa.cnts.lich_tiem", Label: "Lịch tiêm", Route: "/kn/cnts/lich-tiem", Icon: "check"},
			{Key: "xa.cnts.lich_cong_tac", Label: "Lịch công tác", Route: "/kn/nhiem-vu/lich-cong-tac", Icon: "calendar"},
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
			// W11.62: Kỳ KHTC — mở kỳ, tiến độ phiếu các phòng + nguồn, đóng/hủy kỳ.
			{Key: "tinh.khtc.ky_bao_cao", Label: "Kỳ báo cáo & phiếu các phòng", Route: "/kn/khtc/ky-bao-cao", Icon: "task"},
			// W11.61 (pf/extension, FN-TI-02.02): Kế hoạch năm + danh sách CT/DA (phòng khác chỉ xem).
			{Key: "tinh.khtc.ke_hoach_nam", Label: "Kế hoạch năm", Route: "/kn/khtc/ke-hoach-nam", Icon: "calendar"},
			soon("tinh.khtc.so_lieu_ngoai", "Số liệu nguồn ngoài", "/kn/khtc/so-lieu-ngoai", "database"),
			soon("tinh.khtc.mau_bao_cao", "Mẫu báo cáo KHTC", "/kn/khtc/mau-bao-cao", "table"),
			soon("tinh.khtc.tien_do", "Tiến độ các chương trình", "/kn/khtc/tien-do", "chart"),
			soon("tinh.khtc.da_xuat", "Báo cáo đã xuất", "/kn/khtc/bao-cao-da-xuat", "report"),
		},
	},
	// Thông tin Tuyên truyền — module Báo giá nông sản (BRD Báo giá v1.3 §VI.3).
	port.MenuProfileThongTinTuyenTruyen: {
		port.AudienceTW: {
			// W11.77: báo cáo giá đã mở.
			{Key: "tw.bao_gia.bang_gia", Label: "Bảng giá tổng hợp", Route: "/kn/bao-gia/bang-gia", Icon: "chart"},
			{Key: "tw.bao_gia.dashboard", Label: "Dashboard theo vùng", Route: "/kn/bao-gia/dashboard-vung", Icon: "map"},
			{Key: "tw.bao_gia.bien_dong", Label: "Báo cáo biến động giá", Route: "/kn/bao-gia/bien-dong", Icon: "layers"},
			soon("tw.bao_gia.gia_thanh", "Giá thành sản xuất", "/kn/bao-gia/gia-thanh", "report"),
		},
		port.AudienceTinh: {
			{Key: "tinh.bao_gia.tien_do", Label: "Tiến độ nhập giá", Route: "/kn/bao-gia/tien-do", Icon: "bell"}, // W11.77
			// W11.75: chi tiết báo giá, cảnh báo + xác minh, hiệu chỉnh / xóa / nhập thay (FN-TI-02.02…04, 02.09) — đã mở;
			// nút "Duyệt bảng giá" thêm ở W11.76.
			{Key: "tinh.bao_gia.kiem_duyet", Label: "Kiểm duyệt & công bố", Route: "/kn/bao-gia/kiem-duyet", Icon: "approve"},
			{Key: "tinh.bao_gia.bang_gia", Label: "Bảng giá tổng hợp", Route: "/kn/bao-gia/bang-gia", Icon: "chart"},        // W11.77
			{Key: "tinh.bao_gia.bien_dong", Label: "Báo cáo biến động giá", Route: "/kn/bao-gia/bien-dong", Icon: "layers"}, // W11.77
			{Key: "tinh.bao_gia.dashboard", Label: "Dashboard theo vùng", Route: "/kn/bao-gia/dashboard-vung", Icon: "map"}, // W11.77
			{Key: "tinh.bao_gia.thong_ke", Label: "Thống kê phiếu báo giá", Route: "/kn/bao-gia/thong-ke", Icon: "table"},   // W11.77
			soon("tinh.bao_gia.dieu_tra", "Điều tra giá thành", "/kn/bao-gia/dieu-tra", "season"),
			soon("tinh.bao_gia.mat_hang", "Mặt hàng", "/kn/bao-gia/mat-hang", "package"),
			// W11.07: cấu hình kỳ báo giá, giờ khóa, nhắc, ngưỡng — chỉ QTNV (Báo giá v1.3 FN-AD-04.04…06). W11.72: màn
			// lịch báo giá của W11.74 (cùng màn Admin /kn/phong-ban/{department}/bao-gia-lich) — đã mở.
			needs(MenuItem{Key: "tinh.bao_gia.cau_hinh", Label: "Cấu hình báo giá", Route: "/kn/bao-gia/cau-hinh", Icon: "param"},
				NeedsPriceAdmin),
		},
		port.AudienceXa: {
			// W11.75: nhập báo giá, yêu cầu xác minh, lịch sử báo giá (FN-XA-03.01…03, 03.05) — đã mở.
			{Key: "xa.bao_gia.nhap", Label: "Nhập báo giá", Route: "/kn/bao-gia/nhap", Icon: "upload"},
			{Key: "xa.bao_gia.xac_minh", Label: "Yêu cầu xác minh", Route: "/kn/bao-gia/xac-minh", Icon: "check"},
			{Key: "xa.bao_gia.lich_su", Label: "Lịch sử báo giá", Route: "/kn/bao-gia/lich-su", Icon: "journal"},
			{Key: "xa.bao_gia.bang_gia", Label: "Bảng giá tổng hợp", Route: "/kn/bao-gia/bang-gia", Icon: "chart"},        // W11.77
			{Key: "xa.bao_gia.bien_dong", Label: "Báo cáo biến động giá", Route: "/kn/bao-gia/bien-dong", Icon: "layers"}, // W11.77
		},
	},
	// Hành chính - Tổng hợp (BRD HCTH v3.0 §VI.3; Cấp TW chỉ có Contact ở "Dùng chung").
	port.MenuProfileHanhChinhTongHop: {
		port.AudienceTinh: {
			// W11.22 (pf/hrm, FN-07, FN-08): hồ sơ nhân sự — danh sách + chi tiết theo thẻ.
			// W11.28 (pf/hrm, FN-06, FN-12): dashboard nhân sự, biểu mẫu báo cáo Sở NS01–NS06.
			{Key: "tinh.hcth.dashboard", Label: "Dashboard nhân sự", Route: "/kn/nhan-su/dashboard", Icon: "chart"},
			{Key: "tinh.hcth.ho_so", Label: "Hồ sơ nhân sự", Route: "/kn/nhan-su/ho-so", Icon: "users"},
			{Key: "tinh.hcth.bao_cao_so", Label: "Biểu mẫu báo cáo Sở", Route: "/kn/nhan-su/bao-cao-so", Icon: "report"},
			{Key: "tinh.hcth.kho_tai_lieu", Label: "Hồ sơ dữ liệu", Route: "/kn/nhan-su/kho-tai-lieu", Icon: "library"},
			soon("tinh.hcth.danh_gia", "Đánh giá", "/kn/nhan-su/danh-gia", "approve"),
			// W11.21 (pf/hrm, FN-19): Cấp Tỉnh – HCTH xem danh mục nhân sự (Admin sửa).
			{Key: "tinh.hcth.danh_muc", Label: "Danh mục nhân sự", Route: "/kn/nhan-su/danh-muc", Icon: "catalog"},
		},
		port.AudienceXa: {
			soon("xa.hcth.danh_gia", "Đánh giá của tôi", "/kn/nhan-su/danh-gia-cua-toi", "approve"),
		},
	},
}

// adminDeptBusiness: mục nghiệp vụ ("Sắp có" cho tới khi module có màn) thêm vào mục con của phòng ban trong nhóm
// "Phòng ban" của Admin.
var adminDeptBusiness = map[string][]MenuItem{
	port.MenuProfileChanNuoiThuySan: {
		{Key: "admin.pb.cnts_danh_muc", Label: "Danh mục CNTS", Route: adminDeptRoute("/danh-muc-cnts"), Icon: "catalog"}, // W11.41
		{Key: "admin.pb.cnts_bieu_ky", Label: "Biểu kỳ Cấp Xã", Route: adminDeptRoute("/bieu-ky-cnts"), Icon: "table"},    // W11.42
		{Key: "admin.pb.cnts_lich_truc", Label: "Lịch trực KSGM", Route: adminDeptRoute("/lich-truc"), Icon: "calendar"},
		{Key: "admin.pb.cnts_lich_tiem", Label: "Lịch tiêm phòng", Route: adminDeptRoute("/lich-tiem"), Icon: "check"},
	},
	port.MenuProfileKeHoachTaiChinh: {
		// W11.61 (pf/extension): Kế hoạch năm (FN-TI-02.02) và Danh mục KHTC (FN-AD-04.01).
		{Key: "admin.pb.khtc_ke_hoach", Label: "Kế hoạch năm", Route: adminDeptRoute("/ke-hoach-nam"), Icon: "calendar"},
		{Key: "admin.pb.khtc_danh_muc", Label: "Danh mục KHTC", Route: adminDeptRoute("/danh-muc-khtc"), Icon: "catalog"},
	},
	// W11.72: Xã báo giá (FN-AD-04.02, phân công FN-AD-04.01) và Kỳ báo giá & giờ khóa (W11.74 FN-AD-04.04…06) đã mở.
	port.MenuProfileThongTinTuyenTruyen: {
		{Key: "admin.pb.bao_gia_xa", Label: "Xã báo giá", Route: "/kn/bao-gia/xa-bao-gia", Icon: "map"},
		{Key: "admin.pb.bao_gia_lich", Label: "Kỳ báo giá & giờ khóa", Route: adminDeptRoute("/bao-gia-lich"), Icon: "calendar"},
		soon("admin.pb.bao_gia_mat_hang", "Mặt hàng", "/kn/bao-gia/mat-hang", "package"),
	},
	port.MenuProfileHanhChinhTongHop: {
		// W11.21 (pf/hrm, FN-19): Admin thêm, sửa, ngừng sử dụng danh mục nhân sự.
		{Key: "admin.pb.hcth_danh_muc", Label: "Danh mục nhân sự", Route: adminDeptRoute("/danh-muc-nhan-su"), Icon: "catalog"},
		// W11.22 (pf/hrm, FN-20): ma trận phân quyền hồ sơ.
		{Key: "admin.pb.hcth_phan_quyen", Label: "Phân quyền hồ sơ", Route: adminDeptRoute("/phan-quyen-ho-so"), Icon: "shield"},
		// W11.24 (pf/hrm, FN-14, UR-ADM-10): cấu trúc thư mục tài liệu dùng chung + quyền thư mục.
		{Key: "admin.pb.hcth_thu_muc", Label: "Thư mục tài liệu", Route: adminDeptRoute("/thu-muc-tai-lieu"), Icon: "library"},
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
