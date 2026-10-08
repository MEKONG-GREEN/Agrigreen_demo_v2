package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/clusterenv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// eachCluster mở pool ag_migrator (nối thẳng, không PgBouncer) cho từng cụm.
func eachCluster(ctx context.Context, app string, fn func(c migrations.Cluster, pool *pgxpool.Pool) error) error {
	base, err := need("AG_DB_URL")
	if err != nil {
		return err
	}
	clusters, err := clusterenv.Selected()
	if err != nil {
		return err
	}
	for _, c := range clusters {
		dsn, err := dbadmin.RoleURL(clusterenv.BaseURL(c, base), c, "", dbadmin.Migrator)
		if err != nil {
			return err
		}
		pool, err := db.Open(ctx, dsn, db.Options{AppName: app})
		if err != nil {
			return err
		}
		err = fn(c, pool)
		pool.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", dbadmin.DatabaseName(c, ""), err)
		}
	}
	return nil
}

// maintain: tạo trước phân vùng tháng cho mọi bảng sổ. Chạy hằng tháng (CronJob)
// — idempotent. Xem docs/runbooks/partitions.md.
func maintain(ctx context.Context) error {
	months := 24
	if v := os.Getenv("AG_PARTITION_MONTHS_AHEAD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 120 {
			return fmt.Errorf("AG_PARTITION_MONTHS_AHEAD phải là số tháng 1..120")
		}
		months = n
	}
	warn := 0
	err := eachCluster(ctx, "migrate-maintain", func(c migrations.Cluster, pool *pgxpool.Pool) error {
		rs, err := opsjobs.EnsurePartitions(ctx, pool, months)
		if err != nil {
			return err
		}
		for _, r := range rs {
			switch {
			case !r.Partitioned:
				fmt.Printf("%s %s: không phân vùng — bỏ qua\n", c, r.Table)
			default:
				fmt.Printf("%s %s: tạo %d phân vùng (đủ %d tháng tới)\n", c, r.Table, r.Created, months)
			}
			if r.Moved > 0 {
				warn++
				fmt.Printf("CẢNH BÁO %s %s: đã chuyển %d dòng từ phân vùng DEFAULT sang phân vùng tháng — job maintain từng trễ, xem runbook\n", c, r.Table, r.Moved)
			}
			if r.DefaultRows > 0 {
				warn++
				fmt.Printf("CẢNH BÁO %s %s: phân vùng DEFAULT còn ~%d dòng (ước lượng thống kê) — xem runbook\n", c, r.Table, r.DefaultRows)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if warn > 0 {
		fmt.Printf("xong, %d cảnh báo phân vùng DEFAULT\n", warn)
	} else {
		fmt.Println("phân vùng: đủ")
	}
	return nil
}

// verifyChains: xác minh mọi chuỗi băm. Có chỗ đứt → lỗi (sự cố P1, mục 23).
// Xem docs/runbooks/verify-chains.md.
func verifyChains(ctx context.Context) error {
	broken, total := 0, 0
	err := eachCluster(ctx, "migrate-verify-chains", func(c migrations.Cluster, pool *pgxpool.Pool) error {
		cs, err := opsjobs.VerifyAllChains(ctx, pool)
		if err != nil {
			return err
		}
		for _, ch := range cs {
			total++
			if ch.OK {
				continue
			}
			broken++
			seq := "?"
			if ch.BrokenAtSeq != nil {
				seq = strconv.FormatInt(*ch.BrokenAtSeq, 10)
			}
			fmt.Printf("ĐỨT %s sổ=%s bảng=%s tenant=%s stream=%s seq=%s: %s\n",
				c, ch.Ledger, ch.Table, ch.TenantID, ch.StreamKey, seq, ch.Reason)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if broken > 0 {
		return fmt.Errorf("%d/%d chuỗi đứt — xử lý như sự cố P1 (docs/runbooks/verify-chains.md)", broken, total)
	}
	fmt.Printf("chuỗi băm: %d chuỗi hợp lệ\n", total)
	return nil
}

// purgeIdempotency: như job idempotency của worker (R-51) nhưng chạy một lần, cho vùng
// không có lịch worker (vd CronJob vùng mkg/vault ở Kubernetes). Vai ag_ops_jobs,
// nối thẳng. Dọn cả shared.idempotency_key (48 giờ) và shared.ledger_idem (30 ngày, W2.1)
// của từng cụm trong AG_CLUSTERS.
func purgeIdempotency(ctx context.Context) error {
	base, err := need("AG_DB_URL")
	if err != nil {
		return err
	}
	clusters, err := clusterenv.Selected()
	if err != nil {
		return err
	}
	const batch = 5000
	for _, c := range clusters {
		dsn, err := dbadmin.RoleURL(clusterenv.BaseURL(c, base), c, "", dbadmin.OpsJobs)
		if err != nil {
			return err
		}
		pool, err := db.Open(ctx, dsn, db.Options{AppName: "migrate-purge-idempotency", MaxConns: 2})
		if err != nil {
			return err
		}
		r, err := opsjobs.PurgeAllIdempotency(ctx, pool, batch)
		pool.Close()
		fmt.Printf("%s: xóa %d Idempotency-Key hết hạn, %d idem_key sổ quá 30 ngày\n", c, r.HTTPKeys, r.LedgerIdem)
		if err != nil {
			return fmt.Errorf("%s: %w", c, err)
		}
	}
	return nil
}
