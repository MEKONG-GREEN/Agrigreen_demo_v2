package main

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Biến cấu hình riêng của kho (xem chú thích đầu main.go).
const (
	// VaultAudienceEnv: audience RIÊNG của kho. Đặt → kho chỉ nhận token có aud này (thay
	// AG_OIDC_AUDIENCE dùng chung với các API). Muốn dùng phải cấu hình Keycloak cấp token có
	// aud của kho cho lời gọi pf → kho: Token Exchange (khuyến nghị — pf đổi token người dùng
	// lấy token aud=kho) hoặc tối thiểu audience mapper. Rỗng → giữ hành vi cũ.
	VaultAudienceEnv = "AG_VAULT_OIDC_AUDIENCE"
	// RequireMFAEnv: "1" bắt buộc MFA cho Resolve/Unwrap, "0" tắt. Rỗng → bật ở production,
	// tắt ở môi trường khác (token thử/dev chưa có amr).
	RequireMFAEnv = "AG_VAULT_REQUIRE_MFA"
	// MetricKMSSoftware: 1 khi kho chạy KEK phần mềm (KMS local), 0 khi KMS/HSM. Luật
	// AgVaultSoftwareKMS (deploy/observability/prometheus/agrigreen-vault.rules.yml).
	MetricKMSSoftware = "ag.vault.kms_software"
)

// vaultAudience chọn audience: AG_VAULT_OIDC_AUDIENCE nếu đặt, ngược lại AG_OIDC_AUDIENCE.
func vaultAudience(getenv func(string) string) string {
	if a := strings.TrimSpace(getenv(VaultAudienceEnv)); a != "" {
		return a
	}
	return getenv("AG_OIDC_AUDIENCE")
}

// requireMFAFromEnv đọc AG_VAULT_REQUIRE_MFA (mặc định theo môi trường).
func requireMFAFromEnv(getenv func(string) string) (bool, error) {
	switch v := strings.TrimSpace(getenv(RequireMFAEnv)); v {
	case "":
		return residency.Production(getenv), nil
	case "1":
		return true, nil
	case "0":
		return false, nil
	default:
		return false, fmt.Errorf("%s phải là 0 hoặc 1", RequireMFAEnv)
	}
}

// registerKMSGauge phơi trạng thái KMS phần mềm qua OTel (ag.vault.kms_software{provider}).
// No-op khi OTel tắt.
func registerKMSGauge(software bool, provider string) error {
	g, err := telemetry.Meter().Int64ObservableGauge(MetricKMSSoftware,
		metric.WithDescription("1 khi kho định danh dùng KEK phần mềm (chưa có KMS/HSM trong lãnh thổ)"))
	if err != nil {
		return err
	}
	v := int64(0)
	if software {
		v = 1
	}
	_, err = telemetry.Meter().RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(g, v, metric.WithAttributes(attribute.String("provider", provider)))
		return nil
	}, g)
	return err
}
