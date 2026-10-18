package tabular

import (
	"io"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

// Cell là một ô của bảng nhiều trang tính: văn bản, hoặc số thập phân (chuỗi decimal → ô số, hiển thị #,##0.###).
// Package nghiệp vụ cấm float (archlint R-10) chỉ truyền chuỗi decimal; đổi sang số của Excel nằm ở đây.
type Cell struct {
	Text  string
	Num   string // chuỗi decimal; rỗng = ô văn bản
	Bold  bool
	Title bool // chữ đậm cỡ 13 (tiêu đề)
}

// Sheet là một trang tính.
type Sheet struct {
	Name   string
	Rows   [][]Cell // dòng nil = dòng trống
	Widths []int    // độ rộng cột (ký tự), theo thứ tự cột; thiếu = mặc định
}

// WriteSheetsXLSX ghi nhiều trang tính (W11.44 tệp chia sẻ Chi cục CNTS). Ô số ghi kiểu số (không công thức).
func WriteSheetsXLSX(w io.Writer, sheets []Sheet) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	fmtNum := "#,##0.###"
	styles := map[[3]bool]int{}
	style := func(num, bold, title bool) (int, error) {
		k := [3]bool{num, bold, title}
		if id, ok := styles[k]; ok {
			return id, nil
		}
		st := &excelize.Style{}
		if num {
			st.CustomNumFmt = &fmtNum
		}
		if bold || title {
			st.Font = &excelize.Font{Bold: true}
			if title {
				st.Font.Size = 13
			}
		}
		id, err := f.NewStyle(st)
		styles[k] = id
		return id, err
	}
	for i, sh := range sheets {
		if i == 0 {
			if err := f.SetSheetName("Sheet1", sh.Name); err != nil {
				return err
			}
		} else if _, err := f.NewSheet(sh.Name); err != nil {
			return err
		}
		for r, row := range sh.Rows {
			for j, c := range row {
				ref, err := excelize.CoordinatesToCellName(j+1, r+1)
				if err != nil {
					return err
				}
				num := c.Num != ""
				switch {
				case num:
					d, err := decimal.NewFromString(c.Num)
					if err != nil {
						return err
					}
					if err := f.SetCellFloat(sh.Name, ref, d.InexactFloat64(), -1, 64); err != nil {
						return err
					}
				case c.Text != "":
					if err := f.SetCellStr(sh.Name, ref, c.Text); err != nil {
						return err
					}
				default:
					continue
				}
				if num || c.Bold || c.Title {
					id, err := style(num, c.Bold, c.Title)
					if err != nil {
						return err
					}
					if err := f.SetCellStyle(sh.Name, ref, ref, id); err != nil {
						return err
					}
				}
			}
		}
		for j, wd := range sh.Widths {
			col, err := excelize.ColumnNumberToName(j + 1)
			if err != nil {
				return err
			}
			if err := f.SetColWidth(sh.Name, col, col, float64(wd)); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}

// SetColumnWidth applies presentation-only sizing in whole character units.
// Keep the Excel library's floating-point layout API outside exact numeric callers.
func SetColumnWidth(file *excelize.File, sheet, first, last string, width int) error {
	return file.SetColWidth(sheet, first, last, float64(width))
}
