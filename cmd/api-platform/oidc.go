package main

import (
	"context"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// verifierFromEnv dựng bộ xác minh token.
//
// Mặc định dùng discovery của AG_OIDC_ISSUER. Khi đặt AG_OIDC_JWKS_URL, khóa
// ký lấy trực tiếp từ URL đó còn issuer vẫn so đúng AG_OIDC_ISSUER — dùng khi
// issuer công khai (vd http://localhost:8180/realms/agrigreen mà trình duyệt
// thấy) khác địa chỉ nội bộ container gọi được (http://keycloak:8080/...).
// Khóa tải lười khi có token đầu tiên, nên Keycloak chưa sẵn sàng không làm hỏng
// khởi động.
//
// Chính sách yếu tố thứ hai đọc từ AG_MFA_* (authz.MFAPolicyFromEnv); AG_MFA_DISABLED=1 chỉ dành cho demo.
func verifierFromEnv(ctx context.Context, issuer, audience string) (*authz.Verifier, error) {
	policy, err := authz.MFAPolicyFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	if jwks := os.Getenv("AG_OIDC_JWKS_URL"); jwks != "" {
		return authz.NewVerifierWithKeys(issuer, audience, oidc.NewRemoteKeySet(context.WithoutCancel(ctx), jwks)).WithMFAPolicy(policy), nil
	}
	v, err := authz.NewVerifier(ctx, issuer, audience)
	if err != nil {
		return nil, err
	}
	return v.WithMFAPolicy(policy), nil
}
