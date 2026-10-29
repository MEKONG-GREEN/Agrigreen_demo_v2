package port

import "github.com/google/uuid"

// W11.53b (#158 — BRD CNTS v3.7 FN-19 AC-02, UR-QG-01): thông báo mẫu biểu dự án KNQG.

// EventCNTSKNQGChanged: biểu dự án KNQG đổi trạng thái cần người khác xử lý — chủ nhiệm gửi Mẫu 07 (Cấp TW xác nhận),
// Cấp TW xác nhận/trả lại Mẫu 07 (chủ nhiệm), chủ nhiệm trình Mẫu 01 (lãnh đạo phòng). Ghi cùng giao dịch với dòng sổ
// pf_extension.cnts_knqg_doc (doc_id). Người nhận tra NGOÀI giao dịch (pf/accounts, trước khi ghi) — consumer
// pf.notify.cnts_knqg chỉ dựng nội dung.
const EventCNTSKNQGChanged = "pf.extension.cnts_knqg.changed"

// Hành động của sự kiện (trùng tên thao tác của biểu).
const (
	CNTSKNQGSent      = "send"    // Mẫu 07 đã gửi Quốc gia → Cấp TW
	CNTSKNQGConfirmed = "confirm" // Cấp TW xác nhận Mẫu 07 → chủ nhiệm
	CNTSKNQGReturned  = "return"  // Cấp TW trả lại Mẫu 07 → chủ nhiệm
	CNTSKNQGSubmitted = "submit"  // chủ nhiệm trình Mẫu 01 → lãnh đạo phòng
)

// Loại thông báo (pf/notify — đăng ký qua extension.KNQGNotificationKinds).
const (
	KindCNTSKNQGSent      = "cnts_knqg_sent"
	KindCNTSKNQGReviewed  = "cnts_knqg_reviewed"
	KindCNTSKNQGSubmitted = "cnts_knqg_submitted"
)

// NotifyConsumerCNTSKNQG là hàng đợi pf/notify của sự kiện biểu dự án KNQG (thứ tự 18, sau pf.notify.hrm).
const (
	NotifyConsumerCNTSKNQG      = "pf.notify.cnts_knqg"
	NotifyConsumerCNTSKNQGOrder = 18
)

// CNTSKNQGChangedV1 là payload của pf.extension.cnts_knqg.changed. Không dữ liệu cá nhân: mã dự án và tên dự án là
// thông tin hành chính; ý kiến trả lại KHÔNG đi kèm (người nhận mở biểu để đọc).
//
//ag:event pf.extension.cnts_knqg.changed v1
type CNTSKNQGChangedV1 struct {
	DocID         uuid.UUID   `json:"doc_id"`
	DocRef        uuid.UUID   `json:"doc_ref"`
	ProjectRef    uuid.UUID   `json:"project_ref"`
	ProjectCode   string      `json:"project_code"`
	ProjectName   string      `json:"project_name"`
	ProvinceCode  string      `json:"province_code"`
	Form          string      `json:"form"`
	Period        string      `json:"period"`
	Action        string      `json:"action"`
	Status        string      `json:"status"`
	RecipientRefs []uuid.UUID `json:"recipient_refs"`
	ActorRef      uuid.UUID   `json:"actor_ref"`
	Department    string      `json:"department"`
	EventTime     string      `json:"event_time"`
}
