package app

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/xuri/excelize/v2"
)

// Change this immutable identity whenever the Excel layout/compiler changes.
const fixedWorkbookRule = "khtc-xlsx/fixed-v1"

// fixedWorkbook adds the compiled report to a native workbook. The original
// numeric/source/appendix sheets remain available; compiled prose always uses
// SetCellStr, so template text can never become an Excel formula. Historical nil
// and explicit legacy return the original byte stream unchanged after validation.
func fixedWorkbook(source []byte, pinned *port.KHTCTemplateSnapshot, content domain.Draft, terminal string, filter domain.Filter, version int64) ([]byte, error) {
	if pinned == nil {
		return source, nil
	}
	if _, err := fixedTemplateSections(pinned, content); err != nil {
		return nil, err
	}
	if pinned.Mode == "legacy" {
		if pinned.Rule != domain.RuleVersion || pinned.WordHash != WordTemplate(content.Data.Template).Hash() {
			return nil, ErrInvalid.WithDetail("Bộ dựng mẫu nền khác phiên bản đã ghim")
		}
		return source, nil
	}
	if pinned.ExcelRule != fixedWorkbookRule {
		return nil, ErrInvalid.WithDetail("Bộ dựng Excel khác phiên bản đã ghim")
	}
	document, err := renderFixedTemplate(pinned, content, terminal, filter)
	if err != nil {
		return nil, err
	}
	if renderer := FixedWordTemplate(); pinned.WordKey != renderer.Key || pinned.WordVersion != renderer.Version || pinned.WordHash != renderer.Hash() {
		return nil, ErrInvalid.WithDetail("Bộ dựng khác phiên bản đã ghim")
	}
	file, err := excelize.OpenReader(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	original := "Chi tiết số liệu"
	heading, err := file.GetCellStyle("Báo cáo", "A1")
	if err != nil {
		return nil, err
	}
	normal, err := file.GetCellStyle("Báo cáo", "A3")
	if err != nil {
		return nil, err
	}
	header, err := file.GetRows("Báo cáo")
	if err != nil {
		return nil, err
	}
	if len(header) < 6 {
		return nil, ErrInvalid.WithDetail("Khung Excel nền không hợp lệ")
	}
	if err = file.SetSheetName("Báo cáo", original); err != nil {
		return nil, err
	}
	if _, err = file.NewSheet("Báo cáo"); err != nil {
		return nil, err
	}
	if err = file.MoveSheet("Báo cáo", original); err != nil {
		return nil, err
	}
	w := &workbook{file: file, sheet: "Báo cáo", heading: heading, normal: normal}
	w.check(tabular.SetColumnWidth(file, w.sheet, "A", "N", 18))
	w.check(tabular.SetColumnWidth(file, w.sheet, "B", "B", 42))
	for i, row := range header[:6] {
		if i == 0 || i == 1 || i == 3 || i == 4 {
			if len(row) > 0 {
				w.title(row[0])
			}
		} else {
			w.line(row...)
		}
	}
	if len(content.Data.Provisional) > 0 {
		w.title("TẠM TÍNH — nguồn/phiếu chưa đầy đủ")
		for _, reason := range content.Data.Provisional {
			w.line(reasonTitle(reason))
		}
	}
	if filter.Department != "" || filter.Program != "" || filter.Commune != "" || filter.Level != "" {
		w.line("Xuất theo bộ lọc", departmentName(filter.Department), filter.Program, filter.Commune, filter.Level)
	}
	if content.Data.Template == "mau_5" {
		w.line("Kính gửi", admin(content, "recipient"))
		w.line("Căn cứ", admin(content, "basis"))
	}
	writeText := func(text string) {
		// Excel silently truncates a cell above 32767 characters. Split explicitly,
		// preserving every rune in this bounded document instead of dropping source text.
		runes := []rune(text)
		for len(runes) > 32767 {
			w.line(string(runes[:32767]))
			runes = runes[32767:]
		}
		if len(runes) > 0 {
			w.line(string(runes))
		}
	}
	for _, node := range document.Nodes {
		w.title(node.Number + ". " + node.Title)
		for _, row := range node.Rows {
			if row.Preamble != "" {
				writeText(row.Preamble)
			}
			var text strings.Builder
			for _, token := range row.Tokens {
				text.WriteString(token.Text)
			}
			writeText(text.String())
		}
	}
	w.line("Trên đây là báo cáo " + content.Data.Period.Name + " gửi " + admin(content, "recipient") + " tổng hợp./.")
	w.line("Nơi nhận", admin(content, "recipients"))
	w.line(admin(content, "authority"), admin(content, "signer_title"))
	w.line(admin(content, "signer"))
	rows, err := file.GetRows("Mốc dữ liệu")
	if err != nil {
		return nil, err
	}
	w.sheet, w.row = "Mốc dữ liệu", len(rows)+1
	w.line("Định nghĩa mẫu đã ghim", pinned.Key, pinned.Mode, pinned.ID.String(), strconv.FormatInt(pinned.Version, 10), pinned.Hash)
	w.line("Quy tắc và bộ dựng", pinned.Rule, pinned.WordKey, strconv.Itoa(pinned.WordVersion), pinned.WordHash, pinned.ExcelRule)
	w.line("Hiệu lực định nghĩa", pinned.ApplicableFrom.Format(time.RFC3339), "Bắt đầu kỳ", content.Data.Period.From.Format(time.RFC3339))
	w.line("Trạng thái nguồn của revision", terminal, "Phiên bản Excel", strconv.FormatInt(version, 10))
	if w.err != nil {
		return nil, w.err
	}
	active, err := file.GetSheetIndex("Báo cáo")
	if err != nil {
		return nil, err
	}
	file.SetActiveSheet(active)
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
