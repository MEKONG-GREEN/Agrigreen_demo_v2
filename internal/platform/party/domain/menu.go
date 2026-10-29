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
const MenuVersion = "kn.menu/v8"

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
	// Needs (W11.07, phân quyền trong vai): mục chỉ hiện khi ngữ cảnh phiên có thuộc tính này — NeedsManager
	// (lãnh đạo) hoặc mã cờ nhiệm vụ (vd "qtnv_bao_gia"). Rỗng = mọi người của đối tượng.
	Needs    string
	Children []MenuItem
}

// Điều kiện phân quyền trong vai của mục menu (MenuItem.Needs).
const (
	NeedsManager    = "manager"      // lãnh đạo: cấp quản lý ≥ phó phòng/phụ trách hoặc nhóm "Lãnh đạo phòng"
	NeedsPriceAdmin = "qtnv_bao_gia" // QTNV báo giá (Báo giá v1.3)
)

// MenuTraits là thuộc tính của ngữ cảnh phiên dùng để lọc mục có Needs.
type MenuTraits struct {
	Manager bool
	Duties  []string
}

// Has cho biết traits đáp ứng điều kiện needs (rỗng luôn đáp ứng).
func (t MenuTraits) Has(needs string) bool {
	switch needs {
	case "":
		return true
	case NeedsManager:
		return t.Manager
	}
	return slices.Contains(t.Duties, needs)
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

// Bộ menu Phòng Trồng trọt (BRD v7.7 §6).
var trongTrotMenus = catalogValue[map[string][]MenuGroup]("trongTrotMenus")

// Bộ menu chung cho phòng ban mới mở bằng cấu hình (UR-KT-02).
var genericMenus = catalogValue[map[string][]MenuGroup]("genericMenus")

// Mục con của một phòng ban trong nhóm "Phòng ban" của Admin (Hình 3c).
var adminDeptItems = catalogValue[map[string][]MenuItem]("adminDeptItems")

// AdminDepartmentIcon trả icon của mục phòng ban trong nhóm "Phòng ban" theo bộ menu.
func AdminDepartmentIcon(profile string) string {
	if profile == port.MenuProfileTrongTrot {
		return "sprout"
	}
	return "building"
}

// AdminFixedGroups là nhóm Điều hành và Quản trị hệ thống của Admin (không phụ thuộc phòng ban).
var (
	AdminOverview = catalogValue[MenuGroup]("AdminOverview")
	AdminSystem   = catalogValue[MenuGroup]("AdminSystem")
)

// AdminDepartmentGroup là khóa/nhãn của nhóm "Phòng ban" của Admin.
const (
	AdminDepartmentGroupKey   = "phong_ban"
	AdminDepartmentGroupLabel = "Phòng ban"
)

// defaultRoutes là trang mặc định theo đối tượng (FN-CH-00.01). Trùng accounts/domain.DefaultRoute (P1,
// default_route của ngữ cảnh phiên) — cả hai kiểm với ca vàng web kn-menu.json.
var defaultRoutes = catalogValue[map[string]string]("defaultRoutes")

var profiles = registeredProfiles()

// MenuProfiles là mọi bộ menu có trong mã (thứ tự cố định — OpenAPI enum menu_profile).
var MenuProfiles = catalogValue[[]string]("MenuProfiles")

// KnownMenuProfile cho biết p là bộ menu có trong mã.
func KnownMenuProfile(p string) bool { _, ok := profiles[p]; return ok }

// KnownAudience cho biết a là đối tượng menu.
func KnownAudience(a string) bool { return slices.Contains(port.Audiences, a) }

// DepartmentMenu trả nhóm menu của đối tượng (kn_tw/kn_tinh/kn_xa) cho phòng ban dùng bộ profile
// ("{department}" nếu có thì thay bằng mã), kết thúc bằng nhóm "Dùng chung" (kn.menu/v3). Bản sao sâu —
// người gọi được lọc/sửa.
func DepartmentMenu(profile, audience, department string) []MenuGroup {
	eff := EffectiveMenuProfile(profile, department)
	groups := profiles[eff][audience]
	out := make([]MenuGroup, 0, len(groups)+1)
	for _, g := range groups {
		items := g.Items
		if g.Key == GroupBaoCao && audience == port.AudienceTinh && khtcReportProfiles[eff] {
			items = append(slices.Clone(items), KHTCReportItem) // W11.11: "Báo cáo cho KHTC" cuối nhóm "Báo cáo"
		}
		out = append(out, MenuGroup{Key: g.Key, Label: g.Label, Items: bind(items, department)})
	}
	shared := SharedMenuGroup()
	if extra := sharedByAudience[audience]; len(extra) > 0 {
		shared.Items = append(shared.Items, bind(extra, department)...)
	}
	return append(out, shared)
}

// SharedMenuGroup trả bản sao nhóm "Dùng chung" (cuối menu mọi đối tượng, kể cả Admin).
func SharedMenuGroup() MenuGroup {
	return MenuGroup{Key: SharedGroup.Key, Label: SharedGroup.Label, Items: bind(SharedGroup.Items, "")}
}

// FilterNeeds bỏ mục có Needs mà traits không đáp ứng (W11.07); mục cha không còn mục con và nhóm rỗng vì
// lọc cũng bỏ. Bản sao — không sửa đầu vào.
func FilterNeeds(groups []MenuGroup, t MenuTraits) []MenuGroup {
	var filter func(items []MenuItem) []MenuItem
	filter = func(items []MenuItem) []MenuItem {
		out := make([]MenuItem, 0, len(items))
		for _, it := range items {
			if !t.Has(it.Needs) {
				continue
			}
			if len(it.Children) > 0 {
				it.Children = filter(it.Children)
				if len(it.Children) == 0 {
					continue
				}
			}
			out = append(out, it)
		}
		return out
	}
	out := make([]MenuGroup, 0, len(groups))
	for _, g := range groups {
		n := len(g.Items)
		g.Items = filter(g.Items)
		if n > 0 && len(g.Items) == 0 {
			continue
		}
		out = append(out, g)
	}
	return out
}

// AdminDepartmentItems trả mục con của một phòng ban trong nhóm "Phòng ban" của Admin: Trồng trọt có bộ
// riêng; phòng ban khác dùng bộ chung + mục "Sắp có" của khuôn riêng (kn.menu/v3).
func AdminDepartmentItems(profile, department string) []MenuItem {
	profile = EffectiveMenuProfile(profile, department)
	items, ok := adminDeptItems[profile]
	switch {
	case !ok:
		items = slices.Clone(adminDeptItems[port.MenuProfileGeneric])
		if khtcReportProfiles[profile] {
			items = append(items, KHTCReportAdminItem) // W11.11: trước mục nghiệp vụ riêng của phòng
		}
		items = append(items, adminDeptBusiness[profile]...)
	case khtcReportProfiles[profile]:
		items = append(slices.Clone(items), KHTCReportAdminItem)
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

// registeredProfiles composes every registered profile without a second manual index.
func registeredProfiles() map[string]map[string][]MenuGroup {
	out := make(map[string]map[string][]MenuGroup, len(MenuProfiles))
	for _, profile := range MenuProfiles {
		switch profile {
		case port.MenuProfileTrongTrot:
			out[profile] = trongTrotMenus
		case port.MenuProfileGeneric:
			out[profile] = genericMenus
		default:
			out[profile] = withBusiness(profile)
		}
	}
	return out
}
