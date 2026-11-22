package extension

import (
	"fmt"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Danh mục và cờ quyền thuộc tính lần gán do KN sở hữu (KNSPLIT-2-ATTRIBUTE-CATALOG-KN #79) cho
// accounts/port.AttributeCatalog. Cùng mã/nhãn cấp quản lý với luật lần gán #72 (mgmtLevels) và vai #95.

// AttributeCatalogVersion là phiên bản danh mục (giữ nguyên phiên bản đang ghi trong sổ/HTTP; đổi khi khách trả lời Q-059).
const AttributeCatalogVersion = "accounts.kn-roles/2"

// attributeGroups: 5 nhóm Cấp Tỉnh (D4 X3; BRD CNTS v3.7 §1.1). Cờ quyền là dữ liệu chủ đã chốt, không suy từ nhãn:
// Lãnh đạo phòng có quyền lãnh đạo; Ban Giám đốc xem mọi phòng ban, chỉ đọc kể cả phòng mình.
func attributeGroups() []accport.AttributeGroup {
	return []accport.AttributeGroup{
		{Code: "vien_chuc", Label: "Viên chức"},
		{Code: "tong_hop", Label: "Cán bộ tổng hợp"},
		{Code: "chu_nhiem", Label: "Chủ nhiệm chương trình/dự án"},
		{Code: "lanh_dao_phong", Label: "Lãnh đạo phòng", Manager: true},
		{Code: "ban_giam_doc", Label: "Ban Giám đốc (chỉ xem)", ReadsAllDepartments: true, ReadOnly: true},
	}
}

// attributeDuties: cờ nhiệm vụ trong vai Cấp Tỉnh.
func attributeDuties() []accport.CatalogItem {
	return []accport.CatalogItem{
		{Code: authz.DutyPriceAdmin, Label: "Quản trị nghiệp vụ báo giá (QTNV)"},
		{Code: authz.DutyFormOwner, Label: "Người phụ trách biểu"},
		{Code: authz.DutyScheduleFollower, Label: "Người theo dõi lịch công tác cán bộ xã"},
	}
}

// attributeRoles: chỉ Cấp Tỉnh có nhóm/nhiệm vụ và quyền quản lý; Cấp Xã có giới hạn cơ sở/hộ.
func attributeRoles() []accport.AttributeRole {
	return []accport.AttributeRole{
		{Role: authz.KNTW},
		{Role: authz.KNTinh, GroupAndDuties: true, ManagementPermissions: true},
		{Role: authz.KNXa, ScopedTargets: true},
		{Role: authz.KNAdmin},
	}
}

// AttributeCatalog dựng snapshot bất biến mới cho accounts.Deps.Attributes. Không nhận actor/tenant, không trạng thái
// toàn cục; mỗi lần gọi một instance riêng. Lỗi (dữ liệu chủ không hợp lệ) → gốc lắp ráp dừng khởi động, không
// rơi về bridge.
func AttributeCatalog() (*accport.AttributeCatalog, error) {
	c, err := accport.NewAttributeCatalog(accport.AttributeCatalogSpec{Version: AttributeCatalogVersion, Provisional: true,
		Levels: mgmtLevels(), Groups: attributeGroups(), Duties: attributeDuties(), Roles: attributeRoles()})
	if err != nil {
		return nil, fmt.Errorf("kn: attribute catalog: %w", err)
	}
	return c, nil
}
