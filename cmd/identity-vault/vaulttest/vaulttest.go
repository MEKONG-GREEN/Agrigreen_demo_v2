// Package vaulttest dựng kho định danh THẬT (internal/vault/identity + database
// vault của pgtest) chạy trong tiến trình, sau httptest.Server, cho kiểm thử
// tích hợp của gốc lắp ráp khác (tests/e2e, cmd/seed-synthetic).
//
// Gói nằm dưới cmd/identity-vault vì đây là gốc lắp ráp DUY NHẤT được import
// module vault (R-04, tools/archlint). Module ngoài vault vẫn chỉ gọi kho qua
// internal/vault/identity/client (client.NewHTTP tới URL của server này).
//
// Router lặp lại cmd/identity-vault/router.go (NewRouter nằm trong package main
// nên không import được): /healthz, /readyz, nhóm /v1 bắt buộc Bearer token của
// người dùng cuối. Đổi router thật thì đổi cả ở đây.
package vaulttest

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// maxBody khớp cmd/identity-vault.
const maxBody = 64 << 10

// NewServer dựng kho định danh trên database vault của env, xác minh token bằng
// v (issuer giả của kiểm thử). KEK và khóa chỉ mục mù là hằng số kiểm thử.
func NewServer(t testing.TB, env *pgtest.Env, v *authz.Verifier, log *slog.Logger) *httptest.Server {
	t.Helper()
	pool := env.Pool(t, migrations.Vault, dbadmin.AppVault) // W2.6: vai đăng nhập đơn vị, module chạy r_identity
	keys, err := identity.NewLocalKMS("e2e-1", bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := identity.New(identity.Config{DB: pool, Clock: clock.System{}, KMS: keys, BlindKey: bytes.Repeat([]byte{3}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	r := httpx.NewEngine(log, maxBody)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "vault"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if pool.Ping(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	mod.RegisterApp(r.Group("/v1", httpx.Authenticate(v)))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}
