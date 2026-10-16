package domain

import (
	"errors"
	"testing"
	"time"
)

func TestDistanceM(t *testing.T) {
	a := GeoPoint{Lat: 10.0, Lon: 105.0}
	b := GeoPoint{Lat: 10.0045, Lon: 105.0} // ~500 m về phía bắc
	d := DistanceM(a, b)
	if d < 495 || d > 505 {
		t.Fatalf("khoảng cách = %.1f, muốn ~500 m", d)
	}
	if DistanceM(a, a) != 0 {
		t.Fatal("cùng điểm phải 0")
	}
}

func TestEvaluateCheckin(t *testing.T) {
	zone := time.FixedZone("ICT", 7*3600)
	day := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, zone)
	planned := &GeoPoint{Lat: 10.0, Lon: 105.0}
	acc := 80.0
	in := CheckinInput{Point: GeoPoint{Lat: 10.006, Lon: 105.0}, AccuracyM: &acc, CapturedAt: now.Add(-time.Minute)}
	ev, err := EvaluateCheckin(in, day, planned, now, zone, 500, 50)
	if err != nil {
		t.Fatal(err)
	}
	if ev.DistanceM == nil || !ev.Far || !ev.WeakAccuracy {
		t.Fatalf("muốn xa + GPS yếu, có %+v", ev)
	}
	near := in
	near.Point = GeoPoint{Lat: 10.001, Lon: 105.0}
	near.AccuracyM = nil
	if ev, _ := EvaluateCheckin(near, day, planned, now, zone, 500, 50); ev.Far || ev.WeakAccuracy {
		t.Fatalf("gần, không sai số: %+v", ev)
	}
	if ev, _ := EvaluateCheckin(near, day, nil, now, zone, 500, 50); ev.DistanceM != nil {
		t.Fatal("không có địa điểm → không tính khoảng cách")
	}
	cases := []struct {
		name string
		mod  func(*CheckinInput)
		now  time.Time
		want error
	}{
		{"ngoài Việt Nam", func(c *CheckinInput) { c.Point = GeoPoint{Lat: 1, Lon: 105} }, now, ErrPointRange},
		{"tương lai", func(c *CheckinInput) { c.CapturedAt = now.Add(time.Hour) }, now, ErrCheckinFuture},
		{"khác ngày", func(c *CheckinInput) { c.CapturedAt = now.Add(-24 * time.Hour) }, now, ErrCheckinWrongDay},
		{"gửi quá muộn", func(c *CheckinInput) {}, now.AddDate(0, 0, 9), ErrCheckinTooLate},
	}
	for _, tc := range cases {
		c := near
		tc.mod(&c)
		if _, err := EvaluateCheckin(c, day, planned, tc.now, zone, 500, 50); !errors.Is(err, tc.want) {
			t.Errorf("%s: lỗi %v, muốn %v", tc.name, err, tc.want)
		}
	}
}

func TestPlaceNormalizeAndFieldWork(t *testing.T) {
	p, err := GeoPoint{Lat: 10.123456789, Lon: 105.5}.Normalize()
	if err != nil || p.Source != PlaceSourceMap || p.Lat != 10.1234568 {
		t.Fatalf("chuẩn hóa: %+v %v", p, err)
	}
	if _, err := (GeoPoint{Lat: 10, Lon: 105, Source: "x"}).Normalize(); !errors.Is(err, ErrPlaceSource) {
		t.Fatal("nguồn sai phải lỗi")
	}
	zone := time.FixedZone("ICT", 7*3600)
	it := ScheduleItem{Day: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), Location: "Trại X"}
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, zone)
	if !MissedCheckin(it, false, now, zone) || MissedCheckin(it, true, now, zone) {
		t.Fatal("ngày đã qua, ngoài cơ quan, chưa check-in → cảnh báo")
	}
	if MissedCheckin(ScheduleItem{Day: it.Day}, false, now, zone) {
		t.Fatal("làm việc tại cơ quan không cần check-in")
	}
}
