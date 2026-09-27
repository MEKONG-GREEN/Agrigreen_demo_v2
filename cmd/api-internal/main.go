// Lệnh api-internal: API nội bộ MKG (vùng mkg). Các module mkg/* bắt đầu ở G2;
// G0 chỉ có health để kiểm chứng đơn vị triển khai và đường mạng (mục 5, 17).
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
	log := telemetry.NewLogger("api-internal", os.Stdout)
	r := httpx.NewEngine(log, 1<<20)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "mkg"}) })
	addr := os.Getenv("AG_HTTP_ADDR")
	if addr == "" {
		addr = ":8081"
	}
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	log.Info("lắng nghe", "event", "startup", "addr", addr, "outcome", "ok")
	if err := srv.ListenAndServe(); err != nil {
		log.Error("dừng", "event", "shutdown", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}
