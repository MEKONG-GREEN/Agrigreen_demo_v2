package assembly

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// ---------------------------------------------------------------------------
// cap/forms (W6.2): biểu thu thập theo kỳ dùng chung. Cấu hình theo bên dùng — tương đương mục
// `cap/forms: {hierarchy: [TW, TINH, XA], publish_by: KN_TINH, close_by: KN_TINH}` của app/KN/app.yaml trong kiến
// trúc v4.1 §7.2 (app.yaml của repo chưa mang cấu hình module — W1.3). Pool đơn vị (app_platform → r_forms, W2.6) cho
// /v1/forms/*; ghi song song của KN chạy trong giao dịch của pf/extension.
// ---------------------------------------------------------------------------

// KNProfile là cấu hình cap/forms của App Khuyến nông: TW → Tỉnh → Xã, Tỉnh phát hành, rà soát, chốt/mở lại; Xã nộp.
// Vai là vai ngữ cảnh phiên KN (authz.KN*); Admin KN và reports_admin định nghĩa biểu. Không cho biểu mẫu thử:
// 14 biểu thật (Q-01) phải được định nghĩa trước khi Tỉnh phát hành kỳ trên /v1/forms (bật production chờ Q-01).
var KNProfile = formsport.Profile{
	Name:      "kn",
	Hierarchy: []string{"TW", "TINH", "XA"},
	Roles:     map[string]string{authz.KNTW: "TW", authz.KNTinh: "TINH", authz.KNXa: "XA"},
	PublishBy: "TINH",
	CloseBy:   "TINH",
	ReviewBy:  "TINH",
	DefineBy:  []string{authz.KNAdmin, "reports_admin"},
}

// wireForms dựng pf/forms (rà soát bốn mắt qua cap/workflow) và nối ghi song song vào vòng đời báo cáo KN.
func wireForms(p *pgxpool.Pool, clk clock.Clock, wf *workflow.Module, kn *extension.Module) (*forms.Module, error) {
	fm, err := forms.New(p, clk, forms.Deps{Approvals: wf.Approvals(), Profiles: []formsport.Profile{KNProfile}})
	if err != nil {
		return nil, err
	}
	kn.UseFormsFreezer(fm.Freezer())
	return fm, nil
}
