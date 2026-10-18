package domain

import (
	"errors"
	"testing"
	"time"
)

// D8 (CEO 13/10/2026) — lịch tuần theo phòng ban.

func TestCalendar_TagCarriesIdentity(t *testing.T) {
	want := "kn.period_rule/v2;week_start=tue;week=year;month=week_end;cal=chan_nuoi_thuy_san:v3"
	if CNTSRule.Tag() != want {
		t.Fatalf("CNTSRule.Tag() = %q", CNTSRule.Tag())
	}
	r := mustRule(t, want)
	if r != CNTSRule {
		t.Fatalf("ParseRule(Tag()) = %+v", r)
	}
	for _, bad := range []string{
		"kn.period_rule/v2;cal=CNTS:v1", "kn.period_rule/v2;cal=chan_nuoi_thuy_san:v0", "kn.period_rule/v2;cal=chan_nuoi_thuy_san",
		"kn.period_rule/v1-tam;week=season;cal=trong_trot:v1", // v1 không mang cal
	} {
		if _, err := ParseRule(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
	if !SameGrid(CNTSRule, mustRule(t, "kn.period_rule/v2;week_start=tue")) || SameGrid(CNTSRule, DefaultRule) {
		t.Fatal("SameGrid bỏ qua danh tính lịch, so phần chia kỳ")
	}
	if !SameWeeks(LegacyRule, mustRule(t, "kn.period_rule/v2;week_start=mon;week=iso;month=calendar")) || SameWeeks(CNTSRule, DefaultRule) {
		t.Fatal("SameWeeks so ngày đầu tuần")
	}
}

func TestResolveCalendar(t *testing.T) {
	v2 := DefaultRule.Tag()
	cases := []struct {
		name, dept, deptParam, global string
		ref, tag, src                 string
	}{
		{"CNTS nạp sẵn, trước 04/11 (chưa có tham số)", DeptChanNuoiThuySan, "", "", "chan_nuoi_thuy_san:v3", CNTSRule.Tag(), CalendarSourceBuiltin},
		{"CNTS nạp sẵn thắng tham số chung", DeptChanNuoiThuySan, "", v2, "chan_nuoi_thuy_san:v3", CNTSRule.Tag(), CalendarSourceBuiltin},
		{"KHTC nạp sẵn trước 04/11 → v2 Thứ Tư (W11.64)", DeptKeHoachTaiChinh, "", "", "ke_hoach_tai_chinh:v2", v2 + ";cal=ke_hoach_tai_chinh:v2", CalendarSourceBuiltin},
		{"KHTC nạp sẵn thắng tham số chung v1", DeptKeHoachTaiChinh, "", LegacyRule.Tag(), "ke_hoach_tai_chinh:v2", v2 + ";cal=ke_hoach_tai_chinh:v2", CalendarSourceBuiltin},
		{"Trồng trọt theo tham số chung", DeptTrongTrot, "", v2, "trong_trot:v2", v2 + ";cal=trong_trot:v2", CalendarSourceGlobalParam},
		{"tham số chung mang cal → bỏ cal", DeptTrongTrot, "", v2 + ";cal=chan_nuoi_thuy_san:v9", "trong_trot:v2", v2 + ";cal=trong_trot:v2", CalendarSourceGlobalParam},
		{"tham số theo phòng có phiên bản", DeptChanNuoiThuySan, "kn.period_rule/v2;week_start=mon;cal=chan_nuoi_thuy_san:v4", v2,
			"chan_nuoi_thuy_san:v4", "kn.period_rule/v2;week_start=mon;week=year;month=week_end;cal=chan_nuoi_thuy_san:v4", CalendarSourceDepartmentParam},
		{"tham số theo phòng không cal → v2", DeptTrongTrot, "kn.period_rule/v2;week_start=thu", "", "trong_trot:v2",
			"kn.period_rule/v2;week_start=thu;week=year;month=week_end;cal=trong_trot:v2", CalendarSourceDepartmentParam},
		{"tham số theo phòng mang cal phòng khác → bỏ qua", DeptChanNuoiThuySan, "kn.period_rule/v2;cal=trong_trot:v5", v2, "chan_nuoi_thuy_san:v3",
			CNTSRule.Tag(), CalendarSourceBuiltin},
		{"tham số theo phòng sai dạng → bỏ qua", DeptTrongTrot, "rác", v2, "trong_trot:v2", v2 + ";cal=trong_trot:v2", CalendarSourceGlobalParam},
	}
	for _, c := range cases {
		cal := ResolveCalendar(c.dept, c.deptParam, c.global)
		if cal.Ref() != c.ref || cal.Rule.Tag() != c.tag || cal.Source != c.src {
			t.Errorf("%s: %s %q %s", c.name, cal.Ref(), cal.Rule.Tag(), cal.Source)
		}
		back, err := CalendarOf(c.dept, cal.Rule.Tag())
		if err != nil || back.Ref() != cal.Ref() || !SameGrid(back.Rule, cal.Rule) {
			t.Errorf("%s: CalendarOf(tag đã ghi) = %s %v", c.name, back.Ref(), err)
		}
	}
}

func TestCalendarOf_StoredTags(t *testing.T) {
	cases := []struct{ dept, tag, ref string }{
		{"trong_trot", "kn.period_rule/v1-tam;week=season", "trong_trot:v1"},
		{"trong_trot", "kn.period_rule/v2;week_start=wed;week=year;month=week_end", "trong_trot:v2"},
		{"chan_nuoi_thuy_san", "kn.period_rule/v2;week_start=wed;week=year;month=week_end", "chan_nuoi_thuy_san:v2"}, // kỳ thử W11.42
		{"chan_nuoi_thuy_san", CNTSRule.Tag(), "chan_nuoi_thuy_san:v3"},
	}
	for _, c := range cases {
		cal, err := CalendarOf(c.dept, c.tag)
		if err != nil || cal.Ref() != c.ref || cal.Rule.Tag() != mustRule(t, c.tag).Tag() {
			t.Errorf("CalendarOf(%s, %q) = %s %q %v", c.dept, c.tag, cal.Ref(), cal.Rule.Tag(), err)
		}
	}
	if _, err := CalendarOf("chan_nuoi_thuy_san", CNTSRuleWorkWeek); !errors.Is(err, ErrNotCalendar) {
		t.Fatalf("tuần làm việc BM-05 không thuộc lịch tuần: %v", err)
	}
}

// Lịch CNTS Thứ Ba–Thứ Hai: ví dụ BRD, ranh giới năm, năm 53 tuần.
func TestCNTSCalendar_Weeks(t *testing.T) {
	r := CNTSRule
	cases := []struct {
		name     string
		d        time.Time
		year, no int
		from, to time.Time
	}{
		{"BRD CNTS tuần 39 = 22/09–28/09/2026", Date(2026, 9, 25), 2026, 39, Date(2026, 9, 22), Date(2026, 9, 28)},
		{"01/01/2026 (thứ Năm) thuộc tuần 1 từ 30/12/2025", Date(2026, 1, 1), 2026, 1, Date(2025, 12, 30), Date(2026, 1, 5)},
		{"29/12/2025 (thứ Hai) là tuần cuối 2025", Date(2025, 12, 29), 2025, 52, Date(2025, 12, 23), Date(2025, 12, 29)},
		{"2024 có 53 tuần (01/01 thứ Hai)", Date(2024, 12, 30), 2024, 53, Date(2024, 12, 24), Date(2024, 12, 30)},
		{"31/12/2024 (thứ Ba) mở tuần 1/2025", Date(2024, 12, 31), 2025, 1, Date(2024, 12, 31), Date(2025, 1, 6)},
		{"2029 có 53 tuần (01/01 thứ Hai)", Date(2029, 12, 31), 2029, 53, Date(2029, 12, 25), Date(2029, 12, 31)},
		{"sau tuần 53/2029 là tuần 1/2030", Date(2030, 1, 1), 2030, 1, Date(2030, 1, 1), Date(2030, 1, 7)},
	}
	for _, c := range cases {
		w := r.WeekOf(c.d)
		if w.Year != c.year || w.No != c.no || w.From != c.from || w.To != c.to {
			t.Errorf("%s: %+v", c.name, w)
		}
	}
	for y, n := range map[int]int{2024: 53, 2025: 52, 2026: 52, 2027: 52, 2028: 52, 2029: 53, 2035: 53} {
		if got := r.WeeksInYear(y); got != n {
			t.Errorf("CNTS WeeksInYear(%d) = %d, muốn %d", y, got, n)
		}
	}
	if _, err := r.Week(2026, 53); err == nil {
		t.Fatal("2026 CNTS chỉ có 52 tuần")
	}
	// Hạn mặc định tuần: 12:00 thứ Ba ngay sau kỳ (BRD CNTS); tháng 9/2026 = các tuần có thứ Hai trong tháng 9.
	if d := r.DefaultDeadline(PeriodWeek); d != (Deadline{OffsetDays: 1, Time: "12:00"}) {
		t.Fatalf("hạn tuần CNTS: %+v", d)
	}
	m, err := r.PeriodOf(PeriodMonth, Date(2026, 9, 15), nil)
	if err != nil || m.From != Date(2026, 9, 1) || m.To != Date(2026, 9, 28) || m.From.Weekday() != time.Tuesday || m.To.Weekday() != time.Monday {
		t.Fatalf("tháng 9/2026 CNTS: %+v %v", m, err)
	}
}

// Thuộc tính "year" trên 1900–2200 cho cả 7 ngày bắt đầu tuần (bổ sung TestPeriodRule_WeekProperties): tuần thuộc
// năm của NGÀY CUỐI tuần; số tuần của năm = số ngày "cuối tuần" trong năm (53 khi thứ đó xuất hiện 53 lần, tức
// 01/01 — hoặc 02/01 năm nhuận — rơi vào ngày cuối tuần); tuần 53 chỉ có ở năm đó; khóa tuần đọc ngược đúng tuần.
func TestCalendar_YearNumberingProperties(t *testing.T) {
	for ws := time.Sunday; ws <= time.Saturday; ws++ {
		r := PeriodRule{Version: RuleVersionV2, WeekStart: ws, WeekNumbering: WeekNumberingYear, MonthAlign: MonthAlignWeekEnd, CalendarID: "thu_nghiem", CalendarVersion: 7}
		end := (ws + 6) % 7
		for y := 1900; y <= 2200; y++ {
			n := 0
			for d := Date(y, 1, 1); d.Year() == y; d = d.AddDate(0, 0, 1) {
				if d.Weekday() == end {
					n++
				}
			}
			if got := r.WeeksInYear(y); got != n {
				t.Fatalf("%s %d: WeeksInYear=%d, ngày cuối tuần trong năm=%d", r.Tag(), y, got, n)
			}
			first, err := r.Week(y, 1)
			if err != nil || first.From.After(Date(y, 1, 1)) || first.To.Before(Date(y, 1, 1)) || first.To.Year() != y {
				t.Fatalf("%s %d: tuần 1 phải chứa 01/01: %+v %v", r.Tag(), y, first, err)
			}
			last, err := r.Week(y, n)
			if err != nil || last.To.Year() != y || last.To.AddDate(0, 0, 1) != r.WeekOf(Date(y+1, 1, 1)).From {
				t.Fatalf("%s %d: tuần %d phải liền tuần 1 năm sau: %+v %v", r.Tag(), y, n, last, err)
			}
			if _, err := r.Week(y, n+1); err == nil {
				t.Fatalf("%s %d: không có tuần %d", r.Tag(), y, n+1)
			}
			if y%7 == 0 { // khóa ↔ khoảng (mẫu thưa cho nhanh)
				for no := 1; no <= n; no++ {
					w, _ := r.Week(y, no)
					s, err := r.PeriodOf(PeriodWeek, w.From.AddDate(0, 0, int(ws)%7), nil)
					if err != nil {
						t.Fatal(err)
					}
					back, err := SpanOfKey(r, PeriodWeek, s.Key, nil)
					if err != nil || back != s || back.From != w.From || back.WeekNo != no {
						t.Fatalf("%s SpanOfKey(%s) = %+v %v", r.Tag(), s.Key, back, err)
					}
				}
			}
		}
	}
}

// SpanOfKey là nghịch đảo của PeriodOf(...).Key với mọi loại kỳ có khóa, cho lịch CNTS, mặc định và dương lịch.
func TestSpanOfKey_RoundTrip(t *testing.T) {
	ksgm := func(y int, m time.Month) []int {
		if m == time.February {
			return []int{1, 10, 16, 23}
		}
		return nil
	}
	for _, r := range []PeriodRule{CNTSRule, DefaultRule, mustRule(t, "kn.period_rule/v2;week_start=sun;month=calendar")} {
		for _, pt := range []string{PeriodDay, PeriodWeek, PeriodKSGMWeek, PeriodMonth, PeriodQuarter, PeriodYear} {
			for d := Date(2023, 12, 1); d.Before(Date(2031, 2, 1)); d = d.AddDate(0, 0, 3) {
				s, err := r.PeriodOf(pt, d, ksgm)
				if err != nil {
					t.Fatal(err)
				}
				back, err := SpanOfKey(r, pt, s.Key, ksgm)
				if err != nil || back != s {
					t.Fatalf("%s %s %s: %+v ↔ %+v %v", r.Tag(), pt, s.Key, s, back, err)
				}
			}
		}
	}
	for _, c := range []struct{ kind, key string }{
		{PeriodWeek, "2026-W53"}, {PeriodWeek, "2026-41"}, {PeriodMonth, "2026-13"}, {PeriodQuarter, "2026-Q5"}, {PeriodDay, "2026-02-30"},
		{PeriodKSGMWeek, "2026-02-K5"}, {PeriodSeason, "2026"},
	} {
		if _, err := SpanOfKey(CNTSRule, c.kind, c.key, nil); err == nil {
			t.Errorf("SpanOfKey(%s, %s) phải lỗi", c.kind, c.key)
		}
	}
}

func TestParseCalendarRef(t *testing.T) {
	id, v, err := ParseCalendarRef("ke_hoach_tai_chinh:v2")
	if err != nil || id != DeptKeHoachTaiChinh || v != 2 || CalendarRef(id, v) != "ke_hoach_tai_chinh:v2" {
		t.Fatalf("%s %d %v", id, v, err)
	}
	for _, bad := range []string{"", "khtc", "ke_hoach_tai_chinh:2", "Ke:v1", "x:v1", "ke_hoach:v-1"} {
		if _, _, err := ParseCalendarRef(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
}
