// Package migrations nhúng các tệp migration SQL viết tay (R-23).
//
// Thứ tự áp cho mỗi database: shared/ trước (bảng migration riêng
// schema_migrations_shared), rồi thư mục của cụm (pf/, mkg/, vault/, ts/). Bộ chạy v2 (W7.1,
// internal/shared/migrator, AG_MIGRATE_RUNNER=v2) giữ bố cục này, gắn mỗi tệp cho module và ghi sổ trong
// <schema>.schema_migrations của module (ADR-0038).
package migrations

import "embed"

// FS chứa mọi tệp *.sql theo thư mục cụm. tools/migratecheck đòi mọi thư mục
// con của migrations/ có mặt trong chỉ thị go:embed dưới đây.
//
//go:embed shared/*.sql pf/*.sql mkg/*.sql vault/*.sql ts/*.sql
var FS embed.FS

// Cluster là một database đích.
type Cluster string

// Các cụm. ts (chuỗi thời gian, database ag_ts) bổ sung ở G3 — ADR-0017.
const (
	PF    Cluster = "pf"
	MKG   Cluster = "mkg"
	Vault Cluster = "vault"
	// TS là cụm chuỗi thời gian: PostgreSQL 17 phân vùng khai báo theo tháng,
	// schema ts_weigh (số đo cân thô, G3) và ts_env (môi trường, dự phòng).
	// Không join với bảng nghiệp vụ (R-07): khác database.
	TS Cluster = "ts"
)

// Clusters liệt kê các cụm theo thứ tự áp.
var Clusters = []Cluster{PF, MKG, Vault, TS}
