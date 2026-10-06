// Package db mở pool kết nối theo vai DB (R-05) và chuyển giao dịch sang vai của module bằng SET LOCAL ROLE
// (ADR-0028, module.go).
package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Options điều chỉnh pool.
type Options struct {
	// ViaPgBouncer: đi qua PgBouncer transaction mode. Dùng QueryExecModeExec
	// (câu lệnh không tên, gửi kèm Bind/Execute) để không phụ thuộc prepared
	// statement có tên giữa các giao dịch.
	ViaPgBouncer bool
	MaxConns     int32
	AppName      string
	// StatementTimeout: statement_timeout mặc định của mọi phiên trong pool (0 = không đặt, giữ mặc định
	// của máy chủ/vai). Chỉ áp dụng khi kết nối THẲNG Postgres (tham số khởi động): PgBouncer transaction
	// mode không theo dõi statement_timeout — tham số khởi động bị từ chối hoặc bỏ qua (ignore_startup_parameters)
	// — nên ViaPgBouncer = true thì trường này không có tác dụng; giới hạn phía đó đặt ở mức vai DB
	// (ALTER ROLE … SET statement_timeout) hoặc SET LOCAL trong giao dịch.
	StatementTimeout time.Duration

	envErr error // lỗi cú pháp biến môi trường của OptionsFromEnv — Open trả lại (lỗi khởi động rõ ràng)
}

// DefaultStatementTimeout: statement_timeout mặc định của pool ứng dụng (OptionsFromEnv) — câu SQL treo
// (khóa chờ mãi, kế hoạch tồi) bị Postgres hủy thay vì giữ kết nối và giao dịch vô hạn.
const DefaultStatementTimeout = 2 * time.Minute

// OptionsFromEnv đọc AG_DB_VIA_PGBOUNCER và AG_DB_STATEMENT_TIMEOUT (Go duration, vd 30s, 2m; "0" = không
// đặt; chưa đặt = DefaultStatementTimeout). Giá trị sai cú pháp → db.Open trả lỗi nêu tên biến.
func OptionsFromEnv(appName string) Options {
	o := Options{ViaPgBouncer: os.Getenv("AG_DB_VIA_PGBOUNCER") == "1", AppName: appName}
	o.StatementTimeout, o.envErr = statementTimeoutFromEnv(os.Getenv("AG_DB_STATEMENT_TIMEOUT"))
	return o
}

func statementTimeoutFromEnv(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	switch v {
	case "":
		return DefaultStatementTimeout, nil
	case "0":
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("db: AG_DB_STATEMENT_TIMEOUT không hợp lệ (%q): cần Go duration ≥ 0, vd 30s, 2m", v)
	}
	return d, nil
}

// Open mở pool và kiểm kết nối.
func Open(ctx context.Context, dsn string, o Options) (*pgxpool.Pool, error) {
	if o.envErr != nil {
		return nil, o.envErr
	}
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
	// statement_timeout là tham số khởi động (mức phiên, không cần SET — ADR-0028 cấm SET phiên trong mã):
	// chỉ khi kết nối thẳng (xem Options.StatementTimeout). DSN đã có statement_timeout thì tôn trọng DSN.
	if o.StatementTimeout > 0 && !o.ViaPgBouncer {
		if _, ok := cfg.ConnConfig.RuntimeParams["statement_timeout"]; !ok {
			cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(o.StatementTimeout.Milliseconds(), 10)
		}
	}
	// Span SQL (không ghi tham số — R-14) khi OpenTelemetry bật; gọi telemetry.Setup trước db.Open.
	if telemetry.Enabled() {
		cfg.ConnConfig.Tracer = telemetry.PgxTracer()
	}
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
