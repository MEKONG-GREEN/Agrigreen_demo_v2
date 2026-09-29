// Package assembly là gốc lắp ráp của api-internal — API nội bộ MKG (vùng mkg,
// mục 5). Chỉ nối module internal/mkg/*, mỗi module một vai DB ở cụm mkg (R-05).
//
// Ranh giới (R-03): package này KHÔNG import internal/platform/*; ngược lại
// cmd/api-platform không import internal/mkg/*. tools/archlint cưỡng chế cả hai
// chiều ở gốc lắp ráp. Dữ liệu sang nền tảng chỉ đi qua van công bố (outbox của
// cụm mkg → outbox-publisher → RabbitMQ).
package assembly

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// PoolOpener mở pool cho một vai DB của cụm mkg.
type PoolOpener func(ctx context.Context, role string) (*pgxpool.Pool, error)

// Modules là các module mkg đã nối.
type Modules struct {
	Hub *hub.Module
}

// List trả module theo thứ tự gắn route.
func (m Modules) List() []httpx.Module { return []httpx.Module{m.Hub} }

// Build mở pool theo vai của từng module và nối port giữa các module mkg.
// Thêm mkg/fieldops, measurement, logistics, orders, costing, acctbridge ở đây.
//
// ops là pool dùng cho bảng vận hành shared.idempotency_key của cụm mkg. G2 chưa
// có vai vận hành riêng cho api-internal (tương đương platform_api ở cụm pf) nên
// dùng pool của hub_app (thành viên ag_shared_user) — câu hỏi mở Q-MKG-07.
func Build(ctx context.Context, open PoolOpener, clk clock.Clock) (mods Modules, ops *pgxpool.Pool, pools []*pgxpool.Pool, err error) {
	hubPool, err := open(ctx, hub.DBRole)
	if err != nil {
		return mods, nil, pools, err
	}
	pools = append(pools, hubPool)
	mods.Hub = hub.New(hubPool, clk)
	return mods, hubPool, pools, nil
}

// Deps là phụ thuộc của router.
type Deps struct {
	Log      *slog.Logger
	Verifier *authz.Verifier
	Ops      *pgxpool.Pool
	Modules  []httpx.Module
	Pools    []*pgxpool.Pool
}

const maxBody = 1 << 20

func health(d Deps) func(r *gin.Engine) {
	return func(r *gin.Engine) {
		r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "mkg"}) })
		r.GET("/readyz", func(c *gin.Context) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			for _, p := range d.Pools {
				if err := p.Ping(ctx); err != nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
					return
				}
			}
			c.JSON(http.StatusOK, gin.H{"status": "ready", "zone": "mkg"})
		})
	}
}

// NewRouter dựng bề mặt ứng dụng của API nội bộ MKG.
func NewRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	health(d)(r)
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), idempotency.Middleware(d.Ops))
	for _, m := range d.Modules {
		m.RegisterApp(v1)
	}
	return r
}

// NewAdminRouter dựng bề mặt quản trị của API nội bộ MKG: MFA bắt buộc.
func NewAdminRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	health(d)(r)
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), httpx.RequireMFA(), idempotency.Middleware(d.Ops))
	for _, m := range d.Modules {
		m.RegisterAdmin(v1)
	}
	return r
}
