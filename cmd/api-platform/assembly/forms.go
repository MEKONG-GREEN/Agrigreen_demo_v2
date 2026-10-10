package assembly

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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

// KNCNTSProfile là cấu hình cap/forms của Phòng Chăn nuôi – Thủy sản (W11.05, BRD CNTS v3.7): Tỉnh phát hành, rà soát,
// chốt/mở lại; Xã nhập cho đơn vị lá là xã, cơ sở giết mổ (BM-01) hoặc hộ/điểm (nhật ký) — cơ sở/hộ lọc phạm vi theo
// mã xã cha. Chỉ phiên của phòng CNTS (Admin định nghĩa biểu — Q-056 đề xuất "theo Profile từng phòng"). Không giới
// hạn số lần gửi (CNTS BR-02 "được sửa và gửi lại đến khi chốt kỳ").
var KNCNTSProfile = formsport.Profile{
	Name:        "kn_cnts",
	Hierarchy:   []string{"TW", "TINH", "XA"},
	Roles:       map[string]string{authz.KNTW: "TW", authz.KNTinh: "TINH", authz.KNXa: "XA"},
	PublishBy:   "TINH",
	CloseBy:     "TINH",
	ReviewBy:    "TINH",
	DefineBy:    []string{authz.KNAdmin, "reports_admin"},
	UnitKinds:   []string{formsport.UnitCommune, formsport.UnitFacility, formsport.UnitHousehold},
	Departments: []string{partyport.DeptChanNuoiThuySan},
}

// KNKHTCProfile là cấu hình cap/forms của phiếu "Báo cáo cho KHTC" (W11.05, BRD KHTC v2.2): đơn vị lá là PHÒNG BAN.
// Cấp Tỉnh của Phòng KHTC (khóa vai "kn_tinh@ke_hoach_tai_chinh") mở kỳ, rà soát (yêu cầu bổ sung/xác nhận), đóng và
// hủy kỳ; Cấp Tỉnh các phòng khác nộp phiếu của phòng mình. Tối đa 3 lần gửi, lần 3 tự khóa (FN-TI-02.03 — Q-081 đề
// xuất "tối đa 3, tham số"). KHTC không mở lại kỳ đã đóng: route mở lại vẫn có nhưng màn KHTC (W11.62) không dùng.
var KNKHTCProfile = formsport.Profile{
	Name:      "kn_khtc",
	Hierarchy: []string{"TINH", "PHONG"},
	Roles: map[string]string{
		authz.KNTinh + "@" + partyport.DeptKeHoachTaiChinh: "TINH",
		authz.KNTinh: "PHONG",
	},
	PublishBy:      "TINH",
	CloseBy:        "TINH",
	ReviewBy:       "TINH",
	DefineBy:       []string{authz.KNAdmin, "reports_admin"},
	UnitKinds:      []string{formsport.UnitDepartment},
	MaxSubmissions: 3,
}

// KNProfiles là mọi cấu hình cap/forms của App Khuyến nông.
var KNProfiles = []formsport.Profile{KNProfile, KNCNTSProfile, KNKHTCProfile}

// wireForms dựng pf/forms (rà soát bốn mắt qua cap/workflow) và nối ghi song song vào vòng đời báo cáo KN.
func wireForms(p *pgxpool.Pool, clk clock.Clock, wf *workflow.Module, kn *extension.Module) (*forms.Module, error) {
	fm, err := forms.New(p, clk, forms.Deps{Approvals: wf.Approvals(), Profiles: KNProfiles})
	if err != nil {
		return nil, err
	}
	kn.UseFormsFreezer(fm.Freezer())
	return fm, nil
}
