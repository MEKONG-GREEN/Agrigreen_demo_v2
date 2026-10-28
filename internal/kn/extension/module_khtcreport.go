package extension

import (
	"time"

	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	reporthandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/handler"
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
		periods.CancelGuard = m.khtcReports
		m.khtcSourceSvc().Finals = m.khtcReports
	}
	return m.khtcReports
}

// UseKHTCCancelLease đặt hạn ý định "Hủy kỳ" KHTC (kiểm thử hạn ngắn; vận hành dùng mặc định 2 phút — P0-POOL-SHARED-2).
func (m *Module) UseKHTCCancelLease(d time.Duration) { m.khtcReportSvc().CancelLease = d }

func (m *Module) registerKHTCReports(rg *gin.RouterGroup) {
	reporthandler.Handler{Svc: m.khtcReportSvc()}.RegisterApp(rg)
}
