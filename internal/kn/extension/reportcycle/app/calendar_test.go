package app

import (
	"context"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// fakeParams: khóa → (ngày hiệu lực "YYYY-MM-DD" giờ VN, Text).
type fakeParams map[string][2]string

func (f fakeParams) At(_ context.Context, key string, at time.Time) (params.ParamValue, error) {
	v, ok := f[key]
	if !ok || at.Format(time.DateOnly) < v[0] {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Text: v[1]}, nil
}

// D8: lịch tuần theo phòng — tham số theo phòng → nạp sẵn → tham số chung; lô CNTS Thứ Ba–Thứ Hai cả trước 04/11.
func TestCalendarAt_PerDepartment(t *testing.T) {
	ctx := context.Background()
	s := &Service{Params: fakeParams{port.ParamPeriodRule: {"2026-11-04", domain.DefaultRule.Tag()}}}
	for _, c := range []struct {
		dept, day, ref string
	}{
		{domain.DeptChanNuoiThuySan, "2026-10-13", "chan_nuoi_thuy_san:v3"},
		{domain.DeptChanNuoiThuySan, "2026-11-10", "chan_nuoi_thuy_san:v3"},
		{domain.DeptKeHoachTaiChinh, "2026-01-07", "ke_hoach_tai_chinh:v2"}, // W11.64: KHTC nạp sẵn Thứ Tư–Thứ Ba mọi ngày
		{domain.DeptKeHoachTaiChinh, "2026-11-03", "ke_hoach_tai_chinh:v2"},
		{domain.DeptKeHoachTaiChinh, "2026-11-04", "ke_hoach_tai_chinh:v2"},
		{domain.DeptTrongTrot, "2026-11-04", "trong_trot:v2"},
	} {
		d, _ := domain.ParseDate(c.day)
		if got := s.CalendarAt(ctx, c.dept, d); got.Ref() != c.ref {
			t.Errorf("%s %s: %s", c.dept, c.day, got.Ref())
		}
	}
	d, _ := domain.ParseDate("2026-10-13")
	cal := s.cntsCalendar(ctx, d)
	if cal.Rule.WeekStart != time.Tuesday || cal.Rule.Tag() != domain.CNTSRule.Tag() {
		t.Fatalf("lịch CNTS: %+v", cal)
	}
	// Tham số theo phòng đè lên lịch nạp sẵn; tham số theo phòng là v1 → lô CNTS vẫn dùng lịch CNTS (cần kỳ v2).
	s.Params = fakeParams{port.ParamPeriodRule + ":" + domain.DeptChanNuoiThuySan: {"2026-01-01", "kn.period_rule/v2;week_start=mon;cal=chan_nuoi_thuy_san:v4"}}
	if got := s.CalendarAt(ctx, domain.DeptChanNuoiThuySan, d); got.Ref() != "chan_nuoi_thuy_san:v4" || got.Source != domain.CalendarSourceDepartmentParam {
		t.Fatalf("tham số theo phòng: %+v", got)
	}
	s.Params = fakeParams{port.ParamPeriodRule + ":" + domain.DeptChanNuoiThuySan: {"2026-01-01", domain.LegacyRule.Tag()}}
	if got := s.cntsCalendar(ctx, d); got.Ref() != "chan_nuoi_thuy_san:v3" {
		t.Fatalf("v1 theo phòng → lịch CNTS: %+v", got)
	}
	s.Params = nil
	if got := s.CalendarAt(ctx, domain.DeptTrongTrot, d); got.Ref() != "trong_trot:v1" || got.Source != domain.CalendarSourceLegacy {
		t.Fatalf("không có tham số: %+v", got)
	}
}

// BM-04 lô CNTS chia Thứ Ba–Thứ Hai (BRD: tuần 39 = 22/09–28/09/2026, hạn 12:00 Thứ Ba); BM-05 giữ tuần làm việc.
func TestCNTSSeries_UsesCNTSCalendar(t *testing.T) {
	d, _ := domain.ParseDate("2026-09-24")
	s := &Service{}
	cal := s.cntsCalendar(context.Background(), d)
	bm04, _ := domain.CNTSFormByCode(domain.CNTSBM04)
	ps, err := domain.SplitCNTS(domain.CNTSSeriesInput{Form: bm04, From: d, To: d, Rule: cal.Rule, Loc: time.FixedZone("ICT", 7*3600)})
	if err != nil || len(ps) != 1 || ps[0].Key != "2026-W39" || ps[0].From != domain.Date(2026, 9, 22) || ps[0].To != domain.Date(2026, 9, 28) ||
		ps[0].DueAt != time.Date(2026, 9, 29, 5, 0, 0, 0, time.UTC) || ps[0].Rule != domain.CNTSRule.Tag() {
		t.Fatalf("BM-04: %+v %v", ps, err)
	}
	bm05, _ := domain.CNTSFormByCode(domain.CNTSBM05I)
	ps, err = domain.SplitCNTS(domain.CNTSSeriesInput{Form: bm05, From: d, To: d, Rule: cal.Rule, Loc: time.FixedZone("ICT", 7*3600)})
	if err != nil || len(ps) != 1 || ps[0].Rule != domain.CNTSRuleWorkWeek || ps[0].From != domain.Date(2026, 9, 21) || !bm05.IsWorkWeek() {
		t.Fatalf("BM-05: %+v %v", ps, err)
	}
}
