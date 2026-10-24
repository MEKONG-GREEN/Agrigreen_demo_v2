package port

import (
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// --- W11.27 (pf 20261024064206): kế hoạch nhân lực năm (FN-11) trên cap/forms. ---

// PlanProfile là tên profile cap/forms của kế hoạch nhân lực năm (D3 H7): TT (Trung tâm — Phòng HCTH, Admin) mở kỳ, rà
// soát, chốt; DV (đơn vị nội bộ — lãnh đạo đơn vị) lập và gửi.
const PlanProfile = "hrm_plan"

// Cấp của profile hrm_plan.
const (
	PlanLevelCenter = "TT"
	PlanLevelUnit   = "DV"
)

// Vai của profile hrm_plan — do resolver của pf/hrm suy từ ngữ cảnh phiên KN (không phải vai realm).
const (
	PlanRoleAdmin   = "hrm_plan_admin"   // Admin KN
	PlanRoleHCTH    = "hrm_plan_hcth"    // Cấp Tỉnh – Phòng HCTH
	PlanRoleManager = "hrm_plan_manager" // lãnh đạo đơn vị (KNContext.Manager) — đơn vị = KNContext.InternalUnit
)

// Lỗi kế hoạch nhân lực (docs/errors/pf-hrm-plan.md).
var (
	ErrPlanNotFound   = problem.New(http.StatusNotFound, "hrm.plan_not_found", "Chưa mở kỳ kế hoạch nhân lực của năm này")
	ErrPlanExists     = problem.New(http.StatusConflict, "hrm.plan_exists", "Kỳ kế hoạch nhân lực của năm này đã mở")
	ErrPlanUnit       = problem.New(http.StatusNotFound, "hrm.plan_unit", "Đơn vị không thuộc kỳ kế hoạch hoặc ngoài phạm vi")
	ErrPlanClosed     = problem.New(http.StatusConflict, "hrm.plan_closed", "Kế hoạch đã chốt — chỉ điều chỉnh có lý do")
	ErrPlanNotClosed  = problem.New(http.StatusConflict, "hrm.plan_not_closed", "Kế hoạch chưa chốt — sửa bằng cách gửi lại, không điều chỉnh")
	ErrPlanDisabled   = problem.New(http.StatusServiceUnavailable, "hrm.plan_disabled", "Kế hoạch nhân lực chưa nối cap/forms")
	ErrPlanNoPrevious = problem.New(http.StatusNotFound, "hrm.plan_no_previous", "Năm trước chưa có kế hoạch của đơn vị để sao chép")
)
