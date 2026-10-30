package port

// NOTIFY-HCTH-KN (#50, con của #48): sự kiện, loại thông báo, người nhận thông báo HCTH do pf/hrm SỞ HỮU. pf/notify chỉ
// nhận đăng ký qua registry chung (notify/port KindRegistration, ConsumerRegistration, EventBuilderRegistration) — không có
// hằng hrm.* hay chính sách HCTH trong pf/notify.
//
// Quy ước (hợp đồng #48, 21/10): sự kiện outbox có namespace dấu chấm + phiên bản (pf.hrm.*, contracts/events/); loại thông
// báo trong hộp thư là token gạch dưới hrm_* (khóa notification_kind_key_shape ^[a-z][a-z_]{0,39}$).

import (
	"context"

	"github.com/google/uuid"
)

// Tên nguồn sự kiện và sự kiện outbox của pf/hrm.
const (
	EventProducerName = "pf/hrm"
	// EventPromotionDueDetected: job hrm_promotion_scan (W11.25, FN-09 bước 1) thấy trường hợp cần Phòng HCTH xử lý và số
	// đếm khác lần tính trước. Ghi CÙNG giao dịch với dòng sổ promotion_scan (tối đa một sự kiện mỗi tenant mỗi ngày).
	EventPromotionDueDetected = "pf.hrm.promotion_due.detected"
	// EventPlanReminded: Phòng HCTH/Admin bấm "Nhắc phòng chưa lập" (W11.27, FN-11) — ghi CÙNG giao dịch với sổ
	// plan_reminder.
	EventPlanReminded     = "pf.hrm.plan.reminded"
	EventAppraisalChanged = "pf.hrm.appraisal.changed"
)

// Loại thông báo (hộp thư pf/notify) do pf/hrm sở hữu.
const (
	// KindPromotionDue: kết quả tính mốc nâng bậc hằng ngày → Phòng HCTH + Admin (FN-09 bước 1).
	KindPromotionDue = "hrm_promotion_due"
	// KindPlanReminder: nhắc lập/gửi kế hoạch nhân lực năm → lãnh đạo các đơn vị chưa gửi (FN-11).
	KindPlanReminder = "hrm_plan_reminder"
	KindAppraisal    = "hrm_appraisal"
)

// Hàng đợi consumer của pf/notify cho sự kiện pf/hrm. Thứ tự là vị trí ổn định trong danh mục đăng ký (1–16 đã dùng).
const (
	NotifyConsumerHRM      = "pf.notify.hrm"
	NotifyConsumerHRMOrder = 17
)

// PromotionDueDetectedV1 là payload pf.hrm.promotion_due.detected: chỉ số đếm và ngày — không mã nhân sự, không họ tên
// (RB-3). ScanID là dòng sổ promotion_scan (tham chiếu đợt tính).
//
//ag:event pf.hrm.promotion_due.detected v1
type PromotionDueDetectedV1 struct {
	ScanID  uuid.UUID            `json:"scan_id"`
	Day     string               `json:"day"`
	Windows []PromotionDueWindow `json:"windows"`
	Overdue int                  `json:"overdue"`
	Fail    int                  `json:"fail"`
	Review  int                  `json:"review"`
	Missing int                  `json:"missing"`
}

// PromotionDueWindow là số kỳ NLTX/TNVK đến hạn trong Months tháng tới (mốc nhắc của tham số nâng bậc).
type PromotionDueWindow struct {
	Months int `json:"months"`
	Count  int `json:"count"`
}

// PlanRemindedV1 là payload pf.hrm.plan.reminded: kỳ, Nút sở hữu phòng ban của phiên người nhắc (Trung tâm) và các đơn
// vị nội bộ CHƯA gửi tại lúc nhắc. Không PII.
//
//ag:event pf.hrm.plan.reminded v1
type PlanRemindedV1 struct {
	ReminderID uuid.UUID `json:"reminder_id"`
	Year       int       `json:"year"`
	PeriodRef  uuid.UUID `json:"period_ref"`
	NodeRef    uuid.UUID `json:"node_ref"`
	Units      []string  `json:"units"`
	DueAt      string    `json:"due_at"`
	ActorRef   uuid.UUID `json:"actor_ref"`
}

// AppraisalChangedV1 chỉ có tham chiếu và bước; không điểm, nhận xét, tên hay địa chỉ liên hệ.
// Targets lấy từ AccountRef của snapshot; Units tra lãnh đạo tại NodeRef qua cổng chỉ đọc ở consumer.
//
//ag:event pf.hrm.appraisal.changed v1
type AppraisalChangedV1 struct {
	NoticeID  uuid.UUID   `json:"notice_id"`
	PeriodRef uuid.UUID   `json:"period_ref"`
	StaffRef  *uuid.UUID  `json:"staff_ref,omitempty"`
	NodeRef   uuid.UUID   `json:"node_ref"`
	Year      int         `json:"year"`
	Quarter   int         `json:"quarter"`
	Action    string      `json:"action"`
	Step      string      `json:"step"`
	Targets   []uuid.UUID `json:"targets"`
	Units     []string    `json:"units"`
	Center    bool        `json:"center"`
	DueAt     string      `json:"due_at"`
	ActorRef  uuid.UUID   `json:"actor_ref"`
}

// NotifyDirectory là cổng CHỈ ĐỌC tra người nhận thông báo HCTH, do gốc lắp ráp nối (KN: pf/accounts). Tenant truyền tường
// minh — consumer của worker không có Principal và không được giả Principal; tenant lấy từ phong bì sự kiện đã xác thực.
// Chỉ trả mã tham chiếu tài khoản (không tên, không email).
type NotifyDirectory interface {
	// RoleHolders trả tài khoản có lần gán vai role CÒN HIỆU LỰC ở phòng ban dept (rỗng = mọi phòng ban).
	RoleHolders(ctx context.Context, tenantID uuid.UUID, role, dept string) ([]uuid.UUID, error)
	// UnitManagers trả, theo từng mã đơn vị nội bộ trong units, lãnh đạo đơn vị (cấp quản lý/nhóm có quyền lãnh đạo, KHÔNG
	// phải ngữ cảnh chỉ xem) có lần gán Cấp Tỉnh còn hiệu lực tại Nút nodeRef. Đơn vị không có lãnh đạo → vắng trong map.
	UnitManagers(ctx context.Context, tenantID, nodeRef uuid.UUID, units []string) (map[string][]uuid.UUID, error)
}
