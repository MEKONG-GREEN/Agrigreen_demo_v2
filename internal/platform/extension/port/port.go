// Package port là giao diện công khai DUY NHẤT của module pf/extension (nghiệp
// vụ khuyến nông, ADR-0011) mà module khác được import (R-02).
package port

import (
	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Vai nghiệp vụ (tên vai trong Keycloak). Dùng chung tên vai KN với pf/party để
// một tài khoản cán bộ có cùng quyền ở mọi module của App Khuyến nông.
const (
	// RoleKNOfficer: cán bộ xã / Tổ KNCĐ — nhận, xử lý nhiệm vụ; gửi báo cáo hiện trường trong xã.
	RoleKNOfficer = partyport.RoleKNOfficer
	// RoleKNProvinceAdmin: cán bộ TTKN tỉnh — giám sát, phân công, chỉ đạo, chốt kỳ, phân ca trực trong tỉnh.
	RoleKNProvinceAdmin = partyport.RoleKNProvinceAdmin
	// RoleKNNationalAdmin: cán bộ TTKN Quốc gia — xem toàn quốc, chỉ đạo toàn quốc, phân công hàng loạt.
	RoleKNNationalAdmin = partyport.RoleKNNationalAdmin
	// RoleCoopManager: quản lý HTX — tạo yêu cầu hỗ trợ cho chính HTX mình, xem danh bạ trực.
	RoleCoopManager = partyport.RoleCoopManager
)

// Khóa tham số (pf/masterdata, tra theo thời điểm của sự việc — mục 16).
const (
	// ParamTaskSLAHours: số giờ SLA phản hồi nhiệm vụ hỗ trợ (US-TASK-02 AC-2). Mặc định 24.
	ParamTaskSLAHours = "extension.task_sla_hours"
	// ParamDutyStatusTTLHours: trạng thái trực tự khai hết hạn sau số giờ này → "không xác định" (US-DIR-01). Mặc định 12.
	ParamDutyStatusTTLHours = "extension.duty_status_ttl_hours"
)

// Entity đồng bộ (docs/dev/sync-contract.md).
const (
	EntityTask        = "task"         // chiều xuống
	EntityDirective   = "directive"    // chiều xuống
	EntityTaskUpdate  = "task_update"  // chiều lên
	EntityFieldReport = "field_report" // chiều lên
)

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventTaskEscalated     = "pf.extension.task.escalated"
	EventDirectiveIssued   = "pf.extension.directive.issued"
	EventProducerName      = "pf/extension"
	EscalationReasonSLA    = "sla_overdue"
	EscalationReasonManual = "manual"
)

// TaskEscalatedV1 là payload của pf.extension.task.escalated: nhiệm vụ quá SLA
// chưa hoàn thành (hoặc cán bộ chuyển tiếp) được leo thang lên TTKN tỉnh
// (US-TASK-02 AC-2). Consumer thông báo cho cán bộ TTKN tỉnh. Không chứa PII.
//
//ag:event pf.extension.task.escalated v1
type TaskEscalatedV1 struct {
	TaskID           uuid.UUID  `json:"task_id"`
	CommuneCode      string     `json:"commune_code"`
	ProvinceCode     string     `json:"province_code"`
	EscalationOrgRef *uuid.UUID `json:"escalation_org_ref"`
	Reason           string     `json:"reason"`
	Status           string     `json:"status"`
	Priority         string     `json:"priority"`
	Category         string     `json:"category"`
	AssigneeRef      *uuid.UUID `json:"assignee_ref"`
	DueAt            string     `json:"due_at"`
	EventTime        string     `json:"event_time"`
	Seq              int64      `json:"seq"`
}

// DirectiveIssuedV1 là payload của pf.extension.directive.issued: chỉ đạo khẩn đã
// ban hành tới đúng vùng (US-TASK-03). Consumer gửi thông báo đẩy tới từng cán bộ
// nhận; tiêu đề/nội dung đọc qua API, không nằm trong sự kiện.
//
//ag:event pf.extension.directive.issued v1
type DirectiveIssuedV1 struct {
	DirectiveID   uuid.UUID   `json:"directive_id"`
	Severity      string      `json:"severity"`
	AudienceUnits []string    `json:"audience_units"`
	ProvinceCodes []string    `json:"province_codes"`
	TargetRefs    []uuid.UUID `json:"target_refs"`
	IssuerRef     uuid.UUID   `json:"issuer_ref"`
	IssuedAt      string      `json:"issued_at"`
	ExpiresAt     *string     `json:"expires_at"`
}
