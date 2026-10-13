package domain

import (
	"errors"
	"testing"
	"time"

	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
)

var hcm = time.FixedZone("ICT", 7*3600)

func d(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr[T any](v T) *T { return &v }

func TestPlanOf_BRDExamples(t *testing.T) {
	cases := []struct {
		name     string
		req      Request
		key      string
		from, to string
		label    string
	}{
		{"tuần 41", Request{Type: TypeWeek, Year: 2026, No: 41}, "2026-W41", "2026-10-07", "2026-10-13",
			"Tuần 41 năm 2026 (từ ngày 07/10/2026 đến ngày 13/10/2026)"},
		{"tháng 9", Request{Type: TypeMonth, Year: 2026, No: 9}, "2026-09", "2026-08-26", "2026-09-29",
			"Tháng 9 năm 2026 (số liệu đến ngày 29/09/2026)"},
		{"quý III", Request{Type: TypeQuarter, Year: 2026, No: 3}, "2026-Q3", "2026-07-01", "2026-09-29",
			"Quý III năm 2026 (số liệu đến ngày 29/09/2026)"},
		{"9 tháng", Request{Type: TypeQuarter, Year: 2026, No: 3, Scope: ScopeNine}, "2026-9M", "2025-12-31", "2026-09-29",
			"9 tháng đầu năm 2026 (số liệu đến ngày 29/09/2026)"},
		{"6 tháng", Request{Type: TypeQuarter, Year: 2026, No: 2, Scope: ScopeHalf}, "2026-H1", "2025-12-31", "2026-06-30",
			"6 tháng đầu năm 2026 (số liệu đến ngày 30/06/2026)"},
		{"năm", Request{Type: TypeYear, Year: 2026}, "2026", "2025-12-31", "2026-12-29", "Năm 2026 (số liệu đến ngày 29/12/2026)"},
		{"các CT", Request{Type: TypeProgram, Year: 2026, ToDate: ptr(d("2026-10-02"))}, "", "2025-12-31", "2026-09-29",
			"Đến ngày 02/10/2026 (số liệu đến ngày 29/09/2026)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := PlanOf(rc.DefaultRule, c.req, hcm)
			if err != nil {
				t.Fatal(err)
			}
			if p.Key != c.key || p.From.Format(time.DateOnly) != c.from || p.To.Format(time.DateOnly) != c.to || p.Name != c.label {
				t.Fatalf("got key=%q %s..%s %q", p.Key, p.From.Format(time.DateOnly), p.To.Format(time.DateOnly), p.Name)
			}
			if !p.Cutoff.Equal(p.To) {
				t.Fatalf("ngày chốt = ngày kết thúc: %v", p.Cutoff)
			}
		})
	}
}

func TestPlanOf_WeekDeadlines(t *testing.T) {
	p, err := PlanOf(rc.DefaultRule, Request{Type: TypeWeek, Year: 2026, No: 41}, hcm)
	if err != nil {
		t.Fatal(err)
	}
	// Hạn nộp Thứ Tư kế tiếp 10:00, hạn xuất 16:00 (giờ VN).
	if got := p.DueAt.In(hcm).Format("2006-01-02 15:04 Mon"); got != "2026-10-14 10:00 Wed" {
		t.Fatalf("due %s", got)
	}
	if got := p.ExportDueAt.In(hcm).Format("15:04"); got != "16:00" {
		t.Fatalf("export %s", got)
	}
	if p.FormsKind != "week" || p.FormsKey != "2026-W41" || p.DueRequired {
		t.Fatalf("%+v", p)
	}
}

func TestPlanOf_Invalid(t *testing.T) {
	bad := []Request{
		{Type: TypeWeek, Year: 2026, No: 53}, // 2026 có 52 tuần báo cáo
		{Type: TypeMonth, Year: 2026, No: 13},
		{Type: TypeQuarter, Year: 2026, No: 1, Scope: ScopeHalf},
		{Type: TypeQuarter, Year: 2026, No: 3, Scope: "x"},
		{Type: TypeProgram, Year: 2026},
		{Type: TypeProgram, Year: 2027, ToDate: ptr(d("2027-01-02"))}, // chưa hết tuần nào của 2027
		{Type: TypeAdhoc, Name: "ab"},
		{Type: "daily", Year: 2026},
	}
	for i, r := range bad {
		var e ErrInput
		if _, err := PlanOf(rc.DefaultRule, r, hcm); !errors.As(err, &e) {
			t.Fatalf("#%d: muốn ErrInput, có %v", i, err)
		}
	}
	if _, err := PlanOf(rc.LegacyRule, Request{Type: TypeWeek, Year: 2026, No: 1}, hcm); err == nil {
		t.Fatal("quy tắc v1 phải bị từ chối")
	}
}

func TestPlan_Apply(t *testing.T) {
	p, _ := PlanOf(rc.DefaultRule, Request{Type: TypeWeek, Year: 2026, No: 41}, hcm)
	if _, err := p.Apply(Overrides{Cutoff: ptr(d("2026-10-12"))}, hcm); err == nil {
		t.Fatal("ngày chốt trước ngày kết thúc phải lỗi")
	}
	due := time.Date(2026, 10, 15, 10, 0, 0, 0, hcm)
	q, err := p.Apply(Overrides{DueAt: &due}, hcm)
	if err != nil || !q.DueAt.Equal(due) || q.ExportDueAt.Before(q.DueAt) {
		t.Fatalf("%v %+v", err, q)
	}
	if _, err := p.Apply(Overrides{DueAt: &due, ExportDueAt: ptr(due.Add(-time.Hour))}, hcm); err == nil {
		t.Fatal("hạn xuất trước hạn nộp phải lỗi")
	}
	ad, _ := PlanOf(rc.DefaultRule, Request{Type: TypeAdhoc, Name: "Báo cáo hạn mặn", From: ptr(d("2026-10-01")), To: ptr(d("2026-10-10"))}, hcm)
	if _, err := ad.Apply(Overrides{}, hcm); err == nil {
		t.Fatal("phát sinh phải nhập hạn nộp")
	}
	ct, _ := PlanOf(rc.DefaultRule, Request{Type: TypeProgram, Year: 2026, ToDate: ptr(d("2026-10-02"))}, hcm)
	if _, err := ct.Apply(Overrides{Cutoff: ptr(d("2026-09-01")), DueAt: ptr(time.Date(2026, 10, 5, 9, 0, 0, 0, hcm))}, hcm); err != nil {
		t.Fatalf("Mẫu 5 được chốt trước ngày kết thúc: %v", err)
	}
}

func TestPeriodStateOf(t *testing.T) {
	cases := []struct {
		term string
		f    Facts
		want string
	}{
		{"", Facts{Forms: 3}, PeriodOpened},
		{"", Facts{Forms: 3, Overdue: true}, PeriodLate},
		{"", Facts{Forms: 3, FormsSent: 1, AnySent: true}, PeriodCollecting},
		{"", Facts{Forms: 3, FormsSent: 3, AnySent: true, Sources: 2, SourcesClosed: 1}, PeriodCollecting},
		{"", Facts{Forms: 3, FormsSent: 3, AnySent: true, Sources: 2, SourcesClosed: 2}, PeriodComplete},
		{"", Facts{Forms: 3, FormsSent: 3, AnySent: true, Overdue: true}, PeriodComplete},
		{"", Facts{Forms: 3, FormsSent: 2, AnySent: true, Overdue: true}, PeriodLate}, // yêu cầu bổ sung sau hạn
		{PeriodClosed, Facts{Forms: 3}, PeriodClosed},
		{PeriodCancelled, Facts{Forms: 3, FormsSent: 3}, PeriodCancelled},
	}
	for i, c := range cases {
		if got := PeriodStateOf(c.term, c.f); got != c.want {
			t.Fatalf("#%d: %s, muốn %s", i, got, c.want)
		}
	}
}

func TestCanClose(t *testing.T) {
	due := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	if dc, err := CanClose(PeriodComplete, due, due, 3, ""); err != nil || dc.Forced {
		t.Fatal(err)
	}
	if _, err := CanClose(PeriodLate, due, due.Add(48*time.Hour), 3, "Phòng TT chưa gửi phiếu"); !errors.Is(err, ErrNotClosable) {
		t.Fatal("chưa đủ N ngày", err)
	}
	if _, err := CanClose(PeriodLate, due, due.Add(72*time.Hour), 3, "ngắn"); !errors.Is(err, ErrReasonRequired) {
		t.Fatal(err)
	}
	if dc, err := CanClose(PeriodLate, due, due.Add(72*time.Hour), 3, "Phòng TT chưa gửi phiếu"); err != nil || !dc.Forced {
		t.Fatal(err)
	}
	for _, s := range []string{PeriodOpened, PeriodCollecting} {
		if _, err := CanClose(s, due, due.Add(240*time.Hour), 3, "lý do đủ dài ở đây"); !errors.Is(err, ErrNotClosable) {
			t.Fatal(s, err)
		}
	}
	if _, err := CanClose(PeriodClosed, due, due, 3, ""); !errors.Is(err, ErrTerminal) {
		t.Fatal(err)
	}
}

func TestCanCancel(t *testing.T) {
	for _, s := range []string{PeriodOpened, PeriodCollecting, PeriodLate, PeriodComplete} {
		if _, err := CanCancel(s, false, "Mở nhầm loại báo cáo"); err != nil {
			t.Fatal(s, err)
		}
	}
	if _, err := CanCancel(PeriodComplete, true, "Mở nhầm loại báo cáo"); !errors.Is(err, ErrHasFinal) {
		t.Fatal(err)
	}
	if _, err := CanCancel(PeriodOpened, false, "nhầm"); !errors.Is(err, ErrReasonRequired) {
		t.Fatal(err)
	}
	if _, err := CanCancel(PeriodClosed, false, "Mở nhầm loại báo cáo"); !errors.Is(err, ErrTerminal) {
		t.Fatal(err)
	}
}

func TestFormStateOf(t *testing.T) {
	cases := []struct {
		f    FormFacts
		want string
		sup  bool
	}{
		{FormFacts{Max: 3}, FormNotStarted, false},
		{FormFacts{Max: 3, Overdue: true}, FormLate, false},
		{FormFacts{Max: 3, Versions: 1}, FormSubmitted, true},
		{FormFacts{Max: 3, Versions: 2, Decision: "returned"}, FormSupplement, false},
		{FormFacts{Max: 3, Versions: 3}, FormSubmitted, false}, // lần 3 tự khóa: chỉ xác nhận
		{FormFacts{Max: 0, Versions: 9}, FormSubmitted, true},  // không giới hạn
		{FormFacts{Max: 3, Versions: 1, Decision: "accepted"}, FormConfirmed, false},
		{FormFacts{Max: 3, Terminal: PeriodClosed}, FormNotSent, false},
		{FormFacts{Max: 3, Versions: 2, Decision: "returned", Terminal: PeriodClosed}, FormLocked, false},
		{FormFacts{Max: 3, Versions: 2, Decision: "accepted", Terminal: PeriodClosed}, FormConfirmed, false},
		{FormFacts{Max: 3, Versions: 2, Terminal: PeriodCancelled}, FormWithdrawn, false},
	}
	for i, c := range cases {
		v := FormStateOf(c.f)
		if v.State != c.want || v.CanRequestSupplement != c.sup {
			t.Fatalf("#%d: %+v", i, v)
		}
	}
	if v := FormStateOf(FormFacts{Max: 3, Versions: 3}); !v.LimitReached || !v.CanConfirm || !v.CountsAsSent {
		t.Fatalf("%+v", v)
	}
}

func TestNextReport(t *testing.T) {
	steps := []struct{ ev, want string }{
		{ReportCompose, ReportDrafting}, {ReportEdit, ReportDrafting}, {ReportExport, ReportExported},
		{ReportSourceChanges, ReportSourceChanged}, {ReportRefresh, ReportDrafting}, {ReportExport, ReportExported},
		{ReportMarkFinal, ReportFinal},
	}
	s := ReportNotStarted
	for _, st := range steps {
		n, err := NextReport(s, st.ev, PeriodClosed, false)
		if err != nil || n != st.want {
			t.Fatalf("%s --%s--> %s (%v), muốn %s", s, st.ev, n, err, st.want)
		}
		s = n
	}
	if _, err := NextReport(ReportFinal, ReportEdit, PeriodClosed, false); !errors.Is(err, ErrReportTransition) {
		t.Fatal("Bản cuối là trạng thái cuối")
	}
	if _, err := NextReport(ReportExported, ReportMarkFinal, PeriodComplete, true); !errors.Is(err, ErrProvisional) {
		t.Fatal(err)
	}
	if _, err := NextReport(ReportNotStarted, ReportCompose, PeriodCancelled, false); err == nil {
		t.Fatal("kỳ đã hủy không soạn báo cáo")
	}
	if _, err := NextReport(ReportExported, ReportExport, PeriodClosed, false); err == nil {
		t.Fatal("xuất lại phải qua Đang soạn")
	}
}
