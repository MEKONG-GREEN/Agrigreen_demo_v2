package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// Ca vàng cho backend giả lập của web-ops (apps/web-ops/src/api/mock/kn-reports): chia kỳ, hạn nộp, trạng thái công
// việc, so khác lần tổng hợp, tỉ lệ/năng suất — bản port TypeScript (domain.ts) phải cho đúng kết quả của Go
// (Vitest src/api/mock/kn-reports/domain.test.ts). Ghi lại tệp: AG_UPDATE_WEB_FIXTURES=1 go test
// ./internal/platform/extension/reportcycle/domain -run TestWebReportCycleFixture. Không có cờ: lệch → lỗi.
const webFixtureDir = "../../../../../apps/web-ops/src/api/mock/kn-reports/fixtures"

type fxDeadline struct {
	OffsetDays int    `json:"offset_days"`
	Time       string `json:"time"`
}

type fxPeriod struct {
	Slot        int    `json:"slot"`
	Key         string `json:"key"`
	Label       string `json:"label"`
	RangeLabel  string `json:"range_label"`
	ISOWeek     int    `json:"iso_week"`
	WeekYear    int    `json:"week_year"`
	From        string `json:"from_date"`
	To          string `json:"to_date"`
	DueAt       string `json:"due_at"`
	AutoCloseAt string `json:"auto_close_at"`
}

type fxSplit struct {
	Name           string      `json:"name"`
	PeriodType     string      `json:"period_type"`
	From           string      `json:"from_date"`
	To             string      `json:"to_date"`
	Season         string      `json:"season_label"`
	Rule           string      `json:"rule"`
	Deadline       fxDeadline  `json:"deadline"`
	CloseAfterDays int         `json:"close_after_days"`
	Periods        []fxPeriod  `json:"periods"`
	Error          *ErrPeriod  `json:"error"`
	RuleTag        string      `json:"rule_tag"`
	Default        *fxDeadline `json:"default_deadline,omitempty"`
}

type fxStatus struct {
	Status        string  `json:"status"`
	DueAt         string  `json:"due_at"`
	PeriodClosed  bool    `json:"period_closed"`
	ReopenedUntil *string `json:"reopened_until"`
	Now           string  `json:"now"`
	Display       string  `json:"display"`
	Label         string  `json:"label"`
	Open          bool    `json:"open"`
	Pending       bool    `json:"pending"`
}

type fxChange struct {
	Commune string  `json:"commune"`
	RowKey  string  `json:"row_key"`
	Letter  string  `json:"letter"`
	Old     *string `json:"old"`
	New     *string `json:"new"`
	Kind    string  `json:"kind"`
}

type fxDiff struct {
	Name     string            `json:"name"`
	Prev     []Row             `json:"prev"`
	Next     []Row             `json:"next"`
	PrevSrc  map[string]Source `json:"prev_src"`
	NextSrc  map[string]Source `json:"next_src"`
	Formula  []string          `json:"formula"`
	Changes  []fxChange        `json:"changes"`
	Communes int               `json:"communes"`
}

type fxNum struct {
	A      string `json:"a"`
	B      string `json:"b"`
	Result string `json:"result"`
}

type fxReason struct {
	Input string `json:"input"`
	Max   int    `json:"max"`
	Out   string `json:"out"`
	OK    bool   `json:"ok"`
}

type webFixture struct {
	Note       string                `json:"_note"`
	Statuses   map[string]string     `json:"status_labels"`
	Defaults   map[string]fxDeadline `json:"default_deadlines"`
	DefaultsV2 map[string]fxDeadline `json:"default_deadlines_v2"`
	CloseAfter int                   `json:"default_close_after_days"`
	MaxPeriods int                   `json:"max_periods"`
	Split      []fxSplit             `json:"split"`
	Status     []fxStatus            `json:"status"`
	Diff       []fxDiff              `json:"diff"`
	Ratio      []fxNum               `json:"ratio"`
	Yield      []fxNum               `json:"yield"`
	Reason     []fxReason            `json:"reason"`
}

func fxTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func ptr(s string) *string { return &s }

func TestWebReportCycleFixture(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	fx := webFixture{
		Note:       "SINH TỰ ĐỘNG bởi TestWebReportCycleFixture (internal/platform/extension/reportcycle/domain) — không sửa tay.",
		Statuses:   StatusLabel,
		Defaults:   map[string]fxDeadline{},
		DefaultsV2: map[string]fxDeadline{},
		CloseAfter: DefaultCloseAfterDays,
		MaxPeriods: MaxPeriods,
	}
	for _, pt := range []string{PeriodWeek, PeriodMonth, PeriodSeason, PeriodYear, PeriodAdhoc} {
		d := DefaultDeadline(pt)
		fx.Defaults[pt] = fxDeadline(d)
	}
	for _, pt := range []string{PeriodDay, PeriodWeek, PeriodKSGMWeek, PeriodMonth, PeriodQuarter, PeriodSeason, PeriodYear, PeriodAdhoc} {
		fx.DefaultsV2[pt] = fxDeadline(DefaultRule.DefaultDeadline(pt))
	}
	type sc struct {
		name, pt, from, to, season, rule string
		d                                Deadline
		closeAfter                       int
	}
	v2 := DefaultRule.Tag()
	v2d := DefaultRule.DefaultDeadline
	cases := []sc{
		{"tuần từ thứ Tư, 3 kỳ", PeriodWeek, "2026-09-02", "2026-09-20", "Thu Đông 2026", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodWeek), 3},
		{"tuần ISO", PeriodWeek, "2026-09-07", "2026-09-27", "", "kn.period_rule/v1-tam;week=iso", DefaultDeadline(PeriodWeek), 1},
		{"tuần qua năm", PeriodWeek, "2026-12-21", "2027-01-10", "Đông Xuân 2026-2027", "kn.period_rule/v1-tam;week=season", Deadline{OffsetDays: 1, Time: "08:30"}, 0},
		{"tháng", PeriodMonth, "2026-07-15", "2026-09-30", "", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodMonth), 3},
		{"vụ", PeriodSeason, "2026-09-01", "2026-12-31", "Thu Đông 2026", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodSeason), 3},
		{"năm không tên", PeriodYear, "2026-01-01", "2026-12-31", "", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodYear), 5},
		{"đột xuất", PeriodAdhoc, "2026-10-01", "2026-10-03", "Rà soát sau lũ", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodAdhoc), 2},
		{"đến ngày trước từ ngày", PeriodWeek, "2026-09-07", "2026-09-01", "", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodWeek), 3},
		{"quá số kỳ", PeriodWeek, "2025-01-01", "2026-12-31", "", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodWeek), 3},
		{"giờ sai", PeriodWeek, "2026-09-07", "2026-09-13", "", "kn.period_rule/v1-tam;week=season", Deadline{OffsetDays: 2, Time: "25:00"}, 3},
		{"chốt sau hạn quá 60", PeriodWeek, "2026-09-07", "2026-09-13", "", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodWeek), 61},
		{"loại kỳ lạ", "quarter", "2026-09-07", "2026-09-13", "", "kn.period_rule/v1-tam;week=season", DefaultDeadline(PeriodWeek), 3},
		// Quy tắc v2 (W11.03, QĐ-6): tuần Thứ Tư–Thứ Ba, số tuần theo năm, tháng/quý/năm theo Thứ Ba cuối.
		{"v2 tuần Thứ Tư–Thứ Ba", PeriodWeek, "2026-09-02", "2026-09-22", "Thu Đông 2026", v2, v2d(PeriodWeek), 3},
		{"v2 tuần cắt đầu/cuối, qua năm", PeriodWeek, "2026-12-28", "2027-01-08", "", v2, v2d(PeriodWeek), 0},
		{"v2 tuần 53 (2030)", PeriodWeek, "2030-12-25", "2031-01-07", "", v2, v2d(PeriodWeek), 0},
		{"v2 tuần n của đợt", PeriodWeek, "2026-09-02", "2026-09-15", "", "kn.period_rule/v2;week=season", v2d(PeriodWeek), 1},
		{"v2 tuần ISO thứ Hai (53/2026)", PeriodWeek, "2026-12-21", "2027-01-10", "", "kn.period_rule/v2;week_start=mon;week=iso;month=calendar", v2d(PeriodWeek), 0},
		{"v2 tuần thứ Ba–thứ Hai (CNTS)", PeriodWeek, "2026-09-22", "2026-10-05", "", "kn.period_rule/v2;week_start=tue", v2d(PeriodWeek), 0},
		{"v2 tháng theo Thứ Ba cuối", PeriodMonth, "2026-07-15", "2026-09-30", "", v2, v2d(PeriodMonth), 3},
		{"v2 tháng dương lịch", PeriodMonth, "2026-07-15", "2026-09-30", "", "kn.period_rule/v2;month=calendar", DefaultRule.DefaultDeadline(PeriodMonth), 3},
		{"v2 quý", PeriodQuarter, "2026-01-01", "2026-12-31", "", v2, v2d(PeriodQuarter), 3},
		{"v2 năm", PeriodYear, "2025-12-31", "2027-01-05", "", v2, v2d(PeriodYear), 3},
		{"v2 ngày", PeriodDay, "2026-02-27", "2026-03-02", "", v2, v2d(PeriodDay), 0},
		{"v2 tuần KSGM", PeriodKSGMWeek, "2026-02-01", "2026-03-10", "", v2, v2d(PeriodKSGMWeek), 0},
		{"v2 vụ", PeriodSeason, "2026-09-01", "2026-12-31", "Thu Đông 2026", v2, v2d(PeriodSeason), 3},
		{"v2 quá số kỳ ngày", PeriodDay, "2026-01-01", "2027-01-02", "", v2, v2d(PeriodDay), 0},
		{"v2 loại kỳ lạ", "fortnight", "2026-09-07", "2026-09-13", "", v2, v2d(PeriodWeek), 3},
	}
	for _, c := range cases {
		from, _ := ParseDate(c.from)
		to, _ := ParseDate(c.to)
		rule, err := ParseRule(c.rule)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		ps, err := Split(SplitInput{PeriodType: c.pt, From: from, To: to, SeasonLabel: c.season, Rule: rule, Deadline: c.d, CloseAfterDays: c.closeAfter, Loc: loc})
		out := fxSplit{Name: c.name, PeriodType: c.pt, From: c.from, To: c.to, Season: c.season, Rule: c.rule,
			Deadline: fxDeadline{c.d.OffsetDays, c.d.Time}, CloseAfterDays: c.closeAfter, Periods: []fxPeriod{}, RuleTag: rule.Tag()}
		if err != nil {
			var e ErrPeriod
			if !errors.As(err, &e) {
				t.Fatalf("%s: lỗi không phải ErrPeriod: %v", c.name, err)
			}
			out.Error = &e
		}
		for _, p := range ps {
			out.Periods = append(out.Periods, fxPeriod{p.Slot, p.Key, p.Label, p.RangeLabel, p.ISOWeek, p.WeekYear, p.From.Format(time.DateOnly), p.To.Format(time.DateOnly), fxTime(p.DueAt), fxTime(p.AutoCloseAt)})
		}
		fx.Split = append(fx.Split, out)
	}

	due := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	reopen := due.Add(48 * time.Hour)
	for _, st := range []string{TaskNotStarted, TaskDraft, TaskSubmitted, TaskAdjustmentRequested, TaskConfirmed} {
		for _, closed := range []bool{false, true} {
			for _, ru := range []*time.Time{nil, &reopen} {
				for _, now := range []time.Time{due.Add(-time.Minute), due, reopen} {
					s := TaskState{Status: st, DueAt: due, PeriodClosed: closed, ReopenedUntil: ru}
					var rs *string
					if ru != nil {
						rs = ptr(fxTime(*ru))
					}
					fx.Status = append(fx.Status, fxStatus{Status: st, DueAt: fxTime(due), PeriodClosed: closed, ReopenedUntil: rs, Now: fxTime(now),
						Display: s.Display(now), Label: StatusLabel[s.Display(now)], Open: s.Open(now), Pending: s.Pending()})
				}
			}
		}
	}

	src := func(id string, adj bool) Source {
		return Source{SubmissionID: id, VersionNo: 1, SubmittedAt: "2026-09-08T01:40:00Z", ActorRef: "a1", AdjustmentResponse: adj}
	}
	diffs := []fxDiff{
		{Name: "lần đầu: mọi ô là Gửi mới, cột công thức Tự tính",
			Next:    []Row{{Key: "91301", Commune: "91301", Region: "LX", Cells: map[string]string{"D": "126", "F": "28", "M": "28"}}, {Key: "91302", Commune: "91302", Region: "LX", Cells: map[string]string{"F": "30", "M": "30"}}},
			NextSrc: map[string]Source{"91301": src("s1", false), "91302": src("s2", false)}, Formula: []string{"M"}},
		{Name: "gửi lại, điều chỉnh theo yêu cầu, số bằng nhau theo decimal, xóa ô",
			Prev: []Row{{Key: "91301", Commune: "91301", Region: "LX", Cells: map[string]string{"F": "28", "G": "38", "M": "66.0"}}, {Key: "91302", Commune: "91302", Region: "LX", Cells: map[string]string{"F": "30", "G": "20", "M": "50"}},
				{Key: "91303", Commune: "91303", Region: "LX", Cells: map[string]string{"F": "1.50", "N": "2026-09-01"}}},
			Next: []Row{{Key: "91301", Commune: "91301", Region: "LX", Cells: map[string]string{"F": "28", "G": "38", "H": "38.5", "M": "104.5"}}, {Key: "91302", Commune: "91302", Region: "LX", Cells: map[string]string{"F": "30", "G": "15", "M": "45"}},
				{Key: "91303", Commune: "91303", Region: "LX", Cells: map[string]string{"F": "1.5"}}},
			PrevSrc: map[string]Source{"91301": src("s1", false), "91302": src("s2", false), "91303": src("s3", false)},
			NextSrc: map[string]Source{"91301": src("s4", false), "91302": src("s5", true), "91303": src("s6", false)}, Formula: []string{"M"}},
		{Name: "dòng chỉ tiêu, xã mới, sắp Z trước AA",
			Prev:    []Row{{Key: "91301/SB01", Commune: "91301", Indicator: "SB01", Cells: map[string]string{"Z": "1"}}},
			Next:    []Row{{Key: "91301/SB01", Commune: "91301", Indicator: "SB01", Cells: map[string]string{"Z": "2", "AA": "3"}}, {Key: "91304/SB01", Commune: "91304", Indicator: "SB01", Cells: map[string]string{"E": "4"}}},
			PrevSrc: map[string]Source{"91301": src("s1", false)}, NextSrc: map[string]Source{"91301": src("s2", false), "91304": src("s3", false)}},
	}
	for i := range diffs {
		d := &diffs[i]
		formula := map[string]bool{}
		for _, l := range d.Formula {
			formula[l] = true
		}
		if d.Formula == nil {
			d.Formula = []string{}
		}
		if d.Prev == nil {
			d.Prev = []Row{}
		}
		if d.PrevSrc == nil {
			d.PrevSrc = map[string]Source{}
		}
		ch, n := Diff(d.Prev, d.Next, d.PrevSrc, d.NextSrc, formula)
		d.Changes = []fxChange{}
		for _, c := range ch {
			d.Changes = append(d.Changes, fxChange{c.Commune, c.RowKey, c.Letter, c.Old, c.New, c.Kind})
		}
		d.Communes = n
	}
	fx.Diff = diffs

	for _, p := range [][2]string{{"104.5", "126"}, {"0", "0"}, {"5", "0"}, {"1", "3"}, {"2", "3"}, {"88600", "86950"}, {"0.05", "1"}} {
		a, _ := decimal.NewFromString(p[0])
		b, _ := decimal.NewFromString(p[1])
		fx.Ratio = append(fx.Ratio, fxNum{p[0], p[1], Ratio(a, b).String()})
		fx.Yield = append(fx.Yield, fxNum{p[0], p[1], Yield(a, b).String()})
	}
	for _, r := range []struct {
		s string
		m int
	}{{"  ngắn  ", 500}, {"Kiểm tra lại tuần 3", 500}, {"  Đủ mười ký tự  ", 10}, {strings.Repeat("á", 11), 10}, {"", 2000}} {
		out, ok := Reason(r.s, r.m)
		fx.Reason = append(fx.Reason, fxReason{r.s, r.m, out, ok})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(fx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(webFixtureDir, "rc-golden.json")
	if os.Getenv("AG_UPDATE_WEB_FIXTURES") == "1" {
		if err := os.MkdirAll(webFixtureDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("thiếu ca vàng %s (chạy với AG_UPDATE_WEB_FIXTURES=1): %v", path, err)
	}
	if strings.ReplaceAll(string(got), "\r\n", "\n") != buf.String() {
		t.Fatalf("ca vàng %s lệch với Go — chạy AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/extension/reportcycle/domain -run TestWebReportCycleFixture", path)
	}
}
