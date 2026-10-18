package price

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// NotificationConsumers declares subscriptions without carrying a tenant or database callback.
func NotificationConsumers() notifyport.ConsumerRegistration {
	return notifyport.ConsumerRegistration{Definitions: []notifyport.ConsumerDefinition{
		{Name: port.NotifyConsumerPrice, Order: 12, Events: []string{
			port.EventReminderIssued, port.EventAlertRaised, port.EventVerificationRequested, port.EventVerificationResponded,
			port.EventVerificationDecided, port.EventLockSummarized, port.EventBoardPublished, port.EventBoardReopened, port.EventTicketProxied,
		}},
	}}
}
