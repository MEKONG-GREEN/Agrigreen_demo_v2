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
// cmd/api-platform là package main nên không import được: Build lặp lại
// buildModules. TestAssemblyMatchesAPIPlatform (tests/e2e) đọc modules.go và
// báo lệch khi gốc lắp ráp thật đổi Sources/Sinks hay danh sách module.
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/identity-vault/vaulttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// maxBody khớp cmd/api-platform/router.go.
const maxBody = 1 << 20

// CursorKey là khóa HMAC con trỏ đồng bộ cố định của kiểm thử (≥ 32 byte).
var CursorKey = bytes.Repeat([]byte{0x5a}, 32)

// Modules là các module đã nối — kiểm thử dùng để dựng bản của worker (job leo
// thang với đồng hồ giả, handler xuất gói MRV) trên cùng port.
type Modules struct {
	MasterData    *masterdata.Module
	Party         *party.Module
	Geo           *geo.Module
	Content       *content.Module
	Extension     *extension.Module
	Mechanization *mechanization.Module
	MRV           *mrv.Module
	Sync          *syncmod.Module
}

// List trả module theo đúng thứ tự của cmd/api-platform.
func (m Modules) List() []httpx.Module {
	return []httpx.Module{m.MasterData, m.Party, m.Geo, m.Content, m.Extension, m.Mechanization, m.MRV, m.Sync}
}

// Build lặp lại buildModules của cmd/api-platform (modules.go): cùng thứ tự,
// cùng port, cùng Sources/Sinks.
func Build(open func(role string) *pgxpool.Pool, clk clock.Clock, vault client.Client, store objstore.Store) Modules {
	md := masterdata.New(open(masterdata.DBRole), clk)
	pty := party.New(open(party.DBRole), clk, party.Deps{AdminUnits: md.AdminUnits(), Vault: vault})
	g := geo.New(open(geo.DBRole), clk, geo.Deps{Scope: pty.ScopeResolver(), Params: md.Params(), AdminUnits: md.AdminUnits(), Orgs: pty.Orgs()})
	cnt := content.New(open(content.DBRole), clk, content.Deps{Scope: pty.ScopeResolver(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Store: store})
	kn := extension.New(open(extension.DBRole), clk, extension.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs()})
	mech := mechanization.New(open(mechanization.DBRole), clk, mechanization.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Params: md.Params(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Vault: vault,
	})
	mrvMod := mrv.New(open(mrv.DBRole), clk, mrv.Deps{Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Store: store})
	snc := syncmod.New(open(syncmod.DBRole), clk, syncmod.Deps{
		Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Params: md.Params(), Vault: vault,
		Store: store, CursorKey: CursorKey,
		Sources: []syncport.Source{g.SyncSource(), cnt.SyncSource(), kn.SyncSource()},
		Sinks:   []syncport.Sink{g.SyncSink(), kn.SyncSink(), mrvMod.SyncSink()},
	})
	return Modules{MasterData: md, Party: pty, Geo: g, Content: cnt, Extension: kn, Mechanization: mech, MRV: mrvMod, Sync: snc}
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
}

// New dựng Stack trên database của pgtest (bỏ qua kiểm thử khi không có PostgreSQL).
func New(t testing.TB) *Stack {
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
	mods := Build(func(role string) *pgxpool.Pool { return env.Pool(t, migrations.PF, role) }, clock.System{}, vc, store)
	ops := env.Pool(t, migrations.PF, "platform_api")
	verifier := iss.Verifier()
	app := httptest.NewServer(newRouter(log, verifier, ops, mods.List(), false))
	t.Cleanup(app.Close)
	admin := httptest.NewServer(newRouter(log, verifier, ops, mods.List(), true))
	t.Cleanup(admin.Close)
	return &Stack{T: t, Env: env, Issuer: iss, App: app, Admin: admin, Vault: vsrv, Mods: mods, VaultClient: vc, Store: store, StoreDir: dir, Logs: logs}
}

// newRouter lặp lại NewRouter/NewAdminRouter của cmd/api-platform/router.go.
func newRouter(log *slog.Logger, v *authz.Verifier, ops *pgxpool.Pool, mods []httpx.Module, admin bool) *gin.Engine {
	r := httpx.NewEngine(log, maxBody)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	mw := []gin.HandlerFunc{httpx.Authenticate(v)}
	if admin {
		mw = append(mw, httpx.RequireMFA())
	}
	mw = append(mw, idempotency.Middleware(ops))
	v1 := r.Group("/v1", mw...)
	for _, m := range mods {
		if admin {
			m.RegisterAdmin(v1)
		} else {
			m.RegisterApp(v1)
		}
	}
	return r
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
