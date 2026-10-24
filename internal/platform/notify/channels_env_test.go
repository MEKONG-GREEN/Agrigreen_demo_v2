package notify

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
)

// P0-NOTIFY-FAILCLOSED (#127): ChannelsFromEnvWith theo AG_ENV/AG_DEPLOY_ENV — không còn LogMailer/LogPusher mặc định;
// SMTP có nhưng AG_WEB_OPS_URL thiếu/localhost ngoài máy phát triển → kênh email theo người nhận tắt (link_base_missing).
func TestChannelsFromEnvFailClosed(t *testing.T) {
	smtp := map[string]string{"AG_SMTP_HOST": "smtp.agrigreen.vn", "AG_SMTP_FROM": "no-reply@agrigreen.vn"}
	merge := func(a map[string]string, kv ...string) func(string) string {
		m := map[string]string{}
		for k, v := range a {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name               string
		getenv             func(string) string
		email, emailReason string
		push               string
		digest             bool
		link               string
	}{
		{name: "VPS demo không SMTP/FCM", getenv: merge(nil, "AG_ENV", "staging", "AG_DEPLOY_ENV", "demo", EnvWebOpsURL, "https://app.demo.vn"),
			email: channel.ModeDisabled, emailReason: channel.ReasonSMTPNotConfigured, push: channel.ModeDisabled, link: "https://app.demo.vn"},
		{name: "production đủ cấu hình", getenv: merge(smtp, "AG_ENV", "production", EnvWebOpsURL, "https://ops.agrigreen.vn"),
			email: channel.ModeSMTP, push: channel.ModeDisabled, digest: true, link: "https://ops.agrigreen.vn"},
		{name: "production SMTP nhưng thiếu AG_WEB_OPS_URL", getenv: merge(smtp, "AG_ENV", "production"),
			email: channel.ModeDisabled, emailReason: channel.ReasonLinkBaseMissing, push: channel.ModeDisabled, digest: true},
		{name: "staging SMTP nhưng liên kết localhost", getenv: merge(smtp, "AG_ENV", "staging", EnvWebOpsURL, "http://localhost:3000"),
			email: channel.ModeDisabled, emailReason: channel.ReasonLinkBaseMissing, push: channel.ModeDisabled, digest: true},
		{name: "local log tường minh, liên kết tương đối", getenv: merge(nil, "AG_ENV", "local", channel.EnvMailer, "log", channel.EnvPusher, "log"),
			email: channel.ModeLog, push: channel.ModeLog},
		{name: "local SMTP liên kết localhost được phép", getenv: merge(smtp, "AG_ENV", "local", EnvWebOpsURL, "http://localhost:3000"),
			email: channel.ModeSMTP, push: channel.ModeDisabled, digest: true, link: "http://localhost:3000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch, err := ChannelsFromEnvWith(c.getenv, nil)
			if err != nil {
				t.Fatal(err)
			}
			if ch.Email.Mode != c.email || ch.Email.Reason != c.emailReason || ch.Push.Mode != c.push || (ch.Mailer != nil) != c.digest || ch.LinkBase != c.link {
				t.Fatalf("được email=%+v push=%+v digest=%v link=%q", ch.Email, ch.Push, ch.Mailer != nil, ch.LinkBase)
			}
			if ch.EmailMailer == nil || ch.Pusher == nil {
				t.Fatal("kênh không bao giờ nil")
			}
			if r, off := channel.DisabledReason(ch.EmailMailer); off != ch.Email.Disabled() || r != c.emailReason {
				t.Fatalf("EmailMailer tắt=%v lý do=%q", off, r)
			}
		})
	}
	if _, err := ChannelsFromEnvWith(merge(nil, "AG_ENV", "staging", "AG_DEPLOY_ENV", "demo", channel.EnvMailer, "log"), nil); err == nil {
		t.Fatal("AG_MAILER=log trên máy demo phải từ chối khởi động")
	}
}
