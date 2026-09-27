package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Staff là một cán bộ trong danh bạ trực (FN-15).
//
// DisplayName là tên cán bộ khuyến nông: dữ liệu danh bạ công vụ, không phải dữ
// liệu cá nhân của nông hộ, nhưng vẫn là thông tin cá nhân — gắn pii:"true" để
// archlint chặn đưa vào log/lỗi (R-14). Tài khoản cán bộ ở Keycloak không có chủ
// thể trong kho định danh nên không tra được qua vault Display (ghi chú ADR-0009).
type Staff struct {
	ActorRef     uuid.UUID
	UnitCode     string
	ProvinceCode string
	RoleTitle    string
	DisplayName  string `pii:"true"`
	Active       bool
	UpdatedAt    time.Time
}

// ValidateStaff kiểm bản ghi danh bạ.
func ValidateStaff(s *Staff) error {
	s.UnitCode, s.RoleTitle, s.DisplayName = strings.TrimSpace(s.UnitCode), strings.TrimSpace(s.RoleTitle), strings.TrimSpace(s.DisplayName)
	switch {
	case s.ActorRef == uuid.Nil:
		return fieldErr("actor_ref", "bắt buộc")
	case s.UnitCode == "":
		return fieldErr("unit_code", "bắt buộc")
	case len([]rune(s.RoleTitle)) < 2 || len([]rune(s.RoleTitle)) > 100:
		return fieldErr("role_title", "2–100 ký tự")
	case len([]rune(s.DisplayName)) < 2 || len([]rune(s.DisplayName)) > 120:
		return fieldErr("display_name", "2–120 ký tự")
	case ContainsPII(s.DisplayName):
		return FieldError{Field: "display_name", Err: ErrPIIInText}
	}
	return nil
}

// Trạng thái trực.
const (
	DutyOn      = "on_duty"
	DutyOff     = "off_duty"
	DutyUnknown = "unknown"
)

// Nguồn của trạng thái trực.
const (
	DutySourceManual = "manual"
	DutySourceShift  = "shift"
	DutySourceNone   = "none"
)

// ManualStatus là trạng thái tự khai mới nhất.
type ManualStatus struct {
	Status     string
	SetAt      time.Time
	ValidUntil time.Time
}

// Shift là một ca trực đã khai báo.
type Shift struct {
	ID       uuid.UUID
	ActorRef uuid.UUID
	StartsAt time.Time
	EndsAt   time.Time
}

// DutyAt tính trạng thái trực tại thời điểm now:
//  1. trạng thái tự khai còn hạn thắng (US-DIR-01);
//  2. nếu không, đang trong ca đã khai báo → đang trực (US-DIR-03);
//  3. còn lại → không xác định (hết hạn tự khai, US-DIR-01 AC).
func DutyAt(now time.Time, manual *ManualStatus, shifts []Shift) (status, source string, until *time.Time) {
	if manual != nil && now.Before(manual.ValidUntil) && !now.Before(manual.SetAt) {
		u := manual.ValidUntil
		return manual.Status, DutySourceManual, &u
	}
	for _, s := range shifts {
		if !now.Before(s.StartsAt) && now.Before(s.EndsAt) {
			u := s.EndsAt
			return DutyOn, DutySourceShift, &u
		}
	}
	return DutyUnknown, DutySourceNone, nil
}
