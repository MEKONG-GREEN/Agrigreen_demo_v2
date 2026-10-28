package channel

// Kênh TẮT (P0-NOTIFY-FAILCLOSED, #127): kênh email/đẩy chưa cấu hình không được "giả gửi" rồi báo thành công. Thay vì
// LogMailer/LogPusher mặc định, ChannelsFromEnv lắp DisabledMailer/DisabledPusher: Send luôn trả ErrChannelDisabled
// kèm lý do, consumer ghi bản gửi là "dropped" với mã "channel_disabled:<lý do>" (không thử lại, không bao giờ "sent").
// LogMailer/LogPusher chỉ còn khi đặt rõ AG_MAILER=log / AG_PUSHER=log trên máy phát triển.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
)

// ErrChannelDisabled: kênh chưa cấu hình (hoặc tắt có chủ đích) — không gửi, không thử lại.
var ErrChannelDisabled = errors.New("channel: kênh gửi chưa cấu hình")

// Lý do kênh tắt (ghi sau "channel_disabled:" ở last_error, trả ở GET /v1/notify/delivery-health).
const (
	ReasonSMTPNotConfigured = "smtp_not_configured" // thiếu AG_SMTP_HOST
	ReasonFCMNotConfigured  = "fcm_not_configured"  // thiếu AG_FCM_PROJECT_ID/AG_FCM_CREDENTIALS_FILE
	ReasonLinkBaseMissing   = "link_base_missing"   // AG_WEB_OPS_URL thiếu/localhost ngoài máy phát triển
	ReasonDisabledByConfig  = "disabled_by_config"  // AG_MAILER=disabled / AG_PUSHER=disabled
)

// Chế độ kênh (Status.Mode).
const (
	ModeSMTP     = "smtp"
	ModeFCM      = "fcm"
	ModeLog      = "log"
	ModeDisabled = "disabled"
)

// Biến môi trường chọn chế độ kênh.
const (
	EnvMailer = "AG_MAILER" // ""(tự chọn: smtp khi có AG_SMTP_HOST, không thì tắt) | smtp | log (chỉ máy phát triển) | disabled
	EnvPusher = "AG_PUSHER" // ""(tự chọn: fcm khi có AG_FCM_*, không thì tắt) | fcm | log (chỉ máy phát triển) | disabled
)

// Status mô tả một kênh đã dựng (log khởi động, kiểm thử).
type Status struct {
	Mode   string // smtp|fcm|log|disabled
	Reason string // lý do khi Mode=disabled
}

// Disabled cho biết kênh đang tắt.
func (s Status) Disabled() bool { return s.Mode == ModeDisabled }

// disabler là kênh tắt: trả lý do.
type disabler interface{ DisabledReason() string }

// DisabledReason trả lý do khi c là kênh tắt (DisabledMailer/DisabledPusher).
func DisabledReason(c any) (string, bool) {
	d, ok := c.(disabler)
	if !ok {
		return "", false
	}
	return d.DisabledReason(), true
}

// CodeChannelDisabled là tiền tố mã ghi ở last_error của bản gửi bị bỏ vì kênh tắt.
const CodeChannelDisabled = "channel_disabled"

// DisabledCode là mã ghi ở last_error của bản gửi bị bỏ vì kênh tắt: "channel_disabled:<lý do>".
func DisabledCode(reason string) string { return CodeChannelDisabled + ":" + reason }

// DisabledMailer là kênh email tắt: Send luôn trả ErrChannelDisabled.
type DisabledMailer struct{ Reason string }

// DisabledReason thực hiện disabler.
func (m DisabledMailer) DisabledReason() string { return m.Reason }

// Send thực hiện Mailer — không gửi.
func (m DisabledMailer) Send(context.Context, []string, string, string) error {
	return fmt.Errorf("%w: %s", ErrChannelDisabled, m.Reason)
}

// DisabledPusher là kênh đẩy tắt: Send luôn trả ErrChannelDisabled.
type DisabledPusher struct{ Reason string }

// Name thực hiện Pusher.
func (DisabledPusher) Name() string { return ModeDisabled }

// DisabledReason thực hiện disabler.
func (p DisabledPusher) DisabledReason() string { return p.Reason }

// Send thực hiện Pusher — không gửi.
func (p DisabledPusher) Send(context.Context, domain.PushMessage) error {
	return fmt.Errorf("%w: %s", ErrChannelDisabled, p.Reason)
}

func mode(getenv func(string) string, key string) string {
	return strings.ToLower(strings.TrimSpace(getenv(key)))
}

// logModeAllowed: chế độ "log" (giả gửi) chỉ hợp lệ trên máy phát triển — nơi khác từ chối khởi động.
func logModeAllowed(getenv func(string) string, key string) error {
	if !residency.LocalDev(getenv) {
		return fmt.Errorf("channel: %s=log chỉ dùng trên máy phát triển (AG_ENV=development|dev|local|test)", key)
	}
	return nil
}

// MailerFromEnv chọn kênh email theo AG_MAILER và AG_SMTP_* (getenv nil → os.Getenv):
//
//	""        AG_SMTP_HOST có → SMTP; không → DisabledMailer{smtp_not_configured}
//	smtp      bắt buộc AG_SMTP_HOST (thiếu → lỗi khởi động)
//	log       LogMailer — chỉ máy phát triển (nơi khác → lỗi khởi động)
//	disabled  DisabledMailer{disabled_by_config}
//
// Không bao giờ trả nil.
func MailerFromEnv(getenv func(string) string, log *slog.Logger) (Mailer, Status, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch m := mode(getenv, EnvMailer); m {
	case ModeDisabled:
		return DisabledMailer{Reason: ReasonDisabledByConfig}, Status{Mode: ModeDisabled, Reason: ReasonDisabledByConfig}, nil
	case ModeLog:
		if err := logModeAllowed(getenv, EnvMailer); err != nil {
			return nil, Status{}, err
		}
		return LogMailer{Log: log}, Status{Mode: ModeLog}, nil
	case "", ModeSMTP:
		s, err := smtpFrom(getenv)
		if err != nil {
			return nil, Status{}, err
		}
		if s != nil {
			return s, Status{Mode: ModeSMTP}, nil
		}
		if m == ModeSMTP {
			return nil, Status{}, errors.New("channel: AG_MAILER=smtp nhưng chưa đặt AG_SMTP_HOST")
		}
		return DisabledMailer{Reason: ReasonSMTPNotConfigured}, Status{Mode: ModeDisabled, Reason: ReasonSMTPNotConfigured}, nil
	default:
		return nil, Status{}, fmt.Errorf("channel: %s=%q — chỉ nhận smtp, log, disabled", EnvMailer, m)
	}
}

// PusherFromEnvWith chọn kênh đẩy theo AG_PUSHER và AG_FCM_* (getenv nil → os.Getenv):
//
//	""        AG_FCM_PROJECT_ID/AG_FCM_CREDENTIALS_FILE có → FCM HTTP v1; không → DisabledPusher{fcm_not_configured}
//	fcm       bắt buộc cấu hình FCM (thiếu/sai → lỗi khởi động)
//	log       LogPusher — chỉ máy phát triển (nơi khác → lỗi khởi động)
//	disabled  DisabledPusher{disabled_by_config}
func PusherFromEnvWith(getenv func(string) string, log *slog.Logger) (Pusher, Status, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch m := mode(getenv, EnvPusher); m {
	case ModeDisabled:
		return DisabledPusher{Reason: ReasonDisabledByConfig}, Status{Mode: ModeDisabled, Reason: ReasonDisabledByConfig}, nil
	case ModeLog:
		if err := logModeAllowed(getenv, EnvPusher); err != nil {
			return nil, Status{}, err
		}
		return LogPusher{Log: log}, Status{Mode: ModeLog}, nil
	case "", ModeFCM:
		if m == "" && getenv("AG_FCM_PROJECT_ID") == "" && getenv("AG_FCM_CREDENTIALS_FILE") == "" {
			return DisabledPusher{Reason: ReasonFCMNotConfigured}, Status{Mode: ModeDisabled, Reason: ReasonFCMNotConfigured}, nil
		}
		f, err := FCMFromEnv()
		if err != nil {
			return nil, Status{}, err
		}
		return f, Status{Mode: ModeFCM}, nil
	default:
		return nil, Status{}, fmt.Errorf("channel: %s=%q — chỉ nhận fcm, log, disabled", EnvPusher, m)
	}
}
