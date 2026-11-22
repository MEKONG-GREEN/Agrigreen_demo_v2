package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// NotificationConsumers composes owned source subscriptions for API and worker startup.
func NotificationConsumers() []notifyport.ConsumerRegistration {
	return []notifyport.ConsumerRegistration{extension.NotificationConsumers(), price.NotificationConsumers(), hrm.NotificationConsumers(),
		extension.KNQGNotificationConsumers()} // W11.53b: pf.notify.cnts_knqg (thứ tự 18)
}

// NotificationRenderers supplies source-owned pure builders to every API/worker constructor.
func NotificationRenderers() []notifyport.EventRendererRegistration {
	return append(extension.NotificationRenderers(), extension.KNQGNotificationRenderers()...)
}

// NotificationBuilders supplies source-owned builders with explicit read-only recipient lookup. hrmDir: người nhận HCTH
// (NOTIFY-HCTH #48) — nil chỉ dùng để liệt kê đăng ký (builder HRM lỗi đóng an toàn nếu được gọi).
func NotificationBuilders(directory priceport.PriceDesk, staff notifyport.StaffDirectory, units partyport.AdminUnits, hrmDir hrmport.NotifyDirectory) []notifyport.EventBuilderRegistration {
	builders := append(price.NotificationBuilders(directory), extension.TaskNotificationBuilders(staff)...)
	builders = append(builders, extension.DirectiveNotificationBuilders(staff, units)...)
	return append(builders, hrm.NotificationBuilders(KNHRMAccess, hrmDir)...)
}

// NotificationDigests supplies owned formatter and job metadata.
func NotificationDigests() []notifyport.DigestRegistration {
	return []notifyport.DigestRegistration{extension.NotificationDigest()}
}

// NotificationKinds composes source-owned notification keys.
func NotificationKinds() []notifyport.KindRegistration {
	return []notifyport.KindRegistration{extension.NotificationKinds(), price.NotificationKinds(), hrm.NotificationKinds(), extension.KNQGNotificationKinds()}
}
