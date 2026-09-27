// Package domain: thực thể và quy tắc đồng ý theo mục đích (mục 10.5).
// Không phụ thuộc hạ tầng; thời gian nhận qua tham số (R-12).
package domain

import (
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// RuleVersion ghim vào mọi bản ghi đồng ý.
const RuleVersion = "pf.party.consent/v1"

// MaxClockSkew: event_time được phép lệch tới tương lai tối đa chừng này
// (đồng hồ thiết bị hiện trường không đáng tin làm nguồn thứ tự — mục 14).
const MaxClockSkew = 5 * time.Minute

// Status là trạng thái đồng ý.
type Status string

// Các trạng thái.
const (
	Granted   Status = "granted"
	Withdrawn Status = "withdrawn"
)

// Channel là kênh thu đồng ý.
type Channel string

// Các kênh.
const (
	FieldApp Channel = "field_app"
	Web      Channel = "web"
	Zalo     Channel = "zalo"
	Paper    Channel = "paper"
)

var purposeRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{2,63}$`)

// Lỗi kiểm dữ liệu — handler ánh xạ sang problem theo trường.
var (
	ErrPurpose     = errors.New("purpose phải là mã chữ thường, 3–64 ký tự, vd mrv.evidence")
	ErrTextVersion = errors.New("text_version bắt buộc, tối đa 40 ký tự")
	ErrStatus      = errors.New("status phải là granted hoặc withdrawn")
	ErrChannel     = errors.New("channel phải là field_app, web, zalo hoặc paper")
	ErrEventTime   = errors.New("event_time thiếu hoặc ở tương lai")
	ErrSubject     = errors.New("subject_ref bắt buộc")
	ErrID          = errors.New("id phải là UUIDv7")
)

// Consent là một bản ghi trong sổ đồng ý.
type Consent struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	SubjectRef  uuid.UUID
	Purpose     string
	TextVersion string
	Status      Status
	Channel     Channel
	EventTime   time.Time
	ActorRef    uuid.UUID
	DeviceID    *string
	IdemKey     string
	Seq         int64
	RecordedAt  time.Time
}

// FieldError gắn lỗi với tên trường.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }
func (e FieldError) Unwrap() error { return e.Err }

// Validate kiểm bản ghi mới so với thời điểm hệ thống now.
func (c Consent) Validate(now time.Time) error {
	switch {
	case c.ID == uuid.Nil || c.ID.Version() != 7:
		return FieldError{"id", ErrID}
	case c.SubjectRef == uuid.Nil:
		return FieldError{"subject_ref", ErrSubject}
	case !purposeRe.MatchString(c.Purpose):
		return FieldError{"purpose", ErrPurpose}
	case c.TextVersion == "" || len(c.TextVersion) > 40:
		return FieldError{"text_version", ErrTextVersion}
	case c.Status != Granted && c.Status != Withdrawn:
		return FieldError{"status", ErrStatus}
	case c.Channel != FieldApp && c.Channel != Web && c.Channel != Zalo && c.Channel != Paper:
		return FieldError{"channel", ErrChannel}
	case c.EventTime.IsZero() || c.EventTime.After(now.Add(MaxClockSkew)):
		return FieldError{"event_time", ErrEventTime}
	}
	return nil
}

// StreamKey: mỗi chủ thể một chuỗi băm.
func StreamKey(subject uuid.UUID) string { return "subject:" + subject.String() }

// Current là trạng thái hiện tại của (chủ thể, mục đích) — bảng chiếu.
type Current struct {
	Status      Status
	TextVersion string
	ConsentID   uuid.UUID
	Seq         int64
	EventTime   time.Time
}

// Supersedes cho biết bản ghi c có thay trạng thái hiện tại cur không.
// Đồng bộ ngoại tuyến có thể đến trễ: sự việc xảy ra SAU mới thắng (theo
// event_time, R-13); cùng event_time thì bản ghi vào sổ sau thắng.
func (c Consent) Supersedes(cur *Current) bool {
	if cur == nil {
		return true
	}
	if c.EventTime.Equal(cur.EventTime) {
		return c.Seq > cur.Seq
	}
	return c.EventTime.After(cur.EventTime)
}
