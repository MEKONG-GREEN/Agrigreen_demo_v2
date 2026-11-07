package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	AppraisalNotStarted       = "not_started"
	AppraisalSelfSubmitted    = "self_submitted"
	AppraisalEntered          = "entered"
	AppraisalManagerSubmitted = "manager_submitted" // trạng thái lịch sử chỉ đọc
	AppraisalSent             = "sent"
	AppraisalChangeRequested  = "change_requested"
	AppraisalClosed           = "closed"
)

// AppraisalMember là bản chụp không PII tại ngày chốt: đơn vị/chức vụ/AccountRef không lấy lại từ client.
type AppraisalMember struct {
	StaffID        uuid.UUID  `json:"staff_id"`
	SubjectRef     uuid.UUID  `json:"subject_ref"`
	AccountRef     *uuid.UUID `json:"account_ref"`
	Code           string     `json:"code"`
	Type           StaffType  `json:"staff_type"`
	Group          string     `json:"group"`
	Unit           string     `json:"unit"`
	ManagementUnit string     `json:"management_unit"`
	Position       string     `json:"position"`
	PositionName   string     `json:"position_name"`
	PositionOrder  int        `json:"position_order"`
	UnitName       string     `json:"unit_name"`
	SummaryKind    string     `json:"summary_kind"`
	ExcludedReason string     `json:"excluded_reason,omitempty"`
}

// AppraisalQuarterSnapshot giữ kết quả/tham chiếu quý tại lúc mở đợt năm.
// Member chỉ phục vụ giao quyền lịch sử; lớp app bỏ trường này khỏi phản hồi.
type AppraisalQuarterSnapshot struct {
	Quarter      int             `json:"quarter"`
	PeriodID     uuid.UUID       `json:"period_id"`
	EntryVersion uuid.UUID       `json:"entry_version"`
	Version      int64           `json:"version"`
	Status       string          `json:"status"`
	Rating       string          `json:"rating"`
	Member       AppraisalMember `json:"member"`
}

type AppraisalEntry struct {
	QuarterResults     []AppraisalQuarterSnapshot `json:"quarter_results,omitempty"`
	Member             AppraisalMember            `json:"member"`
	Status             string                     `json:"status"`
	Self               AppraisalScoring           `json:"self"`
	Manager            AppraisalScoring           `json:"manager"`
	SelfRating         string                     `json:"self_rating"`
	CollectiveRating   string                     `json:"collective_rating"`
	OfficialRating     string                     `json:"official_rating"`
	ClosedRating       string                     `json:"closed_rating"`
	Reason             string                     `json:"reason"`
	PersonnelProposal  string                     `json:"personnel_proposal"`
	Note               string                     `json:"note"`
	CorrectionReason   string                     `json:"correction_reason"`
	ReturnedTo         string                     `json:"returned_to"`
	Reopened           bool                       `json:"reopened"`
	ProxyDocument      *uuid.UUID                 `json:"proxy_document,omitempty"`
	SelfSubmittedAt    *time.Time                 `json:"self_submitted_at,omitempty"`
	ManagerSubmittedAt *time.Time                 `json:"manager_submitted_at,omitempty"`
}

// AppraisalAuthority chỉ được dựng từ phiên server và bản chụp; không giải mã từ request HTTP.
type AppraisalAuthority struct {
	Actor             uuid.UUID
	Group, Unit       string
	Manager, ReadOnly bool
}

func (a AppraisalAuthority) center() bool { return a.Group == GroupAdmin || a.Group == GroupHCTH }
func (a AppraisalAuthority) self(m AppraisalMember) bool {
	return a.Actor != uuid.Nil && m.AccountRef != nil && *m.AccountRef == a.Actor
}
func (a AppraisalAuthority) management(m AppraisalMember) bool {
	if m.SummaryKind == "MS06" {
		return a.center()
	}
	return a.Group == GroupAdmin || (a.Manager && (a.Group == GroupManager || a.Group == GroupHCTH) && a.Unit != "" && a.Unit == m.ManagementUnit && !a.self(m))
}

// CanReadAppraisal không suy quyền từ tên/email hay đơn vị của người gọi do client tự khai.
func CanReadAppraisal(a AppraisalAuthority, m AppraisalMember) bool {
	if !slices.Contains([]string{GroupAdmin, GroupHCTH, GroupManager}, a.Group) || a.Actor == uuid.Nil {
		return false
	}
	return a.center() || a.management(m)
}

func CanManageAppraisal(a AppraisalAuthority, m AppraisalMember) bool {
	return !a.ReadOnly && CanReadAppraisal(a, m) && a.management(m)
}

type AppraisalStepLocks struct{ Closed bool }
type AppraisalEntryChange struct {
	Action            string `json:"action"`
	Version           int64  `json:"version"`
	SelfRating        string `json:"self_rating,omitempty"`
	CollectiveRating  string `json:"collective_rating,omitempty"`
	OfficialRating    string `json:"official_rating,omitempty"`
	Reason            string `json:"reason,omitempty"`
	PersonnelProposal string `json:"personnel_proposal,omitempty"`
	Note              string `json:"note,omitempty"`
}

// ChangeAppraisalEntry chỉ ghi mức đề xuất DG04–08; các kiểu điểm cũ chỉ dùng cho reader lịch sử.
// Cá nhân không có writer; lãnh đạo nhập cả hai mức từ bản tự đề xuất và kết quả họp đơn vị.
func ChangeAppraisalEntry(old AppraisalEntry, locks AppraisalStepLocks, a AppraisalAuthority, in AppraisalEntryChange) (AppraisalEntry, error) {
	e := old
	e.QuarterResults = slices.Clone(old.QuarterResults)
	if a.ReadOnly || !CanReadAppraisal(a, e.Member) {
		return e, appraisalInvalid("action", "không có quyền ghi đề xuất này")
	}
	if e.Member.ExcludedReason != "" {
		return e, appraisalInvalid("action", "người đã loại trừ khỏi đợt")
	}
	if locks.Closed && !e.Reopened && in.Action != "reopen" {
		return e, appraisalInvalid("action", "đợt đã chốt, phải mở lại riêng người có lý do")
	}
	if utf8.RuneCountInString(in.Reason) > 2000 || utf8.RuneCountInString(in.Note) > 2000 || utf8.RuneCountInString(in.PersonnelProposal) > 2000 {
		return e, appraisalInvalid("reason", "nội dung tối đa 2.000 ký tự")
	}
	switch in.Action {
	case "manager_submit":
		if !a.management(e.Member) {
			return e, appraisalInvalid("action", "chỉ cấp quản lý được phân công; Mẫu số 06 do HCTH nhập")
		}
		if e.Status != AppraisalNotStarted && e.Status != AppraisalEntered && e.Status != AppraisalChangeRequested {
			return e, appraisalInvalid("action", "đã gửi HCTH hoặc đã chốt; chỉ sửa khi được yêu cầu điều chỉnh")
		}
		if err := ValidateAppraisalProposal(in.SelfRating, in.CollectiveRating, in.Reason, true); err != nil {
			return e, err
		}
		e.SelfRating, e.CollectiveRating = in.SelfRating, in.CollectiveRating
		e.Reason, e.PersonnelProposal, e.Note = strings.TrimSpace(in.Reason), in.PersonnelProposal, in.Note
		e.Status, e.ReturnedTo = AppraisalEntered, ""
	case "send":
		if !a.management(e.Member) || e.Status != AppraisalEntered {
			return e, appraisalInvalid("action", "cần cấp quản lý nhập đủ đề xuất trước khi gửi HCTH")
		}
		if err := ValidateAppraisalProposal(e.SelfRating, e.CollectiveRating, e.Reason, true); err != nil {
			return e, err
		}
		e.Status, e.ReturnedTo = AppraisalSent, ""
	case "return":
		if !a.center() || e.Status != AppraisalSent {
			return e, appraisalInvalid("action", "chỉ HCTH yêu cầu điều chỉnh đề xuất đã gửi")
		}
		if utf8.RuneCountInString(strings.TrimSpace(in.Reason)) < 10 {
			return e, appraisalInvalid("reason", "lý do yêu cầu điều chỉnh cần ít nhất 10 ký tự")
		}
		e.Status, e.ReturnedTo, e.CorrectionReason = AppraisalChangeRequested, "manager", strings.TrimSpace(in.Reason)
		e.OfficialRating = ""
	case "official":
		if !a.center() || e.Status != AppraisalSent {
			return e, appraisalInvalid("action", "chỉ HCTH nhập kết luận cho đề xuất đã gửi")
		}
		if in.OfficialRating != "" && !slices.Contains(Ratings, in.OfficialRating) {
			return e, appraisalInvalid("official_rating", "chọn kết quả xếp loại chính thức hoặc để trống để dùng mức tập thể")
		}
		if strings.TrimSpace(in.Reason) != "" {
			if err := ValidateAppraisalProposal(e.SelfRating, e.CollectiveRating, in.Reason, true); err != nil {
				return e, err
			}
			e.Reason = strings.TrimSpace(in.Reason)
		}
		e.OfficialRating = in.OfficialRating
	case "reopen":
		if !a.center() || e.Status != AppraisalClosed {
			return e, appraisalInvalid("action", "chỉ HCTH mở lại một người đã chốt")
		}
		if utf8.RuneCountInString(strings.TrimSpace(in.Reason)) < 10 {
			return e, appraisalInvalid("reason", "lý do mở lại cần ít nhất 10 ký tự")
		}
		e.Reopened, e.Status, e.ReturnedTo, e.CorrectionReason = true, AppraisalChangeRequested, "manager", strings.TrimSpace(in.Reason)
		e.OfficialRating, e.ClosedRating = "", ""
	default:
		return e, appraisalInvalid("action", "không có thao tác đề xuất này")
	}
	return e, nil
}

func CloseAppraisalEntry(in AppraisalEntry) (AppraisalEntry, error) {
	if in.Member.ExcludedReason != "" {
		return in, nil
	}
	if in.Status != AppraisalSent {
		return in, appraisalInvalid("entries", "còn người chưa gửi đủ đề xuất HCTH")
	}
	if err := ValidateAppraisalProposal(in.SelfRating, in.CollectiveRating, in.Reason, true); err != nil {
		return in, err
	}
	in.ClosedRating = in.OfficialRating
	if in.ClosedRating == "" {
		in.ClosedRating = in.CollectiveRating
	}
	if !slices.Contains(Ratings, in.ClosedRating) {
		return in, fmt.Errorf("kết quả chốt không hợp lệ")
	}
	in.Status, in.Reopened, in.ReturnedTo = AppraisalClosed, false, ""
	return in, nil
}
