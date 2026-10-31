package domain

// BF-CN-DUTY (#171, CN-14): đọc và ghép tệp Excel đúng mẫu "Lịch trực ksgm" (BM-03, UR-TINH-10, FN-15 B1). Bố cục mẫu:
// vài dòng tiêu đề ("BẢNG PHÂN CÔNG LỊCH TRỰC KSGM … THÁNG M/YYYY", dòng căn cứ "(theo thông báo số …)", ghi chú ca) →
// dòng tiêu đề cột STT | TÊN CƠ SỞ GIẾT MỔ | ĐỊA CHỈ | VIÊN CHỨC TRỰC KSGM | ĐT viên chức | ngày 1…31 → mỗi cơ sở một
// khối dòng (STT, tên, địa chỉ chỉ ghi ở dòng đầu hoặc gộp ô) với mỗi dòng một viên chức → dòng "TC" (tổng) kết thúc.
// Ô ngày: N, Đ (hoặc D), NĐ (hoặc ND), trống = nghỉ. Hàm ở đây thuần (không excelize, không I/O): app đọc tệp thành
// bảng chuỗi rồi gọi ParseBM03, MatchImport.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// Giới hạn tệp nạp (đọc trước khi ghép, không theo nội dung ô).
const (
	MaxImportRows    = 2000 // dòng viên chức (dòng có dữ liệu sau dòng tiêu đề cột)
	MaxImportScan    = 6000 // dòng vật lý tối đa đọc trong một sheet
	MaxImportCols    = 40   // 5 cột thông tin + 31 ngày + dư
	MaxImportCellLen = 300  // ký tự mỗi ô chữ (tên, địa chỉ, ĐT); ô dài hơn → lỗi dòng
	MaxImportIssues  = 500  // lỗi dòng tối đa trả về
	maxShiftTextLen  = 10
	maxBasisLen      = 300
	headerScanRows   = 30 // dòng tiêu đề cột phải nằm trong 30 dòng đầu
)

// ImportRow là một dòng viên chức của tệp sau khi nối khối cơ sở (tên, địa chỉ của dòng đầu khối áp cho các dòng sau).
type ImportRow struct {
	Line     int // số dòng trong tệp (1 = dòng đầu)
	Facility string
	Address  string
	Officer  string
	Phone    string
	Shifts   map[int]string // ngày → ký hiệu như trong tệp (đã bỏ khoảng trắng hai đầu)
	TooLong  string         // tên cột có ô vượt MaxImportCellLen
}

// ImportSheet là một sheet đúng bố cục BM-03.
type ImportSheet struct {
	Month      *time.Time // tháng ghi ở tiêu đề ("THÁNG M/YYYY"); nil = không ghi
	Basis      string     // dòng căn cứ (nếu có)
	HeaderLine int
	Days       int // số cột ngày có tiêu đề (≤ 31)
	Rows       []ImportRow
}

// ErrTooManyRows: sheet quá MaxImportRows dòng viên chức.
var ErrTooManyRows = fmt.Errorf("tệp quá %d dòng viên chức", MaxImportRows)

var monthTitleRe = regexp.MustCompile(`THÁNG\s*(\d{1,2})\s*[/.\-]\s*(\d{4})`)

// Fold chuẩn hóa chữ để so khớp tên: NFC, chữ thường, gộp khoảng trắng.
func Fold(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFC.String(s))), " ")
}

func upper(s string) string { return strings.ToUpper(norm.NFC.String(strings.TrimSpace(s))) }

// ParseBM03 tìm dòng tiêu đề cột (ô đầu "STT" và có cột "VIÊN CHỨC") trong headerScanRows dòng đầu, đọc tháng ở tiêu đề
// và các dòng viên chức tới dòng "TC"/"TỔNG CỘNG". ok = false: không phải bố cục BM-03. Lỗi chỉ khi vượt giới hạn.
func ParseBM03(rows [][]string) (ImportSheet, bool, error) {
	// hdr giữ chính dòng tiêu đề tìm được, không đánh chỉ số lại rows[at] sau vòng lặp.
	at, cols, hdr := -1, bm03Cols{}, []string(nil)
	for i := 0; i < len(rows) && i < headerScanRows; i++ {
		if c, ok := headerCols(rows[i]); ok {
			at, cols, hdr = i, c, rows[i]
			break
		}
	}
	if at < 0 {
		return ImportSheet{}, false, nil
	}
	out := ImportSheet{HeaderLine: at + 1, Rows: []ImportRow{}}
	for i := 0; i < at; i++ {
		for _, c := range rows[i] {
			t := strings.TrimSpace(c)
			if t == "" {
				continue
			}
			if m := monthTitleRe.FindStringSubmatch(upper(t)); m != nil && out.Month == nil {
				mo, _ := strconv.Atoi(m[1])
				y, _ := strconv.Atoi(m[2])
				if mo >= 1 && mo <= 12 {
					d := time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.UTC)
					out.Month = &d
				}
			}
			u := upper(t)
			if out.Basis == "" && (strings.HasPrefix(u, "(THEO") || strings.HasPrefix(u, "(CĂN CỨ") || strings.HasPrefix(u, "CĂN CỨ") || strings.HasPrefix(u, "THEO ")) {
				out.Basis = truncRunes(strings.Join(strings.Fields(t), " "), maxBasisLen)
			}
		}
	}
	for d := 1; d <= 31; d++ {
		j := cols.day + d - 1
		if j >= len(hdr) || j >= MaxImportCols || strings.TrimSpace(hdr[j]) == "" {
			break
		}
		out.Days = d
	}
	var curFac, curAddr string
	for i := at + 1; i < len(rows); i++ {
		r := rows[i]
		get := func(j int) string {
			if j < 0 || j >= len(r) {
				return ""
			}
			return strings.TrimSpace(r[j])
		}
		stt := upper(get(cols.stt))
		if stt == "TC" || stt == "TỔNG" || stt == "TỔNG CỘNG" || strings.HasPrefix(stt, "TỔNG CỘNG") {
			break
		}
		row := ImportRow{Line: i + 1, Shifts: map[int]string{}}
		for _, x := range []struct {
			name string
			v    string
		}{{"Tên cơ sở", get(cols.facility)}, {"Địa chỉ", get(cols.address)}, {"Viên chức", get(cols.officer)}, {"ĐT viên chức", get(cols.phone)}} {
			if utf8.RuneCountInString(x.v) > MaxImportCellLen && row.TooLong == "" {
				row.TooLong = x.name
			}
		}
		if f := get(cols.facility); f != "" {
			curFac, curAddr = f, get(cols.address)
		} else if a := get(cols.address); a != "" && curFac != "" {
			curAddr = a
		}
		row.Facility, row.Address = curFac, curAddr
		row.Officer, row.Phone = get(cols.officer), get(cols.phone)
		for d := 1; d <= 31; d++ {
			v := get(cols.day + d - 1)
			if v != "" {
				row.Shifts[d] = v
			}
		}
		if row.Officer == "" && row.Phone == "" && len(row.Shifts) == 0 {
			continue // dòng đầu khối cơ sở chưa có viên chức, dòng trống
		}
		if len(out.Rows) >= MaxImportRows {
			return ImportSheet{}, true, ErrTooManyRows
		}
		out.Rows = append(out.Rows, row)
	}
	return out, true, nil
}

type bm03Cols struct{ stt, facility, address, officer, phone, day int }

// headerCols nhận dòng tiêu đề cột: ô đầu khác rỗng là "STT", có cột tên cơ sở và cột viên chức. Cột ngày bắt đầu ngay
// sau cột cuối trong năm cột thông tin.
func headerCols(r []string) (bm03Cols, bool) {
	c := bm03Cols{stt: -1, facility: -1, address: -1, officer: -1, phone: -1}
	first := true
	for j, raw := range r {
		if j >= MaxImportCols {
			break
		}
		u := upper(raw)
		if u == "" {
			continue
		}
		if first {
			if u != "STT" {
				return c, false
			}
			c.stt, first = j, false
			continue
		}
		switch {
		case c.phone < 0 && (strings.HasPrefix(u, "ĐT") || strings.HasPrefix(u, "SĐT") || strings.Contains(u, "ĐIỆN THOẠI")):
			c.phone = j
		case c.facility < 0 && strings.Contains(u, "CƠ SỞ"):
			c.facility = j
		case c.address < 0 && strings.Contains(u, "ĐỊA CHỈ"):
			c.address = j
		case c.officer < 0 && strings.Contains(u, "VIÊN CHỨC"):
			c.officer = j
		}
	}
	if c.stt < 0 || c.facility < 0 || c.officer < 0 {
		return c, false
	}
	c.day = max(c.stt, c.facility, c.address, c.officer, c.phone) + 1
	return c, true
}

// ImportFacility là cơ sở hoạt động trong tháng dùng để ghép theo tên hoặc mã.
type ImportFacility struct {
	Ref  uuid.UUID
	Code string
	Name string
}

// ImportOfficer là viên chức Cấp Xã CNTS dùng để ghép theo SĐT hoặc tên. Phone đã chuẩn hóa (NormalizePhone), rỗng =
// không biết.
type ImportOfficer struct {
	Ref   uuid.UUID
	Name  string
	Phone string
}

// ImportIssue là lỗi một dòng (hoặc một ô) của tệp nạp. Không lặp lại SĐT trong thông báo.
type ImportIssue struct {
	Line    int
	Field   string // facility | officer | phone | day:<n> | row
	Message string
}

// NormalizePhone chuẩn hóa SĐT Việt Nam: bỏ ký tự không phải số; +84/84 → 0; 9 chữ số (Excel làm mất số 0 đầu) → thêm 0.
// Không phải 10 chữ số bắt đầu bằng 0 → "".
func NormalizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
		if b.Len() > 15 {
			return ""
		}
	}
	d := b.String()
	switch {
	case len(d) == 11 && strings.HasPrefix(d, "84"):
		d = "0" + d[2:]
	case len(d) == 9 && d[0] != '0':
		d = "0" + d
	}
	if len(d) != 10 || d[0] != '0' {
		return ""
	}
	return d
}

// MaskPhone che SĐT: 4 số đầu · 3 số cuối (như nhãn của kho định danh). Không hợp lệ → "".
func MaskPhone(p string) string {
	p = NormalizePhone(p)
	if p == "" {
		return ""
	}
	return p[:4] + "•••" + p[len(p)-3:]
}

var parenTail = regexp.MustCompile(`\s*\([^()]*\)\s*$`)

// officerKey: tên đã gộp, bỏ phần ghi chú cuối trong ngoặc ("(CTV)", "(TCCCTY)").
func officerKey(s string) string { return Fold(parenTail.ReplaceAllString(s, "")) }

var codeTail = regexp.MustCompile(`^(.*\S)\s*\(([^()]+)\)\s*$`)

// MatchImport ghép các dòng của sheet với cơ sở (mã, tên hoặc "tên (mã)") và viên chức (SĐT, rồi tên) thành ô lưới của
// tháng m. Dòng không ghép được cơ sở hoặc viên chức bị bỏ cả dòng; ô ký hiệu lạ hoặc ngày ngoài tháng bị bỏ ô. Ô trả về
// đã chuẩn hóa và sắp như NormalizeCells.
func MatchImport(m time.Time, sh ImportSheet, facs []ImportFacility, offs []ImportOfficer) ([]Cell, []ImportIssue) {
	days := DaysIn(m)
	byCode, byName := map[string]ImportFacility{}, map[string][]ImportFacility{}
	for _, f := range facs {
		byCode[strings.ToUpper(strings.TrimSpace(f.Code))] = f
		k := Fold(f.Name)
		byName[k] = append(byName[k], f)
	}
	byPhone, byOName := map[string][]ImportOfficer{}, map[string][]ImportOfficer{}
	for _, o := range offs {
		if o.Phone != "" {
			byPhone[o.Phone] = append(byPhone[o.Phone], o)
		}
		if k := officerKey(o.Name); k != "" {
			byOName[k] = append(byOName[k], o)
		}
	}
	var issues []ImportIssue
	add := func(line int, field, msg string) {
		if len(issues) < MaxImportIssues {
			issues = append(issues, ImportIssue{Line: line, Field: field, Message: msg})
		}
	}
	seen := map[[3]string]int{}
	var cells []Cell
	for _, r := range sh.Rows {
		if r.TooLong != "" {
			add(r.Line, "row", fmt.Sprintf("Ô %s quá %d ký tự", r.TooLong, MaxImportCellLen))
			continue
		}
		fac, ok, msg := matchFacility(r.Facility, byCode, byName)
		if !ok {
			add(r.Line, "facility", msg)
		}
		off, ok2, msg2 := matchOfficer(r.Officer, r.Phone, byPhone, byOName)
		if !ok2 {
			add(r.Line, "officer", msg2)
		}
		if !ok || !ok2 {
			continue
		}
		for _, d := range sortedDays(r.Shifts) {
			raw := r.Shifts[d]
			if d > days {
				add(r.Line, "day:"+strconv.Itoa(d), fmt.Sprintf("Ngày %d không có trong tháng %s", d, m.Format("1/2006")))
				continue
			}
			s := NormalizeShift(cleanShift(raw))
			if s == "" {
				add(r.Line, "day:"+strconv.Itoa(d), fmt.Sprintf("Ngày %d: ký hiệu %q lạ (chỉ N, Đ, NĐ hoặc để trống)", d, truncRunes(raw, maxShiftTextLen)))
				continue
			}
			k := [3]string{fac.Ref.String(), off.Ref.String(), strconv.Itoa(d)}
			if prev, dup := seen[k]; dup {
				add(r.Line, "day:"+strconv.Itoa(d), fmt.Sprintf("Ngày %d: trùng ô với dòng %d (cùng cơ sở, viên chức)", d, prev))
				continue
			}
			seen[k] = r.Line
			cells = append(cells, Cell{Facility: fac.Ref, Officer: off.Ref, Day: d, Shift: s})
		}
	}
	if len(cells) > MaxCells {
		add(0, "row", fmt.Sprintf("Tệp có quá %d ô ca trực", MaxCells))
		cells = cells[:MaxCells]
	}
	sortCells(cells)
	if cells == nil {
		cells = []Cell{}
	}
	return cells, issues
}

func matchFacility(text string, byCode map[string]ImportFacility, byName map[string][]ImportFacility) (ImportFacility, bool, string) {
	if text == "" {
		return ImportFacility{}, false, "Thiếu tên cơ sở giết mổ"
	}
	label := truncRunes(text, 60)
	if f, ok := byCode[strings.ToUpper(text)]; ok {
		return f, true, ""
	}
	if m := codeTail.FindStringSubmatch(text); m != nil {
		if f, ok := byCode[strings.ToUpper(strings.TrimSpace(m[2]))]; ok {
			return f, true, ""
		}
	}
	switch l := byName[Fold(text)]; len(l) {
	case 1:
		return l[0], true, ""
	case 0:
		return ImportFacility{}, false, fmt.Sprintf("Không khớp cơ sở giết mổ %q (đang hoạt động trong tháng, thuộc tỉnh)", label)
	default:
		return ImportFacility{}, false, fmt.Sprintf("Tên cơ sở %q trùng nhiều cơ sở — ghi \"tên (mã cơ sở)\"", label)
	}
}

func matchOfficer(name, phone string, byPhone, byName map[string][]ImportOfficer) (ImportOfficer, bool, string) {
	if name == "" && phone == "" {
		return ImportOfficer{}, false, "Thiếu viên chức trực"
	}
	label := truncRunes(parenTail.ReplaceAllString(name, ""), 60)
	var byN []ImportOfficer
	if k := officerKey(name); k != "" {
		byN = byName[k]
	}
	if p := NormalizePhone(phone); p != "" {
		if l := byPhone[p]; len(l) == 1 {
			if len(byN) == 1 && byN[0].Ref != l[0].Ref {
				return ImportOfficer{}, false, fmt.Sprintf("SĐT và tên %q thuộc hai viên chức khác nhau", label)
			}
			return l[0], true, ""
		} else if len(l) > 1 {
			for _, o := range l {
				if len(byN) == 1 && byN[0].Ref == o.Ref {
					return o, true, ""
				}
			}
			return ImportOfficer{}, false, "SĐT trùng nhiều tài khoản viên chức — kiểm tra danh bạ"
		}
	}
	switch len(byN) {
	case 1:
		return byN[0], true, ""
	case 0:
		if name == "" {
			return ImportOfficer{}, false, "SĐT không khớp viên chức Cấp Xã CNTS nào của tỉnh"
		}
		return ImportOfficer{}, false, fmt.Sprintf("Không khớp viên chức %q (tài khoản Cấp Xã CNTS của tỉnh, theo tên hoặc SĐT)", label)
	default:
		return ImportOfficer{}, false, fmt.Sprintf("Tên viên chức %q trùng nhiều tài khoản — ghi SĐT để phân biệt", label)
	}
}

// cleanShift bỏ khoảng trắng và dấu chấm/chấm giữa gõ lẫn trong ô ngày của tệp thật ("N Đ" → "NĐ", "Đ·" → "Đ").
func cleanShift(s string) string {
	if utf8.RuneCountInString(s) > maxShiftTextLen {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '.' || r == '·' || r == ',' || r == ';' {
			return -1
		}
		return r
	}, s)
}

func sortedDays(m map[int]string) []int {
	out := make([]int, 0, len(m))
	for d := range m {
		out = append(out, d)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimRightFunc(string(r[:n]), unicode.IsSpace) + "…"
}

// ---------------------------------------------------------------------------
// Xuất BM-03
// ---------------------------------------------------------------------------

// ExportTitle là tiêu đề sheet xuất (giữ "THÁNG M/YYYY" để nạp lại nhận đúng tháng).
func ExportTitle(m time.Time) string {
	return fmt.Sprintf("BẢNG PHÂN CÔNG LỊCH TRỰC KSGM VIÊN CHỨC TĂNG CƯỜNG XÃ, PHƯỜNG, ĐẶC KHU THÁNG %d/%d", int(m.Month()), m.Year())
}

// ExportSheetName là tên sheet (≤ 31 ký tự): "LỊCH TRỰC KSGM T102026".
func ExportSheetName(m time.Time) string {
	return fmt.Sprintf("LỊCH TRỰC KSGM T%d%d", int(m.Month()), m.Year())
}

// ExportNote là dòng ghi chú ca.
const ExportNote = "(Ghi chú: N: ca ngày; Đ: ca đêm, qua nửa đêm thuộc ngày bắt đầu ca; NĐ: cả ngày đêm; ô trống: nghỉ)"

// DefaultBasis là dòng căn cứ khi người xuất không ghi.
const DefaultBasis = "(theo thông báo số ……/SNNMT ngày …… của Sở Nông nghiệp và Môi trường và kèm theo danh sách phân công viên chức trực KSGM)"

// CheckBasis kiểm dòng căn cứ người xuất ghi: tối đa 300 ký tự, một dòng.
func CheckBasis(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return DefaultBasis, nil
	}
	if utf8.RuneCountInString(s) > maxBasisLen {
		return "", fmt.Errorf("tối đa %d ký tự", maxBasisLen)
	}
	return s, nil
}

// ShiftText là ký hiệu ca trên mẫu (N, Đ, NĐ).
func ShiftText(s string) string {
	switch s {
	case ShiftDay:
		return "N"
	case ShiftNight:
		return "Đ"
	case ShiftBoth:
		return "NĐ"
	}
	return ""
}
