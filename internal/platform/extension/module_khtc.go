package extension

// Nối Kỳ KHTC (W11.62 — extension/khtcperiod, BRD KHTC v2.2 FN-TI-02.03): kỳ KHTC = kỳ native cap/forms profile kn_khtc
// (một phiếu mỗi phòng) + sổ riêng của KHTC (pf 20261013070316). Gốc lắp ráp gọi UseKHTCPeriods sau khi dựng pf/forms; route
// /v1/extension/khtc/* luôn gắn (thiếu cap/forms → 503 extension.dependency_unavailable).

import (
	"github.com/gin-gonic/gin"

	khtcpapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/app"
	khtcphandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// KHTCDeps là port của module khác cho Kỳ KHTC. Forms, Control bắt buộc; còn lại tùy chọn (xem extension/port/khtc.go).
type KHTCDeps struct {
	Forms       formsport.Publisher
	Control     formsport.PeriodControl
	Departments partyport.Departments
	Crosswalk   port.KHTCSourceCrosswalk // gói PFPUB (bộ quy đổi kỳ nguồn)
	Plan        port.KHTCPlanDepartments // W11.61 (phòng phụ trách theo Kế hoạch năm)
	Reports     port.KHTCReportStates    // KH6 (Bản cuối chặn hủy kỳ)
	// MaxSubmissions: số lần gửi tối đa mặc định của phiếu (= KNKHTCProfile.MaxSubmissions; tham số
	// extension.khtc_max_submissions ghi đè theo tenant).
	MaxSubmissions int
}

// khtcPeriodService là use case Kỳ KHTC (trường Module.khtcPeriods).
type khtcPeriodService = khtcpapp.Service

// registerKHTCPeriods gắn route /v1/extension/khtc/*.
func (m *Module) registerKHTCPeriods(rg *gin.RouterGroup) {
	khtcphandler.Handler{Svc: m.khtcSvc()}.RegisterApp(rg)
}

func (m *Module) khtcSvc() *khtcpapp.Service {
	if m.khtcPeriods == nil {
		m.khtcPeriods = &khtcpapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Params: m.svc.Params}
	}
	return m.khtcPeriods
}

// UseKHTCPeriods nối cap/forms và các cổng tùy chọn cho Kỳ KHTC.
func (m *Module) UseKHTCPeriods(d KHTCDeps) {
	s := m.khtcSvc()
	s.Forms, s.Control, s.MaxSubmissions = d.Forms, d.Control, d.MaxSubmissions
	// Cổng tùy chọn: chỉ ghi khi có (nối riêng bằng Use* khác không bị xóa).
	if d.Departments != nil {
		s.Departments = d.Departments
	}
	if d.Crosswalk != nil {
		s.Crosswalk = d.Crosswalk
	}
	if d.Plan != nil {
		s.Plan = d.Plan
	}
	if d.Reports != nil {
		s.Reports = d.Reports
	}
}

// UseKHTCSourceCrosswalk nối bộ quy đổi kỳ nguồn (gói PFPUB) khi gói đó cài xong.
func (m *Module) UseKHTCSourceCrosswalk(c port.KHTCSourceCrosswalk) { m.khtcSvc().Crosswalk = c }

// UseKHTCPlanDepartments nối phòng phụ trách theo Kế hoạch năm (W11.61).
func (m *Module) UseKHTCPlanDepartments(p port.KHTCPlanDepartments) { m.khtcSvc().Plan = p }

// UseKHTCDepartments nối danh mục phòng ban (pf/party) — phòng tham gia mặc định và tên phòng.
func (m *Module) UseKHTCDepartments(d partyport.Departments) { m.khtcSvc().Departments = d }
