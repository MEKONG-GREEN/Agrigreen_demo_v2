package port

// Owned notification queue identities; literals stay compatible with existing inbox records.
const (
	NotifyConsumerDirective    = "pf.notify.directive"
	NotifyConsumerTask         = "pf.notify.task"
	NotifyConsumerReportCycle  = "pf.notify.report_cycle"
	NotifyConsumerWorkSchedule = "pf.notify.work_schedule"
	NotifyConsumerCNTS         = "pf.notify.cnts"
)
