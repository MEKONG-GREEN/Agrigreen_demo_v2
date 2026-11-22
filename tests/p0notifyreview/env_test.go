package p0notifyreview_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

func TestReviewerPartialNonDemo127(t *testing.T) {
	const privateProbe = "private-probe-never-in-an-error"
	for _, key := range []string{"AG_FCM_PROJECT_ID", "AG_FCM_CREDENTIALS_FILE", "AG_FCM_ENDPOINT", "AG_FCM_TOKEN_URL"} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct{ name, key, value string }{
		{"empty", "", ""},
		{"smtp-host-only", "AG_SMTP_HOST", "smtp.example.test"},
		{"smtp-from-only", "AG_SMTP_FROM", "synthetic@example.test"},
		{"smtp-password-only", "AG_SMTP_PASSWORD", privateProbe},
		{"fcm-project-only", "AG_FCM_PROJECT_ID", "synthetic-project"},
		{"fcm-missing-file", "AG_FCM_CREDENTIALS_FILE", "does-not-exist-127.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"AG_ENV": "production", "AG_DEPLOY_ENV": "production"}
			env[tc.key] = tc.value
			get := func(k string) string { return env[k] }
			mailer, _, err := channel.MailerFromEnv(get, nil)
			if err == nil {
				if mailer == nil {
					t.Fatal("missing SMTP silently returned a nil delivery channel")
				}
				if _, disabled := channel.DisabledReason(mailer); !disabled {
					t.Fatalf("partial SMTP accepted as %T", mailer)
				}
				if !errors.Is(mailer.Send(context.Background(), []string{"synthetic@example.test"}, "probe", "probe"), channel.ErrChannelDisabled) {
					t.Fatal("missing SMTP did not fail closed")
				}
			} else if strings.Contains(err.Error(), privateProbe) {
				t.Fatal("SMTP validation exposed a secret")
			}
			pusher, _, err := channel.PusherFromEnvWith(get, nil)
			if err == nil {
				if pusher == nil {
					t.Fatal("missing FCM silently returned nil")
				}
				if _, disabled := channel.DisabledReason(pusher); !disabled {
					t.Fatalf("partial FCM accepted as %T", pusher)
				}
				if !errors.Is(pusher.Send(context.Background(), domain.PushMessage{Kind: "probe"}), channel.ErrChannelDisabled) {
					t.Fatal("missing FCM did not fail closed")
				}
			} else if strings.Contains(err.Error(), privateProbe) {
				t.Fatal("FCM validation exposed a secret")
			}
			reportMailer, err := reports.SMTPFromEnvWith(get, clock.System{})
			if err == nil && reportMailer != nil {
				t.Fatalf("reports accepted partial SMTP as %T", reportMailer)
			}
			if err != nil && strings.Contains(err.Error(), privateProbe) {
				t.Fatal("reports validation exposed a secret")
			}
		})
	}
}
