package domain

// W11.25 — BỐ CỤC MẪU SỐ 1, 2, 4 (FN-09: bảng trường NL02, NL03, NL04). Dòng xuất (ExportItem) lưu ở sổ promotion_export
// KHÔNG có dữ liệu cá nhân: họ tên, năm sinh, giới tính đọc ở kho định danh khi dựng tệp (PromoPerson). Bố cục chính thức
// của Sở chờ khách xác nhận (giả định W11.25).

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// ExportItem là một dòng danh sách đã xuất (JSON trong promotion_export.items).
type ExportItem struct {
	CaseKey       string     `json:"case_key"`
	StaffID       uuid.UUID  `json:"staff_id"`
	Code          string     `json:"code"`
	Kind          PromoKind  `json:"kind"`
	Due           string     `json:"due"`
	UnitName      string     `json:"unit_name,omitempty"`
	Degree        string     `json:"degree,omitempty"`
	Position      string     `json:"position,omitempty"`
	Grade         string     `json:"grade"`
	GradeName     string     `json:"grade_name,omitempty"`
	FromStep      int        `json:"from_step"`
	ToStep        int        `json:"to_step"`
	FromCoeff     string     `json:"from_coeff"`
	ToCoeff       string     `json:"to_coeff"`
	Diff          string     `json:"diff"`
	StepSince     string     `json:"step_since"`
	Reserved      string     `json:"reserved,omitempty"`
	ReservedAfter string     `json:"reserved_after,omitempty"`
	NextDue       string     `json:"next_due"`
	MonthsEnjoyed string     `json:"months_enjoyed,omitempty"`
	EarlyMonths   int        `json:"early_months,omitempty"`
	AwardEventID  *uuid.UUID `json:"award_event_id,omitempty"`
	AwardLabel    string     `json:"award_label,omitempty"`
	PctFrom       string     `json:"pct_from,omitempty"`
	PctTo         string     `json:"pct_to,omitempty"`
	PctSince      string     `json:"pct_since,omitempty"`
	Note          string     `json:"note,omitempty"`
	Reason        string     `json:"reason,omitempty"` // lý do đưa người "Chưa đủ"/"Cần xem xét" vào danh sách
}

// PromoPerson là phần dữ liệu cá nhân của một dòng (đọc ở kho khi dựng tệp).
type PromoPerson struct {
	FullName  string
	Gender    string // nam | nu
	BirthYear string
}

// FormTitle là tiêu đề mẫu.
func FormTitle(form string) string {
	switch form {
	case "mau2":
		return "DANH SÁCH ĐỀ NGHỊ NÂNG BẬC LƯƠNG TRƯỚC THỜI HẠN DO LẬP THÀNH TÍCH XUẤT SẮC (MẪU SỐ 2)"
	case "mau4":
		return "DANH SÁCH ĐỀ NGHỊ HƯỞNG PHỤ CẤP THÂM NIÊN VƯỢT KHUNG (MẪU SỐ 4)"
	}
	return "DANH SÁCH ĐỀ NGHỊ NÂNG BẬC LƯƠNG THƯỜNG XUYÊN (MẪU SỐ 1)"
}

// FormFileName là tên tệp: MauSo1_T07_2026.xlsx.
func FormFileName(form string, month time.Time) string {
	return fmt.Sprintf("MauSo%s_T%s_%d.xlsx", form[3:], month.Format("01"), month.Year())
}

func dmy(s string) string {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.Format("02/01/2006")
	}
	return s
}

// comma đổi dấu thập phân sang dấu phẩy (mẫu Sở).
func comma(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r == '.' {
			out[i] = ','
		}
	}
	return string(out)
}

func col(key, group, label string) Column { return Column{Key: key, Group: group, Label: label} }

// FormColumns trả cột của mẫu (Mẫu 1, 2: 20 cột; Mẫu 4: 17 cột).
func FormColumns(form string) []Column {
	head := []Column{col("stt", "", "Số TT"), col("name", "", "Họ và tên - cơ quan, đơn vị trực thuộc"),
		col("male", "Năm sinh", "Nam"), col("female", "Năm sinh", "Nữ"), col("degree", "", "Trình độ chuyên môn nghiệp vụ"),
		col("position", "", "Chức vụ (nếu có)")}
	switch form {
	case "mau4":
		return append(head, col("grade", "Ngạch, bậc cuối hiện hưởng", "Ngạch"), col("step", "Ngạch, bậc cuối hiện hưởng", "Bậc cuối"),
			col("coeff", "Ngạch, bậc cuối hiện hưởng", "Hệ số bậc cuối"), col("since", "Ngạch, bậc cuối hiện hưởng", "Thời điểm được xếp"),
			col("pct_from", "% TNVK đang hưởng", "%"), col("pct_since", "% TNVK đang hưởng", "Thời điểm hưởng"),
			col("pct_to", "TNVK được hưởng", "%"), col("next", "TNVK được hưởng", "Thời gian tính hưởng TNVK lần sau"),
			col("diff", "", "Hệ số chênh lệch hưởng phụ cấp TNVK"), col("months", "", "Số tháng được hưởng phụ cấp TNVK"),
			col("note", "", "Ghi chú"))
	case "mau2":
		return append(head, col("grade", "Ngạch, bậc hiện giữ", "Ngạch"), col("step", "Ngạch, bậc hiện giữ", "Bậc"),
			col("coeff", "Ngạch, bậc hiện giữ", "Hệ số"), col("reserved", "Ngạch, bậc hiện giữ", "HS bảo lưu"),
			col("since", "Ngạch, bậc hiện giữ", "Thời điểm được xếp"),
			col("grade2", "Ngạch, bậc sau nâng", "Ngạch"), col("step2", "Ngạch, bậc sau nâng", "Bậc"),
			col("coeff2", "Ngạch, bậc sau nâng", "Hệ số"), col("reserved2", "Ngạch, bậc sau nâng", "HS bảo lưu"),
			col("next", "", "Thời gian tính nâng bậc lần sau"), col("diff", "", "Hệ số chênh lệch do nâng bậc"),
			col("award", "", "Thành tích đạt được"), col("early", "", "Số tháng được nâng bậc trước thời hạn"), col("note", "", "Ghi chú"))
	}
	return append(head, col("grade", "Ngạch, bậc hiện giữ", "Ngạch"), col("step", "Ngạch, bậc hiện giữ", "Bậc"),
		col("coeff", "Ngạch, bậc hiện giữ", "Hệ số"), col("since", "Ngạch, bậc hiện giữ", "Thời điểm được xếp"),
		col("reserved", "Ngạch, bậc hiện giữ", "HS bảo lưu"),
		col("grade2", "Ngạch, bậc sau nâng", "Ngạch"), col("step2", "Ngạch, bậc sau nâng", "Bậc"),
		col("coeff2", "Ngạch, bậc sau nâng", "Hệ số bậc mới"), col("next", "Ngạch, bậc sau nâng", "Thời gian tính nâng bậc lần sau"),
		col("reserved2", "Ngạch, bậc sau nâng", "HS chênh lệch bảo lưu (nếu có)"),
		col("diff", "", "Hệ số chênh lệch do nâng bậc"), col("months", "", "Số tháng được hưởng bậc mới"),
		col("salary", "", "Tiền lương tăng thêm do nâng bậc"), col("note", "", "Ghi chú"))
}

// BuildForm dựng biểu Mẫu số 1/2/4 từ các dòng đã xuất; people[staff] là phần định danh (thiếu → để trống).
func BuildForm(form string, month, asOf time.Time, items []ExportItem, people map[uuid.UUID]PromoPerson) Table {
	t := Table{Code: ReportCode(form), Title: FormTitle(form), Period: "THÁNG " + month.Format("01/2006"), AsOf: asOf,
		Columns: FormColumns(form)}
	for i, it := range items {
		p := people[it.StaffID]
		male, female := "", ""
		switch p.Gender {
		case "nam":
			male = p.BirthYear
		case "nu":
			female = p.BirthYear
		}
		name := p.FullName
		if it.UnitName != "" {
			name += " - " + it.UnitName
		}
		grade := it.GradeName
		if grade == "" {
			grade = it.Grade
		}
		cells := []string{strconv.Itoa(i + 1), name, male, female, it.Degree, it.Position}
		switch form {
		case "mau4":
			cells = append(cells, grade, strconv.Itoa(it.FromStep), comma(it.FromCoeff), dmy(it.StepSince), comma(it.PctFrom), dmy(it.PctSince),
				comma(it.PctTo), dmy(it.NextDue), comma(it.Diff), comma(it.MonthsEnjoyed), it.Note)
		case "mau2":
			cells = append(cells, grade, strconv.Itoa(it.FromStep), comma(it.FromCoeff), comma(it.Reserved), dmy(it.StepSince), grade,
				strconv.Itoa(it.ToStep), comma(it.ToCoeff), comma(it.ReservedAfter), dmy(it.NextDue), comma(it.Diff), it.AwardLabel,
				strconv.Itoa(it.EarlyMonths), it.Note)
		default:
			// Cột 19 (tiền lương tăng thêm) để trống — hệ thống không lưu mức lương cơ sở (BR-04).
			cells = append(cells, grade, strconv.Itoa(it.FromStep), comma(it.FromCoeff), dmy(it.StepSince), comma(it.Reserved), grade,
				strconv.Itoa(it.ToStep), comma(it.ToCoeff), dmy(it.NextDue), comma(it.ReservedAfter), comma(it.Diff), comma(it.MonthsEnjoyed), "",
				it.Note)
		}
		t.Rows = append(t.Rows, Row{Kind: RowPerson, Cells: cells, StaffID: it.StaffID})
		t.People = append(t.People, it.StaffID)
	}
	return t
}
