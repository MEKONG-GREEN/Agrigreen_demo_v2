package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// registerGauges đăng ký chỉ số nghiệp vụ bắt buộc có cảnh báo của ADR-0006 (mục 19):
//
//   - ag.sync.conflict.overdue: xung đột đồng bộ đang mở quá SLA (C-8), theo tenant — tìm
//     tenant bằng pool ag_outbox_publisher của lịch (hai cột, migration pf 000043), đếm bằng
//     vai sync_app (R-05);
//   - ag.db.standby_replication_lag (giây): chỉ khi có AG_DB_STANDBY_URL.
//
// Lỗi đăng ký chỉ cảnh báo, không dừng worker. Trả hàm đóng pool đã mở.
func registerGauges(ctx context.Context, log *slog.Logger, direct string, publisher *pgxpool.Pool) func() {
	var pools []*pgxpool.Pool
	closeFn := func() {
		for _, p := range pools {
			p.Close()
		}
	}
	warn := func(what string, err error) {
		log.Warn("không đăng ký được chỉ số "+what, "event", "startup", "outcome", "error", "error", err.Error())
	}
	if publisher != nil {
		sp, err := openPF(ctx, direct, syncmod.DBRole, &closeFn)
		if err != nil {
			warn("xung đột đồng bộ quá SLA", err)
		} else if _, err := syncmod.RegisterConflictSLAMetrics(sp, clock.System{}, syncmod.ConflictTenants(publisher)); err != nil {
			warn("xung đột đồng bộ quá SLA", err)
		}
	}
	if dsn := os.Getenv(db.EnvStandbyURL); dsn != "" {
		sb, err := db.Open(ctx, dsn, db.Options{AppName: "worker-standby-lag", MaxConns: 1})
		if err != nil {
			warn("độ trễ standby", err)
			return closeFn
		}
		pools = append(pools, sb)
		if _, err := db.RegisterStandbyLag(sb, string(migrations.PF)); err != nil {
			warn("độ trễ standby", err)
		}
	}
	return closeFn
}
