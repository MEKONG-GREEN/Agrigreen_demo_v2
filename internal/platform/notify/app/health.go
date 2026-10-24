package app

// Tình trạng kênh gửi (P0-NOTIFY-FAILCLOSED, #127): quản trị thấy kênh email/đẩy có thật sự gửi hay đang TẮT (chưa cấu
// hình SMTP/FCM/URL liên kết) từ chính bản ghi lần gửi của tenant — api-platform không đọc cấu hình kênh của worker.

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// HealthWindowDays: cửa sổ đếm lần gửi.
const HealthWindowDays = 7

// Trạng thái kênh của ChannelHealth.Status.
const (
	HealthOK       = "ok"       // có lần gửi thành công, lần gần nhất không bị tắt
	HealthDisabled = "disabled" // lần bỏ vì kênh tắt mới hơn lần gửi thành công cuối — kênh chưa cấu hình
	HealthFailing  = "failing"  // có lỗi, chưa lần nào thành công trong cửa sổ
	HealthIdle     = "idle"     // không có lần gửi nào trong cửa sổ
)

// ChannelHealth là tình trạng một kênh (email/đẩy).
type ChannelHealth struct {
	Status         string     `json:"status"`
	Sent           int        `json:"sent"`
	Failed         int        `json:"failed"`
	Dropped        int        `json:"dropped"`
	Pending        int        `json:"pending"`
	Disabled       int        `json:"disabled"`
	DisabledReason *string    `json:"disabled_reason"`
	LastDisabledAt *time.Time `json:"last_disabled_at"`
	LastSentAt     *time.Time `json:"last_sent_at"`
	Warning        *string    `json:"warning"`
}

// DeliveryHealth là tình trạng kênh gửi của tenant người gọi.
type DeliveryHealth struct {
	WindowDays int           `json:"window_days"`
	Email      ChannelHealth `json:"email"`
	Push       ChannelHealth `json:"push"`
}

// reasonLabel: nhãn tiếng Việt của lý do kênh tắt.
var reasonLabel = map[string]string{
	channel.ReasonSMTPNotConfigured: "chưa cấu hình máy chủ thư (AG_SMTP_*)",
	channel.ReasonLinkBaseMissing:   "chưa cấu hình URL web-ops cho liên kết trong email (AG_WEB_OPS_URL)",
	channel.ReasonFCMNotConfigured:  "chưa cấu hình dịch vụ đẩy FCM (AG_FCM_*)",
	channel.ReasonDisabledByConfig:  "kênh đã tắt bằng cấu hình (AG_MAILER/AG_PUSHER=disabled)",
}

// DeliveryHealth trả tình trạng kênh email/đẩy của tenant người gọi trong HealthWindowDays ngày.
func (s *Service) DeliveryHealth(ctx context.Context) (DeliveryHealth, error) {
	p, err := authz.Require(ctx, port.DeliveryHealthRoles...)
	if err != nil {
		return DeliveryHealth{}, err
	}
	since := s.now().AddDate(0, 0, -HealthWindowDays)
	var email, push store.ChannelCounts
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if email, e = store.DeliveryHealth(ctx, tx, store.TableEmailDelivery, since); e != nil {
			return e
		}
		push, e = store.DeliveryHealth(ctx, tx, store.TablePushDelivery, since)
		return e
	}); err != nil {
		return DeliveryHealth{}, err
	}
	return DeliveryHealth{WindowDays: HealthWindowDays,
		Email: channelHealth(email, "Email"), Push: channelHealth(push, "Thông báo đẩy")}, nil
}

func channelHealth(c store.ChannelCounts, name string) ChannelHealth {
	h := ChannelHealth{Sent: c.Sent, Failed: c.Failed, Dropped: c.Dropped, Pending: c.Pending, Disabled: c.Disabled,
		LastDisabledAt: c.LastDisabledAt, LastSentAt: c.LastSentAt}
	if c.LastDisabledCode != nil {
		r := strings.TrimPrefix(strings.TrimPrefix(*c.LastDisabledCode, channel.CodeChannelDisabled), ":")
		h.DisabledReason = &r
	}
	warn := func(w string) { h.Warning = &w }
	switch {
	case c.LastDisabledAt != nil && (c.LastSentAt == nil || c.LastDisabledAt.After(*c.LastSentAt)):
		h.Status = HealthDisabled
		label := "kênh chưa cấu hình"
		if h.DisabledReason != nil {
			if l, ok := reasonLabel[*h.DisabledReason]; ok {
				label = l
			}
		}
		warn(name + " KHÔNG được gửi: " + label + ". Thông báo chỉ hiển thị trong ứng dụng.")
	case c.Failed > 0 && c.Sent == 0:
		h.Status = HealthFailing
		warn(name + " đang gửi lỗi, chưa lần nào thành công trong 7 ngày qua — kiểm tra máy chủ gửi.")
	case c.Sent+c.Failed+c.Dropped+c.Pending == 0:
		h.Status = HealthIdle
	default:
		h.Status = HealthOK
	}
	return h
}
