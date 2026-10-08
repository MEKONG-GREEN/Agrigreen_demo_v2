// Lệnh api-public: BÍ DANH MỘT KỲ của cmd/integration-gateway (ADR-0033 §7.5, W5.2). Cùng thân tiến trình
// (gói gateway), cùng vai đăng nhập app_public, cùng biến môi trường; chỉ khác tên đơn vị (log, telemetry,
// application_name) để chart, DNS partner.<domain> và tài liệu đối tác chuyển dần.
//
// NGỪNG DÙNG: gỡ thư mục này, image agrigreen-api-public và Deployment api-public ở bản phát hành kế tiếp sau
// W5.2 (docs/runbooks/integration-gateway.md mục 5). Mã mới không thêm gì ở đây.
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
	log := telemetry.NewLogger(gateway.AliasUnit, os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, os.Getenv); err != nil {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

// run chạy cổng dưới tên bí danh, kèm cảnh báo ngừng dùng.
func run(ctx context.Context, log *slog.Logger, getenv func(string) string) error {
	if gateway.Enabled(getenv) {
		log.Warn("cmd/api-public là bí danh một kỳ của cmd/integration-gateway — chuyển image/Deployment sang integration-gateway",
			"event", "deprecated_alias", "replacement", gateway.Unit)
	}
	return gateway.Run(ctx, log, getenv, gateway.AliasUnit)
}
