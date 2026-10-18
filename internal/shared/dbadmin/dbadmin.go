// Package dbadmin dựng vai, database và chạy migration.
//
// Bootstrap chạy bằng tài khoản quản trị cụm PostgreSQL (một lần cho mỗi môi
// trường). Migration chạy bằng vai ag_migrator — không bao giờ bằng vai ứng
// dụng (R-25), không bao giờ bằng superuser.
package dbadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // driver pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/migrator"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Role là một vai DB.
type Role struct {
	Name      string
	Login     bool
	BypassRLS bool
	// NoInherit: vai không thừa hưởng quyền của vai nó là thành viên (ADR-0028: vai đăng nhập app_<đơn vị>
	// chỉ có quyền sau SET LOCAL ROLE r_<module>). Membership được cấp WITH INHERIT FALSE, SET TRUE.
	NoInherit bool
	MemberOf  []string
	// Settings: tham số mặc định của vai (ALTER ROLE … SET), vd app_agg chỉ đọc (ADR-0031 §3).
	Settings map[string]string
	// Clusters là các database vai được CONNECT.
	Clusters []migrations.Cluster
}

// Vai dùng chung.
const (
	Migrator        = "ag_migrator"
	SharedUser      = "ag_shared_user"
	OutboxPublisher = "ag_outbox_publisher"
	// OpsJobs là vai của job vận hành xuyên tenant (neo chuỗi, dọn idempotency, tìm tenant cho job định kỳ,
	// đếm phân vùng DEFAULT) — tách khỏi ag_outbox_publisher (shared 000009, pf 000114, mkg 000034): BYPASSRLS
	// nhưng chỉ có quyền migration cấp theo CỘT (tenant_id + cột trạng thái) và EXECUTE hàm vận hành.
	OpsJobs = "ag_ops_jobs"
	// Vai giám sát vận hành (G3, mục 19) và chủ hàm SECURITY DEFINER của chúng.
	MKGMonitor      = "mkg_monitor"
	TSMonitor       = "ts_monitor"
	MKGMonitorOwner = "mkg_monitor_owner"
	TSMonitorOwner  = "ts_monitor_owner"
)

// Roles là danh mục vai đầy đủ theo thứ tự tạo (vai nhóm trước thành viên): baseRoles + vai nhóm r_<module>
// + vai đăng nhập app_<đơn vị> (ADR-0028, modroles.go).
var Roles = buildRoles()

// baseRoles là danh mục vai g1–g5. Mỗi module một vai đăng nhập <module>_app chỉ có quyền trên schema của
// nó (R-05) — kỳ chuyển ADR-0028: vai này là thành viên r_<module> (thêm tự động ở buildRoles); module mới
// GRANT cho r_<module> trong migration (docs/dev/modules.md).
var baseRoles = []Role{
	{Name: SharedUser, Login: false},
	// Chủ các hàm giám sát SECURITY DEFINER (mkg 000013, ts 000003): NOLOGIN, BYPASSRLS để đếm xuyên
	// tenant, chỉ được SELECT theo CỘT vận hành do migration cấp. Tạo trước ag_migrator vì ag_migrator
	// là thành viên (cần để ALTER FUNCTION … OWNER TO); không thêm quyền bảng nào ag_migrator chưa có.
	{Name: MKGMonitorOwner, Login: false, BypassRLS: true},
	{Name: TSMonitorOwner, Login: false, BypassRLS: true},
	{Name: Migrator, Login: true, MemberOf: []string{MKGMonitorOwner, TSMonitorOwner}, Clusters: migrations.Clusters},
	// ag_outbox_publisher: chỉ shared.outbox (đọc xuyên tenant, sửa cột trạng thái) — cmd/outbox-publisher.
	{Name: OutboxPublisher, Login: true, BypassRLS: true, Clusters: migrations.Clusters},
	// ag_ops_jobs: worker (lịch), cmd/migrate anchor|purge-idempotency, api-internal (job mkg), rebuilddrill.
	{Name: OpsJobs, Login: true, BypassRLS: true, Clusters: migrations.Clusters},
	// platform_api: chỉ bảng vận hành trong schema shared (idempotency) của cụm pf, không có quyền nghiệp vụ.
	{Name: "platform_api", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "worker_platform", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "party_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	// Module G1 của cụm pf (docs/dev/g1-conventions.md).
	{Name: "masterdata_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "geo_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "extension_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "content_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "mechanization_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "mrv_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "sync_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	// Đợt hoàn thiện G1.
	{Name: "accounts_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "notify_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "reports_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	// G2 (docs/dev/g2-conventions.md).
	{Name: "cropcycle_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "waterway_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "simulation_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "fieldops_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	{Name: "hub_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	// mkg_api: tương đương platform_api ở cụm mkg — chỉ bảng vận hành trong schema shared
	// (idempotency) cho api-internal, không có quyền nghiệp vụ (Q-MKG-07).
	{Name: "mkg_api", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	{Name: "vault_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.Vault}},
	// G3 (docs/dev/g3-conventions.md §3).
	// mkg/measurement: vai module (sổ phiếu cân, đối soát, tranh chấp, dung sai, sổ thiết bị).
	{Name: "measurement_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	// cmd/ingest phía cụm mkg: đọc sổ thiết bị đã duyệt, ghi phiếu cân dẫn xuất (R-70) — quyền do migration measurement cấp.
	{Name: "ingest_mkg", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	// cmd/worker-internal: consumer của vhost mkg.internal — chỉ shared.inbox (qua ag_shared_user) + quyền module cấp thêm.
	{Name: "worker_internal", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	// cmd/ingest phía cụm ts: INSERT + SELECT trên ts_weigh (migration ts/000001), không UPDATE/DELETE.
	{Name: "ingest_ts", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.TS}},
	// Đọc chuỗi thời gian (tích phân lại, bằng chứng tranh chấp, xuất tầng lạnh): chỉ SELECT.
	{Name: "ts_reader", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.TS}},
	// pf/metrics: định nghĩa chỉ số, giá trị theo ngày, ảnh chụp + watermark, nhật ký lớp (GIS FN-18..21).
	{Name: "metrics_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	// G4 (docs/dev/g4-conventions.md §3): bốn module mkg mới — mỗi module một vai, chỉ CONNECT ag_mkg;
	// schema + USAGE do mkg 000014 tạo, bảng và GRANT do migration của từng module.
	{Name: "logistics_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	{Name: "orders_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	{Name: "costing_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	{Name: "acctbridge_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	// G5 (docs/dev/g5-conventions.md §3; ADR-0022..0024): Public API cho bên thứ ba. Mỗi module một vai, chỉ
	// CONNECT ag_pf; schema + USAGE do pf 000080, bảng và GRANT do migration của từng module (000081, 000082).
	{Name: "apiclient_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "metering_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	// public_api: tiến trình cmd/api-public — như platform_api, chỉ bảng vận hành schema shared (idempotency,
	// outbox), không quyền nghiệp vụ.
	{Name: "public_api", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	// sql_exporter (mục 19): chỉ EXECUTE hàm gộp trong schema mkg_monitor / ts_monitor — không SELECT bảng
	// gốc, không thuộc ag_shared_user, NOBYPASSRLS (deploy/observability/sql-exporter).
	{Name: MKGMonitor, Login: true, Clusters: []migrations.Cluster{migrations.MKG}},
	{Name: TSMonitor, Login: true, Clusters: []migrations.Cluster{migrations.TS}},
}

// PasswordFor đọc mật khẩu của vai từ AG_DB_PASSWORD_<VAI>, rồi AG_DB_APP_PASSWORD.
// Không có mật khẩu mặc định trong mã nguồn.
func PasswordFor(role string) (string, error) {
	if v := os.Getenv("AG_DB_PASSWORD_" + strings.ToUpper(role)); v != "" {
		return v, nil
	}
	if v := os.Getenv("AG_DB_APP_PASSWORD"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("dbadmin: chưa đặt AG_DB_PASSWORD_%s hoặc AG_DB_APP_PASSWORD", strings.ToUpper(role))
}

func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func ident(s string) string { return pgx.Identifier{s}.Sanitize() }

// DatabaseName trả tên database của cụm, vd ag_pf hoặc ag_pf_<suffix> khi kiểm thử.
func DatabaseName(c migrations.Cluster, suffix string) string {
	if suffix == "" {
		return "ag_" + string(c)
	}
	return "ag_" + string(c) + "_" + suffix
}

// Bootstrap tạo (hoặc cập nhật) mọi vai và database. adminURL là tài khoản
// quản trị cụm. Chạy lặp an toàn; nhiều tiến trình kiểm thử chạy song song được.
func Bootstrap(ctx context.Context, adminURL, dbSuffix string) error {
	return BootstrapClusters(ctx, adminURL, dbSuffix, migrations.Clusters)
}

// RolesFor trả các vai cần có khi chỉ dựng các cụm clusters (AG_CLUSTERS, G4 tách đơn vị triển khai theo vùng —
// RB-5): vai nhóm NOLOGIN không gắn cụm (ag_shared_user, chủ hàm giám sát — đích MemberOf của vai khác) và mọi vai
// được CONNECT ít nhất một cụm đã chọn. Giữ thứ tự của Roles (vai nhóm trước thành viên).
func RolesFor(clusters []migrations.Cluster) []Role {
	return rolesFor(Roles, clusters)
}

func rolesFor(roles []Role, clusters []migrations.Cluster) []Role {
	want := map[migrations.Cluster]bool{}
	for _, c := range clusters {
		want[c] = true
	}
	managed := make(map[string]bool, len(roles))
	included := make(map[string]bool, len(roles))
	var out []Role
	for _, r := range roles {
		managed[r.Name] = true
		keep := len(r.Clusters) == 0
		for _, c := range r.Clusters {
			keep = keep || want[c]
		}
		if keep {
			r.MemberOf = slices.Clone(r.MemberOf)
			out = append(out, r)
			included[r.Name] = true
		}
	}
	// Chỉ cấp membership tới vai được dựng. Vai bên ngoài danh mục (vd vai PostgreSQL)
	// giữ nguyên; không sửa danh mục toàn cục hay thu hồi quyền của lần bootstrap trước.
	for i := range out {
		out[i].MemberOf = slices.DeleteFunc(out[i].MemberOf, func(parent string) bool {
			return managed[parent] && !included[parent]
		})
	}
	return out
}

// BootstrapClusters như Bootstrap nhưng chỉ tạo vai và database của các cụm clusters (cmd/migrate bootstrap
// đọc AG_CLUSTERS qua cmd/internal/clusterenv; rỗng = mọi cụm — hành vi cũ). Vai nhiều cụm (ag_migrator,
// ag_outbox_publisher, ag_ops_jobs) chỉ được GRANT CONNECT trên database của cụm đã chọn.
func BootstrapClusters(ctx context.Context, adminURL, dbSuffix string, clusters []migrations.Cluster) error {
	if len(clusters) == 0 {
		return errors.New("dbadmin: không có cụm nào để dựng")
	}
	roles := RolesFor(clusters)
	conn, err := connectRetry(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("dbadmin: kết nối quản trị: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// Khóa tư vấn mức phiên: tuần tự hóa bootstrap giữa các gói kiểm thử chạy song song.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(727001)"); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(727001)") //nolint:errcheck

	for _, r := range roles {
		if err := ensureRole(ctx, conn, r); err != nil {
			return err
		}
	}
	if err := revokeStrayMembers(ctx, conn, roles); err != nil {
		return err
	}
	for _, c := range clusters {
		name := DatabaseName(c, dbSuffix)
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER %s", ident(name), ident(Migrator))); err != nil {
				return fmt.Errorf("dbadmin: tạo database %s: %w", name, err)
			}
		}
		if _, err := conn.Exec(ctx, fmt.Sprintf("REVOKE ALL ON DATABASE %s FROM PUBLIC", ident(name))); err != nil {
			return err
		}
		for _, r := range roles {
			for _, rc := range r.Clusters {
				if rc == c && r.Login {
					if _, err := conn.Exec(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", ident(name), ident(r.Name))); err != nil {
						return err
					}
				}
			}
		}
		if err := ensureExtensions(ctx, adminURL, c, name); err != nil {
			return err
		}
	}
	return nil
}

// Extensions là extension cần quyền quản trị cụm (không phải trusted extension),
// tạo ở Bootstrap vì ag_migrator không phải superuser. Migration chỉ giả định
// chúng đã có (CREATE EXTENSION IF NOT EXISTS trong migration là no-op).
var Extensions = map[migrations.Cluster][]string{
	// pgrouting: pf/waterway (G2). btree_gist: EXCLUDE trên pf_masterdata.admin_unit (pf 000089,
	// ADR-0032) — trusted extension nên migration tự tạo được, liệt kê ở đây cho đủ danh sách cụm.
	migrations.PF:  {"postgis", "pgrouting", "btree_gist"},
	migrations.MKG: {"postgis"},
	// migrations.TS: không cần extension — PostgreSQL 17 phân vùng khai báo thuần,
	// TimescaleDB TSL không dùng (ADR-0017, RB-4).
}

func ensureExtensions(ctx context.Context, adminURL string, c migrations.Cluster, dbName string) error {
	exts := Extensions[c]
	if len(exts) == 0 {
		return nil
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		return err
	}
	u.Path = "/" + dbName
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		return fmt.Errorf("dbadmin: kết nối %s: %w", dbName, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, e := range exts {
		if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS "+ident(e)); err != nil {
			return fmt.Errorf("dbadmin: tạo extension %s trong %s: %w", e, dbName, err)
		}
	}
	return nil
}

func ensureRole(ctx context.Context, conn *pgx.Conn, r Role) error {
	attrs := []string{"NOLOGIN"}
	if r.Login {
		pw, err := PasswordFor(r.Name)
		if err != nil {
			return err
		}
		attrs = []string{"LOGIN", "PASSWORD " + quoteLiteral(pw)}
	}
	if r.BypassRLS {
		attrs = append(attrs, "BYPASSRLS")
	} else {
		attrs = append(attrs, "NOBYPASSRLS")
	}
	if r.NoInherit {
		attrs = append(attrs, "NOINHERIT")
	} else {
		attrs = append(attrs, "INHERIT")
	}
	attrs = append(attrs, "NOSUPERUSER", "NOCREATEDB", "NOCREATEROLE")

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", r.Name).Scan(&exists); err != nil {
		return err
	}
	verb := "ALTER"
	if !exists {
		verb = "CREATE"
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("%s ROLE %s %s", verb, ident(r.Name), strings.Join(attrs, " "))); err != nil {
		return fmt.Errorf("dbadmin: %s ROLE %s: %w", verb, r.Name, err)
	}
	// PostgreSQL 16+: tùy chọn INHERIT/SET của từng membership; ghi rõ để chạy lặp cập nhật được grant cũ
	// (NOINHERIT của vai chỉ là mặc định cho grant MỚI).
	inherit := "TRUE"
	if r.NoInherit {
		inherit = "FALSE"
	}
	for _, m := range r.MemberOf {
		if _, err := conn.Exec(ctx, fmt.Sprintf("GRANT %s TO %s WITH INHERIT %s, SET TRUE", ident(m), ident(r.Name), inherit)); err != nil {
			return fmt.Errorf("dbadmin: GRANT %s TO %s: %w", m, r.Name, err)
		}
	}
	keys := make([]string, 0, len(r.Settings))
	for k := range r.Settings {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER ROLE %s SET %s = %s", ident(r.Name), ident(k), quoteLiteral(r.Settings[k]))); err != nil {
			return fmt.Errorf("dbadmin: ALTER ROLE %s SET %s: %w", r.Name, k, err)
		}
	}
	return nil
}

// revokeStrayMembers thu hồi thành viên không được phép của vai nhóm đặc quyền (ExclusiveMembers) — chỉ khi vai
// nhóm thuộc các vai đang dựng. Chạy lặp an toàn.
func revokeStrayMembers(ctx context.Context, conn *pgx.Conn, roles []Role) error {
	for grp, allowed := range ExclusiveMembers {
		if !slices.ContainsFunc(roles, func(r Role) bool { return r.Name == grp }) {
			continue
		}
		rows, err := conn.Query(ctx, `SELECT m.rolname FROM pg_auth_members a JOIN pg_roles g ON g.oid = a.roleid
		    JOIN pg_roles m ON m.oid = a.member WHERE g.rolname = $1 ORDER BY 1`, grp)
		if err != nil {
			return err
		}
		members, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, m := range members {
			if slices.Contains(allowed, m) {
				continue
			}
			if _, err := conn.Exec(ctx, fmt.Sprintf("REVOKE %s FROM %s", ident(grp), ident(m))); err != nil {
				return fmt.Errorf("dbadmin: REVOKE %s FROM %s: %w", grp, m, err)
			}
		}
	}
	return nil
}

// RoleURL dựng URL kết nối cho một vai từ URL gốc (chỉ lấy host, cổng, tham số).
func RoleURL(baseURL string, c migrations.Cluster, dbSuffix, role string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("dbadmin: URL không hợp lệ: %w", err)
	}
	pw, err := PasswordFor(role)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(role, pw)
	u.Path = "/" + DatabaseName(c, dbSuffix)
	return u.String(), nil
}

// EnvRunner chọn bộ chạy migration (W7.1, ADR-0038): "v2" = internal/shared/migrator (sổ theo module, thứ tự
// topo); rỗng hoặc "v1" = golang-migrate (mặc định trong kỳ chuyển một bản phát hành).
const EnvRunner = "AG_MIGRATE_RUNNER"

// Runner trả bộ chạy đã chọn qua AG_MIGRATE_RUNNER ("v1" hoặc "v2").
func Runner() (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvRunner))); v {
	case "", "v1":
		return "v1", nil
	case "v2":
		return "v2", nil
	default:
		return "", fmt.Errorf("dbadmin: %s=%q không hợp lệ (v1, v2)", EnvRunner, v)
	}
}

// Migrate áp migration shared/ rồi migration của cụm, bằng vai ag_migrator, theo bộ chạy AG_MIGRATE_RUNNER.
func Migrate(baseURL string, c migrations.Cluster, dbSuffix string) error {
	r, err := Runner()
	if err != nil {
		return err
	}
	if r == "v2" {
		return MigrateV2(context.Background(), baseURL, c, dbSuffix, nil)
	}
	return MigrateV1(baseURL, c, dbSuffix)
}

// MigrateV2 áp migration bằng bộ chạy v2 (internal/shared/migrator). log có thể nil.
func MigrateV2(ctx context.Context, baseURL string, c migrations.Cluster, dbSuffix string, log func(string, ...any)) error {
	dsn, err := RoleURL(baseURL, c, dbSuffix, Migrator)
	if err != nil {
		return err
	}
	if c == migrations.PF {
		repaired, err := repairMainPF151(ctx, dsn)
		if err != nil {
			return err
		}
		if repaired {
			log("%s: schema_migrations_pf 151 (tệp của main) → 136 để áp pf 137–151 của dev", DatabaseName(c, dbSuffix))
		}
	}
	if _, err := migrator.Up(ctx, dsn, c, migrator.Options{Log: log}); err != nil {
		return fmt.Errorf("dbadmin: migration v2 → %s: %w", DatabaseName(c, dbSuffix), err)
	}
	return nil
}

// MigrateV1 áp migration shared/ rồi migration của cụm bằng golang-migrate (bộ chạy cũ).
func MigrateV1(baseURL string, c migrations.Cluster, dbSuffix string) error {
	dsn, err := RoleURL(baseURL, c, dbSuffix, Migrator)
	if err != nil {
		return err
	}
	if c == migrations.PF {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		repaired, err := repairMainPF151(ctx, dsn)
		cancel()
		if err != nil {
			return err
		}
		if repaired {
			slog.Info("schema_migrations_pf 151 (tệp của main) → 136 để áp pf 137–151 của dev", "database", DatabaseName(c, dbSuffix))
		}
	}
	for _, step := range []struct{ dir, table string }{
		{"shared", "schema_migrations_shared"},
		{string(c), "schema_migrations_" + string(c)},
	} {
		if err := runDir(dsn, step.dir, step.table); err != nil {
			return fmt.Errorf("dbadmin: migration %s → %s: %w", step.dir, DatabaseName(c, dbSuffix), err)
		}
	}
	return nil
}

func runDir(dsn, dir, table string) error {
	src, err := iofs.New(migrations.FS, dir)
	if err != nil {
		return err
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	u.Scheme = "pgx5"
	q := u.Query()
	q.Set("x-migrations-table", table)
	u.RawQuery = q.Encode()
	m, err := migrate.NewWithSourceInstance("iofs", src, u.String())
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// DropDatabases xóa các database kiểm thử có hậu tố — chỉ dùng trong kiểm thử.
func DropDatabases(ctx context.Context, adminURL, dbSuffix string) error {
	if dbSuffix == "" {
		return errors.New("dbadmin: từ chối xóa database không có hậu tố kiểm thử")
	}
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, c := range migrations.Clusters {
		if _, err := conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", ident(DatabaseName(c, dbSuffix)))); err != nil {
			return err
		}
	}
	return nil
}

// connectRetry nối tới cụm, thử lại tối đa ~30 giây: lúc vừa khởi tạo, PostgreSQL
// có thể đang khởi động lại (image chính thức chạy server tạm rồi mới chạy thật).
func connectRetry(ctx context.Context, dsn string) (*pgx.Conn, error) {
	var last error
	for i := 0; i < 15; i++ {
		conn, err := pgx.Connect(ctx, dsn)
		if err == nil {
			return conn, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, last
}
