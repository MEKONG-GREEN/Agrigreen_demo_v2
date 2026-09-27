// Package db mở pool kết nối cho từng vai DB của module (R-05).
package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options điều chỉnh pool.
type Options struct {
	// ViaPgBouncer: đi qua PgBouncer transaction mode. Dùng QueryExecModeExec
	// (câu lệnh không tên, gửi kèm Bind/Execute) để không phụ thuộc prepared
	// statement có tên giữa các giao dịch.
	ViaPgBouncer bool
	MaxConns     int32
	AppName      string
}

// OptionsFromEnv đọc AG_DB_VIA_PGBOUNCER.
func OptionsFromEnv(appName string) Options {
	return Options{ViaPgBouncer: os.Getenv("AG_DB_VIA_PGBOUNCER") == "1", AppName: appName}
}

// Open mở pool và kiểm kết nối.
func Open(ctx context.Context, dsn string, o Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: DSN không hợp lệ: %w", err)
	}
	if o.ViaPgBouncer {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	}
	if o.MaxConns > 0 {
		cfg.MaxConns = o.MaxConns
	}
	if o.AppName != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = o.AppName
	}
	// UTC ở mức kết nối: timestamptz đọc ra luôn là UTC (R-12).
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: không kết nối được: %w", err)
	}
	return pool, nil
}
