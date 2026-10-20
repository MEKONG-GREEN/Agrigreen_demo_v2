package assembly

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// ---------------------------------------------------------------------------
// cap/forms (W6.2): biểu thu thập theo kỳ dùng chung. Cấu hình theo bên dùng — tương đương mục
// `cap/forms: {hierarchy: [TW, TINH, XA], publish_by: KN_TINH, close_by: KN_TINH}` của app/KN/app.yaml trong kiến
// trúc v4.1 §7.2 (app.yaml của repo chưa mang cấu hình module — W1.3). Pool đơn vị (app_platform → r_forms, W2.6) cho
// /v1/forms/*; ghi song song của KN chạy trong giao dịch của pf/extension.
// ---------------------------------------------------------------------------

// Compatibility names for composition/tests during the two-step registry migration.
// Configuration and session policy are owned by the application module.
var (
	KNProfiles    = extension.FormProfiles()
	KNProfile     = KNProfiles[0]
	KNCNTSProfile = KNProfiles[1]
	KNKHTCProfile = KNProfiles[2]
	// W11.27: kế hoạch nhân lực năm (HCTH FN-11) — profile "hrm_plan" do pf/hrm sở hữu, đơn vị lá = đơn vị nội bộ.
	KNHRMProfiles = hrm.FormProfiles(KNHRMAccess)
)

// wireForms dựng pf/forms (rà soát bốn mắt qua cap/workflow) và nối ghi song song vào vòng đời báo cáo KN. W11.42: danh
// mục cơ sở giết mổ (đơn vị lá BM-01), quy tắc chặn biểu kỳ CNTS, cổng phát hành kỳ native cho pf/extension.
func wireForms(p *pgxpool.Pool, clk clock.Clock, wf *workflow.Module, kn *extension.Module, facilities formsport.FacilityDirectory) (*forms.Module, error) {
	fm, err := forms.New(p, clk, forms.Deps{Approvals: wf.Approvals(), Profiles: append(append([]formsport.Profile{}, KNProfiles...), KNHRMProfiles...),
		Facilities: facilities, Validators: map[string]formsport.Validator{KNCNTSProfile.Name: kn.CNTSValidator(),
			KNKHTCProfile.Name: kn.KHTCFormValidator(), hrmport.PlanProfile: hrm.PlanValidator()}})
	if err != nil {
		return nil, err
	}
	kn.UseFormsFreezer(fm.Freezer())
	kn.UseFormsPublisher(fm.Publisher())
	kn.UseFormsFigures(fm.Figures()) // W11.44: tổng hợp CNTS BM-02, BM-18 đọc số liệu kỳ native theo lô
	// W11.62: Kỳ KHTC — kỳ native kn_khtc (một phiếu mỗi phòng), tối đa KNKHTCProfile.MaxSubmissions lần gửi (tham số
	// extension.khtc_max_submissions ghi đè). Kế hoạch năm (W11.61) nối trong pf/extension; bộ quy đổi kỳ nguồn (PFPUB) nối
	// ngay dưới. W11.63: phiếu Báo cáo cho KHTC — nhập/rà soát qua cổng Entry; bài nộp kn_khtc phải mang bảng số liệu đã
	// niêm phong (KHTCFormValidator ở trên).
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: fm.Publisher(), Control: fm.Control(), Entry: fm.Entry(),
		MaxSubmissions: KNKHTCProfile.MaxSubmissions})
	// PFPUB (ADR-0053 §7): kỳ KHTC mở kèm kỳ nguồn quy đổi theo lịch D8; "Đủ dữ liệu" theo cờ chốt của nguồn.
	kn.UseKHTCSourceCrosswalk(kn.KHTCSourceCrosswalk())
	return fm, nil
}
