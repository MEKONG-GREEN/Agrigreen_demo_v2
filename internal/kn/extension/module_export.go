package extension

import (
	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	obapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/app"
	rcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
)

// ExportTemplates là mẫu Word của pf/extension đăng ký vào pf/export (`cap/export`, W10.17): KHTC Mẫu 1–5; Báo cáo
// tình hình dịch bệnh BM-10 (BF-CN-OUTBREAK, UP-08); Báo cáo phòng CNTS BM-11 tuần, tháng/quý/năm (W11.45).
func ExportTemplates() exportport.TemplateRegistration {
	ts := append(reportapp.WordTemplates(), obapp.WordTemplates()...)
	return exportport.TemplateRegistration{Owner: reportapp.WordOwner, Templates: append(ts, rcapp.BM11WordTemplates()...)}
}

// UseExporter nối năng lực xuất văn bản pf/export (Word KHTC Mẫu 1–5, Word BM-10, Word BM-11).
func (m *Module) UseExporter(e exportport.Exporter) {
	m.khtcReportSvc().Exporter = e
	m.ob.Exporter = e
	m.rc.Exporter = e
}
