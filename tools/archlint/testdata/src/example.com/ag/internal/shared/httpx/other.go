package httpx

import (
	"context"

	"example.com/ag/internal/shared/tenant"
)

// Danh sách duyệt theo tệp: tệp khác cùng gói không được.
func Other(ctx context.Context, tid [16]byte) context.Context {
	return tenant.WithID(ctx, tid) // want "H4: tenant.WithID"
}
