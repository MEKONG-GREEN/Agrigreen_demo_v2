package extension

import (
	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcreport/app"
	reporthandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcreport/handler"
	"github.com/gin-gonic/gin"
)

type khtcReportService = reportapp.Service

func (m *Module) khtcReportSvc() *reportapp.Service {
	if m.khtcReports == nil {
		m.khtcReports = &reportapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Plans: m.KHTCPlans(), Forms: m.khtcFormSvc().Entry, Sources: m.khtcSourceSvc(), Egress: m.svc.Egress, EgressMode: m.svc.EgressMode}
		periods := m.khtcSvc()
		if periods.Reports == nil {
			periods.Reports = m.khtcReports
		}
		periods.GuardCancel = m.khtcReports.GuardCancel
		m.khtcSourceSvc().Finals = m.khtcReports
	}
	return m.khtcReports
}
func (m *Module) registerKHTCReports(rg *gin.RouterGroup) {
	reporthandler.Handler{Svc: m.khtcReportSvc()}.RegisterApp(rg)
}
