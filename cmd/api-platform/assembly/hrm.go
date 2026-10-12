package assembly

import (
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// KNHRMAccess là cấu hình quyền dom/hrm (W11.21, ADR-0050) của App Khuyến nông — BRD HCTH v3.0 §VI.4: danh mục nhân sự
// (FN-19) Admin CRUD, Cấp Tỉnh – HCTH xem; chủ dữ liệu là Phòng Hành chính – Tổng hợp.
var KNHRMAccess = hrmport.Access{
	Department: partyport.DeptHanhChinhTongHop,
	ManageBy:   []string{authz.KNAdmin},
	ReadBy:     []string{authz.KNTinh},
}
