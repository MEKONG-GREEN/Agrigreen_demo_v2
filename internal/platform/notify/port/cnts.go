package port

// Thông báo cảnh báo và nhắc nộp biểu của phòng Chăn nuôi – Thủy sản (W11.43 — pf/extension → pf/notify, pf
// 20261013225942).
const (
	// KindCNTSAlert: cảnh báo mới (Khẩn / Cần chú ý) → người nhận theo bảng Mục III.5 BRD CNTS.
	KindCNTSAlert = "cnts_alert"
	// KindCNTSAlertAssigned: Cấp Tỉnh giao xử lý cảnh báo → người được giao.
	KindCNTSAlertAssigned = "cnts_alert_assigned"
	// KindCNTSAlertOverdue: cảnh báo quá hạn xử lý → Lãnh đạo phòng, người được giao.
	KindCNTSAlertOverdue = "cnts_alert_overdue"
	// KindCNTSFormReminder: nhắc nộp biểu kỳ CNTS (trước hạn, khi trễ, nhắc lặp — UR-TINH-08).
	KindCNTSFormReminder = "cnts_form_reminder"
)

// ConsumerCNTS: cảnh báo, nhắc nộp biểu kỳ CNTS (W11.43).
const ConsumerCNTS = "pf.notify.cnts"
