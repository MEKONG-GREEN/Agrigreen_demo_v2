package domain

// Menu theo cấu hình (UR-KT-02, FN-CH-00.01, BRD v7.7 §6, Hình 3c–3f): bảng tĩnh CÓ PHIÊN BẢN trong mã.
// Backend trả cây menu theo đối tượng (vai KN của phiên) + phòng ban; web-ops (P6) và app (P7) chỉ vẽ
// lại — bật một phòng ban ("Đang áp dụng") hiện menu ngay, không phát hành lại ứng dụng.
//
//   - Mỗi phòng ban chọn một bộ menu (menu_profile): "trong_trot" (Phòng Trồng trọt, đủ 25 FN của
//     module), "generic" (phòng ban mới mở bằng cấu hình danh mục + mẫu báo cáo — chỉ khung chung
//     Phát hành/Nộp/Rà soát/Tổng hợp/Báo cáo theo loại kỳ), hoặc từ kn.menu/v3 (W11.06) một khuôn riêng
//     của phòng ban: chan_nuoi_thuy_san, ke_hoach_tai_chinh, thong_tin_truyen_thong, hanh_chinh_tong_hop
//     = khung chung + nhóm "Nghiệp vụ" của phòng (BRD CNTS v3.7, KHTC v2.2, Báo giá v1.3, HCTH v3.0 §VI.3).
//     Phòng ban cũ còn menu_profile "generic" mà mã có khuôn riêng → dùng khuôn riêng (EffectiveMenuProfile).
//   - kn.menu/v3: mục có ComingSoon (module chưa có) hiện mờ nhãn "Sắp có", không bấm được (W11.06 chọn
//     "hiện Sắp có" thay vì ẩn sau cờ — người dùng thấy lộ trình, không cần bật cờ khi module tới; gói của
//     module bỏ ComingSoon khi màn sẵn sàng). Mọi đối tượng có nhóm cuối "Dùng chung" (Contact, HCTH FN-13).
//   - HỢP ĐỒNG ĐƯỜNG DẪN (KN-fix — một hợp đồng chung Go ↔ web-ops ↔ bản giả lập): đường dẫn là trang
//     của web-ops. Vai + phòng ban của vai KN nằm trong NGỮ CẢNH PHIÊN (P1, header X-AG-Context), KHÔNG
//     nằm trong URL: "/kn/<màn>" (vd /kn/nhiem-vu/lich-cong-tac cho cả Cấp Tỉnh và Cấp Xã — màn đọc vai
//     từ ngữ cảnh). Chỉ màn của Admin theo phòng ban mang mã phòng ban: "/kn/phong-ban/{department}/<màn>"
//     ("{department}" thay bằng mã). Màn hiện có ngoài /kn giữ đường dẫn cũ (/quan-tri/...).
//     Bản giả lập web (apps/web-ops/src/api/mock/kn.ts) khớp bảng này qua ca vàng
//     apps/web-ops/src/api/mock/fixtures/kn-menu.json (Go TestWebMenuFixture ↔ Vitest kn-menu.contract.test.ts).
//   - Icon là tên NavIcon của web-ops (app di động tự ánh xạ).
//   - Mục có Requires chỉ hiện khi phòng ban đã có mẫu báo cáo gán cho loại báo cáo đó ở cấp đó
//     (FN-AD-04.06 "Báo cáo đột xuất").
//   - BadgeSource là khóa số đếm "việc cần xử lý" (ẩn khi bằng 0, §4.0) — client gọi API của module
//     sở hữu (P4/P5) để lấy số; menu chỉ khai báo khóa.
//
// Đổi menu = sửa bảng này VÀ tăng MenuVersion (client có thể lưu đệm theo phiên bản), rồi sinh lại ca vàng
// web: AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/app -run TestWebMenuFixture.

import (
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// MenuVersion là phiên bản của bảng menu tĩnh.
const MenuVersion = "kn.menu/v3"

// Mã loại báo cáo của danh mục report_type (pf/masterdata) mà menu tham chiếu.
const ReportTypeAdHoc = "dot_xuat"

// Cấp nhập liệu của mẫu báo cáo (khớp P3).
const (
	LevelProvince = "province"
	LevelCommune  = "commune"
)

// MenuRequirement là điều kiện hiện một mục.
type MenuRequirement struct {
	ReportType string // mã danh mục report_type
	Level      string // LevelProvince | LevelCommune
}

// MenuItem là một mục menu (có thể có mục con).
type MenuItem struct {
	Key         string
	Label       string
	Route       string // rỗng với mục cha chỉ để mở/đóng
	Icon        string
	BadgeSource string
	Requires    *MenuRequirement
	ComingSoon  bool // module chưa có: hiện mờ "Sắp có" (kn.menu/v3)
	Children    []MenuItem
}

// Trạng thái mục menu "Sắp có" (kn.menu/v3; MenuItemView.Status).
const (
	ItemComingSoon      = "coming_soon"
	ItemComingSoonLabel = "Sắp có"
)

// MenuGroup là một nhóm sidebar (Điều hành / Báo cáo / Dữ liệu; Admin: Điều hành / Phòng ban /
// Quản trị hệ thống).
type MenuGroup struct {
	Key   string
	Label string
	Items []MenuItem
}

// Khóa số đếm (badge_source) — module sở hữu trả số qua API của mình.
const (
	BadgeReportTasksTodo    = "extension.report_tasks.todo"            // P4: công việc cần làm của Cấp Xã
	BadgeSubmissionStatus   = "extension.report_status.not_submitted"  // P4: xã chưa nộp/trễ hạn
	BadgeReviewPending      = "extension.submissions.pending_review"   // P4: bài nộp chờ rà soát
	BadgeScheduleApprovals  = "extension.work_schedules.pending"       // P5: lịch chờ duyệt
	BadgeAdHocReportsOpen   = "extension.report_tasks.ad_hoc_todo"     // P4: báo cáo đột xuất cần làm
	BadgeAdHocReportsStatus = "extension.report_status.ad_hoc_pending" // P4: báo cáo đột xuất chưa nộp
)

// adminDeptRoute: màn của Admin theo phòng ban (P3b: /kn/phong-ban/{department}/mau-bao-cao).
func adminDeptRoute(path string) string { return "/kn/phong-ban/{department}" + path }

// Bộ menu Phòng Trồng trọt (BRD v7.7 §6).
var trongTrotMenus = map[string][]MenuGroup{
	port.AudienceTW: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "tw.dashboard", Label: "Dashboard", Route: "/kn/dashboard", Icon: "dashboard"},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "tw.bao_cao.tuan", Label: "Báo cáo tuần", Route: "/kn/bao-cao/tuan", Icon: "file"},
			{Key: "tw.bao_cao.thang", Label: "Báo cáo tháng", Route: "/kn/bao-cao/thang", Icon: "calendar"},
			{Key: "tw.bao_cao.hang_vu", Label: "Báo cáo hàng vụ", Route: "/kn/bao-cao/vu", Icon: "sprout"},
			{Key: "tw.bao_cao.nam", Label: "Báo cáo năm", Route: "/kn/bao-cao/nam", Icon: "chart"},
		}},
	},
	port.AudienceTinh: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "tinh.dashboard", Label: "Dashboard", Route: "/kn/dashboard", Icon: "dashboard"},
			{Key: "tinh.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "tinh.nhiem_vu.phat_hanh", Label: "Phát hành biểu mẫu", Route: "/kn/nhiem-vu/phat-hanh", Icon: "send"},
				{Key: "tinh.nhiem_vu.trang_thai_nop", Label: "Trạng thái nộp", Route: "/kn/nhiem-vu/trang-thai-nop", Icon: "bell", BadgeSource: BadgeSubmissionStatus},
				{Key: "tinh.nhiem_vu.ra_soat", Label: "Rà soát & chốt kỳ", Route: "/kn/nhiem-vu/ra-soat", Icon: "check", BadgeSource: BadgeReviewPending},
				{Key: "tinh.nhiem_vu.lich_cong_tac", Label: "Lịch công tác", Route: "/kn/nhiem-vu/lich-cong-tac", Icon: "calendar", BadgeSource: BadgeScheduleApprovals},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "tinh.bao_cao.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: "/kn/bao-cao/tong-hop", Icon: "layers"},
			{Key: "tinh.bao_cao.chi_cuc", Label: "Báo cáo Chi cục", Route: "/kn/bao-cao/chi-cuc", Icon: "file"},
			{Key: "tinh.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: "/kn/bao-cao/dot-xuat", Icon: "zap",
				BadgeSource: BadgeAdHocReportsStatus, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelProvince}},
		}},
	},
	port.AudienceXa: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "xa.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "xa.nhiem_vu.cong_viec", Label: "Công việc cần làm", Route: "/kn/nhiem-vu/cong-viec", Icon: "bell", BadgeSource: BadgeReportTasksTodo},
				{Key: "xa.nhiem_vu.lich_cong_tac", Label: "Lịch công tác", Route: "/kn/nhiem-vu/lich-cong-tac", Icon: "calendar"},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "xa.bao_cao.tuan", Label: "Báo cáo tuần", Route: "/kn/bao-cao/tuan", Icon: "file"},
			{Key: "xa.bao_cao.thang", Label: "Báo cáo tháng", Route: "/kn/bao-cao/thang", Icon: "calendar"},
			{Key: "xa.bao_cao.hang_vu", Label: "Báo cáo hàng vụ", Route: "/kn/bao-cao/vu", Icon: "sprout"},
			{Key: "xa.bao_cao.cua_xa", Label: "Báo cáo của xã", Route: "/kn/bao-cao/cua-xa", Icon: "chart"},
			{Key: "xa.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: "/kn/bao-cao/dot-xuat", Icon: "zap",
				BadgeSource: BadgeAdHocReportsOpen, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelCommune}},
		}},
		{Key: "du_lieu", Label: "Dữ liệu", Items: []MenuItem{
			{Key: "xa.thu_thap", Label: "Thu thập dữ liệu", Icon: "database", Children: []MenuItem{
				{Key: "xa.thu_thap.htx", Label: "Danh sách HTX", Route: "/kn/du-lieu/htx", Icon: "coop"},
				{Key: "xa.thu_thap.may_moc", Label: "Máy móc HTX", Route: "/kn/du-lieu/may-moc", Icon: "tractor"},
				{Key: "xa.thu_thap.thua", Label: "Vẽ thửa ruộng", Route: "/kn/du-lieu/ve-thua", Icon: "map"},
				{Key: "xa.thu_thap.vat_tu", Label: "Vật tư", Route: "/kn/du-lieu/vat-tu", Icon: "package"},
			}},
		}},
	},
}

// Bộ menu chung cho phòng ban mới mở bằng cấu hình (UR-KT-02).
var genericMenus = map[string][]MenuGroup{
	port.AudienceTW: trongTrotMenus[port.AudienceTW],
	port.AudienceTinh: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "tinh.dashboard", Label: "Dashboard", Route: "/kn/dashboard", Icon: "dashboard"},
			{Key: "tinh.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "tinh.nhiem_vu.phat_hanh", Label: "Phát hành biểu mẫu", Route: "/kn/nhiem-vu/phat-hanh", Icon: "send"},
				{Key: "tinh.nhiem_vu.trang_thai_nop", Label: "Trạng thái nộp", Route: "/kn/nhiem-vu/trang-thai-nop", Icon: "bell", BadgeSource: BadgeSubmissionStatus},
				{Key: "tinh.nhiem_vu.ra_soat", Label: "Rà soát & chốt kỳ", Route: "/kn/nhiem-vu/ra-soat", Icon: "check", BadgeSource: BadgeReviewPending},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "tinh.bao_cao.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: "/kn/bao-cao/tong-hop", Icon: "layers"},
			{Key: "tinh.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: "/kn/bao-cao/dot-xuat", Icon: "zap",
				BadgeSource: BadgeAdHocReportsStatus, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelProvince}},
		}},
	},
	port.AudienceXa: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "xa.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "xa.nhiem_vu.cong_viec", Label: "Công việc cần làm", Route: "/kn/nhiem-vu/cong-viec", Icon: "bell", BadgeSource: BadgeReportTasksTodo},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "xa.bao_cao.tuan", Label: "Báo cáo tuần", Route: "/kn/bao-cao/tuan", Icon: "file"},
			{Key: "xa.bao_cao.thang", Label: "Báo cáo tháng", Route: "/kn/bao-cao/thang", Icon: "calendar"},
			{Key: "xa.bao_cao.hang_vu", Label: "Báo cáo hàng vụ", Route: "/kn/bao-cao/vu", Icon: "sprout"},
			{Key: "xa.bao_cao.cua_xa", Label: "Báo cáo của xã", Route: "/kn/bao-cao/cua-xa", Icon: "chart"},
			{Key: "xa.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: "/kn/bao-cao/dot-xuat", Icon: "zap",
				BadgeSource: BadgeAdHocReportsOpen, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelCommune}},
		}},
	},
}

// Mục con của một phòng ban trong nhóm "Phòng ban" của Admin (Hình 3c).
var adminDeptItems = map[string][]MenuItem{
	port.MenuProfileTrongTrot: {
		{Key: "admin.pb.theo_doi_nop", Label: "Theo dõi nộp báo cáo", Route: adminDeptRoute("/theo-doi-nop"), Icon: "bell"},
		{Key: "admin.pb.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: adminDeptRoute("/tong-hop"), Icon: "layers"},
		{Key: "admin.pb.du_lieu_bao_cao", Label: "Dữ liệu báo cáo các xã", Route: adminDeptRoute("/du-lieu-bao-cao"), Icon: "file"},
		{Key: "admin.pb.thu_thap", Label: "Thu thập dữ liệu", Route: adminDeptRoute("/thu-thap"), Icon: "database"},
		// FN-TI-02.07 (P5): Admin xem lịch công tác (chỉ đọc) — cùng màn của Cấp Tỉnh, chưa tách theo phòng ban.
		{Key: "admin.pb.lich_cong_tac", Label: "Lịch công tác", Route: "/kn/nhiem-vu/lich-cong-tac", Icon: "calendar"},
		// P6s: màn danh mục nông nghiệp theo phòng ban (FN-AD-04.05); màn danh mục G1 giữ ở /quan-tri/danh-muc.
		{Key: "admin.pb.danh_muc", Label: "Danh mục nông nghiệp", Route: adminDeptRoute("/danh-muc"), Icon: "package"},
		// P3b: trình thiết kế mẫu báo cáo dạng lưới Excel (FN-AD-04.06, Hình 27).
		{Key: "admin.pb.mau_bao_cao", Label: "Mẫu báo cáo", Route: adminDeptRoute("/mau-bao-cao"), Icon: "table"},
	},
	port.MenuProfileGeneric: {
		{Key: "admin.pb.theo_doi_nop", Label: "Theo dõi nộp báo cáo", Route: adminDeptRoute("/theo-doi-nop"), Icon: "bell"},
		{Key: "admin.pb.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: adminDeptRoute("/tong-hop"), Icon: "layers"},
		{Key: "admin.pb.du_lieu_bao_cao", Label: "Dữ liệu báo cáo các xã", Route: adminDeptRoute("/du-lieu-bao-cao"), Icon: "file"},
		{Key: "admin.pb.danh_muc", Label: "Danh mục nông nghiệp", Route: adminDeptRoute("/danh-muc"), Icon: "package"},
		{Key: "admin.pb.mau_bao_cao", Label: "Mẫu báo cáo", Route: adminDeptRoute("/mau-bao-cao"), Icon: "table"},
	},
}

// AdminDepartmentIcon trả icon của mục phòng ban trong nhóm "Phòng ban" theo bộ menu.
func AdminDepartmentIcon(profile string) string {
	if profile == port.MenuProfileTrongTrot {
		return "sprout"
	}
	return "building"
}

// AdminFixedGroups là nhóm Điều hành và Quản trị hệ thống của Admin (không phụ thuộc phòng ban).
var (
	AdminOverview = MenuGroup{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
		{Key: "admin.tong_quan", Label: "Tổng quan hệ thống", Route: "/kn/tong-quan", Icon: "dashboard"},
	}}
	AdminSystem = MenuGroup{Key: "quan_tri", Label: "Quản trị hệ thống", Items: []MenuItem{
		{Key: "admin.qt.dvhc", Label: "Đơn vị hành chính", Route: "/kn/quan-tri/don-vi-hanh-chinh", Icon: "map"},
		// P6s: màn KN v7.7 (FN-AD-04.02); màn G1 giữ ở Công cụ nền tảng — không xóa tính năng.
		{Key: "admin.qt.co_cau", Label: "Cơ cấu tổ chức 3 cấp", Route: "/kn/quan-tri/co-cau-to-chuc", Icon: "org"},
		{Key: "admin.qt.phong_ban", Label: "Phòng ban", Route: "/kn/quan-tri/phong-ban", Icon: "building"},
		{Key: "admin.qt.nguoi_dung", Label: "Người dùng", Route: "/kn/quan-tri/nguoi-dung", Icon: "users"},
		{Key: "admin.qt.menu_cac_cap", Label: "Menu các cấp", Route: "/kn/quan-tri/menu-cac-cap", Icon: "key"},
		// Q-13 (tạm, chờ khách): công cụ quản trị nền tảng đã có (G1–G5) đặt trong nhóm Quản trị hệ thống.
		{Key: "admin.qt.cong_cu", Label: "Công cụ nền tảng (tạm)", Icon: "param", Children: []MenuItem{
			{Key: "admin.qt.cong_cu.tham_so", Label: "Tham số & phê duyệt", Route: "/quan-tri/tham-so", Icon: "param"},
			{Key: "admin.qt.cong_cu.tai_khoan", Label: "Tài khoản cán bộ (G1)", Route: "/quan-tri/tai-khoan", Icon: "user"},
			{Key: "admin.qt.cong_cu.to_chuc", Label: "Cây tổ chức (G1)", Route: "/quan-tri/to-chuc", Icon: "org"},
			{Key: "admin.qt.cong_cu.mau_xuat", Label: "Mẫu xuất & lịch gửi", Route: "/quan-tri/mau-bao-cao", Icon: "report"},
			{Key: "admin.qt.cong_cu.xung_dot", Label: "Xung đột đồng bộ", Route: "/quan-tri/xung-dot", Icon: "conflict"},
			{Key: "admin.qt.cong_cu.giam_sat", Label: "Giám sát đồng bộ", Route: "/quan-tri/giam-sat-dong-bo", Icon: "sync"},
			{Key: "admin.qt.cong_cu.thiet_bi", Label: "Đồng bộ thiết bị", Route: "/dong-bo", Icon: "sync"},
			// G5 (ADR-0022/0023/0024): Public API cho bên thứ ba — màn tự kiểm vai (api_client_*, metering_viewer,
			// billing_*, platform_admin); menu Admin chỉ để tới được màn.
			{Key: "admin.qt.cong_cu.khach_hang_api", Label: "Khách hàng Public API", Route: "/quan-tri/khach-hang-api", Icon: "key"},
			{Key: "admin.qt.cong_cu.su_dung_api", Label: "Sử dụng Public API", Route: "/quan-tri/su-dung-api", Icon: "chart"},
			{Key: "admin.qt.cong_cu.bang_ke", Label: "Bảng kê sử dụng", Route: "/quan-tri/bang-ke", Icon: "report"},
			{Key: "admin.qt.cong_cu.nhat_ky_xuat", Label: "Nhật ký xuất dữ liệu", Route: "/quan-tri/nhat-ky-xuat", Icon: "shield"},
		}},
	}}
)

// AdminDepartmentGroup là khóa/nhãn của nhóm "Phòng ban" của Admin.
const (
	AdminDepartmentGroupKey   = "phong_ban"
	AdminDepartmentGroupLabel = "Phòng ban"
)

// defaultRoutes là trang mặc định theo đối tượng (FN-CH-00.01). Trùng accounts/domain.DefaultRoute (P1,
// default_route của ngữ cảnh phiên) — cả hai kiểm với ca vàng web kn-menu.json.
var defaultRoutes = map[string]string{
	port.AudienceTW:    "/kn/dashboard",
	port.AudienceTinh:  "/kn/dashboard",
	port.AudienceXa:    "/kn/nhiem-vu/cong-viec",
	port.AudienceAdmin: "/kn/tong-quan",
}

var profiles = map[string]map[string][]MenuGroup{
	port.MenuProfileTrongTrot:           trongTrotMenus,
	port.MenuProfileGeneric:             genericMenus,
	port.MenuProfileChanNuoiThuySan:     withBusiness(port.MenuProfileChanNuoiThuySan),
	port.MenuProfileKeHoachTaiChinh:     withBusiness(port.MenuProfileKeHoachTaiChinh),
	port.MenuProfileThongTinTuyenTruyen: withBusiness(port.MenuProfileThongTinTuyenTruyen),
	port.MenuProfileHanhChinhTongHop:    withBusiness(port.MenuProfileHanhChinhTongHop),
}

// MenuProfiles là mọi bộ menu có trong mã (thứ tự cố định — OpenAPI enum menu_profile).
var MenuProfiles = []string{port.MenuProfileTrongTrot, port.MenuProfileGeneric, port.MenuProfileChanNuoiThuySan,
	port.MenuProfileKeHoachTaiChinh, port.MenuProfileThongTinTuyenTruyen, port.MenuProfileHanhChinhTongHop}

// KnownMenuProfile cho biết p là bộ menu có trong mã.
func KnownMenuProfile(p string) bool { _, ok := profiles[p]; return ok }

// KnownAudience cho biết a là đối tượng menu.
func KnownAudience(a string) bool { return slices.Contains(port.Audiences, a) }

// DepartmentMenu trả nhóm menu của đối tượng (kn_tw/kn_tinh/kn_xa) cho phòng ban dùng bộ profile
// ("{department}" nếu có thì thay bằng mã), kết thúc bằng nhóm "Dùng chung" (kn.menu/v3). Bản sao sâu —
// người gọi được lọc/sửa.
func DepartmentMenu(profile, audience, department string) []MenuGroup {
	groups := profiles[EffectiveMenuProfile(profile, department)][audience]
	out := make([]MenuGroup, 0, len(groups)+1)
	for _, g := range groups {
		out = append(out, MenuGroup{Key: g.Key, Label: g.Label, Items: bind(g.Items, department)})
	}
	return append(out, SharedMenuGroup())
}

// SharedMenuGroup trả bản sao nhóm "Dùng chung" (cuối menu mọi đối tượng, kể cả Admin).
func SharedMenuGroup() MenuGroup {
	return MenuGroup{Key: SharedGroup.Key, Label: SharedGroup.Label, Items: bind(SharedGroup.Items, "")}
}

// AdminDepartmentItems trả mục con của một phòng ban trong nhóm "Phòng ban" của Admin: Trồng trọt có bộ
// riêng; phòng ban khác dùng bộ chung + mục "Sắp có" của khuôn riêng (kn.menu/v3).
func AdminDepartmentItems(profile, department string) []MenuItem {
	profile = EffectiveMenuProfile(profile, department)
	items, ok := adminDeptItems[profile]
	if !ok {
		items = append(slices.Clone(adminDeptItems[port.MenuProfileGeneric]), adminDeptBusiness[profile]...)
	}
	return bind(items, department)
}

// DefaultRoute trả trang mặc định của đối tượng (không phụ thuộc phòng ban — phòng ban ở ngữ cảnh phiên).
func DefaultRoute(audience string) string { return defaultRoutes[audience] }

func bind(items []MenuItem, department string) []MenuItem {
	out := make([]MenuItem, 0, len(items))
	for _, it := range items {
		c := it
		c.Route = strings.ReplaceAll(it.Route, "{department}", department)
		if it.Requires != nil {
			req := *it.Requires
			c.Requires = &req
		}
		c.Children = bind(it.Children, department)
		out = append(out, c)
	}
	return out
}
