package public

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// RateDecision là kết quả kiểm giới hạn tần suất một lời gọi.
type RateDecision struct {
	Allowed   bool
	Limit     int64         // lời gọi tối đa mỗi phút của gói (header RateLimit-Limit)
	Remaining int64         // còn lại trong cửa sổ
	Reset     time.Duration // tới khi đầy lại một lượt (Retry-After khi bị chặn) / cả bucket
}

// RateLimiter là giới hạn tần suất backend theo khách hàng (ADR-0022 mục 7). Hiện thực trong tiến
// trình (Memory) mặc định; Valkey (AG_VALKEY_URL) là adapter tùy chọn sau cùng interface này — không
// phải nguồn sự thật, hạn mức thẩm quyền ở pf_metering.
type RateLimiter interface {
	Allow(clientID uuid.UUID, perMinute int64, now time.Time) RateDecision
}

// Memory là token bucket trong tiến trình: dung lượng = perMinute / Replicas (chia hạn mức cho số bản
// sao khi không có Valkey), nạp lại đều trong một phút.
type Memory struct {
	Replicas int

	mu      sync.Mutex
	buckets map[uuid.UUID]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
	cap    float64
}

// NewMemory tạo bộ giới hạn trong tiến trình cho replicas bản sao (≥ 1).
func NewMemory(replicas int) *Memory {
	if replicas < 1 {
		replicas = 1
	}
	return &Memory{Replicas: replicas, buckets: map[uuid.UUID]*bucket{}}
}

// Allow trừ một lượt nếu còn.
func (m *Memory) Allow(clientID uuid.UUID, perMinute int64, now time.Time) RateDecision {
	limit := perMinute / int64(m.Replicas)
	if limit < 1 {
		limit = 1
	}
	capacity := float64(limit)
	rate := capacity / 60 // lượt mỗi giây
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.buckets[clientID]
	if !ok || b.cap != capacity {
		b = &bucket{tokens: capacity, last: now, cap: capacity}
		m.buckets[clientID] = b
	}
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens += el * rate
		if b.tokens > capacity {
			b.tokens = capacity
		}
		b.last = now
	}
	if b.tokens < 1 {
		wait := time.Duration((1 - b.tokens) / rate * float64(time.Second))
		return RateDecision{Allowed: false, Limit: limit, Remaining: 0, Reset: wait}
	}
	b.tokens--
	full := time.Duration((capacity - b.tokens) / rate * float64(time.Second))
	return RateDecision{Allowed: true, Limit: limit, Remaining: int64(b.tokens), Reset: full}
}
