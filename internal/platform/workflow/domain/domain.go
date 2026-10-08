// Package domain là quy tắc thuần của pf/workflow (không I/O): dạng kind, ghi chú, quy tắc bốn mắt (R-56,
// kiến trúc v4.1 §26). Cùng quy tắc được DB kiểm lại (pf 000118: CHECK decision_four_eyes, trigger
// trg_bind_request).
package domain

import (
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ (rule_version).
const RuleVersion = "workflow-2026.10"

// Lỗi quy tắc.
var (
	ErrKind          = errors.New("kind phải dạng <module>.<đối tượng> (chữ thường, số, _)")
	ErrSubject       = errors.New("subject bắt buộc")
	ErrPerson        = errors.New("mã người lập/người quyết định bắt buộc")
	ErrOutcome       = errors.New("outcome phải là approved, rejected hoặc cancelled")
	ErrNoteRequired  = errors.New("ghi chú bắt buộc khi từ chối hoặc hủy")
	ErrNoteTooLong   = errors.New("ghi chú tối đa 1000 ký tự")
	ErrSummary       = errors.New("tóm tắt tối đa 200 ký tự")
	ErrSelfDecision  = errors.New("người duyệt trùng người lập")
	ErrBeneficiary   = errors.New("người duyệt trùng người được hưởng")
	errEmptyDecision = errors.New("thiếu người quyết định")
)

var kindRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}\.[a-z][a-z0-9_]{0,59}$`)

// ValidateKind kiểm dạng kind (khớp CHECK của pf_workflow.request.kind).
func ValidateKind(kind string) error {
	if !kindRe.MatchString(kind) {
		return ErrKind
	}
	return nil
}

// ValidateSubmit kiểm dữ liệu lập yêu cầu.
func ValidateSubmit(in port.SubmitInput) error {
	if err := ValidateKind(in.Kind); err != nil {
		return err
	}
	if in.Subject == uuid.Nil {
		return ErrSubject
	}
	if in.Requester == uuid.Nil {
		return ErrPerson
	}
	if n := len([]rune(strings.TrimSpace(in.Summary))); n > 200 {
		return ErrSummary
	}
	return nil
}

// FourEyes là quy tắc bốn mắt: người duyệt (decider) khác người lập và khác người được hưởng. Chỉ áp cho
// approved/rejected; cancelled (rút, thu hồi trước khi duyệt) ai có quyền trên đối tượng cũng làm được.
func FourEyes(requester uuid.UUID, beneficiary *uuid.UUID, decider uuid.UUID, outcome port.Outcome) error {
	if decider == uuid.Nil {
		return errEmptyDecision
	}
	if outcome == port.OutcomeCancelled {
		return nil
	}
	if decider == requester {
		return ErrSelfDecision
	}
	if beneficiary != nil && decider == *beneficiary {
		return ErrBeneficiary
	}
	return nil
}

// ValidateDecide kiểm dữ liệu quyết định, gồm quy tắc bốn mắt với người lập/người hưởng bên gọi khai.
func ValidateDecide(in port.DecideInput) error {
	if err := ValidateKind(in.Kind); err != nil {
		return err
	}
	if in.Subject == uuid.Nil {
		return ErrSubject
	}
	if in.Requester == uuid.Nil || in.Decider == uuid.Nil {
		return ErrPerson
	}
	switch in.Outcome {
	case port.OutcomeApproved, port.OutcomeRejected, port.OutcomeCancelled:
	default:
		return ErrOutcome
	}
	note := strings.TrimSpace(in.Note)
	if in.Outcome != port.OutcomeApproved && note == "" {
		return ErrNoteRequired
	}
	if len([]rune(note)) > 1000 {
		return ErrNoteTooLong
	}
	return FourEyes(in.Requester, in.Beneficiary, in.Decider, in.Outcome)
}

// IsFourEyes cho biết err là vi phạm bốn mắt.
func IsFourEyes(err error) bool {
	return errors.Is(err, ErrSelfDecision) || errors.Is(err, ErrBeneficiary)
}
