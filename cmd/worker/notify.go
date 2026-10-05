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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
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
	open := func(role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, db.Options{AppName: "worker-notify", MaxConns: 4})
		if err == nil {
			keep(p)
		}
		return p, err
	}
	nPool, err := open(notify.DBRole)
	if err != nil {
		return nil, err
	}
	extPool, err := open(extension.DBRole)
	if err != nil {
		return nil, err
	}
	mdPool, err := open(masterdata.DBRole)
	if err != nil {
		return nil, err
	}
	// Cảnh báo khẩn tới nông dân theo địa bàn (HTX US-NEWS-03): thửa (geo_app) + liên kết
	// tài khoản nông dân (accounts_app), mỗi module một vai.
	geoPool, err := open(geo.DBRole)
	if err != nil {
		return nil, err
	}
	accPool, err := open(accounts.DBRole)
	if err != nil {
		return nil, err
	}
	ch, err := notify.ChannelsFromEnv(log)
	if err != nil {
		return nil, err
	}
	clk := clock.System{}
	md := masterdata.New(mdPool, clk)
	kn := extension.New(extPool, clk, extension.Deps{Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs()})
	log.Info("kênh thông báo", "event", "startup", "push", ch.Pusher.Name(), "email", ch.Mailer != nil, "outcome", "ok")
	farmers := assembly.AreaFarmers{Plots: geo.New(geoPool, clk, geo.Deps{}).PlotHouseholds(),
		Accounts: accounts.New(accPool, clk, accounts.Deps{}).FarmerAccounts()}
	return notify.New(nPool, clk, notify.Deps{Staff: kn.Staff(), Units: md.AdminUnits(), Farmers: farmers, Pusher: ch.Pusher, Mailer: ch.Mailer,
		DigestTo: ch.DigestTo, Log: log}), nil
}

// notifyConsumers: một consumer mỗi nhóm sự kiện (pf.notify.directive, .content,
// .task, .sync, .party, .cropcycle, .push), cùng chạy trên vai notify_app; idempotent qua inbox.
func notifyConsumers(log *slog.Logger) []consumer {
	lazy := &notifyLazy{log: log}
	// Danh sách consumer và sự kiện lấy từ module (không cần DB để liệt kê).
	specs := notify.New(nil, clock.System{}, notify.Deps{}).Consumers()
	out := make([]consumer, 0, len(specs))
	for i := range specs {
		sp := specs[i]
		out = append(out, consumer{
			name: sp.Name, cluster: migrations.PF, role: notify.DBRole, module: notify.DBModule, events: sp.Events,
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
