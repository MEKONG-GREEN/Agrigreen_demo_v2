// Package form kiểm cấu trúc và tính lưới theo định nghĩa do ứng dụng cung cấp:
// ô nhập/công thức/ràng buộc, dòng tổng và thao tác thiết kế trên bản sao.
//
// Thuần (không DB, không đồng hồ). Mọi số là decimal (R-10, archlint agnofloat).
package form

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formula"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Mã lỗi kiểm cấu trúc (docs/errors.md).
const (
	CodeInvalid           = "reports.form_invalid"
	CodeFormulaInvalid    = "reports.formula_invalid"
	CodeFormulaInvalidRef = "reports.formula_invalid_ref"
	CodeFormulaCycle      = "reports.formula_cycle"
)

// Issue là một lỗi cấu trúc tại một trường của định nghĩa.
type Issue struct {
	Field string
	Code  string
	Msg   string
}

// ValidationError gom lỗi cấu trúc.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		parts = append(parts, i.Field+": "+i.Msg)
	}
	return "định nghĩa mẫu không hợp lệ: " + strings.Join(parts, "; ")
}

// Code trả mã lỗi chính: ưu tiên tham chiếu sai, rồi công thức, rồi cấu trúc.
func (e *ValidationError) Code() string {
	for _, want := range []string{CodeFormulaInvalidRef, CodeFormulaCycle, CodeFormulaInvalid} {
		for _, i := range e.Issues {
			if i.Code == want {
				return want
			}
		}
	}
	return CodeInvalid
}

// Giới hạn của mẫu.
const (
	MaxColumns    = 2000
	MaxHeaderRows = 20
	MaxIndicators = 500
	MaxRules      = 200
	maxText       = 4000
)

var codeRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_.-]{1,31}$`)

// ValidCode cho biết mã mẫu hợp lệ (vd T01, M01, CC01).
func ValidCode(c string) bool { return codeRe.MatchString(c) }

// Compiled là định nghĩa đã kiểm và phân tích công thức.
type Compiled struct {
	Def        port.FormDefinition
	idx        map[string]int // chữ cái → cột (từ 1)
	progs      map[int]*formula.Program
	order      []int // thứ tự tính cột công thức (phụ thuộc trước)
	rules      []compiledRule
	indicators map[string]port.Indicator
	slotOf     map[int][2]int // cột → (khe kỳ, vị trí trong khối)
}

type compiledRule struct {
	r    port.FormRule
	expr *formula.Program
	when *formula.Program
}

// Column trả cột theo chữ cái.
func (c *Compiled) Column(letter string) (port.FormColumn, bool) {
	i, ok := c.idx[strings.ToUpper(letter)]
	if !ok {
		return port.FormColumn{}, false
	}
	return c.Def.Columns[i-1], true
}

type checker struct{ issues []Issue }

func (k *checker) add(field, code, msg string, args ...any) {
	k.issues = append(k.issues, Issue{Field: field, Code: code, Msg: fmt.Sprintf(msg, args...)})
}

// Validate kiểm cấu trúc định nghĩa; nil = hợp lệ.
func Validate(def port.FormDefinition) error {
	_, err := Compile(def)
	return err
}

// Compile kiểm và phân tích định nghĩa.
func Compile(def port.FormDefinition) (*Compiled, error) {
	k := &checker{}
	c := &Compiled{Def: def, idx: map[string]int{}, progs: map[int]*formula.Program{}, indicators: map[string]port.Indicator{}, slotOf: map[int][2]int{}}
	if def.Schema != port.FormSchema {
		k.add("schema", CodeInvalid, "phải là %q", port.FormSchema)
	}
	if !ValidCode(def.Code) {
		k.add("code", CodeInvalid, "mã mẫu chữ hoa, số, . _ -; 2–32 ký tự (vd T01)")
	}
	if n := len([]rune(strings.TrimSpace(def.Title))); n < 1 || n > 300 {
		k.add("title", CodeInvalid, "bắt buộc, tối đa 300 ký tự")
	}
	if len([]rune(def.SheetName)) > 100 {
		k.add("sheet_name", CodeInvalid, "tối đa 100 ký tự")
	}
	if len([]rune(def.Requirements)) > maxText {
		k.add("requirements", CodeInvalid, "tối đa %d ký tự", maxText)
	}
	switch def.Completeness {
	case port.CompletenessComplete, port.CompletenessPartial, port.CompletenessPlaceholder:
	default:
		k.add("completeness", CodeInvalid, "complete | partial | placeholder")
	}
	n := len(def.Columns)
	if n < 1 || n > MaxColumns {
		k.add("columns", CodeInvalid, "cần 1–%d cột", MaxColumns)
	}
	for i, col := range def.Columns {
		f := fmt.Sprintf("columns[%d]", i)
		want := formula.ColumnLetter(i + 1)
		if col.Letter != want {
			k.add(f+".letter", CodeInvalid, "cột phải liên tục từ A: muốn %s, nhận %q", want, col.Letter)
			continue
		}
		c.idx[col.Letter] = i + 1
	}
	if len(k.issues) > 0 {
		return nil, &ValidationError{k.issues}
	}
	c.checkColumns(k)
	c.checkPeriodBlocks(k)
	c.checkHeader(k)
	c.checkRows(k)
	c.checkRules(k)
	c.order = c.topo(k)
	if len(k.issues) > 0 {
		return nil, &ValidationError{k.issues}
	}
	return c, nil
}

func validDType(d string) bool {
	return slices.Contains([]string{port.DTypeNumber, port.DTypeInteger, port.DTypeText, port.DTypeDate, port.DTypeCatalog}, d)
}

func numericDType(d string) bool { return d == port.DTypeNumber || d == port.DTypeInteger }

func (c *Compiled) letterOK(l string) bool { _, ok := c.idx[strings.ToUpper(l)]; return ok }

func (c *Compiled) checkColumns(k *checker) {
	groups := map[string]bool{}
	for _, g := range c.Def.Groups {
		groups[g.ID] = true
	}
	for i, col := range c.Def.Columns {
		f := fmt.Sprintf("columns[%d]", i)
		if n := len([]rune(col.Title)); n > 500 {
			k.add(f+".title", CodeInvalid, "tối đa 500 ký tự")
		}
		if !validDType(col.DType) {
			k.add(f+".dtype", CodeInvalid, "number | integer | text | date | catalog")
		}
		if col.DType == port.DTypeCatalog && strings.TrimSpace(col.Catalog) == "" {
			k.add(f+".catalog", CodeInvalid, "cột danh mục cần mã danh mục")
		}
		switch col.Total {
		case "", port.TotalSum, port.TotalFormula, port.TotalNone:
		default:
			k.add(f+".total", CodeInvalid, "sum | formula | none")
		}
		for name, p := range map[string]*string{"min": col.Min, "max": col.Max} {
			if p == nil {
				continue
			}
			if _, err := decimal.NewFromString(*p); err != nil || !numericDType(col.DType) {
				k.add(f+"."+name, CodeInvalid, "giới hạn là số, chỉ cho cột số")
			}
		}
		if col.Decimals != nil && (*col.Decimals < 0 || *col.Decimals > 10) {
			k.add(f+".decimals", CodeInvalid, "0–10")
		}
		if col.Group != "" && !groups[col.Group] {
			k.add(f+".group", CodeInvalid, "nhóm %q không có trong groups", col.Group)
		}
		if col.Kind != port.ColumnFormula && col.Formula != "" {
			k.add(f+".formula", CodeInvalid, "chỉ cột loại công thức có công thức")
		}
		if col.Kind != port.ColumnFixed && col.FixedSource != "" {
			k.add(f+".fixed_source", CodeInvalid, "chỉ cột cố định có nguồn hệ thống")
		}
		inBlock := col.PeriodBlock != "" || col.PeriodSlot != 0
		if inBlock && col.Kind != port.ColumnPerPeriod && col.Kind != port.ColumnFormula {
			k.add(f+".period_block", CodeInvalid, "chỉ cột theo kỳ (hoặc công thức trong khối kỳ) thuộc khối kỳ")
		}
		if inBlock && (col.PeriodBlock == "" || col.PeriodSlot < 1) {
			k.add(f+".period_slot", CodeInvalid, "cột trong khối kỳ cần period_block và period_slot ≥ 1")
		}
		switch col.Kind {
		case port.ColumnFixed:
			src := col.FixedSource
			switch {
			case src == port.FixedRowNo, src == port.FixedRegionName, src == port.FixedCommuneName, src == port.FixedIndicatorLabel, src == port.FixedNone:
			case strings.HasPrefix(src, port.FixedXRefPrefix):
				parts := strings.Split(strings.TrimPrefix(src, port.FixedXRefPrefix), ":")
				if len(parts) != 2 || !ValidCode(parts[0]) || formula.ColumnIndex(parts[1]) == 0 {
					k.add(f+".fixed_source", CodeInvalid, "dạng xref:<mã mẫu>:<cột>, vd xref:T01:M")
				}
			default:
				k.add(f+".fixed_source", CodeInvalid, "row_no | region_name | commune_name | indicator_label | none | xref:<mẫu>:<cột>")
			}
		case port.ColumnProvincial, port.ColumnInput:
		case port.ColumnPerPeriod:
			if !inBlock {
				k.add(f+".period_slot", CodeInvalid, "cột theo kỳ cần period_block và period_slot ≥ 1")
			}
		case port.ColumnFormula:
			p, err := formula.Compile(col.Formula)
			if err != nil {
				k.add(f+".formula", CodeFormulaInvalid, "%v", err)
				continue
			}
			if formula.UsesXRef(p.Root) {
				k.add(f+".formula", CodeFormulaInvalid, "XREF chỉ dùng trong ràng buộc của mẫu, không trong công thức cột")
				continue
			}
			bad := false
			for _, r := range formula.Refs(p.Root) {
				if r > len(c.Def.Columns) {
					k.add(f+".formula", CodeFormulaInvalidRef, "tham chiếu ô không tồn tại: cột %s", formula.ColumnLetter(r))
					bad = true
					break
				}
			}
			if !bad {
				c.progs[i+1] = p
			}
		default:
			k.add(f+".kind", CodeInvalid, "fixed | provincial | input | per_period | formula")
		}
	}
	gids := map[string]bool{}
	for i, g := range c.Def.Groups {
		f := fmt.Sprintf("groups[%d]", i)
		if g.ID == "" || gids[g.ID] {
			k.add(f+".id", CodeInvalid, "id bắt buộc, không trùng")
		}
		gids[g.ID] = true
		if !c.letterOK(g.From) || !c.letterOK(g.To) || formula.ColumnIndex(g.From) > formula.ColumnIndex(g.To) {
			k.add(f, CodeInvalid, "from ≤ to là cột có trong mẫu")
		}
	}
	if c.Def.FreezeThrough != "" && !c.letterOK(c.Def.FreezeThrough) {
		k.add("freeze_through", CodeInvalid, "cột không tồn tại")
	}
}

func (c *Compiled) checkPeriodBlocks(k *checker) {
	ids := map[string]port.PeriodBlock{}
	for i, b := range c.Def.PeriodBlocks {
		f := fmt.Sprintf("period_blocks[%d]", i)
		if b.ID == "" || ids[b.ID].ID != "" {
			k.add(f+".id", CodeInvalid, "id bắt buộc, không trùng")
			continue
		}
		ids[b.ID] = b
		first := formula.ColumnIndex(b.FirstLetter)
		if first == 0 || b.Width < 1 || b.Slots < 1 || first+b.Width*b.Slots-1 > len(c.Def.Columns) {
			k.add(f, CodeInvalid, "first_letter, width ≥ 1, slots ≥ 1 phải nằm trong các cột của mẫu")
			continue
		}
		for j := 0; j < b.Width*b.Slots; j++ {
			col := c.Def.Columns[first-1+j]
			slot := 1 + j/b.Width
			if col.Kind != port.ColumnPerPeriod && col.Kind != port.ColumnFormula || col.PeriodBlock != b.ID || col.PeriodSlot != slot {
				k.add(f, CodeInvalid, "cột %s phải là cột theo kỳ (hoặc công thức) của khối %s, kỳ %d", col.Letter, b.ID, slot)
				break
			}
			c.slotOf[first+j] = [2]int{slot, j % b.Width}
		}
	}
	for i, col := range c.Def.Columns {
		if col.Kind == port.ColumnPerPeriod || col.PeriodBlock != "" {
			if _, ok := c.slotOf[i+1]; !ok {
				k.add(fmt.Sprintf("columns[%d].period_block", i), CodeInvalid, "cột theo kỳ không thuộc khối kỳ nào hợp lệ")
			}
		}
	}
}

// Rect là một vùng gộp (cột, dòng từ 1, bao gồm).
type Rect struct{ C1, R1, C2, R2 int }

var cellRe = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]*)$`)

func parseCell(s string) (int, int, bool) {
	m := cellRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return 0, 0, false
	}
	col := formula.ColumnIndex(m[1])
	row, err := strconv.Atoi(m[2])
	if col == 0 || err != nil {
		return 0, 0, false
	}
	return col, row, true
}

// ParseRect đọc vùng "F1:L1".
func ParseRect(s string) (Rect, error) {
	a, b, ok := strings.Cut(s, ":")
	if !ok {
		return Rect{}, errors.New("vùng gộp dạng A1:B2")
	}
	c1, r1, ok1 := parseCell(a)
	c2, r2, ok2 := parseCell(b)
	if !ok1 || !ok2 {
		return Rect{}, errors.New("vùng gộp dạng A1:B2")
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	return Rect{c1, r1, c2, r2}, nil
}

// String in vùng.
func (r Rect) String() string {
	return fmt.Sprintf("%s%d:%s%d", formula.ColumnLetter(r.C1), r.R1, formula.ColumnLetter(r.C2), r.R2)
}

func (r Rect) overlaps(o Rect) bool {
	return r.C1 <= o.C2 && o.C1 <= r.C2 && r.R1 <= o.R2 && o.R1 <= r.R2
}

func (c *Compiled) checkHeader(k *checker) {
	h := c.Def.Header
	if h.Rows < 1 || h.Rows > MaxHeaderRows {
		k.add("header.rows", CodeInvalid, "1–%d dòng tiêu đề", MaxHeaderRows)
		return
	}
	seen := map[[2]int]bool{}
	for i, cell := range h.Cells {
		f := fmt.Sprintf("header.cells[%d]", i)
		col := formula.ColumnIndex(cell.Letter)
		if cell.Row < 1 || cell.Row > h.Rows || col == 0 || col > len(c.Def.Columns) {
			k.add(f, CodeInvalid, "ô tiêu đề ngoài khung (dòng 1–%d, cột A–%s)", h.Rows, formula.ColumnLetter(len(c.Def.Columns)))
			continue
		}
		if seen[[2]int{col, cell.Row}] {
			k.add(f, CodeInvalid, "trùng ô %s%d", cell.Letter, cell.Row)
		}
		seen[[2]int{col, cell.Row}] = true
		if len([]rune(cell.Text)) > 500 {
			k.add(f+".text", CodeInvalid, "tối đa 500 ký tự")
		}
	}
	var rects []Rect
	for i, m := range h.Merges {
		f := fmt.Sprintf("header.merges[%d]", i)
		r, err := ParseRect(m)
		if err != nil {
			k.add(f, CodeInvalid, "%v", err)
			continue
		}
		if r.R2 > h.Rows || r.C2 > len(c.Def.Columns) {
			k.add(f, CodeInvalid, "vùng gộp %s ngoài khung tiêu đề", m)
			continue
		}
		if r.C1 == r.C2 && r.R1 == r.R2 {
			k.add(f, CodeInvalid, "vùng gộp %s chỉ có một ô", m)
			continue
		}
		for _, o := range rects {
			if r.overlaps(o) {
				k.add(f, CodeInvalid, "vùng gộp %s chồng lên %s", m, o)
			}
		}
		rects = append(rects, r)
	}
}

func (c *Compiled) checkRows(k *checker) {
	if c.Def.Rows.Kind != port.RowsPerCommune {
		k.add("rows.kind", CodeInvalid, "per_commune")
	}
	if len(c.Def.Rows.Indicators) > MaxIndicators {
		k.add("rows.indicators", CodeInvalid, "tối đa %d chỉ tiêu", MaxIndicators)
	}
	for i, in := range c.Def.Rows.Indicators {
		f := fmt.Sprintf("rows.indicators[%d]", i)
		if in.Code == "" || len(in.Code) > 64 || strings.ContainsAny(in.Code, "/ ") {
			k.add(f+".code", CodeInvalid, "mã bắt buộc, ≤ 64 ký tự, không có / hay khoảng trắng")
			continue
		}
		if _, dup := c.indicators[in.Code]; dup {
			k.add(f+".code", CodeInvalid, "trùng mã %s", in.Code)
		}
		c.indicators[in.Code] = in
	}
	for i, t := range c.Def.Totals {
		f := fmt.Sprintf("totals[%d]", i)
		if t.Kind != port.TotalRegion && t.Kind != port.TotalProvince {
			k.add(f+".kind", CodeInvalid, "region | province")
		}
		if !c.letterOK(t.LabelLetter) {
			k.add(f+".label_letter", CodeInvalid, "cột không tồn tại")
		}
	}
}

func (c *Compiled) checkRules(k *checker) {
	if len(c.Def.Rules) > MaxRules {
		k.add("rules", CodeInvalid, "tối đa %d ràng buộc", MaxRules)
	}
	ids := map[string]bool{}
	for i, r := range c.Def.Rules {
		f := fmt.Sprintf("rules[%d]", i)
		if r.ID == "" || ids[r.ID] {
			k.add(f+".id", CodeInvalid, "id bắt buộc, không trùng")
		}
		ids[r.ID] = true
		if r.Severity != port.SeverityError && r.Severity != port.SeverityWarning {
			k.add(f+".severity", CodeInvalid, "error | warning")
		}
		if strings.TrimSpace(r.Message) == "" {
			k.add(f+".message", CodeInvalid, "bắt buộc")
		}
		cr := compiledRule{r: r}
		for name, src := range map[string]string{"expr": r.Expr, "when": r.When} {
			if src == "" && name == "when" {
				continue
			}
			p, err := formula.Compile(src)
			if err != nil {
				k.add(f+"."+name, CodeFormulaInvalid, "%v", err)
				continue
			}
			for _, ref := range formula.Refs(p.Root) {
				if ref > len(c.Def.Columns) {
					k.add(f+"."+name, CodeFormulaInvalidRef, "tham chiếu ô không tồn tại: cột %s", formula.ColumnLetter(ref))
				}
			}
			if name == "expr" {
				cr.expr = p
			} else {
				cr.when = p
			}
		}
		for _, l := range r.Letters {
			if !c.letterOK(l) {
				k.add(f+".letters", CodeInvalid, "cột %q không tồn tại", l)
			}
		}
		if cr.expr != nil {
			c.rules = append(c.rules, cr)
		}
	}
}

// topo sắp cột công thức theo phụ thuộc; vòng lặp → reports.formula_cycle.
func (c *Compiled) topo(k *checker) []int {
	state := map[int]int{} // 0 chưa, 1 đang, 2 xong
	var order []int
	var visit func(col int) bool
	visit = func(col int) bool {
		switch state[col] {
		case 1:
			return false
		case 2:
			return true
		}
		state[col] = 1
		for _, r := range formula.Refs(c.progs[col].Root) {
			if _, isF := c.progs[r]; isF {
				if !visit(r) {
					return false
				}
			}
		}
		state[col] = 2
		order = append(order, col)
		return true
	}
	cols := make([]int, 0, len(c.progs))
	for col := range c.progs {
		cols = append(cols, col)
	}
	slices.Sort(cols)
	for _, col := range cols {
		if !visit(col) {
			k.add(fmt.Sprintf("columns[%d].formula", col-1), CodeFormulaCycle, "công thức cột %s tham chiếu vòng", formula.ColumnLetter(col))
			return nil
		}
	}
	return order
}
