package notify

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

// PushMessage is the public message contract for the injectable Channels/Deps pusher.
type PushMessage = domain.PushMessage

// ErrTokenInvalid tells a channel adapter to retire a token; other failures remain retryable.
var ErrTokenInvalid = channel.ErrTokenInvalid

// ErrEmailRetry identifies a retryable batch result returned by EmailHandler.
var ErrEmailRetry = app.ErrEmailRetry

// BuildFCMRequest encodes the same data-only payload used by the built-in FCM channel.
func BuildFCMRequest(m PushMessage) ([]byte, error) { return channel.BuildFCMRequest(m) }
