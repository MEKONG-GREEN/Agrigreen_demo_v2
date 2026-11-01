package hrm

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
)

func ExportTemplates() exportport.TemplateRegistration {
	return exportport.TemplateRegistration{Owner: app.AppraisalWordOwner, Templates: app.AppraisalWordTemplates()}
}

func (m *Module) UseExporter(e exportport.Exporter) { m.svc.AppraisalExporter = e }
