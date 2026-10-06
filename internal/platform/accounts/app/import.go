package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/unicode/norm"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxImportRows: số dòng tối đa xử lý đồng bộ trong một request.
const MaxImportRows = 200

// MaxImportBytes giới hạn tệp nhập tài khoản (CSV/XLSX) — handler giới hạn thân request bằng cùng hằng;
// readTable tự giới hạn lần nữa để người gọi khác (job, kiểm thử) không đọc nguồn không giới hạn vào bộ nhớ.
const MaxImportBytes = 4 << 20

// Định dạng tệp nhập.
const (
	FormatCSV  = "csv"
	FormatXLSX = "xlsx"
)

// Trạng thái từng dòng.
const (
	RowCreated  = "created"
	RowRejected = "rejected"
)

// ImportRow là kết quả một dòng. Reason không chứa dữ liệu cá nhân (R-14).
type ImportRow struct {
	Row            int
	Status         string
	Reason         string
	UserID         uuid.UUID
	ActivationSent bool
}

// ImportResult là kết quả cả tệp (KN US-ADM-02, HTX US-ACC-02 AC-2).
type ImportResult struct {
	Created, Rejected int
	Rows              []ImportRow
}

// Cột của tệp mẫu; tên tiếng Việt không dấu là bí danh.
var importAliases = map[string]string{
	"username": "username", "ten_dang_nhap": "username",
	"email":     "email",
	"full_name": "full_name", "ho_ten": "full_name",
	"first_name": "first_name", "ten": "first_name",
	"last_name": "last_name", "ho": "last_name", "ho_dem": "last_name",
	"phone": "phone", "so_dien_thoai": "phone", "sdt": "phone",
	"org_ref": "org_ref", "ma_to_chuc": "org_ref", "don_vi": "org_ref",
	"roles": "roles", "vai_tro": "roles",
}

var errBadTemplate = ErrImportInvalid.WithDetail("Tệp không đúng mẫu quy định, vui lòng tải mẫu chuẩn và thử lại")

// readTable đọc bảng (dòng đầu là tiêu đề) từ CSV UTF-8 hoặc XLSX (trang tính đầu tiên).
func readTable(format string, src io.Reader) ([][]string, error) {
	if format != FormatCSV && format != FormatXLSX {
		return nil, ErrImportInvalid.WithDetail("chỉ hỗ trợ CSV (text/csv) hoặc Excel .xlsx")
	}
	raw, err := io.ReadAll(io.LimitReader(src, MaxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxImportBytes {
		return nil, ErrImportInvalid.WithDetail("Tệp vượt " + strconv.Itoa(MaxImportBytes>>20) + " MB")
	}
	switch format {
	case FormatCSV:
		rd := csv.NewReader(bytes.NewReader(raw))
		rd.FieldsPerRecord = -1
		rd.TrimLeadingSpace = true
		rows, err := rd.ReadAll()
		if err != nil {
			return nil, errBadTemplate
		}
		return rows, nil
	case FormatXLSX:
		// Giới hạn giải nén chống zip bomb.
		f, err := excelize.OpenReader(bytes.NewReader(raw), excelize.Options{UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 16 << 20})
		if err != nil {
			return nil, errBadTemplate
		}
		defer func() { _ = f.Close() }()
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, errBadTemplate
		}
		rows, err := f.GetRows(sheets[0])
		if err != nil {
			return nil, errBadTemplate
		}
		return rows, nil
	}
	return nil, ErrImportInvalid.WithDetail("chỉ hỗ trợ CSV (text/csv) hoặc Excel .xlsx")
}

// normHeader đưa tiêu đề cột về dạng không dấu, chữ thường, nối bằng "_" ("Vai trò" → vai_tro, "SĐT" → sdt).
func normHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, string(rune(0xFEFF)))))
	h = strings.NewReplacer("đ", "d").Replace(h)
	var b strings.Builder
	for _, r := range norm.NFD.String(h) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), "_")
}

// Import nạp tài khoản hàng loạt từ CSV hoặc XLSX. Tệp sai mẫu → từ chối cả tệp;
// dòng lỗi bị loại kèm lý do, dòng hợp lệ vẫn được tạo và nhận liên kết kích hoạt.
// Khóa idem của dòng n là <Idempotency-Key>:row:<n>.
func (s *Service) Import(ctx context.Context, format string, src io.Reader, idemKey string) (ImportResult, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	if err := requireKey(idemKey); err != nil {
		return ImportResult{}, err
	}
	table, err := readTable(format, src)
	if err != nil {
		return ImportResult{}, err
	}
	if len(table) < 2 {
		return ImportResult{}, ErrImportInvalid.WithDetail("Tệp không có dòng dữ liệu")
	}
	col := map[string]int{}
	for i, h := range table[0] {
		if c, ok := importAliases[normHeader(h)]; ok {
			col[c] = i
		}
	}
	_, hasFull := col["full_name"]
	_, hasFirst := col["first_name"]
	for _, req := range []string{"email", "org_ref", "roles"} {
		if _, ok := col[req]; !ok {
			return ImportResult{}, errBadTemplate.WithDetail("Tệp thiếu cột bắt buộc: " + req)
		}
	}
	if !hasFull && !hasFirst {
		return ImportResult{}, errBadTemplate.WithDetail("Tệp thiếu cột bắt buộc: full_name (hoặc first_name/last_name)")
	}
	data := table[1:]
	// Bỏ dòng trống ở cuối (Excel hay để lại).
	for len(data) > 0 && strings.Join(data[len(data)-1], "") == "" {
		data = data[:len(data)-1]
	}
	if len(data) > MaxImportRows {
		return ImportResult{}, ErrImportInvalid.WithDetail("Tối đa " + strconv.Itoa(MaxImportRows) + " dòng mỗi lần nhập")
	}
	get := func(rec []string, c string) string {
		i, ok := col[c]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var res ImportResult
	seen := map[string]int{}
	for i, rec := range data {
		rowNo := i + 2
		row := ImportRow{Row: rowNo, Status: RowRejected}
		reject := func(reason string) {
			row.Reason = reason
			res.Rejected++
			res.Rows = append(res.Rows, row)
		}
		if strings.Join(rec, "") == "" {
			reject("Dòng trống")
			continue
		}
		prof := domain.Profile{
			Username: get(rec, "username"), Email: get(rec, "email"), Phone: get(rec, "phone"),
			FirstName: get(rec, "first_name"), LastName: get(rec, "last_name"),
		}
		if full := get(rec, "full_name"); full != "" && prof.FirstName == "" && prof.LastName == "" {
			prof.FirstName, prof.LastName = domain.SplitFullName(full)
		}
		norm, err := prof.Normalize()
		if err != nil {
			reject(reasonOf(err))
			continue
		}
		org, err := uuid.Parse(get(rec, "org_ref"))
		if err != nil || org == uuid.Nil {
			reject("Sai định dạng mã tổ chức (org_ref)")
			continue
		}
		roles := strings.FieldsFunc(get(rec, "roles"), func(r rune) bool { return r == ',' || r == ';' || r == '|' || r == ' ' })
		dup := ""
		for _, k := range []string{"u:" + norm.Username, "e:" + norm.Email, "p:" + norm.Phone} {
			if k == "p:" {
				continue
			}
			if prev, ok := seen[k]; ok {
				dup = "Trùng với dòng " + strconv.Itoa(prev) + " trong tệp"
				break
			}
		}
		if dup != "" {
			reject(dup)
			continue
		}
		for _, k := range []string{"u:" + norm.Username, "e:" + norm.Email, "p:" + norm.Phone} {
			if k != "p:" {
				seen[k] = rowNo
			}
		}
		out, err := s.create(ctx, a, CreateInput{Profile: norm, OrgRef: org, Roles: roles, IdemKey: idemKey + ":row:" + strconv.Itoa(rowNo)})
		if err != nil {
			p := problem.From(err)
			if p.Status >= 500 {
				// Lỗi hệ thống giữa chừng: trả lỗi để client gửi lại cùng khóa;
				// các dòng đã tạo được phát lại theo khóa idem của dòng.
				return ImportResult{}, err
			}
			reject(reasonOf(err))
			continue
		}
		row.Status, row.UserID, row.ActivationSent = RowCreated, out.User.ID, out.ActivationSent
		res.Created++
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// reasonOf quy lỗi về nhãn lý do theo AC (HTX US-ACC-02 AC-2), không chứa dữ liệu nhập.
func reasonOf(err error) string {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return fe.Msg
	}
	p := problem.From(err)
	switch p.Code {
	case ErrPhoneTaken.Code:
		return "SĐT đã tồn tại"
	case ErrUserExists.Code:
		return "Tên đăng nhập hoặc Email đã tồn tại"
	case ErrOrgInvalid.Code:
		return "Không có tổ chức với org_ref này"
	case ErrOrgInactive.Code:
		return "Tổ chức đã bị vô hiệu hóa"
	case ErrOutOfScope.Code:
		return "Tổ chức ngoài phạm vi phụ trách"
	case ErrNotGrantable.Code:
		return "Vai trò vượt quyền cấp của bạn"
	}
	for _, f := range p.Errors {
		return f.Reason
	}
	if p.Detail != "" {
		return p.Detail
	}
	return p.Title
}
