package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func w1174Schedule() Schedule {
	return Schedule{Weekdays: MonFri, LockAt: MustTimeOfDay("12:00"), RespondBy: MustTimeOfDay("13:30"),
		Remind1: MustTimeOfDay("07:30"), Remind2: MustTimeOfDay("11:00"), InApp: true, Email: true, Template: DefaultReminderTemplate}
}

func w1174Day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

// FN-AD-04.04 AC-01, AC-02: lịch T2–T6, khóa 12:00 giờ VN (= 05:00 UTC); Thứ Bảy, Chủ nhật không có kỳ, không nhắc.
func TestPlanMonFriLock_W1174(t *testing.T) {
	s := w1174Schedule()
	mon := Plan(w1174Day("2026-10-12"), s, nil) // Thứ Hai
	if !mon.Quote || mon.Kind != DayRegular || mon.LockAt.Format(time.RFC3339) != "2026-10-12T05:00:00Z" ||
		mon.RespondBy.Format(time.RFC3339) != "2026-10-12T06:30:00Z" {
		t.Fatalf("Thứ Hai: %+v", mon)
	}
	at := func(s string) time.Time { x, _ := time.Parse(time.RFC3339, s); return x }
	if mon.Locked(at("2026-10-12T04:59:59Z")) || !mon.Locked(at("2026-10-12T05:00:00Z")) {
		t.Fatal("khóa đúng 12:00:00 giờ VN")
	}
	for _, d := range []string{"2026-10-17", "2026-10-18"} {
		p := Plan(w1174Day(d), s, nil)
		if p.Quote || p.Kind != DayOff || len(p.DueRuns(at(d+"T10:00:00Z"))) != 0 || p.Locked(at(d+"T10:00:00Z")) {
			t.Fatalf("%s phải không có kỳ: %+v", d, p)
		}
	}
	// Nhắc: 07:30 VN = 00:30 UTC; 11:00 VN = 04:00 UTC; sau khóa chỉ còn lock.
	cases := map[string][]string{
		"2026-10-12T00:29:00Z": nil,
		"2026-10-12T00:30:00Z": {RunRemind1},
		"2026-10-12T04:00:00Z": {RunRemind1, RunRemind2},
		"2026-10-12T05:00:30Z": {RunLock},
	}
	for ts, want := range cases {
		got := mon.DueRuns(at(ts))
		if len(got) != len(want) {
			t.Fatalf("%s: %v, muốn %v", ts, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: %v, muốn %v", ts, got, want)
			}
		}
	}
}

// AC-04: ngày nghỉ 02/09/2026 (Thứ Tư) không có kỳ; ngày bổ sung Thứ Bảy có kỳ; ngoại lệ không khớp lịch bị chặn.
func TestExceptions_W1174(t *testing.T) {
	s := w1174Schedule()
	h := Exception{Day: w1174Day("2026-09-02"), Kind: ExceptionHoliday, Reason: "Quốc khánh"}
	if err := CheckException(h, s); err != nil {
		t.Fatal(err)
	}
	if p := Plan(h.Day, s, &h); p.Quote || p.Kind != DayHoliday || p.Reason != "Quốc khánh" {
		t.Fatalf("ngày nghỉ: %+v", p)
	}
	x := Exception{Day: w1174Day("2026-09-05"), Kind: ExceptionExtra, Reason: "Bù ngày nghỉ"}
	if p := Plan(x.Day, s, &x); !p.Quote || p.Kind != DayExtra {
		t.Fatalf("ngày bổ sung: %+v", p)
	}
	if err := CheckException(Exception{Day: w1174Day("2026-09-05"), Kind: ExceptionHoliday, Reason: "x"}, s); !errors.Is(err, ErrExceptionMismatch) {
		t.Fatalf("nghỉ Thứ Bảy: %v", err)
	}
	if err := CheckException(Exception{Day: w1174Day("2026-09-02"), Kind: ExceptionExtra, Reason: "x"}, s); !errors.Is(err, ErrExceptionMismatch) {
		t.Fatalf("bổ sung Thứ Tư: %v", err)
	}
	if err := ValidateException(Exception{Day: w1174Day("2026-09-02"), Kind: "nghi", Reason: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("loại sai: %v", err)
	}
}

// AC-05 (FN-AD-04.04): hạn phản hồi 11:30 khi khóa 12:00 → chặn; FN-AD-04.05 AC-01: nhắc 2 không trước giờ khóa → chặn;
// biến mẫu lạ → chặn và chỉ ra biến.
func TestValidateSchedule_W1174(t *testing.T) {
	s := w1174Schedule()
	if err := ValidateSchedule(s); err != nil {
		t.Fatal(err)
	}
	bad := s
	bad.RespondBy = MustTimeOfDay("11:30")
	if err := ValidateSchedule(bad); !errors.Is(err, ErrRespondBeforeLock) {
		t.Fatalf("AC-05: %v", err)
	}
	bad = s
	bad.Remind2 = MustTimeOfDay("12:00")
	if err := ValidateSchedule(bad); !errors.Is(err, ErrRemindOrder) {
		t.Fatalf("nhắc 2 = giờ khóa: %v", err)
	}
	bad = s
	bad.Template = "Nhắc {ngày} {tên cán bộ}"
	if err := ValidateSchedule(bad); !errors.Is(err, ErrTemplateVar) || UnknownVars(bad.Template)[0] != "{tên cán bộ}" {
		t.Fatalf("biến lạ: %v", err)
	}
	bad = s
	bad.Weekdays = 0
	if err := ValidateSchedule(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("không ngày: %v", err)
	}
	bad = s
	bad.InApp, bad.Email = false, false
	if err := ValidateSchedule(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("không kênh: %v", err)
	}
	if _, err := ParseTimeOfDay("25:00"); err == nil {
		t.Fatal("25:00 phải lỗi")
	}
	if got := RenderReminder(s.Template, w1174Day("2026-10-12"), "Xã An Bình", s.LockAt); got != "Nhắc nhập giá ngày 12/10/2026 cho Xã An Bình: vui lòng gửi báo giá trước 12:00." {
		t.Fatalf("render: %q", got)
	}
}

// AC-03: đổi giờ khóa áp dụng từ ngày áp dụng, không ảnh hưởng ngày đã khóa; đổi trong ngày báo giá → từ ngày mai.
func TestScheduleVersions_W1174(t *testing.T) {
	def := w1174Schedule()
	v1 := def
	v1.LockAt, v1.ValidFrom, v1.Version = MustTimeOfDay("11:00"), w1174Day("2026-10-14"), 1
	v1.Remind2 = MustTimeOfDay("10:30")
	vs := []Schedule{v1}
	if got := ScheduleAt(vs, w1174Day("2026-10-13"), def); got.LockAt.String() != "12:00" {
		t.Fatalf("trước ngày áp dụng: %s", got.LockAt)
	}
	if got := ScheduleAt(vs, w1174Day("2026-10-14"), def); got.LockAt.String() != "11:00" || got.Version != 1 {
		t.Fatalf("từ ngày áp dụng: %+v", got)
	}
	v2 := v1
	v2.LockAt, v2.Version = MustTimeOfDay("11:30"), 2
	if got := ScheduleAt([]Schedule{v2, v1}, w1174Day("2026-10-20"), def); got.Version != 2 {
		t.Fatalf("cùng ngày áp dụng lấy phiên bản sau: %+v", got)
	}
	if e := EarliestValidFrom(Plan(w1174Day("2026-10-12"), def, nil)); !e.Equal(w1174Day("2026-10-13")) {
		t.Fatalf("ngày báo giá → mai: %s", e)
	}
	if e := EarliestValidFrom(Plan(w1174Day("2026-10-17"), def, nil)); !e.Equal(w1174Day("2026-10-17")) {
		t.Fatalf("Thứ Bảy → hôm nay: %s", e)
	}
}

// FN-AD-04.06 AC-01, AC-02: mặc định 30 %, nhóm Rau màu 40 %; ưu tiên mặt hàng → nhóm → mặc định; +30,0 % là Cao.
func TestThresholds_W1174(t *testing.T) {
	d := func(s string) decimal.Decimal { return decimal.RequireFromString(s) }
	set := ThresholdSet{
		Default: Threshold{Scope: ThresholdScopeDefault, Pct: d("30"), Params: &AlertParams{LookbackDays: 7, FlatPct: d("1"), SurveyPct: d("40")}},
		Groups:  map[string]Threshold{"rau_mau": {Scope: ThresholdScopeGroup, Code: "rau_mau", Pct: d("40")}},
		Items:   map[string]Threshold{"heo_hoi": {Scope: ThresholdScopeItem, Code: "heo_hoi", Pct: d("25")}},
	}
	for _, err := range []error{ValidateThreshold(set.Default), ValidateThreshold(set.Groups["rau_mau"]), ValidateThreshold(set.Items["heo_hoi"])} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := Classify(d("35"), set.For("rau_mau", "cai_xanh").Pct); got != AlertNone {
		t.Fatalf("Rau màu +35 %% không đánh dấu: %q", got)
	}
	if got := Classify(d("-34.5"), set.For("chan_nuoi", "ga_ta").Pct); got != AlertLow {
		t.Fatalf("Chăn nuôi −34,5 %% phải Thấp: %q", got)
	}
	if got := set.For("chan_nuoi", "heo_hoi"); got.Scope != ThresholdScopeItem || !got.Pct.Equal(d("25")) {
		t.Fatalf("ưu tiên mặt hàng: %+v", got)
	}
	if Classify(d("30.0"), d("30")) != AlertHigh || Classify(d("29.9"), d("30")) != AlertNone {
		t.Fatal("AC-02: +30,0 % Cao, +29,9 % không")
	}
	delta, ok := ChangePct(d("13100"), d("20000"))
	if !ok || delta.String() != "-34.5" {
		t.Fatalf("Δ%%: %s %v", delta, ok)
	}
	if _, ok := ChangePct(d("100"), d("0")); ok {
		t.Fatal("không có giá kỳ trước → Giá mới")
	}
	if !Flat(d("0.99"), d("1")) || Flat(d("-1"), d("1")) {
		t.Fatal("đi ngang |Δ| < 1 %")
	}
	for _, bad := range []Threshold{
		{Scope: ThresholdScopeGroup, Code: "rau_mau", Pct: d("0")},
		{Scope: ThresholdScopeGroup, Code: "rau_mau", Pct: d("100.01")},
		{Scope: ThresholdScopeGroup, Code: "rau_mau", Pct: d("30.123")},
		{Scope: ThresholdScopeDefault, Pct: d("30")},
		{Scope: ThresholdScopeItem, Code: "Heo Hơi", Pct: d("30")},
		{Scope: "vung", Code: "x", Pct: d("30")},
	} {
		if err := ValidateThreshold(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v phải lỗi: %v", bad, err)
		}
	}
}
