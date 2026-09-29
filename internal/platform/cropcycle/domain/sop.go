package domain

import (
	"strconv"
	"strings"
	"time"
)

// Phạm vi và trạng thái SOP.
const (
	ScopeSystem = "system"
	ScopeHTX    = "htx"

	TemplateDraft     = "draft"
	TemplatePublished = "published"
	TemplateRetired   = "retired"

	VersionDraft     = "draft"
	VersionPublished = "published"
)

// Giới hạn của bước SOP.
const (
	MaxSteps       = 100
	MinDayOffset   = -60
	MaxDayOffset   = 365
	MaxWindowDays  = 60
	MaxPHIDays     = 120
	DefaultWindow  = 3
	MaxStepNameLen = 200
)

// Step là một bước của quy trình SOP. DayOffset tính từ ngày gieo sạ.
type Step struct {
	Seq           int      `json:"seq"`
	Name          string   `json:"name"`
	StageCode     string   `json:"stage_code"`
	ActivityCode  string   `json:"activity_code"`
	DayOffset     int      `json:"day_offset"`
	WindowDays    int      `json:"window_days"`
	Mandatory     bool     `json:"mandatory"`
	EvidenceKinds []string `json:"evidence_kinds"`
	PHIDays       *int     `json:"phi_days,omitempty"`
	Instruction   string   `json:"instruction,omitempty"`
}

// MsgStepOrder là thông báo của HTX US-CAT-01 AC-6 khi thứ tự bước không liên tục.
const MsgStepOrder = "Thứ tự bước không hợp lệ, vui lòng kiểm tra lại."

// StepOrderError: seq các bước không liên tục 1..n.
type StepOrderError struct{ Index int }

func (e StepOrderError) Error() string {
	return "cropcycle: thứ tự bước không liên tục tại vị trí " + strconv.Itoa(e.Index+1)
}

// NormalizeSteps chuẩn hóa và kiểm danh sách bước. Seq phải liên tục 1..n theo
// đúng thứ tự gửi lên (HTX US-CAT-01 AC-6) — không tự đánh số lại. Trả
// StepOrderError khi sai thứ tự, FieldError khi sai trường.
func NormalizeSteps(in []Step) ([]Step, error) {
	if len(in) > MaxSteps {
		return nil, FieldError{"steps", "tối đa " + strconv.Itoa(MaxSteps) + " bước"}
	}
	out := make([]Step, 0, len(in))
	for i, s := range in {
		f := "steps[" + strconv.Itoa(i) + "]."
		if s.Seq != i+1 {
			return nil, StepOrderError{Index: i}
		}
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" || len([]rune(s.Name)) > MaxStepNameLen {
			return nil, FieldError{f + "name", "bắt buộc, tối đa 200 ký tự"}
		}
		s.ActivityCode = strings.TrimSpace(s.ActivityCode)
		if !KnownActivity(s.ActivityCode) {
			return nil, FieldError{f + "activity_code", "phải là lam_dat, gieo_sa, tuoi_nuoc, bon_phan, phun_thuoc, kiem_tra hoặc thu_hoach"}
		}
		s.StageCode = strings.TrimSpace(s.StageCode)
		if s.StageCode != "" && !ValidCode(s.StageCode) {
			return nil, FieldError{f + "stage_code", "dạng [a-z][a-z0-9_]{1,39}"}
		}
		if s.DayOffset < MinDayOffset || s.DayOffset > MaxDayOffset {
			return nil, FieldError{f + "day_offset", "trong khoảng -60..365 ngày so với ngày gieo sạ"}
		}
		if s.WindowDays < 0 || s.WindowDays > MaxWindowDays {
			return nil, FieldError{f + "window_days", "trong khoảng 0..60 ngày"}
		}
		if s.PHIDays != nil && (*s.PHIDays < 0 || *s.PHIDays > MaxPHIDays) {
			return nil, FieldError{f + "phi_days", "trong khoảng 0..120 ngày"}
		}
		kinds := make([]string, 0, len(s.EvidenceKinds))
		seen := map[string]bool{}
		for _, k := range s.EvidenceKinds {
			k = strings.TrimSpace(k)
			if !KnownEvidenceKind(k) {
				return nil, FieldError{f + "evidence_kinds", "loại bằng chứng không hợp lệ: " + k}
			}
			if !seen[k] {
				seen[k] = true
				kinds = append(kinds, k)
			}
		}
		s.EvidenceKinds = kinds
		instr, err := CleanText(f+"instruction", s.Instruction, 2000)
		if err != nil {
			return nil, err
		}
		s.Instruction = instr
		out = append(out, s)
	}
	return out, nil
}

// HarvestStep trả bước thu hoạch đầu tiên theo seq; ok = false nếu không có.
func HarvestStep(steps []Step) (Step, bool) {
	for _, s := range steps {
		if s.ActivityCode == ActThuHoach {
			return s, true
		}
	}
	return Step{}, false
}

// StepBySeq trả bước theo seq.
func StepBySeq(steps []Step, seq int) (Step, bool) {
	if seq >= 1 && seq <= len(steps) && steps[seq-1].Seq == seq {
		return steps[seq-1], true
	}
	for _, s := range steps {
		if s.Seq == seq {
			return s, true
		}
	}
	return Step{}, false
}

// SeasonDates là các mốc của vụ suy từ SOP đã ghim.
type SeasonDates struct {
	ExpectedHarvest time.Time // sạ + offset bước thu hoạch
	HarvestOpen     time.Time // sạ + offset − cửa sổ: sớm nhất được khai sản lượng
}

// DatesFor tính mốc vụ từ ngày sạ và bước thu hoạch (g2-conventions: cổng thu
// hoạch theo SOP đã ghim, không cứng 60 ngày). HarvestOpen không sớm hơn ngày sạ.
func DatesFor(sowing time.Time, harvest Step) SeasonDates {
	d := SeasonDates{
		ExpectedHarvest: AddDays(sowing, harvest.DayOffset),
		HarvestOpen:     AddDays(sowing, harvest.DayOffset-harvest.WindowDays),
	}
	if d.ExpectedHarvest.Before(sowing) {
		d.ExpectedHarvest = sowing
	}
	if d.HarvestOpen.Before(sowing) {
		d.HarvestOpen = sowing
	}
	return d
}
