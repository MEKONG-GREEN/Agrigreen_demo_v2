package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	simport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// simulationSnapshotConsumer dựng ảnh chụp đầu vào cho pf.simulation.run.submitted
// (pool vai simulation_app; tham số tra ở pf/masterdata bằng vai masterdata_app).
// Nguồn cung (pf/cropcycle) và bộ định tuyến (pf/waterway) chưa nối ở đợt 1:
// thiếu nguồn cung → việc hỏng simulation.dependency_unavailable; thiếu router →
// Haversine × hệ số #10 (FN-04 BR-03).
func simulationSnapshotConsumer(log *slog.Logger) consumer {
	var (
		once sync.Once
		h    inbox.Handler
		err  error
	)
	return consumer{
		name: simport.SnapshotConsumer, cluster: migrations.PF, role: simulation.DBRole,
		events: []string{simport.EventRunSubmitted},
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			once.Do(func() {
				base := os.Getenv("AG_DB_URL")
				if base == "" {
					err = errors.New("thiếu AG_DB_URL")
					return
				}
				dsn, e := dbadmin.RoleURL(base, migrations.PF, "", masterdata.DBRole)
				if e != nil {
					err = e
					return
				}
				mdPool, e := db.Open(context.WithoutCancel(ctx), dsn, db.OptionsFromEnv("worker-simulation"))
				if e != nil {
					err = e
					return
				}
				md := masterdata.New(mdPool, clock.System{})
				h = simulation.New(nil, clock.System{}, simulation.Deps{Params: simulation.MasterdataParams(md.Params()), Log: log}).SnapshotHandler()
			})
			if err != nil {
				log.Error("chưa dựng được pf/simulation ở worker", "event", env.EventType, "event_id", env.EventID, "outcome", "error", "error", err.Error())
				return err
			}
			return h(ctx, tx, env)
		},
	}
}
