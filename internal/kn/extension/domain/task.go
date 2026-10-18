// Package domain: quy tắc nghiệp vụ khuyến nông — không phụ thuộc hạ tầng, không
// đọc đồng hồ hệ thống (R-12): thời điểm luôn truyền vào.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleVersion ghi vào cột rule_version của sổ.
const RuleVersion = "pf.extension/2026.09"

// DefaultSLAHours là SLA mặc định khi chưa có tham số đã duyệt (US-TASK-02 AC-2).
var DefaultSLAHours = decimal.NewFromInt(24)

// FieldError là lỗi theo trường để app/ quy về problem 422.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }
func (e FieldError) Unwrap() error { return e.Err }

func fieldErr(field, msg string) error { return FieldError{Field: field, Err: errors.New(msg)} }

// Status là trạng thái nhiệm vụ.
type Status string

// Trạng thái nhiệm vụ hỗ trợ.
const (
	StatusNew        Status = "new"
	StatusAssigned   Status = "assigned"
	StatusAccepted   Status = "accepted"
	StatusInProgress Status = "in_progress"
	StatusResolved   Status = "resolved"
)

// Action là sự việc trong sổ nhiệm vụ.
type Action string

// Các sự việc của nhiệm vụ.
const (
	ActionCreated   Action = "created"
	ActionAssigned  Action = "assigned"
	ActionAccepted  Action = "accepted"
	ActionProgress  Action = "progress"
	ActionResolved  Action = "resolved"
	ActionEscalated Action = "escalated"
	ActionReopened  Action = "reopened"
)

// Danh mục cố định của nhiệm vụ.
var (
	Categories = []string{"sau_benh", "ky_thuat", "co_gioi", "khac"}
	Priorities = []string{"low", "normal", "high", "urgent"}
)

// Source cho biết ai tạo nhiệm vụ.
const (
	SourceOfficer     = "officer"
	SourceCoopManager = "coop_manager"
	SourceHTXApp      = "htx_app"
)

// ErrTransition: sự việc không hợp lệ ở trạng thái hiện tại.
var ErrTransition = errors.New("trạng thái nhiệm vụ không cho phép thao tác này")

// Task là trạng thái hiện tại của nhiệm vụ hỗ trợ.
type Task struct {
	ID               uuid.UUID
	CommuneCode      string
	ProvinceCode     string
	CooperativeRef   uuid.UUID
	HouseholdRef     uuid.UUID
	PlotRef          uuid.UUID
	Category         string
	Description      string
	Priority         string
	Source           string
	Status           Status
	AssigneeRef      uuid.UUID
	CreatedBy        uuid.UUID
	CreatedAt        time.Time
	SLAHours         decimal.Decimal
	DueAt            time.Time
	EscalatedAt      *time.Time
	EscalatedUnit    string
	EscalationOrgRef uuid.UUID
	ResolvedAt       *time.Time
	LastNote         string
	LastSeq          int64
	SyncSeq          int64
	UpdatedAt        time.Time
}

// Overdue: quá SLA mà chưa hoàn thành (US-TASK-02 AC-2).
func (t Task) Overdue(now time.Time) bool {
	return t.Status != StatusResolved && now.After(t.DueAt)
}

// Event là một dòng sổ task_event.
type Event struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	TaskID           uuid.UUID
	Action           Action
	StatusAfter      Status
	AssigneeRef      uuid.UUID
	DueAt            time.Time
	Note             string
	EscalatedUnit    string
	EscalationOrgRef uuid.UUID
	Snapshot         map[string]any
	EventTime        time.Time
	RecordedAt       time.Time
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	Seq              int64
}

// StreamKey của một nhiệm vụ.
func StreamKey(taskID uuid.UUID) string { return "task:" + taskID.String() }

// DueAt = thời điểm tạo + SLA (giờ, decimal). Làm tròn tới giây.
func DueAt(created time.Time, slaHours decimal.Decimal) time.Time {
	secs := slaHours.Mul(decimal.NewFromInt(3600)).Round(0).IntPart()
	return created.Add(time.Duration(secs) * time.Second)
}

// ValidSLA kiểm SLA đọc từ tham số: 1 giờ – 30 ngày.
func ValidSLA(h decimal.Decimal) bool {
	return h.GreaterThanOrEqual(decimal.NewFromInt(1)) && h.LessThanOrEqual(decimal.NewFromInt(720))
}

// Dấu hiệu dữ liệu cá nhân trong văn bản tự do: SĐT Việt Nam, số CCCD 12 chữ số.
var (
	phoneRe      = regexp.MustCompile(`(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b`)
	nationalIDRe = regexp.MustCompile(`\b\d{12}\b`)
)

// ErrPIIInText: mô tả chứa SĐT hoặc số CCCD (R-04: dữ liệu cá nhân chỉ ở kho định danh).
var ErrPIIInText = errors.New("không ghi SĐT hoặc số CCCD vào mô tả — chọn nông hộ bằng mã tham chiếu")

// ContainsPII phát hiện SĐT/CCCD trong văn bản tự do.
func ContainsPII(s string) bool {
	return phoneRe.MatchString(s) || nationalIDRe.MatchString(s)
}

// NewTaskInput là dữ liệu vào đã chuẩn hóa để kiểm.
type NewTaskInput struct {
	CommuneCode string
	Category    string
	Description string
	Priority    string
}

// Validate kiểm thuộc tính khi tạo nhiệm vụ.
func (in *NewTaskInput) Validate() error {
	in.CommuneCode = strings.TrimSpace(in.CommuneCode)
	in.Description = strings.TrimSpace(in.Description)
	if in.Priority == "" {
		in.Priority = "normal"
	}
	switch {
	case in.CommuneCode == "":
		return fieldErr("commune_code", "bắt buộc")
	case !contains(Categories, in.Category):
		return fieldErr("category", "phải là một trong sau_benh, ky_thuat, co_gioi, khac")
	case !contains(Priorities, in.Priority):
		return fieldErr("priority", "phải là một trong low, normal, high, urgent")
	case len([]rune(in.Description)) < 10 || len([]rune(in.Description)) > 2000:
		return fieldErr("description", "10–2000 ký tự")
	case ContainsPII(in.Description):
		return FieldError{Field: "description", Err: ErrPIIInText}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// MinNoteLen là độ dài tối thiểu của kết quả xử lý / lý do mở lại / ghi chú chuyển tiếp.
const MinNoteLen = 10

// Transition tính trạng thái sau sự việc a. Trả lỗi nếu không hợp lệ.
//
//   - assigned: mọi trạng thái chưa hoàn thành (phân công lại được)
//   - accepted: chỉ từ assigned
//   - progress: từ accepted hoặc in_progress, bắt buộc ghi chú
//   - resolved: từ assigned/accepted/in_progress, bắt buộc kết quả ≥ 10 ký tự (US-TASK-02 AC-1)
//   - escalated: mọi trạng thái chưa hoàn thành, không đổi trạng thái
//   - reopened: chỉ từ resolved, bắt buộc lý do ≥ 10 ký tự
func Transition(cur Status, a Action, note string, hasAssignee bool) (Status, error) {
	note = strings.TrimSpace(note)
	switch a {
	case ActionAssigned:
		if cur == StatusResolved {
			return "", ErrTransition
		}
		return StatusAssigned, nil
	case ActionAccepted:
		if cur != StatusAssigned {
			return "", ErrTransition
		}
		return StatusAccepted, nil
	case ActionProgress:
		if cur != StatusAccepted && cur != StatusInProgress {
			return "", ErrTransition
		}
		if note == "" {
			return "", fieldErr("note", "ghi nội dung tiến độ")
		}
		return StatusInProgress, nil
	case ActionResolved:
		if cur != StatusAssigned && cur != StatusAccepted && cur != StatusInProgress {
			return "", ErrTransition
		}
		if len([]rune(note)) < MinNoteLen {
			return "", fieldErr("note", "nhiệm vụ chỉ được đóng khi có kết quả xử lý cụ thể (≥ 10 ký tự)")
		}
		return StatusResolved, nil
	case ActionEscalated:
		if cur == StatusResolved {
			return "", ErrTransition
		}
		return cur, nil
	case ActionReopened:
		if cur != StatusResolved {
			return "", ErrTransition
		}
		if len([]rune(note)) < MinNoteLen {
			return "", fieldErr("note", "nêu lý do mở lại (≥ 10 ký tự)")
		}
		if hasAssignee {
			return StatusAssigned, nil
		}
		return StatusNew, nil
	}
	return "", fieldErr("action", "không hỗ trợ")
}

// ParseAction đọc sự việc do người dùng/thiết bị gửi (không gồm created).
func ParseAction(s string) (Action, bool) {
	switch a := Action(s); a {
	case ActionAssigned, ActionAccepted, ActionProgress, ActionResolved, ActionEscalated, ActionReopened:
		return a, true
	}
	return "", false
}

// ContainsPIINote kiểm ghi chú tự do (kết quả xử lý, lý do) không chứa SĐT/CCCD.
func ContainsPIINote(note string) error {
	if ContainsPII(note) {
		return FieldError{Field: "note", Err: ErrPIIInText}
	}
	return nil
}
