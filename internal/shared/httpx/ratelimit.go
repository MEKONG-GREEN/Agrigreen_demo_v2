package httpx

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ErrRateLimited là phản hồi 429 của RateLimitByIP.
var ErrRateLimited = problem.New(http.StatusTooManyRequests, "rate_limited", "Quá nhiều yêu cầu, vui lòng thử lại sau")

// maxIPBuckets chặn bộ nhớ của bảng IP: vượt ngưỡng thì dọn các bucket đã nạp đầy (rảnh).
const maxIPBuckets = 10000

// RateLimitByIP giới hạn perMinute yêu cầu/phút theo ClientIP, dồn tối đa burst — cho route
// công khai không đăng nhập (vd liên kết tải Chi cục): mỗi yêu cầu sai vẫn tốn một giao dịch DB.
// Giới hạn trong tiến trình (mỗi bản sao riêng) — chỉ là lớp chặn thô, bổ sung cho giới hạn ở
// Ingress. ClientIP đúng chỉ khi AG_TRUSTED_PROXIES khai báo proxy (xem configureProxies).
func RateLimitByIP(clk clock.Clock, perMinute, burst int) gin.HandlerFunc {
	if burst < 1 {
		burst = 1
	}
	l := &ipLimiter{clk: clk, per: time.Minute / time.Duration(max(perMinute, 1)), burst: float64(burst), buckets: map[string]*ipBucket{}}
	return func(c *gin.Context) {
		ok, wait := l.allow(c.ClientIP())
		if !ok {
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			Fail(c, ErrRateLimited)
			return
		}
		c.Next()
	}
}

type ipLimiter struct {
	clk     clock.Clock
	per     time.Duration // thời gian nạp một token
	burst   float64
	mu      sync.Mutex
	buckets map[string]*ipBucket
}

type ipBucket struct {
	tokens float64
	last   time.Time
}

func (l *ipLimiter) refill(b *ipBucket, now time.Time) {
	if el := now.Sub(b.last); el > 0 {
		b.tokens = min(l.burst, b.tokens+float64(el)/float64(l.per))
		b.last = now
	}
}

func (l *ipLimiter) allow(key string) (bool, time.Duration) {
	now := l.clk.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxIPBuckets {
			l.sweep(now)
			if len(l.buckets) >= maxIPBuckets {
				return false, l.per // bảng đầy IP đang hoạt động (bị dội): từ chối IP mới
			}
		}
		b = &ipBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	l.refill(b, now)
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(l.per))
}

// sweep bỏ bucket đã nạp đầy (tương đương bucket mới); người gọi giữ mu.
func (l *ipLimiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		l.refill(b, now)
		if b.tokens >= l.burst {
			delete(l.buckets, k)
		}
	}
}
