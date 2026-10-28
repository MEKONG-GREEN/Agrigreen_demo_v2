package extension

import (
	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	obapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/app"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
)

// ExportTemplates là mẫu Word của pf/extension đăng ký vào pf/export (`cap/export`, W10.17): KHTC Mẫu 1–5; Báo cáo
// tình hình dịch bệnh BM-10 (BF-CN-OUTBREAK, UP-08).
func ExportTemplates() exportport.TemplateRegistration {
	return exportport.TemplateRegistration{Owner: reportapp.WordOwner, Templates: append(reportapp.WordTemplates(), obapp.WordTemplates()...)}
}

// UseExporter nối năng lực xuất văn bản pf/export (Word KHTC Mẫu 1–5, Word BM-10).
func (m *Module) UseExporter(e exportport.Exporter) {
	m.khtcReportSvc().Exporter = e
	m.ob.Exporter = e
}
