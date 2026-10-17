package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// notifyLazy dựng module pf/notify cho worker ở sự kiện đầu tiên: pool notify_app
// (ghi kết quả gửi đẩy bằng giao dịch riêng), danh bạ cán bộ (extension_app),
// đơn vị hành chính (masterdata_app) — mỗi module một vai (R-05) — và kênh đẩy từ
// môi trường (AG_FCM_* → FCM HTTP v1; không đặt → chỉ ghi log).
type notifyLazy struct {
	log  *slog.Logger
	once sync.Once
	mod  *notify.Module
	err  error
}

func (n *notifyLazy) get(ctx context.Context) (*notify.Module, error) {
	n.once.Do(func() { n.mod, n.err = buildNotify(ctx, n.log, os.Getenv("AG_DB_URL"), func(*pgxpool.Pool) {}) })
	return n.mod, n.err
}

// buildNotify mở pool theo vai và dựng module; keep nhận từng pool để đóng khi dừng.
func buildNotify(ctx context.Context, log *slog.Logger, base string, keep func(*pgxpool.Pool)) (*notify.Module, error) {
	if base == "" {
		return nil, errors.New("thiếu AG_DB_URL")
	}
	// W2.6: một pool vai app_worker; mỗi module tự bọc db.ForModule (r_<module>).
	dsn, err := dbadmin.RoleURL(base, migrations.PF, "", dbadmin.AppWorker)
	if err != nil {
		return nil, err
	}
	pool, err := db.Open(ctx, dsn, db.Options{AppName: "worker-notify", MaxConns: 8})
	if err != nil {
		return nil, err
	}
	keep(pool)
	nPool, extPool, mdPool, coopPool, accPool := pool, pool, pool, pool, pool
	ch, err := notify.ChannelsFromEnv(log)
	if err != nil {
		return nil, err
	}
	clk := clock.System{}
	md := masterdata.New(mdPool, clk, masterdata.Deps{Params: []mdport.ParamRegistration{extension.MasterdataParams()}, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}})
	kn := extension.New(extPool, clk, extension.Deps{Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs()})
	log.Info("kênh thông báo", "event", "startup", "push", ch.Pusher.Name(), "email", ch.Mailer != nil, "outcome", "ok")
	// pf/cooperative (W3.3): nông hộ có thửa theo xã (schema pf_coop, r_cooperative).
	coop := cooperative.New(coopPool, clk, cooperative.Deps{})
	// Gói NOTIF: email theo người nhận tra địa chỉ qua pf/accounts (Keycloak, AG_KC_*) lúc gửi; người nhận Cấp Tỉnh của
	// Báo giá từ lần gán vai + cờ QTNV (pf_accounts, r_accounts).
	kc, err := accounts.DirectoryFromEnv()
	if err != nil {
		return nil, err
	}
	acc := accounts.New(accPool, clk, accounts.Deps{Directory: kc})
	farmers := assembly.AreaFarmers{Households: coop.Households(), Accounts: acc.FarmerAccounts()}
	d := notify.Deps{Consumers: assembly.NotificationConsumers(), Renderers: assembly.NotificationRenderers(), Staff: kn.Staff(), Units: md.AdminUnits(), Farmers: farmers, Pusher: ch.Pusher, Mailer: ch.Mailer,
		EmailMailer: ch.EmailMailer, DigestTo: ch.DigestTo, Log: log, LinkBase: os.Getenv("AG_WEB_OPS_URL"),
		PriceDesk: assembly.NotifyPriceDesk{Holders: acc.RoleHolders(), Profiles: assembly.PriceProfiles()}}
	if dir := acc.MailDirectory(); dir != nil {
		d.Emails = assembly.NotifyEmails{Dir: dir}
	} else {
		log.Warn("chưa cấu hình AG_KC_* — thông báo chỉ gửi trong ứng dụng, không email theo người nhận", "event", "startup", "outcome", "skipped")
	}
	return notify.New(nPool, clk, d), nil
}

// notifyConsumers: một consumer mỗi nhóm sự kiện (pf.notify.directive, .content,
// .task, .sync, .party, .cropcycle, .push), cùng chạy trên r_notify; idempotent qua inbox.
func notifyConsumers(log *slog.Logger) []consumer {
	lazy := &notifyLazy{log: log}
	// Danh sách consumer và sự kiện lấy từ module (không cần DB để liệt kê).
	specs := notify.New(nil, clock.System{}, notify.Deps{Consumers: assembly.NotificationConsumers(), Renderers: assembly.NotificationRenderers()}).Consumers()
	out := make([]consumer, 0, len(specs))
	for i := range specs {
		sp := specs[i]
		out = append(out, consumer{
			name: sp.Name, cluster: migrations.PF, module: notify.DBModule, events: sp.Events,
			handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
				m, err := lazy.get(ctx)
				if err != nil {
					log.Error("không dựng được module thông báo", "event", env.EventType, "event_id", env.EventID, "outcome", "error")
					return err
				}
				for _, c := range m.Consumers() {
					if c.Name == sp.Name {
						return c.Handler(ctx, tx, env)
					}
				}
				return nil
			},
		})
	}
	return out
}
