package app

// BF-TT-FORMS (#107, UP-04) "Nhập từ Excel" trên màn nhập biểu Cấp Xã: đọc tệp .xlsx (đúng bố cục "Tải mẫu Excel":
// tiêu đề → hàng chữ cái cột → dòng loại cột → dòng dữ liệu) hoặc CSV UTF-8 có hàng chữ cái, trả các dòng dữ liệu dạng
// chữ cái cột → giá trị. CHỈ ĐỌC, không ghi: web ghép dòng với xã (cột tên xã) / chỉ tiêu, chỉ điền ô nhập được, người
// dùng xem lại rồi Lưu nháp / Gửi (máy chủ kiểm lại khi nộp). Không đọc công thức (giá trị đã lưu của ô được dùng).
// Ô số của .xlsx trả dạng thập phân dấu chấm và được đánh dấu `numeric` (xem markNumericCells).

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Giới hạn tệp nhập lưới.
const (
	MaxGridImportBytes = 4 << 20
	MaxGridImportRows  = 2000
	maxGridImportCols  = 400
)

// ErrGridImportInvalid: tệp không đọc được hoặc không có hàng chữ cái cột.
var ErrGridImportInvalid = problem.New(http.StatusUnprocessableEntity, "extension.grid_import_invalid", "Tệp không đúng bố cục mẫu Excel của biểu")

// GridImportRow là một dòng dữ liệu của tệp: số dòng trong tệp và ô khác rỗng theo chữ cái cột. Numeric liệt kê (theo
// thứ tự cột) các ô là ô SỐ của .xlsx: giá trị đã chuẩn hóa dạng thập phân dấu chấm ("1.234" là một phẩy hai ba bốn),
// web KHÔNG được đọc lại kiểu số VN; ô còn lại là chữ (CSV luôn là chữ) — web mới chuẩn hóa "1.234,5" → 1234.5.
type GridImportRow struct {
	Line    int               `json:"line"`
	Cells   map[string]string `json:"cells"`
	Numeric []string          `json:"numeric"`
}

// GridImport là kết quả đọc tệp.
type GridImport struct {
	Sheet string
	Rows  []GridImportRow
}

var gridKindLabels = map[string]bool{"cố định": true, "tỉnh giao": true, "nhập": true, "kỳ": true, "ƒx": true, "fx": true, "công thức": true}

// ImportGrid đọc tệp cho màn nhập của công việc taskID (Cấp Xã; công việc phải trong phạm vi). Không ghi gì.
func (s *Service) ImportGrid(ctx context.Context, taskID uuid.UUID, src io.Reader) (GridImport, error) {
	a, err := s.writer(ctx, port.SessionRoleCommune)
	if err != nil {
		return GridImport{}, err
	}
	if err := s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		_, err := s.taskInScope(ctx, tx, a, taskID, false)
		return err
	}); err != nil {
		return GridImport{}, err
	}
	data, err := io.ReadAll(io.LimitReader(src, MaxGridImportBytes+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return GridImport{}, ErrGridImportInvalid.WithDetail("Tệp vượt 4 MB")
		}
		return GridImport{}, err
	}
	if len(data) > MaxGridImportBytes {
		return GridImport{}, ErrGridImportInvalid.WithDetail("Tệp vượt 4 MB")
	}
	return ParseGridImport(data)
}

// ParseGridImport đọc .xlsx (sheet đầu tiên có hàng chữ cái cột) hoặc CSV UTF-8.
func ParseGridImport(data []byte) (GridImport, error) {
	if len(data) >= 4 && data[0] == 0xd0 && data[1] == 0xcf {
		return GridImport{}, ErrGridImportInvalid.WithDetail("Tệp .xls cũ — lưu lại dạng .xlsx")
	}
	if len(data) >= 2 && data[0] == 'P' && data[1] == 'K' {
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return GridImport{}, ErrGridImportInvalid.WithDetail("Không mở được tệp .xlsx")
		}
		defer func() { _ = f.Close() }()
		for _, sh := range f.GetSheetList() {
			if v, _ := f.GetSheetVisible(sh); !v {
				continue
			}
			rows, err := f.GetRows(sh)
			if err != nil {
				continue
			}
			if out, ok := gridRows(rows); ok {
				out.Sheet = sh
				if err := checkGridSize(out); err != nil {
					return GridImport{}, err
				}
				markNumericCells(f, sh, out.Rows)
				return out, nil
			}
		}
		return GridImport{}, ErrGridImportInvalid.WithDetail("Không thấy hàng chữ cái cột (A, B, C…) — dùng tệp \"Tải mẫu Excel\" của biểu")
	}
	if !utf8.Valid(data) {
		return GridImport{}, ErrGridImportInvalid.WithDetail("CSV phải mã hóa UTF-8")
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return GridImport{}, ErrGridImportInvalid.WithDetail("CSV không hợp lệ")
	}
	out, ok := gridRows(rows)
	if !ok {
		return GridImport{}, ErrGridImportInvalid.WithDetail("Không thấy hàng chữ cái cột (A, B, C…) — dùng tệp \"Tải mẫu Excel\" của biểu")
	}
	return out, checkGridSize(out)
}

func checkGridSize(g GridImport) error {
	if len(g.Rows) > MaxGridImportRows {
		return ErrGridImportInvalid.WithDetail("Tệp quá 2000 dòng dữ liệu")
	}
	return nil
}

// gridRows tìm hàng chữ cái cột (A, B, C… liên tiếp từ cột A, ít nhất 3 cột), bỏ dòng loại cột ngay sau, lấy các dòng
// dữ liệu khác rỗng tới dòng "Yêu cầu biểu mẫu".
func gridRows(rows [][]string) (GridImport, bool) {
	letters := -1
	var cols []string
	for i, r := range rows {
		if ls := lettersOf(r); len(ls) >= 3 {
			letters, cols = i, ls
			break
		}
	}
	if letters < 0 {
		return GridImport{}, false
	}
	out := GridImport{Rows: []GridImportRow{}}
	for i := letters + 1; i < len(rows); i++ {
		r := rows[i]
		first := ""
		for _, c := range r {
			if t := strings.TrimSpace(c); t != "" {
				first = t
				break
			}
		}
		if first == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(first, "*"), "Yêu cầu biểu mẫu") {
			break
		}
		if i == letters+1 && isGridKindRow(r) {
			continue
		}
		cells := map[string]string{}
		for j, c := range r {
			if j >= len(cols) {
				break
			}
			if t := strings.TrimSpace(c); t != "" {
				cells[cols[j]] = t
			}
		}
		if len(cells) > 0 {
			out.Rows = append(out.Rows, GridImportRow{Line: i + 1, Cells: cells, Numeric: []string{}})
		}
	}
	return out, true
}

// markNumericCells (Codex P1 #107): GetRows trả giá trị ĐÃ ĐỊNH DẠNG theo máy chủ ("1.234", "1,234.50"…), không phân
// biệt ô số với ô chữ. Với mỗi ô của dòng dữ liệu, ô kiểu số (không có t / t="n", kể cả công thức ra số) được thay bằng
// giá trị thô chuẩn hóa dạng thập phân dấu chấm bằng decimal (canonicalNumber; 1.234 → "1.234", 1E-3 → "0.001",
// 1234.5 định dạng "#,##0.00" → "1234.5") và ghi chữ cái cột vào Numeric. Ô chữ / chuỗi công thức / logic giữ nguyên.
func markNumericCells(f *excelize.File, sh string, rows []GridImportRow) {
	for i := range rows {
		r := &rows[i]
		for _, l := range sortedLetters(r.Cells) {
			ref := l + strconv.Itoa(r.Line)
			typ, err := f.GetCellType(sh, ref)
			if err != nil || (typ != excelize.CellTypeUnset && typ != excelize.CellTypeNumber) {
				continue
			}
			raw, err := f.GetCellValue(sh, ref, excelize.Options{RawCellValue: true})
			if err != nil {
				continue
			}
			v, ok := canonicalNumber(raw)
			if !ok {
				continue
			}
			r.Cells[l] = v
			r.Numeric = append(r.Numeric, l)
		}
	}
}

// excelSignificantDigits: Excel chỉ giữ 15 chữ số có nghĩa; phần đuôi kiểu "0.30000000000000004" là nhiễu nhị phân.
const excelSignificantDigits = 15

// canonicalNumber chuẩn hóa giá trị thô của ô số .xlsx bằng decimal (R-10: không float trong reportcycle): đọc chuỗi
// ("1.234", "1E-3", "1.2340000000000001"), làm tròn còn 15 chữ số có nghĩa như Excel, in dạng thập phân dấu chấm, bỏ số 0
// thừa ("1.234", "0.001", "1234.5"). Chuỗi không phải số (rỗng, NaN, INF…) → false.
func canonicalNumber(raw string) (string, bool) {
	d, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if d.IsZero() {
		return "0", true
	}
	// Vị trí chữ số có nghĩa đầu tiên: 10^msd ≤ |d| < 10^(msd+1).
	coef := d.Coefficient() // bản sao
	msd := int32(len(coef.Abs(coef).String())) - 1 + d.Exponent()
	return d.Round(excelSignificantDigits - 1 - msd).String(), true
}

// sortedLetters: chữ cái cột theo thứ tự cột (A, B, …, Z, AA, …).
func sortedLetters(cells map[string]string) []string {
	out := make([]string, 0, len(cells))
	for l := range cells {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

func lettersOf(r []string) []string {
	var out []string
	for j, c := range r {
		if j >= maxGridImportCols {
			break
		}
		want := columnLetter(j + 1)
		if strings.TrimSpace(c) != want {
			break
		}
		out = append(out, want)
	}
	return out
}

func isGridKindRow(r []string) bool {
	n := 0
	for _, c := range r {
		t := strings.ToLower(strings.TrimSpace(c))
		if t == "" {
			continue
		}
		if !gridKindLabels[t] {
			return false
		}
		n++
	}
	return n > 0
}

func columnLetter(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}
