package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// APIClient là bên gọi Public API đã xác thực ở dạng tối thiểu module khác cần: mã khách hàng và tenant bên
// thứ ba (lấy từ bản ghi khóa, không bao giờ từ request), mã yêu cầu. pf/apiclient gắn lúc xác thực khóa
// (apiclient/port.WithCaller); module đọc qua APIClientFrom — không import pf/apiclient (R-MOD-05).
type APIClient struct {
	ClientID uuid.UUID
	TenantID uuid.UUID
	// RequestID: mã yêu cầu UUIDv7 sinh ở api-public (AG-Request-Id) — khóa idempotent của sổ ghi theo yêu cầu.
	RequestID  uuid.UUID
	ReceivedAt time.Time // thời điểm api-public nhận yêu cầu (event_time của sổ)
}

type apiClientKey struct{}

// WithAPIClient gắn bên gọi Public API vào context. Chỉ pf/apiclient gọi.
func WithAPIClient(ctx context.Context, c APIClient) context.Context {
	return context.WithValue(ctx, apiClientKey{}, c)
}

// APIClientFrom đọc bên gọi Public API từ context.
func APIClientFrom(ctx context.Context) (APIClient, bool) {
	c, ok := ctx.Value(apiClientKey{}).(APIClient)
	return c, ok
}
