package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Tần suất lịch gửi (US-DASH-03).
const (
	FreqWeekly  = "weekly"
	FreqMonthly = "monthly"
)

// Schedule là lịch gửi báo cáo định kỳ. Recipients là actor_ref (không lưu email).
type Schedule struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	TemplateKey       string
	Format            string
	UnitCode          string
	Params            Params
	Frequency         string
	Weekday           int // 1 = thứ Hai … 7 = Chủ nhật (ISO)
	DayOfMonth        int // 1..28 (tránh tháng thiếu ngày)
	Hour              int // giờ Việt Nam
	Recipients        []uuid.UUID
	OwnerRef          uuid.UUID
	OwnerRoles        []string
	ScopeAll          bool
	ScopeUnits        []string
	Active            bool
	DeactivatedReason *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	IdemKey           string
}

// Validate kiểm cấu hình lịch.
func (s *Schedule) Validate() error {
	switch s.Frequency {
	case FreqWeekly:
		if s.Weekday < 1 || s.Weekday > 7 {
			return FieldError{"weekday", "1 (thứ Hai) … 7 (Chủ nhật)"}
		}
		s.DayOfMonth = 0
	case FreqMonthly:
		if s.DayOfMonth < 1 || s.DayOfMonth > 28 {
			return FieldError{"day_of_month", "1 … 28"}
		}
		s.Weekday = 0
	default:
		return FieldError{"frequency", "weekly hoặc monthly"}
	}
	if s.Hour < 0 || s.Hour > 23 {
		return FieldError{"hour", "0 … 23 (giờ Việt Nam)"}
	}
	if len(s.Recipients) == 0 || len(s.Recipients) > 50 {
		return FieldError{"recipients", "1 … 50 tài khoản (actor_ref)"}
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range s.Recipients {
		if r == uuid.Nil || seen[r] {
			return FieldError{"recipients", "actor_ref không hợp lệ hoặc trùng"}
		}
		seen[r] = true
	}
	if s.Format != FormatXLSX && s.Format != FormatPDF {
		return FieldError{"format", "xlsx hoặc pdf"}
	}
	return nil
}

// Slot trả mốc lịch gần nhất ≤ now và khóa mốc (tuần ISO "2026-W40" hoặc tháng "2026-10").
// Mỗi mốc chạy đúng một lần (khóa chính schedule_run).
func (s Schedule) Slot(now time.Time) (time.Time, string) {
	z := clock.Zone()
	t := now.In(z)
	switch s.Frequency {
	case FreqWeekly:
		iso := int(t.Weekday())
		if iso == 0 {
			iso = 7
		}
		c := time.Date(t.Year(), t.Month(), t.Day()-(iso-s.Weekday), s.Hour, 0, 0, 0, z)
		if c.After(t) {
			c = c.AddDate(0, 0, -7)
		}
		y, w := c.ISOWeek()
		return c.UTC(), fmt.Sprintf("%d-W%02d", y, w)
	default:
		c := time.Date(t.Year(), t.Month(), s.DayOfMonth, s.Hour, 0, 0, 0, z)
		if c.After(t) {
			c = c.AddDate(0, -1, 0)
		}
		return c.UTC(), c.Format("2006-01")
	}
}

// ReportPeriod là kỳ báo cáo của một mốc: lịch tháng gửi số liệu tháng trước
// (đã đủ kỳ); lịch tuần gửi số liệu lũy kế tháng đang chạy.
func (s Schedule) ReportPeriod(slotAt time.Time) string {
	if s.Frequency == FreqMonthly {
		return PreviousMonth(slotAt)
	}
	return MonthOf(slotAt)
}
