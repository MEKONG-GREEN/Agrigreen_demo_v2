// Package formimport nạp mẫu báo cáo từ một sheet Excel (.xlsx) thành định nghĩa lưới
// nháp (FN-AD-04.06 "nạp từ Excel": tiêu đề gộp nhiều tầng, chữ cái cột, công thức của
// dòng dữ liệu) và xuất định nghĩa thành tệp mẫu Excel đúng bố cục (tiêu đề gộp, hàng
// chữ cái, dòng loại cột, dòng dữ liệu có công thức) — dùng cho "Tải mẫu Excel" và kiểm
// thử khứ hồi. Khi TTKN An Giang gửi tệp gốc (Q-01), Admin nạp từng sheet thành phiên
// bản nháp mới của mẫu rồi rà trong trình thiết kế.
//
// Không float (R-10): ô số đọc/ghi dạng chuỗi.
package formimport

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formula"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// MaxBytes giới hạn tệp nạp.
const MaxBytes = 8 << 20

// Nhãn dòng loại cột (dòng dưới hàng chữ cái, như "dòng cuối hiển thị loại cột" của Hình 27).
var kindLabels = map[string]string{
	port.ColumnFixed: "Cố định", port.ColumnProvincial: "Tỉnh giao", port.ColumnInput: "Nhập",
	port.ColumnPerPeriod: "Kỳ", port.ColumnFormula: "ƒx",
}

func kindOfLabel(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for k, v := range kindLabels {
		if strings.EqualFold(s, v) {
			return k, true
		}
	}
	if strings.EqualFold(s, "Công thức") || s == "fx" {
		return port.ColumnFormula, true
	}
	return "", false
}

// RequirementsPrefix là nhãn dòng "Yêu cầu biểu mẫu" trong tệp.
const RequirementsPrefix = "Yêu cầu biểu mẫu:"

// Export dựng tệp mẫu Excel của một định nghĩa: dòng 1…H tiêu đề (có gộp), dòng H+1 hàng
// chữ cái, H+2 dòng loại cột, H+3 dòng dữ liệu mẫu (công thức theo Excel), H+5 "Yêu cầu
// biểu mẫu". Cố định cột tới freeze_through và các dòng tiêu đề.
func Export(def port.FormDefinition) ([]byte, error) {
	if err := form.Validate(def); err != nil {
		return nil, err
	}
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	sh := sheetName(def)
	if err := x.SetSheetName(x.GetSheetName(0), sh); err != nil {
		return nil, err
	}
	h := def.Header.Rows
	set := func(col, row int, v string) error {
		cell, err := excelize.CoordinatesToCellName(col, row)
		if err != nil {
			return err
		}
		return x.SetCellStr(sh, cell, v)
	}
	for _, c := range def.Header.Cells {
		if err := set(formula.ColumnIndex(c.Letter), c.Row, c.Text); err != nil {
			return nil, err
		}
	}
	for _, m := range def.Header.Merges {
		a, b, _ := strings.Cut(m, ":")
		if err := x.MergeCell(sh, a, b); err != nil {
			return nil, err
		}
	}
	dataRow := h + 3
	for i, c := range def.Columns {
		col := i + 1
		if err := set(col, h+1, c.Letter); err != nil {
			return nil, err
		}
		if err := set(col, h+2, kindLabels[c.Kind]); err != nil {
			return nil, err
		}
		if c.Kind == port.ColumnFormula {
			p, err := formula.Compile(c.Formula)
			if err != nil {
				return nil, err
			}
			cell, _ := excelize.CoordinatesToCellName(col, dataRow)
			if err := x.SetCellFormula(sh, cell, formula.ToExcel(p.Root, dataRow)); err != nil {
				return nil, err
			}
		}
	}
	if def.Requirements != "" {
		if err := set(1, dataRow+2, RequirementsPrefix+" "+def.Requirements); err != nil {
			return nil, err
		}
	}
	if def.FreezeThrough != "" {
		xs := formula.ColumnIndex(def.FreezeThrough)
		tl, _ := excelize.CoordinatesToCellName(xs+1, h+3)
		if err := x.SetPanes(sh, &excelize.Panes{Freeze: true, XSplit: xs, YSplit: h + 2, TopLeftCell: tl, ActivePane: "bottomRight"}); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	if err := x.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sheetName(def port.FormDefinition) string {
	s := def.SheetName
	if s == "" {
		s = def.Code
	}
	// Excel: ≤ 31 ký tự, không có : \ / ? * [ ]
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:\/?*[]`, r) {
			return '_'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	return s
}

// Options là tham số nạp (kiểu ở port để cmd/formimport dùng được, R-02).
type Options = port.FormImportOptions

// Report là kết quả phân tích.
type Report = port.FormImportReport

// ErrInvalid: tệp không đọc được hoặc không nhận ra bố cục mẫu.
var ErrInvalid = errors.New("formimport: tệp không phải mẫu báo cáo hợp lệ")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

var periodRe = regexp.MustCompile(`(?i)^\s*(tuần|kỳ|tháng|đợt)\s*(\d+)\s*$`)
var unitRe = regexp.MustCompile(`\(([^()]{1,12})\)\s*$`)

// Import đọc một sheet thành định nghĩa nháp (đã kiểm cấu trúc) và báo cáo phân tích.
func Import(r io.Reader, o Options) (port.FormDefinition, Report, error) {
	var rep Report
	x, err := excelize.OpenReader(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return port.FormDefinition{}, rep, invalid("không mở được .xlsx")
	}
	defer func() { _ = x.Close() }()
	rep.Sheets = x.GetSheetList()
	sh := o.Sheet
	if sh == "" {
		sh = x.GetSheetName(0)
	}
	if !slices.Contains(rep.Sheets, sh) {
		return port.FormDefinition{}, rep, invalid("không có sheet %q", sh)
	}
	rep.Sheet = sh
	rows, err := x.GetRows(sh, excelize.Options{RawCellValue: true})
	if err != nil {
		return port.FormDefinition{}, rep, invalid("không đọc được sheet")
	}
	val := func(col, row int) string {
		if row < 1 || row > len(rows) || col < 1 || col > len(rows[row-1]) {
			return ""
		}
		return strings.TrimSpace(rows[row-1][col-1])
	}
	formulaAt := func(col, row int) string {
		cell, _ := excelize.CoordinatesToCellName(col, row)
		f, _ := x.GetCellFormula(sh, cell)
		return f
	}
	start := o.HeaderStart
	if start < 1 {
		start = 1
	}
	// Dòng chữ cái (A, B, C…) và dòng loại cột (nếu có) tách tiêu đề với dữ liệu.
	lettersRow, kindRow := 0, 0
	for r := start; r <= len(rows) && r < start+form.MaxHeaderRows+3; r++ {
		if isLettersRow(rows[r-1]) {
			lettersRow = r
			if r+1 <= len(rows) && isKindRow(rows[r]) {
				kindRow = r + 1
			}
			break
		}
	}
	headerEnd := 0
	dataRow := o.DataRow
	switch {
	case lettersRow > 0:
		headerEnd = lettersRow - 1
		if dataRow == 0 {
			dataRow = lettersRow + 1
			if kindRow > 0 {
				dataRow = kindRow + 1
			}
		}
	case dataRow > 0:
		headerEnd = dataRow - 1
	default:
		// Dòng dữ liệu đầu = dòng đầu có công thức; không có → dòng có STT "1" ở cột A.
		width := 1
		for r := start; r <= len(rows) && r <= start+form.MaxHeaderRows; r++ {
			width = max(width, len(rows[r-1]))
		}
		for r := start + 1; r <= start+form.MaxHeaderRows+1 && dataRow == 0; r++ {
			for c := 1; c <= min(width+50, form.MaxColumns); c++ {
				if formulaAt(c, r) != "" {
					dataRow = r
					break
				}
			}
		}
		if dataRow == 0 {
			for r := start + 1; r <= len(rows); r++ {
				if val(1, r) == "1" {
					dataRow = r
					break
				}
			}
		}
		if dataRow == 0 {
			return port.FormDefinition{}, rep, invalid("không nhận ra dòng dữ liệu đầu — truyền data_row")
		}
		headerEnd = dataRow - 1
	}
	hRows := headerEnd - start + 1
	if hRows < 1 || hRows > form.MaxHeaderRows {
		return port.FormDefinition{}, rep, invalid("khung tiêu đề %d dòng (cần 1–%d)", hRows, form.MaxHeaderRows)
	}
	rep.HeaderRows, rep.DataRow, rep.KindRow = hRows, dataRow, kindRow > 0
	// Số cột: cột cuối có chữ ở tiêu đề, hàng chữ cái hay công thức ở dòng dữ liệu.
	nCols := 0
	for r := start; r <= max(headerEnd, lettersRow); r++ {
		if r <= len(rows) {
			for c := len(rows[r-1]); c > nCols; c-- {
				if strings.TrimSpace(rows[r-1][c-1]) != "" {
					nCols = c
					break
				}
			}
		}
	}
	merges, _ := x.GetMergeCells(sh)
	type mrect struct{ c1, r1, c2, r2 int }
	var rects []mrect
	for _, m := range merges {
		c1, r1, e1 := excelize.CellNameToCoordinates(m.GetStartAxis())
		c2, r2, e2 := excelize.CellNameToCoordinates(m.GetEndAxis())
		if e1 != nil || e2 != nil {
			continue
		}
		if r1 >= start && r2 <= headerEnd {
			rects = append(rects, mrect{c1, r1, c2, r2})
			nCols = max(nCols, c2)
		}
	}
	for c := nCols + 1; c <= form.MaxColumns; c++ {
		if formulaAt(c, dataRow) == "" {
			break
		}
		nCols = c
	}
	if nCols < 1 || nCols > form.MaxColumns {
		return port.FormDefinition{}, rep, invalid("không có cột nào trong khung tiêu đề")
	}
	rep.Columns = nCols
	cover := func(col, row int) (mrect, bool) { // vùng gộp chứa ô
		for _, m := range rects {
			if col >= m.c1 && col <= m.c2 && row >= m.r1 && row <= m.r2 {
				return m, true
			}
		}
		return mrect{}, false
	}
	def := port.FormDefinition{
		Schema: port.FormSchema, Code: o.Code, Title: o.Title, SheetName: sh,
		Header:  port.FormHeader{Rows: hRows, Cells: []port.HeaderCell{}, Merges: []string{}},
		Rows:    port.FormRows{Kind: port.RowsPerCommune, Indicators: []port.Indicator{}},
		Columns: make([]port.FormColumn, 0, nCols), PeriodBlocks: []port.PeriodBlock{}, Groups: []port.ColumnGroup{}, Rules: []port.FormRule{}, Pending: []port.PendingItem{},
		Source: &port.FormSource{Kind: "import", File: o.FileName, Sheet: sh, DataRow: dataRow},
	}
	if def.Title == "" {
		def.Title = sh
	}
	for _, m := range rects {
		def.Header.Merges = append(def.Header.Merges, fmt.Sprintf("%s%d:%s%d", formula.ColumnLetter(m.c1), m.r1-start+1, formula.ColumnLetter(m.c2), m.r2-start+1))
	}
	rep.Merges = len(rects)
	// Ô tiêu đề và tiêu đề cột (chữ của tầng thấp nhất phủ cột).
	titles := make([]string, nCols+1)
	path := make([][]string, nCols+1)
	for r := start; r <= headerEnd; r++ {
		for c := 1; c <= nCols; c++ {
			text := val(c, r)
			if m, ok := cover(c, r); ok && (m.c1 != c || m.r1 != r) {
				text = "" // ô bên trong vùng gộp: chữ thuộc ô góc
			}
			if text != "" {
				def.Header.Cells = append(def.Header.Cells, port.HeaderCell{Row: r - start + 1, Letter: formula.ColumnLetter(c), Text: text})
			}
		}
	}
	for c := 1; c <= nCols; c++ {
		for r := start; r <= headerEnd; r++ {
			text := val(c, r)
			if m, ok := cover(c, r); ok {
				text = val(m.c1, m.r1)
				if m.r1 != r {
					continue
				}
			}
			if text != "" && (len(path[c]) == 0 || path[c][len(path[c])-1] != text) {
				path[c] = append(path[c], text)
			}
		}
		if n := len(path[c]); n > 0 {
			titles[c] = path[c][n-1]
		}
	}
	// Loại, kiểu, công thức từng cột.
	for c := 1; c <= nCols; c++ {
		col := port.FormColumn{Letter: formula.ColumnLetter(c), Title: titles[c], DType: port.DTypeNumber}
		low := strings.ToLower(strings.Join(path[c], " "))
		kind, fromRow := "", false
		if kindRow > 0 {
			kind, fromRow = kindOfLabel(val(c, kindRow))
		}
		if f := formulaAt(c, dataRow); f != "" {
			conv, err := ToRowFormula(f, dataRow)
			if err != nil {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("Cột %s: công thức %q không dùng được (%v) — để cột nhập, Admin sửa trong trình thiết kế", col.Letter, f, err))
				col.Kind, col.Placeholder, col.Note = port.ColumnInput, true, "Công thức gốc: ="+f
			} else {
				col.Kind, col.Formula = port.ColumnFormula, conv
				rep.Formulas++
			}
		} else if fromRow && kind != port.ColumnFormula {
			col.Kind = kind
		} else {
			col.Kind = port.ColumnInput
			if fromRow && kind == port.ColumnFormula {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("Cột %s: dòng loại ghi công thức nhưng dòng dữ liệu không có công thức", col.Letter))
			}
		}
		if col.Kind == port.ColumnFixed || !fromRow && col.Kind == port.ColumnInput {
			switch {
			case c <= 4 && (low == "stt" || strings.HasPrefix(low, "stt ") || low == "tt"):
				col.Kind, col.FixedSource, col.DType = port.ColumnFixed, port.FixedRowNo, port.DTypeInteger
			case c <= 4 && strings.HasPrefix(low, "khu vực"):
				col.Kind, col.FixedSource, col.DType = port.ColumnFixed, port.FixedRegionName, port.DTypeText
			case c <= 4 && (strings.HasPrefix(low, "xã") || strings.HasPrefix(low, "tên xã") || strings.HasPrefix(low, "đơn vị")):
				col.Kind, col.FixedSource, col.DType = port.ColumnFixed, port.FixedCommuneName, port.DTypeText
			case col.Kind == port.ColumnFixed:
				col.FixedSource, col.DType = port.FixedNone, port.DTypeText
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("Cột %s: cột cố định chưa rõ nguồn — đặt fixed_source", col.Letter))
			case !fromRow && strings.Contains(low, "tỉnh giao"):
				col.Kind = port.ColumnProvincial
			}
		}
		if col.Kind != port.ColumnFixed && strings.Contains(low, "ngày") {
			col.DType = port.DTypeDate
		}
		if m := unitRe.FindStringSubmatch(titles[c]); m != nil && col.DType == port.DTypeNumber {
			col.Unit = strings.TrimSpace(m[1])
		}
		def.Columns = append(def.Columns, col)
	}
	importPeriods(&def, &rep)
	if o := firstMatch(rows, RequirementsPrefix); o != "" {
		def.Requirements = o
	}
	if p, err := x.GetPanes(sh); err == nil && p.Freeze && p.XSplit > 0 && p.XSplit <= nCols {
		def.FreezeThrough = formula.ColumnLetter(p.XSplit)
	}
	if hasCol(def, port.FixedRegionName) != "" {
		def.Totals = []port.TotalRowDef{
			{Kind: port.TotalRegion, Label: "Cộng {region}", LabelLetter: hasCol(def, port.FixedRegionName)},
			{Kind: port.TotalProvince, Label: "Toàn tỉnh", LabelLetter: hasCol(def, port.FixedRegionName)},
		}
	} else {
		def.Totals = []port.TotalRowDef{}
		rep.Warnings = append(rep.Warnings, "Không thấy cột Khu vực — chưa đặt dòng tổng")
	}
	def.Completeness = port.CompletenessComplete
	if len(rep.Warnings) > 0 {
		def.Completeness = port.CompletenessPartial
	}
	if err := form.Validate(def); err != nil {
		return def, rep, err
	}
	return def, rep, nil
}

func hasCol(def port.FormDefinition, src string) string {
	for _, c := range def.Columns {
		if c.Kind == port.ColumnFixed && c.FixedSource == src {
			return c.Letter
		}
	}
	return ""
}

// importPeriods nhận khối theo kỳ: các ô tiêu đề "Tuần n"/"Kỳ n"/"Tháng n" liên tiếp cùng
// dòng, cùng bề rộng (vùng gộp) tạo một khối; cột nhập trong khối thành cột theo kỳ.
func importPeriods(def *port.FormDefinition, rep *Report) {
	type slot struct {
		cell  int // chỉ số ô tiêu đề
		from  int
		width int
		n     int
		word  string
	}
	byRow := map[int][]slot{}
	for i, hc := range def.Header.Cells {
		m := periodRe.FindStringSubmatch(hc.Text)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		c := formula.ColumnIndex(hc.Letter)
		w := 1
		for _, mg := range def.Header.Merges {
			r, err := form.ParseRect(mg)
			if err == nil && r.C1 == c && r.R1 == hc.Row {
				w = r.C2 - r.C1 + 1
			}
		}
		byRow[hc.Row] = append(byRow[hc.Row], slot{i, c, w, n, m[1]})
	}
	rowsSorted := make([]int, 0, len(byRow))
	for r := range byRow {
		rowsSorted = append(rowsSorted, r)
	}
	slices.Sort(rowsSorted)
	bid := 0
	for _, r := range rowsSorted {
		ss := byRow[r]
		slices.SortFunc(ss, func(a, b slot) int { return a.from - b.from })
		for i := 0; i < len(ss); {
			j := i + 1
			for j < len(ss) && ss[j].from == ss[j-1].from+ss[j-1].width && ss[j].width == ss[i].width && ss[j].n == ss[j-1].n+1 && strings.EqualFold(ss[j].word, ss[i].word) {
				j++
			}
			if ss[i].n == 1 {
				bid++
				id := fmt.Sprintf("p%d", bid)
				ok := true
				for k := i; k < j && ok; k++ {
					for c := ss[k].from; c < ss[k].from+ss[k].width; c++ {
						kind := def.Columns[c-1].Kind
						if kind != port.ColumnInput && kind != port.ColumnPerPeriod && kind != port.ColumnFormula {
							ok = false
						}
					}
				}
				if ok {
					for k := i; k < j; k++ {
						s := ss[k]
						for c := s.from; c < s.from+s.width; c++ {
							col := &def.Columns[c-1]
							if col.Kind == port.ColumnInput {
								col.Kind = port.ColumnPerPeriod
							}
							col.PeriodBlock, col.PeriodSlot = id, s.n
						}
						def.Header.Cells[s.cell].PeriodBlock, def.Header.Cells[s.cell].PeriodSlot = id, s.n
					}
					word := []rune(ss[i].word)
					word[0] = unicode.ToUpper(word[0])
					def.PeriodBlocks = append(def.PeriodBlocks, port.PeriodBlock{ID: id, FirstLetter: formula.ColumnLetter(ss[i].from), Width: ss[i].width, Slots: j - i, TitlePattern: string(word) + " {n}"})
				} else {
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("Khối kỳ ở dòng tiêu đề %d có cột cố định/Tỉnh giao — bỏ qua", r))
				}
			}
			i = j
		}
	}
	for i := range def.Columns {
		if def.Columns[i].Kind == port.ColumnPerPeriod && def.Columns[i].PeriodBlock == "" {
			def.Columns[i].Kind = port.ColumnInput
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("Cột %s: ghi loại Kỳ nhưng không có tiêu đề kỳ (Tuần n) — để cột nhập", def.Columns[i].Letter))
		}
	}
}

func firstMatch(rows [][]string, prefix string) string {
	for _, r := range rows {
		for _, v := range r {
			if s := strings.TrimSpace(v); strings.HasPrefix(s, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(s, prefix))
			}
		}
	}
	return ""
}

func isLettersRow(r []string) bool {
	n := 0
	for i, v := range r {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if v != formula.ColumnLetter(i+1) {
			return false
		}
		n++
	}
	return n >= 2
}

func isKindRow(r []string) bool {
	n := 0
	for _, v := range r {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if _, ok := kindOfLabel(v); !ok {
			return false
		}
		n++
	}
	return n >= 1
}

var excelRefRe = regexp.MustCompile(`^\$?([A-Za-z]{1,3})\$?([0-9]+)`)

// ToRowFormula đổi công thức Excel của dòng dữ liệu (vd "SUM(F7:L7)" ở dòng 7) thành
// công thức dòng của mẫu ("SUM(F#:L#)"). Tham chiếu dòng khác, sheet khác hoặc hàm
// ngoài tập con → lỗi.
func ToRowFormula(src string, dataRow int) (string, error) {
	src = strings.TrimPrefix(strings.TrimSpace(src), "=")
	if strings.ContainsAny(src, "!") {
		return "", errors.New("tham chiếu sheet khác")
	}
	var b strings.Builder
	rs := []rune(src)
	for i := 0; i < len(rs); {
		c := rs[i]
		if c == '"' { // giữ nguyên chuỗi
			j := i + 1
			for j < len(rs) {
				if rs[j] == '"' {
					if j+1 < len(rs) && rs[j+1] == '"' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= len(rs) {
				return "", errors.New("chuỗi chưa đóng")
			}
			b.WriteString(string(rs[i : j+1]))
			i = j + 1
			continue
		}
		prevIdent := i > 0 && (unicode.IsLetter(rs[i-1]) || unicode.IsDigit(rs[i-1]) || rs[i-1] == '_' || rs[i-1] == '.')
		if !prevIdent && (c == '$' || unicode.IsLetter(c)) {
			if m := excelRefRe.FindStringSubmatch(string(rs[i:])); m != nil {
				end := i + len([]rune(m[0]))
				nextIdent := end < len(rs) && (unicode.IsLetter(rs[end]) || unicode.IsDigit(rs[end]) || rs[end] == '(' || rs[end] == '_')
				if !nextIdent {
					row, _ := strconv.Atoi(m[2])
					if row != dataRow {
						return "", fmt.Errorf("tham chiếu dòng khác (%s)", m[0])
					}
					b.WriteString(strings.ToUpper(m[1]) + "#")
					i = end
					continue
				}
			}
		}
		b.WriteRune(c)
		i++
	}
	p, err := formula.Compile(b.String())
	if err != nil {
		return "", err
	}
	return p.Src, nil
}
