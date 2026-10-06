// Module thường: không WithID tenant bên cấp, không đúc bằng chứng, không dùng bản giả porttest ngoài kiểm thử.
package tenantuse

import (
	"context"

	_ "example.com/ag/internal/platform/party/port/porttest" // want "H4: internal/platform/party/port/porttest"
	"example.com/ag/internal/shared/tenant"
)

func ReadGrantor(ctx context.Context, grantor [16]byte) context.Context {
	return tenant.WithID(ctx, grantor) // want "H4: tenant.WithID"
}

func Forge(grantee, grantor [16]byte) tenant.Grant {
	return tenant.MintGrant(grantee, grantor) // want "H4: tenant.MintGrant"
}

// Tham chiếu hàm (không gọi) cũng bị bắt.
var withID = tenant.WithID // want "H4: tenant.WithID"

// Đường hợp lệ: bằng chứng nhận qua port.
func Use(ctx context.Context, g tenant.Grant) (context.Context, error) {
	return tenant.WithGrant(ctx, g)
}
