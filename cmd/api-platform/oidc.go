package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// mfaPolicyFromEnv đọc chính sách yếu tố thứ hai (authz.MFAPolicyFromEnv). Gọi NGAY đầu run, trước khi mở DB:
// AG_MFA_DISABLED bật ngoài máy demo (AG_DEPLOY_ENV=demo và AG_ENV khác production) → lỗi, tiến trình từ chối
// khởi động. Bật hợp lệ → cảnh báo lớn có event riêng để luật log/alert bắt được.
func mfaPolicyFromEnv(getenv func(string) string, log *slog.Logger) (authz.MFAPolicy, error) {
	p, err := authz.MFAPolicyFromEnv(getenv)
	if err != nil {
		return authz.MFAPolicy{}, err
	}
	if p.Disabled {
		log.Warn("!!! DEMO: Admin API KHÔNG kiểm yếu tố thứ hai ("+authz.EnvMFADisabled+"=1, "+authz.EnvDeployEnv+"=demo). "+
			"Chỉ chấp nhận trên máy demo; đăng nhập TOTP của Keycloak (D24) vẫn áp dụng theo realm !!!",
			"event", "security.mfa_disabled", "outcome", "degraded")
	}
	return p, nil
}

// verifierFromEnv dựng bộ xác minh token.
//
// Mặc định dùng discovery của AG_OIDC_ISSUER. Khi đặt AG_OIDC_JWKS_URL, khóa
// ký lấy trực tiếp từ URL đó còn issuer vẫn so đúng AG_OIDC_ISSUER — dùng khi
// issuer công khai (vd http://localhost:8180/realms/agrigreen mà trình duyệt
// thấy) khác địa chỉ nội bộ container gọi được (http://keycloak:8080/...).
// Khóa tải lười khi có token đầu tiên, nên Keycloak chưa sẵn sàng không làm hỏng
// khởi động.
//
// Chính sách yếu tố thứ hai lấy từ mfaPolicyFromEnv (AG_MFA_*).
func verifierFromEnv(ctx context.Context, issuer, audience string, policy authz.MFAPolicy) (*authz.Verifier, error) {
	if jwks := os.Getenv("AG_OIDC_JWKS_URL"); jwks != "" {
		return authz.NewVerifierWithKeys(issuer, audience, oidc.NewRemoteKeySet(context.WithoutCancel(ctx), jwks)).WithMFAPolicy(policy), nil
	}
	v, err := authz.NewVerifier(ctx, issuer, audience)
	if err != nil {
		return nil, err
	}
	return v.WithMFAPolicy(policy), nil
}
