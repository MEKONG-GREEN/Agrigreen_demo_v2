package port

import "github.com/google/uuid"

// Nhiệm vụ tự sinh từ yêu cầu hỗ trợ App HTX (KN US-TASK-01). Từ PA1-HTX-SUPPORT-GPSLOG (#313) yêu cầu hỗ trợ thuộc
// module chủ HTX pf/cooperative (pf_coop.support_request) và không còn sinh nhiệm vụ KN; bảng cũ
// pf_extension.support_request (pf 000026) giữ chỉ đọc. Sự kiện và quy tắc giao dưới đây giữ cho nhiệm vụ đã có.

// EventTaskCreated: nhiệm vụ mới (schema trong contracts/events/).
const EventTaskCreated = "pf.extension.task.created"

// Quy tắc giao nhiệm vụ tự sinh (chuỗi dự phòng theo cây cấu trúc, KN US-TASK-01 AC-2).
const (
	AssignCommuneOnDuty  = "commune_on_duty"  // cán bộ xã đang trực (roster DutyAt)
	AssignProvinceOnDuty = "province_on_duty" // không ai ở xã đang trực → cán bộ TTKN tỉnh đang trực
	AssignProvinceStaff  = "province_officer" // → cán bộ TTKN tỉnh đang hoạt động
	AssignAdminQueue     = "admin_queue"      // không có ai → để trống, báo quản trị tỉnh gán lại
)

// TaskCreatedV1 là payload của pf.extension.task.created: nhiệm vụ mới (chỉ phát cho nhiệm vụ tự sinh từ App HTX —
// từ #313 không còn nguồn phát, giữ hợp đồng và consumer cho sự kiện đã có). Consumer pf/notify báo người được giao;
// không ai được giao (admin_queue) → báo quản trị tỉnh gán lại.
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
