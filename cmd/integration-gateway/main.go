// Lệnh integration-gateway: cổng tích hợp cho mọi đối tác, gồm MKG (ADR-0033, W5.2) — mở rộng từ Public API
// cho bên thứ ba (ADR-0022). Thân tiến trình ở gói gateway (dùng chung với bí danh một kỳ cmd/api-public);
// gốc lắp ráp ở gói assembly. Cấu hình: xem tài liệu gói gateway.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func main() {
	log := telemetry.NewLogger(gateway.Unit, os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, os.Getenv); err != nil {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, getenv func(string) string) error {
	return gateway.Run(ctx, log, getenv, gateway.Unit)
}
