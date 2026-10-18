package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

// Text uses SetCellStr; validated decimals use numeric OOXML without a float conversion.
// The canonical version is the immutable stored byte stream, not a later re-render.
type workbook struct {
	file    *excelize.File
	sheet   string
	row     int
	err     error
	heading int
	normal  int
	percent int
}

func (w *workbook) check(err error) {
	if w.err == nil {
		w.err = err
	}
}
func (w *workbook) line(values ...string) {
	if w.err != nil {
		return
	}
	w.row++
	for i, value := range values {
		cell, err := excelize.CoordinatesToCellName(i+1, w.row)
		w.check(err)
		if w.err != nil {
			return
		}
		w.check(w.file.SetCellStr(w.sheet, cell, value))
	}
	w.check(w.file.SetCellStyle(w.sheet, "A"+strconv.Itoa(w.row), "N"+strconv.Itoa(w.row), w.normal))
}

// numericLine keeps user text inert and writes only explicitly selected numeric columns.
func (w *workbook) numericLine(values []string, columns ...int) {
	w.line(values...)
	for _, column := range columns {
		if w.err != nil {
			return
		}
		raw := values[column]
		if raw == "" || raw == "–" {
			continue
		}
		n, err := decimal.NewFromString(strings.TrimSuffix(raw, "%"))
		if err != nil {
			w.check(err)
			return
		}
		cell, err := excelize.CoordinatesToCellName(column+1, w.row)
		w.check(err)
		if strings.HasSuffix(raw, "%") {
			n = n.Div(decimal.NewFromInt(100))
			w.check(w.file.SetCellStyle(w.sheet, cell, cell, w.percent))
		}
		w.check(w.file.SetCellDefault(w.sheet, cell, n.String()))
	}
}
func (w *workbook) title(value string) {
	w.line(value)
	if w.err != nil {
		return
	}
	a, b := "A"+strconv.Itoa(w.row), "N"+strconv.Itoa(w.row)
	w.check(w.file.MergeCell(w.sheet, a, b))
	w.check(w.file.SetCellStyle(w.sheet, a, b, w.heading))
}
func (w *workbook) section(name string) {
	w.sheet = name
	w.row = 0
	_, err := w.file.NewSheet(name)
	w.check(err)
	w.check(tabular.SetColumnWidth(w.file, name, "A", "N", 18))
	w.check(tabular.SetColumnWidth(w.file, name, "B", "B", 42))
}
func dec(v *decimal.Decimal) string {
	if v == nil {
		return "–"
	}
	return v.String()
}
func text(d domain.Draft, key string) string {
	value := d.Texts[key]
	if value == "" && key == "D" {
		return "Không có."
	}
	return value
}
func admin(d domain.Draft, key string) string { return d.Admin[key].Value() }

var departments = map[string]string{"trong_trot": "Phòng Trồng trọt", "chan_nuoi_thuy_san": "Phòng Chăn nuôi - Thủy sản", "thong_tin_truyen_thong": "Phòng Thông tin – Truyền thông", "hanh_chinh_tong_hop": "Phòng Hành chính tổng hợp", "ke_hoach_tai_chinh": "Phòng Kế hoạch - Tài chính"}

func departmentName(code string) string {
	if name := departments[code]; name != "" {
		return name
	}
	return code
}
func reasonTitle(reason string) string {
	for code, name := range departments {
		reason = strings.ReplaceAll(reason, code, name)
	}
	return strings.ReplaceAll(reason, "Phòng Phòng ", "Phòng ")
}
func nextPeriod(p domain.Period) string {
	switch p.Type {
	case "week":
		return p.To.AddDate(0, 0, 1).Format("02/01/2006") + " đến " + p.To.AddDate(0, 0, 7).Format("02/01/2006")
	case "month", "program":
		next := p.To.AddDate(0, 1, 0)
		return fmt.Sprintf("tháng %d năm %d", next.Month(), next.Year())
	case "quarter":
		if p.Scope != nil && *p.Scope == "half" {
			return fmt.Sprintf("6 tháng cuối năm %d", p.Year)
		}
		if p.Scope != nil && *p.Scope == "nine_months" {
			return fmt.Sprintf("3 tháng cuối năm %d", p.Year)
		}
		n := (int(p.To.Month())-1)/3 + 2
		year := p.Year
		if n == 5 {
			n = 1
			year++
		}
		return fmt.Sprintf("quý %d năm %d", n, year)
	case "year":
		return fmt.Sprintf("năm %d", p.Year+1)
	}
	return "kỳ tiếp theo"
}

func writePrograms(w *workbook, rows []domain.ProgramRow, tab string) {
	w.line("TT", "Chương trình, nhóm, hạng mục", "ĐVT", "Khối lượng KH", "Quy mô", "Trong kỳ", "Điều chỉnh cùng năm", "Lũy kế năm", "% KH", "Tình trạng", "Nội dung, xã/điểm", "Số người", "Kinh phí", "Nguồn")
	for _, row := range rows {
		if tab == "previous" && row.Tab != "prev_year" {
			continue
		}
		if tab == "current" && row.Tab == "prev_year" {
			continue
		}
		planned, percent := "", ""
		if row.Planned != nil {
			planned = *row.Planned
		}
		if row.Percent != nil {
			percent = *row.Percent + "%"
		}
		status := row.Status
		if row.Kind == "item" && row.Missing {
			status = departmentName(row.Department) + " chưa gửi số liệu."
			if row.SourceKey != "" {
				status = departmentName(row.Department) + " chưa chốt số liệu."
			}
		} else if row.Kind == "item" && row.InPeriod == "0" && row.Content == "" {
			status = "Trong kỳ không tổ chức."
		}
		number := row.Roman
		if number == "" && row.Kind == "item" {
			number = "–"
		}
		sourceName := departmentName(row.Department)
		if row.SourceKey != "" {
			sourceName = "Số liệu module (xem trang nguồn)"
		}
		w.numericLine([]string{number, row.Name, row.Unit, planned, row.Scale, row.InPeriod, row.Adjustment, row.YearToDate, percent, status, row.Content, row.People, row.Budget, sourceName}, 3, 5, 6, 7, 8, 11, 12)
	}
}

// writeAppendix preserves the five BRD columns. Exact numeric cells also appear in the detail sheet.
func writeAppendix(w *workbook, rows []domain.ProgramRow) {
	w.line("TT", "Tên chương trình", "Quy mô", "Tiến độ", "Ghi chú")
	for _, row := range rows {
		number := row.Roman
		scale, progress, note := row.Scale, row.Status, row.Content
		if row.Kind == "item" {
			number = "–"
			if row.Planned != nil {
				scale = strings.TrimSpace(*row.Planned + " " + row.Unit + " " + row.Scale)
			}
			if row.Percent != nil && row.Planned != nil {
				progress = row.YearToDate + "/" + *row.Planned + "; đạt " + *row.Percent + "% kế hoạch"
			}
			if row.Percent == nil && progress == "" {
				progress = row.Content
			}
			if row.Missing {
				note = departmentName(row.Department) + " chưa gửi/chốt số liệu. " + note
			}
		}
		w.line(number, row.Name, scale, progress, note)
	}
}
func writeMetrics(w *workbook, metrics []domain.SourceMetric, section string) {
	if section == "A.III" {
		w.line("Mặt hàng", "ĐVT", "Ngày trước", "Giá từ", "Giá đến", "Ngày sau", "Giá từ", "Giá đến")
		for _, metric := range metrics {
			if metric.Value.Mode != source.ModePrice {
				continue
			}
			values := []string{metric.Label, metric.Unit, "–", "–", "–", "–", "–", "–"}
			for i, day := range metric.Value.PriceDays {
				if i >= 2 {
					break
				}
				j := 2 + 3*i
				values[j], values[j+1], values[j+2] = day.Day, day.Min.String(), day.Max.String()
			}
			w.numericLine(values, 3, 4, 6, 7)
		}
		return
	}
	w.line("Chỉ tiêu", "ĐVT", "Trong kỳ", "Lũy kế nguồn / hiện có", "Lũy kế năm KHTC", "Chưa xác nhận", "Trạng thái")
	hasAggregate := false
	for _, metric := range metrics {
		if metric.Form == "cnts-bm18" || metric.Form == "bm-18" {
			hasAggregate = true
			break
		}
	}
	for _, metric := range metrics {
		if section == "A.I.3" && hasAggregate && (metric.Form == "cnts-bm17" || metric.Form == "bm-17") {
			continue
		}
		if section == "A.I.1" && metric.Department != "trong_trot" {
			continue
		}
		if section == "A.I.2" && (metric.Department != "chan_nuoi_thuy_san" || !metric.Stock) {
			continue
		}
		if section == "A.I.3" && (metric.Department != "chan_nuoi_thuy_san" || metric.Stock) {
			continue
		}
		if metric.Value.Mode == source.ModePrice {
			continue
		}
		v := metric.Value
		in := v.Increment
		if v.Sum != nil {
			in = v.Sum
		}
		actual, ytd := dec(v.Cumulative), dec(v.YearToDate)
		period := dec(in)
		if metric.Stock {
			period = ""
			ytd = ""
		}
		state := "Đã chốt"
		if v.Provisional() {
			state = "Tạm tính"
		} else if v.NoSource {
			state = "Không phát sinh"
		}
		w.numericLine([]string{metric.Label, metric.Unit, period, actual, ytd, dec(v.Unconfirmed), state}, 2, 3, 4, 5)
	}
}
func writeFormTexts(w *workbook, data domain.Data, hcth bool) {
	keys := make([]string, 0, len(data.FormTexts))
	for key := range data.FormTexts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, department := range keys {
		if (department == "hanh_chinh_tong_hop") != hcth {
			continue
		}
		fields := data.FormTexts[department]
		names := make([]string, 0, len(fields))
		for name := range fields {
			if name != "kho_khan" && name != "nhiem_vu_ky_toi" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if fields[name] != "" {
				w.line(departmentName(department), fields[name])
			}
		}
	}
}

func RenderXLSX(d domain.Draft, filter domain.Filter, version int64) ([]byte, error) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	heading, err := file.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Family: "Times New Roman"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"E7EFEC"}, Pattern: 1}, Alignment: &excelize.Alignment{WrapText: true}})
	if err != nil {
		return nil, err
	}
	normal, err := file.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Times New Roman"}, Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	if err != nil {
		return nil, err
	}
	percentFormat := "0.0%"
	percent, err := file.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Times New Roman"}, CustomNumFmt: &percentFormat})
	if err != nil {
		return nil, err
	}
	w := &workbook{file: file, sheet: "Báo cáo", heading: heading, normal: normal, percent: percent}
	w.check(file.SetSheetName("Sheet1", w.sheet))
	w.check(tabular.SetColumnWidth(file, w.sheet, "A", "N", 18))
	w.check(tabular.SetColumnWidth(file, w.sheet, "B", "B", 42))
	data := d.Data
	rows := domain.SelectRows(data.Rows, filter)
	w.title("TRUNG TÂM KHUYẾN NÔNG")
	w.title("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM — Độc lập - Tự do - Hạnh phúc")
	w.line("Số: "+admin(d, "document_no")+"/BC-TTKN", admin(d, "place")+", ngày "+admin(d, "report_date"))
	w.title("BÁO CÁO")
	title := "Kết quả thực hiện nhiệm vụ công tác khuyến nông"
	if data.Template == "mau_5" {
		title = fmt.Sprintf("Kết quả tiến độ thực hiện Chương trình khuyến nông địa phương năm %d", data.Period.Year)
	}
	w.title(title + " — " + data.Period.Name)
	w.line("Mẫu", data.Template, "Phiên bản", strconv.FormatInt(version, 10), "Số liệu đến", data.Period.Cutoff.Format("02/01/2006"))
	if len(data.Provisional) > 0 {
		w.title("TẠM TÍNH — nguồn/phiếu chưa đầy đủ")
		for _, reason := range data.Provisional {
			w.line(reasonTitle(reason))
		}
	}
	if filter.Department != "" || filter.Program != "" || filter.Commune != "" || filter.Level != "" {
		w.line("Xuất theo bộ lọc", departmentName(filter.Department), filter.Program, filter.Commune, filter.Level)
	}
	if data.Template == "mau_5" {
		w.line("Kính gửi", admin(d, "recipient"))
		w.line("Căn cứ", admin(d, "basis"))
	}
	for _, section := range domain.Sections(data.Template) {
		if section.Key == "phu_luc" {
			continue
		}
		title := section.Title
		if section.Key == "IV" || section.Key == "II" && data.Template == "mau_5" || strings.HasPrefix(section.Key, "C.") {
			title += " — " + nextPeriod(data.Period)
		}
		w.title(section.Key + ". " + title)
		switch section.Key {
		case "A.I.1", "A.I.2", "A.I.3", "A.III":
			writeMetrics(w, data.Metrics, section.Key)
		case "A.II.1", "I.1":
			writePrograms(w, rows, "previous")
		case "A.II.2", "I.2":
			writePrograms(w, rows, "current")
		case "A.II.3.2", "I.3.2":
			writeFormTexts(w, data, false)
		case "A.IV", "II.1", "II.2":
			writeFormTexts(w, data, true)
		}
		if section.Editable {
			w.line(text(d, section.Key))
		}
	}
	w.line("Trên đây là báo cáo " + data.Period.Name + " gửi " + admin(d, "recipient") + " tổng hợp./.")
	w.line("Nơi nhận", admin(d, "recipients"))
	w.line(admin(d, "authority"), admin(d, "signer_title"))
	w.line(admin(d, "signer"))
	if data.Template == "mau_3" || data.Template == "mau_4" || data.Template == "mau_5" {
		w.section("Phụ lục")
		w.title(fmt.Sprintf("Tiến độ thực hiện chương trình khuyến nông năm %d (đến ngày %s)", data.Period.Year, data.Period.Cutoff.Format("02/01/2006")))
		w.line("Kèm theo Báo cáo số", admin(d, "document_no"), "ngày", admin(d, "report_date"))
		writeAppendix(w, rows)
		w.section("Số liệu chương trình")
		writePrograms(w, rows, "")
	}
	w.section("Số liệu nguồn")
	w.title("Số đã chụp từ các nguồn — chỉ đọc; không thay thế bằng dữ liệu trực tiếp khi tải lại")
	w.line("Khóa nguồn", "Chỉ tiêu", "Trong kỳ", "Lũy kế nguồn", "Lũy kế năm KHTC", "Tổng phát sinh", "Chưa xác nhận", "Đã chốt", "Kỳ nguồn")
	for _, metric := range data.Metrics {
		v := metric.Value
		w.numericLine([]string{metric.Key, metric.Label, dec(v.Increment), dec(v.Cumulative), dec(v.YearToDate), dec(v.Sum), dec(v.Unconfirmed), strconv.FormatBool(v.Closed), strings.Join(v.SourceKeys, ", ")}, 2, 3, 4, 5, 6)
	}
	w.line("Chi tiết nguồn đã chụp")
	w.line("Khóa số", "Module", "Biểu", "Lịch nguồn", "Kỳ nguồn", "Từ", "Đến", "Đã chốt", "Quy về tuần KHTC", "Băm số")
	for _, metric := range data.Metrics {
		for _, trace := range metric.Trace {
			from, to := "", ""
			if trace.From != nil {
				from = trace.From.Format(time.DateOnly)
			}
			if trace.To != nil {
				to = trace.To.Format(time.DateOnly)
			}
			hash := ""
			if trace.FiguresHash != nil {
				hash = *trace.FiguresHash
			}
			w.line(metric.Key, trace.Module, metric.Form, trace.Calendar, trace.NativeKey, from, to, strconv.FormatBool(trace.Closed), trace.TargetKey, hash)
		}
	}
	w.line("Cách tính theo tuần KHTC")
	w.line("Khóa số", "Tuần KHTC", "Từ", "Đến", "Lũy kế", "Gốc", "Nguồn gốc", "Trong kỳ", "Lũy kế năm", "Đã chốt", "Giữ số đóng", "Kỳ nguồn", "Cách tính")
	for _, metric := range data.Metrics {
		for _, week := range metric.Value.Weeks {
			method := "lũy kế − gốc"
			if metric.Value.Mode == source.ModeSum {
				method = "tổng phát sinh"
			}
			if metric.Value.Mode == source.ModePrice {
				method = "min/max ngày đã duyệt"
			}
			w.numericLine([]string{metric.Key, week.Key, week.From.Format(time.DateOnly), week.To.Format(time.DateOnly), week.Cumulative.String(), week.Base.String(), week.BaseRef, week.Increment.String(), week.YearToDate.String(), strconv.FormatBool(week.Closed), strconv.FormatBool(week.Frozen), strings.Join(week.SourceKeys, ", "), method}, 4, 5, 7, 8)
		}
	}
	w.section("Mốc dữ liệu")
	w.line("Kỳ KHTC", data.Period.ID.String(), data.Period.Name)
	w.line("Phiên bản kế hoạch", data.Plan.VersionID.String(), strconv.Itoa(data.Plan.Version), data.Plan.DecisionNo)
	w.line("Dấu dữ liệu", data.InputHash)
	w.line("Lúc chụp", data.CapturedAt.Format(time.RFC3339))
	if data.Sources != nil {
		w.line("Ảnh chụp nguồn", data.Sources.ID.String(), strconv.FormatInt(data.Sources.Version, 10), data.Sources.At.Format(time.RFC3339))
		w.line("Băm số", data.Sources.FiguresHash)
		w.line("Băm trạng thái HTX", data.Sources.StatusHash)
	}
	w.line("Kỳ phiếu", "Phòng", "Bài nộp", "Lần gửi", "Bảng niêm phong", "Băm bảng")
	for _, ref := range data.Submissions {
		w.line(ref.PeriodID.String(), departmentName(ref.Department), ref.SubmissionID.String(), strconv.FormatInt(ref.Attempt, 10), ref.SheetID.String(), ref.Hash)
	}
	w.line("Ô hành chính", "Giá trị hệ thống", "Giá trị đã dùng", "Người sửa", "Thời điểm sửa")
	for _, key := range domain.AdminKeys {
		field := d.Admin[key]
		actor, at := "", ""
		if field.Actor != nil {
			actor = field.Actor.String()
		}
		if field.At != nil {
			at = field.At.Format(time.RFC3339)
		}
		w.line(key, field.System, field.Value(), actor, at)
	}
	if w.err != nil {
		return nil, w.err
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
