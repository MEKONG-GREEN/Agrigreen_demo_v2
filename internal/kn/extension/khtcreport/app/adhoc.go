package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

func writeAdhocFigures(w *workbook, data domain.Data) {
	w.line("Chỉ tiêu", "ĐVT", "Trong khoảng đã chọn", "Nguồn")
	for _, f := range domain.AdhocFigures(data) {
		w.line(f.Name, f.Unit, f.Value, f.Source)
	}
}
func adhocWordTable() doctpl.Block {
	return doctpl.Block{Kind: doctpl.KindTable, Each: "adhoc_figures", EmptyText: "Không có chỉ tiêu.", Columns: []doctpl.Column{
		{Header: "Chỉ tiêu", Value: "{adhoc_figures.ten}", Width: 3300},
		{Header: "ĐVT", Value: "{adhoc_figures.dvt}", Width: 800},
		{Header: "Trong khoảng đã chọn", Value: "{adhoc_figures.gia_tri}", Width: 1800},
		{Header: "Nguồn", Value: "{adhoc_figures.nguon}", Width: 2554},
	}}
}
func adhocWordItems(data domain.Data) []doctpl.Item {
	out := []doctpl.Item{}
	for _, f := range domain.AdhocFigures(data) {
		out = append(out, doctpl.Item{"ten": f.Name, "dvt": f.Unit, "gia_tri": VNNumber(f.Value), "nguon": f.Source})
	}
	return out
}
