package form

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formula"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Mã vấn đề tại ô khi tính lưới (docs/errors.md, hợp đồng với P4/web/app).
const (
	IssueRowInvalid     = "reports.row_invalid"
	IssueUnknownColumn  = "reports.cell_unknown_column"
	IssueNotEditable    = "reports.cell_not_editable"
	IssuePeriodClosed   = "reports.cell_period_closed"
	IssueInvalidType    = "reports.cell_invalid_type"
	IssueOutOfRange     = "reports.cell_out_of_range"
	IssueRequired       = "reports.cell_required"
	IssueFormulaError   = "reports.formula_error"
	IssueRuleFailed     = "reports.rule_failed"
	IssueRuleError      = "reports.rule_error"
	IssueDefinitionFail = "reports.form_invalid"
)

// ResultPlaces là số chữ số thập phân lưu cho kết quả công thức.
const ResultPlaces = 10

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// excelEpoch: số ngày Excel tính từ 30/12/1899 (ô ngày dùng trong công thức).
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// rowEnv là môi trường tính một dòng.
type rowEnv struct {
	vals  map[int]formula.Value
	xrefs map[string]map[string]string
}

func (e *rowEnv) Cell(col int) formula.Value { return e.vals[col] }

func (e *rowEnv) XRef(form, letter string) formula.Value {
	s, ok := e.xrefs[form][letter]
	if !ok || s == "" {
		return formula.Value{}
	}
	return parseLoose(s)
}

// parseLoose đọc giá trị hệ thống/liên biểu: số nếu đọc được, không thì chuỗi.
func parseLoose(s string) formula.Value {
	if d, err := decimal.NewFromString(strings.TrimSpace(s)); err == nil {
		return formula.NumberValue(d)
	}
	if dateRe.MatchString(s) {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return formula.NumberValue(decimal.NewFromInt(int64(t.Sub(excelEpoch) / (24 * time.Hour))))
		}
	}
	return formula.TextValue(s)
}

// parseCell đọc giá trị ô theo kiểu dữ liệu của cột; trả giá trị cho công thức, chuỗi chuẩn hóa để lưu.
func parseTyped(col port.FormColumn, raw string) (formula.Value, string, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return formula.Value{}, "", ""
	}
	switch col.DType {
	case port.DTypeNumber, port.DTypeInteger:
		d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
		if err != nil || strings.ContainsAny(s, "eE") {
			return formula.Value{}, "", "phải là số (dùng dấu chấm thập phân)"
		}
		if col.DType == port.DTypeInteger && !d.Equal(d.Truncate(0)) {
			return formula.Value{}, "", "phải là số nguyên"
		}
		return formula.NumberValue(d), d.String(), ""
	case port.DTypeDate:
		t, err := time.Parse("2006-01-02", s)
		if err != nil || !dateRe.MatchString(s) {
			return formula.Value{}, "", "ngày dạng YYYY-MM-DD"
		}
		return formula.NumberValue(decimal.NewFromInt(int64(t.Sub(excelEpoch) / (24 * time.Hour)))), s, ""
	}
	if len([]rune(s)) > 500 {
		return formula.Value{}, "", "tối đa 500 ký tự"
	}
	return formula.TextValue(s), s, ""
}

func outString(v formula.Value) string {
	switch v.Kind {
	case formula.Number:
		return v.Num.Round(ResultPlaces).String()
	case formula.Blank:
		return "0" // ô công thức trỏ ô trống hiển thị 0 như Excel
	}
	return v.String()
}

// Evaluate kiểm và tính một lưới bài nộp theo định nghĩa (FN-XA-03.02): chỉ ô nhập và ô
// của kỳ đang mở được nhập; kiểu dữ liệu, bắt buộc, giới hạn và ràng buộc của mẫu —
// không kiểm gì ngoài yêu cầu của mẫu; công thức tính đúng công thức mẫu.
func Evaluate(def port.FormDefinition, in port.EvalInput) port.EvalResult {
	res := port.EvalResult{Rows: []port.EvalRowResult{}, Issues: []port.CellIssue{}}
	c, err := Compile(def)
	if err != nil {
		res.Issues = append(res.Issues, port.CellIssue{Code: IssueDefinitionFail, Message: err.Error(), Severity: port.SeverityError})
		return res
	}
	return c.Evaluate(in)
}

// Evaluate trên định nghĩa đã biên dịch.
func (c *Compiled) Evaluate(in port.EvalInput) port.EvalResult {
	res := port.EvalResult{Rows: []port.EvalRowResult{}, Issues: []port.CellIssue{}}
	issue := func(row, letter, code, msg, sev string) {
		res.Issues = append(res.Issues, port.CellIssue{RowKey: row, Letter: letter, Code: code, Message: msg, Severity: sev})
	}
	var open map[int]bool
	if in.OpenSlots != nil {
		open = map[int]bool{}
		for _, s := range in.OpenSlots {
			open[s] = true
		}
	}
	seen := map[string]bool{}
	communeNo := map[string]int{}
	for _, row := range in.Rows {
		key := row.Key
		if key == "" {
			key = row.Commune
			if row.Indicator != "" {
				key += "/" + row.Indicator
			}
		}
		if row.Commune == "" || seen[key] {
			issue(key, "", IssueRowInvalid, "dòng thiếu mã xã hoặc trùng khóa", port.SeverityError)
			continue
		}
		seen[key] = true
		if _, ok := communeNo[row.Commune]; !ok {
			communeNo[row.Commune] = len(communeNo) + 1
		}
		var ind port.Indicator
		if len(c.indicators) > 0 {
			var ok bool
			if ind, ok = c.indicators[row.Indicator]; !ok {
				issue(key, "", IssueRowInvalid, "chỉ tiêu không có trong mẫu", port.SeverityError)
				continue
			}
		} else if row.Indicator != "" {
			issue(key, "", IssueRowInvalid, "mẫu không có dòng chỉ tiêu", port.SeverityError)
			continue
		}
		env := &rowEnv{vals: map[int]formula.Value{}, xrefs: row.XRefs}
		cells := map[string]string{}
		// 1. Ô hệ thống: cố định + Tỉnh giao.
		for i, col := range c.Def.Columns {
			ci := i + 1
			var raw string
			switch col.Kind {
			case port.ColumnFixed:
				raw = row.System[col.Letter]
				if raw == "" {
					raw = c.fixedDefault(col, row, ind, communeNo[row.Commune])
				}
			case port.ColumnProvincial:
				raw = row.System[col.Letter]
			default:
				continue
			}
			if raw == "" {
				continue
			}
			v, s, msg := parseTyped(col, raw)
			if msg != "" {
				v, s = parseLoose(raw), raw
			}
			env.vals[ci], cells[col.Letter] = v, s
		}
		// 2. Ô nhập.
		letters := make([]string, 0, len(row.Input))
		for l := range row.Input {
			letters = append(letters, l)
		}
		sort.Strings(letters)
		for _, l := range letters {
			raw := row.Input[l]
			ci, ok := c.idx[strings.ToUpper(l)]
			if !ok {
				issue(key, l, IssueUnknownColumn, "cột không có trong mẫu", port.SeverityError)
				continue
			}
			col := c.Def.Columns[ci-1]
			switch col.Kind {
			case port.ColumnInput:
			case port.ColumnPerPeriod:
				if open != nil && !open[c.slotOf[ci][0]] {
					if strings.TrimSpace(raw) != "" {
						issue(key, col.Letter, IssuePeriodClosed, "chỉ cột của kỳ đang mở được nhập", port.SeverityError)
					}
					continue
				}
			default:
				if strings.TrimSpace(raw) != "" {
					issue(key, col.Letter, IssueNotEditable, "cột cố định, Tỉnh giao hoặc công thức không nhập tay", port.SeverityError)
				}
				continue
			}
			v, s, msg := parseTyped(col, raw)
			if msg != "" {
				issue(key, col.Letter, IssueInvalidType, msg, port.SeverityError)
				continue
			}
			if v.Kind == formula.Number {
				if col.Min != nil && v.Num.LessThan(decimal.RequireFromString(*col.Min)) ||
					col.Max != nil && v.Num.GreaterThan(decimal.RequireFromString(*col.Max)) {
					issue(key, col.Letter, IssueOutOfRange, rangeMsg(col), port.SeverityError)
				}
			}
			if s != "" {
				env.vals[ci], cells[col.Letter] = v, s
			}
		}
		// 3. Bắt buộc (khi gửi): cột nhập, và cột theo kỳ của kỳ đang mở.
		if in.Mode == port.EvalSubmit {
			for i, col := range c.Def.Columns {
				if !col.Required || cells[col.Letter] != "" {
					continue
				}
				if col.Kind == port.ColumnInput || col.Kind == port.ColumnPerPeriod && (open == nil || open[c.slotOf[i+1][0]]) {
					issue(key, col.Letter, IssueRequired, "bắt buộc nhập", port.SeverityError)
				}
			}
		}
		// 4. Công thức theo thứ tự phụ thuộc.
		for _, ci := range c.order {
			v := c.progs[ci].Run(env)
			if v.Kind == formula.Blank {
				v = formula.NumberValue(decimal.Zero)
			}
			env.vals[ci] = v
			col := c.Def.Columns[ci-1]
			cells[col.Letter] = outString(v)
			if v.Kind == formula.Error {
				issue(key, col.Letter, IssueFormulaError, "công thức "+col.Formula+" cho "+v.Err, port.SeverityWarning)
			}
		}
		// 5. Ràng buộc của mẫu.
		for _, r := range c.rules {
			if r.when != nil {
				w := r.when.Run(env)
				if ok, _ := truthy(w); !ok {
					continue
				}
			}
			v := r.expr.Run(env)
			ok, isErr := truthy(v)
			if isErr {
				res.Issues = append(res.Issues, port.CellIssue{RowKey: key, Letter: firstLetter(r.r.Letters), Code: IssueRuleError, Message: r.r.Message + " (không tính được: " + v.String() + ")", Severity: port.SeverityWarning, RuleID: r.r.ID})
				continue
			}
			if !ok {
				if len(r.r.Letters) == 0 {
					res.Issues = append(res.Issues, port.CellIssue{RowKey: key, Code: IssueRuleFailed, Message: r.r.Message, Severity: r.r.Severity, RuleID: r.r.ID})
				}
				for _, l := range r.r.Letters {
					res.Issues = append(res.Issues, port.CellIssue{RowKey: key, Letter: strings.ToUpper(l), Code: IssueRuleFailed, Message: r.r.Message, Severity: r.r.Severity, RuleID: r.r.ID})
				}
			}
		}
		res.Rows = append(res.Rows, port.EvalRowResult{Key: key, Commune: row.Commune, Region: row.Region, Indicator: row.Indicator, Cells: cells})
	}
	res.Valid = !slices.ContainsFunc(res.Issues, func(i port.CellIssue) bool { return i.Severity == port.SeverityError })
	return res
}

func firstLetter(ls []string) string {
	if len(ls) == 0 {
		return ""
	}
	return strings.ToUpper(ls[0])
}

func truthy(v formula.Value) (ok, isErr bool) {
	switch v.Kind {
	case formula.Error:
		return false, true
	case formula.Boolean:
		return v.Bool, false
	case formula.Number:
		return !v.Num.IsZero(), false
	case formula.Blank:
		return false, false
	}
	return false, true
}

func rangeMsg(col port.FormColumn) string {
	switch {
	case col.Min != nil && col.Max != nil:
		return fmt.Sprintf("trong khoảng %s–%s", *col.Min, *col.Max)
	case col.Min != nil:
		return "không nhỏ hơn " + *col.Min
	}
	return "không lớn hơn " + *col.Max
}

// fixedDefault điền cột cố định khi P4 không gửi giá trị hệ thống.
func (c *Compiled) fixedDefault(col port.FormColumn, row port.EvalRow, ind port.Indicator, no int) string {
	switch {
	case col.FixedSource == port.FixedRowNo:
		return fmt.Sprint(no)
	case col.FixedSource == port.FixedIndicatorLabel:
		return ind.Label
	case col.FixedSource == port.FixedCommuneName:
		return row.Commune
	case col.FixedSource == port.FixedRegionName:
		return row.Region
	case strings.HasPrefix(col.FixedSource, port.FixedXRefPrefix):
		parts := strings.Split(strings.TrimPrefix(col.FixedSource, port.FixedXRefPrefix), ":")
		if len(parts) == 2 {
			return row.XRefs[parts[0]][strings.ToUpper(parts[1])]
		}
	}
	return ""
}

// Totals dựng dòng tổng theo định nghĩa: cột số (nhập, theo kỳ, Tỉnh giao) cộng các dòng
// thành viên; cột công thức mặc định tính lại trên dòng tổng (năng suất bình quân gia quyền
// = tổng sản lượng / tổng diện tích); cột chữ, ngày, danh mục để trống; nhãn ở label_letter.
// Mẫu có dòng chỉ tiêu: tổng theo từng chỉ tiêu.
func Totals(def port.FormDefinition, rows []port.EvalRowResult) []port.TotalRowResult {
	c, err := Compile(def)
	if err != nil {
		return nil
	}
	return c.Totals(rows)
}

// Totals trên định nghĩa đã biên dịch.
func (c *Compiled) Totals(rows []port.EvalRowResult) []port.TotalRowResult {
	var out []port.TotalRowResult
	inds := []string{""}
	if len(c.Def.Rows.Indicators) > 0 {
		inds = inds[:0]
		for _, in := range c.Def.Rows.Indicators {
			inds = append(inds, in.Code)
		}
	}
	for _, t := range c.Def.Totals {
		switch t.Kind {
		case port.TotalRegion:
			regions := []string{}
			for _, r := range rows {
				if r.Region != "" && !slices.Contains(regions, r.Region) {
					regions = append(regions, r.Region)
				}
			}
			sort.Strings(regions)
			for _, reg := range regions {
				for _, ind := range inds {
					members := filterRows(rows, reg, ind)
					key := "region:" + reg
					if ind != "" {
						key += "/" + ind
					}
					out = append(out, c.totalRow(t, key, reg, ind, members))
				}
			}
		case port.TotalProvince:
			for _, ind := range inds {
				key := "province"
				if ind != "" {
					key += "/" + ind
				}
				out = append(out, c.totalRow(t, key, "", ind, filterRows(rows, "", ind)))
			}
		}
	}
	return out
}

func filterRows(rows []port.EvalRowResult, region, ind string) []port.EvalRowResult {
	var out []port.EvalRowResult
	for _, r := range rows {
		if (region == "" || r.Region == region) && r.Indicator == ind {
			out = append(out, r)
		}
	}
	return out
}

func (c *Compiled) totalRow(t port.TotalRowDef, key, region, ind string, members []port.EvalRowResult) port.TotalRowResult {
	cells := map[string]string{}
	env := &rowEnv{vals: map[int]formula.Value{}}
	for i, col := range c.Def.Columns {
		ci := i + 1
		mode := col.Total
		if mode == "" {
			mode = port.TotalSum
			if col.Kind == port.ColumnFormula {
				mode = port.TotalFormula
			}
		}
		if mode != port.TotalSum || !numericDType(col.DType) && col.Kind != port.ColumnFormula || col.Kind == port.ColumnFixed {
			continue
		}
		sum, any := decimal.Zero, false
		for _, m := range members {
			if d, err := decimal.NewFromString(m.Cells[col.Letter]); err == nil {
				sum, any = sum.Add(d), true
			}
		}
		if any {
			env.vals[ci], cells[col.Letter] = formula.NumberValue(sum), sum.String()
		}
	}
	for _, ci := range c.order {
		col := c.Def.Columns[ci-1]
		if col.Total != "" && col.Total != port.TotalFormula {
			continue
		}
		v := c.progs[ci].Run(env)
		env.vals[ci] = v
		cells[col.Letter] = outString(v)
	}
	label := strings.ReplaceAll(t.Label, "{region}", region)
	if ind != "" {
		if in, ok := c.indicators[ind]; ok {
			label += " – " + in.Label
		}
	}
	cells[strings.ToUpper(t.LabelLetter)] = label
	return port.TotalRowResult{Key: key, Kind: t.Kind, Region: region, Indicator: ind, Members: len(members), Cells: cells}
}
