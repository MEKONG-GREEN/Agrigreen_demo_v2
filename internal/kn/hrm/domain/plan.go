package domain

// W11.27 (BRD HCTH v3.0 FN-11, UR-HC-23, UR-LDP-03; D3 H7): KẾ HOẠCH NHÂN LỰC NĂM — quy tắc thuần.
//
// Kỳ kế hoạch năm là MỘT kỳ native của cap/forms, profile "hrm_plan" (đơn vị lá = đơn vị nội bộ W11.07). Bài nộp của đơn
// vị là bộ giá trị PlanValues: số hiện có và số dự kiến nghỉ hưu do máy chủ TỰ TÍNH từ hồ sơ tại ngày chốt (BR-01), số
// dự kiến nghỉ/chuyển khác, số cần bổ sung, nhu cầu theo vị trí do lãnh đạo đơn vị nhập. Nhu cầu theo kế hoạch là số tự
// tính. Sau chốt chỉ điều chỉnh có lý do (BR-02) — sổ plan_adjustment của pf_hrm.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// PlanTemplateCode là mã biểu của kỳ kế hoạch nhân lực trên cap/forms.
const PlanTemplateCode = "HRM_PLAN"

// PlanTemplateTitle là tiêu đề biểu.
const PlanTemplateTitle = "Kế hoạch nhân lực năm"

// Khóa trường của biểu HRM_PLAN (giá trị bài nộp cap/forms là chuỗi).
const (
	PlanCurrent    = "current"    // Số nhân sự hiện có (tự tính)
	PlanRetiring   = "retiring"   // Dự kiến nghỉ hưu trong năm kế hoạch (tự tính)
	PlanLeaving    = "leaving"    // Dự kiến nghỉ, chuyển công tác khác
	PlanAdditional = "additional" // Số cần bổ sung
	PlanPositions  = "positions"  // Nhu cầu theo vị trí — JSON mảng PlanPosition
	PlanNote       = "note"       // Ghi chú của đơn vị
)

// PlanFormField là trường biểu HRM_PLAN (cùng hình dạng JSON với forms/domain.Field — pf/forms nhận qua TemplateDef).
type PlanFormField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	NonNegative bool   `json:"non_negative,omitempty"`
	Group       string `json:"group,omitempty"`
}

// PlanFields là định nghĩa biểu HRM_PLAN. Đổi trường → phiên bản biểu mới (kỳ đã mở giữ phiên bản cũ).
func PlanFields() []PlanFormField {
	return []PlanFormField{
		{Key: PlanCurrent, Label: "Số nhân sự hiện có", Type: "integer", Required: true, NonNegative: true, Group: "Số liệu hiện có (tự động)"},
		{Key: PlanRetiring, Label: "Dự kiến nghỉ hưu", Type: "integer", Required: true, NonNegative: true, Group: "Số liệu hiện có (tự động)"},
		{Key: PlanLeaving, Label: "Dự kiến nghỉ, chuyển công tác khác", Type: "integer", Required: true, NonNegative: true, Group: "Kế hoạch của đơn vị"},
		{Key: PlanAdditional, Label: "Số cần bổ sung", Type: "integer", Required: true, NonNegative: true, Group: "Kế hoạch của đơn vị"},
		{Key: PlanPositions, Label: "Nhu cầu theo vị trí", Type: "text", Group: "Nhu cầu theo vị trí"},
		{Key: PlanNote, Label: "Ghi chú", Type: "text", Group: "Kế hoạch của đơn vị"},
	}
}

// Giới hạn dữ liệu kế hoạch.
const (
	PlanMaxPositions = 10   // số dòng nhu cầu theo vị trí
	PlanMaxCount     = 9999 // trần mọi số người
	PlanMaxNote      = 500
	PlanMinYear      = 2020
	PlanMaxYear      = 2100
)

// PlanPosition là một dòng "nhu cầu theo vị trí" (vị trí, trình độ yêu cầu, số lượng, thời điểm cần, lý do).
type PlanPosition struct {
	Title  string `json:"title"`
	Level  string `json:"level,omitempty"`
	Count  int    `json:"count"`
	When   string `json:"when,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// PlanInput là phần lãnh đạo đơn vị nhập (nháp, gửi, điều chỉnh sau chốt).
type PlanInput struct {
	Leaving    int            `json:"leaving"`
	Additional int            `json:"additional"`
	Positions  []PlanPosition `json:"positions"`
	Note       string         `json:"note,omitempty"`
}

// PlanValues là bộ giá trị đầy đủ của kế hoạch một đơn vị (tự tính + nhập).
type PlanValues struct {
	Current  int `json:"current"`
	Retiring int `json:"retiring"`
	PlanInput
}

// Demand là nhu cầu theo kế hoạch = hiện có − dự kiến nghỉ hưu − dự kiến nghỉ/chuyển khác + cần bổ sung (W11.27 giả định:
// prototype HTML không trừ "nghỉ, chuyển khác" — xem docs/kn/gia-dinh/W11.27.md).
func (v PlanValues) Demand() int { return v.Current - v.Retiring - v.Leaving + v.Additional }

// PlanFieldError là lỗi kiểm dữ liệu kế hoạch (trường, thông báo).
type PlanFieldError struct{ Field, Msg string }

func (e PlanFieldError) Error() string { return e.Field + ": " + e.Msg }

func countOK(n int) bool { return n >= 0 && n <= PlanMaxCount }

// NormalizePlanInput kiểm và chuẩn hóa phần nhập: số không âm (≤ 9999), tối đa 10 vị trí (vị trí bắt buộc ≤ 200 ký tự,
// số lượng 1–9999, trình độ/thời điểm ≤ 100, lý do ≤ 500), ghi chú ≤ 500.
func NormalizePlanInput(in PlanInput) (PlanInput, error) {
	if !countOK(in.Leaving) {
		return in, PlanFieldError{PlanLeaving, "số nguyên 0–9999"}
	}
	if !countOK(in.Additional) {
		return in, PlanFieldError{PlanAdditional, "số nguyên 0–9999"}
	}
	if len(in.Positions) > PlanMaxPositions {
		return in, PlanFieldError{PlanPositions, fmt.Sprintf("tối đa %d vị trí", PlanMaxPositions)}
	}
	out := in
	out.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(out.Note) > PlanMaxNote {
		return in, PlanFieldError{PlanNote, "tối đa 500 ký tự"}
	}
	out.Positions = make([]PlanPosition, 0, len(in.Positions))
	for i, p := range in.Positions {
		p.Title, p.Level, p.When, p.Reason = strings.TrimSpace(p.Title), strings.TrimSpace(p.Level), strings.TrimSpace(p.When),
			strings.TrimSpace(p.Reason)
		f := fmt.Sprintf("%s[%d]", PlanPositions, i)
		switch {
		case p.Title == "" || utf8.RuneCountInString(p.Title) > 200:
			return in, PlanFieldError{f + ".title", "vị trí bắt buộc, tối đa 200 ký tự"}
		case p.Count < 1 || p.Count > PlanMaxCount:
			return in, PlanFieldError{f + ".count", "số lượng 1–9999"}
		case utf8.RuneCountInString(p.Level) > 100 || utf8.RuneCountInString(p.When) > 100:
			return in, PlanFieldError{f, "trình độ, thời điểm tối đa 100 ký tự"}
		case utf8.RuneCountInString(p.Reason) > 500:
			return in, PlanFieldError{f + ".reason", "lý do tối đa 500 ký tự"}
		}
		out.Positions = append(out.Positions, p)
	}
	return out, nil
}

// FormValues đổi bộ giá trị sang giá trị bài nộp cap/forms (chuỗi). Nhu cầu theo vị trí là JSON gọn.
func (v PlanValues) FormValues() (map[string]string, error) {
	pos := v.Positions
	if pos == nil {
		pos = []PlanPosition{}
	}
	raw, err := json.Marshal(pos)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCount(raw) > 2000 {
		return nil, PlanFieldError{PlanPositions, "nhu cầu theo vị trí quá dài (tối đa 2.000 ký tự khi lưu) — rút gọn lý do"}
	}
	out := map[string]string{
		PlanCurrent: strconv.Itoa(v.Current), PlanRetiring: strconv.Itoa(v.Retiring), PlanLeaving: strconv.Itoa(v.Leaving),
		PlanAdditional: strconv.Itoa(v.Additional), PlanPositions: string(raw),
	}
	if v.Note != "" {
		out[PlanNote] = v.Note
	}
	return out, nil
}

// PlanValuesFromForm đọc lại giá trị bài nộp cap/forms (đã kiểm khi nộp); lỗi nếu sai hình dạng.
func PlanValuesFromForm(m map[string]string) (PlanValues, error) {
	var v PlanValues
	for _, x := range []struct {
		key string
		dst *int
	}{{PlanCurrent, &v.Current}, {PlanRetiring, &v.Retiring}, {PlanLeaving, &v.Leaving}, {PlanAdditional, &v.Additional}} {
		n, err := strconv.Atoi(strings.TrimSpace(m[x.key]))
		if err != nil || !countOK(n) {
			return v, PlanFieldError{x.key, "số nguyên 0–9999"}
		}
		*x.dst = n
	}
	v.Positions = []PlanPosition{}
	if s := strings.TrimSpace(m[PlanPositions]); s != "" {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v.Positions); err != nil {
			return v, PlanFieldError{PlanPositions, "JSON mảng vị trí"}
		}
	}
	v.Note = m[PlanNote]
	in, err := NormalizePlanInput(v.PlanInput)
	if err != nil {
		return v, err
	}
	v.PlanInput = in
	return v, nil
}

// ValidatePlanForm là quy tắc chặn của bài nộp hrm_plan (Validator của cap/forms): hình dạng nhu cầu theo vị trí, giới
// hạn số. Trả danh sách (trường, lý do); rỗng = qua.
func ValidatePlanForm(templateCode string, values map[string]string) [][2]string {
	if templateCode != PlanTemplateCode {
		return [][2]string{{"template", "profile hrm_plan chỉ nhận biểu " + PlanTemplateCode}}
	}
	if _, err := PlanValuesFromForm(values); err != nil {
		if fe, ok := err.(PlanFieldError); ok {
			return [][2]string{{fe.Field, fe.Msg}}
		}
		return [][2]string{{"values", err.Error()}}
	}
	return nil
}

// ValidPlanYear kiểm năm kế hoạch.
func ValidPlanYear(y int) bool { return y >= PlanMinYear && y <= PlanMaxYear }

// PlanPeriodKey là khóa kỳ năm (lịch kỳ hệ thống W11.03: kind year, khóa "2027").
func PlanPeriodKey(year int) string { return strconv.Itoa(year) }

// PlanLabel là nhãn kỳ.
func PlanLabel(year int) string { return fmt.Sprintf("Kế hoạch nhân lực năm %d", year) }

// RetireOn trả ngày đến tuổi nghỉ hưu của người (birth + limit tháng); ok = false nếu thiếu giới tính/ngày sinh/tham số.
func (r RetirementAges) RetireOn(p Person) (time.Time, bool) {
	lim := r.limit(p)
	if lim <= 0 || p.Birth == nil {
		return time.Time{}, false
	}
	return p.Birth.AddDate(0, lim, 0), true
}

// RetiringIn cho biết người (đang công tác tại ngày chốt) dự kiến nghỉ hưu trong năm year: ngày đủ tuổi ≤ 31/12/year
// (kể cả người đã quá tuổi mà còn công tác). known = false nếu thiếu dữ liệu để tính.
func (r RetirementAges) RetiringIn(p Person, year int) (retiring, known bool) {
	on, ok := r.RetireOn(p)
	if !ok {
		return false, false
	}
	return !on.After(time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)), true
}

// Trạng thái hiển thị của kế hoạch một đơn vị (màn FN-11, prototype HTML: Chưa lập, Đang lập, Đã gửi, Trễ hạn, Đã chốt) —
// thêm "Yêu cầu điều chỉnh" (rà soát trả lại) và "Đã xác nhận" (HCTH chấp nhận).
const (
	PlanStatusNone      = "not_started" // Chưa lập
	PlanStatusDraft     = "draft"       // Đang lập (có nháp, chưa gửi)
	PlanStatusSubmitted = "submitted"   // Đã gửi (chờ HCTH)
	PlanStatusReturned  = "returned"    // Yêu cầu điều chỉnh
	PlanStatusAccepted  = "accepted"    // Đã xác nhận
	PlanStatusClosed    = "closed"      // Đã chốt (kỳ chốt, đơn vị đã gửi)
	PlanStatusMissed    = "missed"      // Không gửi (kỳ chốt, đơn vị chưa gửi)
)

// PlanStatus chiếu trạng thái hiển thị từ trạng thái kỳ, số phiên bản đã gửi, rà soát cuối và nháp.
func PlanStatus(periodClosed bool, versions int64, decision string, hasDraftAfter bool) string {
	switch {
	case periodClosed && versions == 0:
		return PlanStatusMissed
	case periodClosed:
		return PlanStatusClosed
	case versions == 0 && hasDraftAfter:
		return PlanStatusDraft
	case versions == 0:
		return PlanStatusNone
	case decision == "returned" && hasDraftAfter:
		return PlanStatusDraft
	case decision == "returned":
		return PlanStatusReturned
	case decision == "accepted":
		return PlanStatusAccepted
	}
	return PlanStatusSubmitted
}
