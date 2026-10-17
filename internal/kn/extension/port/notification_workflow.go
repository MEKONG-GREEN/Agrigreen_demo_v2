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

const (
	// KindWorkScheduleSubmitted: lịch công tác tuần gửi duyệt / điều chỉnh chờ duyệt lại → người duyệt Cấp Tỉnh
	// (KN v7.7 FN-TI-02.07: "lịch Cán bộ xã gửi hiện ngay ở Chờ duyệt"). Ràng buộc kind trong DB do migration
	// pf_notify của P4 (000073) mở rộng — chưa có thì consumer không ghi được (người tích hợp thêm kind này).
	KindWorkScheduleSubmitted = "work_schedule_submitted"
	// KindWorkScheduleReviewed: kết quả duyệt (Đã duyệt / Yêu cầu chỉnh sửa kèm lý do) → người đăng ký
	// (KN v7.7 FN-XA-03.08 AC; kế hoạch 000073).
	KindWorkScheduleReviewed = "work_schedule_reviewed"
	// KindWorkScheduleDue: nhắc hạn đăng ký lịch tuần (cán bộ chưa gửi) hoặc lịch chờ duyệt quá hạn (người duyệt)
	// (KN v7.7 FN-TI-02.07 luồng phụ; kế hoạch 000073).
	KindWorkScheduleDue = "work_schedule_due"
)
