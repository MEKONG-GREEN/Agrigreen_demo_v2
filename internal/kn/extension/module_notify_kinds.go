package extension

import (
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// NotificationKinds declares source-owned persistence keys without capability aliases.
func NotificationKinds() notifyport.KindRegistration {
	return notifyport.KindRegistration{Kinds: []string{
		extport.KindCNTSAlert,
		extport.KindCNTSAlertAssigned,
		extport.KindCNTSAlertOverdue,
		extport.KindCNTSDutyChanged,
		extport.KindCNTSFormReminder,
		extport.KindDirective,
		extport.KindReportAdjustmentRequested,
		extport.KindReportPeriodReopened,
		extport.KindReportReminder,
		extport.KindReportTaskAssigned,
		extport.KindReportTaskDue,
		extport.KindTaskAssigned,
		extport.KindTaskEscalated,
		extport.KindWorkScheduleDue,
		extport.KindWorkScheduleReviewed,
		extport.KindWorkScheduleSubmitted,
	}}
}
