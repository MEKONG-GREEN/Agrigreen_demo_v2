// Package render dựng tệp Excel (excelize, BSD-3) và PDF (go-pdf/fpdf, MIT) từ
// domain.Dataset. Hai định dạng đọc CÙNG một tập dữ liệu nên khớp nội dung.
//
// Số liệu không đi qua float: ô số trong Excel ghi nguyên chuỗi decimal đã lưu
// (SetCellDefault — Excel hiểu là số, hiển thị theo định dạng số và dấu ngăn cách
// theo máy người xem); PDF in chuỗi đã định dạng kiểu Việt Nam (domain.FormatVN).
package render

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
)

// SheetName là tên trang tính dữ liệu.
const SheetName = "Báo cáo"

// Layout của trang tính (để kiểm thử tra ô).
const (
	TitleRow = 1 // dòng tiêu đề
	MetaRow  = 2 // dòng đầu của phần "Ngày xuất", "Kỳ"…
)

// HeaderRow trả dòng tiêu đề cột của tập dữ liệu.
func HeaderRow(ds domain.Dataset) int { return MetaRow + len(ds.Meta) + 1 }

func numFmt(c domain.DataColumn) string {
	if c.Kind == domain.KindInt || c.Scale <= 0 {
		return "#,##0"
	}
	return "#,##0." + strings.Repeat("0", int(c.Scale))
}

// XLSX dựng tệp Excel. at là "Ngày xuất" (ghi vào thuộc tính tệp để hai lần
// dựng cùng việc xuất cho cùng nội dung).
func XLSX(ds domain.Dataset, at time.Time) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", SheetName); err != nil {
		return nil, err
	}
	stamp := at.UTC().Format(time.RFC3339)
	if err := f.SetDocProps(&excelize.DocProperties{
		Title: ds.Title, Creator: "AgriGreen", LastModifiedBy: "AgriGreen", Created: stamp, Modified: stamp, Language: "vi-VN",
	}); err != nil {
		return nil, err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	if err != nil {
		return nil, err
	}
	head, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{WrapText: true, Vertical: "center", Horizontal: "center"},
		Fill:   excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DCEFE3"}},
		Border: []excelize.Border{{Type: "left", Style: 1}, {Type: "right", Style: 1}, {Type: "top", Style: 1}, {Type: "bottom", Style: 1}},
	})
	if err != nil {
		return nil, err
	}
	numStyles := map[string]int{}
	styleFor := func(c domain.DataColumn, strong bool) (int, error) {
		k := fmt.Sprintf("%s|%d|%v", c.Kind, c.Scale, strong)
		if id, ok := numStyles[k]; ok {
			return id, nil
		}
		st := &excelize.Style{Border: []excelize.Border{{Type: "left", Style: 1}, {Type: "right", Style: 1}, {Type: "bottom", Style: 1}}}
		if strong {
			st.Font = &excelize.Font{Bold: true}
		}
		if c.Kind != domain.KindText {
			nf := numFmt(c)
			st.CustomNumFmt = &nf
		}
		id, err := f.NewStyle(st)
		numStyles[k] = id
		return id, err
	}
	cell := func(col, row int) string {
		name, _ := excelize.CoordinatesToCellName(col, row)
		return name
	}
	if err := f.SetCellStr(SheetName, cell(1, TitleRow), ds.Title); err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(SheetName, cell(1, TitleRow), cell(1, TitleRow), bold); err != nil {
		return nil, err
	}
	for i, m := range ds.Meta {
		if err := f.SetCellStr(SheetName, cell(1, MetaRow+i), m.Label+": "+m.Value); err != nil {
			return nil, err
		}
	}
	hr := HeaderRow(ds)
	for i, c := range ds.Columns {
		if err := f.SetCellStr(SheetName, cell(i+1, hr), c.Title); err != nil {
			return nil, err
		}
		width := float64(len([]rune(c.Title)))*0.9 + 2
		if width < 12 {
			width = 12
		}
		if width > 40 {
			width = 40
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(SheetName, col, col, width); err != nil {
			return nil, err
		}
	}
	if len(ds.Columns) > 0 {
		if err := f.SetCellStyle(SheetName, cell(1, hr), cell(len(ds.Columns), hr), head); err != nil {
			return nil, err
		}
	}
	row := hr + 1
	write := func(vals []domain.Value, strong bool) error {
		for i, c := range ds.Columns {
			ref := cell(i+1, row)
			v := vals[i]
			var err error
			switch {
			case v.Dec != nil:
				err = f.SetCellDefault(SheetName, ref, v.Dec.String()) // chuỗi decimal nguyên vẹn, không qua float
			case v.Text != "":
				err = f.SetCellStr(SheetName, ref, v.Text)
			}
			if err != nil {
				return err
			}
			st, err := styleFor(c, strong)
			if err != nil {
				return err
			}
			if err := f.SetCellStyle(SheetName, ref, ref, st); err != nil {
				return err
			}
		}
		row++
		return nil
	}
	for _, r := range ds.Rows {
		if err := write(r, false); err != nil {
			return nil, err
		}
	}
	if len(ds.Rows) == 0 {
		if err := f.SetCellStr(SheetName, cell(1, row), ds.Empty); err != nil {
			return nil, err
		}
		row++
	}
	if ds.Total != nil {
		if err := write(ds.Total, true); err != nil {
			return nil, err
		}
	}
	row++
	for _, n := range footer(ds) {
		if err := f.SetCellStr(SheetName, cell(1, row), n); err != nil {
			return nil, err
		}
		row++
	}
	if err := f.SetPanes(SheetName, &excelize.Panes{Freeze: true, YSplit: hr, TopLeftCell: cell(1, hr+1), ActivePane: "bottomLeft"}); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// footer là các dòng chân trang: nhãn "Số liệu tạm tính" (nếu có) rồi ghi chú.
func footer(ds domain.Dataset) []string {
	var out []string
	if ds.Provisional {
		out = append(out, domain.ProvisionalLabel+" — số liệu chưa đầy đủ tại thời điểm xuất; có thể tạo lại khi đủ dữ liệu.")
	}
	return append(out, ds.Notes...)
}
