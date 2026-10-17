package extension

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// NotificationConsumers declares KN source subscriptions; notify keeps inbox and delivery authority.
func NotificationConsumers() notifyport.ConsumerRegistration {
	return notifyport.ConsumerRegistration{Definitions: []notifyport.ConsumerDefinition{
		{Name: notifyport.ConsumerDirective, Order: 1, Events: []string{port.EventDirectiveIssued}},
		{Name: notifyport.ConsumerTask, Order: 3, Events: []string{port.EventTaskEscalated, port.EventTaskCreated}},
		{Name: notifyport.ConsumerReportCycle, Order: 7, Events: []string{port.EventReportBatchIssued, port.EventReportTaskReminded,
			port.EventSubmissionAdjustmentRequested, port.EventPeriodReopened}},
		{Name: notifyport.ConsumerWorkSchedule, Order: 10, Events: []string{port.EventWorkScheduleSubmitted, port.EventWorkScheduleReviewed,
			port.EventWorkScheduleReminded}},
		{Name: notifyport.ConsumerCNTS, Order: 13, Events: []string{port.EventCNTSAlertChanged, port.EventCNTSFormReminded, port.EventCNTSDutyChanged}},
	}}
}
