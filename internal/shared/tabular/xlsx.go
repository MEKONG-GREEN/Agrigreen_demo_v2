package tabular

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// Hệ số dung lượng sau giải nén so với dung lượng tệp (chống zip bomb). Tệp
// .xlsx thường nén 5–10 lần; giới hạn rộng hơn nhiều nhưng vẫn chặn bom nén.
const (
	unzipFactor   = 40
	minUnzipLimit = 16 << 20
)

// readXLSX đọc trang tính đầu tiên. Giá trị lấy dạng thô (không áp định dạng số)
// để số không bị làm tròn theo định dạng hiển thị; ô định dạng ngày được đổi
// sang YYYY-MM-DD. Ô có công thức làm cả tệp bị từ chối.
func readXLSX(data []byte, lim Limits) (*Table, error) {
	unzip := int64(len(data)) * unzipFactor
	if unzip < minUnzipLimit {
		unzip = minUnzipLimit
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		RawCellValue: true, UnzipSizeLimit: unzip, UnzipXMLSizeLimit: unzip,
	})
	if err != nil {
		if strings.Contains(err.Error(), "unzip size") {
			return nil, ErrTooLarge
		}
		return nil, ErrFormat
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, ErrEmpty
	}
	sheet := sheets[0]
	raw, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, ErrFormat
	}
	date1904 := false
	if wp, err := f.GetWorkbookProps(); err == nil && wp.Date1904 != nil {
		date1904 = *wp.Date1904
	}
	dates := dateStyles{f: f, cache: map[int]bool{}}

	t := &Table{Format: FormatXLSX}
	for i, rec := range raw {
		line := i + 1
		// Kiểm công thức trên mọi ô của dòng (trong giới hạn cột), kể cả ô có giá trị rỗng.
		for j := range min(len(rec), lim.MaxCols) {
			ref, _ := excelize.CoordinatesToCellName(j+1, line)
			if fm, err := f.GetCellFormula(sheet, ref); err != nil {
				return nil, ErrFormat
			} else if fm != "" {
				return nil, &FormulaError{Cell: ref}
			}
		}
		cells := trimCells(rec)
		if cells == nil {
			continue
		}
		if len(cells) > lim.MaxCols {
			return nil, ErrTooManyColumns
		}
		for j, v := range cells {
			if v == "" {
				continue
			}
			ref, _ := excelize.CoordinatesToCellName(j+1, line)
			cells[j] = dates.convert(sheet, ref, v, date1904)
			if !utf8.ValidString(cells[j]) {
				return nil, ErrFormat
			}
		}
		if t.Header == nil {
			t.Header = cells
			continue
		}
		if len(t.Rows) >= lim.MaxRows {
			return nil, ErrTooManyRows
		}
		t.Rows = append(t.Rows, Row{Line: line, Cells: cells})
	}
	if t.Header == nil {
		return nil, ErrEmpty
	}
	return t, nil
}

// dateStyles nhận ô định dạng ngày theo mã định dạng số của kiểu ô.
type dateStyles struct {
	f     *excelize.File
	cache map[int]bool
}

func (d dateStyles) convert(sheet, ref, v string, date1904 bool) string {
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return v
	}
	if typ, err := d.f.GetCellType(sheet, ref); err != nil || (typ != excelize.CellTypeNumber && typ != excelize.CellTypeUnset) {
		return v
	}
	id, err := d.f.GetCellStyle(sheet, ref)
	if err != nil || id == 0 {
		return v
	}
	isDate, ok := d.cache[id]
	if !ok {
		st, err := d.f.GetStyle(id)
		isDate = err == nil && st != nil && isDateFormat(st.NumFmt, st.CustomNumFmt)
		d.cache[id] = isDate
	}
	if !isDate || n < 1 {
		return v
	}
	t, err := excelize.ExcelDateToTime(n, date1904)
	if err != nil {
		return v
	}
	return t.Format("2006-01-02")
}

// isDateFormat: mã định dạng dựng sẵn của ngày (ECMA-376 18.8.30) hoặc định dạng
// riêng có thành phần ngày/năm (bỏ qua phần trong ngoặc kép và [..]).
func isDateFormat(id int, custom *string) bool {
	switch {
	case id >= 14 && id <= 17, id == 22, id >= 27 && id <= 36, id >= 50 && id <= 58:
		return true
	}
	if custom == nil {
		return false
	}
	var b strings.Builder
	quoted, bracket, esc := false, false, false
	for _, r := range strings.ToLower(*custom) {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '[':
			bracket = true
		case r == ']':
			bracket = false
		case bracket:
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	return strings.ContainsAny(s, "dy")
}

// WriteXLSX ghi một trang tính: dòng tiêu đề in đậm, cố định khi cuộn, sau đó
// các dòng dữ liệu. Mọi ô ghi dạng chuỗi — chuỗi bắt đầu bằng "=" vẫn là chữ,
// không thành công thức.
func WriteXLSX(w io.Writer, sheet string, header []string, rows [][]string) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if sheet == "" {
		sheet = "Sheet1"
	}
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	text, err := f.NewStyle(&excelize.Style{NumFmt: 49}) // "@": giữ số 0 đầu của mã, SĐT
	if err != nil {
		return err
	}
	widths := make([]int, len(header))
	put := func(line int, vals []string, style int) error {
		for j, v := range vals {
			ref, err := excelize.CoordinatesToCellName(j+1, line)
			if err != nil {
				return err
			}
			if err := f.SetCellStr(sheet, ref, v); err != nil {
				return err
			}
			if err := f.SetCellStyle(sheet, ref, ref, style); err != nil {
				return err
			}
			if j < len(widths) {
				widths[j] = max(widths[j], utf8.RuneCountInString(v))
			}
		}
		return nil
	}
	if err := put(1, header, bold); err != nil {
		return err
	}
	for i, r := range rows {
		if err := put(i+2, r, text); err != nil {
			return err
		}
	}
	for j, n := range widths {
		col, _ := excelize.ColumnNumberToName(j + 1)
		if err := f.SetColWidth(sheet, col, col, float64(min(max(n+2, 10), 60))); err != nil {
			return err
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	_, err = f.WriteTo(w)
	return err
}
