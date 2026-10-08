package webhook

// Ngắt mạch mỗi webhook (R-INT-09, ADR-0033 §4) và lùi thời gian thử lại — thuần, không I/O.
//
//	closed ──N lỗi liên tiếp──▶ open ──hết thời gian chờ──▶ half_open (MỘT lần thử thật)
//	   ▲                          ▲                              │
//	   └──────── thành công ──────┼──────────────────────────────┘
//	                              └───────── thất bại (thời gian chờ ×2, tối đa MaxCooldown)
//
// Đóng ngắt không làm mất dữ liệu: chỉ báo chỉ là cursor; đối tác đọc tiếp từ cursor của mình.

import (
	"math/rand/v2"
	"time"
)

// Trạng thái ngắt mạch.
const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
)

// Mặc định của chính sách.
const (
	DefaultThreshold   = 5
	DefaultCooldown    = 5 * time.Minute
	DefaultMaxCooldown = time.Hour
	DefaultRetryBase   = 10 * time.Second
	DefaultRetryMax    = 10 * time.Minute
)

// Policy là tham số ngắt mạch và lùi thời gian.
type Policy struct {
	Cooldown    time.Duration // chờ trước lần thử half-open đầu tiên
	MaxCooldown time.Duration // trần thời gian chờ khi thử half-open lỗi liên tiếp
	RetryBase   time.Duration // lùi lần thử lại đầu tiên (mạch đóng)
	RetryMax    time.Duration // trần lùi thử lại
}

func (p Policy) norm() Policy {
	if p.Cooldown <= 0 {
		p.Cooldown = DefaultCooldown
	}
	if p.MaxCooldown < p.Cooldown {
		p.MaxCooldown = max(DefaultMaxCooldown, p.Cooldown)
	}
	if p.RetryBase <= 0 {
		p.RetryBase = DefaultRetryBase
	}
	if p.RetryMax < p.RetryBase {
		p.RetryMax = max(DefaultRetryMax, p.RetryBase)
	}
	return p
}

// expo trả base × 2^(n−1), tối đa ceil.
func expo(base, ceil time.Duration, n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := base
	for i := 1; i < n; i++ {
		if d >= ceil/2 {
			return ceil
		}
		d *= 2
	}
	return min(d, ceil)
}

// CooldownFor là thời gian chờ sau lần mở thứ n (n ≥ 1).
func (p Policy) CooldownFor(n int) time.Duration {
	p = p.norm()
	return expo(p.Cooldown, p.MaxCooldown, n)
}

// Backoff là thời gian chờ trước lần thử lại sau attempts lần lỗi liên tiếp của một chỉ báo: lùi hàm mũ, nhiễu
// trong [d/2, d] (jitter ∈ [0, 1); nil → ngẫu nhiên).
func (p Policy) Backoff(attempts int, jitter func() float64) time.Duration {
	p = p.norm()
	d := expo(p.RetryBase, p.RetryMax, attempts)
	j := 0.0
	if jitter != nil {
		j = jitter()
	} else {
		j = rand.Float64() //nolint:gosec // nhiễu lịch thử lại, không phải mật mã
	}
	if j < 0 || j >= 1 {
		j = 0
	}
	return d/2 + time.Duration(float64(d/2)*j)
}

// Breaker là trạng thái ngắt mạch của một webhook.
type Breaker struct {
	State       string
	Failures    int // lỗi liên tiếp
	OpenedCount int // số lần mở liên tiếp (không xen thành công)
	OpenedAt    *time.Time
	NextProbeAt *time.Time
	LastSuccess *time.Time
	LastFailure *time.Time
	LastError   string
}

// Decision là quyết định giao của một lượt.
type Decision int

// Quyết định.
const (
	Deny  Decision = iota // mạch mở, chưa tới lúc thử
	Allow                 // mạch đóng
	Probe                 // tới lúc thử half-open: đúng MỘT lần giao
)

// Decide quyết định tại now. half_open còn hạn thử (một lần thử đang chạy ở tiến trình khác) → Deny; quá hạn (tiến
// trình thử chết giữa chừng) → Probe lại.
func (b Breaker) Decide(now time.Time) Decision {
	switch b.State {
	case StateOpen, StateHalfOpen:
		if b.NextProbeAt != nil && !now.Before(*b.NextProbeAt) {
			return Probe
		}
		return Deny
	default:
		return Allow
	}
}

func tp(t time.Time) *time.Time { return &t }

// StartProbe chuyển sang half_open; NextProbeAt = hạn của lần thử (một thời gian chờ).
func (b *Breaker) StartProbe(now time.Time, p Policy) {
	b.State = StateHalfOpen
	b.NextProbeAt = tp(now.Add(p.CooldownFor(max(b.OpenedCount, 1))))
}

// Success ghi một lần giao thành công: đóng mạch, xóa bộ đếm. Trả true khi trạng thái đổi.
func (b *Breaker) Success(now time.Time) bool {
	changed := b.State != StateClosed
	b.State, b.Failures, b.OpenedCount, b.OpenedAt, b.NextProbeAt, b.LastError = StateClosed, 0, 0, nil, nil, ""
	b.LastSuccess = tp(now)
	return changed
}

// Failure ghi một lần giao lỗi với ngưỡng threshold. Trả true khi trạng thái đổi (mở hoặc mở lại).
func (b *Breaker) Failure(now time.Time, class string, threshold int, p Policy) bool {
	if threshold < 1 {
		threshold = DefaultThreshold
	}
	b.Failures++
	b.LastFailure, b.LastError = tp(now), class
	switch b.State {
	case StateHalfOpen, StateOpen:
		b.OpenedCount++
		b.State, b.OpenedAt, b.NextProbeAt = StateOpen, tp(now), tp(now.Add(p.CooldownFor(b.OpenedCount)))
		return true
	default:
		if b.Failures >= threshold {
			b.OpenedCount = 1
			b.State, b.OpenedAt, b.NextProbeAt = StateOpen, tp(now), tp(now.Add(p.CooldownFor(1)))
			return true
		}
		return false
	}
}

// Reset đóng mạch thủ công (quản trị viên, sau khi đối tác sửa đầu nhận).
func (b *Breaker) Reset() {
	b.State, b.Failures, b.OpenedCount, b.OpenedAt, b.NextProbeAt = StateClosed, 0, 0, nil, nil
}
