package render

import (
	"strconv"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
)

// TablePDF dựng PDF A4 ngang (phông Roboto nhúng, ADR-0014) từ một bảng chữ — dùng cho module khác qua gốc lắp
// ráp (vd "Xuất PDF" lịch công tác của pf/extension, KN v7.7 P5). Cùng bố cục với báo cáo của pf/reports.
func TablePDF(title string, meta [][2]string, header []string, rows [][]string, at time.Time) ([]byte, error) {
	ds := domain.Dataset{Title: title, Empty: domain.EmptyMessage}
	for _, m := range meta {
		ds.Meta = append(ds.Meta, domain.Meta{Label: m[0], Value: m[1]})
	}
	for i, h := range header {
		ds.Columns = append(ds.Columns, domain.DataColumn{Key: "c" + strconv.Itoa(i), Title: h, Kind: domain.KindText})
	}
	for _, r := range rows {
		row := make([]domain.Value, len(header))
		for i := range header {
			if i < len(r) {
				row[i] = domain.Txt(r[i])
			}
		}
		ds.Rows = append(ds.Rows, row)
	}
	return PDF(ds, at, PDFOptions{})
}
