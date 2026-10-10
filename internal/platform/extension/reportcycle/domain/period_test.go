package domain

import (
	"errors"
	"testing"
	"time"
	_ "time/tzdata" // Asia/Ho_Chi_Minh trên máy không có cơ sở dữ liệu múi giờ (Windows)
)

// W11.03 — lịch kỳ hệ thống dùng chung (quy tắc v2, QĐ-6/Q-045).

func mustRule(t *testing.T, tag string) PeriodRule {
	t.Helper()
	r, err := ParseRule(tag)
	if err != nil {
		t.Fatalf("ParseRule(%q): %v", tag, err)
	}
	return r
}

func TestPeriodRule_TagParse(t *testing.T) {
	cases := []struct {
		tag, want string
		ok        bool
	}{
		{"", "", false},
		{"kn.period_rule/v1-tam;week=season", "kn.period_rule/v1-tam;week=season", true}, // đợt cũ giữ nguyên tag
		{"kn.period_rule/v1-tam;week=iso", "kn.period_rule/v1-tam;week=iso", true},
		{"kn.period_rule/v2", "kn.period_rule/v2;week_start=wed;week=year;month=week_end", true},
		{"kn.period_rule/v2;week_start=tue;week=season;month=calendar", "kn.period_rule/v2;week_start=tue;week=season;month=calendar", true},
		{"kn.period_rule/v1-tam;week=year", "", false}, // v1 không có số tuần theo năm
		{"kn.period_rule/v2;week_start=xyz", "", false},
		{"kn.period_rule/v2;month=fiscal", "", false},
		{"kn.period_rule/v2;color=red", "", false},
		{"kn.period_rule/v3", "", false},
	}
	for _, c := range cases {
		r, err := ParseRule(c.tag)
		if (err == nil) != c.ok {
			t.Fatalf("%q: err=%v, muốn ok=%v", c.tag, err, c.ok)
		}
		if c.ok && r.Tag() != c.want {
			t.Fatalf("%q: Tag()=%q, muốn %q", c.tag, r.Tag(), c.want)
		}
	}
	if DefaultRule.WeekStart != time.Wednesday || LegacyRule.Tag() != "kn.period_rule/v1-tam;week=season" {
		t.Fatal("mặc định QĐ-6: Thứ Tư; v1 giữ tag cũ")
	}
	for in, want := range map[string]PeriodRule{"": LegacyRule, "rác": LegacyRule, DefaultRule.Tag(): DefaultRule, "kn.period_rule/v1-tam;week=iso": {RuleVersionV1, time.Monday, WeekNumberingISO, MonthAlignCalendar}} {
		if got := EffectiveRule(in); got != want {
			t.Errorf("EffectiveRule(%q) = %+v", in, got)
		}
	}
}

func TestPeriodRule_WeekOf(t *testing.T) {
	v2 := DefaultRule
	cnts := mustRule(t, "kn.period_rule/v2;week_start=tue")
	iso := mustRule(t, "kn.period_rule/v2;week_start=mon;week=iso;month=calendar")
	cases := []struct {
		name     string
		r        PeriodRule
		d        time.Time
		year, no int
		from, to time.Time
	}{
		{"KHTC ví dụ tuần 41/2026", v2, Date(2026, 10, 9), 2026, 41, Date(2026, 10, 7), Date(2026, 10, 13)},
		{"01/01 thuộc tuần 1 (bắt đầu cuối năm trước)", v2, Date(2026, 1, 1), 2026, 1, Date(2025, 12, 31), Date(2026, 1, 6)},
		{"31/12/2025 cũng là tuần 1/2026", v2, Date(2025, 12, 31), 2026, 1, Date(2025, 12, 31), Date(2026, 1, 6)},
		{"tuần cuối 2025 = 52", v2, Date(2025, 12, 30), 2025, 52, Date(2025, 12, 24), Date(2025, 12, 30)},
		{"năm 53 tuần (2030)", v2, Date(2030, 12, 31), 2030, 53, Date(2030, 12, 25), Date(2030, 12, 31)},
		{"sau tuần 53 là tuần 1/2031", v2, Date(2031, 1, 1), 2031, 1, Date(2031, 1, 1), Date(2031, 1, 7)},
		{"CNTS H04 tuần 39 thứ Ba–thứ Hai", cnts, Date(2026, 9, 25), 2026, 39, Date(2026, 9, 22), Date(2026, 9, 28)},
		{"CNTS tuần 40 (hạn 06/10)", cnts, Date(2026, 10, 5), 2026, 40, Date(2026, 9, 29), Date(2026, 10, 5)},
		{"ISO tuần 53/2026", iso, Date(2027, 1, 3), 2026, 53, Date(2026, 12, 28), Date(2027, 1, 3)},
		{"ISO tuần 1/2025 bắt đầu 30/12/2024", iso, Date(2024, 12, 31), 2025, 1, Date(2024, 12, 30), Date(2025, 1, 5)},
		{"v1 (ISO, thứ Hai)", LegacyRule, Date(2026, 9, 9), 2026, 37, Date(2026, 9, 7), Date(2026, 9, 13)},
	}
	for _, c := range cases {
		w := c.r.WeekOf(c.d)
		if w.Year != c.year || w.No != c.no || w.From != c.from || w.To != c.to {
			t.Errorf("%s: %+v", c.name, w)
		}
	}
	for _, c := range []struct {
		r    PeriodRule
		y, n int
	}{{v2, 2025, 52}, {v2, 2026, 52}, {v2, 2030, 53}, {iso, 2026, 53}, {iso, 2027, 52}, {iso, 2020, 53}} {
		if got := c.r.WeeksInYear(c.y); got != c.n {
			t.Errorf("%s WeeksInYear(%d)=%d, muốn %d", c.r.Tag(), c.y, got, c.n)
		}
	}
	if _, err := v2.Week(2026, 53); err == nil {
		t.Fatal("2026 chỉ có 52 tuần")
	}
}

// Thuộc tính trên 2000–2040 cho cả 7 ngày bắt đầu tuần: tuần đủ 7 ngày bắt đầu đúng thứ, chứa d, đánh số liên tục,
// tuần chứa 01/01 là tuần 1 ("year"), Week(y, no) quay về đúng tuần; với thứ Hai + "iso" trùng time.ISOWeek.
func TestPeriodRule_WeekProperties(t *testing.T) {
	for ws := time.Sunday; ws <= time.Saturday; ws++ {
		for _, numbering := range []string{WeekNumberingYear, WeekNumberingISO} {
			r := PeriodRule{Version: RuleVersionV2, WeekStart: ws, WeekNumbering: numbering, MonthAlign: MonthAlignWeekEnd}
			prev := r.WeekOf(Date(1999, 12, 31))
			for d := Date(2000, 1, 1); d.Before(Date(2041, 1, 1)); d = d.AddDate(0, 0, 1) {
				w := r.WeekOf(d)
				if w.From.Weekday() != ws || w.To != w.From.AddDate(0, 0, 6) || d.Before(w.From) || d.After(w.To) {
					t.Fatalf("%s %s: %+v", r.Tag(), d.Format(time.DateOnly), w)
				}
				if w.From != prev.From {
					if (w.Year == prev.Year && w.No != prev.No+1) || (w.Year != prev.Year && (w.No != 1 || prev.No != r.WeeksInYear(prev.Year))) {
						t.Fatalf("%s: đánh số không liên tục %+v → %+v", r.Tag(), prev, w)
					}
					back, err := r.Week(w.Year, w.No)
					if err != nil || back != w {
						t.Fatalf("%s: Week(%d,%d)=%+v %v", r.Tag(), w.Year, w.No, back, err)
					}
					prev = w
				}
				if numbering == WeekNumberingYear && d.Month() == 1 && d.Day() == 1 && (w.No != 1 || w.Year != d.Year()) {
					t.Fatalf("%s: tuần chứa 01/01/%d phải là tuần 1: %+v", r.Tag(), d.Year(), w)
				}
				if ws == time.Monday && numbering == WeekNumberingISO {
					if y, n := d.ISOWeek(); y != w.Year || n != w.No {
						t.Fatalf("ISO %s: %d-W%d, muốn %d-W%d", d.Format(time.DateOnly), w.Year, w.No, y, n)
					}
				}
			}
		}
	}
}

func TestPeriodRule_PeriodOf(t *testing.T) {
	v2 := DefaultRule
	cal := mustRule(t, "kn.period_rule/v2;month=calendar")
	aug := func(y int, m time.Month) []int {
		if y == 2026 && m == time.August {
			return []int{1, 10, 16, 23}
		}
		return nil
	}
	cases := []struct {
		name, pt string
		r        PeriodRule
		d        time.Time
		key      string
		label    string
		from, to time.Time
	}{
		{"ngày", PeriodDay, v2, Date(2028, 2, 29), "2028-02-29", "Ngày 29/02/2028", Date(2028, 2, 29), Date(2028, 2, 29)},
		{"tuần", PeriodWeek, v2, Date(2026, 10, 13), "2026-W41", "Tuần 41/2026", Date(2026, 10, 7), Date(2026, 10, 13)},
		{"tháng 9 tới Thứ Ba cuối 29/09", PeriodMonth, v2, Date(2026, 9, 15), "2026-09", "Tháng 9/2026", Date(2026, 8, 26), Date(2026, 9, 29)},
		{"30/09 thuộc tháng 10 (Thứ Ba 06/10)", PeriodMonth, v2, Date(2026, 9, 30), "2026-10", "Tháng 10/2026", Date(2026, 9, 30), Date(2026, 10, 27)},
		{"tháng dương lịch", PeriodMonth, cal, Date(2026, 9, 30), "2026-09", "Tháng 9/2026", Date(2026, 9, 1), Date(2026, 9, 30)},
		{"quý III tới 29/09", PeriodQuarter, v2, Date(2026, 8, 1), "2026-Q3", "Quý III/2026", Date(2026, 7, 1), Date(2026, 9, 29)},
		{"quý dương lịch", PeriodQuarter, cal, Date(2026, 12, 31), "2026-Q4", "Quý IV/2026", Date(2026, 10, 1), Date(2026, 12, 31)},
		{"năm 2026 = 31/12/2025–29/12/2026", PeriodYear, v2, Date(2026, 6, 1), "2026", "Năm 2026", Date(2025, 12, 31), Date(2026, 12, 29)},
		{"30/12/2026 thuộc năm 2027", PeriodYear, v2, Date(2026, 12, 30), "2027", "Năm 2027", Date(2026, 12, 30), Date(2027, 12, 28)},
		{"năm 53 tuần (2030)", PeriodYear, v2, Date(2030, 12, 31), "2030", "Năm 2030", Date(2029, 12, 26), Date(2030, 12, 31)},
		{"KSGM mặc định tuần 4 tới cuối tháng", PeriodKSGMWeek, v2, Date(2026, 2, 28), "2026-02-K4", "Tuần KSGM 4 tháng 2/2026", Date(2026, 2, 22), Date(2026, 2, 28)},
		{"KSGM tháng nhuận", PeriodKSGMWeek, v2, Date(2028, 2, 29), "2028-02-K4", "Tuần KSGM 4 tháng 2/2028", Date(2028, 2, 22), Date(2028, 2, 29)},
		{"KSGM sửa ranh giới 8/2026", PeriodKSGMWeek, v2, Date(2026, 8, 9), "2026-08-K1", "Tuần KSGM 1 tháng 8/2026", Date(2026, 8, 1), Date(2026, 8, 9)},
		{"KSGM sửa ranh giới 8/2026 tuần 4", PeriodKSGMWeek, v2, Date(2026, 8, 31), "2026-08-K4", "Tuần KSGM 4 tháng 8/2026", Date(2026, 8, 23), Date(2026, 8, 31)},
	}
	for _, c := range cases {
		s, err := c.r.PeriodOf(c.pt, c.d, aug)
		if err != nil || s.Key != c.key || s.Label != c.label || s.From != c.from || s.To != c.to {
			t.Errorf("%s: %+v %v", c.name, s, err)
		}
	}
	for _, c := range []struct {
		r  PeriodRule
		pt string
	}{{v2, PeriodSeason}, {v2, "fortnight"}, {LegacyRule, PeriodDay}, {LegacyRule, PeriodQuarter}, {LegacyRule, PeriodKSGMWeek}} {
		if _, err := c.r.PeriodOf(c.pt, Date(2026, 1, 1), nil); err == nil {
			t.Errorf("%s %s: phải lỗi", c.r.Tag(), c.pt)
		}
	}
}

// Mọi ngày thuộc đúng một tháng/quý/năm; kỳ liền nhau không hở, không chồng; tháng theo Thứ Ba cuối kết thúc đúng
// ngày cuối tuần và gồm 4 hoặc 5 tuần.
func TestPeriodRule_AlignedPeriodsTile(t *testing.T) {
	for _, r := range []PeriodRule{DefaultRule, mustRule(t, "kn.period_rule/v2;week_start=tue"), mustRule(t, "kn.period_rule/v2;month=calendar")} {
		for _, pt := range []string{PeriodMonth, PeriodQuarter, PeriodYear} {
			prev, _ := r.PeriodOf(pt, Date(2019, 12, 1), nil)
			for d := Date(2020, 1, 1); d.Before(Date(2036, 1, 1)); d = d.AddDate(0, 0, 1) {
				s, err := r.PeriodOf(pt, d, nil)
				if err != nil || d.Before(s.From) || d.After(s.To) {
					t.Fatalf("%s %s %s: %+v %v", r.Tag(), pt, d.Format(time.DateOnly), s, err)
				}
				if s.Key != prev.Key {
					if s.From != prev.To.AddDate(0, 0, 1) {
						t.Fatalf("%s %s: hở/chồng %+v → %+v", r.Tag(), pt, prev, s)
					}
					prev = s
				}
				if r.MonthAlign == MonthAlignWeekEnd {
					if s.To != r.WeekOf(s.To).To || s.From != r.WeekOf(s.From).From {
						t.Fatalf("%s %s: kỳ phải gồm trọn tuần: %+v", r.Tag(), pt, s)
					}
					if n := days(s.From, s.To) + 1; pt == PeriodMonth && n != 28 && n != 35 {
						t.Fatalf("%s: tháng %s có %d ngày", r.Tag(), s.Key, n)
					}
				}
			}
		}
	}
}

func TestKSGMWeeks(t *testing.T) {
	cases := []struct {
		name   string
		y      int
		m      time.Month
		starts []int
		ends   []int // ngày cuối 4 tuần; nil = lỗi
	}{
		{"mặc định tháng 31 ngày", 2026, time.October, nil, []int{7, 14, 21, 31}},
		{"mặc định tháng 2 thường", 2026, time.February, nil, []int{7, 14, 21, 28}},
		{"mặc định tháng 2 nhuận", 2028, time.February, nil, []int{7, 14, 21, 29}},
		{"sửa ranh giới (H04 ví dụ 8/2026)", 2026, time.August, []int{1, 10, 16, 23}, []int{9, 15, 22, 31}},
		{"tuần 1 không từ ngày 1", 2026, time.August, []int{2, 10, 16, 23}, nil},
		{"không tăng dần", 2026, time.August, []int{1, 16, 10, 23}, nil},
		{"quá cuối tháng", 2026, time.February, []int{1, 8, 15, 29}, nil},
		{"thiếu tuần", 2026, time.August, []int{1, 10, 16}, nil},
	}
	for _, c := range cases {
		ws, err := KSGMWeeks(c.y, c.m, c.starts)
		if c.ends == nil {
			if err == nil {
				t.Errorf("%s: phải lỗi", c.name)
			}
			continue
		}
		if err != nil || len(ws) != 4 || ws[0].From.Day() != 1 {
			t.Fatalf("%s: %+v %v", c.name, ws, err)
		}
		for i, w := range ws {
			if w.To.Day() != c.ends[i] || (i > 0 && w.From != ws[i-1].To.AddDate(0, 0, 1)) {
				t.Errorf("%s: tuần %d %+v", c.name, i+1, w)
			}
		}
	}
	if st, err := ParseKSGMStarts(" 1, 10,16 ,23"); err != nil || len(st) != 4 || st[1] != 10 {
		t.Fatalf("ParseKSGMStarts: %v %v", st, err)
	}
	if _, err := ParseKSGMStarts("1,a"); err == nil {
		t.Fatal("ParseKSGMStarts phải lỗi")
	}
}

func TestSplitV2(t *testing.T) {
	type want struct {
		key, label string
		from, to   time.Time
		due        time.Time
	}
	z := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }
	cases := []struct {
		name   string
		in     SplitInput
		want   []want
		errFld string
	}{
		{"tuần Thứ Tư–Thứ Ba, hạn 12:00 hôm sau", SplitInput{PeriodType: PeriodWeek, From: Date(2026, 9, 2), To: Date(2026, 9, 15), Rule: DefaultRule, Deadline: DefaultRule.DefaultDeadline(PeriodWeek)},
			[]want{{"2026-W36", "Tuần 36/2026", Date(2026, 9, 2), Date(2026, 9, 8), z(2026, 9, 9, 5)}, {"2026-W37", "Tuần 37/2026", Date(2026, 9, 9), Date(2026, 9, 15), z(2026, 9, 16, 5)}}, ""},
		{"tuần cắt đầu/cuối qua năm", SplitInput{PeriodType: PeriodWeek, From: Date(2025, 12, 29), To: Date(2026, 1, 2), Rule: DefaultRule, Deadline: Deadline{0, "17:00"}},
			[]want{{"2025-W52", "Tuần 52/2025", Date(2025, 12, 29), Date(2025, 12, 30), z(2025, 12, 30, 10)}, {"2026-W01", "Tuần 1/2026", Date(2025, 12, 31), Date(2026, 1, 2), z(2026, 1, 2, 10)}}, ""},
		{"tuần n của đợt", SplitInput{PeriodType: PeriodWeek, From: Date(2026, 9, 2), To: Date(2026, 9, 9), Rule: mustRule(t, "kn.period_rule/v2;week=season"), Deadline: Deadline{1, "12:00"}},
			[]want{{"2026-W36", "Tuần 1", Date(2026, 9, 2), Date(2026, 9, 8), z(2026, 9, 9, 5)}, {"2026-W37", "Tuần 2", Date(2026, 9, 9), Date(2026, 9, 9), z(2026, 9, 10, 5)}}, ""},
		{"tháng theo Thứ Ba cuối", SplitInput{PeriodType: PeriodMonth, From: Date(2026, 8, 26), To: Date(2026, 10, 27), Rule: DefaultRule, Deadline: DefaultRule.DefaultDeadline(PeriodMonth)},
			[]want{{"2026-09", "Tháng 9/2026", Date(2026, 8, 26), Date(2026, 9, 29), z(2026, 9, 30, 3)}, {"2026-10", "Tháng 10/2026", Date(2026, 9, 30), Date(2026, 10, 27), z(2026, 10, 28, 3)}}, ""},
		{"ngày", SplitInput{PeriodType: PeriodDay, From: Date(2028, 2, 28), To: Date(2028, 3, 1), Rule: DefaultRule, Deadline: DefaultRule.DefaultDeadline(PeriodDay)},
			[]want{{"2028-02-28", "Ngày 28/02/2028", Date(2028, 2, 28), Date(2028, 2, 28), z(2028, 2, 29, 2)}, {"2028-02-29", "Ngày 29/02/2028", Date(2028, 2, 29), Date(2028, 2, 29), z(2028, 3, 1, 2)},
				{"2028-03-01", "Ngày 01/03/2028", Date(2028, 3, 1), Date(2028, 3, 1), z(2028, 3, 2, 2)}}, ""},
		{"KSGM 4 tuần, ranh giới sửa", SplitInput{PeriodType: PeriodKSGMWeek, From: Date(2026, 8, 1), To: Date(2026, 8, 31), Rule: DefaultRule, Deadline: Deadline{1, "12:00"},
			KSGM: func(int, time.Month) []int { return []int{1, 10, 16, 23} }},
			[]want{{"2026-08-K1", "Tuần KSGM 1 tháng 8/2026", Date(2026, 8, 1), Date(2026, 8, 9), z(2026, 8, 10, 5)}, {"2026-08-K2", "Tuần KSGM 2 tháng 8/2026", Date(2026, 8, 10), Date(2026, 8, 15), z(2026, 8, 16, 5)},
				{"2026-08-K3", "Tuần KSGM 3 tháng 8/2026", Date(2026, 8, 16), Date(2026, 8, 22), z(2026, 8, 23, 5)}, {"2026-08-K4", "Tuần KSGM 4 tháng 8/2026", Date(2026, 8, 23), Date(2026, 8, 31), z(2026, 9, 1, 5)}}, ""},
		{"năm v2 theo Thứ Ba cuối", SplitInput{PeriodType: PeriodYear, From: Date(2025, 12, 31), To: Date(2026, 12, 29), Rule: DefaultRule, Deadline: Deadline{1, "10:00"}},
			[]want{{"2026", "Năm 2026", Date(2025, 12, 31), Date(2026, 12, 29), z(2026, 12, 30, 3)}}, ""},
		{"KSGM ranh giới sai", SplitInput{PeriodType: PeriodKSGMWeek, From: Date(2026, 2, 1), To: Date(2026, 2, 28), Rule: DefaultRule, Deadline: Deadline{1, "12:00"},
			KSGM: func(int, time.Month) []int { return []int{1, 8, 15, 30} }}, nil, "ksgm_week_starts"},
		{"quá 366 kỳ ngày", SplitInput{PeriodType: PeriodDay, From: Date(2026, 1, 1), To: Date(2027, 1, 2), Rule: DefaultRule, Deadline: Deadline{1, "09:00"}}, nil, "to_date"},
		{"quá 10 năm", SplitInput{PeriodType: PeriodYear, From: Date(2000, 1, 1), To: Date(2030, 1, 1), Rule: DefaultRule, Deadline: Deadline{1, "09:00"}}, nil, "to_date"},
		{"quy tắc sai", SplitInput{PeriodType: PeriodWeek, From: Date(2026, 1, 1), To: Date(2026, 1, 2), Rule: PeriodRule{Version: "x"}, Deadline: Deadline{1, "09:00"}}, nil, "period_rule"},
		{"loại kỳ lạ", SplitInput{PeriodType: "fortnight", From: Date(2026, 1, 1), To: Date(2026, 1, 2), Rule: DefaultRule, Deadline: Deadline{1, "09:00"}}, nil, "period_type"},
	}
	for _, c := range cases {
		c.in.Loc = vn
		ps, err := Split(c.in)
		if c.errFld != "" {
			var pe ErrPeriod
			if !errors.As(err, &pe) || pe.Field != c.errFld {
				t.Errorf("%s: lỗi %v, muốn trường %s", c.name, err, c.errFld)
			}
			continue
		}
		if err != nil || len(ps) != len(c.want) {
			t.Fatalf("%s: %+v %v", c.name, ps, err)
		}
		for i, w := range c.want {
			p := ps[i]
			if p.Slot != i+1 || p.Key != w.key || p.Label != w.label || p.From != w.from || p.To != w.to || p.DueAt != w.due {
				t.Errorf("%s: kỳ %d = %+v", c.name, i+1, p)
			}
		}
	}
}

// Asia/Ho_Chi_Minh không có giờ mùa hè: hạn nộp theo vùng IANA trùng UTC+7 cố định mọi ngày; ngày của đợt chỉ lấy
// phần ngày dương lịch nên giờ/múi giờ của giá trị vào không làm lệch kỳ.
func TestSplit_HoChiMinhNoDST(t *testing.T) {
	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	dl := Deadline{OffsetDays: 1, Time: "12:00"}
	for d := Date(2026, 1, 1); d.Before(Date(2028, 1, 1)); d = d.AddDate(0, 0, 1) {
		a, _ := dl.DueAt(d, hcm)
		b, _ := dl.DueAt(d, vn)
		if !a.Equal(b) || a.Hour() != 5 {
			t.Fatalf("%s: %v ≠ %v", d.Format(time.DateOnly), a, b)
		}
	}
	late := time.Date(2026, 9, 2, 23, 30, 0, 0, hcm) // 16:30Z cùng ngày dương lịch ở VN
	ps, err := Split(SplitInput{PeriodType: PeriodWeek, From: late, To: late.AddDate(0, 0, 6), Rule: DefaultRule, Deadline: dl, Loc: hcm})
	if err != nil || len(ps) != 1 || ps[0].From != Date(2026, 9, 2) || ps[0].To != Date(2026, 9, 8) || ps[0].Key != "2026-W36" {
		t.Fatalf("ngày đợt theo lịch VN: %+v %v", ps, err)
	}
}

// W11.05b: loại kỳ chọn được theo loại báo cáo; rỗng = mặc định ReportPeriodType.
func TestResolvePeriodType(t *testing.T) {
	ok := map[[2]string]string{
		{"weekly", ""}: PeriodWeek, {"weekly", PeriodKSGMWeek}: PeriodKSGMWeek, {"weekly", PeriodDay}: PeriodDay,
		{"monthly", ""}: PeriodMonth, {"monthly", PeriodQuarter}: PeriodQuarter, {"annual", PeriodQuarter}: PeriodQuarter,
		{"seasonal", ""}: PeriodSeason, {"adhoc", PeriodDay}: PeriodDay, {"annual", ""}: PeriodYear,
	}
	for in, want := range ok {
		if got, err := ResolvePeriodType(in[0], in[1]); err != nil || got != want {
			t.Errorf("%v: %q %v, muốn %q", in, got, err, want)
		}
	}
	bad := map[[2]string]string{
		{"weekly", PeriodQuarter}: "period_type", {"seasonal", PeriodDay}: "period_type", {"monthly", "fortnight"}: "period_type",
		{"daily", ""}: "report_type",
	}
	for in, field := range bad {
		var pe ErrPeriod
		if _, err := ResolvePeriodType(in[0], in[1]); !errors.As(err, &pe) || pe.Field != field {
			t.Errorf("%v: muốn lỗi %s, nhận %v", in, field, err)
		}
	}
	for rt, pt := range ReportPeriodType {
		if ReportPeriodTypes[rt][0] != pt {
			t.Errorf("%s: mặc định %s ≠ %s", rt, ReportPeriodTypes[rt][0], pt)
		}
	}
}
