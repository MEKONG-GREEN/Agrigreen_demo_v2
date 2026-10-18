package port

// PurposePolicy ràng buộc mục đích do module chủ đăng ký với loại thực thể tệp.
// Chỉ cấu hình tại gốc lắp ráp trước khi phục vụ; không nhận từ request.
type PurposePolicy struct {
	SubjectModule   string
	SubjectType     string
	RequiresConsent bool
}
