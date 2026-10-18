package port

import "github.com/google/uuid"

// Yêu cầu hỗ trợ từ App HTX (HTX US-SUPPORT-01) → nhiệm vụ tự sinh cho cán bộ
// khuyến nông trong vòng 1 phút (KN US-TASK-01). Migration pf 000026.

// EntitySupportRequest là entity đồng bộ HAI chiều của nông dân (docs/dev/sync-contract.md):
// chiều lên = yêu cầu gửi ngoại tuyến (op create), chiều xuống = yêu cầu của hộ mình
// kèm trạng thái nhiệm vụ.
const EntitySupportRequest = "support_request"

// Sự kiện (schema trong contracts/events/).
const (
	EventSupportRequestSubmitted = "pf.extension.support_request.submitted"
	EventTaskCreated             = "pf.extension.task.created"
)

// GpsLogConsumer là consumer của worker ghi log GPS (pf/geo) cho yêu cầu hỗ trợ SAU khi
// giao dịch đã commit (HTX US-GPS-03 AC-3, mục đích support), idempotent theo yêu cầu.
const GpsLogConsumer = "pf.extension.gps_log"

// Quy tắc giao nhiệm vụ tự sinh (chuỗi dự phòng theo cây cấu trúc, KN US-TASK-01 AC-2).
const (
	AssignCommuneOnDuty  = "commune_on_duty"  // cán bộ xã đang trực (roster DutyAt)
	AssignProvinceOnDuty = "province_on_duty" // không ai ở xã đang trực → cán bộ TTKN tỉnh đang trực
	AssignProvinceStaff  = "province_officer" // → cán bộ TTKN tỉnh đang hoạt động
	AssignAdminQueue     = "admin_queue"      // không có ai → để trống, báo quản trị tỉnh gán lại
)

// MetricSupportTaskLatency: độ trễ từ lúc máy chủ NHẬN yêu cầu hỗ trợ tới lúc nhiệm
// vụ đã ghi (giây, histogram). Cảnh báo khi p99 > 60 giây (KN US-TASK-01 — xem
// docs/runbooks/README.md).
const MetricSupportTaskLatency = "ag.extension.support_task.latency"

// SupportRequestSubmittedV1 là payload của pf.extension.support_request.submitted.
// Không có mô tả, ảnh hay toạ độ (đọc qua API trong phạm vi) — không PII.
//
//ag:event pf.extension.support_request.submitted v1
type SupportRequestSubmittedV1 struct {
	SupportRequestID uuid.UUID `json:"support_request_id"`
	TaskID           uuid.UUID `json:"task_id"`
	HouseholdRef     uuid.UUID `json:"household_ref"`
	CooperativeRef   uuid.UUID `json:"cooperative_ref"`
	CommuneCode      string    `json:"commune_code"`
	ProvinceCode     string    `json:"province_code"`
	Category         string    `json:"category"`
	Urgency          string    `json:"urgency"`
	PhotoCount       int       `json:"photo_count"`
	GPSMissing       bool      `json:"gps_missing"`
	EventTime        string    `json:"event_time"`
	ReceivedAt       string    `json:"received_at"`
}

// TaskCreatedV1 là payload của pf.extension.task.created: nhiệm vụ mới (hiện chỉ phát
// cho nhiệm vụ tự sinh từ App HTX). Consumer pf/notify báo người được giao; không ai
// được giao (admin_queue) → báo quản trị tỉnh gán lại.
//
//ag:event pf.extension.task.created v1
type TaskCreatedV1 struct {
	TaskID           uuid.UUID  `json:"task_id"`
	Source           string     `json:"source"`
	CommuneCode      string     `json:"commune_code"`
	ProvinceCode     string     `json:"province_code"`
	CooperativeRef   *uuid.UUID `json:"cooperative_ref"`
	Category         string     `json:"category"`
	Priority         string     `json:"priority"`
	Status           string     `json:"status"`
	AssigneeRef      *uuid.UUID `json:"assignee_ref"`
	AssignmentRule   string     `json:"assignment_rule"`
	SupportRequestID *uuid.UUID `json:"support_request_id"`
	DueAt            string     `json:"due_at"`
	CreatedAt        string     `json:"created_at"`
	Seq              int64      `json:"seq"`
}
