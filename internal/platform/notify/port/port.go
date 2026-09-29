// Package port là giao diện công khai DUY NHẤT của module pf/notify mà module
// khác được import (R-02): tên vai, loại thông báo, sự kiện, tên consumer.
package port

import (
	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// EntityNotification là entity chiều xuống của pf/sync: hộp thư của người gọi.
const EntityNotification = "notification"

// Loại thông báo (cột kind, trường "kind" của thông điệp đẩy).
const (
	KindDirective      = "directive"       // chỉ đạo từ TTKN (KN US-TASK-03)
	KindAlert          = "alert"           // cảnh báo khẩn (HTX US-NEWS-03)
	KindDocument       = "document"        // tài liệu/tin mới hoặc cập nhật (US-LIB-06)
	KindTaskEscalated  = "task_escalated"  // nhiệm vụ leo thang lên tỉnh (KN US-TASK-02)
	KindSyncConflict   = "sync_conflict"   // xung đột đồng bộ chờ xử lý (C-8)
	KindOrgDeactivated = "org_deactivated" // tổ chức bị vô hiệu hóa
	// KindHouseholdRegistered / KindHouseholdMembershipChanged: nông hộ mới từ App HTX,
	// nông hộ vào/rời/chuyển HTX → cán bộ khuyến nông của xã (KN US-HTX-05, migration 000028).
	KindHouseholdRegistered        = "household_registered"
	KindHouseholdMembershipChanged = "household_membership_changed"
	// KindTaskAssigned: nhiệm vụ tự sinh từ yêu cầu hỗ trợ App HTX → cán bộ được giao (KN US-TASK-01).
	KindTaskAssigned = "task_assigned"
	// KindSyncNow là thông điệp dữ liệu "đồng bộ ngay" (US-SYNC-02) — không tạo thông báo.
	KindSyncNow = "sync_now"
)

// Mức độ.
const (
	SeverityUrgent = "urgent"
	SeverityNormal = "normal"
)

// MaxNudgeEntities: số nhóm dữ liệu tối đa trong một yêu cầu đồng bộ ngay (KN US-SYNC-02).
const MaxNudgeEntities = 30

// NudgeRoles là các vai được yêu cầu thiết bị đồng bộ ngay (US-SYNC-02).
var NudgeRoles = []string{
	partyport.RoleKNProvinceAdmin, partyport.RoleKNNationalAdmin, syncport.RoleDeviceAdmin, authz.RolePlatformAdmin,
}

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventNotificationCreated = "pf.notify.notification.created"
	EventPushRequested       = "pf.notify.push.requested"
	EventProducerName        = "pf/notify"
)

// Tên consumer trong cmd/worker (-modules), một consumer mỗi nhóm sự kiện.
const (
	ConsumerDirective = "pf.notify.directive"
	ConsumerContent   = "pf.notify.content"
	ConsumerTask      = "pf.notify.task"
	ConsumerSync      = "pf.notify.sync"
	ConsumerParty     = "pf.notify.party"
	ConsumerPush      = "pf.notify.push"
)

// JobEmailDigest là tên job gửi email tóm tắt chỉ đạo khẩn trong cmd/worker (-jobs).
const JobEmailDigest = "notify_email_digest"

// NotificationCreatedV1 là payload của pf.notify.notification.created: một thông
// báo đã vào hộp thư. Không có tiêu đề/nội dung (đọc qua API) và không có dữ liệu
// cá nhân. recipient_ref rỗng với thông báo chủ đề.
//
//ag:event pf.notify.notification.created v1
type NotificationCreatedV1 struct {
	NotificationID uuid.UUID  `json:"notification_id"`
	Kind           string     `json:"kind"`
	Severity       string     `json:"severity"`
	RecipientRef   *uuid.UUID `json:"recipient_ref"`
	Topic          bool       `json:"topic"`
	SubjectRef     *uuid.UUID `json:"subject_ref"`
	SourceEventID  uuid.UUID  `json:"source_event_id"`
	CreatedAt      string     `json:"created_at"`
}

// PushRequestedV1 là payload của pf.notify.push.requested: một lô lần gửi đẩy đã
// xếp hàng trong pf_notify.push_delivery. Consumer pf.notify.push gửi từng thiết bị.
//
//ag:event pf.notify.push.requested v1
type PushRequestedV1 struct {
	BatchID    uuid.UUID `json:"batch_id"`
	Kind       string    `json:"kind"`
	Deliveries int       `json:"deliveries"`
}
