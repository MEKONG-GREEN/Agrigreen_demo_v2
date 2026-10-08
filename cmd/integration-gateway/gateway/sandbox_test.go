package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// W5.8 (R-INT-08): production từ chối chế độ thử của đối tác — cổng không khởi động, trước mọi kết nối.
func TestProductionRefusesSandbox_W58(t *testing.T) {
	for _, env := range []string{"production", "prod"} {
		err := Run(context.Background(), quiet(), envOf(map[string]string{EnvEnabled: "1", "AG_ENV": env, port.EnvSandbox: "1"}), Unit)
		if !errors.Is(err, port.ErrSandboxInProduction) {
			t.Fatalf("AG_ENV=%s + %s=1 → ErrSandboxInProduction, nhận %v", env, port.EnvSandbox, err)
		}
	}
}

// Giới hạn kênh tệp đọc từ môi trường; ngoài khoảng → không khởi động.
func TestIngestFileLimitsFromEnv_W58(t *testing.T) {
	cfg, err := IngestFromEnv(envOf(map[string]string{port.EnvIngestMaxFileBytes: "2048", port.EnvIngestMaxFileRecords: "20"}), false, quiet())
	if err != nil || cfg.MaxFileBytes != 2048 || cfg.MaxFileRecords != 20 {
		t.Fatalf("giới hạn tệp: %+v %v", cfg, err)
	}
	for k, v := range map[string]string{port.EnvIngestMaxFileBytes: "10", port.EnvIngestMaxFileRecords: "0"} {
		if _, err := IngestFromEnv(envOf(map[string]string{k: v}), false, quiet()); err == nil {
			t.Fatalf("%s=%s ngoài khoảng → lỗi", k, v)
		}
	}
}
