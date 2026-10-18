package authz

// Ngữ cảnh phiên KN v7.7 (docs/brd/kn-v7.7-gap.md §4.5 phương án A, plan §3 P1).
//
// Keycloak chỉ xác thực. Vai nghiệp vụ KN (Cấp Quốc gia/Tỉnh/Xã/Admin), Nút, phòng ban và xã phụ trách
// nằm ở sổ gán vai trò của pf/accounts. Client chọn một cặp vai trò × phòng ban (POST /v1/me/context) rồi
// gửi context_id qua header X-AG-Context ở MỌI request; middleware của pf/accounts tra context ở server
// (thuộc đúng người trong token, còn hạn, lần gán còn hiệu lực, phòng ban Đang áp dụng) và gắn KNContext
// vào Principal. Không có trường nào của KNContext lấy từ client.
//
// Bổ sung tương thích ngược: Principal.KN = nil khi request không có header — mọi module cũ giữ nguyên
// đường org_ref + realm roles (ScopeResolver, ADR-0009).

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Header của ngữ cảnh phiên.
const (
	// ContextHeader mang context_id do POST /v1/me/context trả (UUID).
	ContextHeader = "X-AG-Context"
	// ReasonHeader mang lý do (UTF-8, mã hóa phần trăm RFC 3986) của thao tác ghi trong chế độ hỗ trợ
	// (FN-AD-04.07: "khi lưu, nhập lý do").
	ReasonHeader = "X-AG-Reason"
)

// Vai nghiệp vụ KN v7.7 trong ngữ cảnh phiên — KHÔNG phải vai realm (không mang tiền tố Role để
// realm_test không đòi khai trong Keycloak).
const (
	KNTW    = "kn_tw"   // Cấp Quốc gia (chỉ xem)
	KNTinh  = "kn_tinh" // Cấp Tỉnh
	KNXa    = "kn_xa"   // Cấp Xã
	KNAdmin = "admin"   // Admin — không phòng ban, toàn hệ thống
)

// KNRoles là mọi vai KN hợp lệ.
var KNRoles = []string{KNTW, KNTinh, KNXa, KNAdmin}

// KNSupport mô tả chế độ hỗ trợ (FN-AD-04.07): Admin thao tác trong menu của cấp khác.
type KNSupport struct {
	SessionID  uuid.UUID
	OnBehalfOf uuid.UUID // tài khoản được hỗ trợ; Nil khi Admin chỉ chọn vai + xã
	Reason     string    // lý do của request hiện tại (header X-AG-Reason), rỗng với request đọc
}

// KNContext là ngữ cảnh phiên đã được server giải.
type KNContext struct {
	ID           uuid.UUID
	AssignmentID uuid.UUID // Nil với ngữ cảnh hỗ trợ không gắn tài khoản
	Role         string    // KNTW | KNTinh | KNXa | KNAdmin
	Department   string    // mã phòng ban; rỗng với Admin
	OrgRef       uuid.UUID // Nút của lần gán; Nil với Admin không gắn Nút
	UnitRef      uuid.UUID // đơn vị sở hữu phòng ban (Nút cấp 1/2)
	Communes     []string  // xã phụ trách (Cấp Xã)
	// Scope = Nút xuống dưới (Cấp Xã: chỉ các xã phụ trách). Phòng ban lọc riêng bằng AllowsDepartment.
	Scope        Scope
	DefaultRoute string
	ExpiresAt    time.Time
	Support      *KNSupport // khác nil: Admin đang hỗ trợ (thao tác ghi ghi nhật ký hỗ trợ)

	// Phân quyền trong vai (W11.07, D4 X3, D3 H0/C3–C5). Giải ở server từ sổ phụ thuộc tính vai
	// (pf_accounts.assignment_attr_current) và sổ ủy quyền — không lấy từ client.

	// MgmtLevel là mã cấp quản lý của lần gán (accounts/port.MgmtLevels, Q-059).
	MgmtLevel string
	// Manager: cấp quản lý ≥ phó trưởng phòng (kể cả phụ trách, lãnh đạo đơn vị) hoặc nhóm Cấp Tỉnh "Lãnh đạo
	// phòng" — quyền của lãnh đạo phòng, menu "Nhân sự của phòng" (Q-067).
	Manager bool
	// ProvinceGroup là nhóm trong Cấp Tỉnh (vien_chuc, tong_hop, chu_nhiem, lanh_dao_phong, ban_giam_doc).
	ProvinceGroup string
	// Duties là cờ nhiệm vụ của lần gán (DutyPriceAdmin, DutyFormOwner).
	Duties []string
	// InternalUnit là mã đơn vị nội bộ (phòng/trạm/trại/BGĐ — pf/party internal_unit) của người gọi: "UnitRef
	// nội bộ" của D3 H0. Cấp Xã: đơn vị quản lý (HS-37). KHÁC UnitRef ở trên (Nút sở hữu phòng ban).
	InternalUnit string
	// Facilities, Households: Cấp Xã giới hạn trong cơ sở (vd cơ sở giết mổ) / hộ (CNTS: phạm vi = xã HOẶC cơ
	// sở HOẶC hộ). Cả hai rỗng = cả các xã phụ trách. Chỉ thu hẹp, không bao giờ mở rộng Scope.
	Facilities []uuid.UUID
	Households []uuid.UUID
	// Powers là quyền nhận qua ủy quyền có hạn, còn hiệu lực, trong phòng ban của phiên.
	Powers []string
	// ViewOnly: ngữ cảnh chỉ xem — Lãnh đạo đơn vị xem phòng ban ngoài lần gán (BR-04 HCTH, Q-059), nhóm
	// "Ban Giám đốc". Middleware pf/accounts chặn mọi thao tác ghi; module dùng ReadOnly().
	ViewOnly bool
}

// AllowsDepartment cho biết dữ liệu của phòng ban code thuộc ngữ cảnh: Admin (không ở chế độ hỗ trợ)
// thấy mọi phòng ban; vai khác chỉ phòng ban của phiên.
func (k KNContext) AllowsDepartment(code string) bool {
	if k.Role == KNAdmin {
		return true
	}
	return k.Department != "" && k.Department == code
}

// Allows cho biết ngữ cảnh được thao tác dữ liệu của đơn vị hành chính unit thuộc phòng ban dept.
func (k KNContext) Allows(dept, unit string) bool {
	return k.AllowsDepartment(dept) && k.Scope.Allows(unit)
}

// ReadOnly: Cấp Quốc gia chỉ xem (BRD §5.1); ngữ cảnh chỉ xem của W11.07 (ViewOnly).
func (k KNContext) ReadOnly() bool { return k.Role == KNTW || k.ViewOnly }

// Quyền nghiệp vụ ủy quyền được (W11.07, ủy quyền có hạn). Mặc định thuộc lãnh đạo phòng.
const (
	PowerReopenPeriod = "mo_lai_ky"     // mở lại kỳ đã chốt (CNTS III.3.0, AC-04)
	PowerApprove      = "duyet_bao_cao" // duyệt/xác thực báo cáo của phòng
)

// Cờ nhiệm vụ trong vai (W11.07).
const (
	DutyPriceAdmin = "qtnv_bao_gia"   // Quản trị nghiệp vụ báo giá (Báo giá v1.3 FN-AD-04.03…06)
	DutyFormOwner  = "phu_trach_bieu" // được chọn làm người phụ trách biểu (CNTS UR-TINH-08)
	// DutyScheduleFollower: người theo dõi được phân công — duyệt lịch công tác của cán bộ xã khi phòng ban dùng quy
	// tắc duyệt "follower" (CNTS FN-12 BR-04, Q-057; W11.46).
	DutyScheduleFollower = "theo_doi_lich"
)

// HasDuty cho biết lần gán có cờ nhiệm vụ d. Ngữ cảnh chỉ xem không mang nhiệm vụ.
func (k KNContext) HasDuty(d string) bool { return !k.ReadOnly() && slices.Contains(k.Duties, d) }

// Can cho biết ngữ cảnh có quyền nghiệp vụ power: Admin (ngoài chế độ hỗ trợ) và lãnh đạo phòng Cấp Tỉnh có
// sẵn; người khác chỉ khi được ủy quyền còn hạn trong phòng ban của phiên. Ngữ cảnh chỉ xem không có quyền nào.
func (k KNContext) Can(power string) bool {
	switch {
	case k.ReadOnly():
		return false
	case k.Role == KNAdmin && k.Support == nil:
		return true
	case k.Role == KNTinh && k.Manager:
		return true
	}
	return slices.Contains(k.Powers, power)
}

// limitedTargets: Cấp Xã có giới hạn cơ sở/hộ.
func (k KNContext) limitedTargets() bool {
	return k.Role == KNXa && (len(k.Facilities) > 0 || len(k.Households) > 0)
}

// AllowsFacility cho biết ngữ cảnh được thao tác cơ sở ref. Cấp Xã có giới hạn cơ sở/hộ → chỉ cơ sở trong
// danh sách; không giới hạn → mọi cơ sở (người gọi vẫn kiểm Scope theo xã của cơ sở).
func (k KNContext) AllowsFacility(ref uuid.UUID) bool {
	return !k.limitedTargets() || slices.Contains(k.Facilities, ref)
}

// AllowsHousehold như AllowsFacility cho hộ.
func (k KNContext) AllowsHousehold(ref uuid.UUID) bool {
	return !k.limitedTargets() || slices.Contains(k.Households, ref)
}

// AllowsCommune là tên theo kế hoạch P1 cho Allows trên mã xã.
func (s Scope) AllowsCommune(code string) bool { return s.Allows(code) }

// ErrNoKNContext: route KN v7.7 cần header X-AG-Context.
var ErrNoKNContext = problem.New(428, "accounts.context_required", "Cần chọn vai trò và phòng ban (header X-AG-Context)")

// RequireKN trả Principal và ngữ cảnh phiên nếu vai KN của ngữ cảnh là một trong roles (rỗng = mọi vai).
// Route KN v7.7 gọi hàm này thay cho Require: vai nằm trong ngữ cảnh, không trong token.
func RequireKN(ctx context.Context, roles ...string) (Principal, KNContext, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, KNContext{}, problem.Unauthenticated
	}
	if p.KN == nil {
		return p, KNContext{}, ErrNoKNContext
	}
	if len(roles) > 0 && !slices.Contains(roles, p.KN.Role) {
		return p, *p.KN, problem.Forbidden.WithDetail("vai trò của ngữ cảnh phiên không được thao tác này")
	}
	return p, *p.KN, nil
}
