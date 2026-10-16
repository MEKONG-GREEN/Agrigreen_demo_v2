// Package domain là quy tắc thuần của phiếu "Báo cáo cho KHTC" (W11.63 rút gọn — BRD KHTC v2.2 FN-PN-03.02, FN-AD-04.02,
// FN-TI-02.04, BR-DL-05, BR-DL-06, BR-DL-10, BR-DL-12, BR-DL-21; D4 KH3): mẫu phiếu nạp sẵn cho 4 phòng (lưới cột chữ cái
// Excel + mục lời văn), dòng phiếu sinh từ Kế hoạch năm, hai loại cột mới "Từ Kế hoạch năm" và "Lũy kế kỳ trước", khối
// "Điều chỉnh kỳ trước", ba quy tắc kiểm tra. Không I/O, không time.Now (R-12), số là decimal (R-10).
package domain

import (
	"slices"
)

// RuleVersion ghi vào rule_version của sổ bảng số liệu.
const RuleVersion = "khtc-form-2026.10"

// FieldSheet là trường của biểu cap/forms KHTC-PHIEU (phiên bản 2) mang mã bảng số liệu đã niêm phong.
const FieldSheet = "bang_so_lieu"

// Loại cột của mẫu phiếu KHTC (FN-AD-04.02 B3: Cố định / Từ Kế hoạch năm / Lũy kế kỳ trước / Nhập / Công thức). Hai loại
// "Từ Kế hoạch năm", "Lũy kế kỳ trước" là loại MỚI so với Quản lý mẫu dùng chung (FN-AD-04.06 — reports/port Column*).
const (
	ColFixed           = "fixed"            // Cố định: STT, tên hạng mục — hệ thống điền
	ColFromPlan        = "from_plan"        // Từ Kế hoạch năm: ĐVT, khối lượng KH, quy mô — phiên bản hiệu lực tại ngày chốt (BR-DL-13)
	ColPriorCumulative = "prior_cumulative" // Lũy kế kỳ trước: lũy kế đến kỳ KHTC đã đóng gần nhất cùng năm (BR-DL-05)
	ColInput           = "input"            // Nhập: phòng nhập
	ColFormula         = "formula"          // Công thức: hệ thống tính, không sửa tay
)

// ColumnKinds theo thứ tự hiển thị trong thuộc tính cột.
var ColumnKinds = []string{ColFixed, ColFromPlan, ColPriorCumulative, ColInput, ColFormula}

// Kiểu dữ liệu cột.
const (
	DTypeNumber  = "number"  // decimal (chuỗi, tối đa 6 chữ số lẻ)
	DTypeInteger = "integer" // số nguyên
	DTypeText    = "text"
)

// Nguồn của cột cố định / Từ Kế hoạch năm.
const (
	SrcRowNo      = "row_no"
	SrcEntryName  = "entry_name"
	SrcUnit       = "unit"
	SrcPlannedQty = "planned_qty"
	SrcScaleNote  = "scale_note"
)

// Khóa ô nhập (CellInput).
const (
	InDone    = "done"    // Thực hiện trong kỳ — số phát sinh thật của kỳ, không âm (BR-DL-12)
	InStatus  = "status"  // Tình trạng
	InContent = "content" // Nội dung, xã/điểm thực hiện
	InPeople  = "people"  // Số người tham gia
	InBudget  = "budget"  // Kinh phí (đồng)
)

// Công thức của cột công thức.
const (
	FxAdjust     = "adjust"     // Điều chỉnh kỳ trước = Σ số điều chỉnh cùng năm kế hoạch của hạng mục (khối bên dưới)
	FxCumulative = "cumulative" // Lũy kế = Lũy kế kỳ trước + Thực hiện trong kỳ + Điều chỉnh kỳ trước (BR-DL-05)
	FxPercent    = "percent"    // % KH = Lũy kế / Khối lượng KH × 100, 1 chữ số lẻ
)

// Column là một cột của lưới mẫu phiếu (chữ cái Excel liên tục từ A).
type Column struct {
	Letter      string `json:"letter"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	DType       string `json:"dtype"`
	Unit        string `json:"unit,omitempty"`
	Source      string `json:"source,omitempty"`  // fixed, from_plan: Src*
	Input       string `json:"input,omitempty"`   // input: In*
	Formula     string `json:"formula,omitempty"` // formula: Fx*
	Required    bool   `json:"required,omitempty"`
	NonNegative bool   `json:"non_negative,omitempty"`
	Note        string `json:"note,omitempty"`
}

// Section là một mục lời văn (tab lời văn của phiếu).
type Section struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Required bool   `json:"required,omitempty"`
}

// Template là một phiên bản mẫu phiếu số liệu KHTC gán cho MỘT phòng nhập (FN-AD-04.02 "Mỗi mẫu một phòng nhập").
type Template struct {
	Code         string    `json:"code"`
	Version      int       `json:"version"`
	Department   string    `json:"department"` // phòng nhập; "" = mẫu chung cho phòng chưa có mẫu riêng
	Title        string    `json:"title"`
	Requirements string    `json:"requirements"` // "Yêu cầu biểu mẫu"
	Columns      []Column  `json:"columns"`      // lưới hạng mục — chỉ phiếu tuần (BR-DL-06)
	Sections     []Section `json:"sections"`
}

// Column trả cột theo chữ cái.
func (t Template) Column(letter string) (Column, bool) {
	i := slices.IndexFunc(t.Columns, func(c Column) bool { return c.Letter == letter })
	if i < 0 {
		return Column{}, false
	}
	return t.Columns[i], true
}

// LetterOf trả chữ cái của cột nhập/công thức theo khóa ("" nếu mẫu không có).
func (t Template) LetterOf(kind, key string) string {
	for _, c := range t.Columns {
		if c.Kind == kind && (c.Input == key || c.Formula == key || c.Source == key) {
			return c.Letter
		}
	}
	return ""
}

// Section trả mục lời văn theo khóa.
func (t Template) Section(key string) (Section, bool) {
	i := slices.IndexFunc(t.Sections, func(s Section) bool { return s.Key == key })
	if i < 0 {
		return Section{}, false
	}
	return t.Sections[i], true
}
