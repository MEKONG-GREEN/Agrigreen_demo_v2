package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// buildPlatform dựng toàn bộ module pf (assembly.Build) với pool nối thẳng theo vai của
// từng module — job metrics_snapshot và lệnh backfill cần nguồn của nhiều module.
func buildPlatform(ctx context.Context, direct string) (assembly.Modules, []*pgxpool.Pool, error) {
	open := func(ctx context.Context, role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(direct, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		return db.Open(ctx, dsn, db.Options{AppName: "worker-metrics", MaxConns: 8})
	}
	return assembly.Build(ctx, open, clock.System{}, assembly.External{Login: dbadmin.AppWorker})
}

// parseRange đọc "YYYY-MM-DD:YYYY-MM-DD".
func parseRange(s string) (clock.Date, clock.Date, error) {
	a, b, ok := strings.Cut(s, ":")
	if !ok {
		return clock.Date{}, clock.Date{}, errors.New("-metrics-backfill phải dạng YYYY-MM-DD:YYYY-MM-DD")
	}
	from, err := metrics.ParseDate(a)
	if err != nil {
		return from, from, err
	}
	to, err := metrics.ParseDate(b)
	return from, to, err
}

// runMetricsBackfill ghi sổ chỉ số + ảnh chụp lớp cho mọi ngày đã kết thúc trong khoảng.
func runMetricsBackfill(log *slog.Logger, rng, tenantArg, keysArg string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	from, to, err := parseRange(rng)
	if err != nil {
		return err
	}
	direct := os.Getenv("AG_DB_DIRECT_URL")
	if direct == "" {
		direct = os.Getenv("AG_DB_URL")
	}
	if direct == "" {
		return errors.New("thiếu AG_DB_URL")
	}
	mods, pools, err := buildPlatform(ctx, direct)
	defer func() {
		for _, p := range pools {
			p.Close()
		}
	}()
	if err != nil {
		return err
	}
	var keys []metricsport.Key
	for _, k := range strings.Split(keysArg, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, metricsport.Key(k))
		}
	}
	var tids []uuid.UUID
	if tenantArg != "" {
		id, err := uuid.Parse(tenantArg)
		if err != nil {
			return fmt.Errorf("-metrics-tenant: %w", err)
		}
		tids = []uuid.UUID{id}
	} else {
		dsn, err := dbadmin.RoleURL(direct, migrations.PF, "", dbadmin.OpsJobs)
		if err != nil {
			return err
		}
		p, err := db.Open(ctx, dsn, db.Options{AppName: "worker-metrics-backfill", MaxConns: 2})
		if err != nil {
			return err
		}
		defer p.Close()
		if tids, err = metrics.JobTenants(p)(ctx, to); err != nil {
			return err
		}
	}
	svc := mods.Metrics.Service()
	for _, tid := range tids {
		r, err := svc.Backfill(ctx, tid, from, to, keys)
		if err != nil {
			return fmt.Errorf("tenant %s: %w", tid, err)
		}
		snaps := 0
		for d := from; !metrics.Before(to, d) && metrics.Closed(d, svc.Clock.Now()); d = metrics.AddDays(d, 1) {
			n, err := svc.BuildSnapshots(ctx, tid, d)
			if err != nil {
				return fmt.Errorf("tenant %s ảnh chụp %s: %w", tid, d, err)
			}
			snaps += n
		}
		log.Info("backfill chỉ số", "event", "metrics.backfill", "tenant_id", tid, "from", from.String(), "to", to.String(),
			"inserted", r.Inserted, "superseded", r.Superseded, "unchanged", r.Unchanged, "no_data", r.NoData, "snapshots", snaps, "outcome", "ok")
	}
	return nil
}
