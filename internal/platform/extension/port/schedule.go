package port

// KN v7.7 gói P5 — lịch công tác tuần (FN-XA-03.08 đăng ký, FN-TI-02.07 duyệt). Kế hoạch
// docs/brd/kn-v7.7-plan.md §3 P5; migration pf 000075.

import (
	"context"
	"time"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Vai KN v7.7 của ngữ cảnh phiên (gap §4.5 phương án A) — KHÔNG phải vai realm. Trùng tên đối tượng menu
// của pf/party (P2) để P1 dùng chung một bộ mã.
const (
	KNRoleTW    = partyport.AudienceTW    // Cấp Quốc gia — không có quyền với lịch công tác (ma trận §5)
	KNRoleTinh  = partyport.AudienceTinh  // Cấp Tỉnh — đăng ký lịch mình, xem + duyệt lịch cùng phòng ban, cùng tỉnh
	KNRoleXa    = partyport.AudienceXa    // Cấp Xã — đăng ký lịch mình, gửi duyệt
	KNRoleAdmin = partyport.AudienceAdmin // Admin — xem mọi lịch (R)
)

// Cấp quản lý (trường "Cấp quản lý" của gán vai trò, Hình 25b). Danh sách giá trị chờ khách xác nhận (Q-05).
const (
	MgmtLevelHead       = "truong_phong"     // Trưởng phòng — mặc định là người duyệt lịch
	MgmtLevelDeputyHead = "pho_truong_phong" // Phó trưởng phòng — duyệt được nếu khách xác nhận ủy quyền (Q-05)
	MgmtLevelSpecialist = "chuyen_vien"      // Chuyên viên
)

// KNContext là ngữ cảnh KN của người gọi: vai trò, phòng ban, tỉnh, xã phụ trách, cấp quản lý.
type KNContext struct {
	Role         string // KNRoleTW | KNRoleTinh | KNRoleXa | KNRoleAdmin
	Department   string // mã phòng ban (P2); rỗng với Admin
	ProvinceCode string // tỉnh của Nút (Cấp Tỉnh, Cấp Xã)
	Communes     []string
	// MgmtLevel là cấp quản lý của vai trò. Rỗng = không biết (chưa có P1) → mọi Cấp Tỉnh cùng phòng ban,
	// cùng tỉnh được coi là người duyệt (tạm, chờ khách Q-05).
	MgmtLevel string
}

// KNContextResolver (do P1 cung cấp qua gốc lắp ráp) trả ngữ cảnh KN của request hiện tại (X-AG-Context).
// ok = false: request không mang ngữ cảnh KN → module suy ngữ cảnh từ vai realm + phạm vi đơn vị (đường cũ).
type KNContextResolver interface {
	KNContext(ctx context.Context) (c KNContext, ok bool, err error)
}

// KNOfficer là một cán bộ có vai KN trong một phòng ban của một tỉnh.
type KNOfficer struct {
	ActorRef     uuid.UUID
	Level        string // ScheduleLevelCommune | ScheduleLevelProvince
	ProvinceCode string
	Communes     []string
	MgmtLevel    string // rỗng = không biết
}

// KNDirectory (do P1 cung cấp qua gốc lắp ráp, từ role_assignment_current) liệt kê cán bộ KN của phòng ban ×
// tỉnh: nguồn tab "Chưa đăng ký", người nhận nhắc và người duyệt nhận thông báo. Thiếu → module dùng danh bạ
// trực pf_extension.staff (mọi cán bộ coi là Phòng Trồng trọt, cấp quản lý không biết). Tenant truyền tường
// minh vì job của worker không có Principal.
type KNDirectory interface {
	Officers(ctx context.Context, tenantID uuid.UUID, department, provinceCode string) ([]KNOfficer, error)
}

// TablePDF (tùy chọn, pf/reports qua gốc lắp ráp) dựng PDF khổ A4 ngang có phông tiếng Việt (ADR-0014) từ một
// bảng chữ — dùng cho "Xuất PDF" lịch công tác. meta là các dòng "nhãn: giá trị" dưới tiêu đề.
type TablePDF interface {
	RenderTablePDF(title string, meta [][2]string, header []string, rows [][]string, at time.Time) ([]byte, error)
}

// Cấp của người đăng ký lịch.
const (
	ScheduleLevelCommune  = "commune"
	ScheduleLevelProvince = "province"
)

// Khóa tham số lịch công tác (pf/masterdata, tạm — chờ khách Q-05, Q-16). Khóa có chiều tỉnh
// "<khóa>:<mã tỉnh>" được ưu tiên hơn khóa chung; thiếu cả hai → mặc định ghi bên cạnh.
const (
	// ParamScheduleSubmitDueOffsetHours: hạn đăng ký = 00:00 Thứ Hai của tuần (giờ VN) + số giờ. Mặc định −55
	// (17:00 Thứ Sáu tuần trước).
	ParamScheduleSubmitDueOffsetHours = "extension.work_schedule_submit_due_offset_hours"
	// ParamScheduleReviewDueOffsetHours: hạn duyệt = 00:00 Thứ Hai + số giờ. Mặc định 0.
	ParamScheduleReviewDueOffsetHours = "extension.work_schedule_review_due_offset_hours"
	// ParamScheduleRemindBeforeHours: nhắc cán bộ chưa gửi trước hạn đăng ký bao nhiêu giờ. Mặc định 24.
	ParamScheduleRemindBeforeHours = "extension.work_schedule_remind_before_hours"
	// ParamScheduleApproverLevels (chuỗi, phân cách dấu phẩy): cấp quản lý được duyệt. Mặc định "truong_phong".
	ParamScheduleApproverLevels = "extension.work_schedule_approver_levels"
	// ParamScheduleProvinceApproval (chuỗi): lịch cán bộ Cấp Tỉnh — "peer" (người duyệt khác người đăng ký, mặc
	// định) hoặc "auto" (tự duyệt khi gửi). Q-16.
	ParamScheduleProvinceApproval = "extension.work_schedule_province_approval"
)

// Giá trị của ParamScheduleProvinceApproval.
const (
	ProvinceApprovalPeer = "peer"
	ProvinceApprovalAuto = "auto"
)

// Sự kiện lịch công tác (schema trong contracts/events/).
const (
	EventWorkScheduleSubmitted = "pf.extension.work_schedule.submitted"
	EventWorkScheduleReviewed  = "pf.extension.work_schedule.reviewed"
	EventWorkScheduleReminded  = "pf.extension.work_schedule.reminded"
)

// Quyết định duyệt.
const (
	ScheduleDecisionApproved        = "approved"
	ScheduleDecisionChangeRequested = "change_requested"
)

// Loại nhắc.
const (
	ScheduleReminderSubmitDue     = "submit_due"
	ScheduleReminderReviewOverdue = "review_overdue"
)

// WorkScheduleSubmittedV1 là payload của pf.extension.work_schedule.submitted: lịch tuần vừa gửi duyệt (lần đầu,
// gửi lại sau yêu cầu chỉnh sửa, hoặc điều chỉnh lịch đã duyệt). approver_refs là người duyệt tìm được lúc gửi
// (cùng phòng ban, cùng tỉnh, cấp quản lý được duyệt, khác người gửi); rỗng → consumer báo chủ đề cho Cấp Tỉnh
// của tỉnh. Không có dữ liệu cá nhân, không có nội dung lịch.
//
//ag:event pf.extension.work_schedule.submitted v1
type WorkScheduleSubmittedV1 struct {
	ScheduleID     uuid.UUID   `json:"schedule_id"`
	OwnerRef       uuid.UUID   `json:"owner_ref"`
	OwnerLevel     string      `json:"owner_level"`
	DepartmentCode string      `json:"department_code"`
	ProvinceCode   string      `json:"province_code"`
	CommuneCodes   []string    `json:"commune_codes"`
	WeekStart      string      `json:"week_start"`
	Version        int         `json:"version"`
	Amendment      bool        `json:"amendment"`
	Late           bool        `json:"late"`
	ApproverRefs   []uuid.UUID `json:"approver_refs"`
	EventTime      string      `json:"event_time"`
}

// WorkScheduleReviewedV1 là payload của pf.extension.work_schedule.reviewed: kết quả duyệt (Đã duyệt / Yêu cầu
// chỉnh sửa kèm lý do) → người đăng ký nhận thông báo kèm lý do (FN-XA-03.08 AC). Lý do là văn bản nghiệp vụ đã
// chặn SĐT/CCCD ở tầng ứng dụng.
//
//ag:event pf.extension.work_schedule.reviewed v1
type WorkScheduleReviewedV1 struct {
	ScheduleID     uuid.UUID `json:"schedule_id"`
	OwnerRef       uuid.UUID `json:"owner_ref"`
	ReviewerRef    uuid.UUID `json:"reviewer_ref"`
	DepartmentCode string    `json:"department_code"`
	ProvinceCode   string    `json:"province_code"`
	WeekStart      string    `json:"week_start"`
	Version        int       `json:"version"`
	Decision       string    `json:"decision"`
	Reason         *string   `json:"reason"`
	Auto           bool      `json:"auto"`
	EventTime      string    `json:"event_time"`
}

// WorkScheduleRemindedV1 là payload của pf.extension.work_schedule.reminded: job nhắc cán bộ chưa gửi lịch trước
// hạn đăng ký (submit_due) hoặc nhắc người duyệt khi lịch chờ duyệt quá hạn duyệt (review_overdue).
//
//ag:event pf.extension.work_schedule.reminded v1
type WorkScheduleRemindedV1 struct {
	Kind           string      `json:"kind"`
	DepartmentCode string      `json:"department_code"`
	ProvinceCode   string      `json:"province_code"`
	WeekStart      string      `json:"week_start"`
	DueAt          string      `json:"due_at"`
	RecipientRefs  []uuid.UUID `json:"recipient_refs"`
	Pending        int         `json:"pending"`
}
