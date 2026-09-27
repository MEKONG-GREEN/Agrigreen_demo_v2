package main

import (
	"context"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// verifierFromEnv dựng bộ xác minh token: discovery của AG_OIDC_ISSUER, hoặc
// khóa từ AG_OIDC_JWKS_URL khi issuer công khai khác địa chỉ nội bộ (xem
// cmd/api-platform/oidc.go). Issuer và audience luôn được so (R-54).
func verifierFromEnv(ctx context.Context, issuer, audience string) (*authz.Verifier, error) {
	if jwks := os.Getenv("AG_OIDC_JWKS_URL"); jwks != "" {
		return authz.NewVerifierWithKeys(issuer, audience, oidc.NewRemoteKeySet(context.WithoutCancel(ctx), jwks)), nil
	}
	return authz.NewVerifier(ctx, issuer, audience)
}
