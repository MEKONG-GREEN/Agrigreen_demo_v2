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
	"net/url"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // driver pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Role là một vai DB.
type Role struct {
	Name      string
	Login     bool
	BypassRLS bool
	MemberOf  []string
	// Clusters là các database vai được CONNECT.
	Clusters []migrations.Cluster
}

// Vai dùng chung.
const (
	Migrator        = "ag_migrator"
	SharedUser      = "ag_shared_user"
	OutboxPublisher = "ag_outbox_publisher"
)

// Roles là danh mục vai. Mỗi module một vai riêng chỉ có quyền trên schema của
// nó (R-05) — thêm module mới thì thêm vai ở đây và GRANT trong migration.
var Roles = []Role{
	{Name: SharedUser, Login: false},
	{Name: Migrator, Login: true, Clusters: migrations.Clusters},
	{Name: OutboxPublisher, Login: true, BypassRLS: true, Clusters: migrations.Clusters},
	// platform_api: chỉ bảng vận hành trong schema shared (idempotency) của cụm pf, không có quyền nghiệp vụ.
	{Name: "platform_api", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "worker_platform", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "party_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF}},
	{Name: "fieldops_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.MKG}},
	{Name: "vault_app", Login: true, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.Vault}},
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
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("dbadmin: kết nối quản trị: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// Khóa tư vấn mức phiên: tuần tự hóa bootstrap giữa các gói kiểm thử chạy song song.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(727001)"); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(727001)") //nolint:errcheck

	for _, r := range Roles {
		if err := ensureRole(ctx, conn, r); err != nil {
			return err
		}
	}
	for _, c := range migrations.Clusters {
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
		for _, r := range Roles {
			for _, rc := range r.Clusters {
				if rc == c && r.Login {
					if _, err := conn.Exec(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", ident(name), ident(r.Name))); err != nil {
						return err
					}
				}
			}
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
	for _, m := range r.MemberOf {
		if _, err := conn.Exec(ctx, fmt.Sprintf("GRANT %s TO %s", ident(m), ident(r.Name))); err != nil {
			return err
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

// Migrate áp migration shared/ rồi migration của cụm, bằng vai ag_migrator.
func Migrate(baseURL string, c migrations.Cluster, dbSuffix string) error {
	dsn, err := RoleURL(baseURL, c, dbSuffix, Migrator)
	if err != nil {
		return err
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
