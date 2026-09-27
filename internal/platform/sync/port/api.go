package port

import (
	"time"

	"github.com/google/uuid"
)

// EntityPubKey là entity chiều xuống do chính pf/sync cung cấp: khóa công khai
// X25519 của hộ thuộc địa bàn (đọc từ kho định danh qua client.PubKeys). Không
// module nào khác được khai báo Source cho entity này.
const EntityPubKey = "pubkey"

// HeaderDeviceID là header bắt buộc trên mọi lời gọi đồng bộ (pull, push, tải tệp).
const HeaderDeviceID = "X-Device-Id"

// HeaderPurpose là header mang mục đích xử lý khi đọc tệp minh chứng (R-42).
const HeaderPurpose = "X-Purpose"

// Vai nghiệp vụ của module (tên vai trong Keycloak).
const (
	// RoleDeviceAdmin thu hồi thiết bị (Admin API) — máy bị thu hồi xóa dữ liệu cục bộ.
	RoleDeviceAdmin = "device_admin"
	// RoleConflictResolver xem và đóng xung đột đồng bộ (Admin API, C-8).
	RoleConflictResolver = "sync_conflict_resolver"
)

// Mã kết quả theo bản ghi (trường code của kết quả rejected), ngoài validation_failed.
const (
	// CodeUnknownEntity: entity không có Sink nào nhận.
	CodeUnknownEntity = "sync.unknown_entity"
	// CodeSinkFailed: module nhận gặp lỗi hệ thống — máy GIỮ bản ghi và TỰ gửi lại
	// sau, không bắt người dùng tạo bản ghi bù.
	CodeSinkFailed = "sync.sink_failed"
)

// ParamConflictSLAHours là khóa tham số masterdata: số giờ để xử lý xung đột (C-8).
const ParamConflictSLAHours = "sync.conflict_sla_hours"

// DefaultConflictSLA dùng khi chưa có tham số đã phê duyệt (C-8 đang chờ chốt).
const DefaultConflictSLA = 72 * time.Hour

// ConflictSLA là trạng thái SLA xung đột của một tenant — chỉ số nghiệp vụ bắt
// buộc có cảnh báo (mục 19).
type ConflictSLA struct {
	Open        int64      // xung đột đang mở
	Overdue     int64      // đang mở và đã quá due_at
	OldestDueAt *time.Time // due_at sớm nhất trong các xung đột quá hạn
}

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventDeviceRevoked    = "pf.sync.device.revoked"
	EventConflictOpened   = "pf.sync.conflict.opened"
	EventConflictResolved = "pf.sync.conflict.resolved"
)

// DeviceRevokedV1 là payload của pf.sync.device.revoked. Consumer quản trị tài
// khoản có thể thu hồi phiên đăng nhập của thiết bị.
//
//ag:event pf.sync.device.revoked v1
type DeviceRevokedV1 struct {
	DeviceID  uuid.UUID `json:"device_id"`
	ActorRef  uuid.UUID `json:"actor_ref"`
	RevokedBy uuid.UUID `json:"revoked_by"`
	Reason    string    `json:"reason"`
	EventTime string    `json:"event_time"`
}

// ConflictOpenedV1 là payload của pf.sync.conflict.opened. Không có payload bản ghi.
//
//ag:event pf.sync.conflict.opened v1
type ConflictOpenedV1 struct {
	ConflictID  uuid.UUID  `json:"conflict_id"`
	Entity      string     `json:"entity"`
	RecordID    uuid.UUID  `json:"record_id"`
	DeviceID    uuid.UUID  `json:"device_id"`
	OwnerOrgRef *uuid.UUID `json:"owner_org_ref"`
	OpenedAt    string     `json:"opened_at"`
	DueAt       string     `json:"due_at"`
}

// ConflictResolvedV1 là payload của pf.sync.conflict.resolved.
//
//ag:event pf.sync.conflict.resolved v1
type ConflictResolvedV1 struct {
	ConflictID       uuid.UUID  `json:"conflict_id"`
	Entity           string     `json:"entity"`
	ResolvedBy       uuid.UUID  `json:"resolved_by"`
	ResolvedAt       string     `json:"resolved_at"`
	ResolvedRecordID *uuid.UUID `json:"resolved_record_id"`
	Overdue          bool       `json:"overdue"`
}
