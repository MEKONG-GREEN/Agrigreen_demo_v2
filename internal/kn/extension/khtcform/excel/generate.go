package excel

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

func codeTitle(spec sheetSpec) string {
	if spec.def.Kind == "narrative" {
		return "Mã mục lời văn"
	}
	return "Mã hạng mục"
}

var adjustmentHeaders = []string{"Mã hạng mục", "Kỳ được điều chỉnh (ID)", "Số điều chỉnh", "Lý do"}

// Generate preserves configured business column letters. Its visible appended
// code column identifies rows without relying on position or display names.
// Every value is explicitly written as text, including formula-looking prose.
// Declared formula columns use only the explicit trusted server_value subset.
func (c *Codec) Generate(content port.KHTCFormExcelContent) ([]byte, error) {
	if !c.initialized() {
		return nil, ErrSnapshot
	}
	if err := c.validateContentShape(content); err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	lockedStyle, err := f.NewStyle(&excelize.Style{NumFmt: 49, Fill: excelize.Fill{Type: "pattern", Color: []string{"E7E6E6"}, Pattern: 1}, Protection: &excelize.Protection{Locked: true}})
	if err != nil {
		return nil, ErrWorkbook
	}
	inputStyle, err := f.NewStyle(&excelize.Style{NumFmt: 49, Protection: &excelize.Protection{Locked: false}})
	if err != nil {
		return nil, ErrWorkbook
	}
	for i, sh := range c.snapshot.Sheets {
		spec := c.sheets[sh.Key]
		if i == 0 {
			err = f.SetSheetName("Sheet1", sh.Name)
		} else {
			_, err = f.NewSheet(sh.Name)
		}
		if err != nil {
			return nil, ErrWorkbook
		}
		put := func(col, row int, value string, style int) error {
			ref, err := excelize.CoordinatesToCellName(col, row)
			if err != nil {
				return err
			}
			if err := f.SetCellStr(sh.Name, ref, value); err != nil {
				return err
			}
			return f.SetCellStyle(sh.Name, ref, ref, style)
		}
		metadata := []string{magic, c.hash, sh.Key, sh.TemplateID.String(), strconv.FormatInt(sh.TemplateVersion, 10), sh.TemplateHash}
		for col, value := range metadata {
			if err := put(col+1, 1, value, lockedStyle); err != nil {
				return nil, ErrWorkbook
			}
		}
		if err := f.SetRowVisible(sh.Name, 1, false); err != nil {
			return nil, ErrWorkbook
		}
		for _, col := range sh.Columns {
			n, _ := excelize.ColumnNameToNumber(col.Letter)
			if err := put(n, 2, col.Letter, lockedStyle); err != nil {
				return nil, ErrWorkbook
			}
			if err := put(n, 3, col.Title, lockedStyle); err != nil {
				return nil, ErrWorkbook
			}
		}
		codeColumn := spec.lastColumn + 1
		codeLetter, _ := excelize.ColumnNumberToName(codeColumn)
		if err := put(codeColumn, 2, codeLetter, lockedStyle); err != nil {
			return nil, ErrWorkbook
		}
		if err := put(codeColumn, 3, codeTitle(spec), lockedStyle); err != nil {
			return nil, ErrWorkbook
		}
		for rowIndex, row := range sh.Rows {
			line := firstDataRow + rowIndex
			for _, col := range sh.Columns {
				n, _ := excelize.ColumnNameToNumber(col.Letter)
				value, style := row.SystemValues[col.Letter], lockedStyle
				if editable(col, row) {
					value, style = content.Rows[sh.Key][row.Code][col.Letter], inputStyle
				}
				if err := put(n, line, value, style); err != nil {
					return nil, ErrWorkbook
				}
			}
			if err := put(codeColumn, line, row.Code, lockedStyle); err != nil {
				return nil, ErrWorkbook
			}
		}
		markerLine := firstDataRow + len(sh.Rows) + 1
		marker := endMarker
		if sh.AllowAdjustments {
			marker = adjustmentMarker
		}
		if err := put(1, markerLine, marker, lockedStyle); err != nil {
			return nil, ErrWorkbook
		}
		if sh.AllowAdjustments {
			for col, header := range adjustmentHeaders {
				if err := put(col+1, markerLine+1, header, lockedStyle); err != nil {
					return nil, ErrWorkbook
				}
			}
			line := markerLine + 2
			for _, a := range content.Adjustments {
				if a.Sheet != sh.Key {
					continue
				}
				period := ""
				if a.PeriodID != [16]byte{} {
					period = a.PeriodID.String()
				}
				for col, value := range []string{a.ItemCode, period, a.Amount, a.Reason} {
					if err := put(col+1, line, value, inputStyle); err != nil {
						return nil, ErrWorkbook
					}
				}
				line++
			}
		}
		if err := f.SetPanes(sh.Name, &excelize.Panes{Freeze: true, YSplit: 3, TopLeftCell: "A4", ActivePane: "bottomLeft"}); err != nil {
			return nil, ErrWorkbook
		}
	}
	out, err := f.WriteToBuffer()
	if err != nil {
		return nil, ErrWorkbook
	}
	if out.Len() > MaxWorkbookBytes {
		return nil, ErrLimit
	}
	if err := preflight(out.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ErrorWorkbook exports fixed diagnostics as text, without copying uploaded
// values or formulas into a new workbook. An empty report is still a valid XLSX.
func (c *Codec) ErrorWorkbook(preview port.KHTCFormExcelPreview) ([]byte, error) {
	if !c.initialized() {
		return nil, ErrSnapshot
	}
	p, ok := preview.(*previewData)
	if !ok || p == nil || p.owner != c {
		return nil, ErrPreview
	}
	// Group diagnostics by original row, bounding export cells by the input row
	// limit rather than multiplying each cell issue into a seven-cell XLSX row.
	type rowKey struct {
		sheet string
		line  int
		code  string
	}
	type details struct{ columns, severities, rules, messages []string }
	order := []rowKey{}
	grouped := map[rowKey]*details{}
	for _, issue := range p.report.Issues {
		key := rowKey{issue.Sheet, issue.Row, issue.ItemCode}
		if grouped[key] == nil {
			grouped[key] = &details{}
			order = append(order, key)
		}
		d := grouped[key]
		d.columns = append(d.columns, issue.Column)
		d.severities = append(d.severities, issue.Severity)
		d.rules = append(d.rules, issue.Code)
		d.messages = append(d.messages, issue.Column+": "+issue.Message)
	}
	rows := make([][]string, 0, len(order))
	for _, key := range order {
		d := grouped[key]
		row := []string{key.sheet, strconv.Itoa(key.line), key.code, strings.Join(d.columns, "; "), strings.Join(d.severities, "; "), strings.Join(d.rules, "; "), strings.Join(d.messages, "\n")}
		for _, value := range row {
			if len(value) > MaxCellBytes {
				return nil, ErrLimit
			}
		}
		rows = append(rows, row)
	}
	var out bytes.Buffer
	if err := tabular.WriteXLSX(&out, "Dòng cần chú ý", []string{"Sheet", "Dòng", "Mã hạng mục / mục", "Cột", "Mức", "Mã lỗi", "Nội dung"}, rows); err != nil {
		return nil, ErrWorkbook
	}
	if out.Len() > MaxWorkbookBytes {
		return nil, ErrLimit
	}
	return out.Bytes(), nil
}
