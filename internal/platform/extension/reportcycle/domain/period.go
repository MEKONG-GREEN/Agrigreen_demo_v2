// Package domain: quy tắc thuần của vòng đời báo cáo KN v7.7 (gói P4) — chia kỳ, hạn nộp, trạng thái công
// việc, so khác lần tổng hợp, chỉ số dashboard. Không DB, không time.Now() (R-12), không float (R-10).
package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Loại kỳ (FN-TI-02.02 B3).
const (
	PeriodWeek   = "week"
	PeriodMonth  = "month"
	PeriodSeason = "season"
	PeriodYear   = "year"
	PeriodAdhoc  = "adhoc"
)

// Loại báo cáo (khớp reports/port.Report*) và loại kỳ mặc định của nó.
var ReportPeriodType = map[string]string{
	"weekly": PeriodWeek, "monthly": PeriodMonth, "seasonal": PeriodSeason, "annual": PeriodYear, "adhoc": PeriodAdhoc,
}

// Cách đánh số tuần (Q-17, TẠM — chờ khách): "season" = Tuần n của đợt (cột Tuần 1, Tuần 2 của PL 01),
// "iso" = tuần ISO 8601 (vd "Tuần 36"). Tuần bắt đầu thứ Hai trong cả hai cách.
const (
	WeekNumberingSeason = "season"
	WeekNumberingISO    = "iso"
)

// PeriodRule là quy tắc chia kỳ có phiên bản (ghi vào đợt phát hành để kỳ đã chia không đổi khi quy tắc đổi).
type PeriodRule struct {
	Version       string
	WeekNumbering string
}

// RuleVersion là phiên bản quy tắc chia kỳ hiện hành (tạm, chờ khách Q-17).
const RuleVersion = "kn.period_rule/v1-tam"

// DefaultRule: tuần thứ n của đợt, bắt đầu thứ Hai; tháng dương lịch; vụ/năm/đột xuất = một kỳ.
var DefaultRule = PeriodRule{Version: RuleVersion, WeekNumbering: WeekNumberingSeason}

// Tag là chuỗi ghi vào cột period_rule của đợt.
func (r PeriodRule) Tag() string { return r.Version + ";week=" + r.WeekNumbering }

// Deadline là quy tắc hạn nộp mỗi kỳ: ngày cuối kỳ + OffsetDays, lúc Time (giờ Việt Nam).
type Deadline struct {
	OffsetDays int
	Time       string // "HH:MM"
}

// DefaultDeadline trả hạn mặc định theo loại kỳ (TẠM, chờ khách A4/Q-17): tuần → "Thứ Ba tuần sau, 17:00"
// (+2 ngày từ Chủ nhật); tháng → ngày 10 tháng sau 17:00 (UR-XA-09 "trước ngày 10"); vụ/năm/đột xuất → +7 ngày.
func DefaultDeadline(periodType string) Deadline {
	switch periodType {
	case PeriodWeek:
		return Deadline{OffsetDays: 2, Time: "17:00"}
	case PeriodMonth:
		return Deadline{OffsetDays: 10, Time: "17:00"}
	}
	return Deadline{OffsetDays: 7, Time: "17:00"}
}

// DefaultCloseAfterDays: số ngày chốt kỳ sau hạn (tạm).
const DefaultCloseAfterDays = 3

// MaxPeriods giới hạn số kỳ một đợt (vụ dài nhất ~ 26 tuần; năm 12 tháng).
const MaxPeriods = 60

// Period là một kỳ đã chia.
type Period struct {
	Slot        int
	Label       string
	RangeLabel  string
	ISOWeek     int // 0 nếu không phải kỳ tuần
	From, To    time.Time
	DueAt       time.Time // UTC
	AutoCloseAt time.Time // UTC
}

// Date chuẩn hóa một ngày dương lịch về 00:00 UTC.
func Date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// ParseDate đọc "YYYY-MM-DD".
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, errors.New("ngày phải dạng YYYY-MM-DD")
	}
	return t, nil
}

// ParseClock đọc "HH:MM".
func ParseClock(s string) (int, int, error) {
	h, m, ok := strings.Cut(s, ":")
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	if !ok || e1 != nil || e2 != nil || len(h) != 2 || len(m) != 2 || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, errors.New("giờ phải dạng HH:MM")
	}
	return hh, mm, nil
}

// DueAt tính hạn nộp UTC của kỳ kết thúc ngày to theo quy tắc d (giờ Việt Nam loc).
func (d Deadline) DueAt(to time.Time, loc *time.Location) (time.Time, error) {
	hh, mm, err := ParseClock(d.Time)
	if err != nil {
		return time.Time{}, err
	}
	day := to.AddDate(0, 0, d.OffsetDays)
	return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc).UTC(), nil
}

// ErrPeriod là lỗi tham số chia kỳ (field, thông báo).
type ErrPeriod struct{ Field, Msg string }

func (e ErrPeriod) Error() string { return e.Field + ": " + e.Msg }

// Split tự chia [from, to] thành các kỳ theo loại kỳ (FN-TI-02.02 "hệ thống tự chia các kỳ").
//   - week: tuần thứ Hai–Chủ nhật giao với [from, to] (kỳ đầu/cuối có thể ngắn hơn 7 ngày);
//   - month: tháng dương lịch giao với [from, to];
//   - season/year/adhoc: một kỳ [from, to], nhãn = tên vụ/đợt.
//
// Hạn nộp = ngày cuối kỳ + OffsetDays lúc Time; giờ chốt tự động = hạn + closeAfterDays ngày.
func Split(periodType string, from, to time.Time, seasonLabel string, rule PeriodRule, d Deadline, closeAfterDays int, loc *time.Location) ([]Period, error) {
	if to.Before(from) {
		return nil, ErrPeriod{"to_date", "phải không trước from_date"}
	}
	if closeAfterDays < 0 || closeAfterDays > 60 {
		return nil, ErrPeriod{"close_after_days", "0–60 ngày"}
	}
	if d.OffsetDays < 0 || d.OffsetDays > 60 {
		return nil, ErrPeriod{"deadline.offset_days", "0–60 ngày"}
	}
	if _, _, err := ParseClock(d.Time); err != nil {
		return nil, ErrPeriod{"deadline.time", err.Error()}
	}
	var out []Period
	add := func(f, t time.Time, label string, iso int) error {
		if len(out) >= MaxPeriods {
			return ErrPeriod{"to_date", fmt.Sprintf("tối đa %d kỳ mỗi đợt", MaxPeriods)}
		}
		due, err := d.DueAt(t, loc)
		if err != nil {
			return ErrPeriod{"deadline.time", err.Error()}
		}
		out = append(out, Period{Slot: len(out) + 1, Label: label, RangeLabel: rangeLabel(f, t), ISOWeek: iso, From: f, To: t,
			DueAt: due, AutoCloseAt: due.AddDate(0, 0, closeAfterDays)})
		return nil
	}
	switch periodType {
	case PeriodWeek:
		// thứ Hai của tuần chứa from
		start := from.AddDate(0, 0, -((int(from.Weekday()) + 6) % 7))
		for ws := start; !ws.After(to); ws = ws.AddDate(0, 0, 7) {
			f, t := maxT(ws, from), minT(ws.AddDate(0, 0, 6), to)
			_, iso := f.ISOWeek()
			n := len(out) + 1
			label := "Tuần " + strconv.Itoa(n)
			if rule.WeekNumbering == WeekNumberingISO {
				label = "Tuần " + strconv.Itoa(iso)
			}
			if err := add(f, t, label, iso); err != nil {
				return nil, err
			}
		}
	case PeriodMonth:
		for ms := Date(from.Year(), from.Month(), 1); !ms.After(to); ms = ms.AddDate(0, 1, 0) {
			f, t := maxT(ms, from), minT(ms.AddDate(0, 1, -1), to)
			if err := add(f, t, fmt.Sprintf("Tháng %d/%d", ms.Month(), ms.Year()), 0); err != nil {
				return nil, err
			}
		}
	case PeriodSeason, PeriodAdhoc:
		if err := add(from, to, seasonLabel, 0); err != nil {
			return nil, err
		}
	case PeriodYear:
		label := seasonLabel
		if label == "" {
			label = "Năm " + strconv.Itoa(from.Year())
		}
		if err := add(from, to, label, 0); err != nil {
			return nil, err
		}
	default:
		return nil, ErrPeriod{"period_type", "week | month | season | year | adhoc"}
	}
	return out, nil
}

func rangeLabel(f, t time.Time) string {
	if f.Year() == t.Year() {
		return f.Format("02/01") + "–" + t.Format("02/01/2006")
	}
	return f.Format("02/01/2006") + "–" + t.Format("02/01/2006")
}

func maxT(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
