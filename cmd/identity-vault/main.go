// Lệnh identity-vault: kho định danh hộ (mục 10), cụm vault riêng, chỉ mTLS
// nội bộ (mục 11). API /v1/households… làm ở G1 theo ADR-0004; G0 chỉ có health.
package main

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func main() {
	log := telemetry.NewLogger("identity-vault", os.Stdout)
	r := httpx.NewEngine(log, 64<<10)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "vault"}) })
	addr := os.Getenv("AG_HTTP_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	log.Info("lắng nghe", "event", "startup", "addr", addr, "outcome", "ok")
	if err := srv.ListenAndServe(); err != nil {
		log.Error("dừng", "event", "shutdown", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}
