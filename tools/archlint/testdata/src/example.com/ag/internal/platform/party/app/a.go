// pf/party app/: được đúc bằng chứng sau kiểm cấp quyền; không được WithID.
package app

import (
	"context"

	"example.com/ag/internal/shared/tenant"
)

func Authorize(grantee, grantor [16]byte) tenant.Grant { return tenant.MintGrant(grantee, grantor) }

func Bad(ctx context.Context, grantor [16]byte) context.Context {
	return tenant.WithID(ctx, grantor) // want "H4: tenant.WithID"
}
