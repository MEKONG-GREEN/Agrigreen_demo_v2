package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	AppraisalNotStarted       = "not_started"
	AppraisalSelfSubmitted    = "self_submitted"
	AppraisalManagerSubmitted = "manager_submitted"
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

type AppraisalEntry struct {
	Member            AppraisalMember  `json:"member"`
	Status            string           `json:"status"`
	Self              AppraisalScoring `json:"self"`
	Manager           AppraisalScoring `json:"manager"`
	SelfRating        string           `json:"self_rating"`
	CollectiveRating  string           `json:"collective_rating"`
	OfficialRating    string           `json:"official_rating"`
	ClosedRating      string           `json:"closed_rating"`
	Reason            string           `json:"reason"`
	PersonnelProposal string           `json:"personnel_proposal"`
	Note              string           `json:"note"`
	CorrectionReason  string           `json:"correction_reason"`
	ReturnedTo        string           `json:"returned_to"`
	Reopened          bool             `json:"reopened"`
	ProxyDocument     *uuid.UUID       `json:"proxy_document,omitempty"`
}

// AppraisalAuthority chỉ được dựng từ phiên server và bản chụp; không giải mã từ request HTTP.
type AppraisalAuthority struct {
	Actor             uuid.UUID
	Group, Unit       string
	Manager, ReadOnly bool
	ProofVerified     bool
}

func (a AppraisalAuthority) center() bool { return a.Group == GroupAdmin || a.Group == GroupHCTH }
func (a AppraisalAuthority) self(m AppraisalMember) bool {
	return a.Actor != uuid.Nil && m.AccountRef != nil && *m.AccountRef == a.Actor
}
func (a AppraisalAuthority) management(m AppraisalMember) bool {
	if m.SummaryKind == "MS06" {
		return a.center()
	}
	return a.Group == GroupAdmin || (a.Manager && a.Unit != "" && a.Unit == m.ManagementUnit && !a.self(m))
}

// CanReadAppraisal không suy quyền từ tên/email hay đơn vị của người gọi do client tự khai.
func CanReadAppraisal(a AppraisalAuthority, m AppraisalMember) bool {
	if !slices.Contains([]string{GroupAdmin, GroupHCTH, GroupManager, GroupTinh, GroupXa}, a.Group) || a.Actor == uuid.Nil {
		return false
	}
	return a.center() || a.self(m) || a.management(m)
}

func CanSelfAppraisal(a AppraisalAuthority, m AppraisalMember) bool {
	return !a.ReadOnly && CanReadAppraisal(a, m) && a.self(m)
}

func CanManageAppraisal(a AppraisalAuthority, m AppraisalMember) bool {
	return !a.ReadOnly && CanReadAppraisal(a, m) && a.management(m)
}

type AppraisalStepLocks struct{ Self, Manager, Closed bool }
type AppraisalEntryChange struct {
	Action            string            `json:"action"`
	Version           int64             `json:"version"`
	Answers           []AppraisalAnswer `json:"answers,omitempty"`
	SelfRating        string            `json:"self_rating,omitempty"`
	CollectiveRating  string            `json:"collective_rating,omitempty"`
	OfficialRating    string            `json:"official_rating,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	PersonnelProposal string            `json:"personnel_proposal,omitempty"`
	Note              string            `json:"note,omitempty"`
	ReturnTo          string            `json:"return_to,omitempty"`
	ProxyDocument     *uuid.UUID        `json:"proxy_document,omitempty"`
}

// ChangeAppraisalEntry không ghi DB, không I/O. Gọi sau khi ứng dụng đã kiểm tenant, idempotency và phiên bản.
func ChangeAppraisalEntry(old AppraisalEntry, form AppraisalForm, locks AppraisalStepLocks, a AppraisalAuthority, in AppraisalEntryChange) (AppraisalEntry, error) {
	e := old
	e.Self.Answers = slices.Clone(old.Self.Answers)
	e.Manager.Answers = slices.Clone(old.Manager.Answers)
	if old.Member.AccountRef != nil {
		account := *old.Member.AccountRef
		e.Member.AccountRef = &account
	}
	if old.ProxyDocument != nil {
		document := *old.ProxyDocument
		e.ProxyDocument = &document
	}
	if a.ReadOnly || !CanReadAppraisal(a, e.Member) {
		return e, appraisalInvalid("action", "không có quyền ghi phiếu này")
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
	case "self_draft", "self_submit", "proxy_self":
		proxy := in.Action == "proxy_self"
		if (!proxy && !a.self(e.Member)) || (proxy && (!a.management(e.Member) || !a.ProofVerified || in.ProxyDocument == nil)) {
			return e, appraisalInvalid("action", "chỉ chủ thể tự chấm; nhập thay cần người quản lý và bản scan đã kiểm quyền")
		}
		if (locks.Self && !e.Reopened) || !(e.Status == AppraisalNotStarted || (e.Status == AppraisalChangeRequested && e.ReturnedTo == "self")) {
			return e, appraisalInvalid("action", "bước tự đánh giá đã khóa hoặc đã nộp")
		}
		complete := in.Action != "self_draft"
		score, err := ScoreAppraisal(form, in.Answers, complete)
		if err != nil {
			return e, err
		}
		rating := in.SelfRating
		if rating == "" && score.Complete {
			rating = score.Suggested
		}
		if rating != "" {
			if err := ValidateAppraisalProposal(rating, "", in.Reason, false); err != nil {
				return e, err
			}
		}
		e.Self, e.SelfRating, e.Reason, e.Note = score, rating, strings.TrimSpace(in.Reason), in.Note
		if proxy {
			proof := *in.ProxyDocument
			e.ProxyDocument = &proof
		}
		if complete {
			e.Status = AppraisalSelfSubmitted
			e.ReturnedTo = ""
			e.Manager = AppraisalScoring{}
			e.CollectiveRating = ""
			e.OfficialRating = ""
		}
	case "manager_draft", "manager_submit":
		if !a.management(e.Member) {
			return e, appraisalInvalid("action", "chỉ cấp quản lý được phân công; MS06 do HCTH nhập kết quả tập thể")
		}
		if (locks.Manager && !e.Reopened) || !(e.Status == AppraisalSelfSubmitted || e.Status == AppraisalManagerSubmitted || (e.Status == AppraisalChangeRequested && e.ReturnedTo == "manager")) {
			return e, appraisalInvalid("action", "chưa có phiếu tự nộp, đã gửi HCTH hoặc bước quản lý đã khóa")
		}
		complete := in.Action == "manager_submit"
		score, err := ScoreAppraisal(form, in.Answers, complete)
		if err != nil {
			return e, err
		}
		collective := in.CollectiveRating
		if collective == "" && score.Complete {
			collective = score.Suggested
		}
		if err := ValidateAppraisalProposal(e.SelfRating, collective, in.Reason, complete); err != nil {
			return e, err
		}
		e.Manager, e.CollectiveRating, e.Reason = score, collective, strings.TrimSpace(in.Reason)
		e.PersonnelProposal, e.Note = in.PersonnelProposal, in.Note
		if complete {
			e.Status = AppraisalManagerSubmitted
			e.ReturnedTo = ""
		} else {
			e.Status = AppraisalSelfSubmitted
		}
	case "send":
		if !a.management(e.Member) || (locks.Manager && !e.Reopened) || e.Status != AppraisalManagerSubmitted {
			return e, appraisalInvalid("action", "cần cấp quản lý hoàn tất đánh giá trước khi gửi HCTH")
		}
		if err := ValidateAppraisalProposal(e.SelfRating, e.CollectiveRating, e.Reason, true); err != nil {
			return e, err
		}
		e.Status = AppraisalSent
		e.ReturnedTo = ""
	case "return":
		if utf8.RuneCountInString(strings.TrimSpace(in.Reason)) < 10 {
			return e, appraisalInvalid("reason", "lý do trả phiếu cần ít nhất 10 ký tự")
		}
		if in.ReturnTo == "self" {
			if !a.management(e.Member) || e.Status == AppraisalNotStarted || e.Status == AppraisalClosed || e.Status == AppraisalSent || (locks.Self && !a.center()) {
				return e, appraisalInvalid("action", "không trả được phiếu về bước tự đánh giá")
			}
			if a.center() && locks.Self {
				e.Reopened = true
			}
		} else if in.ReturnTo == "manager" {
			if !a.center() || e.Status != AppraisalSent {
				return e, appraisalInvalid("action", "chỉ HCTH trả đề xuất đã gửi")
			}
			if locks.Manager {
				e.Reopened = true
			}
		} else {
			return e, appraisalInvalid("return_to", "chọn self hoặc manager")
		}
		e.Status, e.ReturnedTo, e.CorrectionReason = AppraisalChangeRequested, in.ReturnTo, strings.TrimSpace(in.Reason)
	case "official":
		if !a.center() || e.Status != AppraisalSent {
			return e, appraisalInvalid("action", "chỉ HCTH nhập kết luận cho đề xuất đã gửi")
		}
		if !slices.Contains(Ratings, in.OfficialRating) {
			return e, appraisalInvalid("official_rating", "chọn kết quả xếp loại chính thức")
		}
		if in.OfficialRating != e.CollectiveRating && utf8.RuneCountInString(strings.TrimSpace(in.Reason)) < 10 {
			return e, appraisalInvalid("reason", "kết luận khác tập thể đề xuất cần lý do ít nhất 10 ký tự")
		}
		e.OfficialRating = in.OfficialRating
		if strings.TrimSpace(in.Reason) != "" {
			e.Reason = strings.TrimSpace(in.Reason)
		}
	case "reopen":
		if !a.center() || e.Status != AppraisalClosed {
			return e, appraisalInvalid("action", "chỉ HCTH mở lại một người đã chốt")
		}
		if utf8.RuneCountInString(strings.TrimSpace(in.Reason)) < 10 {
			return e, appraisalInvalid("reason", "lý do mở lại cần ít nhất 10 ký tự")
		}
		e.Reopened, e.Status, e.ReturnedTo, e.CorrectionReason = true, AppraisalChangeRequested, "manager", strings.TrimSpace(in.Reason)
		e.OfficialRating = ""
	default:
		return e, appraisalInvalid("action", "không có thao tác đánh giá này")
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
