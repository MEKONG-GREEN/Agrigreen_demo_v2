package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
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
