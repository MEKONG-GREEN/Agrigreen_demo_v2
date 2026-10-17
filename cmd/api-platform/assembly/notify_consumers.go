package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// NotificationConsumers composes owned source subscriptions for API and worker startup.
func NotificationConsumers() []notifyport.ConsumerRegistration {
	return []notifyport.ConsumerRegistration{extension.NotificationConsumers(), price.NotificationConsumers()}
}

// NotificationRenderers supplies source-owned pure builders to every API/worker constructor.
func NotificationRenderers() []notifyport.EventRendererRegistration {
	return extension.NotificationRenderers()
}

// NotificationBuilders supplies source-owned builders with explicit read-only recipient lookup.
func NotificationBuilders(directory priceport.PriceDesk, staff notifyport.StaffDirectory) []notifyport.EventBuilderRegistration {
	return append(price.NotificationBuilders(directory), extension.TaskNotificationBuilders(staff)...)
}
