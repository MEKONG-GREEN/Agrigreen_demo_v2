// Package assembly là gốc lắp ráp của api-platform, tách khỏi package main để cmd/api-platform,
// kiểm thử đầu–cuối (tests/e2e) và cmd/seed-synthetic dùng CHUNG một cách nối module —
// không còn bản sao có thể lệch nhau.
package assembly

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// Deps là phụ thuộc của api-platform. Mỗi module có pool theo vai DB riêng (R-05)
// và được dựng bằng Build (modules.go).
type Deps struct {
	Log      *slog.Logger
	Verifier *authz.Verifier
	// Ops là pool của vai platform_api: chỉ bảng vận hành trong schema shared.
	Ops     *pgxpool.Pool
	Modules []httpx.Module
	// Pools liệt kê mọi pool để /readyz kiểm.
	Pools []*pgxpool.Pool
	// CORSOrigins là origin trình duyệt được gọi chéo (AG_CORS_ORIGINS, khớp chính xác).
	// Rỗng → không origin nào; app di động và server-to-server không bị ảnh hưởng.
	CORSOrigins []string
}

// maxBody giới hạn thân request (tải tệp đi qua đường riêng có giới hạn riêng).
const maxBody = 1 << 20

func health(d Deps) func(r *gin.Engine) {
	return func(r *gin.Engine) {
		r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
		r.GET("/readyz", func(c *gin.Context) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			for _, p := range append([]*pgxpool.Pool{d.Ops}, d.Pools...) {
				if err := p.Ping(ctx); err != nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
					return
				}
			}
			c.JSON(http.StatusOK, gin.H{"status": "ready"})
		})
	}
}

// NewRouter dựng Application API (api.<domain>). Public API tắt ở ĐTK 1 (mục 11).
func NewRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	r.Use(httpx.CORS(d.CORSOrigins)) // trước mọi route: preflight đi qua cả nhánh 404/405
	health(d)(r)
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), idempotency.Middleware(d.Ops))
	for _, m := range d.Modules {
		m.RegisterApp(v1)
	}
	return r
}

// NewAdminRouter dựng Admin API (admin.<domain>): MFA bắt buộc cho mọi route (mục 11).
func NewAdminRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	r.Use(httpx.CORS(d.CORSOrigins)) // trước mọi route: preflight đi qua cả nhánh 404/405
	health(d)(r)
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), httpx.RequireMFA(), idempotency.Middleware(d.Ops))
	for _, m := range d.Modules {
		m.RegisterAdmin(v1)
	}
	return r
}
