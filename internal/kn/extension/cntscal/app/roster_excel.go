package app

// BF-CN-DUTY (#171, CN-14): nạp và xuất Excel đúng mẫu "Lịch trực ksgm" (BM-03, UR-TINH-10, FN-15 B1 và luồng phụ "Xuất
// Excel đúng mẫu").
//   - Nạp: CHỈ ĐỌC, không ghi. Đọc .xlsx (≤ 4 MB, ≤ 2000 dòng viên chức), chọn sheet đúng tháng, ghép cơ sở (mã/tên) và
//     viên chức (SĐT/tên) trong phạm vi tỉnh, trả ô lưới + lỗi theo dòng + cảnh báo như khi lưu. Web cho xem trước rồi
//     người lập lịch bấm Lưu (source=excel) — đường lưu chạy lại đủ kiểm tra, cảnh báo, thông báo.
//   - Xuất: bản đã lưu của tháng thành .xlsx đúng bố cục mẫu (tiêu đề tháng, dòng căn cứ, ghi chú ca, STT, Tên cơ sở,
//     Địa chỉ, Viên chức, ĐT, ngày 1…n; gộp ô STT/tên/địa chỉ theo cơ sở). Tệp xuất nạp lại được, ra cùng lịch.
// SĐT đọc từ pf/accounts lúc gọi (không lưu, không ghi log); tệp xuất chỉ có SĐT đầy đủ với người lập lịch.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Giới hạn tệp nạp lịch trực.
const (
	MaxRosterImportBytes = 4 << 20
	maxUnzipBytes        = 64 << 20
	maxUnzipXMLBytes     = 16 << 20
)

// ErrRosterImportInvalid: tệp không đọc được hoặc không đúng mẫu BM-03.
var ErrRosterImportInvalid = problem.New(http.StatusUnprocessableEntity, "extension.cnts_roster_import_invalid", "Tệp không đúng mẫu Lịch trực KSGM (BM-03)")

// RosterImport là kết quả đọc tệp (chưa lưu).
type RosterImport struct {
	Month    time.Time
	Province string
	Sheet    string
	Basis    string
	RowCount int
	Cells    []domain.Cell
	Issues   []domain.ImportIssue
	Warnings []domain.Warning
}

// ImportRoster đọc tệp BM-03 cho tháng monthRaw của tỉnh (người lập lịch). Không ghi gì.
func (s *Service) ImportRoster(ctx context.Context, monthRaw, province string, src io.Reader) (RosterImport, error) {
	c, err := s.planner(ctx, "chỉ cán bộ tổng hợp Cấp Tỉnh (hoặc Admin) nạp lịch trực KSGM", accport.GroupTongHop)
	if err != nil {
		return RosterImport{}, err
	}
	c, err = s.rosterScope(ctx, c, province, true)
	if err != nil {
		return RosterImport{}, err
	}
	m, err := s.month(monthRaw)
	if err != nil {
		return RosterImport{}, err
	}
	if s.Officers == nil {
		return RosterImport{}, ErrDependency.WithDetail("Chưa nối danh bạ cán bộ Cấp Xã để ghép viên chức")
	}
	data, err := io.ReadAll(io.LimitReader(src, MaxRosterImportBytes+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return RosterImport{}, ErrRosterImportInvalid.WithDetail("Tệp vượt 4 MB")
		}
		return RosterImport{}, err
	}
	if len(data) > MaxRosterImportBytes {
		return RosterImport{}, ErrRosterImportInvalid.WithDetail("Tệp vượt 4 MB")
	}
	sheet, sh, err := ReadBM03(data, m)
	if err != nil {
		return RosterImport{}, err
	}
	facs, err := s.facilities(ctx, c, m)
	if err != nil {
		return RosterImport{}, err
	}
	offs, idx, err := s.officers(ctx, c, facs)
	if err != nil {
		return RosterImport{}, err
	}
	refs := make([]uuid.UUID, 0, len(offs))
	for _, o := range offs {
		refs = append(refs, o.Ref)
	}
	names, phones := s.names(ctx, refs), s.phonesOf(ctx, c, refs)
	ifs := make([]domain.ImportFacility, 0, len(facs))
	for _, f := range facs {
		ifs = append(ifs, domain.ImportFacility{Ref: f.Ref, Code: f.Code, Name: f.Name})
	}
	ios := make([]domain.ImportOfficer, 0, len(offs))
	for _, o := range offs {
		ios = append(ios, domain.ImportOfficer{Ref: o.Ref, Name: names[o.Ref], Phone: domain.NormalizePhone(phones[o.Ref])})
	}
	cells, issues := domain.MatchImport(m, sh, ifs, ios)
	if issues == nil {
		issues = []domain.ImportIssue{}
	}
	return RosterImport{Month: m, Province: c.province, Sheet: sheet, Basis: sh.Basis, RowCount: len(sh.Rows), Cells: cells, Issues: issues,
		Warnings: domain.RosterWarnings(m, cells, facilitiesOf(facs), grantOf(idx))}, nil
}

// ReadBM03 mở tệp .xlsx (giới hạn giải nén) và trả sheet BM-03 của tháng m: sheet hiển thị đầu tiên có tiêu đề "THÁNG
// M/YYYY" đúng tháng; nếu không sheet nào ghi tháng thì nhận sheet BM-03 duy nhất.
func ReadBM03(data []byte, m time.Time) (string, domain.ImportSheet, error) {
	if len(data) >= 4 && data[0] == 0xd0 && data[1] == 0xcf {
		return "", domain.ImportSheet{}, ErrRosterImportInvalid.WithDetail("Tệp .xls cũ — lưu lại dạng .xlsx")
	}
	if len(data) < 2 || data[0] != 'P' || data[1] != 'K' {
		return "", domain.ImportSheet{}, ErrRosterImportInvalid.WithDetail("Chỉ nhận tệp Excel .xlsx")
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: maxUnzipBytes, UnzipXMLSizeLimit: maxUnzipXMLBytes})
	if err != nil {
		return "", domain.ImportSheet{}, ErrRosterImportInvalid.WithDetail("Không mở được tệp .xlsx")
	}
	defer func() { _ = f.Close() }()
	var undated []string
	var undatedSheet domain.ImportSheet
	var other []string
	for _, name := range f.GetSheetList() {
		if v, _ := f.GetSheetVisible(name); !v {
			continue
		}
		rows, err := sheetRows(f, name)
		if err != nil {
			continue
		}
		sh, ok, err := domain.ParseBM03(rows)
		if errors.Is(err, domain.ErrTooManyRows) {
			return "", domain.ImportSheet{}, ErrRosterImportInvalid.WithDetail(fmt.Sprintf("Sheet \"%s\": %s", name, err.Error()))
		}
		if !ok || err != nil {
			continue
		}
		switch {
		case sh.Month == nil:
			undated, undatedSheet = append(undated, name), sh
		case sh.Month.Equal(m):
			return name, sh, nil
		default:
			other = append(other, sh.Month.Format("1/2006"))
		}
	}
	if len(undated) == 1 && len(other) == 0 {
		return undated[0], undatedSheet, nil
	}
	if len(other) > 0 {
		return "", domain.ImportSheet{}, ErrRosterImportInvalid.WithDetail(fmt.Sprintf("Tệp không có sheet tháng %s (tệp có tháng %s)", m.Format("1/2006"), strings.Join(uniqStr(other), ", ")))
	}
	if len(undated) > 1 {
		return "", domain.ImportSheet{}, ErrRosterImportInvalid.WithDetail("Tệp có nhiều sheet lịch trực không ghi tháng ở tiêu đề — ghi \"THÁNG M/YYYY\" hoặc để một sheet")
	}
	return "", domain.ImportSheet{}, ErrRosterImportInvalid.WithDetail("Không thấy dòng tiêu đề cột STT | TÊN CƠ SỞ GIẾT MỔ | ĐỊA CHỈ | VIÊN CHỨC TRỰC KSGM | ĐT — dùng mẫu \"Lịch trực KSGM\"")
}

// sheetRows đọc tối đa MaxImportScan dòng × MaxImportCols cột, giá trị thô (không theo định dạng số: SĐT, ngày tiêu đề).
func sheetRows(f *excelize.File, name string) ([][]string, error) {
	it, err := f.Rows(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = it.Close() }()
	var out [][]string
	for it.Next() {
		if len(out) >= domain.MaxImportScan {
			break
		}
		cols, err := it.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, err
		}
		if len(cols) > domain.MaxImportCols {
			cols = cols[:domain.MaxImportCols]
		}
		out = append(out, cols)
	}
	return out, it.Error()
}

func uniqStr(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// phonesOf tra SĐT công vụ (đã chuẩn hóa) của viên chức qua pf/accounts. Lỗi tra cứu hoặc chưa khai số → vắng (không
// chặn đọc lưới). Gọi NGOÀI giao dịch DB.
func (s *Service) phonesOf(ctx context.Context, c caller, refs []uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Contacts == nil {
		return out
	}
	for _, r := range refs {
		p, err := s.Contacts.PhoneOf(ctx, c.p.TenantID, r)
		if err != nil {
			continue
		}
		if n := domain.NormalizePhone(p); n != "" {
			out[r] = n
		}
	}
	return out
}

// ExportRoster xuất bản đã lưu của tháng thành .xlsx đúng mẫu BM-03. basis là dòng căn cứ (rỗng → mẫu chờ điền).
func (s *Service) ExportRoster(ctx context.Context, monthRaw, province, basis string) ([]byte, string, error) {
	b, err := domain.CheckBasis(basis)
	if err != nil {
		return nil, "", problem.Validation.WithField("basis", err.Error())
	}
	if exdomain.ContainsPII(b) {
		return nil, "", problem.Validation.WithField("basis", exdomain.ErrPIIInText.Error())
	}
	v, err := s.GetRoster(ctx, monthRaw, province)
	if err != nil {
		return nil, "", err
	}
	data, err := s.rosterXLSX(ctx, v, b)
	if err != nil {
		return nil, "", err
	}
	name := "lich-truc-ksgm-" + domain.MonthKey(v.Month) + ".xlsx"
	if v.Province != "" {
		name = "lich-truc-ksgm-" + v.Province + "-" + domain.MonthKey(v.Month) + ".xlsx"
	}
	return data, name, nil
}

type exportRow struct {
	officer uuid.UUID
	name    string
}

func (s *Service) rosterXLSX(ctx context.Context, v RosterView, basis string) ([]byte, error) {
	days := domain.DaysIn(v.Month)
	shift := map[[2]uuid.UUID]map[int]string{}
	byFac := map[uuid.UUID][]uuid.UUID{}
	for _, x := range v.Cells {
		k := [2]uuid.UUID{x.Facility, x.Officer}
		if shift[k] == nil {
			shift[k] = map[int]string{}
			byFac[x.Facility] = append(byFac[x.Facility], x.Officer)
		}
		shift[k][x.Day] = x.Shift
	}
	facs := append([]FacilityView(nil), v.Facilities...)
	known := map[uuid.UUID]bool{}
	for _, f := range facs {
		known[f.Ref] = true
	}
	var extra []uuid.UUID
	for ref := range byFac {
		if !known[ref] {
			extra = append(extra, ref)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].String() < extra[j].String() })
	for _, ref := range extra { // cơ sở đã ngừng hoạt động sau khi lưu lịch
		facs = append(facs, FacilityView{Ref: ref, Name: "Cơ sở " + ref.String()[:8]})
	}
	nameCount := map[string]int{}
	for _, f := range facs {
		nameCount[domain.Fold(f.Name)]++
	}
	communes := map[string]string{}
	communeName := func(code string) string {
		if code == "" || s.Units == nil {
			return code
		}
		if n, ok := communes[code]; ok {
			return n
		}
		n := code
		if u, err := s.Units.Get(ctx, code, s.today()); err == nil && u.Name != "" {
			n = u.Name
		}
		communes[code] = n
		return n
	}

	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	sh := domain.ExportSheetName(v.Month)
	if err := x.SetSheetName("Sheet1", sh); err != nil {
		return nil, err
	}
	last := 5 + days
	lastCol, _ := excelize.ColumnNumberToName(last)
	border := []excelize.Border{{Type: "left", Color: "000000", Style: 1}, {Type: "right", Color: "000000", Style: 1},
		{Type: "top", Color: "000000", Style: 1}, {Type: "bottom", Color: "000000", Style: 1}}
	title, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 13}, Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}})
	italic, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Italic: true}, Alignment: &excelize.Alignment{Horizontal: "center", WrapText: true}})
	head, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Border: border, Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}})
	text, _ := x.NewStyle(&excelize.Style{Border: border, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	center, _ := x.NewStyle(&excelize.Style{Border: border, Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}})
	set := func(cell string, val any) error { return x.SetCellValue(sh, cell, val) }
	for i, line := range []string{domain.ExportTitle(v.Month), "(" + strings.Trim(basis, "()") + ")", domain.ExportNote} {
		r := i + 1
		if err := set(fmt.Sprintf("A%d", r), line); err != nil {
			return nil, err
		}
		if err := x.MergeCell(sh, fmt.Sprintf("A%d", r), fmt.Sprintf("%s%d", lastCol, r)); err != nil {
			return nil, err
		}
		st := italic
		if r == 1 {
			st = title
		}
		if err := x.SetCellStyle(sh, fmt.Sprintf("A%d", r), fmt.Sprintf("A%d", r), st); err != nil {
			return nil, err
		}
	}
	_ = x.SetRowHeight(sh, 1, 36)
	_ = x.SetRowHeight(sh, 2, 30)
	const hr = 5
	for j, h := range []string{"STT", "TÊN CƠ SỞ GIẾT MỔ", "ĐỊA CHỈ", "VIÊN CHỨC TRỰC KSGM", "ĐT viên chức"} {
		col, _ := excelize.ColumnNumberToName(j + 1)
		if err := set(fmt.Sprintf("%s%d", col, hr), h); err != nil {
			return nil, err
		}
	}
	for d := 1; d <= days; d++ {
		col, _ := excelize.ColumnNumberToName(5 + d)
		if err := set(fmt.Sprintf("%s%d", col, hr), d); err != nil {
			return nil, err
		}
	}
	if err := x.SetCellStyle(sh, fmt.Sprintf("A%d", hr), fmt.Sprintf("%s%d", lastCol, hr), head); err != nil {
		return nil, err
	}
	for col, w := range map[string]float64{"A": 5, "B": 26, "C": 20, "D": 24, "E": 14} {
		_ = x.SetColWidth(sh, col, col, w)
	}
	first, _ := excelize.ColumnNumberToName(6)
	_ = x.SetColWidth(sh, first, lastCol, 4.6)

	r := hr + 1
	for i, f := range facs {
		offs := byFac[f.Ref]
		rows := make([]exportRow, 0, len(offs))
		for _, o := range offs {
			rows = append(rows, exportRow{officer: o, name: v.Names[o]})
		}
		sort.Slice(rows, func(a, b int) bool {
			if fa, fb := domain.Fold(rows[a].name), domain.Fold(rows[b].name); fa != fb {
				return fa < fb
			}
			return rows[a].officer.String() < rows[b].officer.String()
		})
		if len(rows) == 0 {
			rows = []exportRow{{}}
		}
		start := r
		name := f.Name
		if nameCount[domain.Fold(f.Name)] > 1 && f.Code != "" {
			name = f.Name + " (" + f.Code + ")"
		}
		addr := f.Address
		if addr == "" {
			addr = communeName(f.CommuneCode)
		}
		if err := set(fmt.Sprintf("A%d", r), i+1); err != nil {
			return nil, err
		}
		_ = set(fmt.Sprintf("B%d", r), name)
		_ = set(fmt.Sprintf("C%d", r), addr)
		for _, row := range rows {
			if row.officer != uuid.Nil {
				_ = set(fmt.Sprintf("D%d", r), row.name)
				if v.CanViewPhone {
					if p := v.phones[row.officer]; p != "" {
						_ = x.SetCellStr(sh, fmt.Sprintf("E%d", r), p)
					}
				}
				for d, sv := range shift[[2]uuid.UUID{f.Ref, row.officer}] {
					col, _ := excelize.ColumnNumberToName(5 + d)
					_ = set(fmt.Sprintf("%s%d", col, r), domain.ShiftText(sv))
				}
			}
			r++
		}
		end := r - 1
		if end > start {
			for _, col := range []string{"A", "B", "C"} {
				if err := x.MergeCell(sh, fmt.Sprintf("%s%d", col, start), fmt.Sprintf("%s%d", col, end)); err != nil {
					return nil, err
				}
			}
		}
		_ = x.SetCellStyle(sh, fmt.Sprintf("A%d", start), fmt.Sprintf("A%d", end), center)
		_ = x.SetCellStyle(sh, fmt.Sprintf("B%d", start), fmt.Sprintf("E%d", end), text)
		_ = x.SetCellStyle(sh, fmt.Sprintf("%s%d", first, start), fmt.Sprintf("%s%d", lastCol, end), center)
	}
	_ = x.SetPanes(sh, &excelize.Panes{Freeze: true, XSplit: 5, YSplit: hr, TopLeftCell: fmt.Sprintf("%s%d", first, hr+1), ActivePane: "bottomRight"})
	buf, err := x.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
