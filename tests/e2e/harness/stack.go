// Package harness dựng toàn bộ nền tảng AgriGreen G1 trong tiến trình cho kiểm
// thử chấp nhận đầu–cuối (tests/e2e) và kiểm thử bộ sinh dữ liệu tổng hợp
// (cmd/seed-synthetic):
//
//   - Application API và Admin API dựng như cmd/api-platform (buildModules +
//     NewRouter/NewAdminRouter), mỗi module một pool theo vai DB (R-05);
//   - kho định danh THẬT (identity + database vault riêng) sau httptest.Server,
//     module gọi qua client.NewHTTP chuyển tiếp token người dùng (R-04, R-54);
//   - issuer OIDC giả (authztest), kho đối tượng thư mục cục bộ;
//   - bộ gom log của cả ba server để kiểm không lộ dữ liệu cá nhân (R-14).
//
// Gốc lắp ráp và router lấy từ cmd/api-platform/assembly — cùng mã với tiến trình thật.
package harness

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/identity-vault/vaulttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// CursorKey là khóa HMAC con trỏ đồng bộ cố định của kiểm thử (≥ 32 byte).
var CursorKey = bytes.Repeat([]byte{0x5a}, 32)

// Modules là các module đã nối (assembly.Modules) — kiểm thử dùng để dựng bản
// của worker (job leo thang với đồng hồ giả, handler xuất gói MRV) trên cùng port.
type Modules = assembly.Modules

// Build nối module bằng CHÍNH gốc lắp ráp của cmd/api-platform (assembly.Build).
func Build(open func(role string) *pgxpool.Pool, clk clock.Clock, vault client.Client, store objstore.Store) Modules {
	return build(open, clk, vault, store, nil)
}

func build(open func(role string) *pgxpool.Pool, clk clock.Clock, vault client.Client, store objstore.Store, dir *accounts.MemoryDirectory) Modules {
	ext := assembly.External{Vault: vault, Store: store, CursorKey: CursorKey}
	if dir != nil {
		ext.Accounts = dir
	}
	mods, _, err := assembly.Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return open(role), nil
	}, clk, ext)
	if err != nil {
		panic(err)
	}
	return mods
}

// Stack là nền tảng đang chạy trong tiến trình.
type Stack struct {
	T      testing.TB
	Env    *pgtest.Env
	Issuer *authztest.FakeIssuer
	// App, Admin, Vault là ba bề mặt HTTP: Application API, Admin API (MFA), kho định danh.
	App, Admin, Vault *httptest.Server
	Mods              Modules
	VaultClient       client.Client
	Store             *objstore.Local
	StoreDir          string
	Logs              *LogBuffer
	// Directory là Keycloak Admin giả (bộ nhớ) của pf/accounts — tài khoản nông dân App HTX.
	Directory *accounts.MemoryDirectory
}

// New dựng Stack trên database của pgtest (bỏ qua kiểm thử khi không có PostgreSQL).
func New(t testing.TB) *Stack {
	t.Helper()
	return NewAt(t, clock.System{})
}

// NewAt như New nhưng mọi module dùng đồng hồ clk (KN v7.7 P8: đồng hồ giả cho hạn nộp, chốt tự động, hạn mở lại).
func NewAt(t testing.TB, clk clock.Clock) *Stack {
	t.Helper()
	env := pgtest.Get(t)
	iss := authztest.New(t)
	logs := &LogBuffer{}
	log := telemetry.NewLogger("e2e", logs)
	vsrv := vaulttest.NewServer(t, env, iss.Verifier(), telemetry.NewLogger("identity-vault", logs))
	vc := client.NewHTTP(vsrv.URL, &http.Client{Timeout: 30 * time.Second})
	dir := t.TempDir()
	store, err := objstore.NewLocal(dir, objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	kc := accounts.NewMemoryDirectory()
	mods := build(func(role string) *pgxpool.Pool { return env.Pool(t, migrations.PF, role) }, clk, vc, store, kc)
	ops := env.Pool(t, migrations.PF, "platform_api")
	verifier := iss.Verifier()
	app := httptest.NewServer(newRouter(log, verifier, ops, mods.List(), false))
	t.Cleanup(app.Close)
	admin := httptest.NewServer(newRouter(log, verifier, ops, mods.List(), true))
	t.Cleanup(admin.Close)
	return &Stack{T: t, Env: env, Issuer: iss, App: app, Admin: admin, Vault: vsrv, Mods: mods, VaultClient: vc, Store: store, StoreDir: dir, Logs: logs, Directory: kc}
}

// newRouter dùng router của cmd/api-platform (assembly).
func newRouter(log *slog.Logger, v *authz.Verifier, ops *pgxpool.Pool, mods []httpx.Module, admin bool) *gin.Engine {
	d := assembly.Deps{Log: log, Verifier: v, Ops: ops, Modules: mods}
	if admin {
		return assembly.NewAdminRouter(d)
	}
	return assembly.NewRouter(d)
}

// PF trả pool của một vai trên cụm pf (vd dbadmin.OutboxPublisher để đọc outbox).
func (s *Stack) PF(role string) *pgxpool.Pool { return s.Env.Pool(s.T, migrations.PF, role) }

// AdminPF trả pool quản trị cụm pf — CHỈ để kiểm tra catalog (information_schema).
func (s *Stack) AdminPF() *pgxpool.Pool { return s.Env.AdminPool(s.T, migrations.PF) }

// Ctx là context nền của kiểm thử.
func (s *Stack) Ctx() context.Context { return context.Background() }

// NewTenant sinh tenant mới (mỗi kịch bản một tenant — RLS cách ly).
func NewTenant() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// LogBuffer gom log JSON của các server (an toàn đồng thời).
type LogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

var _ io.Writer = (*LogBuffer)(nil)

func (l *LogBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// String trả toàn bộ log đã gom.
func (l *LogBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
