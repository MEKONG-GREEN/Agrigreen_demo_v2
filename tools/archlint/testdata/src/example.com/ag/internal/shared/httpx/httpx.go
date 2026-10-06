// Middleware xác thực: tệp có tên trong danh sách duyệt WithID.
package httpx

import (
	"context"

	"example.com/ag/internal/shared/tenant"
)

func Auth(ctx context.Context, tid [16]byte) context.Context { return tenant.WithID(ctx, tid) }
