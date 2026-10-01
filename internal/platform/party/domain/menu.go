package domain

// Menu theo cấu hình (UR-KT-02, FN-CH-00.01, BRD v7.7 §6, Hình 3c–3f): bảng tĩnh CÓ PHIÊN BẢN trong mã.
// Backend trả cây menu theo đối tượng (vai KN của phiên) + phòng ban; web-ops (P6) và app (P7) chỉ vẽ
// lại — bật một phòng ban ("Đang áp dụng") hiện menu ngay, không phát hành lại ứng dụng.
//
//   - Mỗi phòng ban chọn một bộ menu (menu_profile): "trong_trot" (Phòng Trồng trọt, đủ 25 FN của
//     module) hoặc "generic" (phòng ban mới mở bằng cấu hình danh mục + mẫu báo cáo — chỉ khung chung
//     Phát hành/Nộp/Rà soát/Tổng hợp/Báo cáo theo loại kỳ).
//   - Đường dẫn chứa "{department}" thay bằng mã phòng ban.
//   - Mục có Requires chỉ hiện khi phòng ban đã có mẫu báo cáo gán cho loại báo cáo đó ở cấp đó
//     (FN-AD-04.06 "Báo cáo đột xuất").
//   - BadgeSource là khóa số đếm "việc cần xử lý" (ẩn khi bằng 0, §4.0) — client gọi API của module
//     sở hữu (P4/P5) để lấy số; menu chỉ khai báo khóa.
//
// Đổi menu = sửa bảng này VÀ tăng MenuVersion (client có thể lưu đệm theo phiên bản).

import (
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// MenuVersion là phiên bản của bảng menu tĩnh.
const MenuVersion = "kn.menu/v1"

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
	Children    []MenuItem
}

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

func deptRoute(path string) string { return "/kn/{department}" + path }

// Bộ menu Phòng Trồng trọt (BRD v7.7 §6).
var trongTrotMenus = map[string][]MenuGroup{
	port.AudienceTW: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "tw.dashboard", Label: "Dashboard", Route: deptRoute("/tw/dashboard"), Icon: "dashboard"},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "tw.bao_cao.tuan", Label: "Báo cáo tuần", Route: deptRoute("/tw/bao-cao/tuan"), Icon: "report"},
			{Key: "tw.bao_cao.thang", Label: "Báo cáo tháng", Route: deptRoute("/tw/bao-cao/thang"), Icon: "calendar"},
			{Key: "tw.bao_cao.hang_vu", Label: "Báo cáo hàng vụ", Route: deptRoute("/tw/bao-cao/hang-vu"), Icon: "season"},
			{Key: "tw.bao_cao.nam", Label: "Báo cáo năm", Route: deptRoute("/tw/bao-cao/nam"), Icon: "chart"},
		}},
	},
	port.AudienceTinh: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "tinh.dashboard", Label: "Dashboard", Route: deptRoute("/tinh/dashboard"), Icon: "dashboard"},
			{Key: "tinh.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "tinh.nhiem_vu.phat_hanh", Label: "Phát hành biểu mẫu", Route: deptRoute("/tinh/phat-hanh"), Icon: "send"},
				{Key: "tinh.nhiem_vu.trang_thai_nop", Label: "Trạng thái nộp", Route: deptRoute("/tinh/trang-thai-nop"), Icon: "bell", BadgeSource: BadgeSubmissionStatus},
				{Key: "tinh.nhiem_vu.ra_soat", Label: "Rà soát & chốt kỳ", Route: deptRoute("/tinh/ra-soat"), Icon: "approve", BadgeSource: BadgeReviewPending},
				{Key: "tinh.nhiem_vu.lich_cong_tac", Label: "Lịch công tác", Route: deptRoute("/tinh/lich-cong-tac"), Icon: "calendar", BadgeSource: BadgeScheduleApprovals},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "tinh.bao_cao.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: deptRoute("/tinh/tong-hop"), Icon: "layers"},
			{Key: "tinh.bao_cao.chi_cuc", Label: "Báo cáo Chi cục", Route: deptRoute("/tinh/chi-cuc"), Icon: "report"},
			{Key: "tinh.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: deptRoute("/tinh/dot-xuat"), Icon: "megaphone",
				BadgeSource: BadgeAdHocReportsStatus, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelProvince}},
		}},
	},
	port.AudienceXa: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "xa.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "xa.nhiem_vu.cong_viec", Label: "Công việc cần làm", Route: deptRoute("/xa/cong-viec"), Icon: "bell", BadgeSource: BadgeReportTasksTodo},
				{Key: "xa.nhiem_vu.lich_cong_tac", Label: "Lịch công tác", Route: deptRoute("/xa/lich-cong-tac"), Icon: "calendar"},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "xa.bao_cao.tuan", Label: "Báo cáo tuần", Route: deptRoute("/xa/bao-cao/tuan"), Icon: "report"},
			{Key: "xa.bao_cao.thang", Label: "Báo cáo tháng", Route: deptRoute("/xa/bao-cao/thang"), Icon: "calendar"},
			{Key: "xa.bao_cao.hang_vu", Label: "Báo cáo hàng vụ", Route: deptRoute("/xa/bao-cao/hang-vu"), Icon: "season"},
			{Key: "xa.bao_cao.cua_xa", Label: "Báo cáo của xã", Route: deptRoute("/xa/bao-cao/cua-xa"), Icon: "chart"},
			{Key: "xa.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: deptRoute("/xa/dot-xuat"), Icon: "megaphone",
				BadgeSource: BadgeAdHocReportsOpen, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelCommune}},
		}},
		{Key: "du_lieu", Label: "Dữ liệu", Items: []MenuItem{
			{Key: "xa.thu_thap", Label: "Thu thập dữ liệu", Icon: "database", Children: []MenuItem{
				{Key: "xa.thu_thap.htx", Label: "Danh sách HTX", Route: deptRoute("/xa/htx"), Icon: "coop"},
				{Key: "xa.thu_thap.may_moc", Label: "Máy móc HTX", Route: deptRoute("/xa/may-moc"), Icon: "tractor"},
				{Key: "xa.thu_thap.thua", Label: "Vẽ thửa ruộng", Route: deptRoute("/xa/ve-thua"), Icon: "plot"},
				{Key: "xa.thu_thap.vat_tu", Label: "Vật tư", Route: deptRoute("/xa/vat-tu"), Icon: "box"},
			}},
		}},
	},
}

// Bộ menu chung cho phòng ban mới mở bằng cấu hình (UR-KT-02).
var genericMenus = map[string][]MenuGroup{
	port.AudienceTW: trongTrotMenus[port.AudienceTW],
	port.AudienceTinh: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "tinh.dashboard", Label: "Dashboard", Route: deptRoute("/tinh/dashboard"), Icon: "dashboard"},
			{Key: "tinh.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "tinh.nhiem_vu.phat_hanh", Label: "Phát hành biểu mẫu", Route: deptRoute("/tinh/phat-hanh"), Icon: "send"},
				{Key: "tinh.nhiem_vu.trang_thai_nop", Label: "Trạng thái nộp", Route: deptRoute("/tinh/trang-thai-nop"), Icon: "bell", BadgeSource: BadgeSubmissionStatus},
				{Key: "tinh.nhiem_vu.ra_soat", Label: "Rà soát & chốt kỳ", Route: deptRoute("/tinh/ra-soat"), Icon: "approve", BadgeSource: BadgeReviewPending},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "tinh.bao_cao.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: deptRoute("/tinh/tong-hop"), Icon: "layers"},
			{Key: "tinh.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: deptRoute("/tinh/dot-xuat"), Icon: "megaphone",
				BadgeSource: BadgeAdHocReportsStatus, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelProvince}},
		}},
	},
	port.AudienceXa: {
		{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
			{Key: "xa.nhiem_vu", Label: "Nhiệm vụ", Icon: "task", Children: []MenuItem{
				{Key: "xa.nhiem_vu.cong_viec", Label: "Công việc cần làm", Route: deptRoute("/xa/cong-viec"), Icon: "bell", BadgeSource: BadgeReportTasksTodo},
			}},
		}},
		{Key: "bao_cao", Label: "Báo cáo", Items: []MenuItem{
			{Key: "xa.bao_cao.tuan", Label: "Báo cáo tuần", Route: deptRoute("/xa/bao-cao/tuan"), Icon: "report"},
			{Key: "xa.bao_cao.thang", Label: "Báo cáo tháng", Route: deptRoute("/xa/bao-cao/thang"), Icon: "calendar"},
			{Key: "xa.bao_cao.hang_vu", Label: "Báo cáo hàng vụ", Route: deptRoute("/xa/bao-cao/hang-vu"), Icon: "season"},
			{Key: "xa.bao_cao.cua_xa", Label: "Báo cáo của xã", Route: deptRoute("/xa/bao-cao/cua-xa"), Icon: "chart"},
			{Key: "xa.bao_cao.dot_xuat", Label: "Báo cáo đột xuất", Route: deptRoute("/xa/dot-xuat"), Icon: "megaphone",
				BadgeSource: BadgeAdHocReportsOpen, Requires: &MenuRequirement{ReportType: ReportTypeAdHoc, Level: LevelCommune}},
		}},
	},
}

// Mục con của một phòng ban trong nhóm "Phòng ban" của Admin (Hình 3c).
var adminDeptItems = map[string][]MenuItem{
	port.MenuProfileTrongTrot: {
		{Key: "admin.pb.theo_doi_nop", Label: "Theo dõi nộp báo cáo", Route: deptRoute("/admin/theo-doi-nop"), Icon: "bell"},
		{Key: "admin.pb.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: deptRoute("/admin/tong-hop"), Icon: "layers"},
		{Key: "admin.pb.du_lieu_bao_cao", Label: "Dữ liệu báo cáo các xã", Route: deptRoute("/admin/du-lieu-bao-cao"), Icon: "report"},
		{Key: "admin.pb.thu_thap", Label: "Thu thập dữ liệu", Route: deptRoute("/admin/thu-thap"), Icon: "database"},
		{Key: "admin.pb.danh_muc", Label: "Danh mục nông nghiệp", Route: deptRoute("/admin/danh-muc"), Icon: "catalog"},
		{Key: "admin.pb.mau_bao_cao", Label: "Mẫu báo cáo", Route: deptRoute("/admin/mau-bao-cao"), Icon: "template"},
	},
	port.MenuProfileGeneric: {
		{Key: "admin.pb.theo_doi_nop", Label: "Theo dõi nộp báo cáo", Route: deptRoute("/admin/theo-doi-nop"), Icon: "bell"},
		{Key: "admin.pb.tong_hop", Label: "Báo cáo tổng hợp các xã", Route: deptRoute("/admin/tong-hop"), Icon: "layers"},
		{Key: "admin.pb.du_lieu_bao_cao", Label: "Dữ liệu báo cáo các xã", Route: deptRoute("/admin/du-lieu-bao-cao"), Icon: "report"},
		{Key: "admin.pb.danh_muc", Label: "Danh mục nông nghiệp", Route: deptRoute("/admin/danh-muc"), Icon: "catalog"},
		{Key: "admin.pb.mau_bao_cao", Label: "Mẫu báo cáo", Route: deptRoute("/admin/mau-bao-cao"), Icon: "template"},
	},
}

// AdminFixedGroups là nhóm Điều hành và Quản trị hệ thống của Admin (không phụ thuộc phòng ban).
var (
	AdminOverview = MenuGroup{Key: "dieu_hanh", Label: "Điều hành", Items: []MenuItem{
		{Key: "admin.tong_quan", Label: "Tổng quan hệ thống", Route: "/quan-tri/tong-quan", Icon: "dashboard"},
	}}
	AdminSystem = MenuGroup{Key: "quan_tri", Label: "Quản trị hệ thống", Items: []MenuItem{
		{Key: "admin.qt.dvhc", Label: "Đơn vị hành chính", Route: "/quan-tri/don-vi-hanh-chinh", Icon: "map"},
		{Key: "admin.qt.co_cau", Label: "Cơ cấu tổ chức 3 cấp", Route: "/quan-tri/co-cau-to-chuc", Icon: "org"},
		{Key: "admin.qt.phong_ban", Label: "Phòng ban", Route: "/quan-tri/phong-ban", Icon: "building"},
		{Key: "admin.qt.nguoi_dung", Label: "Người dùng", Route: "/quan-tri/nguoi-dung", Icon: "user"},
		{Key: "admin.qt.menu_cac_cap", Label: "Menu các cấp", Route: "/quan-tri/menu-cac-cap", Icon: "key"},
	}}
)

// AdminDepartmentGroup là khóa/nhãn của nhóm "Phòng ban" của Admin.
const (
	AdminDepartmentGroupKey   = "phong_ban"
	AdminDepartmentGroupLabel = "Phòng ban"
)

// defaultRoutes là trang mặc định theo đối tượng (FN-CH-00.01).
var defaultRoutes = map[string]string{
	port.AudienceTW:    deptRoute("/tw/dashboard"),
	port.AudienceTinh:  deptRoute("/tinh/dashboard"),
	port.AudienceXa:    deptRoute("/xa/cong-viec"),
	port.AudienceAdmin: "/quan-tri/tong-quan",
}

var profiles = map[string]map[string][]MenuGroup{
	port.MenuProfileTrongTrot: trongTrotMenus,
	port.MenuProfileGeneric:   genericMenus,
}

// KnownMenuProfile cho biết p là bộ menu có trong mã.
func KnownMenuProfile(p string) bool { _, ok := profiles[p]; return ok }

// KnownAudience cho biết a là đối tượng menu.
func KnownAudience(a string) bool { return slices.Contains(port.Audiences, a) }

// DepartmentMenu trả nhóm menu của đối tượng (kn_tw/kn_tinh/kn_xa) cho phòng ban dùng bộ profile,
// đường dẫn đã thay mã phòng ban. Bản sao sâu — người gọi được lọc/sửa.
func DepartmentMenu(profile, audience, department string) []MenuGroup {
	groups := profiles[profile][audience]
	out := make([]MenuGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, MenuGroup{Key: g.Key, Label: g.Label, Items: bind(g.Items, department)})
	}
	return out
}

// AdminDepartmentItems trả mục con của một phòng ban trong nhóm "Phòng ban" của Admin.
func AdminDepartmentItems(profile, department string) []MenuItem {
	items, ok := adminDeptItems[profile]
	if !ok {
		items = adminDeptItems[port.MenuProfileGeneric]
	}
	return bind(items, department)
}

// DefaultRoute trả trang mặc định của đối tượng.
func DefaultRoute(audience, department string) string {
	return strings.ReplaceAll(defaultRoutes[audience], "{department}", department)
}

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
