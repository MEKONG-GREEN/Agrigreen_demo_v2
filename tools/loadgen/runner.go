package main

import (
	"context"
	"sync"
	"time"
)

// Scenario là một loại tải chạy với tốc độ cố định (vòng hở: lượt đến theo đồng hồ, không đợi lượt trước).
type Scenario struct {
	Name string
	// Rate: số lượt mỗi giây (> 0).
	Rate float64
	// Do chạy một lượt và tự ghi kết quả vào Recorder. Trả false khi không phát được lượt nào (vd mọi
	// thiết bị đang bận) — runner ghi là lượt bị bỏ.
	Do func(ctx context.Context, rec *Recorder) bool
}

// Run chạy các kịch bản song song trong duration. maxInFlight giới hạn số lượt đang bay mỗi kịch bản;
// vượt → lượt bị bỏ (ghi Drop) thay vì dồn hàng đợi phía máy phát (tránh che giấu độ trễ thật).
func Run(ctx context.Context, scenarios []Scenario, duration time.Duration, maxInFlight int, rec *Recorder) time.Duration {
	if maxInFlight < 1 {
		maxInFlight = 1
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	start := time.Now()
	var all sync.WaitGroup
	for _, s := range scenarios {
		if s.Rate <= 0 {
			continue
		}
		all.Add(1)
		go func(s Scenario) {
			defer all.Done()
			drive(ctx, s, maxInFlight, rec)
		}(s)
	}
	all.Wait()
	return time.Since(start)
}

// drive phát lượt theo tốc độ s.Rate bằng bộ tích lũy: mỗi nhịp (≥ 1 ms) cộng Rate × Δt lượt.
func drive(ctx context.Context, s Scenario, maxInFlight int, rec *Recorder) {
	interval := time.Duration(float64(time.Second) / s.Rate)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	sem := make(chan struct{}, maxInFlight)
	var inflight sync.WaitGroup
	defer inflight.Wait()
	t := time.NewTicker(interval)
	defer t.Stop()
	last := time.Now()
	tokens := 1.0 // lượt đầu phát ngay
	for {
		for tokens >= 1 {
			tokens--
			select {
			case sem <- struct{}{}:
				inflight.Add(1)
				go func() {
					defer inflight.Done()
					defer func() { <-sem }()
					// Lượt đã bắt đầu thì chạy hết (không hủy giữa chừng khi hết giờ): context riêng có hạn.
					octx, ocancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
					defer ocancel()
					if !s.Do(octx, rec) {
						rec.Drop(s.Name)
					}
				}()
			default:
				rec.Drop(s.Name)
			}
		}
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			tokens += s.Rate * now.Sub(last).Seconds()
			last = now
		}
	}
}
