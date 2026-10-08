package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// EnvAMQPURL: RabbitMQ của cụm pf — hàng đợi riêng từng đối tác cho nạp theo lô (W5.4). Không đặt: dev/test dùng
// hàng đợi trong tiến trình; production tắt /v1/batches (503) vì hàng đợi trong tiến trình không bền.
const EnvAMQPURL = "AG_AMQP_URL"

// IngestConfig là cấu hình nạp theo lô đã đọc.
type IngestConfig struct {
	Queue         apiclient.IngestQueue // nil → tắt /v1/batches
	AllowProposed bool
	MaxQueued     int
	RetryAfter    time.Duration
}

// ErrProposedInProduction: cờ dev/test cho hợp đồng proposed đặt ở production — không khởi động (đóng khi lỗi).
var ErrProposedInProduction = errors.New("integration-gateway: " + port.EnvIngestAllowProposed + " chỉ cho dev/test — production từ chối hợp đồng nạp proposed/draft")

// IngestFromEnv đọc cấu hình nạp theo lô (W5.4).
func IngestFromEnv(getenv func(string) string, production bool, log *slog.Logger) (IngestConfig, error) {
	cfg := IngestConfig{AllowProposed: getenv(port.EnvIngestAllowProposed) == "1"}
	if cfg.AllowProposed && production {
		return IngestConfig{}, ErrProposedInProduction
	}
	if v := getenv(port.EnvIngestMaxQueued); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100000 {
			return IngestConfig{}, fmt.Errorf("integration-gateway: %s phải là số nguyên 1–100000", port.EnvIngestMaxQueued)
		}
		cfg.MaxQueued = n
	}
	if v := getenv(port.EnvIngestRetryAfter); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 3600 {
			return IngestConfig{}, fmt.Errorf("integration-gateway: %s phải là số giây 1–3600", port.EnvIngestRetryAfter)
		}
		cfg.RetryAfter = time.Duration(n) * time.Second
	}
	switch url := getenv(EnvAMQPURL); {
	case url != "":
		cfg.Queue = apiclient.NewIngestRabbitQueue(url, cfg.MaxQueued, log)
	case production:
		log.Warn("thiếu AG_AMQP_URL ở production — tắt nạp theo lô (/v1/batches trả 503)", "event", "startup", "outcome", "degraded")
	default:
		log.Warn("thiếu AG_AMQP_URL — nạp theo lô dùng hàng đợi trong tiến trình (chỉ dev/test, không bền)", "event", "startup", "outcome", "degraded")
		cfg.Queue = apiclient.NewIngestMemoryQueue(cfg.MaxQueued)
	}
	return cfg, nil
}
