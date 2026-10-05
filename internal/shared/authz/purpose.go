package authz

import (
	"context"
	"strings"
)

// Mục đích xử lý của request (mã có cấu trúc, vd "khuyen-nong", "mrv.evidence" — ADR-0030 purpose_code,
// R-42). Module nhận mục đích (header X-Purpose, thân request) và đã kiểm nó gắn vào context bằng
// WithPurpose; internal/shared/db đặt thành biến giao dịch app.purpose (W2.4). Không có → rỗng.

type purposeKey struct{}

// WithPurpose gắn mã mục đích đã kiểm vào context. Chuỗi rỗng (sau khi bỏ khoảng trắng) xóa mục đích.
func WithPurpose(ctx context.Context, code string) context.Context {
	return context.WithValue(ctx, purposeKey{}, strings.TrimSpace(code))
}

// PurposeFrom trả mã mục đích của request, rỗng nếu chưa gắn.
func PurposeFrom(ctx context.Context) string {
	s, _ := ctx.Value(purposeKey{}).(string)
	return s
}
