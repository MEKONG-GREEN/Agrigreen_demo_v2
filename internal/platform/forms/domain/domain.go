// Package domain là quy tắc thuần của pf/forms (`cap/forms`, không I/O, không time.Now — R-12, không float — R-10):
// cấu hình bên dùng, định nghĩa trường biểu, kiểm số liệu nộp, máy trạng thái kỳ và bài nộp, nội dung ảnh chụp
// đóng băng. Cùng bất biến được DB kiểm lại (pf 000121: trg_transition, trg_bind_period, review_four_eyes).
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ (rule_version) và vào ảnh chụp.
const RuleVersion = "forms-2026.10"

// SnapshotSchema là định dạng nội dung ảnh chụp native.
const SnapshotSchema = "pf.forms.snapshot/v1"

var (
	reProfile = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	reLevel   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,19}$`)
	reCode    = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,63}$`)
	reKey     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	reUnit    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
	reInt     = regexp.MustCompile(`^-?[0-9]{1,18}$`)
	reDec     = regexp.MustCompile(`^-?[0-9]{1,18}(\.[0-9]{1,6})?$`)
)

// ValidateProfile kiểm cấu hình bên dùng: tên, cấp (≥ 2, không trùng), cấp phát hành/chốt/rà soát thuộc cây và
// không phải cấp lá, vai ánh xạ vào cấp có thật, có vai định nghĩa biểu.
func ValidateProfile(p port.Profile) error {
	if !reProfile.MatchString(p.Name) {
		return fmt.Errorf("profile %q: tên phải dạng [a-z][a-z0-9_]*", p.Name)
	}
	if len(p.Hierarchy) < 2 {
		return fmt.Errorf("profile %s: hierarchy cần ít nhất 2 cấp", p.Name)
	}
	for i, l := range p.Hierarchy {
		if !reLevel.MatchString(l) || slices.Contains(p.Hierarchy[:i], l) {
			return fmt.Errorf("profile %s: cấp %q không hợp lệ hoặc trùng", p.Name, l)
		}
	}
	leaf := p.Leaf()
	for field, l := range map[string]string{"publish_by": p.PublishBy, "close_by": p.CloseBy, "review_by": p.Reviewer()} {
		if !slices.Contains(p.Hierarchy, l) || l == leaf {
			return fmt.Errorf("profile %s: %s = %q phải là cấp trên cấp lá của hierarchy", p.Name, field, l)
		}
	}
	if len(p.Roles) == 0 {
		return fmt.Errorf("profile %s: roles bắt buộc", p.Name)
	}
	for r, l := range p.Roles {
		if r == "" || !slices.Contains(p.Hierarchy, l) {
			return fmt.Errorf("profile %s: vai %q → cấp %q không thuộc hierarchy", p.Name, r, l)
		}
	}
	if len(p.DefineBy) == 0 {
		return fmt.Errorf("profile %s: define_by bắt buộc", p.Name)
	}
	return nil
}

// Kiểu trường biểu.
const (
	TypeText    = "text"
	TypeInteger = "integer"
	TypeDecimal = "decimal" // chuỗi thập phân tối đa 6 chữ số lẻ (R-10, không float)
	TypeDate    = "date"    // YYYY-MM-DD
	TypeChoice  = "choice"
)

// Field là một trường của biểu.
type Field struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"` // choice
	Unit     string   `json:"unit,omitempty"`    // đơn vị đo hiển thị (ha, tấn…)
}

// ErrTemplate là lỗi định nghĩa biểu (field, thông báo).
type ErrTemplate struct{ Field, Msg string }

func (e ErrTemplate) Error() string { return e.Field + ": " + e.Msg }

// MaxFields là số trường tối đa của một biểu (khớp CHECK của pf_forms.template.fields).
const MaxFields = 500

// ValidateTemplate kiểm mã, tiêu đề và danh sách trường của một phiên bản biểu.
func ValidateTemplate(code, title string, fields []Field) error {
	if !reCode.MatchString(code) {
		return ErrTemplate{"code", "chữ HOA, số, . _ - (≤ 64 ký tự)"}
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(title)); n < 1 || n > 300 {
		return ErrTemplate{"title", "1–300 ký tự"}
	}
	if len(fields) == 0 || len(fields) > MaxFields {
		return ErrTemplate{"fields", fmt.Sprintf("1–%d trường", MaxFields)}
	}
	seen := map[string]bool{}
	for i, f := range fields {
		at := fmt.Sprintf("fields[%d]", i)
		if !reKey.MatchString(f.Key) || seen[f.Key] {
			return ErrTemplate{at + ".key", "dạng [a-z][a-z0-9_]*, không trùng"}
		}
		seen[f.Key] = true
		if n := utf8.RuneCountInString(f.Label); n < 1 || n > 300 {
			return ErrTemplate{at + ".label", "1–300 ký tự"}
		}
		switch f.Type {
		case TypeText, TypeInteger, TypeDecimal, TypeDate:
			if len(f.Options) > 0 {
				return ErrTemplate{at + ".options", "chỉ dùng với choice"}
			}
		case TypeChoice:
			if len(f.Options) == 0 || len(f.Options) > 100 {
				return ErrTemplate{at + ".options", "choice cần 1–100 lựa chọn"}
			}
		default:
			return ErrTemplate{at + ".type", "text | integer | decimal | date | choice"}
		}
	}
	return nil
}

// ValidateValues kiểm số liệu một bài nộp theo định nghĩa biểu: không có khóa lạ, đủ trường bắt buộc, đúng kiểu.
// Giá trị rỗng = không nhập. Trả lỗi đầu tiên dạng ErrTemplate (field = khóa trường).
func ValidateValues(fields []Field, values map[string]string) error {
	byKey := make(map[string]Field, len(fields))
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for k := range values {
		if _, ok := byKey[k]; !ok {
			return ErrTemplate{k, "trường không có trong biểu"}
		}
	}
	for _, f := range fields {
		v := strings.TrimSpace(values[f.Key])
		if v == "" {
			if f.Required {
				return ErrTemplate{f.Key, "bắt buộc"}
			}
			continue
		}
		var ok bool
		switch f.Type {
		case TypeText:
			ok = utf8.RuneCountInString(v) <= 2000
		case TypeInteger:
			ok = reInt.MatchString(v)
		case TypeDecimal:
			ok = reDec.MatchString(v)
		case TypeDate:
			_, err := time.Parse(time.DateOnly, v)
			ok = err == nil
		case TypeChoice:
			ok = slices.Contains(f.Options, v)
		}
		if !ok {
			return ErrTemplate{f.Key, "sai kiểu " + f.Type}
		}
	}
	return nil
}

// ValidateUnits kiểm danh sách đơn vị nhận của kỳ (không rỗng, không trùng, đúng dạng mã).
func ValidateUnits(units []string) error {
	if len(units) == 0 || len(units) > 5000 {
		return errors.New("units: 1–5000 đơn vị")
	}
	for i, u := range units {
		if !reUnit.MatchString(u) || slices.Contains(units[:i], u) {
			return fmt.Errorf("units: %q không hợp lệ hoặc trùng", u)
		}
	}
	return nil
}

// ValidateUnit kiểm một mã đơn vị.
func ValidateUnit(u string) bool { return reUnit.MatchString(u) }

// Reason kiểm lý do mở lại (10–500 ký tự sau khi bỏ khoảng trắng hai đầu).
func Reason(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 10 && n <= 500
}

// Trạng thái kỳ (phép chiếu sự kiện cuối).
const (
	PeriodOpen     = "open"
	PeriodClosed   = "closed"
	PeriodReopened = "reopened"
)

// Event là một sự kiện kỳ đã ghi.
type Event struct {
	ID     uuid.UUID
	Seq    int64
	Action string // published | closed | reopened
	Mode   string
	Units  []string
	Reason string
	Actor  uuid.UUID
	At     time.Time
}

// PeriodState là trạng thái kỳ suy từ sổ sự kiện (đã sắp theo seq).
type PeriodState struct {
	Status      string   // open | closed | reopened
	ReopenUnits []string // reopened: phạm vi (rỗng = cả kỳ)
	ReopenedAt  *time.Time
	Closes      int // số lần chốt
}

// StateOf chiếu trạng thái kỳ từ các sự kiện.
func StateOf(evs []Event) PeriodState {
	var s PeriodState
	for _, e := range evs {
		switch e.Action {
		case "published":
			s.Status = PeriodOpen
		case "closed":
			s.Status, s.ReopenUnits = PeriodClosed, nil
			s.Closes++
		case "reopened":
			at := e.At
			s.Status, s.ReopenUnits, s.ReopenedAt = PeriodReopened, e.Units, &at
		}
	}
	return s
}

// AcceptsUnit cho biết kỳ đang nhận bài của đơn vị u (khớp trg_bind_period).
func (s PeriodState) AcceptsUnit(u string) bool {
	switch s.Status {
	case PeriodOpen:
		return true
	case PeriodReopened:
		return len(s.ReopenUnits) == 0 || slices.Contains(s.ReopenUnits, u)
	}
	return false
}

// Trạng thái bài nộp của một đơn vị.
const (
	SubNone      = "not_submitted"
	SubSubmitted = "submitted"
	SubAccepted  = "accepted"
	SubReturned  = "returned"
)

// Review là kết quả rà soát một phiên bản.
type Review struct {
	ID       uuid.UUID
	Decision string // accepted | returned
	Reviewer uuid.UUID
	Note     string
	At       time.Time
}

// SubmissionStatus là trạng thái phiên bản cuối của bài nộp (r = rà soát của nó, nil nếu chưa).
func SubmissionStatus(hasVersion bool, r *Review) string {
	switch {
	case !hasVersion:
		return SubNone
	case r == nil:
		return SubSubmitted
	}
	return r.Decision
}

// CanResubmit cho biết đơn vị được nộp phiên bản mới: chưa nộp, bị trả lại, đang chờ (thay bản chờ), hoặc đã
// chấp nhận nhưng kỳ được mở lại SAU lúc chấp nhận.
func CanResubmit(status string, acceptedAt *time.Time, st PeriodState) bool {
	if status != SubAccepted {
		return true
	}
	return st.Status == PeriodReopened && st.ReopenedAt != nil && acceptedAt != nil && st.ReopenedAt.After(*acceptedAt)
}

// UnitSnapshot là một dòng của ảnh chụp native.
type UnitSnapshot struct {
	Unit         string            `json:"unit"`
	Status       string            `json:"status"`
	SubmissionID *string           `json:"submission_id"`
	Version      int64             `json:"version"`
	Values       map[string]string `json:"values"`
	SubmittedBy  *string           `json:"submitted_by"`
	ReviewedBy   *string           `json:"reviewed_by"`
}

// Snapshot là nội dung ảnh chụp đóng băng của kỳ native lúc chốt (§26: số đã chốt giữ nguyên dù sau đó nộp lại).
type Snapshot struct {
	Schema          string         `json:"schema"`
	RuleVersion     string         `json:"rule_version"`
	Profile         string         `json:"profile"`
	PeriodID        string         `json:"period_id"`
	Label           string         `json:"label"`
	TemplateID      string         `json:"template_id"`
	TemplateCode    string         `json:"template_code"`
	TemplateVersion int64          `json:"template_version"`
	SnapshotNo      int64          `json:"snapshot_no"`
	ClosedAt        string         `json:"closed_at"`
	Mode            string         `json:"mode"`
	Units           []UnitSnapshot `json:"units"`
}

// PendingReview đếm đơn vị còn bài chờ rà soát (validator trước chốt).
func PendingReview(units []UnitSnapshot) []string {
	var out []string
	for _, u := range units {
		if u.Status == SubSubmitted {
			out = append(out, u.Unit)
		}
	}
	return out
}
