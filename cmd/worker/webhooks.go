package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// apiclientWebhookJobs dựng job giao webhook mỏng của cổng tích hợp (W5.7, ADR-0033 §4): chỉ báo {feed, cursor,
// occurred_at} ký HMAC, lùi hàm mũ, ngắt mạch sau N lỗi (pf/apiclient, r_apiclient, tenant AG_PUBLIC_OPERATOR_TENANT).
// Đích qua Guard chặn SSRF lúc kết nối (AG_WEBHOOK_ALLOW_HOSTS; AG_WEBHOOK_ALLOW_CIDRS chỉ dev/test). Thiếu khóa chủ
// AG_WEBHOOK_KEY_V<n> → không job (cảnh báo).
func apiclientWebhookJobs(ctx context.Context, direct string, closeFn *func(), log *slog.Logger) ([]schedule.Job, error) {
	op, err := apiclient.OperatorTenantFromEnv()
	if err != nil {
		return nil, err
	}
	keys, err := apiclient.WebhookKeysFromEnv()
	if err != nil {
		return nil, err
	}
	if keys == nil {
		log.Warn("webhook: chưa cấu hình AG_WEBHOOK_KEY_V<n> — không chạy job giao", "event", "startup", "outcome", "degraded")
		return nil, nil
	}
	guard, err := apiclient.WebhookGuardFromEnv(os.Getenv, residency.Production(os.Getenv))
	if err != nil {
		return nil, err
	}
	interval, cooldown, err := apiclient.WebhookIntervalsFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	pool, err := openPF(ctx, direct, closeFn)
	if err != nil {
		return nil, err
	}
	mod := apiclient.New(pool, clock.System{}, apiclient.Deps{OperatorTenant: op, Log: log})
	d := mod.WebhookDispatcher(apiclient.WebhookConfig{Keys: keys, Guard: guard, Policy: apiclient.WebhookPolicy{Cooldown: cooldown}, Log: log})
	return mod.WebhookJobs(d, interval, log), nil
}
