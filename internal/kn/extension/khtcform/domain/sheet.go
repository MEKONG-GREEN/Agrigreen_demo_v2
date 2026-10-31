package domain

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CellInput là các ô nhập của một dòng (khóa In*). Số là chuỗi thập phân.
type CellInput struct {
	Done    string `json:"done,omitempty"`
	Status  string `json:"status,omitempty"`
	Content string `json:"content,omitempty"`
	People  string `json:"people,omitempty"`
	Budget  string `json:"budget,omitempty"`
}

func (c CellInput) empty() bool {
	return c.Done == "" && c.Status == "" && c.Content == "" && c.People == "" && c.Budget == ""
}

// Value trả ô theo khóa In*.
func (c CellInput) Value(key string) string {
	switch key {
	case InDone:
		return c.Done
	case InStatus:
		return c.Status
	case InContent:
		return c.Content
	case InPeople:
		return c.People
	case InBudget:
		return c.Budget
	}
	return ""
}

// Adjustment là một dòng khối "Điều chỉnh kỳ trước" (BR-DL-12): hạng mục, kỳ được điều chỉnh (kỳ KHTC đã khóa), số
// điều chỉnh (dương: phần chưa báo cáo; âm: phần ghi thừa), lý do. PeriodYear, PeriodName do hệ thống ghi theo kỳ chọn.
type Adjustment struct {
	EntryRef   string `json:"entry_ref"`
	PeriodID   string `json:"period_id"`
	PeriodYear int    `json:"period_year,omitempty"`
	PeriodName string `json:"period_name,omitempty"`
	Amount     string `json:"amount"`
	Reason     string `json:"reason"`
}

// Sheet là nội dung bảng số liệu (nháp hoặc niêm phong).
type Sheet struct {
	AdhocValues    map[string]string    `json:"adhoc_values,omitempty"`
	AdhocText      string               `json:"adhoc_text,omitempty"`
	AdhocRequestID string               `json:"adhoc_request_id,omitempty"`
	Cells          map[string]CellInput `json:"cells"`
	Adjustments    []Adjustment         `json:"adjustments"`
	Texts          map[string]string    `json:"texts"`
}

// Giới hạn kích thước.
const (
	MaxText        = 2000 // ô chữ của lưới
	MaxSectionText = 8000 // mục lời văn
	MaxReason      = 500
	MaxAdjustments = 200
	MaxCells       = 2000
)

// Normalize cắt khoảng trắng, bỏ dòng trống, bảo đảm map/mảng không nil (để băm ổn định).
func (s Sheet) Normalize() Sheet {
	out := Sheet{AdhocRequestID: strings.ToLower(strings.TrimSpace(s.AdhocRequestID)), Cells: map[string]CellInput{}, Adjustments: []Adjustment{}, Texts: map[string]string{}}
	out.AdhocText = strings.TrimSpace(s.AdhocText)
	if len(s.AdhocValues) > 0 {
		out.AdhocValues = map[string]string{}
		for k, v := range s.AdhocValues {
			out.AdhocValues[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for k, c := range s.Cells {
		c = CellInput{Done: strings.TrimSpace(c.Done), Status: strings.TrimSpace(c.Status), Content: strings.TrimSpace(c.Content),
			People: strings.TrimSpace(c.People), Budget: strings.TrimSpace(c.Budget)}
		if !c.empty() {
			out.Cells[strings.ToLower(strings.TrimSpace(k))] = c
		}
	}
	for _, a := range s.Adjustments {
		a = Adjustment{EntryRef: strings.ToLower(strings.TrimSpace(a.EntryRef)), PeriodID: strings.ToLower(strings.TrimSpace(a.PeriodID)),
			PeriodYear: a.PeriodYear, PeriodName: strings.TrimSpace(a.PeriodName), Amount: strings.TrimSpace(a.Amount), Reason: strings.TrimSpace(a.Reason)}
		if a.EntryRef == "" && a.PeriodID == "" && a.Amount == "" && a.Reason == "" {
			continue
		}
		out.Adjustments = append(out.Adjustments, a)
	}
	for k, v := range s.Texts {
		if v = strings.TrimSpace(v); v != "" {
			out.Texts[strings.TrimSpace(k)] = v
		}
	}
	return out
}

// Hash là SHA-256 của JSON chuẩn hóa (khóa map đã sắp).
func (s Sheet) Hash() ([]byte, []byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, nil, err
	}
	h := sha256.Sum256(raw)
	return raw, h[:], nil
}

// AdjPeriod là một kỳ được phép điều chỉnh: kỳ KHTC tuần đã đóng (đã khóa), trước kỳ hiện tại, cùng năm kế hoạch hoặc
// năm trước (năm trước chỉ lưu vết, không cộng lũy kế — BR-DL-12).
type AdjPeriod struct {
	ID   uuid.UUID
	Name string
	Year int
}

// Quy tắc kiểm tra (FN-TI-02.04 "Không áp dụng kiểm tra ngoài 3 nhóm").
const (
	RuleTemplate       = "template"        // (1) yêu cầu của mẫu phiếu — chặn gửi
	RuleNegativeReason = "negative_reason" // (2) số âm phải có lý do — chặn gửi
	RuleOverPlan       = "over_plan"       // (3) lũy kế vượt 100% KH — cảnh báo, vẫn gửi
)

// Mức.
const (
	SevError   = "error"
	SevWarning = "warning"
)

// Issue là một kết quả kiểm tra, báo tại ô (EntryRef + Letter), tại dòng điều chỉnh (Adjustment ≥ 0) hoặc mục lời văn.
type Issue struct {
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	EntryRef   string `json:"entry_ref,omitempty"`
	Letter     string `json:"letter,omitempty"`
	Adjustment int    `json:"adjustment"` // chỉ số dòng điều chỉnh; -1 = không
	Section    string `json:"section,omitempty"`
	Message    string `json:"message"`
}

// RowResult là các ô hệ thống của một dòng (chữ cái → giá trị chuỗi; rỗng = không có).
type RowResult struct {
	Cells    map[string]string
	OverPlan bool
}

// Result là kết quả tính + kiểm một bảng.
type Result struct {
	Rows   map[uuid.UUID]RowResult
	Issues []Issue
}

// Valid: không có lỗi chặn gửi.
func (r Result) Valid() bool {
	for _, i := range r.Issues {
		if i.Severity == SevError {
			return false
		}
	}
	return true
}

// Input là đầu vào Evaluate.
type Input struct {
	Template Template
	Rows     []Row
	Grid     bool                          // phiếu tuần có lưới hạng mục (BR-DL-06: tháng/quý/năm chỉ lời văn)
	Prior    map[uuid.UUID]decimal.Decimal // Lũy kế kỳ trước theo hạng mục
	Periods  map[uuid.UUID]AdjPeriod       // kỳ được phép điều chỉnh
	PlanYear int
	Sheet    Sheet // đã Normalize
	Submit   bool  // kiểm bắt buộc (gửi); nháp chỉ báo
}

var reNumber = regexp.MustCompile(`^-?[0-9]{1,15}(\.[0-9]{1,6})?$`)
var reInteger = regexp.MustCompile(`^-?[0-9]{1,9}$`)

// ParseNumber đọc số thập phân của ô (dấu chấm thập phân).
func ParseNumber(s string) (decimal.Decimal, bool) {
	if !reNumber.MatchString(s) {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	return d, err == nil
}

// FormatNumber ghi số ra chuỗi (bỏ số 0 thừa sau dấu chấm).
func FormatNumber(d decimal.Decimal) string { return d.String() }

// Counted: điều chỉnh có cộng vào lũy kế năm kế hoạch year (kỳ được điều chỉnh cùng năm — BR-DL-05).
func (a Adjustment) Counted(year int) bool { return a.PeriodYear == year }

// Contribution là phần một bảng đã gửi cộng vào lũy kế năm year theo hạng mục: Thực hiện trong kỳ + điều chỉnh cùng năm.
// Ô/điều chỉnh sai định dạng bỏ qua (bảng niêm phong đã kiểm).
func Contribution(s Sheet, year int) map[uuid.UUID]decimal.Decimal {
	out := map[uuid.UUID]decimal.Decimal{}
	for k, c := range s.Cells {
		ref, err := uuid.Parse(k)
		if err != nil {
			continue
		}
		if d, ok := ParseNumber(c.Done); ok {
			out[ref] = out[ref].Add(d)
		}
	}
	for _, a := range s.Adjustments {
		ref, err := uuid.Parse(a.EntryRef)
		if err != nil || !a.Counted(year) {
			continue
		}
		if d, ok := ParseNumber(a.Amount); ok {
			out[ref] = out[ref].Add(d)
		}
	}
	return out
}

// Evaluate tính ô hệ thống (Từ Kế hoạch năm, Lũy kế kỳ trước, công thức) và kiểm 3 quy tắc (FN-PN-03.02, FN-TI-02.04).
func Evaluate(in Input) Result {
	res := Result{Rows: map[uuid.UUID]RowResult{}}
	add := func(i Issue) { res.Issues = append(res.Issues, i) }
	t := in.Template
	byRef := map[string]Row{}
	for _, r := range in.Rows {
		byRef[r.EntryRef.String()] = r
	}
	letter := func(kind, key string) string { return t.LetterOf(kind, key) }
	// Mục lời văn.
	for k, v := range in.Sheet.Texts {
		if _, ok := t.Section(k); !ok {
			add(Issue{Rule: RuleTemplate, Severity: SevError, Section: k, Adjustment: -1, Message: "mục lời văn không có trong mẫu"})
			continue
		}
		if utf8.RuneCountInString(v) > MaxSectionText {
			add(Issue{Rule: RuleTemplate, Severity: SevError, Section: k, Adjustment: -1, Message: fmt.Sprintf("tối đa %d ký tự", MaxSectionText)})
		}
	}
	if in.Submit {
		for _, s := range t.Sections {
			if s.Required && in.Sheet.Texts[s.Key] == "" {
				add(Issue{Rule: RuleTemplate, Severity: SevError, Section: s.Key, Adjustment: -1, Message: "\"" + s.Title + "\" bắt buộc"})
			}
		}
	}
	grid := in.Grid && len(t.Columns) > 0
	if !grid {
		if len(in.Sheet.Cells) > 0 || len(in.Sheet.Adjustments) > 0 {
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: -1,
				Message: "phiếu tháng, quý, năm chỉ nhập lời văn — không nhập lại số đã có ở phiếu tuần (BR-DL-06)"})
		}
		return res
	}
	if len(in.Sheet.Cells) > MaxCells || len(in.Sheet.Adjustments) > MaxAdjustments {
		add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: -1, Message: "bảng quá lớn"})
		return res
	}
	// Ô nhập.
	for k, c := range in.Sheet.Cells {
		r, ok := byRef[k]
		if !ok {
			add(Issue{Rule: RuleTemplate, Severity: SevError, EntryRef: k, Adjustment: -1, Message: "hạng mục không thuộc phiếu của phòng"})
			continue
		}
		for _, col := range t.Columns {
			if col.Kind != ColInput {
				continue
			}
			v := c.Value(col.Input)
			if v == "" {
				continue
			}
			if col.Input == InDone && r.ModuleSourced {
				add(Issue{Rule: RuleTemplate, Severity: SevError, EntryRef: k, Letter: col.Letter, Adjustment: -1,
					Message: "hạng mục lấy từ module (BR-DL-10) — không nhập"})
				continue
			}
			switch col.DType {
			case DTypeNumber:
				d, ok := ParseNumber(v)
				switch {
				case !ok:
					add(Issue{Rule: RuleTemplate, Severity: SevError, EntryRef: k, Letter: col.Letter, Adjustment: -1, Message: "phải là số"})
				case col.NonNegative && d.IsNegative():
					msg := "không nhận số âm"
					if col.Input == InDone {
						msg = "không nhận số âm — số âm nhập ở khối \"Điều chỉnh kỳ trước\" (BR-DL-12)"
					}
					add(Issue{Rule: RuleTemplate, Severity: SevError, EntryRef: k, Letter: col.Letter, Adjustment: -1, Message: msg})
				}
			case DTypeInteger:
				if !reInteger.MatchString(v) || (col.NonNegative && strings.HasPrefix(v, "-")) {
					add(Issue{Rule: RuleTemplate, Severity: SevError, EntryRef: k, Letter: col.Letter, Adjustment: -1, Message: "phải là số nguyên không âm"})
				}
			default:
				if utf8.RuneCountInString(v) > MaxText {
					add(Issue{Rule: RuleTemplate, Severity: SevError, EntryRef: k, Letter: col.Letter, Adjustment: -1,
						Message: fmt.Sprintf("tối đa %d ký tự", MaxText)})
				}
			}
		}
	}
	// Khối Điều chỉnh kỳ trước.
	adjSum := map[uuid.UUID]decimal.Decimal{}
	gLetter := letter(ColFormula, FxAdjust)
	for i, a := range in.Sheet.Adjustments {
		r, ok := byRef[a.EntryRef]
		switch {
		case !ok:
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, Message: "chọn hạng mục của phiếu"})
			continue
		case r.ModuleSourced:
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef,
				Message: "hạng mục lấy từ module — sửa ở module của phòng (BR-DL-09)"})
			continue
		}
		pid, err := uuid.Parse(a.PeriodID)
		p, allowed := in.Periods[pid]
		switch {
		case a.PeriodID == "" || err != nil:
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef, Message: "bắt buộc chọn kỳ được điều chỉnh"})
		case !allowed:
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef,
				Message: "kỳ được điều chỉnh phải là kỳ KHTC tuần đã khóa, trước kỳ này (cùng năm kế hoạch hoặc năm trước)"})
		}
		d, okNum := ParseNumber(a.Amount)
		switch {
		case !okNum:
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef, Message: "số điều chỉnh phải là số"})
		case d.IsZero():
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef, Message: "số điều chỉnh khác 0"})
		}
		switch {
		case a.Reason == "" && okNum && d.IsNegative():
			add(Issue{Rule: RuleNegativeReason, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef, Letter: gLetter,
				Message: "số âm phải có lý do"})
		case a.Reason == "":
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef, Message: "bắt buộc nhập lý do điều chỉnh"})
		case utf8.RuneCountInString(a.Reason) > MaxReason:
			add(Issue{Rule: RuleTemplate, Severity: SevError, Adjustment: i, EntryRef: a.EntryRef, Message: fmt.Sprintf("lý do tối đa %d ký tự", MaxReason)})
		}
		if okNum && allowed && p.Year == in.PlanYear {
			adjSum[r.EntryRef] = adjSum[r.EntryRef].Add(d)
		}
	}
	// Ô hệ thống và quy tắc (3).
	for _, r := range in.Rows {
		cells := map[string]string{}
		prior := in.Prior[r.EntryRef]
		var done decimal.Decimal
		if c, ok := in.Sheet.Cells[r.EntryRef.String()]; ok {
			done, _ = ParseNumber(c.Done)
		}
		cum := prior.Add(done).Add(adjSum[r.EntryRef])
		rr := RowResult{Cells: cells}
		for _, col := range t.Columns {
			switch col.Kind {
			case ColFixed:
				switch col.Source {
				case SrcRowNo:
					cells[col.Letter] = fmt.Sprint(r.No)
				case SrcEntryName:
					cells[col.Letter] = r.Name
				}
			case ColFromPlan:
				switch col.Source {
				case SrcUnit:
					cells[col.Letter] = r.Unit
				case SrcPlannedQty:
					if r.PlannedQty != nil {
						cells[col.Letter] = FormatNumber(*r.PlannedQty)
					}
				case SrcScaleNote:
					cells[col.Letter] = r.ScaleNote
				}
			case ColPriorCumulative:
				if !r.ModuleSourced {
					cells[col.Letter] = FormatNumber(prior)
				}
			case ColInput:
				if col.Input == InDone && r.ModuleSourced && r.Module != nil && r.Module.InPeriod != nil {
					cells[col.Letter] = FormatNumber(*r.Module.InPeriod) // ô hệ thống điền (BR-DL-10), không nhập
				}
			case ColFormula:
				if r.ModuleSourced {
					// Số của hạng mục nguồn module lấy từ ảnh chụp "Cập nhật số liệu" (W11.64); chưa cập nhật → trống.
					if m := r.Module; m != nil && m.Cumulative != nil {
						switch col.Formula {
						case FxCumulative:
							cells[col.Letter] = FormatNumber(*m.Cumulative)
						case FxPercent:
							if r.PlannedQty != nil && r.PlannedQty.IsPositive() {
								cells[col.Letter] = FormatNumber(m.Cumulative.Mul(decimal.NewFromInt(100)).Div(*r.PlannedQty).Round(1))
							}
						}
					}
					continue
				}
				switch col.Formula {
				case FxAdjust:
					if v, ok := adjSum[r.EntryRef]; ok {
						cells[col.Letter] = FormatNumber(v)
					}
				case FxCumulative:
					cells[col.Letter] = FormatNumber(cum)
				case FxPercent:
					if r.PlannedQty != nil && r.PlannedQty.IsPositive() {
						cells[col.Letter] = FormatNumber(cum.Mul(decimal.NewFromInt(100)).Div(*r.PlannedQty).Round(1))
					}
				}
			}
		}
		if !r.ModuleSourced && r.PlannedQty != nil && r.PlannedQty.IsPositive() && cum.GreaterThan(*r.PlannedQty) {
			rr.OverPlan = true
			add(Issue{Rule: RuleOverPlan, Severity: SevWarning, EntryRef: r.EntryRef.String(), Letter: letter(ColFormula, FxCumulative),
				Adjustment: -1, Message: "lũy kế vượt 100% kế hoạch — kiểm tra lại, vẫn gửi được"})
		}
		res.Rows[r.EntryRef] = rr
	}
	return res
}
