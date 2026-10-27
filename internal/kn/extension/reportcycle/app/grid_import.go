package app

// BF-TT-FORMS (#107, UP-04) "Nhập từ Excel" trên màn nhập biểu Cấp Xã: đọc tệp .xlsx (đúng bố cục "Tải mẫu Excel":
// tiêu đề → hàng chữ cái cột → dòng loại cột → dòng dữ liệu) hoặc CSV UTF-8 có hàng chữ cái, trả các dòng dữ liệu dạng
// chữ cái cột → giá trị. CHỈ ĐỌC, không ghi: web ghép dòng với xã (cột tên xã) / chỉ tiêu, chỉ điền ô nhập được, người
// dùng xem lại rồi Lưu nháp / Gửi (máy chủ kiểm lại khi nộp). Không đọc công thức (giá trị đã lưu của ô được dùng).

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// GridImportRow là một dòng dữ liệu của tệp: số dòng trong tệp và ô khác rỗng theo chữ cái cột.
type GridImportRow struct {
	Line  int
	Cells map[string]string
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
				return out, checkGridSize(out)
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
			out.Rows = append(out.Rows, GridImportRow{Line: i + 1, Cells: cells})
		}
	}
	return out, true
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
