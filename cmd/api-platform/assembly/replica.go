package assembly

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ReplicaEnv là biến môi trường URL gốc (host, cổng, sslmode — như AG_PF_DB_URL/AG_DB_URL) của bản sao chỉ đọc
// (hot standby) cụm pf cho truy vấn phân tích (R-DB-10 §22.1, W6.3). Vai và mật khẩu ghép như primary
// (dbadmin.RoleURL: vai đăng nhập đơn vị app_*, AG_DB_PASSWORD_<VAI>/AG_DB_APP_PASSWORD). Không đặt → primary.
const ReplicaEnv = "AG_DB_REPLICA_URL"

// ReplicaOpenerFromEnv trả PoolOpener của bản sao chỉ đọc (nil khi AG_DB_REPLICA_URL rỗng); opts như pool primary
// của đơn vị (PgBouncer, statement_timeout), AppName thêm hậu tố -replica. pf/metrics chỉ mở giao dịch READ ONLY
// trên pool này.
func ReplicaOpenerFromEnv(getenv func(string) string, opts db.Options) PoolOpener {
	base := strings.TrimSpace(getenv(ReplicaEnv))
	if base == "" {
		return nil
	}
	return func(ctx context.Context, role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		o := opts
		o.AppName += "-replica"
		return db.Open(ctx, dsn, o)
	}
}
