package authz

import "context"

type rawTokenKey struct{}

// WithRawToken gắn access token gốc (chưa giải mã) của NGƯỜI DÙNG CUỐI vào
// context — chỉ middleware xác thực gọi, sau khi token đã được xác minh.
//
// Dùng để chuyển tiếp token sang dịch vụ nội bộ tự xác minh lại (kho định danh,
// R-54): dịch vụ đích biết đúng người yêu cầu và tenant từ chính token, không
// tin tham số do module gọi truyền vào (R-16). Không bao giờ log giá trị này (R-92).
func WithRawToken(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, rawTokenKey{}, raw)
}

// RawToken trả access token gốc đã gắn bởi WithRawToken.
func RawToken(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(rawTokenKey{}).(string)
	return s, ok && s != ""
}
