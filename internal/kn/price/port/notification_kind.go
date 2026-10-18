package port

// Loại thông báo của Báo giá nông sản (CHECK kind — pf 20261013070025).
const (
	// KindPriceReminder: nhắc nhập giá lần 1 / lần 2 → cán bộ Xã (FN-XA-03.04; W11.74 tạm dùng report_task_due).
	KindPriceReminder = "price_reminder"
	// KindPriceAlert: cảnh báo biến động Tạm tính mới khi Xã gửi → Cấp Tỉnh phụ trách xã + QTNV (FN-TI-02.03 B1).
	KindPriceAlert = "price_alert"
	// KindPriceVerifyRequest: yêu cầu xác minh → cán bộ Xã phụ trách (FN-TI-02.03 B3, FN-XA-03.05).
	KindPriceVerifyRequest = "price_verify_request"
	// KindPriceVerifyReply: Xã phản hồi xác minh → Cấp Tỉnh phụ trách xã + QTNV (FN-XA-03.05 B4).
	KindPriceVerifyReply = "price_verify_reply"
	// KindPriceVerifyDecision: kết quả xử lý cảnh báo / hiệu chỉnh → cán bộ Xã (FN-TI-02.03 B6, FN-TI-02.04).
	KindPriceVerifyDecision = "price_verify_decision"
	// KindPriceLockSummary: tổng hợp lúc khóa nhập (12:00) → Cấp Tỉnh + QTNV (FN-TI-02.03 B4).
	KindPriceLockSummary = "price_lock_summary"
	// KindPriceBoardApproved: bảng giá ngày đã duyệt (phiên bản mới) → Cấp Tỉnh + QTNV (FN-TI-02.05).
	KindPriceBoardApproved = "price_board_approved"
	// KindPriceBoardReopened: bảng giá ngày đang mở duyệt → Cấp Tỉnh + QTNV (FN-TI-02.06).
	KindPriceBoardReopened = "price_board_reopened"
	// KindPriceProxyEntry: Cấp Tỉnh nhập thay báo giá Nộp muộn → cán bộ Xã phụ trách (FN-TI-02.09; W11.77, pf
	// 20261013164236).
	KindPriceProxyEntry = "price_proxy_entry"
)
