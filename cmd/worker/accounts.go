package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// accountsOrgStatusConsumer khóa/mở tài khoản theo pf.party.org.deactivated/
// reactivated (HTX US-HTXSTATUS-01 AC-2, US-HTXSTATUS-02 AC-2). Handler chạy trong
// giao dịch của inbox.Process trên pool vai accounts_app. Client Keycloak Admin
// (AG_KC_*) mở ở sự kiện đầu tiên; thiếu cấu hình → handler lỗi, sự kiện được giao
// lại rồi vào DLQ (R-62) — không mất yêu cầu khóa.
func accountsOrgStatusConsumer(log *slog.Logger) consumer {
	var (
		once sync.Once
		h    inbox.Handler
		err  error
	)
	return consumer{
		name: accport.OrgStatusConsumer, cluster: migrations.PF, role: accounts.DBRole,
		events: accounts.OrgStatusEvents,
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			once.Do(func() {
				var dir accport.Directory
				dir, err = accounts.DirectoryFromEnv()
				if err == nil && dir == nil {
					err = errors.New("thiếu AG_KC_ADMIN_URL")
				}
				if err == nil {
					h = accounts.New(nil, clock.System{}, accounts.Deps{Directory: dir}).OrgStatusHandler()
				}
			})
			if err != nil {
				log.Error("chưa cấu hình Keycloak Admin cho pf/accounts", "event", env.EventType, "event_id", env.EventID, "outcome", "error")
				return err
			}
			if err := h(ctx, tx, env); err != nil {
				return err
			}
			log.Info("đã áp trạng thái tổ chức cho tài khoản", "event", env.EventType, "event_id", env.EventID, "tenant_id", env.TenantID, "outcome", "ok")
			return nil
		},
	}
}
