package form

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formula"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Thao tác của trình thiết kế (FN-AD-04.06 B2–B4). Mỗi thao tác chạy trên bản sao;
// lỗi ở thao tác nào thì cả lô bị từ chối (không lưu nửa chừng).
const (
	OpInsertColumn    = "insert_column"     // {at, side: before|after, column}
	OpDeleteColumn    = "delete_column"     // {letter}
	OpUpdateColumn    = "update_column"     // {letter, column} — giữ chữ cái
	OpInsertHeaderRow = "insert_header_row" // {row, side}
	OpDeleteHeaderRow = "delete_header_row" // {row}
	OpSetHeaderCell   = "set_header_cell"   // {row, letter, text}
	OpMerge           = "merge"             // {range}
	OpUnmerge         = "unmerge"           // {range}
	OpAddIndicator    = "add_indicator"     // {indicator, after?}
	OpDeleteIndicator = "delete_indicator"  // {code}
	OpSetRule         = "set_rule"          // {rule} — thêm hoặc thay theo id
	OpDeleteRule      = "delete_rule"       // {id}
	OpSetMeta         = "set_meta"          // {meta}
	OpSetGroups       = "set_groups"        // {groups}
	OpSetTotals       = "set_totals"        // {totals}
	OpSetPeriodBlock  = "set_period_block"  // {block} — thêm hoặc thay theo id (đổi tiêu đề mẫu)
)

// Op là một thao tác lưới.
type Op struct {
	Op        string             `json:"op"`
	At        string             `json:"at,omitempty"`
	Side      string             `json:"side,omitempty"` // before | after
	Letter    string             `json:"letter,omitempty"`
	Row       int                `json:"row,omitempty"`
	Text      string             `json:"text,omitempty"`
	Range     string             `json:"range,omitempty"`
	Code      string             `json:"code,omitempty"`
	ID        string             `json:"id,omitempty"`
	After     string             `json:"after,omitempty"`
	Column    *port.FormColumn   `json:"column,omitempty"`
	Indicator *port.Indicator    `json:"indicator,omitempty"`
	Rule      *port.FormRule     `json:"rule,omitempty"`
	Meta      *Meta              `json:"meta,omitempty"`
	Groups    []port.ColumnGroup `json:"groups,omitempty"`
	Totals    []port.TotalRowDef `json:"totals,omitempty"`
	Block     *port.PeriodBlock  `json:"block,omitempty"`
}

// Meta là thuộc tính mẫu sửa được qua set_meta (nil = giữ).
type Meta struct {
	Title         *string             `json:"title,omitempty"`
	SheetName     *string             `json:"sheet_name,omitempty"`
	Requirements  *string             `json:"requirements,omitempty"`
	FreezeThrough *string             `json:"freeze_through,omitempty"`
	Completeness  *string             `json:"completeness,omitempty"`
	Pending       *[]port.PendingItem `json:"pending,omitempty"`
}

// OpError là lỗi của một thao tác (vị trí trong lô).
type OpError struct {
	Index int
	Code  string
	Msg   string
}

func (e *OpError) Error() string { return fmt.Sprintf("thao tác %d: %s", e.Index, e.Msg) }

// Clone sao sâu định nghĩa.
func Clone(def port.FormDefinition) port.FormDefinition {
	b, _ := json.Marshal(def)
	var out port.FormDefinition
	_ = json.Unmarshal(b, &out)
	return out
}

// Apply áp lô thao tác rồi kiểm cấu trúc kết quả.
func Apply(def port.FormDefinition, ops []Op) (port.FormDefinition, error) {
	d := Clone(def)
	for i, op := range ops {
		var err error
		switch op.Op {
		case OpInsertColumn:
			err = insertColumn(&d, op)
		case OpDeleteColumn:
			err = deleteColumn(&d, op.Letter)
		case OpUpdateColumn:
			err = updateColumn(&d, op)
		case OpInsertHeaderRow:
			err = insertHeaderRow(&d, op)
		case OpDeleteHeaderRow:
			err = deleteHeaderRow(&d, op.Row)
		case OpSetHeaderCell:
			err = setHeaderCell(&d, op)
		case OpMerge:
			err = merge(&d, op.Range)
		case OpUnmerge:
			err = unmerge(&d, op.Range)
		case OpAddIndicator:
			err = addIndicator(&d, op)
		case OpDeleteIndicator:
			idx := slices.IndexFunc(d.Rows.Indicators, func(x port.Indicator) bool { return x.Code == op.Code })
			if idx < 0 {
				err = errors.New("không có chỉ tiêu " + op.Code)
			} else {
				d.Rows.Indicators = slices.Delete(d.Rows.Indicators, idx, idx+1)
			}
		case OpSetRule:
			if op.Rule == nil {
				err = errors.New("thiếu rule")
				break
			}
			if idx := slices.IndexFunc(d.Rules, func(r port.FormRule) bool { return r.ID == op.Rule.ID }); idx >= 0 {
				d.Rules[idx] = *op.Rule
			} else {
				d.Rules = append(d.Rules, *op.Rule)
			}
		case OpDeleteRule:
			idx := slices.IndexFunc(d.Rules, func(r port.FormRule) bool { return r.ID == op.ID })
			if idx < 0 {
				err = errors.New("không có ràng buộc " + op.ID)
			} else {
				d.Rules = slices.Delete(d.Rules, idx, idx+1)
			}
		case OpSetMeta:
			err = setMeta(&d, op.Meta)
		case OpSetGroups:
			d.Groups = append([]port.ColumnGroup{}, op.Groups...)
		case OpSetTotals:
			d.Totals = append([]port.TotalRowDef{}, op.Totals...)
		case OpSetPeriodBlock:
			if op.Block == nil {
				err = errors.New("thiếu block")
				break
			}
			if idx := slices.IndexFunc(d.PeriodBlocks, func(b port.PeriodBlock) bool { return b.ID == op.Block.ID }); idx >= 0 {
				d.PeriodBlocks[idx] = *op.Block
			} else {
				d.PeriodBlocks = append(d.PeriodBlocks, *op.Block)
			}
		default:
			err = errors.New("thao tác không hỗ trợ " + op.Op)
		}
		if err != nil {
			var oe *OpError
			if errors.As(err, &oe) {
				oe.Index = i
				return def, oe
			}
			code := CodeInvalid
			if errors.Is(err, formula.ErrRefDeleted) {
				code = CodeFormulaInvalidRef
			}
			return def, &OpError{Index: i, Code: code, Msg: err.Error()}
		}
	}
	if err := Validate(d); err != nil {
		return def, err
	}
	return d, nil
}

func colIndex(d *port.FormDefinition, letter string) (int, error) {
	i := formula.ColumnIndex(letter)
	if i == 0 || i > len(d.Columns) {
		return 0, fmt.Errorf("cột %q không tồn tại", letter)
	}
	return i, nil
}

// shiftAll dời mọi tham chiếu khi chèn (delta = +1) hay xóa (delta = -1) cột at.
func shiftAll(d *port.FormDefinition, at, delta int) error {
	for i := range d.Columns {
		if d.Columns[i].Formula != "" {
			s, err := formula.ShiftString(d.Columns[i].Formula, at, delta)
			if err != nil {
				return fmt.Errorf("công thức cột %s: %w", d.Columns[i].Letter, err)
			}
			d.Columns[i].Formula = s
		}
		// fixed_source "xref:T01:M" trỏ cột của mẫu khác: không dời.
	}
	for i := range d.Rules {
		for _, p := range []*string{&d.Rules[i].Expr, &d.Rules[i].When} {
			if *p == "" {
				continue
			}
			s, err := formula.ShiftString(*p, at, delta)
			if err != nil {
				return fmt.Errorf("ràng buộc %s: %w", d.Rules[i].ID, err)
			}
			*p = s
		}
		var ls []string
		for _, l := range d.Rules[i].Letters {
			if c, ok := shiftLetter(l, at, delta); ok {
				ls = append(ls, c)
			}
		}
		d.Rules[i].Letters = ls
	}
	var cells []port.HeaderCell
	for _, hc := range d.Header.Cells {
		if c, ok := shiftLetter(hc.Letter, at, delta); ok {
			hc.Letter = c
			cells = append(cells, hc)
		}
	}
	d.Header.Cells = cells
	var merges []string
	for _, m := range d.Header.Merges {
		r, err := ParseRect(m)
		if err != nil {
			return err
		}
		r.C1, r.C2 = shiftSpan(r.C1, r.C2, at, delta)
		if r.C1 > r.C2 || r.C1 == r.C2 && r.R1 == r.R2 {
			continue // vùng gộp mất hoặc còn một ô
		}
		merges = append(merges, r.String())
	}
	d.Header.Merges = merges
	var groups []port.ColumnGroup
	for _, g := range d.Groups {
		f, t := shiftSpan(formula.ColumnIndex(g.From), formula.ColumnIndex(g.To), at, delta)
		if f > t {
			continue
		}
		g.From, g.To = formula.ColumnLetter(f), formula.ColumnLetter(t)
		groups = append(groups, g)
	}
	d.Groups = groups
	for i := range d.PeriodBlocks {
		f := formula.ColumnIndex(d.PeriodBlocks[i].FirstLetter)
		if f >= at && (delta > 0 || f > at) {
			d.PeriodBlocks[i].FirstLetter = formula.ColumnLetter(f + delta)
		}
	}
	for i := range d.Totals {
		if c, ok := shiftLetter(d.Totals[i].LabelLetter, at, delta); ok {
			d.Totals[i].LabelLetter = c
		}
	}
	if d.FreezeThrough != "" {
		f := formula.ColumnIndex(d.FreezeThrough)
		switch {
		case delta > 0 && f >= at, delta < 0 && f > at:
			f += delta
		case delta < 0 && f == at:
			f--
		}
		d.FreezeThrough = formula.ColumnLetter(f)
	}
	return nil
}

func shiftLetter(l string, at, delta int) (string, bool) {
	c := formula.ColumnIndex(l)
	if delta < 0 {
		if c == at {
			return "", false
		}
		if c > at {
			c--
		}
		return formula.ColumnLetter(c), true
	}
	if c >= at {
		c += delta
	}
	return formula.ColumnLetter(c), true
}

// shiftSpan dời một khoảng cột như dải công thức (nở khi chèn giữa, co khi xóa).
func shiftSpan(from, to, at, delta int) (int, int) {
	if delta < 0 {
		if from > at {
			from--
		}
		if to >= at {
			to--
		}
		return from, to
	}
	if from >= at {
		from += delta
	}
	if to >= at {
		to += delta
	}
	return from, to
}

func relabel(d *port.FormDefinition) {
	for i := range d.Columns {
		d.Columns[i].Letter = formula.ColumnLetter(i + 1)
	}
}

func insertColumn(d *port.FormDefinition, op Op) error {
	if op.Column == nil {
		return errors.New("thiếu column")
	}
	at, err := colIndex(d, op.At)
	if err != nil {
		if len(d.Columns) == 0 {
			at = 1
		} else {
			return err
		}
	}
	switch op.Side {
	case "after":
		at++
	case "", "before":
	default:
		return errors.New("side là before hoặc after")
	}
	if len(d.Columns)+1 > MaxColumns {
		return fmt.Errorf("tối đa %d cột", MaxColumns)
	}
	if err := shiftAll(d, at, 1); err != nil {
		return err
	}
	col := *op.Column
	d.Columns = slices.Insert(d.Columns, at-1, col)
	relabel(d)
	// Công thức của cột mới viết theo chữ cái SAU khi chèn (đã đúng vị trí).
	return nil
}

func deleteColumn(d *port.FormDefinition, letter string) error {
	at, err := colIndex(d, letter)
	if err != nil {
		return err
	}
	if len(d.Columns) == 1 {
		return errors.New("mẫu phải còn ít nhất một cột")
	}
	removed := d.Columns[at-1]
	d.Columns = slices.Delete(d.Columns, at-1, at)
	if err := shiftAll(d, at, -1); err != nil {
		return err
	}
	relabel(d)
	if removed.Kind == port.ColumnPerPeriod {
		for i := range d.PeriodBlocks {
			if d.PeriodBlocks[i].ID == removed.PeriodBlock {
				return fmt.Errorf("cột %s thuộc khối kỳ %s: đổi số kỳ bằng set_period_block thay vì xóa lẻ", letter, removed.PeriodBlock)
			}
		}
	}
	return nil
}

func updateColumn(d *port.FormDefinition, op Op) error {
	at, err := colIndex(d, op.Letter)
	if err != nil {
		return err
	}
	if op.Column == nil {
		return errors.New("thiếu column")
	}
	col := *op.Column
	col.Letter = d.Columns[at-1].Letter
	d.Columns[at-1] = col
	return nil
}

func insertHeaderRow(d *port.FormDefinition, op Op) error {
	r := op.Row
	if r < 1 || r > d.Header.Rows {
		if d.Header.Rows == 0 && r <= 1 {
			r = 1
		} else {
			return fmt.Errorf("dòng tiêu đề %d không tồn tại", op.Row)
		}
	}
	if op.Side == "after" {
		r++
	}
	if d.Header.Rows+1 > MaxHeaderRows {
		return fmt.Errorf("tối đa %d dòng tiêu đề", MaxHeaderRows)
	}
	d.Header.Rows++
	for i := range d.Header.Cells {
		if d.Header.Cells[i].Row >= r {
			d.Header.Cells[i].Row++
		}
	}
	for i, m := range d.Header.Merges {
		rc, err := ParseRect(m)
		if err != nil {
			return err
		}
		rc.R1, rc.R2 = shiftSpan(rc.R1, rc.R2, r, 1)
		d.Header.Merges[i] = rc.String()
	}
	return nil
}

func deleteHeaderRow(d *port.FormDefinition, r int) error {
	if r < 1 || r > d.Header.Rows {
		return fmt.Errorf("dòng tiêu đề %d không tồn tại", r)
	}
	if d.Header.Rows == 1 {
		return errors.New("mẫu phải còn ít nhất một dòng tiêu đề")
	}
	d.Header.Rows--
	var cells []port.HeaderCell
	for _, c := range d.Header.Cells {
		switch {
		case c.Row == r:
			continue
		case c.Row > r:
			c.Row--
		}
		cells = append(cells, c)
	}
	d.Header.Cells = cells
	var merges []string
	for _, m := range d.Header.Merges {
		rc, err := ParseRect(m)
		if err != nil {
			return err
		}
		rc.R1, rc.R2 = shiftSpan(rc.R1, rc.R2, r, -1)
		if rc.R1 > rc.R2 || rc.C1 == rc.C2 && rc.R1 == rc.R2 {
			continue
		}
		merges = append(merges, rc.String())
	}
	d.Header.Merges = merges
	return nil
}

func setHeaderCell(d *port.FormDefinition, op Op) error {
	if _, err := colIndex(d, op.Letter); err != nil {
		return err
	}
	if op.Row < 1 || op.Row > d.Header.Rows {
		return fmt.Errorf("dòng tiêu đề %d không tồn tại", op.Row)
	}
	l := strings.ToUpper(op.Letter)
	idx := slices.IndexFunc(d.Header.Cells, func(c port.HeaderCell) bool { return c.Row == op.Row && c.Letter == l })
	if op.Text == "" {
		if idx >= 0 {
			d.Header.Cells = slices.Delete(d.Header.Cells, idx, idx+1)
		}
		return nil
	}
	if idx >= 0 {
		d.Header.Cells[idx].Text = op.Text
		return nil
	}
	d.Header.Cells = append(d.Header.Cells, port.HeaderCell{Row: op.Row, Letter: l, Text: op.Text})
	return nil
}

func merge(d *port.FormDefinition, rng string) error {
	r, err := ParseRect(rng)
	if err != nil {
		return err
	}
	for _, m := range d.Header.Merges {
		o, _ := ParseRect(m)
		if r.overlaps(o) {
			return fmt.Errorf("vùng %s chồng lên vùng gộp %s — tách trước", r, m)
		}
	}
	d.Header.Merges = append(d.Header.Merges, r.String())
	// Như Excel: chỉ giữ chữ của ô góc trên trái.
	var cells []port.HeaderCell
	for _, c := range d.Header.Cells {
		ci := formula.ColumnIndex(c.Letter)
		inside := ci >= r.C1 && ci <= r.C2 && c.Row >= r.R1 && c.Row <= r.R2
		if inside && (ci != r.C1 || c.Row != r.R1) {
			continue
		}
		cells = append(cells, c)
	}
	d.Header.Cells = cells
	return nil
}

func unmerge(d *port.FormDefinition, rng string) error {
	r, err := ParseRect(rng)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(d.Header.Merges, func(m string) bool { o, _ := ParseRect(m); return o == r })
	if idx < 0 {
		return fmt.Errorf("không có vùng gộp %s", r)
	}
	d.Header.Merges = slices.Delete(d.Header.Merges, idx, idx+1)
	return nil
}

func addIndicator(d *port.FormDefinition, op Op) error {
	if op.Indicator == nil {
		return errors.New("thiếu indicator")
	}
	pos := len(d.Rows.Indicators)
	if op.After != "" {
		idx := slices.IndexFunc(d.Rows.Indicators, func(x port.Indicator) bool { return x.Code == op.After })
		if idx < 0 {
			return errors.New("không có chỉ tiêu " + op.After)
		}
		pos = idx + 1
	}
	d.Rows.Indicators = slices.Insert(d.Rows.Indicators, pos, *op.Indicator)
	return nil
}

func setMeta(d *port.FormDefinition, m *Meta) error {
	if m == nil {
		return errors.New("thiếu meta")
	}
	if m.Title != nil {
		d.Title = *m.Title
	}
	if m.SheetName != nil {
		d.SheetName = *m.SheetName
	}
	if m.Requirements != nil {
		d.Requirements = *m.Requirements
	}
	if m.FreezeThrough != nil {
		d.FreezeThrough = strings.ToUpper(*m.FreezeThrough)
	}
	if m.Completeness != nil {
		d.Completeness = *m.Completeness
	}
	if m.Pending != nil {
		d.Pending = append([]port.PendingItem{}, (*m.Pending)...)
	}
	return nil
}
