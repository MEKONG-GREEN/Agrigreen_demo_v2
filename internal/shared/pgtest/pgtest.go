// Package pgtest dựng database PostgreSQL thật cho kiểm thử tích hợp.
//
// Mỗi gói kiểm thử có bộ database riêng (ag_pf_<hậu tố>, ag_mkg_<hậu tố>,
// ag_vault_<hậu tố>) đã chạy migration, xóa khi xong.
//
// Biến môi trường:
//
//	AG_TEST_PG_ADMIN_URL   tài khoản quản trị cụm, vd postgres://postgres:***@localhost:5432/postgres
//	AG_TEST_PGBOUNCER_URL  (tùy chọn) PgBouncer transaction mode; pool vai ứng dụng đi qua đây (R-32)
//	AG_DB_APP_PASSWORD     mật khẩu vai ứng dụng trong môi trường kiểm thử
//
// Thiếu AG_TEST_PG_ADMIN_URL: bỏ qua trên máy dev, HỎNG khi CI=true.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Env là bộ database của một gói kiểm thử.
type Env struct {
	AdminURL   string
	DirectURL  string // URL gốc tới PostgreSQL
	PoolURL    string // URL gốc cho vai ứng dụng: PgBouncer nếu có, không thì DirectURL
	Suffix     string
	ViaBouncer bool

	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
}

var (
	current *Env
	initErr error
)

// Run dùng trong TestMain: dựng database, chạy kiểm thử, dọn dẹp.
func Run(m *testing.M) int {
	admin := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if admin != "" {
		current, initErr = setup(admin)
	}
	code := m.Run()
	if current != nil {
		current.closePools()
		if err := dbadmin.DropDatabases(context.Background(), admin, current.Suffix); err != nil {
			fmt.Fprintln(os.Stderr, "pgtest: không xóa được database kiểm thử:", err)
		}
	}
	return code
}

func setup(admin string) (*Env, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	e := &Env{AdminURL: admin, DirectURL: admin, PoolURL: admin, Suffix: "t" + hex.EncodeToString(b), pools: map[string]*pgxpool.Pool{}}
	if pb := os.Getenv("AG_TEST_PGBOUNCER_URL"); pb != "" {
		e.PoolURL, e.ViaBouncer = pb, true
	}
	ctx := context.Background()
	if err := dbadmin.Bootstrap(ctx, admin, e.Suffix); err != nil {
		return nil, err
	}
	for _, c := range migrations.Clusters {
		if err := dbadmin.Migrate(e.DirectURL, c, e.Suffix); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// Get trả Env của gói hiện tại, hoặc bỏ qua/hỏng kiểm thử khi không có PostgreSQL.
func Get(t testing.TB) *Env {
	t.Helper()
	if initErr != nil {
		t.Fatalf("pgtest: dựng database thất bại: %v", initErr)
	}
	if current == nil {
		if os.Getenv("CI") == "true" {
			t.Fatal("pgtest: CI bắt buộc AG_TEST_PG_ADMIN_URL")
		}
		t.Skip("pgtest: chưa đặt AG_TEST_PG_ADMIN_URL — bỏ qua kiểm thử tích hợp")
	}
	return current
}

// Pool trả pool của một vai trên một cụm. Vai ứng dụng đi qua PgBouncer nếu có;
// ag_migrator (owner) luôn nối thẳng.
func (e *Env) Pool(t testing.TB, c migrations.Cluster, role string) *pgxpool.Pool {
	t.Helper()
	key := string(c) + "/" + role
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.pools[key]; ok {
		return p
	}
	// Vai ứng dụng luôn dùng chế độ thực thi của PgBouncer (QueryExecModeExec), kể cả khi nối thẳng,
	// để lỗi mã hóa tham số chỉ lộ ra qua PgBouncer cũng bị bắt trên máy dev.
	base, bouncer := e.PoolURL, true
	if role == dbadmin.Migrator {
		base, bouncer = e.DirectURL, false
	}
	dsn, err := dbadmin.RoleURL(base, c, e.Suffix, role)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: bouncer, MaxConns: 16, AppName: "pgtest"})
	if err != nil {
		t.Fatalf("pgtest: mở pool %s: %v", key, err)
	}
	e.pools[key] = p
	return p
}

// DirectPool mở pool của một vai nối THẲNG PostgreSQL, không qua PgBouncer —
// cho mã cần tính năng mức phiên (khóa tư vấn của leader, LISTEN), vốn không
// dùng được qua PgBouncer transaction mode.
func (e *Env) DirectPool(t testing.TB, c migrations.Cluster, role string) *pgxpool.Pool {
	t.Helper()
	key := string(c) + "/" + role + "/@direct"
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.pools[key]; ok {
		return p
	}
	dsn, err := dbadmin.RoleURL(e.DirectURL, c, e.Suffix, role)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.Open(context.Background(), dsn, db.Options{MaxConns: 8, AppName: "pgtest-direct"})
	if err != nil {
		t.Fatalf("pgtest: mở pool %s: %v", key, err)
	}
	e.pools[key] = p
	return p
}

// AdminPool mở pool bằng tài khoản quản trị cụm tới database kiểm thử — chỉ để
// giả lập kẻ tấn công có quyền cao (vd sửa dữ liệu vượt trigger) trong kiểm thử.
func (e *Env) AdminPool(t testing.TB, c migrations.Cluster) *pgxpool.Pool {
	t.Helper()
	key := string(c) + "/@admin"
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.pools[key]; ok {
		return p
	}
	u, err := url.Parse(e.AdminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + dbadmin.DatabaseName(c, e.Suffix)
	p, err := db.Open(context.Background(), u.String(), db.Options{MaxConns: 4, AppName: "pgtest-admin"})
	if err != nil {
		t.Fatal(err)
	}
	e.pools[key] = p
	return p
}

// ExpireLedgerIdem đặt hạn các dòng đăng ký shared.ledger_idem có idem_key cho trước
// về quá khứ — giả lập 30 ngày đã trôi qua (ADR-0012, W2.1). Lần ghi sổ sau đó với
// cùng idem_key được shared.fn_chain nhận như bản ghi MỚI, nên sổ có hai dòng cùng
// khóa; dùng để kiểm truy vấn tra cứu theo idem_key chọn dòng mới nhất. Chạy bằng
// tài khoản quản trị (vai ứng dụng không có quyền trên ledger_idem); chỉ dùng trong kiểm thử.
func (e *Env) ExpireLedgerIdem(t testing.TB, c migrations.Cluster, idemKeys ...string) {
	t.Helper()
	tag, err := e.AdminPool(t, c).Exec(context.Background(),
		"UPDATE shared.ledger_idem SET expires_at = now() - interval '1 second' WHERE idem_key = ANY($1::text[])", idemKeys)
	if err != nil {
		t.Fatalf("pgtest: đặt hạn ledger_idem: %v", err)
	}
	if tag.RowsAffected() < int64(len(idemKeys)) {
		t.Fatalf("pgtest: chỉ %d/%d dòng ledger_idem khớp %v", tag.RowsAffected(), len(idemKeys), idemKeys)
	}
}

func (e *Env) closePools() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.pools {
		p.Close()
	}
}
