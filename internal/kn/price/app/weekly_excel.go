package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

const weeklyMaxFile = 5 << 20

type weeklyFileRow struct {
	line  int
	name  string
	patch domain.WeeklyPatch
	err   string
}

// WeeklyPreview is the server-validated import result. Invalid rows do not block valid rows.
type WeeklyPreview struct {
	Version       int64                `json:"version"`
	WeekID        string               `json:"week_id"`
	SourceColumns string               `json:"source_columns"`
	SHA256        string               `json:"sha256"`
	Rows          []domain.WeeklyIssue `json:"rows"`
	ValidRows     int                  `json:"valid_rows"`
	OverwriteRows int                  `json:"overwrite_rows"`
	ErrorRows     int                  `json:"error_rows"`
	patches       []domain.WeeklyPatch
}

func weeklyImportRows(data []byte, program string, week int) ([]weeklyFileRow, string, error) {
	if len(data) == 0 || len(data) > weeklyMaxFile {
		return nil, "", tabular.ErrTooLarge
	}
	var records []tabular.Row
	var file *excelize.File
	sheet := ""
	if tabular.Detect(data) == tabular.FormatXLSX {
		f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true, UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 16 << 20})
		if err != nil {
			return nil, "", tabular.ErrFormat
		}
		defer func() { _ = f.Close() }()
		file = f
		names := f.GetSheetList()
		if len(names) == 0 || len(names) > 20 {
			return nil, "", tabular.ErrFormat
		}
		for index, name := range names {
			raw, err := f.GetRows(name, excelize.Options{RawCellValue: true})
			if err != nil {
				return nil, "", tabular.ErrFormat
			}
			if len(raw) > 2000 {
				return nil, "", tabular.ErrTooManyRows
			}
			matches := false
			for _, row := range raw[:min(30, len(raw))] {
				for _, value := range row {
					if tabular.NormalizeHeader(value) == tabular.NormalizeHeader(domain.WeeklyTitle(program)) {
						matches = true
					}
				}
			}
			if index == 0 || matches {
				sheet = name
				records = nil
				for j, row := range raw {
					if len(row) > 80 {
						return nil, "", tabular.ErrTooManyColumns
					}
					records = append(records, tabular.Row{Line: j + 1, Cells: row})
				}
			}
			if matches {
				break
			}
		}
	} else {
		t, err := tabular.ReadBytes(data, tabular.Limits{MaxBytes: weeklyMaxFile, MaxRows: 2000, MaxCols: 80})
		if err != nil {
			return nil, "", err
		}
		records = append([]tabular.Row{{Line: 1, Cells: t.Header}}, t.Rows...)
	}
	h, nameAt := -1, -1
	for j, row := range records[:min(30, len(records))] {
		for c, value := range row.Cells {
			n := tabular.NormalizeHeader(value)
			if strings.Contains(n, "ten_chuong_trinh") || strings.Contains(n, "ten_hang_muc") {
				h, nameAt = j, c
				break
			}
		}
		if h >= 0 {
			break
		}
	}
	if h < 0 {
		return nil, "", fmt.Errorf("không tìm thấy cột Tên chương trình khuyến nông/Tên hạng mục")
	}
	progress, money, evaluation, note := -1, -1, -1, -1
	selected := false
	head := records[h].Cells
	isProgress := func(value string) bool {
		n := tabular.NormalizeHeader(value)
		return strings.Contains(n, "tien_do") && strings.Contains(n, "tuan") && !strings.Contains(n, "luy_ke")
	}
	for c, value := range head {
		n := tabular.NormalizeHeader(value)
		matchWeek := strings.Contains(n, fmt.Sprintf("tuan_%d_", week)) || strings.HasSuffix(n, fmt.Sprintf("tuan_%d", week))
		if isProgress(value) && (!selected || matchWeek) {
			progress, selected = c, matchWeek
		}
		if strings.Contains(n, "ghi_chu") {
			note = c
		}
	}
	// Keep all weekly inputs inside the selected period; never mix money/evaluation from another week.
	periodEnd := len(head)
	for c := progress + 1; c < len(head); c++ {
		if isProgress(head[c]) {
			periodEnd = c
			break
		}
	}
	if progress >= 0 && progress+1 < periodEnd && strings.TrimSpace(head[progress+1]) == "" {
		money = progress + 1
	}
	for c := progress + 1; progress >= 0 && c < periodEnd; c++ {
		n := tabular.NormalizeHeader(head[c])
		if strings.Contains(n, "luy_ke") {
			continue
		}
		if strings.Contains(n, "kinh_phi") {
			money = c
		}
		if strings.Contains(n, "danh_gia") {
			evaluation = c
		}
	}
	if progress < 0 || money < 0 {
		return nil, "", fmt.Errorf("thiếu cột Tiến độ thực hiện tuần/Kinh phí thực hiện tuần cùng kỳ")
	}
	columnLabel := func(c int) string {
		if c < 0 {
			return "không có"
		}
		column, _ := excelize.ColumnNumberToName(c + 1)
		return strings.TrimSpace(column + " " + strings.Join(strings.Fields(head[c]), " "))
	}
	source := fmt.Sprintf("Tiến độ: %s; kinh phí: %s; đánh giá: %s.", columnLabel(progress), columnLabel(money), columnLabel(evaluation))
	if selected {
		source = fmt.Sprintf("Số liệu tuần %d. %s", week, source)
	} else {
		source = fmt.Sprintf("Không có cột tiến độ ghi rõ tuần %d; dùng cặp tuần cuối trong tệp. %s", week, source)
	}
	cell := func(row tabular.Row, col int) string {
		if col < 0 || col >= len(row.Cells) {
			return ""
		}
		return strings.TrimSpace(row.Cells[col])
	}
	var result []weeklyFileRow
	for _, row := range records[h+1:] {
		name := cell(row, nameAt)
		if name == "" || tabular.NormalizeHeader(name) == tabular.NormalizeHeader(domain.WeeklyTitle(program)) {
			continue
		}
		r := weeklyFileRow{line: row.Line, name: name}
		for _, field := range []struct {
			col    int
			target **string
		}{{progress, &r.patch.Progress}, {money, &r.patch.Money}, {evaluation, &r.patch.Evaluation}, {note, &r.patch.Note}} {
			if field.col < 0 {
				continue
			}
			value := cell(row, field.col)
			if file != nil {
				ref, _ := excelize.CoordinatesToCellName(field.col+1, row.Line)
				formula, err := file.GetCellFormula(sheet, ref)
				if err != nil {
					return nil, "", tabular.ErrFormat
				}
				if formula != "" {
					r.err = "cột nhập liệu chứa công thức; nhập giá trị số thay cho công thức"
				}
			}
			if value != "" {
				*field.target = &value
			}
		}
		result = append(result, r)
	}
	if len(result) == 0 {
		return nil, "", tabular.ErrEmpty
	}
	return result, source, nil
}

func makeWeeklyPreview(v WeeklyView, data []byte) (WeeklyPreview, error) {
	hash := sha256.Sum256(data)
	p := WeeklyPreview{Version: v.Version, WeekID: v.Report.ID, SHA256: hex.EncodeToString(hash[:]), Rows: []domain.WeeklyIssue{}}
	rows, source, err := weeklyImportRows(data, v.Program, v.Report.Number)
	if err != nil {
		return p, problem.Validation.WithField("file", err.Error())
	}
	p.SourceColumns = source
	counts := map[string]int{}
	for _, row := range rows {
		counts[tabular.NormalizeHeader(row.name)]++
	}
	for _, r := range rows {
		issue := domain.WeeklyIssue{Line: r.line, Name: r.name, Status: "valid", Errors: []string{}}
		at := slices.IndexFunc(v.Report.Rows, func(i domain.WeeklyRow) bool {
			return tabular.NormalizeHeader(i.Name) == tabular.NormalizeHeader(r.name)
		})
		switch {
		case r.err != "":
			issue.Errors = append(issue.Errors, r.err)
		case at < 0:
			issue.Errors = append(issue.Errors, "tên hạng mục không có trong danh mục")
		case counts[tabular.NormalizeHeader(r.name)] > 1:
			issue.Errors = append(issue.Errors, "hạng mục xuất hiện nhiều lần trong tệp")
		case v.Report.Rows[at].Group:
			issue.Errors = append(issue.Errors, "mục nhóm không nhập số liệu")
		default:
			item := v.Report.Rows[at].WeeklyItem
			r.patch.ID = item.ID
			trial := domain.WeeklyReport{Items: []domain.WeeklyItem{item}}
			if err := domain.ApplyWeeklyPatches(&trial, nil, v.Program, []domain.WeeklyPatch{r.patch}); err != nil {
				issue.Errors = append(issue.Errors, err.Error())
			}
			if len(issue.Errors) == 0 {
				next := trial.Items[0]
				if (item.ProgressSet && !next.Progress.Equal(item.Progress)) || (item.MoneySet && !next.Money.Equal(item.Money)) || (item.Evaluation != "" && next.Evaluation != item.Evaluation) || (item.Note != "" && next.Note != item.Note) {
					issue.Status = "overwrite"
					p.OverwriteRows++
				}
				p.patches = append(p.patches, r.patch)
				p.ValidRows++
			}
		}
		if len(issue.Errors) > 0 {
			issue.Status = "error"
			p.ErrorRows++
		}
		p.Rows = append(p.Rows, issue)
	}
	return p, nil
}

// PreviewImport parses the file only after authorization and outside any DB transaction.
func (s *WeeklyService) PreviewImport(ctx context.Context, profile, program, weekID string, version int64, data []byte) (WeeklyPreview, error) {
	if err := s.AuthorizeWeekly(ctx, profile); err != nil {
		return WeeklyPreview{}, err
	}
	v, err := s.Read(ctx, profile, program, weekID)
	if err != nil {
		return WeeklyPreview{}, err
	}
	if v.Report.Status != "draft" {
		return WeeklyPreview{}, port.ErrWeeklyClosed
	}
	if v.Version != version {
		return WeeklyPreview{}, port.ErrWeeklyStale
	}
	return makeWeeklyPreview(v, data)
}

// CommitImport reparses and validates the same file outside the write transaction, then applies CAS.
func (s *WeeklyService) CommitImport(ctx context.Context, profile, program, weekID, idem string, version int64, sha string, confirm bool, data []byte) (WeeklyView, error) {
	// Authorization precedes even digest comparison and replay.
	if err := s.AuthorizeWeekly(ctx, profile); err != nil {
		return WeeklyView{}, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != sha {
		return WeeklyView{}, port.ErrWeeklyImportChanged
	}
	v, err := s.Read(ctx, profile, program, weekID)
	if err != nil {
		return WeeklyView{}, err
	}
	// Replayed imports still validate current access, but Write handles their prior version atomically.
	p, err := makeWeeklyPreview(v, data)
	if err != nil {
		return WeeklyView{}, err
	}
	if p.OverwriteRows > 0 && !confirm {
		return WeeklyView{}, port.ErrWeeklyConfirm.WithDetail("các ô khác số liệu hiện tại sẽ bị ghi đè; cần xác nhận")
	}
	in := WeeklyCommand{Version: version, WeekID: weekID, Action: "import", Rows: p.patches, Confirm: confirm, Issues: p.Rows, FileHash: sha}
	return s.Write(ctx, profile, program, idem, in)
}

// ExportWeekly renders the selected report or a blank-data template after its DB snapshot has closed.
func (s *WeeklyService) ExportWeekly(ctx context.Context, profile, program, weekID string, template bool) (Export, error) {
	if err := s.AuthorizeWeekly(ctx, profile); err != nil {
		return Export{}, err
	}
	v, err := s.Read(ctx, profile, program, weekID)
	if err != nil {
		return Export{}, err
	}
	return renderWeekly(v, template)
}

func renderWeekly(v WeeklyView, template bool) (Export, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "Sheet1"
	last := "H"
	if v.Program == "information" {
		last = "I"
	}
	var writeErr error
	text := func(ref, value string) {
		if writeErr == nil {
			writeErr = f.SetCellStr(sheet, ref, value)
		}
	}
	num := func(ref, value string) {
		if writeErr == nil {
			writeErr = f.SetCellDefault(sheet, ref, value)
		}
	}
	merge := func(from, to string) {
		if writeErr == nil {
			writeErr = f.MergeCell(sheet, from, to)
		}
	}
	text("A1", strings.ToUpper(v.Title))
	merge("A1", last+"1")
	text("A2", fmt.Sprintf("Tuần %d, đến ngày %s — Quyết định 2855/QĐ-UBND ngày 19/6/2026", v.Report.Number, v.Report.To))
	merge("A2", last+"2")
	for col, value := range map[string]string{"A": "TT", "B": "TÊN CHƯƠNG TRÌNH KHUYẾN NÔNG", "C": "QUY MÔ", "D": fmt.Sprintf("TIẾN ĐỘ THỰC HIỆN TUẦN %d (đến ngày %s)", v.Report.Number, v.Report.To), "F": fmt.Sprintf("ĐÁNH GIÁ TUẦN %d (khó khăn, vướng mắc, đề xuất, kế hoạch thời gian tới)", v.Report.Number), "G": "LŨY KẾ"} {
		text(col+"4", value)
	}
	for _, col := range []string{"A", "B", "C", "F"} {
		merge(col+"4", col+"5")
	}
	merge("D4", "E4")
	merge("G4", "H4")
	text("D5", "Tiến độ thực hiện")
	text("E5", "Kinh phí thực hiện (đồng)")
	text("G5", "Tiến độ thực hiện")
	text("H5", "Kinh phí thực hiện (đồng)")
	if v.Program == "information" {
		text("I4", "GHI CHÚ")
		merge("I4", "I5")
	}
	borders := []excelize.Border{{Type: "left", Color: "404040", Style: 1}, {Type: "right", Color: "404040", Style: 1}, {Type: "top", Color: "404040", Style: 1}, {Type: "bottom", Color: "404040", Style: 1}}
	bodyStyle, err := f.NewStyle(&excelize.Style{Border: borders, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	if err != nil {
		return Export{}, err
	}
	headerStyle, err := f.NewStyle(&excelize.Style{Border: borders, Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"E2F0D9"}, Pattern: 1}, Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}})
	if err != nil {
		return Export{}, err
	}
	groupStyle, err := f.NewStyle(&excelize.Style{Border: borders, Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	if err != nil {
		return Export{}, err
	}
	for j, row := range v.Report.Rows {
		r := strconv.Itoa(j + 6)
		text("A"+r, row.Number)
		text("B"+r, row.Name)
		if row.ParentID != "" {
			text("B"+r, "    "+row.Name)
		}
		if row.Group {
			merge("B"+r, last+r)
			if err := f.SetCellStyle(sheet, "A"+r, last+r, groupStyle); err != nil {
				return Export{}, err
			}
			continue
		}
		plan := row.Plan.String()
		if row.AdjustedPlan != nil {
			plan += " (điều chỉnh: " + row.AdjustedPlan.String() + ")"
		}
		text("C"+r, plan)
		if !template {
			num("D"+r, row.Progress.String())
			num("E"+r, row.Money.String())
			text("F"+r, row.Evaluation)
			planValue := row.Plan
			if row.AdjustedPlan != nil {
				planValue = *row.AdjustedPlan
			}
			cumulative := row.CumulativeProgress.String() + " / " + planValue.String()
			if row.Rate != nil {
				cumulative += "; đạt " + row.Rate.StringFixed(2) + "% KH"
			}
			text("G"+r, cumulative)
			num("H"+r, row.CumulativeMoney.String())
			note := row.Note
			if row.PlanReason != "" {
				if note != "" {
					note += "; "
				}
				note += "Điều chỉnh kế hoạch: " + row.PlanReason
			}
			if v.Program == "information" {
				text("I"+r, note)
			} else if row.PlanReason != "" {
				text("C"+r, plan+"; "+row.PlanReason)
			}
		}
		if err := f.SetCellStyle(sheet, "A"+r, last+r, bodyStyle); err != nil {
			return Export{}, err
		}
	}
	if writeErr != nil {
		return Export{}, writeErr
	}
	for _, area := range [][2]string{{"A1", last + "2"}, {"A4", last + "5"}} {
		if err := f.SetCellStyle(sheet, area[0], area[1], headerStyle); err != nil {
			return Export{}, err
		}
	}
	for _, set := range []func() error{
		func() error { return tabular.SetColumnWidth(f, sheet, "A", "A", 6) }, func() error { return tabular.SetColumnWidth(f, sheet, "B", "B", 56) },
		func() error { return tabular.SetColumnWidth(f, sheet, "C", "C", 22) }, func() error { return tabular.SetColumnWidth(f, sheet, "D", "E", 20) },
		func() error { return tabular.SetColumnWidth(f, sheet, "F", "F", 50) }, func() error { return tabular.SetColumnWidth(f, sheet, "G", "H", 28) },
		func() error { return tabular.SetColumnWidth(f, sheet, "I", "I", 40) }, func() error { return tabular.SetRowHeight(f, sheet, 4, 70) },
		func() error { return tabular.SetRowHeight(f, sheet, 5, 40) },
		func() error {
			return f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 5, TopLeftCell: "A6", ActivePane: "bottomLeft"})
		},
	} {
		if err := set(); err != nil {
			return Export{}, err
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return Export{}, err
	}
	kind := "bao-cao"
	if template {
		kind = "mau-nhap"
	}
	return Export{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: fmt.Sprintf("%s-%s-%s.xlsx", kind, v.Program, v.Report.Key), Rows: len(v.Report.Rows)}, nil
}

// ExportIssues returns the errors from the last confirmed import of the selected week.
func (s *WeeklyService) ExportIssues(ctx context.Context, profile, program, weekID string) (Export, error) {
	if err := s.AuthorizeWeekly(ctx, profile); err != nil {
		return Export{}, err
	}
	v, err := s.Read(ctx, profile, program, weekID)
	if err != nil {
		return Export{}, err
	}
	rows := [][]string{}
	for _, issue := range v.Report.ImportIssues {
		if issue.Status == "error" {
			rows = append(rows, []string{strconv.Itoa(issue.Line), issue.Name, strings.Join(issue.Errors, "; ")})
		}
	}
	return xlsxExport("Dong loi", []string{"Dòng", "Tên hạng mục", "Lỗi"}, rows, "dong-loi-bao-cao-tuan.xlsx")
}
