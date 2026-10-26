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
//	AG_TEST_PG_WAIT        (tùy chọn) số giây chờ PostgreSQL sẵn sàng, mặc định 120; 0 = không chờ (infra.go)
//
// Thiếu AG_TEST_PG_ADMIN_URL: bỏ qua trên máy dev, HỎNG khi CI=true. PostgreSQL không sẵn sàng (lỗi hạ tầng): gói
// HỎNG một lần với dòng "INFRA: PostgreSQL unavailable …", không phải hàng loạt kiểm thử 0.00s (infra.go). Cụm
// PostgreSQL tạm cho máy dev: scripts/pg-ephemeral.ps1 (docs/dev/parallel-packages.md).
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

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

	healthMu   sync.Mutex
	lastHealth time.Time
	infraDown  error // khác nil: PostgreSQL sập giữa chừng và không hồi phục — kiểm thử sau bỏ qua
)

// Run dùng trong TestMain: dựng database, chạy kiểm thử, dọn dẹp.
func Run(m *testing.M) int {
	admin := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if admin != "" {
		current, initErr = setupWithWait(admin)
		if IsInfra(initErr) {
			// Một dòng rõ ràng thay cho hàng loạt HỎNG 0.00s; không chạy kiểm thử nào của gói.
			markDown(admin, true)
			stderrf("%v", initErr)
			stderrf("pgtest: không chạy kiểm thử nào của gói này (lỗi hạ tầng, không phải lỗi kiểm thử)")
			return 3
		}
		if initErr == nil {
			markDown(admin, false)
		}
	}
	code := m.Run()
	if infraDown != nil {
		markDown(admin, true)
		stderrf("%v", infraDown)
	}
	if current != nil {
		current.closePools()
		if err := dbadmin.DropDatabases(context.Background(), admin, current.Suffix); err != nil {
			fmt.Fprintln(os.Stderr, "pgtest: không xóa được database kiểm thử:", err)
		}
	}
	return code
}

// setupWithWait chờ PostgreSQL sẵn sàng rồi dựng database; sập giữa lúc dựng (lỗi hạ tầng) → dọn và dựng lại với hậu
// tố mới trong phần hạn còn lại.
func setupWithWait(admin string) (*Env, error) {
	ctx := context.Background()
	budget := waitBudget()
	if recentlyDown(admin) {
		budget = min(budget, downRetryWait)
	}
	deadline := time.Now().Add(budget)
	for attempt := 1; ; attempt++ {
		left := max(time.Until(deadline), 0)
		if err := waitReady(ctx, left, func(ctx context.Context) error { return probe(ctx, admin) }, time.Sleep, time.Now, stderrf); err != nil {
			return nil, err
		}
		e, err := setup(admin)
		if err == nil || !IsInfra(err) {
			return e, err
		}
		if attempt >= 3 || time.Now().After(deadline) {
			return nil, &InfraError{Waited: budget - max(time.Until(deadline), 0), Err: err}
		}
		stderrf("pgtest: mất PostgreSQL khi dựng database (%v) — thử lại", err)
	}
}

// setup dựng database; hỏng giữa chừng thì xóa phần đã dựng (best effort — PostgreSQL có thể đang sập).
func setup(admin string) (e *Env, err error) {
	defer func() {
		if err != nil && e != nil {
			_ = dbadmin.DropDatabases(context.Background(), admin, e.Suffix)
			e = nil
		}
	}()
	return setupOnce(admin)
}

func setupOnce(admin string) (*Env, error) {
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
		return e, err
	}
	for _, c := range migrations.Clusters {
		if err := dbadmin.Migrate(e.DirectURL, c, e.Suffix); err != nil {
			return e, err
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
	if err := checkHealth(current.AdminURL); err != nil {
		if errors.Is(err, errSkipInfra) {
			t.Skip("INFRA: PostgreSQL unavailable — bỏ qua; gói đã HỎNG ở kiểm thử gặp sự cố đầu tiên")
		}
		t.Fatalf("%v", err)
	}
	return current
}

// checkHealth thăm dò PostgreSQL (tối đa mỗi probeEvery). Sập → chờ theo AG_TEST_PG_WAIT; hồi phục → nil. Không hồi
// phục → *InfraError cho kiểm thử đầu tiên gặp; các kiểm thử sau nhận errSkipInfra và bỏ qua — gói vẫn HỎNG nhờ
// kiểm thử đầu.
func checkHealth(admin string) error {
	healthMu.Lock()
	defer healthMu.Unlock()
	if infraDown != nil {
		return errSkipInfra
	}
	if time.Since(lastHealth) < probeEvery {
		return nil
	}
	err := waitReady(context.Background(), waitBudget(), func(ctx context.Context) error { return probe(ctx, admin) }, time.Sleep, time.Now, stderrf)
	if err == nil {
		lastHealth = time.Now()
		return nil
	}
	if IsInfra(err) {
		infraDown = err
	}
	return err
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

// SizedPool mở pool RIÊNG (không dùng chung, đóng khi test xong) của một vai với cỡ maxConns — cho kiểm thử cạn
// pool (P0-POOL-SHARED: thao tác không giữ kết nối khi gọi dịch vụ ngoài). Như Pool: vai ứng dụng qua PgBouncer nếu có.
func (e *Env) SizedPool(t testing.TB, c migrations.Cluster, role string, maxConns int32) *pgxpool.Pool {
	t.Helper()
	base, bouncer := e.PoolURL, true
	if role == dbadmin.Migrator {
		base, bouncer = e.DirectURL, false
	}
	dsn, err := dbadmin.RoleURL(base, c, e.Suffix, role)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: bouncer, MaxConns: maxConns, AppName: "pgtest-sized"})
	if err != nil {
		t.Fatalf("pgtest: mở pool %s/%s cỡ %d: %v", c, role, maxConns, err)
	}
	t.Cleanup(p.Close)
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
