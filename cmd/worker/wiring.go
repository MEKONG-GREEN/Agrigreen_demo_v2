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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// platformLazy dựng MỘT lần toàn bộ module pf qua assembly.Build (cùng gốc lắp ráp với
// api-platform: mỗi module mở pool bằng vai DB riêng, R-05) ở sự kiện đầu tiên của các
// consumer liên module. Keycloak (AG_KC_*), kho đối tượng, khóa con trỏ đọc từ môi trường
// như api-platform; thiếu thì module liên quan trả 503 — không ảnh hưởng consumer ở đây.
type platformLazy struct {
	once sync.Once
	mods assembly.Modules
	err  error
}

func (p *platformLazy) get(ctx context.Context) (assembly.Modules, error) {
	p.once.Do(func() {
		base := os.Getenv("AG_DB_URL")
		if base == "" {
			p.err = errors.New("thiếu AG_DB_URL")
			return
		}
		open := func(ctx context.Context, role string) (*pgxpool.Pool, error) {
			dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
			if err != nil {
				return nil, err
			}
			return db.Open(ctx, dsn, db.Options{AppName: "worker-platform", MaxConns: 2})
		}
		p.mods, _, p.err = assembly.Build(context.WithoutCancel(ctx), open, clock.System{}, assembly.External{Egress: workerEgress})
	})
	return p.mods, p.err
}

// wiredConsumers: consumer liên module của G2 (assembly.Consumers) — FN-07 diện tích vụ,
// van công bố hub MKG, log GPS sau commit, ảnh chụp mô phỏng. Mỗi consumer có hàng đợi,
// DLQ và inbox riêng trên vai DB của module nhận.
func wiredConsumers(log *slog.Logger) []consumer {
	lazy := &platformLazy{}
	out := make([]consumer, 0, len(assembly.Consumers))
	for _, c := range assembly.Consumers {
		spec := c
		out = append(out, consumer{
			name: spec.Name, cluster: migrations.PF, role: spec.Role, events: spec.Events,
			handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
				m, err := lazy.get(ctx)
				if err != nil {
					log.Error("chưa dựng được module pf ở worker", "event", env.EventType, "event_id", env.EventID, "consumer", spec.Name,
						"outcome", "error", "error", err.Error())
					return err
				}
				return spec.Handler(m)(ctx, tx, env)
			},
		})
	}
	return out
}

// workerEgress là điểm kiểm soát xuất (pf/metering Checkpoint, ADR-0024) cho job của worker dựng
// module riêng (lịch gửi báo cáo). Nil → hành vi cũ; người tích hợp nối khi có pf/metering.
var workerEgress meter.Checkpoint
