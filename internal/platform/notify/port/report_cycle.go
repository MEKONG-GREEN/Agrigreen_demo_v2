package port

// Kiểu thông báo vòng đời báo cáo KN v7.7 (gói P4, pf/extension → pf/notify; ràng buộc kind ở pf 000073).
const (
	// KindReportTaskAssigned: đợt phát hành sinh công việc nộp biểu cho xã phụ trách (FN-TI-02.02 AC: Cán bộ xã
	// nhận công việc và thông báo ngay sau phát hành). Một thông báo mỗi cán bộ mỗi đợt.
	KindReportTaskAssigned = "report_task_assigned"
	// KindReportTaskDue: tự nhắc trước hạn / khi trễ hạn theo cấu hình (FN-TI-02.03, Q-15).
	KindReportTaskDue = "report_task_due"
	// KindReportReminder: Cấp Tỉnh "Gửi nhắc nhở" kèm nội dung (FN-TI-02.03 AC: nhận trong 1 phút).
	KindReportReminder = "report_reminder"
	// KindReportAdjustmentRequested: yêu cầu điều chỉnh kèm lý do (FN-TI-02.04 AC).
	KindReportAdjustmentRequested = "report_adjustment_requested"
	// KindReportPeriodReopened: mở lại kỳ cho xã/biểu, kèm hạn mở (FN-TI-02.04).
	KindReportPeriodReopened = "report_period_reopened"
)
