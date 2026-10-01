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

// ReadOnly: Cấp Quốc gia chỉ xem (BRD §5.1).
func (k KNContext) ReadOnly() bool { return k.Role == KNTW }

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
