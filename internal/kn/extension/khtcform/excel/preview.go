package excel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

type previewData struct {
	owner  *Codec
	bytes  []byte
	report port.KHTCFormExcelValidation
	valid  port.KHTCFormExcelContent
}

func (p *previewData) Result() port.KHTCFormExcelValidation {
	if p == nil {
		return port.KHTCFormExcelValidation{}
	}
	out := p.report
	out.Sheets = append([]port.KHTCFormExcelSheetSummary{}, out.Sheets...)
	out.Issues = append([]port.KHTCFormExcelIssue{}, out.Issues...)
	return out
}

var number = regexp.MustCompile(`^-?[0-9]{1,15}(\.[0-9]{1,6})?$`)
var integer = regexp.MustCompile(`^-?[0-9]{1,9}$`)

// Trusted computed values can exceed input digit counts after accumulation.
// They remain bounded ordinary numeric text, never an expression to execute.
var projectedNumber = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
var projectedInteger = regexp.MustCompile(`^-?[0-9]+$`)

func validateField(value string, col port.KHTCFormExcelColumn) (string, string) {
	if value == "" {
		if col.Required {
			return "required", "Bắt buộc nhập giá trị"
		}
		return "", ""
	}
	limit := col.MaxLength
	if limit == 0 {
		limit = MaxTextRunes
	}
	if !validText(value, limit) {
		return "length", "Giá trị vượt giới hạn độ dài của cột"
	}
	switch col.DataType {
	case "number":
		if !number.MatchString(value) {
			return "number", "Phải là số thập phân thường, tối đa 15 chữ số nguyên và 6 chữ số lẻ"
		}
	case "integer":
		if !integer.MatchString(value) {
			return "integer", "Phải là số nguyên, tối đa 9 chữ số"
		}
	}
	if col.NonNegative && strings.HasPrefix(value, "-") && (col.DataType == "integer" || strings.ContainsAny(value, "123456789")) {
		return "negative", "Không nhận số âm; số điều chỉnh nhập ở khối Điều chỉnh kỳ trước"
	}
	if len(col.Choices) > 0 && !slices.Contains(col.Choices, value) {
		return "choice", "Giá trị không thuộc danh sách chọn của cột"
	}
	return "", ""
}

func rawCell(f *excelize.File, sheet, letter string, line int) (string, bool, error) {
	ref := letter + strconv.Itoa(line)
	value, err := f.GetCellValue(sheet, ref, excelize.Options{RawCellValue: true})
	if err != nil {
		return "", false, ErrWorkbook
	}
	formula, err := f.GetCellFormula(sheet, ref)
	if err != nil {
		return "", false, ErrWorkbook
	}
	if !validText(value, MaxCellBytes) {
		return "", false, ErrLimit
	}
	return value, formula != "", nil
}

// Preview checks structure against the pinned snapshot and validates each row.
// Rows with errors are excluded independently; ignored locked values do not
// invalidate editable values in that row. No uploaded formula is evaluated.
func (c *Codec) Preview(data []byte) (port.KHTCFormExcelPreview, error) {
	if err := preflight(data); err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true, UnzipSizeLimit: MaxUnzipBytes, UnzipXMLSizeLimit: MaxPartBytes})
	if err != nil {
		return nil, ErrWorkbook
	}
	defer func() { _ = f.Close() }()
	list := f.GetSheetList()
	if len(list) != len(c.snapshot.Sheets) {
		return nil, ErrWorkbook
	}
	for _, sh := range c.snapshot.Sheets {
		if !slices.Contains(list, sh.Name) {
			return nil, ErrWorkbook
		}
	}
	hash := sha256.Sum256(data)
	p := &previewData{owner: c, bytes: bytes.Clone(data), report: port.KHTCFormExcelValidation{SnapshotHash: c.hash, FileSHA256: hex.EncodeToString(hash[:]), Sheets: []port.KHTCFormExcelSheetSummary{}, Issues: []port.KHTCFormExcelIssue{}}, valid: port.KHTCFormExcelContent{Rows: map[string]map[string]map[string]string{}, Adjustments: []port.KHTCFormExcelAdjustment{}}}
	totalRows, totalAdjustments := 0, 0
	for _, sh := range c.snapshot.Sheets {
		spec := c.sheets[sh.Key]
		rows, err := f.GetRows(sh.Name, excelize.Options{RawCellValue: true})
		if err != nil || len(rows) < firstDataRow {
			return nil, ErrWorkbook
		}
		metadata := []string{magic, c.hash, sh.Key, sh.TemplateID.String(), strconv.FormatInt(sh.TemplateVersion, 10), sh.TemplateHash}
		for i, expected := range metadata {
			letter, _ := excelize.ColumnNumberToName(i + 1)
			value, formula, err := rawCell(f, sh.Name, letter, 1)
			if err != nil {
				return nil, err
			}
			if formula || value != expected {
				return nil, ErrWorkbook
			}
		}
		if err := onlyColumns(f, sh.Name, 1, len(metadata)); err != nil {
			return nil, err
		}
		for i := len(metadata); i < len(rows[0]); i++ {
			if rows[0][i] != "" {
				return nil, ErrWorkbook
			}
		}
		codeLetter, _ := excelize.ColumnNumberToName(spec.lastColumn + 1)
		for n := 1; n <= spec.lastColumn+1; n++ {
			letter, _ := excelize.ColumnNumberToName(n)
			expectedLetter, expectedTitle := "", ""
			if col, ok := spec.columns[letter]; ok {
				expectedLetter, expectedTitle = letter, col.Title
			}
			if n == spec.lastColumn+1 {
				expectedLetter, expectedTitle = letter, codeTitle(spec)
			}
			for line, expected := range map[int]string{2: expectedLetter, 3: expectedTitle} {
				value, formula, err := rawCell(f, sh.Name, letter, line)
				if err != nil {
					return nil, err
				}
				if formula || value != expected {
					return nil, ErrWorkbook
				}
			}
		}
		for _, line := range []int{2, 3} {
			if len(rows[line-1]) > spec.lastColumn+1 {
				return nil, ErrWorkbook
			}
		}
		markerLine := 0
		marker := endMarker
		if sh.AllowAdjustments {
			marker = adjustmentMarker
		}
		for i := firstDataRow - 1; i < len(rows); i++ {
			if len(rows[i]) > 0 && rows[i][0] == marker {
				otherValues := false
				for _, value := range rows[i][1:] {
					if value != "" {
						otherValues = true
						break
					}
				}
				if otherValues {
					continue
				} // a literal in a coded data row is not a marker.
				if markerLine != 0 {
					return nil, ErrWorkbook
				}
				markerLine = i + 1
				if err := onlyColumns(f, sh.Name, markerLine, 1); err != nil {
					return nil, err
				}
				for _, value := range rows[i][1:] {
					if value != "" {
						return nil, ErrWorkbook
					}
				}
				_, formula, err := rawCell(f, sh.Name, "A", markerLine)
				if err != nil {
					return nil, err
				}
				if formula {
					return nil, ErrWorkbook
				}
			}
		}
		if markerLine < firstDataRow {
			return nil, ErrWorkbook
		}
		summary := port.KHTCFormExcelSheetSummary{Sheet: sh.Name}
		candidates := []candidate{}
		byCode := map[string][]int{}
		for line := firstDataRow; line < markerLine; line++ {
			record := rows[line-1]
			if blank(record) {
				continue
			}
			totalRows++
			if totalRows > MaxRows {
				return nil, ErrLimit
			}
			if len(record) > spec.lastColumn+1 {
				return nil, ErrWorkbook
			}
			for n := 1; n <= spec.lastColumn; n++ {
				letter, _ := excelize.ColumnNumberToName(n)
				if _, ok := spec.columns[letter]; ok {
					continue
				}
				value, formula, err := rawCell(f, sh.Name, letter, line)
				if err != nil {
					return nil, err
				}
				if value != "" || formula {
					return nil, ErrWorkbook
				}
			}
			code, codeFormula, err := rawCell(f, sh.Name, codeLetter, line)
			if err != nil {
				return nil, err
			}
			code = strings.TrimSpace(code)
			item := candidate{line: line, code: code, values: map[string]string{}}
			row, exists := spec.rows[code]
			if !exists || codeFormula {
				item.code = "" // never echo an unknown uploaded value into diagnostics.
				item.issues = append(item.issues, issue(sh.Name, line, "", codeLetter, "error", "item_code", "Chọn mã hạng mục hoặc mã mục lời văn thuộc sheet"))
			} else {
				byCode[code] = append(byCode[code], len(candidates))
				for _, col := range sh.Columns {
					value, formula, err := rawCell(f, sh.Name, col.Letter, line)
					if err != nil {
						return nil, err
					}
					if !editable(col, row) {
						if formula || value != row.SystemValues[col.Letter] {
							item.issues = append(item.issues, issue(sh.Name, line, code, col.Letter, "ignored", "locked", "Bỏ qua giá trị đã đổi; giữ giá trị hệ thống"))
						}
						continue
					}
					value = strings.TrimSpace(value)
					if formula {
						item.issues = append(item.issues, issue(sh.Name, line, code, col.Letter, "error", "formula", "Ô nhập không nhận công thức Excel"))
						continue
					}
					if rule, message := validateField(value, col); rule != "" {
						item.issues = append(item.issues, issue(sh.Name, line, code, col.Letter, "error", rule, message))
					}
					item.values[col.Letter] = value
				}
			}
			candidates = append(candidates, item)
		}
		for code, indexes := range byCode {
			if len(indexes) > 1 {
				for _, index := range indexes {
					candidates[index].issues = append(candidates[index].issues, issue(sh.Name, candidates[index].line, code, codeLetter, "error", "duplicate_code", "Mã xuất hiện nhiều lần; bỏ qua mọi dòng trùng mã"))
				}
			}
		}
		p.valid.Rows[sh.Key] = map[string]map[string]string{}
		for _, item := range candidates {
			summary.Rows++
			bad := false
			for _, i := range item.issues {
				if i.Severity == "error" {
					bad = true
				} else {
					summary.IgnoredCells++
				}
				p.report.Issues = append(p.report.Issues, i)
			}
			if bad {
				summary.Invalid++
			} else {
				summary.Valid++
				p.valid.Rows[sh.Key][item.code] = item.values
			}
		}
		if sh.AllowAdjustments {
			if markerLine+1 > len(rows) {
				return nil, ErrWorkbook
			}
			for n, expected := range adjustmentHeaders {
				letter, _ := excelize.ColumnNumberToName(n + 1)
				value, formula, err := rawCell(f, sh.Name, letter, markerLine+1)
				if err != nil {
					return nil, err
				}
				if formula || value != expected {
					return nil, ErrWorkbook
				}
			}
			if len(rows[markerLine]) > 4 {
				return nil, ErrWorkbook
			}
			for line := markerLine + 2; line <= len(rows); line++ {
				if blank(rows[line-1]) {
					continue
				}
				totalAdjustments++
				if totalAdjustments > MaxAdjustments {
					return nil, ErrLimit
				}
				if len(rows[line-1]) > 4 {
					return nil, ErrWorkbook
				}
				a, issues, err := c.readAdjustment(f, spec, line)
				if err != nil {
					return nil, err
				}
				summary.Rows++
				if len(issues) > 0 {
					summary.Invalid++
					p.report.Issues = append(p.report.Issues, issues...)
				} else {
					summary.Valid++
					p.valid.Adjustments = append(p.valid.Adjustments, a)
				}
			}
		} else {
			for line := markerLine + 1; line <= len(rows); line++ {
				if !blank(rows[line-1]) {
					return nil, ErrWorkbook
				}
			}
		}
		p.report.ValidRows += summary.Valid
		p.report.Sheets = append(p.report.Sheets, summary)
	}
	return p, nil
}

type candidate struct {
	line   int
	code   string
	values map[string]string
	issues []port.KHTCFormExcelIssue
}

func blank(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func issue(sheet string, line int, code, letter, severity, rule, message string) port.KHTCFormExcelIssue {
	return port.KHTCFormExcelIssue{Sheet: sheet, Row: line, ItemCode: code, Column: letter, Severity: severity, Code: rule, Message: message}
}

func (c *Codec) readAdjustment(f *excelize.File, spec sheetSpec, line int) (port.KHTCFormExcelAdjustment, []port.KHTCFormExcelIssue, error) {
	values := make([]string, 4)
	issues := []port.KHTCFormExcelIssue{}
	for i := range values {
		letter, _ := excelize.ColumnNumberToName(i + 1)
		value, formula, err := rawCell(f, spec.def.Name, letter, line)
		if err != nil {
			return port.KHTCFormExcelAdjustment{}, nil, err
		}
		values[i] = strings.TrimSpace(value)
		if formula {
			issues = append(issues, issue(spec.def.Name, line, "", letter, "error", "formula", "Ô điều chỉnh không nhận công thức Excel"))
		}
	}
	code := values[0]
	row, ok := spec.rows[code]
	if !ok {
		code = ""
		issues = append(issues, issue(spec.def.Name, line, "", "A", "error", "item_code", "Chọn mã hạng mục thuộc sheet"))
	}
	if ok && row.AdjustmentLocked {
		issues = append(issues, issue(spec.def.Name, line, code, "A", "error", "adjustment_locked", "Hạng mục do hệ thống cấp số liệu; điều chỉnh tại nguồn"))
	}
	period, err := uuid.Parse(values[1])
	if err != nil || !c.periods[period] {
		issues = append(issues, issue(spec.def.Name, line, code, "B", "error", "period", "Chọn kỳ đã khóa được phép điều chỉnh"))
	}
	if !number.MatchString(values[2]) {
		issues = append(issues, issue(spec.def.Name, line, code, "C", "error", "number", "Số điều chỉnh phải là số thập phân thường có giới hạn chữ số"))
	} else if !strings.ContainsAny(values[2], "123456789") {
		issues = append(issues, issue(spec.def.Name, line, code, "C", "error", "zero", "Số điều chỉnh phải khác 0"))
	}
	if values[3] == "" || utf8.RuneCountInString(values[3]) > 500 {
		issues = append(issues, issue(spec.def.Name, line, code, "D", "error", "reason", "Bắt buộc lý do điều chỉnh, tối đa 500 ký tự"))
	}
	return port.KHTCFormExcelAdjustment{Sheet: spec.def.Key, ItemCode: code, PeriodID: period, Amount: values[2], Reason: values[3]}, issues, nil
}

// GetRows may trim a formula-only cell with no cached value. Structural rows
// therefore inspect formulas too, rather than treating cached emptiness as shape.
func onlyColumns(f *excelize.File, sheet string, line, last int) error {
	for n := last + 1; n <= MaxColumns+1; n++ {
		letter, _ := excelize.ColumnNumberToName(n)
		value, formula, err := rawCell(f, sheet, letter, line)
		if err != nil {
			return err
		}
		if value != "" || formula {
			return ErrWorkbook
		}
	}
	return nil
}
