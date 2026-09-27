package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// 00:30 giờ Việt Nam = 17:30 UTC hôm trước (mục 8.2).
func TestDailyNextInBusinessZone(t *testing.T) {
	d := Daily{Hour: 0, Minute: 30}
	cases := []struct{ after, want string }{
		{"2026-09-26T10:00:00Z", "2026-09-26T17:30:00Z"}, // 17:00 VN → 00:30 VN hôm sau
		{"2026-09-26T17:30:00Z", "2026-09-27T17:30:00Z"}, // đúng mốc → lần kế tiếp
		{"2026-09-26T17:29:59Z", "2026-09-26T17:30:00Z"},
		{"2026-12-31T18:00:00Z", "2027-01-01T17:30:00Z"}, // qua năm theo giờ VN
	}
	for _, c := range cases {
		after, _ := time.Parse(time.RFC3339, c.after)
		if got := d.Next(after).Format(time.RFC3339); got != c.want {
			t.Errorf("Next(%s) = %s, mong %s", c.after, got, c.want)
		}
	}
}

func TestEveryNextAligned(t *testing.T) {
	after, _ := time.Parse(time.RFC3339, "2026-09-26T10:17:05Z")
	if got := (Every{Interval: time.Hour}).Next(after).Format(time.RFC3339); got != "2026-09-26T11:00:00Z" {
		t.Fatalf("got %s", got)
	}
}

func TestSchedulerRunsAndStops(t *testing.T) {
	var ok, bad atomic.Int32
	s := &Scheduler{Clock: clock.System{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Jobs: []Job{
		{Name: "tick", Spec: Every{Interval: 20 * time.Millisecond}, RunOnStart: true, Run: func(context.Context) error { ok.Add(1); return nil }},
		{Name: "boom", Spec: Every{Interval: 20 * time.Millisecond}, Run: func(context.Context) error { bad.Add(1); panic("x") }},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := s.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run phải trả khi ctx hết hạn: %v", err)
	}
	if ok.Load() < 3 {
		t.Fatalf("job tick chạy %d lần", ok.Load())
	}
	if bad.Load() < 2 {
		t.Fatalf("panic của job không được làm dừng lịch: %d", bad.Load())
	}
}
