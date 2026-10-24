package handler_test

// P0-POOL-KN-2 (#139): đường người dùng của extension/app (chỉ đạo, nhiệm vụ, lịch công tác, danh bạ trực) và job leo thang
// SLA trên CHUNG một pool vai đăng nhập app_platform nhỏ như gốc lắp ráp thật (ADR-0028). Port giả của module khác
// (pf/party phạm vi + tổ chức, đơn vị hành chính, pf/masterdata tham số + danh mục) MỞ GIAO DỊCH NGẮN trên cùng pool như
// cài đặt thật. Rà P0-POOL-KN liệt kê directives/escalation/roster/schedule/tasks theo TÊN; rà theo luồng gọi cho thấy mọi
// lời gọi port ở các đường này nằm NGOÀI giao dịch — kiểm thử này giữ điều đó (song song > cỡ pool, hạn 10 s mỗi lời gọi).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// poolToucher: "giao dịch riêng của port module khác" trên pool chung.
type poolToucher struct{ pool *pgxpool.Pool }

func (p poolToucher) touch(ctx context.Context) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = tx.Exec(ctx, "SELECT 1")
	return err
}

type touchOrgs struct {
	*orgTree
	poolToucher
}

func (o touchOrgs) Get(ctx context.Context, ref uuid.UUID) (partyport.Org, error) {
	if err := o.touch(ctx); err != nil {
		return partyport.Org{}, err
	}
	return o.orgTree.Get(ctx, ref)
}

func (o touchOrgs) Scope(ctx context.Context) (authz.Scope, error) {
	if err := o.touch(ctx); err != nil {
		return authz.Scope{}, err
	}
	return o.orgTree.Scope(ctx)
}

type touchUnits struct {
	*fakeUnits
	poolToucher
}

func (u touchUnits) Get(ctx context.Context, code string, at time.Time) (partyport.AdminUnit, error) {
	if err := u.touch(ctx); err != nil {
		return partyport.AdminUnit{}, err
	}
	return u.fakeUnits.Get(ctx, code, at)
}

func (u touchUnits) Descendants(ctx context.Context, code string, at time.Time) ([]string, error) {
	if err := u.touch(ctx); err != nil {
		return nil, err
	}
	return u.fakeUnits.Descendants(ctx, code, at)
}

type touchParams struct {
	*fakeParams
	poolToucher
}

func (p touchParams) At(ctx context.Context, key string, at time.Time) (params.ParamValue, error) {
	if err := p.touch(ctx); err != nil {
		return params.ParamValue{}, err
	}
	return p.fakeParams.At(ctx, key, at)
}

type touchCatalogs struct{ poolToucher }

func (c touchCatalogs) Get(ctx context.Context, catalog, code string) (mdport.CatalogItem, error) {
	if err := c.touch(ctx); err != nil {
		return mdport.CatalogItem{}, err
	}
	return fakeCatalogs{}.Get(ctx, catalog, code)
}

func (c touchCatalogs) List(ctx context.Context, catalog string) ([]mdport.CatalogItem, error) {
	return nil, c.touch(ctx)
}

// newSharedFixture như newFixture nhưng module chạy trên pool app_platform MaxConns = n và port giả chạm pool đó.
func newSharedFixture(t *testing.T, n int32) *fixture {
	env := pgtest.Get(t)
	dsn, err := dbadmin.RoleURL(env.PoolURL, migrations.PF, env.Suffix, dbadmin.AppPlatform)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: true, MaxConns: n, AppName: "pgtest-p0poolkn2"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	tp := poolToucher{pool: shared}
	iss := authztest.New(t)
	ops := env.Pool(t, migrations.PF, "platform_api")
	clk := clock.NewFixed(t0)
	prm := &fakeParams{v: map[string]decimal.Decimal{}}
	org := newOrgTree()
	to := touchOrgs{orgTree: org, poolToucher: tp}
	mod := extension.New(shared, clk, extension.Deps{Scope: to, Orgs: to, Units: touchUnits{fakeUnits: newFakeUnits(), poolToucher: tp},
		Params: touchParams{fakeParams: prm, poolToucher: tp}, Catalogs: touchCatalogs{tp}})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return &fixture{t: t, env: env, iss: iss, app: srv, mod: mod, pool: env.Pool(t, migrations.PF, extension.DBRole), clk: clk, params: prm,
		org: org, tenant: ids.New()}
}

// callCtx như call nhưng có hạn (ctx) và trả lỗi thay vì Fatal (gọi được từ goroutine).
func (c caller) callCtx(ctx context.Context, method, path string, body any) (int, string, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.f.app.URL+path, rd)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", key())
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(raw), nil
}

type stormCall struct {
	name string
	want []int
	do   func(ctx context.Context) (int, string, error)
}

func storm(t *testing.T, poolSize int, calls []stormCall) {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var fails []string
	start := make(chan struct{})
	for _, c := range calls {
		wg.Add(1)
		go func(c stormCall) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			st, body, err := c.do(ctx)
			ok := err == nil
			if ok {
				ok = false
				for _, w := range c.want {
					ok = ok || st == w
				}
			}
			if !ok {
				mu.Lock()
				fails = append(fails, fmt.Sprintf("%s: status=%d err=%v body=%.200s", c.name, st, err, body))
				mu.Unlock()
			}
		}(c)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	for _, f := range fails {
		t.Error(f)
	}
	if len(fails) > 0 {
		t.Fatalf("P0-POOL-KN-2: %d/%d lời gọi song song hỏng trên pool %d kết nối sau %s", len(fails), len(calls), poolSize,
			time.Since(began).Round(time.Millisecond))
	}
	t.Logf("P0-POOL-KN-2: %d lời gọi song song trên pool %d kết nối xong sau %s", len(calls), poolSize, time.Since(began).Round(time.Millisecond))
}

// TestP0PoolKN2_ExtensionPathsNoNestedTx: ban hành/xác nhận/xem chỉ đạo, tạo/xem nhiệm vụ, lưu nháp lịch công tác, danh bạ
// trực và job leo thang SLA — song song trên pool 2 kết nối.
func TestP0PoolKN2_ExtensionPathsNoNestedTx(t *testing.T) {
	const poolSize = 2
	const per = 4
	f := newSharedFixture(t, poolSize)
	p := f.seedPeople()
	d := p.admin89.must(p.admin89.post("/v1/extension/directives", map[string]any{"title": "Phòng trừ rầy nâu", "body": "Tổ chức thăm đồng",
		"audience_units": []string{"30001"}}), http.StatusCreated, "chỉ đạo").body
	dirID := str(d, "directive_id")
	overdue := p.off30001a.createTask("30001", nil)
	f.clk.Advance(25 * time.Hour) // nhiệm vụ quá SLA 24 giờ → job leo thang có việc
	jobs := f.mod.Jobs(func(context.Context, time.Time) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)

	var calls []stormCall
	for i := 0; i < per; i++ {
		calls = append(calls,
			stormCall{name: fmt.Sprintf("IssueDirective#%d", i), want: []int{201}, do: func(ctx context.Context) (int, string, error) {
				return p.admin89.callCtx(ctx, http.MethodPost, "/v1/extension/directives", map[string]any{"title": "Cảnh báo rầy nâu",
					"body": "Thăm đồng hằng ngày", "audience_units": []string{"30001", "30002"}})
			}},
			stormCall{name: fmt.Sprintf("AckDirective#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return p.off30001a.callCtx(ctx, http.MethodPost, "/v1/extension/directives/"+dirID+"/ack", nil)
			}},
			stormCall{name: fmt.Sprintf("ListDirectives#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return p.off30001b.callCtx(ctx, http.MethodGet, "/v1/extension/directives", nil)
			}},
			stormCall{name: fmt.Sprintf("CreateTask#%d", i), want: []int{201}, do: func(ctx context.Context) (int, string, error) {
				return p.off30001b.callCtx(ctx, http.MethodPost, "/v1/extension/tasks", taskBody("30001"))
			}},
			stormCall{name: fmt.Sprintf("ListTasks#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return p.admin89.callCtx(ctx, http.MethodGet, "/v1/extension/tasks", nil)
			}},
			stormCall{name: fmt.Sprintf("Roster#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return p.admin89.callCtx(ctx, http.MethodGet, "/v1/extension/roster", nil)
			}},
			stormCall{name: fmt.Sprintf("UpsertStaff#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return p.admin89.callCtx(ctx, http.MethodPost, "/v1/extension/staff", map[string]any{"actor_ref": p.off30002.actor.String(),
					"unit_code": "30002", "role_title": "Cán bộ khuyến nông", "display_name": "Cán bộ 30002"})
			}},
		)
	}
	for i, off := range []caller{p.off30001a, p.off30001b, p.off30002} {
		off := off
		calls = append(calls, stormCall{name: fmt.Sprintf("SaveSchedule#%d", i), want: []int{200, 201}, do: func(ctx context.Context) (int, string, error) {
			return off.callCtx(ctx, http.MethodPost, "/v1/extension/work-schedules", map[string]any{"week_start": nextWeek, "action": "draft",
				"items": off.fill(sItem("2026-09-21", "morning", "08:00", "10:00", "Kiểm tra đồng ruộng ấp 1"))})
		}})
	}
	for i := 0; i < 2; i++ {
		calls = append(calls, stormCall{name: fmt.Sprintf("EscalationJob#%d", i), want: []int{0}, do: func(ctx context.Context) (int, string, error) {
			return 0, "", jobs[0].Run(ctx)
		}})
	}
	storm(t, poolSize, calls)
	got := p.admin89.must(p.admin89.get("/v1/extension/tasks/"+str(overdue, "task_id")), http.StatusOK, "nhiệm vụ quá hạn").body
	if str(got, "task", "escalated_at") == "" {
		t.Fatalf("job leo thang chưa leo thang nhiệm vụ quá SLA: %v", got["task"])
	}
}
