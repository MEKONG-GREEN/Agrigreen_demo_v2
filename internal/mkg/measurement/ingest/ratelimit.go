package ingest

import (
	"sync"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// RateLimiter là token bucket theo thiết bị (khóa = SHA-256 chứng thư client, đã
// qua mTLS nên số khóa bị chặn trên bởi số chứng thư CA nội bộ cấp). Số học
// nguyên theo nano giây — không float (R-10 cho package measurement).
type RateLimiter struct {
	clk    clock.Clock
	per    time.Duration // thời gian nạp một token
	burst  int64
	mu     sync.Mutex
	bucket map[string]*bucket
}

type bucket struct {
	tokens int64
	last   time.Time
}

// NewRateLimiter cho phép perMinute lô/phút mỗi thiết bị, dồn tối đa burst.
// perMinute ≤ 0 → không giới hạn (nil).
func NewRateLimiter(clk clock.Clock, perMinute, burst int) *RateLimiter {
	if perMinute <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{clk: clk, per: time.Minute / time.Duration(perMinute), burst: int64(burst), bucket: map[string]*bucket{}}
}

// Allow lấy một token của key; false kèm thời gian chờ tới token kế tiếp.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.clk.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.bucket[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.bucket[key] = b
	}
	if el := now.Sub(b.last); el > 0 {
		n := int64(el / l.per)
		if n > 0 {
			b.tokens += n
			b.last = b.last.Add(time.Duration(n) * l.per)
		}
		if b.tokens >= l.burst {
			b.tokens, b.last = l.burst, now
		}
	}
	if b.tokens > 0 {
		b.tokens--
		return true, 0
	}
	return false, l.per - now.Sub(b.last)
}
