package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// registerGauges đăng ký chỉ số nghiệp vụ bắt buộc có cảnh báo của ADR-0006 (mục 19):
//
//   - ag.sync.conflict.overdue: xung đột đồng bộ đang mở quá SLA (C-8), theo tenant — tìm
//     tenant bằng pool ag_ops_jobs của lịch (hai cột, migration pf 000043), đếm bằng
//     vai sync_app (R-05);
//   - ag.db.standby_replication_lag (giây): chỉ khi có AG_DB_STANDBY_URL;
//   - ag.db.ledger_default_rows: số dòng ước lượng trong phân vùng DEFAULT của mọi bảng phân vùng, theo
//     cụm của lịch (pool ag_ops_jobs, shared 000010) — > 0 nghĩa là từng thiếu phân vùng tháng.
//
// Lỗi đăng ký chỉ cảnh báo, không dừng worker. Trả hàm đóng pool đã mở.
func registerGauges(ctx context.Context, log *slog.Logger, direct string, opsPools map[migrations.Cluster]*pgxpool.Pool) func() {
	publisher := opsPools[migrations.PF]
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
	if len(opsPools) > 0 {
		byName := make(map[string]*pgxpool.Pool, len(opsPools))
		for c, p := range opsPools {
			byName[string(c)] = p
		}
		if _, err := opsjobs.RegisterDefaultPartitionRows(byName); err != nil {
			warn("phân vùng DEFAULT có dòng", err)
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
