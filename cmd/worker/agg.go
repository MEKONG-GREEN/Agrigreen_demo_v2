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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// EnvAggGovernance là tenant quản trị giữ danh mục job tổng hợp, quyết định duyệt và nhật ký chạy (ADR-0031 §4–§5).
const EnvAggGovernance = "AG_AGG_GOVERNANCE_TENANT"

// aggPoolMaxConns: pool app_agg nhỏ — job chạy tuần tự, mỗi lần một giao dịch chỉ đọc.
const aggPoolMaxConns = 2

// openAggPools mở hai pool của bộ chạy job tổng hợp (ADR-0031 §3, gói FC): app_worker nối thẳng (r_metrics: danh
// mục, kiểm duyệt, nhật ký) và pool NHỎ vai đăng nhập riêng app_agg (r_agg) — trên bản sao chỉ đọc AG_DB_REPLICA_URL
// khi có, không thì nối thẳng cụm chính (vai app_agg mặc định chỉ đọc + statement_timeout).
func openAggPools(ctx context.Context, getenv func(string) string) (worker, agg *pgxpool.Pool, err error) {
	direct := getenv("AG_DB_DIRECT_URL")
	if direct == "" {
		direct = getenv("AG_DB_URL")
	}
	if direct == "" {
		return nil, nil, errors.New("thiếu AG_DB_URL")
	}
	aggBase := strings.TrimSpace(getenv(assembly.ReplicaEnv))
	if aggBase == "" {
		aggBase = direct
	}
	wdsn, err := dbadmin.RoleURL(direct, migrations.PF, "", dbadmin.AppWorker)
	if err != nil {
		return nil, nil, err
	}
	adsn, err := dbadmin.RoleURL(aggBase, migrations.PF, "", dbadmin.AppAgg)
	if err != nil {
		return nil, nil, err
	}
	if worker, err = db.Open(ctx, wdsn, db.Options{AppName: "worker-agg", MaxConns: aggPoolMaxConns}); err != nil {
		return nil, nil, err
	}
	if agg, err = db.Open(ctx, adsn, db.Options{AppName: "worker-agg-reader", MaxConns: aggPoolMaxConns}); err != nil {
		worker.Close()
		return nil, nil, err
	}
	return worker, agg, nil
}

// runAggJob chạy MỘT job tổng hợp đã khai (metrics.AggJobs) rồi thoát — `worker -agg-run <job> -agg-actor <uuid>`.
// Chỉ job có phiên bản + định nghĩa đúng phiên bản đã duyệt bốn mắt mới nhất mới đổi được vai; mọi lần chạy ghi nhật
// ký pf_metrics.agg_run. Log chỉ có số nhóm (không chi tiết). Chạy theo lịch ở production chờ C-4 (ADR-0031 §7).
func runAggJob(log *slog.Logger, jobID, actorArg string, k int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	idx := -1
	for i := range metrics.AggJobs {
		if metrics.AggJobs[i].ID == jobID {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("-agg-run: job %q chưa khai trong mã", jobID)
	}
	actor, err := uuid.Parse(actorArg)
	if err != nil {
		return fmt.Errorf("-agg-actor: %w", err)
	}
	gov, err := uuid.Parse(os.Getenv(EnvAggGovernance))
	if err != nil {
		return fmt.Errorf("%s: %w", EnvAggGovernance, err)
	}
	worker, agg, err := openAggPools(ctx, os.Getenv)
	if err != nil {
		return err
	}
	defer worker.Close()
	defer agg.Close()
	runner := metrics.Agg(clock.System{}, metrics.AggDeps{DB: worker, AggDB: agg, Governance: gov})
	res, err := runner.Run(ctx, metrics.AggJobs[idx], actor, k)
	if err != nil {
		log.Error("job tổng hợp", "event", "metrics.agg_run", "job", jobID, "run_id", res.RunID, "outcome", "error", "error", err.Error())
		return err
	}
	log.Info("job tổng hợp", "event", "metrics.agg_run", "job", jobID, "version", res.Version, "run_id", res.RunID, "k", res.KUsed,
		"tenants", res.Tenants, "rows", res.Rows, "groups", len(res.Groups), "groups_suppressed", res.GroupsSuppressed, "outcome", "ok")
	return nil
}
