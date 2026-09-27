package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ParamRuleVersion ghim vào mọi dòng sổ param_approved.
const ParamRuleVersion = "pf.masterdata.param/v1"

// ParamFormat là định dạng tệp tham số nhúng.
const ParamFormat = "agrigreen.params/v1"

// ParamStatus là trạng thái một phiên bản tham số.
type ParamStatus string

// Vòng đời: draft → submitted → approved | rejected. Đã quyết định thì bất biến.
const (
	StatusDraft     ParamStatus = "draft"
	StatusSubmitted ParamStatus = "submitted"
	StatusApproved  ParamStatus = "approved"
	StatusRejected  ParamStatus = "rejected"
)

// Khóa: <vùng>.<tên>[.<tên>…][:<chiều>…], vd "cgh.norm_ha_per_machine:MGD:thu_hoach".
var paramKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+(:[A-Za-z0-9_.-]+)*$`)

// Giới hạn của cột numeric(24,8).
var maxParamValue = decimal.New(1, 16)

// Lỗi kiểm dữ liệu và quy trình tham số.
var (
	ErrParamKey        = errors.New("khóa dạng vùng.tên[:chiều…], chữ thường, tối đa 200 ký tự")
	ErrParamValue      = errors.New("cần đúng một trong value (số thập phân, tối đa 8 chữ số lẻ, |x| < 1e16) hoặc text (1–1000 ký tự)")
	ErrParamUnit       = errors.New("đơn vị tối đa 30 ký tự")
	ErrParamEffective  = errors.New("effective_from bắt buộc, theo RFC 3339 hoặc YYYY-MM-DD")
	ErrParamSourceDoc  = errors.New("số/ngày văn bản ban hành bắt buộc, tối đa 300 ký tự (CGH FN-02 B3)")
	ErrCorrection      = errors.New("correction_reason tối thiểu 10 ký tự")
	ErrNotDraft        = errors.New("chỉ gửi duyệt được phiên bản nháp")
	ErrNotSetter       = errors.New("chỉ người đặt được gửi duyệt phiên bản của mình")
	ErrNotSubmitted    = errors.New("chỉ phê duyệt/từ chối được phiên bản đã gửi duyệt")
	ErrDecisionNote    = errors.New("lý do từ chối bắt buộc, 10–500 ký tự")
	ErrCorrectionNeeds = errors.New("đã có phiên bản được duyệt cùng effective_from: phải nêu correction_reason (bản ghi bù)")
)

// ValidateParamKey kiểm khóa tham số.
func ValidateParamKey(k string) error {
	if len(k) > 200 || !paramKeyRe.MatchString(k) {
		return FieldError{"key", ErrParamKey}
	}
	return nil
}

// ParamVersion là một phiên bản tham số trong quy trình duyệt.
type ParamVersion struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Key              string
	Version          int
	Value            *decimal.Decimal
	Text             *string
	Unit             string
	EffectiveFrom    time.Time
	SourceDoc        string
	CorrectionReason *string
	Status           ParamStatus
	SetterRef        uuid.UUID
	SubmittedAt      *time.Time
	ApproverRef      *uuid.UUID
	DecidedAt        *time.Time
	DecisionNote     *string
	IdemKey          string
	CreatedAt        time.Time
}

// ValidateProposal kiểm nội dung đề xuất.
func (v ParamVersion) ValidateProposal() error {
	if err := ValidateParamKey(v.Key); err != nil {
		return err
	}
	switch {
	case (v.Value == nil) == (v.Text == nil):
		return FieldError{"value", ErrParamValue}
	case v.Value != nil && (v.Value.Exponent() < -8 || v.Value.Abs().GreaterThanOrEqual(maxParamValue)):
		return FieldError{"value", ErrParamValue}
	case v.Text != nil && (*v.Text == "" || utf8.RuneCountInString(*v.Text) > 1000):
		return FieldError{"text", ErrParamValue}
	case utf8.RuneCountInString(v.Unit) > 30:
		return FieldError{"unit", ErrParamUnit}
	case v.EffectiveFrom.IsZero():
		return FieldError{"effective_from", ErrParamEffective}
	case strings.TrimSpace(v.SourceDoc) == "" || utf8.RuneCountInString(v.SourceDoc) > 300:
		return FieldError{"source_doc", ErrParamSourceDoc}
	case v.CorrectionReason != nil && utf8.RuneCountInString(strings.TrimSpace(*v.CorrectionReason)) < 10:
		return FieldError{"correction_reason", ErrCorrection}
	}
	return nil
}

// CanSubmit: chỉ phiên bản nháp, chỉ bởi chính người đặt.
func (v ParamVersion) CanSubmit(actor uuid.UUID) error {
	if v.Status != StatusDraft {
		return ErrNotDraft
	}
	if actor != v.SetterRef {
		return ErrNotSetter
	}
	return nil
}

// CanDecide: chỉ phiên bản đã gửi duyệt. Phân tách nhiệm vụ (R-56) kiểm ở app/
// bằng authz.EnsureDistinctApprover.
func (v ParamVersion) CanDecide() error {
	if v.Status != StatusSubmitted {
		return ErrNotSubmitted
	}
	return nil
}

// ValidateDecisionNote kiểm lý do từ chối.
func ValidateDecisionNote(n string) error {
	c := utf8.RuneCountInString(strings.TrimSpace(n))
	if c < 10 || c > 500 {
		return FieldError{"note", ErrDecisionNote}
	}
	return nil
}

// Approved là một dòng sổ phiên bản đã phê duyệt — nguồn của Params.At.
type Approved struct {
	ID               uuid.UUID
	Key              string
	Version          int
	ParamVersionID   uuid.UUID
	Value            *decimal.Decimal
	Text             *string
	Unit             string
	EffectiveFrom    time.Time
	SourceDoc        string
	SetterRef        uuid.UUID
	ApproverRef      uuid.UUID
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	IdemKey          string
	Seq              int64
	RecordedAt       time.Time
}

// ParamStreamKey: mỗi khóa tham số một chuỗi băm.
func ParamStreamKey(key string) string { return "param:" + key }

// ParseInstant đọc thời điểm RFC 3339, hoặc ngày YYYY-MM-DD = 00:00 giờ Việt Nam.
func ParseInstant(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	d, err := ParseDay(s)
	if err != nil {
		return time.Time{}, err
	}
	return DayStart(d), nil
}
