package handler

// P0-POOL-KN-2 (#139): báo cáo Biến động giá theo TUẦN (BG-FN-TW-01.02) trên CHUNG một pool vai đăng nhập app_platform nhỏ
// như gốc lắp ráp thật. Lịch kỳ hệ thống (Weeks — gốc lắp ráp nối pf/extension ReportWeekOf, đọc tham số pf/masterdata) là
// bản giả MỞ GIAO DỊCH NGẮN trên cùng pool như cài đặt thật. Trước sửa: tuần của từng kỳ xu hướng tra TRONG giao dịch đọc
// báo cáo → song song nhiều hơn cỡ pool làm cạn pool.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// touchWeeks: lịch kỳ hệ thống mở giao dịch riêng trên pool chung (như ReportWeekOf → Params.At thật).
type touchWeeks struct{ pool *pgxpool.Pool }

func (w touchWeeks) WeekOf(ctx context.Context, day time.Time) (port.Week, error) {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return port.Week{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SELECT 1"); err != nil {
		return port.Week{}, err
	}
	d := domain.DefaultWeekOf(day)
	return port.Week{Key: d.Key, Year: d.Year, From: d.From, To: d.To}, nil
}

func TestP0PoolKN2_PriceMovementWeekNoNestedTx(t *testing.T) {
	const poolSize = 2
	const per = 6
	env := pgtest.Get(t)
	dsn, err := dbadmin.RoleURL(env.PoolURL, migrations.PF, env.Suffix, dbadmin.AppPlatform)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: true, MaxConns: poolSize, AppName: "pgtest-p0poolkn2"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	clk := clock.NewFixed(time.Date(2026, 10, 8, 0, 31, 0, 0, time.UTC))
	prof := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH",
		"kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(shared, app.DBModule), clk, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(svc), app.NewTicketService(svc)
	svc.Points.Submitted = tickets.Submitted
	items.Hooks.HasPrice, items.Hooks.HasSheets = tickets.HasPrice, tickets.Submitted
	tickets.WireBoard()
	tickets.Weeks = touchWeeks{pool: shared}
	tid := ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
	})
	g := r.Group("/v1")
	Handler{Svc: svc}.RegisterApp(g)
	ItemHandler{Svc: items}.RegisterApp(g)
	ReportHandler{Svc: tickets}.RegisterApp(g)
	call := func(ctx context.Context, as, method, path string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader("")).WithContext(ctx)
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	if c, b := call(context.Background(), "admin", http.MethodPost, "/v1/price/catalog/defaults"); c != http.StatusCreated {
		t.Fatalf("danh mục mặc định: %d %s", c, b)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var fails []string
	start := make(chan struct{})
	for i := 0; i < per; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, b := call(ctx, "tinh", http.MethodGet, "/v1/price/reports/movement?period=week&day=2026-10-08")
			if c != http.StatusOK || ctx.Err() != nil {
				mu.Lock()
				fails = append(fails, fmt.Sprintf("MovementWeek#%d: %d ctx=%v %.200s", i, c, ctx.Err(), b))
				mu.Unlock()
			}
		}(i)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	for _, f := range fails {
		t.Error(f)
	}
	if len(fails) > 0 {
		t.Fatalf("P0-POOL-KN-2: %d/%d lời gọi Biến động giá theo tuần song song hỏng trên pool %d kết nối sau %s", len(fails), per, poolSize,
			time.Since(began).Round(time.Millisecond))
	}
	t.Logf("P0-POOL-KN-2: %d lời gọi Biến động giá theo tuần song song trên pool %d kết nối xong sau %s", per, poolSize,
		time.Since(began).Round(time.Millisecond))
	// Kỳ xu hướng theo tuần của lịch kỳ: kỳ hiện tại là tuần chứa 08/10/2026.
	c, b := call(context.Background(), "tinh", http.MethodGet, "/v1/price/reports/movement?period=week&day=2026-10-08")
	if w := domain.DefaultWeekOf(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)); c != http.StatusOK || !strings.Contains(b, w.Key) {
		t.Fatalf("Biến động theo tuần: %d %s (muốn kỳ %s)", c, b, w.Key)
	}
}
