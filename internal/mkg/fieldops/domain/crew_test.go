package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func dp(s string) *time.Time { d := day(s); return &d }

// proto FM-01: vụ ứng với lệnh gom = ngày gặt gần ngày bắt đầu khung nhất (tất định);
// cuộn trước ngày gặt bị từ chối; ngày nghiệp vụ theo giờ Việt Nam.
func TestSelectSeasonAndBaleDateFM01(t *testing.T) {
	s1, s2 := ids.New(), ids.New()
	seasons := []port.SeasonHarvest{
		{SeasonRef: &s1, HarvestDate: day("2026-03-01"), Kind: port.HarvestActual},
		{SeasonRef: &s2, HarvestDate: day("2026-10-05"), Kind: port.HarvestExpected},
	}
	if got := SelectSeason(seasons, day("2026-10-01")); got == nil || *got.SeasonRef != s2 {
		t.Fatalf("khung 01/10 → vụ gặt 05/10: %+v", got)
	}
	if got := SelectSeason(seasons, day("2026-04-01")); got == nil || *got.SeasonRef != s1 {
		t.Fatalf("khung 01/04 → vụ gặt 01/03: %+v", got)
	}
	// Đảo thứ tự đầu vào không đổi kết quả.
	rev := []port.SeasonHarvest{seasons[1], seasons[0]}
	if a, b := SelectSeason(seasons, day("2026-10-01")), SelectSeason(rev, day("2026-10-01")); *a.SeasonRef != *b.SeasonRef {
		t.Fatal("chọn vụ phải tất định")
	}
	if SelectSeason(nil, day("2026-10-01")) != nil {
		t.Fatal("không có vụ → nil (chưa biết ngày gặt)")
	}
	if err := CheckBaleDate(day("2026-10-04"), day("2026-10-05")); !errors.Is(err, ErrBeforeHarvest) {
		t.Fatalf("cuộn 04/10 trước gặt 05/10: %v", err)
	}
	if err := CheckBaleDate(day("2026-10-05"), day("2026-10-05")); err != nil {
		t.Fatalf("cuộn đúng ngày gặt được phép: %v", err)
	}
	// 2026-10-04T18:30Z là 05/10 ở Việt Nam.
	if got := Day(time.Date(2026, 10, 4, 18, 30, 0, 0, time.UTC)); !got.Equal(day("2026-10-05")) {
		t.Fatalf("ngày nghiệp vụ giờ VN: %v", got)
	}
}

// proto FM-02: hạn = ngày gặt + N (mặc định 3); quá hạn chưa cuộn → overdue (không chặn);
// cuộn sau hạn → baled_late; chưa biết ngày gặt → harvest_unknown (không im lặng).
func TestBalingStateFM02(t *testing.T) {
	today := day("2026-10-10")
	cases := []struct {
		name    string
		harvest *time.Time
		baled   *time.Time
		want    string
		days    *int
	}{
		{"chưa biết ngày gặt", nil, nil, BalingHarvestUnknown, nil},
		{"chưa gặt", dp("2026-10-12"), nil, BalingNotHarvested, nil},
		{"trong hạn", dp("2026-10-08"), nil, BalingPending, ip(2)},
		{"đúng hạn cuối", dp("2026-10-07"), nil, BalingPending, ip(3)},
		{"quá hạn", dp("2026-10-06"), nil, BalingOverdue, ip(4)},
		{"cuộn trong hạn", dp("2026-10-01"), dp("2026-10-04"), BalingOnTime, ip(3)},
		{"cuộn trễ", dp("2026-10-01"), dp("2026-10-05"), BalingLate, ip(4)},
	}
	for _, c := range cases {
		b := BalingState(c.harvest, c.baled, today, DefaultBalingDeadlineDays)
		if b.Status != c.want {
			t.Errorf("%s: %s, muốn %s", c.name, b.Status, c.want)
		}
		if (c.days == nil) != (b.DaysOnField == nil) || (c.days != nil && *c.days != *b.DaysOnField) {
			t.Errorf("%s: số ngày nằm ruộng %v, muốn %v", c.name, b.DaysOnField, c.days)
		}
		if b.Flagged() != (c.want == BalingOverdue || c.want == BalingLate) {
			t.Errorf("%s: cờ FM-02 sai", c.name)
		}
	}
	if b := BalingState(dp("2026-10-06"), nil, today, 7); b.Status != BalingPending {
		t.Fatalf("N = 7 (phiên bản tham số khác) → còn hạn: %s", b.Status)
	}
}

func ip(n int) *int { return &n }

// proto FM-06: mỗi ngày Σ tải + việc mới ≤ năng lực/ngày; báo ngày vượt đầu tiên.
func TestCheckCapacityFM06(t *testing.T) {
	capacity := decimal.NewFromInt(1000)
	w := Workload{From: day("2026-10-01"), To: day("2026-10-03"), ExpectedQty: decimal.NewFromInt(2400)}
	if !w.DailyLoad().Equal(decimal.NewFromInt(800)) || w.Days() != 3 {
		t.Fatalf("tải/ngày: %s × %d", w.DailyLoad(), w.Days())
	}
	if c := CheckCapacity(nil, w, capacity); !c.Fits || !c.PeakLoad.Equal(decimal.NewFromInt(800)) {
		t.Fatalf("tổ rỗng: %+v", c)
	}
	books := []Booking{{OrderID: ids.New(), From: day("2026-10-03"), To: day("2026-10-05"), DailyLoad: decimal.NewFromInt(200)}}
	if c := CheckCapacity(books, w, capacity); !c.Fits || !c.PeakLoad.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("vừa khít ngày 03/10: %+v", c)
	}
	books = append(books, Booking{OrderID: ids.New(), From: day("2026-10-02"), To: day("2026-10-02"), DailyLoad: decimal.NewFromInt(201)})
	c := CheckCapacity(books, w, capacity)
	if c.Fits || c.OverDay == nil || !c.OverDay.Equal(day("2026-10-02")) || !c.OverLoad.Equal(decimal.NewFromInt(1001)) {
		t.Fatalf("vượt ngày 02/10: %+v", c)
	}
}

// proto FM-08: chỉ tổ vừa năng lực; gần nhất (xã phục vụ) trước, rồi ít tải nhất, hòa theo mã;
// cùng dữ liệu → cùng thứ tự bất kể thứ tự đầu vào.
func TestSuggestDeterministicFM08(t *testing.T) {
	w := &Workload{From: day("2026-10-01"), To: day("2026-10-01"), ExpectedQty: decimal.NewFromInt(900)}
	a, b, c, d, e := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cands := []Candidate{
		{CrewID: a, Code: "A", CapacityPerDay: decimal.NewFromInt(800), ServiceCommunes: []string{"31117"}, Workload: w},            // vượt
		{CrewID: b, Code: "B", CapacityPerDay: decimal.NewFromInt(3000), ServiceCommunes: []string{"31117"}, Workload: w},           // gần, 0,3
		{CrewID: c, Code: "C", CapacityPerDay: decimal.NewFromInt(9000), ServiceCommunes: []string{"99999"}, Workload: w},           // xa, 0,1
		{CrewID: d, Code: "D", CapacityPerDay: decimal.NewFromInt(1800), ServiceCommunes: []string{"31117"}, Workload: w},           // gần, 0,5
		{CrewID: e, Code: "E", CapacityPerDay: decimal.NewFromInt(10), ServiceCommunes: []string{"31117"}, Workload: nil},           // đơn vị ha
		{CrewID: uuid.New(), Code: "B2", CapacityPerDay: decimal.NewFromInt(3000), ServiceCommunes: []string{"31117"}, Workload: w}, // hòa B
	}
	fit, unfit := Suggest(cands, "31117")
	codes := func(xs []Suggestion) (out []string) {
		for _, x := range xs {
			out = append(out, x.Code)
		}
		return
	}
	if got := codes(fit); len(got) != 4 || got[0] != "B" || got[1] != "B2" || got[2] != "D" || got[3] != "C" {
		t.Fatalf("thứ tự gợi ý: %v", got)
	}
	if fit[0].Rank != 1 || !fit[0].Local || fit[3].Local {
		t.Fatalf("hạng / gần: %+v", fit)
	}
	if got := codes(unfit); len(got) != 2 || got[0] != "A" || got[1] != "E" || unfit[0].Unfit != UnfitCapacity || unfit[1].Unfit != UnfitUnit {
		t.Fatalf("không vừa: %+v", unfit)
	}
	rev := make([]Candidate, len(cands))
	for i := range cands {
		rev[i] = cands[len(cands)-1-i]
	}
	fit2, _ := Suggest(rev, "31117")
	if got := codes(fit2); got[0] != "B" || got[1] != "B2" || got[2] != "D" || got[3] != "C" {
		t.Fatalf("đảo đầu vào đổi thứ tự: %v", got)
	}
}

func TestNormalizeCrew(t *testing.T) {
	m := ids.New()
	c, err := NormalizeCrew(CrewSpec{Code: " to-01 ", Name: "Tổ 1", MemberRefs: []uuid.UUID{m, m}, CapacityUnit: UnitBales,
		CapacityPerDay: decimal.NewFromInt(1200), ServiceCommunes: []string{"31117", "31117"}})
	if err != nil || c.Code != "TO-01" || len(c.MemberRefs) != 1 || len(c.ServiceCommunes) != 1 {
		t.Fatalf("chuẩn hóa: %+v %v", c, err)
	}
	bad := []CrewSpec{
		{Code: "x", Name: "Tổ", MemberRefs: []uuid.UUID{m}, CapacityUnit: UnitBales, CapacityPerDay: decimal.NewFromInt(1)},
		{Code: "TO-1", Name: "Tổ", MemberRefs: nil, CapacityUnit: UnitBales, CapacityPerDay: decimal.NewFromInt(1)},
		{Code: "TO-1", Name: "Tổ", MemberRefs: []uuid.UUID{m}, CapacityUnit: "tan", CapacityPerDay: decimal.NewFromInt(1)},
		{Code: "TO-1", Name: "Tổ", MemberRefs: []uuid.UUID{m}, CapacityUnit: UnitBales, CapacityPerDay: decimal.RequireFromString("10.5")},
		{Code: "TO-1", Name: "Tổ", MemberRefs: []uuid.UUID{m}, CapacityUnit: UnitHa, CapacityPerDay: decimal.Zero},
		{Code: "TO-1", Name: "Tổ", MemberRefs: []uuid.UUID{m}, CapacityUnit: UnitHa, CapacityPerDay: decimal.NewFromInt(4), ServiceCommunes: []string{"x"}},
	}
	for i, b := range bad {
		if _, err := NormalizeCrew(b); err == nil {
			t.Errorf("ca %d phải lỗi", i)
		}
	}
}
