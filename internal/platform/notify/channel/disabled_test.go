package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// P0-NOTIFY-FAILCLOSED (#127): thiếu AG_SMTP_* / AG_FCM_* ở MỌI môi trường → kênh TẮT (không giả gửi); log chỉ khi đặt rõ
// AG_MAILER/AG_PUSHER=log trên máy phát triển, nơi khác là lỗi khởi động.
func TestMailerFromEnvPerDeployEnv(t *testing.T) {
	smtp := map[string]string{"AG_SMTP_HOST": "smtp.agrigreen.vn", "AG_SMTP_FROM": "no-reply@agrigreen.vn"}
	with := func(base map[string]string, kv ...string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}
	cases := []struct {
		name   string
		env    map[string]string
		mode   string
		reason string
		fail   bool
	}{
		{name: "production thiếu SMTP", env: with(nil, "AG_ENV", "production"), mode: ModeDisabled, reason: ReasonSMTPNotConfigured},
		{name: "demo thiếu SMTP", env: with(nil, "AG_ENV", "staging", "AG_DEPLOY_ENV", "demo"), mode: ModeDisabled, reason: ReasonSMTPNotConfigured},
		{name: "local thiếu SMTP vẫn tắt", env: with(nil, "AG_ENV", "local"), mode: ModeDisabled, reason: ReasonSMTPNotConfigured},
		{name: "AG_ENV trống thiếu SMTP", env: with(nil), mode: ModeDisabled, reason: ReasonSMTPNotConfigured},
		{name: "local log tường minh", env: with(nil, "AG_ENV", "local", EnvMailer, "log"), mode: ModeLog},
		{name: "test log tường minh", env: with(nil, "AG_ENV", "test", EnvMailer, "LOG"), mode: ModeLog},
		{name: "demo log bị từ chối", env: with(nil, "AG_ENV", "staging", "AG_DEPLOY_ENV", "demo", EnvMailer, "log"), fail: true},
		{name: "production log bị từ chối", env: with(nil, "AG_ENV", "production", EnvMailer, "log"), fail: true},
		{name: "smtp tường minh thiếu host", env: with(nil, "AG_ENV", "production", EnvMailer, "smtp"), fail: true},
		{name: "giá trị lạ", env: with(nil, "AG_ENV", "local", EnvMailer, "mailhog"), fail: true},
		{name: "tắt có chủ đích", env: with(smtp, "AG_ENV", "production", EnvMailer, "disabled"), mode: ModeDisabled, reason: ReasonDisabledByConfig},
		{name: "production có SMTP", env: with(smtp, "AG_ENV", "production"), mode: ModeSMTP},
		{name: "smtp tường minh có host", env: with(smtp, "AG_ENV", "staging", EnvMailer, "smtp"), mode: ModeSMTP},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, st, err := MailerFromEnv(envOf(c.env), nil)
			if c.fail {
				if err == nil {
					t.Fatalf("muốn lỗi khởi động, được %T %+v", m, st)
				}
				return
			}
			if err != nil || m == nil || st.Mode != c.mode || st.Reason != c.reason {
				t.Fatalf("được %T %+v %v, muốn mode=%s reason=%s", m, st, err, c.mode, c.reason)
			}
			reason, off := DisabledReason(m)
			if off != (c.mode == ModeDisabled) || reason != c.reason {
				t.Fatalf("DisabledReason=%q,%v", reason, off)
			}
			if off {
				if err := m.Send(context.Background(), []string{"a@b.vn"}, "s", "b"); !errors.Is(err, ErrChannelDisabled) {
					t.Fatalf("kênh tắt phải trả ErrChannelDisabled, được %v", err)
				}
			}
		})
	}
}

func TestPusherFromEnvPerDeployEnv(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		mode   string
		reason string
		fail   bool
	}{
		{name: "production thiếu FCM", env: map[string]string{"AG_ENV": "production"}, mode: ModeDisabled, reason: ReasonFCMNotConfigured},
		{name: "demo thiếu FCM", env: map[string]string{"AG_ENV": "staging", "AG_DEPLOY_ENV": "demo"}, mode: ModeDisabled, reason: ReasonFCMNotConfigured},
		{name: "local thiếu FCM vẫn tắt", env: map[string]string{"AG_ENV": "local"}, mode: ModeDisabled, reason: ReasonFCMNotConfigured},
		{name: "local log tường minh", env: map[string]string{"AG_ENV": "development", EnvPusher: "log"}, mode: ModeLog},
		{name: "staging log bị từ chối", env: map[string]string{"AG_ENV": "staging", EnvPusher: "log"}, fail: true},
		{name: "fcm tường minh thiếu cấu hình", env: map[string]string{"AG_ENV": "production", EnvPusher: "fcm"}, fail: true},
		{name: "tắt có chủ đích", env: map[string]string{"AG_ENV": "production", EnvPusher: "disabled"}, mode: ModeDisabled, reason: ReasonDisabledByConfig},
		{name: "giá trị lạ", env: map[string]string{"AG_ENV": "local", EnvPusher: "apns"}, fail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, st, err := PusherFromEnvWith(envOf(c.env), nil)
			if c.fail {
				if err == nil {
					t.Fatalf("muốn lỗi khởi động, được %T %+v", p, st)
				}
				return
			}
			if err != nil || p == nil || st.Mode != c.mode || st.Reason != c.reason {
				t.Fatalf("được %T %+v %v", p, st, err)
			}
			if _, off := DisabledReason(p); off {
				if p.Name() != ModeDisabled {
					t.Fatalf("Name=%q", p.Name())
				}
				if err := p.Send(context.Background(), domain.PushMessage{Kind: "x"}); !errors.Is(err, ErrChannelDisabled) {
					t.Fatalf("kênh tắt phải trả ErrChannelDisabled, được %v", err)
				}
			}
		})
	}
}

func TestDisabledCode(t *testing.T) {
	if got := DisabledCode(ReasonSMTPNotConfigured); got != "channel_disabled:smtp_not_configured" || len(got) > 200 {
		t.Fatal(got)
	}
	if _, off := DisabledReason(LogMailer{}); off {
		t.Fatal("LogMailer không phải kênh tắt")
	}
}
