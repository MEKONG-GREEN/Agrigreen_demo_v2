// Package tabular đọc tệp bảng CSV (UTF-8) hoặc Excel .xlsx thành các dòng chữ
// có tiêu đề, và ghi tệp .xlsx. Dùng chung cho các đường nhập/xuất hàng loạt
// (nông hộ, cây tổ chức, máy nông nghiệp).
//
// Nguyên tắc an toàn:
//   - giới hạn dung lượng tệp, dung lượng sau giải nén (chống zip bomb), số dòng
//     và số cột;
//   - KHÔNG tính công thức: ô .xlsx có công thức làm cả tệp bị từ chối
//     (FormulaError), vì giá trị lưu sẵn có thể không khớp công thức;
//   - khi ghi, mọi ô là chuỗi — không bao giờ sinh công thức.
//
// Gói không chứa quy tắc nghiệp vụ: module tự đặt bí danh cột và tự kiểm giá trị.
package tabular

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ContentTypeXLSX là kiểu MIME của tệp Excel .xlsx.
const ContentTypeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// Định dạng tệp đã nhận ra.
const (
	FormatCSV  = "csv"
	FormatXLSX = "xlsx"
)

// Lỗi đọc tệp. Message trả câu tiếng Việt cho người dùng.
var (
	ErrEmpty          = errors.New("tabular: tệp không có dòng tiêu đề")
	ErrFormat         = errors.New("tabular: tệp không phải CSV UTF-8 hoặc Excel .xlsx hợp lệ")
	ErrEncoding       = errors.New("tabular: tệp CSV không phải UTF-8")
	ErrTooLarge       = errors.New("tabular: tệp vượt dung lượng cho phép")
	ErrTooManyRows    = errors.New("tabular: tệp vượt số dòng cho phép")
	ErrTooManyColumns = errors.New("tabular: tệp vượt số cột cho phép")
	ErrLegacyExcel    = errors.New("tabular: tệp Excel 97-2003 (.xls) không được hỗ trợ")
)

// FormulaError: ô .xlsx chứa công thức — không tính công thức, cả tệp bị từ chối.
type FormulaError struct{ Cell string }

func (e *FormulaError) Error() string { return "tabular: ô " + e.Cell + " chứa công thức" }

// Limits là giới hạn khi đọc. Giá trị 0 dùng mặc định.
type Limits struct {
	MaxBytes int64 // dung lượng tệp; mặc định 5 MB
	MaxRows  int   // số dòng dữ liệu (không kể tiêu đề); mặc định 1000
	MaxCols  int   // số cột; mặc định 64
}

func (l Limits) withDefaults() Limits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = 5 << 20
	}
	if l.MaxRows <= 0 {
		l.MaxRows = 1000
	}
	if l.MaxCols <= 0 {
		l.MaxCols = 64
	}
	return l
}

// Row là một dòng dữ liệu. Line là số dòng trong tệp (dòng tiêu đề thường là 1),
// dùng để báo lỗi theo dòng cho người dùng.
type Row struct {
	Line  int
	Cells []string // đã cắt khoảng trắng hai đầu
}

// Table là nội dung tệp.
type Table struct {
	Format string
	Header []string // tiêu đề gốc (đã cắt khoảng trắng, bỏ BOM)
	Keys   []string // tiêu đề đã chuẩn hóa (NormalizeHeader)
	Rows   []Row    // chỉ dòng có ít nhất một ô khác rỗng
}

// Read đọc CSV (UTF-8, có thể có BOM) hoặc .xlsx (nhận theo chữ ký tệp) từ r.
// Dòng đầu tiên khác rỗng là tiêu đề; dòng trống hoàn toàn bị bỏ qua.
func Read(r io.Reader, lim Limits) (*Table, error) {
	lim = lim.withDefaults()
	data, err := io.ReadAll(io.LimitReader(r, lim.MaxBytes+1))
	if err != nil {
		var mbe *http.MaxBytesError // thân request vượt giới hạn của route
		if errors.As(err, &mbe) {
			return nil, ErrTooLarge
		}
		return nil, fmt.Errorf("tabular: đọc tệp: %w", err)
	}
	if int64(len(data)) > lim.MaxBytes {
		return nil, ErrTooLarge
	}
	return ReadBytes(data, lim)
}

// ReadBytes như Read nhưng nhận nội dung đã có trong bộ nhớ.
func ReadBytes(data []byte, lim Limits) (*Table, error) {
	lim = lim.withDefaults()
	if int64(len(data)) > lim.MaxBytes {
		return nil, ErrTooLarge
	}
	var t *Table
	var err error
	switch Detect(data) {
	case FormatXLSX:
		t, err = readXLSX(data, lim)
	case "xls":
		return nil, ErrLegacyExcel
	default:
		t, err = readCSV(data, lim)
	}
	if err != nil {
		return nil, err
	}
	t.Keys = make([]string, len(t.Header))
	for i, h := range t.Header {
		t.Keys[i] = NormalizeHeader(h)
	}
	return t, nil
}

// Detect nhận định dạng theo chữ ký đầu tệp: "xlsx" (gói zip), "xls" (OLE2 cũ) hoặc "csv".
func Detect(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return FormatXLSX
	case bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return "xls"
	}
	return FormatCSV
}

func readCSV(data []byte, lim Limits) (*Table, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // BOM của Excel
	if !utf8.Valid(data) {
		return nil, ErrEncoding
	}
	rd := csv.NewReader(bytes.NewReader(data))
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true
	t := &Table{Format: FormatCSV}
	for {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		line, _ := rd.FieldPos(0)
		cells := trimCells(rec)
		if len(cells) == 0 {
			continue
		}
		if len(cells) > lim.MaxCols {
			return nil, ErrTooManyColumns
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

// trimCells cắt khoảng trắng mỗi ô và bỏ các ô rỗng ở cuối; trả nil nếu mọi ô rỗng.
func trimCells(rec []string) []string {
	out := make([]string, len(rec))
	last := -1
	for i, c := range rec {
		out[i] = strings.TrimSpace(c)
		if out[i] != "" {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	return out[:last+1]
}

// NormalizeHeader chuẩn hóa tiêu đề cột để so với bí danh: bỏ BOM, bỏ dấu tiếng
// Việt, đ → d, chữ thường, mọi chuỗi ký tự không phải chữ/số thành một "_".
// "Số máy/SN" → "so_may_sn", "Mã HTX" → "ma_htx", "cooperative_code" giữ nguyên.
func NormalizeHeader(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "\xef\xbb\xbf"))
	var b strings.Builder
	sep := false
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == 'đ' || r == 'Đ':
			r = 'd'
		}
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
			sep = false
			continue
		}
		sep = true
	}
	return b.String()
}

// Columns ánh xạ tên cột chuẩn → chỉ số cột theo bảng bí danh (khóa là tiêu đề
// đã chuẩn hóa). Cột xuất hiện hai lần: lấy lần đầu.
func (t *Table) Columns(aliases map[string]string) map[string]int {
	col := map[string]int{}
	for i, k := range t.Keys {
		if canon, ok := aliases[k]; ok {
			if _, dup := col[canon]; !dup {
				col[canon] = i
			}
		}
	}
	return col
}

// Missing trả các cột chuẩn bắt buộc chưa có trong col.
func Missing(col map[string]int, required ...string) []string {
	var out []string
	for _, c := range required {
		if _, ok := col[c]; !ok {
			out = append(out, c)
		}
	}
	return out
}

// Get trả ô của cột chuẩn name ("" nếu không có cột hoặc dòng ngắn hơn).
func (r Row) Get(col map[string]int, name string) string {
	i, ok := col[name]
	if !ok || i >= len(r.Cells) {
		return ""
	}
	return r.Cells[i]
}

// Message trả câu tiếng Việt giải thích lỗi đọc tệp (dùng làm detail của problem).
func Message(err error, lim Limits) string {
	lim = lim.withDefaults()
	var fe *FormulaError
	switch {
	case errors.As(err, &fe):
		return "Ô " + fe.Cell + " chứa công thức — hệ thống không tính công thức; hãy dán giá trị (Paste Values) rồi tải lên lại"
	case errors.Is(err, ErrTooLarge):
		return fmt.Sprintf("Tệp vượt dung lượng cho phép (tối đa %d KB)", lim.MaxBytes>>10)
	case errors.Is(err, ErrTooManyRows):
		return fmt.Sprintf("Tối đa %d dòng mỗi lần nhập", lim.MaxRows)
	case errors.Is(err, ErrTooManyColumns):
		return fmt.Sprintf("Tệp có quá nhiều cột (tối đa %d)", lim.MaxCols)
	case errors.Is(err, ErrEncoding):
		return "Tệp CSV phải lưu bằng mã UTF-8 (Excel: Lưu thành → CSV UTF-8)"
	case errors.Is(err, ErrLegacyExcel):
		return "Không hỗ trợ tệp Excel 97-2003 (.xls) — hãy lưu thành .xlsx hoặc CSV UTF-8"
	case errors.Is(err, ErrEmpty):
		return "Tệp không đúng mẫu quy định, vui lòng tải mẫu chuẩn và thử lại"
	}
	return "Sai định dạng tệp — chỉ nhận CSV UTF-8 hoặc Excel .xlsx"
}
