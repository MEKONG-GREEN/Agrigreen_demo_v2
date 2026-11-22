package extension

import (
	"slices"
	"strings"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Luật lần gán vai trò KN (KNSPLIT-2-ASSIGNMENT-RULES-KN #72) cho accounts/port.AssignmentRules. accounts đã chuẩn
// hóa cú pháp/khử trùng/thời hạn và vẫn tự kiểm tenant, Nút đang hoạt động, phòng ban, xã thuộc Nút, phạm vi người
// gọi, Admin chỉ do platform_admin cấp, MFA. Adapter chỉ kiểm danh mục vai/cấp quản lý, tổ hợp thuộc tính và loại
// Nút — không trả giá trị thay thế, không cấp quyền.

// Thông điệp lỗi giữ nguyên câu chữ hiện hành (parity HTTP).
const (
	msgAssignRole        = "vai trò phải là kn_tw | kn_tinh | kn_xa | admin"
	msgAssignOrgRequired = "bắt buộc Nút tổ chức của vai trò"
	msgAssignAdminScope  = "Admin không chọn phòng ban, không gán xã"
	msgAssignDeptMissing = "bắt buộc ít nhất một phòng ban"
	msgAssignCommunes    = "chỉ Cấp Xã có xã phụ trách"
	msgAssignMgmtLevel   = "cấp quản lý không có trong danh mục (tạm, chờ khách Q-05)"
	msgAssignOrgKind     = "Nút không đúng cấp của vai trò"
)

// assignmentRoles: vai nghiệp vụ KN của sổ gán vai trò (mã ghi sổ, không đổi).
var assignmentRoles = []string{authz.KNTW, authz.KNTinh, authz.KNXa, authz.KNAdmin}

// assignmentOrgKinds: loại Nút hợp lệ của từng vai. Cấp Xã gắn Nút TỈNH + danh sách xã phụ trách; Admin không hạn.
var assignmentOrgKinds = map[string][]string{
	authz.KNTW:   {string(partyport.OrgTTKNNational)},
	authz.KNTinh: {string(partyport.OrgTTKNProvince)},
	authz.KNXa:   {string(partyport.OrgTTKNProvince)},
}

// mgmtLevels: danh mục cấp quản lý của KN — bản TẠM chờ khách (Q-05 → Q-059). Mã là định danh ghi sổ; thứ tự là thứ
// tự hiển thị. Manager = quyền lãnh đạo; ReadsAllDepartments = xem mọi phòng ban của đơn vị (chỉ xem).
func mgmtLevels() []accport.MgmtLevel {
	return []accport.MgmtLevel{
		{Code: "chuyen_vien", Label: "Chuyên viên - Cán bộ"},
		{Code: "phu_trach", Label: "Phụ trách", Manager: true},
		{Code: "pho_truong_phong", Label: "Phó trưởng phòng", Manager: true},
		{Code: "truong_phong", Label: "Trưởng phòng", Manager: true},
		{Code: "lanh_dao_don_vi", Label: "Lãnh đạo đơn vị", Manager: true, ReadsAllDepartments: true},
		{Code: "pho_giam_doc", Label: "Phó Giám đốc", Manager: true},
		{Code: "giam_doc", Label: "Giám đốc", Manager: true},
	}
}

type assignmentRules struct{}

// AssignmentRules trả adapter KN cho accounts.Deps.AssignmentRules. Không trạng thái, không nhận actor/tenant.
func AssignmentRules() accport.AssignmentRules { return assignmentRules{} }

// ValidateAssignment kiểm theo đúng thứ tự cũ: vai → cấp quản lý → tổ hợp Admin/Nút/phòng ban/xã.
func (assignmentRules) ValidateAssignment(v accport.AssignmentRuleValues) error {
	if !slices.Contains(assignmentRoles, v.Role) {
		return accport.AssignmentRuleError{Field: "role", Msg: msgAssignRole}
	}
	if v.MgmtLevel != "" && !slices.ContainsFunc(mgmtLevels(), func(l accport.MgmtLevel) bool { return l.Code == v.MgmtLevel }) {
		return accport.AssignmentRuleError{Field: "mgmt_level", Msg: msgAssignMgmtLevel}
	}
	switch {
	case v.Role == authz.KNAdmin && (len(v.Departments) > 0 || len(v.Communes) > 0):
		return accport.AssignmentRuleError{Field: "department_codes", Msg: msgAssignAdminScope}
	case v.Role != authz.KNAdmin && v.OrgRef == uuid.Nil:
		return accport.AssignmentRuleError{Field: "org_ref", Msg: msgAssignOrgRequired}
	case v.Role != authz.KNAdmin && len(v.Departments) == 0:
		return accport.AssignmentRuleError{Field: "department_codes", Msg: msgAssignDeptMissing}
	case v.Role != authz.KNXa && len(v.Communes) > 0:
		return accport.AssignmentRuleError{Field: "commune_codes", Msg: msgAssignCommunes}
	}
	return nil
}

// ValidateNodeKind kiểm loại Nút của vai; vai không có trong bảng (Admin) không hạn loại.
func (assignmentRules) ValidateNodeKind(role, kind string) error {
	kinds, ok := assignmentOrgKinds[role]
	if ok && !slices.Contains(kinds, kind) {
		return accport.AssignmentRuleError{Field: "org_ref", Msg: msgAssignOrgKind + " (" + strings.Join(kinds, ", ") + ")"}
	}
	return nil
}
