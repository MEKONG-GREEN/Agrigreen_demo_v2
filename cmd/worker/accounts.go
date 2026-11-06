package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// accountsOrgStatusConsumer khóa/mở tài khoản theo pf.party.org.deactivated/
// reactivated (HTX US-HTXSTATUS-01 AC-2, US-HTXSTATUS-02 AC-2). Handler chạy trong
// giao dịch của inbox.Process trên pool r_accounts. Client Keycloak Admin
// (AG_KC_*) mở ở sự kiện đầu tiên; thiếu cấu hình → handler lỗi, sự kiện được giao
// lại rồi vào DLQ (R-62) — không mất yêu cầu khóa.
func accountsOrgStatusConsumer(log *slog.Logger) consumer {
	var (
		once sync.Once
		h    inbox.Handler
		err  error
	)
	return consumer{
		name: accport.OrgStatusConsumer, cluster: migrations.PF, module: accounts.DBModule,
		events: accounts.OrgStatusEvents,
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			once.Do(func() {
				var dir accport.Directory
				dir, err = accounts.DirectoryFromEnv()
				if err == nil && dir == nil {
					err = errors.New("thiếu AG_KC_ADMIN_URL")
				}
				if err == nil {
					var acc *accounts.Module
					acc, err = workerAccounts(nil, clock.System{}, accounts.Deps{Directory: dir})
					if err == nil {
						h = acc.OrgStatusHandler()
					}
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

// resetPagePath là trang web đặt mật khẩu mới (apps/web-ops) mà liên kết trong email trỏ tới.
const resetPagePath = "/login/dat-lai-mat-khau"

// accountsPasswordResetConsumer gửi email liên kết tự đặt lại mật khẩu (#255, ADR-0056; BRD 2.0 FN-CH-00.01). pf/accounts
// dựng một lần lúc khởi động consumer trên pool vai app_worker (mỗi giao dịch SET LOCAL ROLE r_accounts): Keycloak
// Admin (AG_KC_*), kênh email theo người nhận của pf/notify (AG_MAILER/AG_SMTP_*) và AG_WEB_OPS_URL. Thiếu → kênh tắt,
// gửi lỗi, sự kiện giao lại rồi vào DLQ — không gửi liên kết hỏng.
func accountsPasswordResetConsumer(log *slog.Logger) consumer {
	var (
		once sync.Once
		mod  *accounts.Module
		err  error
	)
	build := func(ctx context.Context) (*accounts.Module, error) {
		once.Do(func() { mod, err = buildPasswordResetAccounts(ctx, log) })
		return mod, err
	}
	return consumer{
		name: accport.PasswordResetConsumer, cluster: migrations.PF, module: accounts.DBModule,
		events:  []string{accport.EventPasswordResetRequested},
		prepare: func(ctx context.Context) error { _, err := build(ctx); return err },
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			m, err := build(ctx)
			if err != nil {
				log.Error("chưa dựng được pf/accounts cho đặt lại mật khẩu", "event", env.EventType, "event_id", env.EventID, "outcome", "error")
				return err
			}
			return m.PasswordResetHandler()(ctx, tx, env)
		},
	}
}

func buildPasswordResetAccounts(ctx context.Context, log *slog.Logger) (*accounts.Module, error) {
	base := os.Getenv("AG_DB_URL")
	if base == "" {
		return nil, errors.New("thiếu AG_DB_URL")
	}
	dsn, err := dbadmin.RoleURL(base, migrations.PF, "", dbadmin.AppWorker)
	if err != nil {
		return nil, err
	}
	dir, err := accounts.DirectoryFromEnv()
	if err != nil {
		return nil, err
	}
	if dir == nil {
		return nil, errors.New("thiếu AG_KC_ADMIN_URL")
	}
	ch, err := notify.ChannelsFromEnv(log)
	if err != nil {
		return nil, err
	}
	if ch.LinkBase == "" {
		log.Warn("chưa cấu hình AG_WEB_OPS_URL — email đặt lại mật khẩu sẽ không được gửi", "event", "startup", "outcome", "skipped")
	}
	pool, err := db.Open(context.WithoutCancel(ctx), dsn, db.OptionsFromEnv("worker-accounts-reset"))
	if err != nil {
		return nil, err
	}
	acc, err := workerAccounts(pool, clock.System{}, accounts.Deps{Directory: dir})
	if err != nil {
		pool.Close()
		return nil, err
	}
	link := ""
	if ch.LinkBase != "" {
		link = ch.LinkBase + resetPagePath
	}
	acc.UsePasswordReset(ch.EmailMailer, link, log)
	return acc, nil
}
