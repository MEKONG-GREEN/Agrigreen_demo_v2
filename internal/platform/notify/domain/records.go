package domain

import (
	"time"

	"github.com/google/uuid"
)

// PushToken là token đẩy của một thiết bị pf_sync, kèm ảnh chụp phạm vi/vai của
// chủ thiết bị lúc gắn (dùng để chọn thiết bị cho thông báo chủ đề và nudge).
type PushToken struct {
	DeviceID       uuid.UUID
	ActorRef       uuid.UUID
	OrgRef         *uuid.UUID
	Provider       string
	Token          string `pii:"true"` // không ghi log, không trả ra API
	Platform       string
	ScopeAll       bool
	ScopeUnits     []string
	Roles          []string
	RegisteredAt   time.Time
	UpdatedAt      time.Time
	DisabledAt     *time.Time
	DisabledReason *string
}

// Lý do ngừng dùng token (không xóa cứng).
const (
	TokenDisabledDeviceRevoked = "device_revoked"
	TokenDisabledInvalid       = "token_invalid"
	TokenDisabledOrg           = "org_deactivated"
	TokenDisabledReplaced      = "replaced"
)

// Trạng thái một lần gửi đẩy.
const (
	DeliveryPending = "pending"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"  // lỗi tạm thời — sẽ thử lại
	DeliveryDropped = "dropped" // token hỏng/ngừng dùng — không thử lại
)

// MaxDeliveryAttempts: số lần thử tối đa của một lần gửi (khớp giới hạn giao lại
// của hàng đợi, shared/inbox.DeliveryLimit).
const MaxDeliveryAttempts = 5

// Delivery là một lần gửi thông điệp dữ liệu tới một thiết bị.
type Delivery struct {
	ID             uuid.UUID
	BatchID        uuid.UUID
	DeviceID       uuid.UUID
	NotificationID *uuid.UUID
	Kind           string
	Urgent         bool
	Status         string
	Attempts       int
	CreatedAt      time.Time
	SentAt         *time.Time
}

// DeliveryJob là một lần gửi kèm token hiện tại của thiết bị (nil nếu không còn).
type DeliveryJob struct {
	Delivery
	Token *PushToken
	// Entities: nhóm dữ liệu của lệnh sync_now (rỗng = tất cả, KN US-SYNC-02).
	Entities []string
}

// DigestItem là tham chiếu nguồn chờ đưa vào email tóm tắt đã đăng ký.
type DigestItem struct {
	Reference     uuid.UUID
	AudienceUnits []string
	IssuedAt      time.Time
}

// Nudge target.
const (
	NudgeDevice = "device"
	NudgeUnit   = "unit"
)
