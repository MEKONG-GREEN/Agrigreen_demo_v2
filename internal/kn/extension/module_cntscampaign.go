package extension

// Nối Chiến dịch điều tra mở rộng CNTS (W11.50 — D4 CN10, extension/cntscampaign): đợt = kỳ native cap/forms profile
// kn_cnts loại kỳ `adhoc` + sổ riêng của đợt (pf 20261024091058). Route /v1/extension/cnts/campaigns* luôn gắn; thiếu
// cap/forms → 503 extension.dependency_unavailable. Gốc lắp ráp gọi UseCNTSCampaigns sau khi dựng pf/forms.

import (
	"github.com/gin-gonic/gin"

	campapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/app"
	camphandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/handler"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// CNTSCampaignDeps là port của module khác cho chiến dịch điều tra. Forms, Control bắt buộc; Figures cho bảng kết quả.
// Control cài thêm cntscampaign/app.Reopener (pf/forms app.Control) thì "Mở lại đợt" dùng được.
type CNTSCampaignDeps struct {
	Forms   formsport.Publisher
	Control formsport.PeriodControl
	Figures formsport.FiguresReader
}

// campaignService là use case chiến dịch điều tra (trường Module.campaigns).
type campaignService = campapp.Service

// campaignSvc dựng use case chiến dịch điều tra (khu vực, tỉnh, tên xã, tên người dùng chung với vòng đời báo cáo).
func (m *Module) campaignSvc() *campapp.Service {
	if m.campaigns == nil {
		m.campaigns = &campapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Orgs: m.rc.Orgs, Units: m.rc.Units,
			Regions: func() partyport.Regions { return m.rc.Regions },
			Names:   func() partyport.ActorNames { return m.rc.Names }}
	}
	return m.campaigns
}

// registerCNTSCampaigns gắn route /v1/extension/cnts/campaigns*.
func (m *Module) registerCNTSCampaigns(rg *gin.RouterGroup) {
	camphandler.Handler{Svc: m.campaignSvc()}.RegisterApp(rg)
}

// UseCNTSCampaigns nối cap/forms cho chiến dịch điều tra.
func (m *Module) UseCNTSCampaigns(d CNTSCampaignDeps) {
	s := m.campaignSvc()
	s.Forms, s.Control = d.Forms, d.Control
	// BF-CN-PUBLISH (#108): cùng cổng tiến độ kỳ native cho nháp của biểu kỳ CNTS và nút Nhắc của màn theo dõi (gốc lắp
	// ráp chỉ gọi UseCNTSCampaigns — không thêm lời gọi nối mới ngoài làn KN).
	m.rc.FormsControl = d.Control
	m.alert.Control = d.Control
	if d.Figures != nil {
		s.Figures = d.Figures
	}
}
