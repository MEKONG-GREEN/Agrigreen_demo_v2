package port

// Notification kind owned by directive policy.
const (
	KindDirective = "directive" // chỉ đạo từ TTKN (KN US-TASK-03)
)

// JobEmailDigest là tên job gửi email tóm tắt chỉ đạo khẩn trong cmd/worker (-jobs).
const JobEmailDigest = "notify_email_digest"
