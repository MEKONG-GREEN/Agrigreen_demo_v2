package extension

// Nối Kỳ KHTC (W11.62 — extension/khtcperiod, BRD KHTC v2.2 FN-TI-02.03): kỳ KHTC = kỳ native cap/forms profile kn_khtc
// (một phiếu mỗi phòng) + sổ riêng của KHTC (pf 20261013120542). Gốc lắp ráp gọi UseKHTCPeriods sau khi dựng pf/forms; route
// /v1/extension/khtc/* luôn gắn (thiếu cap/forms → 503 extension.dependency_unavailable).

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	khtcfapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/app"
	khtcfhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/handler"
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
	Entry       formsport.Entry // W11.63: nhập, rà soát phiếu (thiếu → route phiếu trả 503)
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

// khtcFormService là use case phiếu Báo cáo cho KHTC (trường Module.khtcForms — W11.63).
type khtcFormService = khtcfapp.Service

// registerKHTCPeriods gắn route /v1/extension/khtc/* (kỳ — W11.62, phiếu — W11.63).
func (m *Module) registerKHTCPeriods(rg *gin.RouterGroup) {
	khtcphandler.Handler{Svc: m.khtcSvc()}.RegisterApp(rg)
	khtcfhandler.Handler{Svc: m.khtcFormSvc()}.RegisterApp(rg)
}

// khtcFormSvc dựng use case phiếu KHTC (W11.63) và nối móc: Kế hoạch năm chỉ cho ngừng (không bỏ) hạng mục đã có số liệu ở
// phiếu (W11.61 EntryUsage); bảng tiến độ kỳ hiện "Lưu nháp" (W11.62 Drafts).
func (m *Module) khtcFormSvc() *khtcfapp.Service {
	if m.khtcForms == nil {
		m.khtcForms = &khtcfapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Plans: m.KHTCPlans(), Programs: m.KHTCPrograms()}
		if m.khtc != nil {
			m.khtc.EntryUsage = m.khtcForms.EntryUsage
		}
		m.khtcSvc().Drafts = m.khtcForms.Drafts
	}
	return m.khtcForms
}

// KHTCFormValidator là quy tắc cap/forms của profile kn_khtc (W11.63): bài nộp biểu KHTC-PHIEU phải mang bảng số liệu đã
// niêm phong qua màn phiếu KHTC (gốc lắp ráp nối vào forms.Deps.Validators).
func (m *Module) KHTCFormValidator() formsport.Validator { return khtcfapp.Validator{Svc: m.khtcFormSvc()} }

func (m *Module) khtcSvc() *khtcpapp.Service {
	if m.khtcPeriods == nil {
		// W11.61 ↔ W11.62: phòng phụ trách theo Kế hoạch năm hiệu lực (KHTCPlans.PlanAt); Kế hoạch năm chặn ngày hiệu lực
		// trước ngày chốt của kỳ KHTC đã đóng gần nhất (BR-DL-13).
		m.khtcPeriods = &khtcpapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Params: m.svc.Params, Plan: planDepartments{plans: m.KHTCPlans()}}
		if m.khtc != nil {
			m.khtc.ClosedCutoff = m.khtcPeriods.ClosedCutoff
		}
	}
	return m.khtcPeriods
}

// UseKHTCPeriods nối cap/forms và các cổng tùy chọn cho Kỳ KHTC.
func (m *Module) UseKHTCPeriods(d KHTCDeps) {
	s := m.khtcSvc()
	s.Forms, s.Control, s.MaxSubmissions = d.Forms, d.Control, d.MaxSubmissions
	if d.Entry != nil {
		m.khtcFormSvc().Entry = d.Entry
	}
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

// planDepartments cài port.KHTCPlanDepartments trên Kế hoạch năm (W11.61): phòng phụ trách các hạng mục đang theo dõi
// của phiên bản hiệu lực tại ngày on (BR-DL-13). Chưa có kế hoạch → rỗng (Kỳ KHTC lấy mọi phòng đang áp dụng trừ KHTC).
type planDepartments struct{ plans port.KHTCPlans }

func (p planDepartments) PlanDepartments(ctx context.Context, year int, on time.Time) ([]string, error) {
	plan, err := p.plans.PlanAt(ctx, year, on)
	if errors.Is(err, port.ErrKHTCPlanNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range plan.Entries {
		if e.Department == "" || e.Tracking == "stopped" || seen[e.Department] {
			continue
		}
		seen[e.Department] = true
		out = append(out, e.Department)
	}
	return out, nil
}
