package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// Deps là phụ thuộc của api-platform. Mỗi module một pool theo vai DB riêng (R-05).
type Deps struct {
	Log      *slog.Logger
	Verifier *authz.Verifier
	Clock    clock.Clock
	// Ops là pool của vai platform_api: chỉ bảng vận hành trong schema shared.
	Ops   *pgxpool.Pool
	Party *pgxpool.Pool
}

// maxBody giới hạn thân request của Application API.
const maxBody = 1 << 20

// NewRouter dựng Application API (api.<domain>). Admin API và Public API là
// engine riêng trên hostname riêng — Public API tắt ở ĐTK 1 (mục 11).
func NewRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		for _, p := range []*pgxpool.Pool{d.Ops, d.Party} {
			if err := p.Ping(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), idempotency.Middleware(d.Ops))
	party.New(d.Party, d.Clock).Register(v1)
	return r
}
