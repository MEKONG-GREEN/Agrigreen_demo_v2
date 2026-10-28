package clock

import (
	"testing"
	"time"
)

func TestBusinessDateUsesVietnamTime(t *testing.T) {
	cases := []struct {
		utc  string
		want string
	}{
		{"2026-12-02T16:59:59Z", "2026-12-02"},
		{"2026-12-02T17:00:00Z", "2026-12-03"}, // 00:00 giờ Việt Nam
		{"2026-12-03T09:40:05Z", "2026-12-03"},
	}
	for _, c := range cases {
		ts, err := time.Parse(time.RFC3339, c.utc)
		if err != nil {
			t.Fatal(err)
		}
		if got := BusinessDate(ts).String(); got != c.want {
			t.Errorf("BusinessDate(%s) = %s, muốn %s", c.utc, got, c.want)
		}
	}
}

func TestBusinessDayIsVietnamDateAtUTCMidnight(t *testing.T) {
	for _, c := range []struct{ utc, want string }{
		{"2026-10-25T16:59:59Z", "2026-10-25T00:00:00Z"}, // Chủ Nhật 23:59:59 VN
		{"2026-10-25T17:00:00Z", "2026-10-26T00:00:00Z"}, // Thứ Hai 00:00 VN
		{"2026-10-25T17:30:00Z", "2026-10-26T00:00:00Z"}, // Thứ Hai 00:30 VN
		{"2026-10-26T16:59:59Z", "2026-10-26T00:00:00Z"},
	} {
		ts, _ := time.Parse(time.RFC3339, c.utc)
		got := BusinessDay(ts)
		if got.Format(time.RFC3339) != c.want || got.Location() != time.UTC {
			t.Errorf("BusinessDay(%s) = %s, muốn %s (UTC)", c.utc, got.Format(time.RFC3339), c.want)
		}
	}
}

func TestStartOfBusinessDate(t *testing.T) {
	got := StartOfBusinessDate(Date{2026, time.December, 3})
	want := time.Date(2026, time.December, 2, 17, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSystemClockIsUTC(t *testing.T) {
	if loc := (System{}).Now().Location(); loc != time.UTC {
		t.Fatalf("System.Now() phải là UTC, nhận %s", loc)
	}
}

func TestFixedAdvance(t *testing.T) {
	f := NewFixed(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f.Advance(90 * time.Minute)
	if f.Now().Hour() != 1 || f.Now().Minute() != 30 {
		t.Fatalf("Advance sai: %s", f.Now())
	}
}
