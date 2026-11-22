package domain

// W11.53c (#176, BRD CNTS v3.7 FN-19 luồng phụ "Nạp Excel điểm, hộ"): quy tắc thuần đọc hai bảng của tệp mẫu — "Điểm
// trình diễn" và "Hộ tham gia" — thành các dòng đã kiểm, kèm lỗi theo dòng/cột. Không đọc tệp, không gọi hạ tầng: tầng app
// mở .xlsx (giới hạn giải nén) và đưa vào đây các ô dạng chuỗi thô (RawCellValue). Kiểm với danh mục, đơn vị hành chính,
// danh bạ cán bộ, dữ liệu đã có làm ở tầng app.
//
// Số (quy mô) là số thập phân thường: kiểm cú pháp và số chữ số TRƯỚC khi gọi decimal (AUTO-REVIEW §4 — không số mũ, không
// chuỗi dài, không float). Họ tên, SĐT chủ hộ chỉ sống trong bộ nhớ (Owner) để gửi kho định danh; lỗi không nhắc lại giá trị.

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/unicode/norm"
)

// Giới hạn tệp nạp.
const (
	ImportMaxBytes      = 2 << 20 // 2 MB
	ImportMaxSites      = 200     // dòng điểm mỗi tệp
	ImportMaxHouseholds = 2000    // dòng hộ mỗi tệp
	ImportMaxCols       = 16      // cột đọc mỗi dòng
	ImportHeaderScan    = 5       // dòng đầu tìm dòng tiêu đề cột
	importMaxCellBytes  = 8000    // ô dài hơn bị từ chối trước mọi kiểm khác
	scaleMaxInt         = 12      // chữ số phần nguyên (< 10^12, như numeric(14,2))
	scaleMaxFrac        = 15      // chữ số lẻ đọc được (Excel lưu số thực); làm tròn 2 số lẻ như API
)

// Tên sheet của tệp mẫu.
const (
	SheetSites      = "Điểm trình diễn"
	SheetHouseholds = "Hộ tham gia"
	SheetGuide      = "Hướng dẫn"
)

// SiteHeaders, HouseholdHeaders là tiêu đề cột của tệp mẫu (đúng thứ tự). "(*)" là cột bắt buộc.
var (
	SiteHeaders      = []string{"Mã điểm (*)", "Tên điểm (*)", "Mã xã (*)", "Mã loại mô hình (*)", "Quy mô", "Đơn vị quy mô", "Mẫu nhật ký", "Cán bộ theo dõi", "Ghi chú"}
	HouseholdHeaders = []string{"Mã điểm (*)", "Họ tên chủ hộ (*)", "SĐT chủ hộ (*)", "Quy mô", "Đơn vị quy mô", "Người ghi nhật ký", "Ngày tham gia", "Ghi chú"}
)

// Giá trị cột "Người ghi nhật ký".
const (
	KeeperTextOfficer = "Cán bộ theo dõi"
	KeeperTextSelf    = "Hộ tự ghi"
)

// ErrImportLayout: tệp không đúng bố cục mẫu (thiếu dòng tiêu đề, cột sai, quá số dòng) — từ chối cả tệp.
var ErrImportLayout = errors.New("tệp không đúng mẫu nạp điểm, hộ")

// ImportIssue là một lỗi của một ô/dòng. Row là số dòng Excel (từ 1); Column là chữ cột ("" = cả dòng).
type ImportIssue struct {
	Sheet   string
	Row     int
	Column  string
	Message string
}

// SiteRow là một dòng điểm đã đọc.
type SiteRow struct {
	Row     int
	Code    string // mã điểm trong tệp (khóa nối với sheet hộ), chữ hoa
	Officer string // tên cán bộ theo dõi như ghi trong tệp ("" = chưa giao)
	Site
}

// HouseholdRow là một dòng hộ đã đọc.
type HouseholdRow struct {
	Row      int
	SiteCode string     // mã điểm trong tệp (chữ hoa) — hoặc rỗng khi SiteRef có
	SiteRef  *uuid.UUID // mã điểm đã có của dự án (ghi đúng mã UUID trong tệp)
	Owner    Owner      // chỉ trong bộ nhớ → kho định danh
	PhoneKey string     // SĐT chuẩn hóa 0xxxxxxxxx (khóa kiểm trùng trong tệp)
	Household
}

// SiteKey là khóa nối hộ với điểm trong tệp.
func (h HouseholdRow) SiteKey() string {
	if h.SiteRef != nil {
		return h.SiteRef.String()
	}
	return h.SiteCode
}

var (
	plainDecRe  = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	groupedRe   = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{3})+(,[0-9]+)?$`)
	commaDecRe  = regexp.MustCompile(`^[0-9]+,[0-9]+$`)
	serialRe    = regexp.MustCompile(`^[0-9]{1,5}(\.0+)?$`)
	dmyRe       = regexp.MustCompile(`^([0-9]{1,2})[/.-]([0-9]{1,2})[/.-]([0-9]{4})$`)
	isoRe       = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})$`)
	phoneSepper = strings.NewReplacer(" ", "", ".", "", "-", "", "(", "", ")", "", "\t", "")
	digitsRe    = regexp.MustCompile(`^[0-9]+$`)
)

// Fold chuẩn hóa chữ để so khớp: NFC, chữ thường, gộp khoảng trắng.
func Fold(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFC.String(s))), " ")
}

func headerKey(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(Fold(s), "(*)", ""))
}

// ColumnName trả chữ cột Excel của cột thứ i (0 → A).
func ColumnName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// ParseScale đọc quy mô: số thập phân thường, dấu chấm hoặc dấu phẩy thập phân (định dạng Việt Nam "1.500,5" có nhóm
// nghìn bằng dấu chấm). Kiểm cú pháp và số chữ số TRƯỚC khi gọi decimal; số mũ, dấu âm, chuỗi dài bị chặn. Trả lý do lỗi
// (rỗng = hợp lệ); s rỗng → nil.
func ParseScale(s string) (*decimal.Decimal, string) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return nil, ""
	case len(s) > scaleMaxInt+scaleMaxFrac+8:
		return nil, "quy mô quá dài"
	case strings.ContainsAny(s, "eE"):
		return nil, "không dùng dạng số mũ (1E+…) — ghi số thường"
	case strings.HasPrefix(s, "-"):
		return nil, "phải dương"
	}
	v := s
	switch {
	case groupedRe.MatchString(v) && strings.Contains(v, ","):
		v = strings.ReplaceAll(strings.ReplaceAll(v, ".", ""), ",", ".")
	case groupedRe.MatchString(v):
		return nil, "không rõ dấu chấm là phân cách nghìn hay thập phân — ghi số không phân cách nghìn"
	case commaDecRe.MatchString(v):
		v = strings.ReplaceAll(v, ",", ".")
	}
	if !plainDecRe.MatchString(v) {
		return nil, "phải là số thập phân (vd 1500 hoặc 2,5)"
	}
	intPart, frac, _ := strings.Cut(v, ".")
	intPart = strings.TrimLeft(intPart, "0")
	if len(intPart) > scaleMaxInt {
		return nil, "phải nhỏ hơn 1.000.000.000.000"
	}
	if len(frac) > scaleMaxFrac {
		return nil, "tối đa 15 chữ số thập phân"
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return nil, "phải là số thập phân"
	}
	d = d.Round(2)
	if !d.IsPositive() {
		return nil, "phải dương"
	}
	if d.GreaterThanOrEqual(decimal.New(1, scaleMaxInt)) {
		return nil, "phải nhỏ hơn 1.000.000.000.000"
	}
	return &d, ""
}

// excelEpoch là gốc số ngày của Excel (hệ 1900, đã tính lỗi năm nhuận 1900).
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// ParseDate đọc ngày: dd/mm/yyyy, yyyy-mm-dd hoặc số ngày Excel (ô định dạng ngày đọc thô). Ngày ngoài 2000–2099 bị chặn.
func ParseDate(s string) (*time.Time, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ""
	}
	const bad = "ngày dạng dd/mm/yyyy"
	if len(s) > 20 {
		return nil, bad
	}
	var d time.Time
	switch {
	case serialRe.MatchString(s):
		n, err := strconv.Atoi(strings.SplitN(s, ".", 2)[0])
		if err != nil {
			return nil, bad
		}
		d = excelEpoch.AddDate(0, 0, n)
	case dmyRe.MatchString(s):
		m := dmyRe.FindStringSubmatch(s)
		day, _ := strconv.Atoi(m[1])
		mon, _ := strconv.Atoi(m[2])
		yr, _ := strconv.Atoi(m[3])
		d = time.Date(yr, time.Month(mon), day, 0, 0, 0, 0, time.UTC)
		if d.Day() != day || int(d.Month()) != mon {
			return nil, "ngày không có thật"
		}
	case isoRe.MatchString(s):
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return nil, "ngày không có thật"
		}
		d = t
	default:
		return nil, bad
	}
	if d.Year() < 2000 || d.Year() > 2099 {
		return nil, "ngày ngoài 2000–2099"
	}
	return &d, ""
}

// NormalizePhone đưa SĐT Việt Nam về 0 + 9 chữ số. Nhận +84/84/0084, dấu cách/chấm/gạch; ô số của Excel làm mất số 0 đầu
// (912345678) cũng nhận. ok = false khi không phải SĐT di động/cố định 10 số.
func NormalizePhone(s string) (string, bool) {
	p := phoneSepper.Replace(strings.TrimSpace(s))
	if len(p) > 20 {
		return "", false
	}
	switch {
	case strings.HasPrefix(p, "+84"):
		p = p[3:]
	case strings.HasPrefix(p, "0084"):
		p = p[4:]
	case strings.HasPrefix(p, "84") && len(p) == 11:
		p = p[2:]
	case strings.HasPrefix(p, "0"):
		p = p[1:]
	}
	if len(p) != 9 || !digitsRe.MatchString(p) || p[0] == '0' {
		return "", false
	}
	return "0" + p, true
}

// MaskPhone che SĐT đã chuẩn hóa (giữ 3 số đầu, 3 số cuối).
func MaskPhone(p string) string {
	if len(p) < 7 {
		return "•••"
	}
	return p[:3] + "•••" + p[len(p)-3:]
}

// MaskName che họ tên: chữ cái đầu của mỗi tiếng, giữ nguyên tiếng cuối (vd "N. V. Bình").
func MaskName(s string) string {
	parts := strings.Fields(norm.NFC.String(s))
	if len(parts) == 0 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i == len(parts)-1 {
			out = append(out, p)
			continue
		}
		r, _ := utf8.DecodeRuneInString(p)
		out = append(out, string(r)+".")
	}
	return strings.Join(out, " ")
}

// scaleUnit đọc đơn vị quy mô (mã hoặc chữ có dấu).
func scaleUnit(s string) (string, bool) {
	switch Fold(s) {
	case "":
		return "", true
	case "con":
		return "con", true
	case "m2", "m²":
		return "m2", true
	case "m3", "m³":
		return "m3", true
	case "ha":
		return "ha", true
	case "long", "lồng":
		return "long", true
	case "be", "bè":
		return "be", true
	}
	return "", false
}

// sheet là bảng đã tìm dòng tiêu đề.
type sheet struct {
	name   string
	header int // chỉ số dòng tiêu đề trong rows
	rows   [][]string
	issues []ImportIssue
}

func (s *sheet) add(row, col int, msg string) {
	c := ""
	if col >= 0 {
		c = ColumnName(col)
	}
	s.issues = append(s.issues, ImportIssue{Sheet: s.name, Row: row, Column: c, Message: msg})
}

// locate tìm dòng tiêu đề (ô A là "Mã điểm") trong ImportHeaderScan dòng đầu và kiểm đúng các cột của mẫu.
func locate(name string, rows [][]string, headers []string) (*sheet, error) {
	at := -1
	for i := 0; i < len(rows) && i < ImportHeaderScan; i++ {
		if len(rows[i]) > 0 && headerKey(rows[i][0]) == headerKey(headers[0]) {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("%w: sheet \"%s\" không có dòng tiêu đề bắt đầu bằng cột \"%s\" — dùng tệp mẫu", ErrImportLayout, name, headers[0])
	}
	h := rows[at]
	for j, want := range headers {
		got := ""
		if j < len(h) {
			got = h[j]
		}
		if headerKey(got) != headerKey(want) {
			return nil, fmt.Errorf("%w: sheet \"%s\" cột %s phải là \"%s\" — dùng tệp mẫu, không đổi thứ tự cột", ErrImportLayout, name, ColumnName(j), want)
		}
	}
	for j := len(headers); j < len(h); j++ {
		if strings.TrimSpace(h[j]) != "" {
			return nil, fmt.Errorf("%w: sheet \"%s\" có cột thừa %s — dùng tệp mẫu", ErrImportLayout, name, ColumnName(j))
		}
	}
	return &sheet{name: name, header: at, rows: rows}, nil
}

// each gọi fn cho mỗi dòng dữ liệu không trống (cells đủ ncol ô, đã cắt khoảng trắng; start là số lỗi trước dòng — dòng có
// lỗi nào thì không nhận). Ô quá dài → lỗi ô, bỏ qua dòng. Vượt max dòng dữ liệu → ErrImportLayout.
func (s *sheet) each(ncol, max int, fn func(row int, cells []string, start int)) error {
	n := 0
	for i := s.header + 1; i < len(s.rows); i++ {
		r := s.rows[i]
		start := len(s.issues)
		cells := make([]string, ncol)
		empty, long := true, false
		for j := 0; j < len(r); j++ {
			v := strings.TrimSpace(r[j])
			if v == "" {
				continue
			}
			empty = false
			switch {
			case len(v) > importMaxCellBytes:
				s.add(i+1, j, "ô quá dài")
				long = true
			case j >= ncol:
				s.add(i+1, j, "ô ngoài bảng mẫu — xóa hoặc chuyển vào cột Ghi chú")
			default:
				cells[j] = v
			}
		}
		if empty {
			continue
		}
		n++
		if n > max {
			return fmt.Errorf("%w: sheet \"%s\" quá %d dòng — tách thành nhiều tệp", ErrImportLayout, s.name, max)
		}
		if long {
			continue
		}
		fn(i+1, cells, start)
	}
	return nil
}

func (s *sheet) text(row, col int, v string, max int, required bool, what string) string {
	switch {
	case v == "" && required:
		s.add(row, col, what+" bắt buộc")
	case utf8.RuneCountInString(v) > max:
		s.add(row, col, what+" tối đa "+strconv.Itoa(max)+" ký tự")
		return ""
	}
	return v
}

func (s *sheet) scale(row, col int, value, unit string) Scale {
	d, why := ParseScale(value)
	if why != "" {
		s.add(row, col, "Quy mô: "+why)
		return Scale{}
	}
	u, ok := scaleUnit(unit)
	if !ok {
		s.add(row, col+1, "Đơn vị quy mô: con, m2, m3, ha, lồng hoặc bè")
		return Scale{}
	}
	if (d == nil) != (u == "") {
		s.add(row, col, "nhập cả quy mô và đơn vị (hoặc bỏ trống cả hai)")
		return Scale{}
	}
	return Scale{Value: d, Unit: u}
}

// ParseSites đọc sheet "Điểm trình diễn". Lỗi trả về chỉ khi sai bố cục; lỗi theo ô ở issues.
func ParseSites(rows [][]string) ([]SiteRow, []ImportIssue, error) {
	sh, err := locate(SheetSites, rows, SiteHeaders)
	if err != nil {
		return nil, nil, err
	}
	var out []SiteRow
	codes := map[string]int{}
	names := map[string]int{}
	err = sh.each(len(SiteHeaders), ImportMaxSites, func(row int, c []string, before int) {
		x := SiteRow{Row: row}
		x.Code = strings.ToUpper(sh.text(row, 0, c[0], 40, true, "Mã điểm"))
		switch {
		case x.Code == "":
		case !codeRe.MatchString(x.Code):
			sh.add(row, 0, "Mã điểm 1–40 ký tự chữ, số, . _ / -")
		case isUUID(x.Code):
			sh.add(row, 0, "Mã điểm không được là mã UUID (dành cho điểm đã có)")
		case codes[x.Code] > 0:
			sh.add(row, 0, "Mã điểm trùng dòng "+strconv.Itoa(codes[x.Code]))
		default:
			codes[x.Code] = row
		}
		x.Name = sh.text(row, 1, c[1], MaxName, true, "Tên điểm")
		x.CommuneCode = c[2]
		switch {
		case c[2] == "":
			sh.add(row, 2, "Mã xã bắt buộc")
		case !unitRe.MatchString(c[2]):
			sh.add(row, 2, "Mã xã không hợp lệ")
		}
		x.ModelType = sh.text(row, 3, c[3], 64, true, "Mã loại mô hình")
		x.Scale = sh.scale(row, 4, c[4], c[5])
		x.DiaryForm = strings.ToUpper(c[6])
		if x.DiaryForm != "" && !slices.Contains(DiaryForms, x.DiaryForm) {
			sh.add(row, 6, "Mẫu nhật ký: BM-08, BM-09 hoặc BM-19")
		}
		x.Officer = sh.text(row, 7, c[7], 200, false, "Cán bộ theo dõi")
		x.Note = sh.text(row, 8, c[8], MaxText, false, "Ghi chú")
		if x.Name != "" && x.CommuneCode != "" {
			k := x.CommuneCode + "\x00" + Fold(x.Name)
			if names[k] > 0 {
				sh.add(row, 1, "Tên điểm trùng dòng "+strconv.Itoa(names[k])+" cùng xã")
			} else {
				names[k] = row
			}
		}
		if len(sh.issues) == before {
			if es := x.Normalize(); len(es) > 0 {
				sh.add(row, -1, es.Error())
				return
			}
			out = append(out, x)
		}
	})
	if err != nil {
		return nil, nil, err
	}
	return out, sh.issues, nil
}

// keeper đọc cột "Người ghi nhật ký".
func keeper(s string) (string, bool) {
	switch Fold(s) {
	case "", Fold(KeeperTextOfficer), "cán bộ", "can bo", "officer":
		return KeeperOfficer, true
	case Fold(KeeperTextSelf), "tự ghi", "tu ghi", "self":
		return KeeperSelf, true
	}
	return "", false
}

// ParseHouseholds đọc sheet "Hộ tham gia". Lỗi trả về chỉ khi sai bố cục; lỗi theo ô ở issues. Người ghi nhật ký "Cán bộ
// theo dõi" (mặc định) để KeeperRef trống — tầng app gán cán bộ theo dõi của điểm.
func ParseHouseholds(rows [][]string) ([]HouseholdRow, []ImportIssue, error) {
	sh, err := locate(SheetHouseholds, rows, HouseholdHeaders)
	if err != nil {
		return nil, nil, err
	}
	var out []HouseholdRow
	seen := map[string]int{}
	err = sh.each(len(HouseholdHeaders), ImportMaxHouseholds, func(row int, c []string, before int) {
		x := HouseholdRow{Row: row}
		code := sh.text(row, 0, c[0], 40, true, "Mã điểm")
		if code != "" {
			if r, err := uuid.Parse(code); err == nil && isUUID(code) {
				x.SiteRef = &r
			} else if codeRe.MatchString(code) {
				x.SiteCode = strings.ToUpper(code)
			} else {
				sh.add(row, 0, "Mã điểm không hợp lệ")
			}
		}
		x.Owner.FullName = sh.text(row, 1, c[1], 200, true, "Họ tên chủ hộ")
		switch p, ok := NormalizePhone(c[2]); {
		case c[2] == "":
			sh.add(row, 2, "SĐT chủ hộ bắt buộc (khóa kiểm trùng chủ hộ ở kho định danh)")
		case !ok:
			sh.add(row, 2, "SĐT chủ hộ không hợp lệ (10 số, bắt đầu bằng 0)")
		default:
			x.Owner.Phone, x.PhoneKey = p, p
		}
		x.Scale = sh.scale(row, 3, c[3], c[4])
		k, ok := keeper(c[5])
		if !ok {
			sh.add(row, 5, "Người ghi nhật ký: \""+KeeperTextOfficer+"\" hoặc \""+KeeperTextSelf+"\"")
		}
		x.KeeperKind, x.SelfEntry = k, k == KeeperSelf
		d, why := ParseDate(c[6])
		if why != "" {
			sh.add(row, 6, "Ngày tham gia: "+why)
		}
		x.JoinedOn = d
		x.Note = sh.text(row, 7, c[7], MaxText, false, "Ghi chú")
		if x.PhoneKey != "" && x.SiteKey() != "" {
			key := x.SiteKey() + "\x00" + x.PhoneKey
			if seen[key] > 0 {
				sh.add(row, 2, "Chủ hộ trùng dòng "+strconv.Itoa(seen[key])+" cùng điểm")
			} else {
				seen[key] = row
			}
		}
		if len(sh.issues) == before {
			if es := x.Owner.Validate(); len(es) > 0 {
				sh.add(row, 1, "Họ tên chủ hộ bắt buộc, tối đa 200 ký tự")
				return
			}
			out = append(out, x)
		}
	})
	if err != nil {
		return nil, nil, err
	}
	return out, sh.issues, nil
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil && len(s) == 36
}
