package domain

import (
	"testing"
	"time"
)

// GIS US-SEASON-01 / FN-13 B2: giai đoạn nâu → xanh → vàng → đỏ → xám theo ngày của vụ.
func TestSeasonStageFollowsSeasonDates(t *testing.T) {
	sow := Day(2026, time.September, 10)
	open := Day(2026, time.December, 9)
	exp := Day(2026, time.December, 14)
	cases := []struct {
		status string
		day    time.Time
		want   string
	}{
		{"open", Day(2026, time.September, 5), StagePrep},
		{"open", sow, StageGrowing},
		{"open", Day(2026, time.December, 8), StageGrowing},
		{"open", open, StageHarvestOK},
		{"open", exp, StageHarvesting},
		{"open", Day(2026, time.December, 20), StageHarvesting},
		{"completed", Day(2026, time.December, 20), StageDone},
		{"cancelled", Day(2026, time.December, 20), ""},
	}
	for _, c := range cases {
		if got := SeasonStage(c.status, sow, open, exp, c.day); got != c.want {
			t.Errorf("%s @ %s: muốn %q, nhận %q", c.status, FormatDay(c.day), c.want, got)
		}
	}
	for _, s := range Stages {
		if StageLabel(s) == "" {
			t.Errorf("thiếu nhãn giai đoạn %s", s)
		}
	}
}

// GIS US-YIELD-01 / FN-14: vùng chín rộ = thu hoạch dự kiến trong 1–3 ngày tới, chỉ vụ đang mở.
func TestRipeningWindowOneToThreeDays(t *testing.T) {
	exp := Day(2026, time.December, 14)
	for _, c := range []struct {
		day  time.Time
		want bool
	}{
		{Day(2026, time.December, 10), false}, // còn 4 ngày
		{Day(2026, time.December, 11), true},  // 3 ngày
		{Day(2026, time.December, 13), true},  // 1 ngày
		{exp, false},                          // hôm nay: đã chín rộ, không còn "sắp tới"
	} {
		if got := Ripening("open", exp, c.day); got != c.want {
			t.Errorf("@ %s: muốn %v, nhận %v", FormatDay(c.day), c.want, got)
		}
	}
	if Ripening("completed", exp, Day(2026, time.December, 12)) {
		t.Error("vụ đã đóng không được cảnh báo chín rộ")
	}
}
