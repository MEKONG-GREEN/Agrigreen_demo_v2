package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Hạn mức theo người yêu cầu cho thao tác trả/dò định danh (Lookup, Resolve) — M13:
// vai vault_registrar tra được toàn tenant, token bị lộ không được dò hàng loạt
// SĐT/CCCD. Token bucket TRONG BỘ NHỚ theo (tenant, actor, thao tác): đúng cho một
// bản sao identity-vault (cấu hình hiện tại); chạy N bản sao thì hạn mức thực tế
// là N lần — khi đó cần bộ đếm dùng chung (vd đếm vault.access_log theo actor) —
// xem docs/runbooks.
//
// Biến môi trường (cmd/identity-vault):
//
//	AG_VAULT_QUOTA_PER_HOUR  số lần/giờ mỗi người mỗi thao tác (mặc định 600; 0 = tắt hạn mức)
//	AG_VAULT_QUOTA_BURST     dồn tối đa (mặc định 200)
const (
	EnvQuotaPerHour = "AG_VAULT_QUOTA_PER_HOUR"
	EnvQuotaBurst   = "AG_VAULT_QUOTA_BURST"

	DefaultQuotaPerHour = 600
	DefaultQuotaBurst   = 200

	// MetricQuotaExceeded đếm lần bị từ chối vì vượt hạn mức, theo thao tác (không
	// gắn actor vào metric — cardinality; actor_ref nằm trong log sự kiện).
	MetricQuotaExceeded = "ag.vault.quota_exceeded"
)

// maxQuotaBuckets chặn bộ nhớ: vượt ngưỡng thì dọn bucket đã nạp đầy (rảnh).
const maxQuotaBuckets = 50000

// Quota là hạn mức theo người yêu cầu. nil → không giới hạn (kiểm thử, công cụ).
type Quota struct {
	clk     clock.Clock
	log     *slog.Logger
	per     time.Duration // thời gian nạp một lượt
	burst   float64
	mu      sync.Mutex
	buckets map[quotaKey]*quotaBucket
	counter metric.Int64Counter
}

type quotaKey struct {
	tenant, actor uuid.UUID
	op            string
}

type quotaBucket struct {
	tokens float64
	last   time.Time
}

// NewQuota tạo hạn mức perHour lượt/giờ, dồn tối đa burst. perHour ≤ 0 → nil (tắt).
func NewQuota(clk clock.Clock, log *slog.Logger, perHour, burst int) *Quota {
	if perHour <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	c, _ := telemetry.Meter().Int64Counter(MetricQuotaExceeded,
		metric.WithDescription("Số lần kho định danh từ chối vì người yêu cầu vượt hạn mức tra/đọc định danh"))
	return &Quota{
		clk: clk, log: log, per: time.Hour / time.Duration(perHour), burst: float64(burst),
		buckets: map[quotaKey]*quotaBucket{}, counter: c,
	}
}

// QuotaFromEnv đọc AG_VAULT_QUOTA_PER_HOUR / AG_VAULT_QUOTA_BURST (mặc định 600/200).
func QuotaFromEnv(clk clock.Clock, log *slog.Logger) (*Quota, error) {
	perHour, err := intEnv(EnvQuotaPerHour, DefaultQuotaPerHour)
	if err != nil {
		return nil, err
	}
	burst, err := intEnv(EnvQuotaBurst, DefaultQuotaBurst)
	if err != nil {
		return nil, err
	}
	return NewQuota(clk, log, perHour, burst), nil
}

func intEnv(name string, def int) (int, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("vault: %s phải là số nguyên ≥ 0", name)
	}
	return n, nil
}

// allow trừ một lượt; hết lượt → false và thời gian chờ tới lượt kế.
func (q *Quota) allow(k quotaKey) (bool, time.Duration) {
	now := q.clk.Now()
	q.mu.Lock()
	defer q.mu.Unlock()
	b, ok := q.buckets[k]
	if !ok {
		if len(q.buckets) >= maxQuotaBuckets {
			q.sweep(now)
		}
		b = &quotaBucket{tokens: q.burst, last: now}
		q.buckets[k] = b
	}
	q.refill(b, now)
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(q.per))
}

func (q *Quota) refill(b *quotaBucket, now time.Time) {
	if el := now.Sub(b.last); el > 0 {
		b.tokens = min(q.burst, b.tokens+float64(el)/float64(q.per))
		b.last = now
	}
}

// sweep bỏ bucket đã nạp đầy (tương đương bucket mới); người gọi giữ mu.
func (q *Quota) sweep(now time.Time) {
	for k, b := range q.buckets {
		q.refill(b, now)
		if b.tokens >= q.burst {
			delete(q.buckets, k)
		}
	}
}

// check: nil → không giới hạn. Vượt → ghi log sự kiện vault.quota_exceeded (có
// actor_ref, KHÔNG có giá trị tra cứu) + metric, trả 429 vault.quota_exceeded.
func (q *Quota) check(ctx context.Context, tenantID, actor uuid.UUID, op string, purpose client.Purpose) error {
	if q == nil {
		return nil
	}
	ok, wait := q.allow(quotaKey{tenant: tenantID, actor: actor, op: op})
	if ok {
		return nil
	}
	q.counter.Add(ctx, 1, metric.WithAttributes(attribute.String("op", op)))
	q.log.WarnContext(ctx, "người yêu cầu vượt hạn mức tra/đọc định danh", "event", "vault.quota_exceeded", "outcome", "rejected",
		"op", op, "purpose", string(purpose), "tenant_id", tenantID.String(), "actor_ref", actor.String())
	return client.ProblemQuotaExceeded.With("retry_after_s", int(math.Ceil(wait.Seconds())))
}
