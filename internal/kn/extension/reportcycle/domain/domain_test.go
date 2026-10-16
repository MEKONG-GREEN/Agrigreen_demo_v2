package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

var vn = time.FixedZone("ICT", 7*3600)

func splitV1(pt string, from, to time.Time, season string, numbering string, d Deadline, closeAfter int) ([]Period, error) {
	r := LegacyRule
	r.WeekNumbering = numbering
	return Split(SplitInput{PeriodType: pt, From: from, To: to, SeasonLabel: season, Rule: r, Deadline: d, CloseAfterDays: closeAfter, Loc: vn})
}

// FN-TI-02.02 "hệ thống tự chia các kỳ" — quy tắc v1 (Q-17 cũ, giữ để tái tạo đợt đã phát hành): tuần thứ
// Hai–Chủ nhật, kỳ đầu/cuối cắt theo khoảng ngày; nhãn "Tuần n của đợt" hoặc tuần ISO; hạn = cuối kỳ + n ngày lúc
// HH:MM giờ Việt Nam; giờ chốt = hạn + số ngày chốt. Quy tắc v2: period_test.go.
func TestFN_TI_02_02_SplitWeeks(t *testing.T) {
	ps, err := splitV1(PeriodWeek, Date(2026, 9, 2), Date(2026, 9, 20), "Thu Đông 2026", WeekNumberingSeason, DefaultDeadline(PeriodWeek), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 || ps[0].From != Date(2026, 9, 2) || ps[0].To != Date(2026, 9, 6) || ps[2].To != Date(2026, 9, 20) {
		t.Fatalf("3 tuần (kỳ đầu từ thứ Tư): %+v", ps)
	}
	if ps[1].Label != "Tuần 2" || ps[1].ISOWeek != 37 || ps[1].RangeLabel != "07/09–13/09/2026" {
		t.Fatalf("nhãn: %+v", ps[1])
	}
	if ps[1].DueAt != time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC) || ps[1].AutoCloseAt != time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("hạn Thứ Ba tuần sau 17:00, chốt +3 ngày: %v %v", ps[1].DueAt, ps[1].AutoCloseAt)
	}
	iso, _ := splitV1(PeriodWeek, Date(2026, 9, 7), Date(2026, 9, 13), "", WeekNumberingISO, DefaultDeadline(PeriodWeek), 0)
	if iso[0].Label != "Tuần 37" {
		t.Fatalf("tuần ISO: %s", iso[0].Label)
	}
	ms, _ := splitV1(PeriodMonth, Date(2026, 7, 15), Date(2026, 9, 30), "", WeekNumberingSeason, DefaultDeadline(PeriodMonth), 3)
	if len(ms) != 3 || ms[0].Label != "Tháng 7/2026" || ms[0].From != Date(2026, 7, 15) || ms[0].DueAt != time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("tháng (nộp ngày 10 tháng sau): %+v", ms[0])
	}
	if s, _ := splitV1(PeriodSeason, Date(2026, 9, 1), Date(2026, 12, 31), "Thu Đông 2026", WeekNumberingSeason, DefaultDeadline(PeriodSeason), 3); len(s) != 1 || s[0].Label != "Thu Đông 2026" {
		t.Fatalf("vụ = một kỳ: %+v", s)
	}
	if _, err := splitV1(PeriodWeek, Date(2026, 9, 7), Date(2026, 9, 1), "", WeekNumberingSeason, DefaultDeadline(PeriodWeek), 3); err == nil {
		t.Fatal("đến ngày trước từ ngày")
	}
	if _, err := splitV1(PeriodWeek, Date(2025, 1, 1), Date(2026, 12, 31), "", WeekNumberingSeason, DefaultDeadline(PeriodWeek), 3); err == nil {
		t.Fatal("quá số kỳ tối đa")
	}
	if _, err := splitV1(PeriodWeek, Date(2026, 9, 7), Date(2026, 9, 13), "", WeekNumberingSeason, Deadline{OffsetDays: 2, Time: "25:00"}, 3); err == nil {
		t.Fatal("giờ sai")
	}
	if _, err := splitV1(PeriodQuarter, Date(2026, 7, 1), Date(2026, 9, 30), "", WeekNumberingSeason, DefaultDeadline(PeriodMonth), 3); err == nil {
		t.Fatal("v1 không có kỳ quý")
	}
}

// FN-XA-03.01: trạng thái hiển thị — Trễ hạn khi chưa gửi và quá hạn; Đã chốt khi kỳ chốt trừ khi đang mở lại.
func TestFN_XA_03_01_TaskStatus(t *testing.T) {
	due := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	s := TaskState{Status: TaskDraft, DueAt: due}
	if s.Display(due.Add(-time.Minute)) != TaskDraft || s.Display(due) != TaskOverdue {
		t.Fatal("trễ hạn")
	}
	s.Status = TaskSubmitted
	if s.Display(due.Add(time.Hour)) != TaskSubmitted {
		t.Fatal("đã nộp không trễ")
	}
	until := due.Add(48 * time.Hour)
	s = TaskState{Status: TaskSubmitted, DueAt: due, PeriodClosed: true, ReopenedUntil: &until}
	if !s.Open(due.Add(time.Hour)) || s.Open(until) || s.Display(until) != TaskClosed {
		t.Fatal("mở lại đến hạn mở")
	}
	if _, ok := Reason("  ngắn ", 500); ok {
		t.Fatal("lý do ≥ 10 ký tự")
	}
}

// FN-TI-02.05: so khác lần tổng hợp — chỉ ô khác (decimal 1.50 = 1.5), loại Gửi mới / Gửi lại / Điều chỉnh / Tự tính.
func TestFN_TI_02_05_Diff(t *testing.T) {
	prev := []Row{{Key: "a", Commune: "a", Cells: map[string]string{"F": "1.50", "M": "1.5"}}, {Key: "b", Commune: "b", Cells: map[string]string{"F": "2"}}}
	next := []Row{{Key: "a", Commune: "a", Cells: map[string]string{"F": "1.5", "M": "1.5"}}, {Key: "b", Commune: "b", Cells: map[string]string{"F": "3", "M": "3"}},
		{Key: "c", Commune: "c", Cells: map[string]string{"F": "4"}}}
	ps := map[string]Source{"a": {SubmissionID: "1"}, "b": {SubmissionID: "2"}}
	ns := map[string]Source{"a": {SubmissionID: "1"}, "b": {SubmissionID: "3", AdjustmentResponse: true}, "c": {SubmissionID: "4"}}
	ch, communes := Diff(prev, next, ps, ns, map[string]bool{"M": true})
	if communes != 2 || len(ch) != 3 {
		t.Fatalf("thay đổi: %d %+v", communes, ch)
	}
	want := map[string]string{"b/F": ChangeAdjusted, "b/M": ChangeComputed, "c/F": ChangeNew}
	for _, c := range ch {
		if want[c.RowKey+"/"+c.Letter] != c.Kind {
			t.Fatalf("loại %s/%s = %s", c.RowKey, c.Letter, c.Kind)
		}
	}
	if !letterLess("Z", "AA") || letterLess("AB", "AA") {
		t.Fatal("thứ tự cột Excel")
	}
}

// FN-TI-02.01 AC: chỉ số dựng từ đúng các ô của lần tổng hợp; Toàn tỉnh = Σ khu vực = Σ xã (party/port.SumByRegion).
func TestFN_TI_02_01_ExtractMetrics(t *testing.T) {
	def := repport.FormDefinition{Code: "T01", Columns: []repport.FormColumn{
		{Letter: "D", Key: "kh_tinh_giao", Kind: repport.ColumnProvincial, DType: repport.DTypeNumber},
		{Letter: "F", Kind: repport.ColumnPerPeriod, DType: repport.DTypeNumber, PeriodBlock: "xg", PeriodSlot: 1},
		{Letter: "G", Kind: repport.ColumnPerPeriod, DType: repport.DTypeNumber, PeriodBlock: "xg", PeriodSlot: 2},
		{Letter: "M", Key: "luy_ke", Kind: repport.ColumnFormula, DType: repport.DTypeNumber},
	}}
	rows := []Row{{Commune: "x1", Cells: map[string]string{"D": "100", "F": "10.25", "G": "5", "M": "15.25"}},
		{Commune: "x2", Cells: map[string]string{"D": "50", "F": "4", "M": "4"}}, {Commune: "x3", Cells: map[string]string{"D": "x", "M": "1"}}}
	assign := map[string]partyport.CommuneRegion{"x1": {RegionCode: "KV1"}, "x2": {RegionCode: "KV2"}}
	got := map[string]decimal.Decimal{}
	for _, m := range ExtractMetrics("T01", "89", def, rows, assign) {
		got[m.Metric+"|"+m.Scope+"|"+m.Unit] = m.Value
	}
	check := func(k, v string) {
		if !got[k].Equal(decimal.RequireFromString(v)) {
			t.Fatalf("%s = %s, muốn %s", k, got[k], v)
		}
	}
	check("sown_area|province|89", "20.25")
	check("sown_area|region|KV1", "15.25")
	check("sown_area|region|", "1")
	check("sown_area|commune|x2", "4")
	check("sown_plan|province|89", "150")
	check("sown_week:1|province|89", "14.25")
	check("sown_week:2|province|89", "5")
	if !Ratio(decimal.NewFromInt(1), decimal.NewFromInt(3)).Equal(decimal.RequireFromString("33.3")) || !Yield(decimal.NewFromInt(10), decimal.Zero).IsZero() {
		t.Fatal("tỷ lệ, năng suất")
	}
}

// W11.05: vòng đời mở rộng (CNTS) — kỳ chốt: chưa gửi → Không nộp, đang yêu cầu điều chỉnh → Đã chốt – chưa điều
// chỉnh; Trồng trọt giữ "Đã chốt".
func TestExtendedLifecycle_W1105(t *testing.T) {
	now := time.Date(2026, 10, 10, 5, 0, 0, 0, time.UTC)
	if ExtendedLifecycle("trong_trot") || !ExtendedLifecycle("chan_nuoi_thuy_san") {
		t.Fatal("ExtendedLifecycle")
	}
	for _, c := range []struct {
		status   string
		extended bool
		want     string
	}{
		{TaskNotStarted, false, TaskClosed},
		{TaskDraft, false, TaskClosed},
		{TaskAdjustmentRequested, false, TaskClosed},
		{TaskNotStarted, true, TaskMissed},
		{TaskDraft, true, TaskMissed},
		{TaskAdjustmentRequested, true, TaskClosedUnadjusted},
		{TaskSubmitted, true, TaskClosed},
		{TaskConfirmed, true, TaskClosed},
	} {
		st := TaskState{Status: c.status, DueAt: now.Add(-time.Hour), PeriodClosed: true, Extended: c.extended}
		if got := st.Display(now); got != c.want || StatusLabel[got] == "" {
			t.Errorf("%s extended=%v: %s, muốn %s", c.status, c.extended, got, c.want)
		}
	}
}
