package extension

import (
	knqgapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/app"
	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	obapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/app"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
)

// ExportTemplates là mẫu Word của pf/extension đăng ký vào pf/export (`cap/export`, W10.17): KHTC Mẫu 1–5; Báo cáo
// tình hình dịch bệnh BM-10 (BF-CN-OUTBREAK, UP-08); mẫu biểu dự án KNQG Mẫu 01, 03, 04, 05, 07 (W11.53, FN-19 AC-01).
func ExportTemplates() exportport.TemplateRegistration {
	ts := append(reportapp.WordTemplates(), obapp.WordTemplates()...)
	return exportport.TemplateRegistration{Owner: reportapp.WordOwner, Templates: append(ts, knqgapp.WordTemplates()...)}
}

// UseExporter nối năng lực xuất văn bản pf/export (Word KHTC Mẫu 1–5, Word BM-10, Word mẫu KNQG).
func (m *Module) UseExporter(e exportport.Exporter) {
	m.khtcReportSvc().Exporter = e
	m.ob.Exporter = e
	m.knqgSvc().Exporter = e
}
