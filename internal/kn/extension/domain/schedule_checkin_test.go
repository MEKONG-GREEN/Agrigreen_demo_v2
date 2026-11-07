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

func TestVisitPhotoType(t *testing.T) {
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	if ct, err := VisitPhotoType(jpeg); err != nil || ct != "image/jpeg" {
		t.Fatalf("JPEG: %q %v", ct, err)
	}
	for name, tc := range map[string]struct {
		b    []byte
		want error
	}{
		"thiếu ảnh (AC-09)": {nil, ErrVisitPhotoRequired},
		"quá 500 KB":        {append(jpeg, make([]byte, MaxVisitPhotoBytes)...), ErrVisitPhotoSize},
		"không phải ảnh":    {[]byte("%PDF-1.7 không phải ảnh"), ErrVisitPhotoType},
	} {
		if _, err := VisitPhotoType(tc.b); !errors.Is(err, tc.want) {
			t.Errorf("%s: muốn %v, có %v", name, tc.want, err)
		}
	}
}

func TestCheckoutRules(t *testing.T) {
	zone := time.FixedZone("ICT", 7*3600)
	in := time.Date(2026, 10, 6, 8, 0, 0, 0, zone)
	out := time.Date(2026, 10, 6, 11, 30, 0, 0, zone)
	// AC-12: check-in 08:00, check-out 11:30 → 3 giờ 30 phút.
	if m := StayMinutes(in, out); m != 210 {
		t.Fatalf("thời gian ở địa điểm = %d phút, muốn 210", m)
	}
	if err := CheckCheckoutAfter(in, out); err != nil {
		t.Fatal(err)
	}
	if err := CheckCheckoutAfter(out, in); !errors.Is(err, ErrCheckoutBeforeIn) {
		t.Fatalf("check-out trước check-in: %v", err)
	}
	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	// AC-13: đồng bộ 07:10 ngày 07/10 → trễ; 23:59 ngày 06/10 → không trễ.
	if !SyncedLate(day, time.Date(2026, 10, 7, 7, 10, 0, 0, zone), zone) {
		t.Fatal("đồng bộ ngày hôm sau phải là Đồng bộ trễ")
	}
	if SyncedLate(day, time.Date(2026, 10, 6, 23, 59, 0, 0, zone), zone) {
		t.Fatal("đồng bộ trong ngày công tác không trễ")
	}
}

func TestNoCheckout(t *testing.T) {
	zone := time.FixedZone("ICT", 7*3600)
	day := time.Date(2027, 10, 6, 0, 0, 0, 0, time.UTC)
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := NoCheckoutAt(day, zone); !got.Equal(at("2027-10-06T23:59:00+07:00")) {
		t.Fatalf("mốc 23:59 giờ Việt Nam: %v", got)
	}
	cases := []struct {
		name    string
		in, out bool
		now     string
		want    bool
	}{
		{"AC-10: 23:58 chưa tới mốc", true, false, "2027-10-06T23:58:59+07:00", false},
		{"AC-10: đúng 23:59", true, false, "2027-10-06T23:59:00+07:00", true},
		{"AC-10: sáng hôm sau", true, false, "2027-10-07T07:00:00+07:00", true},
		{"BR-08/AC-13: check-out đồng bộ trễ gỡ nhãn", true, true, "2027-10-07T07:10:00+07:00", false},
		{"chưa check-in không gắn nhãn", false, false, "2027-10-07T07:00:00+07:00", false},
		{"16:59 UTC (= 23:59 VN) không lệch múi giờ", true, false, "2027-10-06T16:59:00Z", true},
	}
	for _, c := range cases {
		if got := NoCheckout(day, c.in, c.out, at(c.now), zone); got != c.want {
			t.Errorf("%s: %v, muốn %v", c.name, got, c.want)
		}
	}
}
