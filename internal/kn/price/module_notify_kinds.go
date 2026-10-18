package price

import (
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// NotificationKinds declares source-owned persistence keys without capability aliases.
func NotificationKinds() notifyport.KindRegistration {
	return notifyport.KindRegistration{Kinds: []string{
		priceport.KindPriceAlert,
		priceport.KindPriceBoardApproved,
		priceport.KindPriceBoardReopened,
		priceport.KindPriceLockSummary,
		priceport.KindPriceProxyEntry,
		priceport.KindPriceReminder,
		priceport.KindPriceVerifyDecision,
		priceport.KindPriceVerifyReply,
		priceport.KindPriceVerifyRequest,
	}}
}
