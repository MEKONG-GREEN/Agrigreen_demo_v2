package handler

// P0-POOL-KN-2 (#139): hồ sơ nhân sự (W11.22) trên CHUNG một pool vai đăng nhập app_platform nhỏ như gốc lắp ráp thật.
// Danh mục đơn vị nội bộ (pf/accounts qua gốc lắp ráp — KNHRMUnits) là bản giả MỞ GIAO DỊCH NGẮN trên cùng pool như cài đặt
// thật. Trước sửa: tạo hồ sơ / thêm quá trình kiểm "đơn vị có trong danh mục" TRONG giao dịch của dom/hrm → song song nhiều
// hơn cỡ pool làm cạn pool.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// touchUnits: danh mục đơn vị nội bộ mở giao dịch riêng trên pool chung (như pf/accounts thật).
type touchUnits struct {
	pool *pgxpool.Pool
	u    unitsStub
}

func (t touchUnits) InternalUnits(ctx context.Context, ref uuid.UUID) ([]port.Unit, error) {
	tx, err := t.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SELECT 1"); err != nil {
		return nil, err
	}
	return t.u.InternalUnits(ctx, ref)
}

func TestP0PoolKN2_HRMRecordsNoNestedTx(t *testing.T) {
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
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	svc, err := app.NewService(db.ForModule(shared, app.DBModule), clock.NewFixed(now),
		port.Access{Session: testOwnerSession, Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}})
	if err != nil {
		t.Fatal(err)
	}
	org, tid := ids.New(), ids.New()
	svc.UseRecords(clienttest.New(), touchUnits{pool: shared, u: unitsStub{org: {{Code: "trong_trot", Active: true}, {Code: "hcth", Active: true}}}}, true)
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"hcth": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "hanh_chinh_tong_hop", InternalUnit: "hcth",
			UnitRef: org}},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(ctx context.Context, as, method, path string, body any) (int, string) {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
		req.Header.Set("X-Test-As", as)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	if c, b := call(context.Background(), "admin", http.MethodPost, "/v1/hrm/defaults", nil); c != http.StatusCreated {
		t.Fatalf("danh mục mặc định: %d %s", c, b)
	}
	unitTT := ev("unit", "2020-01-01", map[string]any{"unit": "trong_trot"})
	vc := ev("position", "2020-01-01", map[string]any{"position": "vien_chuc"})

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
			c, b := call(ctx, "hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", fmt.Sprintf("Cán bộ Song Song %c", 'A'+i),
				fmt.Sprintf("09120009%02d", i), unitTT, vc))
			if c != http.StatusCreated || ctx.Err() != nil {
				mu.Lock()
				fails = append(fails, fmt.Sprintf("CreateStaff#%d: %d ctx=%v %.200s", i, c, ctx.Err(), b))
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
		t.Fatalf("P0-POOL-KN-2: %d/%d lời gọi tạo hồ sơ song song hỏng trên pool %d kết nối sau %s", len(fails), per, poolSize,
			time.Since(began).Round(time.Millisecond))
	}
	t.Logf("P0-POOL-KN-2: %d lời gọi tạo hồ sơ song song trên pool %d kết nối xong sau %s", per, poolSize, time.Since(began).Round(time.Millisecond))
	// Kiểm đơn vị vẫn chạy: đơn vị không có trong danh mục → 422 như cũ.
	if c, b := call(context.Background(), "hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Cán bộ Sai Đơn Vị", "0912000999",
		ev("unit", "2020-01-01", map[string]any{"unit": "khong_co"}), vc)); c != http.StatusUnprocessableEntity {
		t.Fatalf("đơn vị không có trong danh mục: %d %s", c, b)
	}
}
