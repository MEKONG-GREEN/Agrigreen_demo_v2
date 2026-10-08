package apiclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/webhook"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// Webhook mỏng + ngắt mạch (W5.7, ADR-0033 §4) — gốc lắp ráp (cmd/api-platform: Admin API; cmd/worker: job giao) chỉ
// đi qua gốc module này (R-02).
type (
	// WebhookKeyring là khóa chủ dẫn xuất bí mật webhook (AG_WEBHOOK_KEY_V<n>).
	WebhookKeyring = webhook.Keyring
	// WebhookGuard là chính sách đích webhook (chặn SSRF).
	WebhookGuard = webhook.Guard
	// WebhookDispatcher giao chỉ báo cursor.
	WebhookDispatcher = webhook.Dispatcher
	// WebhookPolicy là tham số ngắt mạch + lùi thời gian.
	WebhookPolicy = webhook.Policy
	// WebhookRunResult tóm tắt một lượt giao.
	WebhookRunResult = webhook.RunResult
)

// Thư viện xác minh phía đối tác (mẫu tích hợp và kiểm thử).
var (
	// VerifyWebhookSignature xác minh header AG-Webhook-Signature (webhook.Verify).
	VerifyWebhookSignature = webhook.Verify
	// ParseWebhookSecret giải bí mật "whsec_…".
	ParseWebhookSecret = webhook.ParseSecret
	// NewWebhookKeyring dựng khóa chủ (kiểm thử).
	NewWebhookKeyring = webhook.NewKeyring
)

// Header của lời gọi webhook.
const (
	WebhookHeaderID        = webhook.HeaderID
	WebhookHeaderTimestamp = webhook.HeaderTimestamp
	WebhookHeaderSignature = webhook.HeaderSignature
)

// Biến môi trường của webhook.
const (
	// EnvWebhookAllowHosts: danh sách tên miền đích cho phép (phân tách dấu phẩy; tên con được nhận). Rỗng → mọi tên
	// miền công khai qua Guard.
	EnvWebhookAllowHosts = "AG_WEBHOOK_ALLOW_HOSTS"
	// EnvWebhookAllowCIDRs: dải được MIỄN chặn SSRF — CHỈ dev/test; production từ chối khởi động nếu đặt.
	EnvWebhookAllowCIDRs = "AG_WEBHOOK_ALLOW_CIDRS"
	// EnvWebhookDispatchInterval: chu kỳ job giao (≥ 5s, mặc định 30s).
	EnvWebhookDispatchInterval = "AG_WEBHOOK_DISPATCH_INTERVAL"
	// EnvWebhookCooldown: thời gian chờ trước lần thử half-open đầu (≥ 30s, mặc định 5m; gấp đôi mỗi lần thử lỗi, tối
	// đa 1 giờ).
	EnvWebhookCooldown = "AG_WEBHOOK_BREAKER_COOLDOWN"
	// DefaultWebhookDispatchInterval là chu kỳ giao mặc định.
	DefaultWebhookDispatchInterval = 30 * time.Second
)

type webhookWiring struct {
	keys  *webhook.Keyring
	guard webhook.Guard
	svc   *webhook.Service
}

// WebhookKeysFromEnv đọc khóa chủ webhook (không đặt → nil, nil: đăng ký/xoay trả 503, worker không chạy job giao).
func WebhookKeysFromEnv() (*webhook.Keyring, error) {
	return webhook.KeyringFromEnv(os.Getenv, os.Environ())
}

// WebhookGuardFromEnv đọc chính sách đích. production: AG_WEBHOOK_ALLOW_CIDRS bị cấm (lỗi).
func WebhookGuardFromEnv(getenv func(string) string, production bool) (webhook.Guard, error) {
	var g webhook.Guard
	for _, h := range strings.Split(getenv(EnvWebhookAllowHosts), ",") {
		if h = strings.TrimSpace(h); h != "" {
			g.AllowHosts = append(g.AllowHosts, h)
		}
	}
	cidrs := strings.TrimSpace(getenv(EnvWebhookAllowCIDRs))
	if cidrs == "" {
		return g, nil
	}
	if production {
		return g, fmt.Errorf("%s bị cấm ở production (miễn chặn SSRF chỉ cho dev/test)", EnvWebhookAllowCIDRs)
	}
	for _, c := range strings.Split(cidrs, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(c))
		if err != nil {
			return g, fmt.Errorf("%s: %q không phải CIDR", EnvWebhookAllowCIDRs, c)
		}
		g.AllowNets = append(g.AllowNets, p)
	}
	g.AllowAnyPort = true
	return g, nil
}

// UseWebhooks nối khóa chủ và chính sách đích cho Admin API (đăng ký / xoay bí mật).
func (m *Module) UseWebhooks(keys *webhook.Keyring, g webhook.Guard) {
	m.wh.keys, m.wh.guard = keys, g
	if m.wh.svc != nil { // route Admin đã gắn trước: cập nhật use case đang dùng (chỉ lúc lắp ráp)
		m.wh.svc.Keys, m.wh.svc.Guard = keys, g
	}
}

// Webhooks trả use case quản trị webhook (Admin API).
func (m *Module) Webhooks() *webhook.Service {
	if m.wh.svc == nil {
		m.wh.svc = &webhook.Service{DB: m.svc.DB, Keys: m.wh.keys, Guard: m.wh.guard, OperatorTenant: m.svc.OperatorTenant, Clock: m.svc.Clock,
			Log: m.svc.Log}
	}
	return m.wh.svc
}

// WebhookConfig là tham số job giao.
type WebhookConfig struct {
	Keys        *webhook.Keyring
	Guard       webhook.Guard
	Policy      webhook.Policy
	Concurrency int
	Timeout     time.Duration
	TLS         *tls.Config // kiểm thử: RootCAs của httptest
	Jitter      func() float64
	Log         *slog.Logger
}

// WebhookDispatcher dựng bộ giao trên pool của module (tenant đơn vị vận hành).
func (m *Module) WebhookDispatcher(c WebhookConfig) *webhook.Dispatcher {
	return &webhook.Dispatcher{DB: m.svc.DB, Keys: c.Keys, Guard: c.Guard, Policy: c.Policy, Concurrency: c.Concurrency, Timeout: c.Timeout,
		TLS: c.TLS, OperatorTenant: m.svc.OperatorTenant, Clock: m.svc.Clock, Jitter: c.Jitter, Log: c.Log}
}

// WebhookIntervalsFromEnv đọc chu kỳ giao và thời gian chờ ngắt mạch.
func WebhookIntervalsFromEnv(getenv func(string) string) (interval, cooldown time.Duration, err error) {
	interval, cooldown = DefaultWebhookDispatchInterval, webhook.DefaultCooldown
	if v := getenv(EnvWebhookDispatchInterval); v != "" {
		d, perr := time.ParseDuration(v)
		if perr != nil || d < 5*time.Second {
			return 0, 0, fmt.Errorf("%s: thời lượng ≥ 5s (vd 30s)", EnvWebhookDispatchInterval)
		}
		interval = d
	}
	if v := getenv(EnvWebhookCooldown); v != "" {
		d, perr := time.ParseDuration(v)
		if perr != nil || d < 30*time.Second {
			return 0, 0, fmt.Errorf("%s: thời lượng ≥ 30s (vd 5m)", EnvWebhookCooldown)
		}
		cooldown = d
	}
	return interval, cooldown, nil
}

// WebhookJobs trả job port.JobWebhookDispatch (leader cmd/worker). Không tenant đơn vị vận hành hoặc không khóa chủ
// → không job nào.
func (m *Module) WebhookJobs(d *webhook.Dispatcher, interval time.Duration, log *slog.Logger) []schedule.Job {
	if m.svc.OperatorTenant == uuid.Nil || d == nil || d.Keys == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return []schedule.Job{{Name: port.JobWebhookDispatch, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 5 * time.Minute,
		Run: func(ctx context.Context) error {
			r, err := d.RunOnce(ctx)
			outcome := "ok"
			if err != nil && !errors.Is(err, context.Canceled) {
				outcome = "error"
			}
			if err != nil || r.Delivered+r.Failed+r.Blocked > 0 {
				log.Info("webhook: lượt giao", "event", "job."+port.JobWebhookDispatch, "delivered", r.Delivered, "failed", r.Failed,
					"blocked", r.Blocked, "deferred", r.Deferred, "opened", r.Opened, "closed", r.Closed, "outcome", outcome)
			}
			return err
		}}}
}
