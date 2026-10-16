package extension

import reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"

const reportCatalogProvisionalNote = "Mẫu tạm: BRD chỉ nêu \"theo mẫu Bộ NN&MT/NN&PTNT\" nhưng chưa có mẫu biểu chính thức — bố cục sẽ điều chỉnh khi Bộ ban hành."

func reportCatalogLevels() []string { return []string{"national", "province", "commune"} }
func reportCatalogColumns(extra ...reportsport.CatalogColumn) []reportsport.CatalogColumn {
	return append([]reportsport.CatalogColumn{
		{Key: "province_code", Title: "Mã tỉnh", Kind: "text", MinLevel: "province"},
		{Key: "province_name", Title: "Tỉnh/Thành phố", Kind: "text", MinLevel: "province"},
		{Key: "commune_code", Title: "Mã xã", Kind: "text", MinLevel: "commune"},
		{Key: "commune_name", Title: "Xã/Phường", Kind: "text", MinLevel: "commune"},
	}, extra...)
}
func reportCatalogTemplateColumns(sources map[string]reportsport.CatalogSource, source string, keys ...string) []reportsport.CatalogTemplateColumn {
	out := make([]reportsport.CatalogTemplateColumn, 0, len(keys))
	for _, key := range keys {
		for _, column := range sources[source].Columns {
			if column.Key == key {
				out = append(out, reportsport.CatalogTemplateColumn{Key: key, Title: column.Title})
				break
			}
		}
	}
	return out
}

// ReportCatalog owns the source schema and default report layouts of KN.
func ReportCatalog() reportsport.CatalogRegistration {
	sources := map[string]reportsport.CatalogSource{
		reportsport.SourceSowing: {Key: reportsport.SourceSowing, Module: "extension", Levels: reportCatalogLevels(), DefaultLevel: "province", NeedsPeriod: true,
			Columns: reportCatalogColumns(
				reportsport.CatalogColumn{Key: "season_code", Title: "Vụ", Kind: "text"},
				reportsport.CatalogColumn{Key: "variety_code", Title: "Giống", Kind: "text"},
				reportsport.CatalogColumn{Key: "sown_area_ha", Title: "Diện tích đã sạ (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "cooperatives", Title: "Số HTX báo cáo", Kind: "int"},
				reportsport.CatalogColumn{Key: "reports", Title: "Số báo cáo", Kind: "int"},
			)},
		reportsport.SourceGrowthPest: {Key: reportsport.SourceGrowthPest, Module: "extension", Levels: reportCatalogLevels(), DefaultLevel: "province", NeedsPeriod: true,
			Columns: reportCatalogColumns(
				reportsport.CatalogColumn{Key: "pest_code", Title: "Sâu bệnh", Kind: "text"},
				reportsport.CatalogColumn{Key: "severity", Title: "Mức độ nhiễm", Kind: "text"},
				reportsport.CatalogColumn{Key: "affected_area_ha", Title: "Diện tích nhiễm (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "observations", Title: "Số lượt quan sát", Kind: "int"},
				reportsport.CatalogColumn{Key: "urgent", Title: "Phản ánh khẩn", Kind: "int"},
			)},
		reportsport.SourceProgramProgress: {Key: reportsport.SourceProgramProgress, Module: "extension",
			Levels: []string{"national", "province", "commune", "cooperative"}, DefaultLevel: "province", UsesSeason: true,
			Columns: reportCatalogColumns(
				reportsport.CatalogColumn{Key: "cooperative_name", Title: "HTX", Kind: "text", MinLevel: "cooperative"},
				reportsport.CatalogColumn{Key: "season", Title: "Vụ", Kind: "text"},
				reportsport.CatalogColumn{Key: "target_ha", Title: "Mục tiêu Đề án (ha)", Kind: "decimal", Scale: 2, MaxLevel: "province"},
				reportsport.CatalogColumn{Key: "registered_ha", Title: "Đăng ký (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "deploying_ha", Title: "Đang triển khai (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "completed_ha", Title: "Hoàn thành (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "registered_pct", Title: "Đăng ký/Mục tiêu (%)", Kind: "percent", Scale: 2, MaxLevel: "province"},
				reportsport.CatalogColumn{Key: "cooperatives", Title: "Số HTX", Kind: "int"},
				reportsport.CatalogColumn{Key: "unapproved", Title: "Chưa duyệt", Kind: "int"},
			)},
		reportsport.SourceTasksSLA: {Key: reportsport.SourceTasksSLA, Module: "extension", Levels: reportCatalogLevels(), DefaultLevel: "province", NeedsPeriod: true,
			Columns: reportCatalogColumns(
				reportsport.CatalogColumn{Key: "opened", Title: "Nhiệm vụ mới", Kind: "int"},
				reportsport.CatalogColumn{Key: "resolved", Title: "Đã xử lý", Kind: "int"},
				reportsport.CatalogColumn{Key: "on_time", Title: "Đúng hạn SLA", Kind: "int"},
				reportsport.CatalogColumn{Key: "escalated", Title: "Leo thang", Kind: "int"},
				reportsport.CatalogColumn{Key: "open", Title: "Đang mở", Kind: "int"},
				reportsport.CatalogColumn{Key: "overdue", Title: "Quá hạn", Kind: "int"},
			)},
		reportsport.SourcePeriodSummary: {Key: reportsport.SourcePeriodSummary, Module: "extension", Levels: reportCatalogLevels(), DefaultLevel: "province", NeedsPeriod: true,
			Columns: reportCatalogColumns(
				reportsport.CatalogColumn{Key: "sown_area_ha", Title: "Diện tích đã sạ (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "affected_area_ha", Title: "Diện tích nhiễm sâu bệnh (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "pest_reports", Title: "Báo cáo sâu bệnh", Kind: "int"},
				reportsport.CatalogColumn{Key: "urgent_reports", Title: "Phản ánh khẩn", Kind: "int"},
				reportsport.CatalogColumn{Key: "registered_ha", Title: "Đề án: đăng ký (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "completed_ha", Title: "Đề án: hoàn thành (ha)", Kind: "decimal", Scale: 2},
				reportsport.CatalogColumn{Key: "tasks_opened", Title: "Nhiệm vụ mới", Kind: "int"},
				reportsport.CatalogColumn{Key: "tasks_resolved", Title: "Nhiệm vụ đã xử lý", Kind: "int"},
				reportsport.CatalogColumn{Key: "tasks_overdue", Title: "Nhiệm vụ quá hạn", Kind: "int"},
			)},
	}
	mk := func(key string, def reportsport.CatalogDefinition) reportsport.CatalogTemplate {
		return reportsport.CatalogTemplate{Key: key, Def: def}
	}
	return reportsport.CatalogRegistration{Sources: sources, Templates: []reportsport.CatalogTemplate{
		mk("kn.sowing_progress", reportsport.CatalogDefinition{Title: "Báo cáo tiến độ gieo sạ", Source: reportsport.SourceSowing, Grouping: "commune",
			Columns: reportCatalogTemplateColumns(sources, reportsport.SourceSowing, "province_name", "commune_name", "season_code", "variety_code", "sown_area_ha", "cooperatives"),
			Formats: []string{"xlsx", "pdf"}, Provisional: true, FormNote: reportCatalogProvisionalNote}),
		mk("kn.growth_pest", reportsport.CatalogDefinition{Title: "Báo cáo tình hình sinh trưởng và sâu bệnh", Source: reportsport.SourceGrowthPest, Grouping: "commune",
			Columns: reportCatalogTemplateColumns(sources, reportsport.SourceGrowthPest, "province_name", "commune_name", "pest_code", "severity", "affected_area_ha", "observations", "urgent"),
			Formats: []string{"xlsx", "pdf"}, Provisional: true, FormNote: reportCatalogProvisionalNote}),
		mk("kn.program_progress", reportsport.CatalogDefinition{Title: "Báo cáo tiến độ Đề án 1 triệu ha lúa chất lượng cao, phát thải thấp", Source: reportsport.SourceProgramProgress,
			Grouping: "province",
			Columns: reportCatalogTemplateColumns(sources, reportsport.SourceProgramProgress, "province_name", "season", "target_ha", "registered_ha", "deploying_ha", "completed_ha",
				"registered_pct", "cooperatives", "unapproved"),
			Formats: []string{"xlsx", "pdf"}, Provisional: true, FormNote: reportCatalogProvisionalNote}),
		mk("kn.tasks_sla", reportsport.CatalogDefinition{Title: "Báo cáo xử lý nhiệm vụ hỗ trợ", Source: reportsport.SourceTasksSLA, Grouping: "province",
			Columns: reportCatalogTemplateColumns(sources, reportsport.SourceTasksSLA, "province_name", "opened", "resolved", "on_time", "escalated", "open", "overdue"),
			Formats: []string{"xlsx", "pdf"}, Provisional: true, FormNote: reportCatalogProvisionalNote}),
		mk("kn.period_summary", reportsport.CatalogDefinition{Title: "Tổng hợp báo cáo kỳ", Source: reportsport.SourcePeriodSummary, Grouping: "province",
			Columns: reportCatalogTemplateColumns(sources, reportsport.SourcePeriodSummary, "province_name", "sown_area_ha", "affected_area_ha", "pest_reports", "urgent_reports",
				"registered_ha", "completed_ha", "tasks_opened", "tasks_resolved", "tasks_overdue"),
			Formats: []string{"xlsx", "pdf"}, Provisional: true, FormNote: reportCatalogProvisionalNote}),
	}}
}
