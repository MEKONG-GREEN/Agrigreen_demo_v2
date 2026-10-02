package port

// Báo cáo Chi cục KN v7.7 (FN-TI-02.06): xuất tệp toàn tỉnh từ lần tổng hợp gần nhất (pf/extension) rồi gửi email
// kèm tệp/link tới hộp thư đơn vị của Chi cục. Gửi SMTP chạy ở worker (consumer DispatchConsumer, R-52).

// Sự kiện và consumer của báo cáo Chi cục.
const (
	EventDispatchRequested = "pf.reports.dispatch.requested"
	// DispatchConsumer gửi email Chi cục cho pf.reports.dispatch.requested.
	DispatchConsumer = "pf.reports.dispatch"
)

// DispatchRequestedV1 là payload của pf.reports.dispatch.requested: Cấp Tỉnh bấm "Gửi email" (hoặc gửi lại). Worker
// nạp người nhận, tệp từ bảng dispatch — sự kiện không chứa địa chỉ email (R-14).
//
//ag:event pf.reports.dispatch.requested v1
type DispatchRequestedV1 struct {
	DispatchID  string `json:"dispatch_id"`
	Attempt     int    `json:"attempt"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
}
