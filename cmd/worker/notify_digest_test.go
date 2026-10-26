package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNotifyWorkerAlwaysBindsKNDigestRecipients(t *testing.T) {
	tid := uuid.MustParse("01990000-0000-7000-8000-000000000001")
	ch := notify.Channels{DigestTo: []string{"global@example.vn"}}
	t.Setenv("AG_KN_DIGEST_RECIPIENTS_JSON", "")
	d, err := notifyDeliveryDeps(ch)
	if err != nil || d.DigestRecipients == nil {
		t.Fatal("worker omitted authoritative KN resolver")
	}
	if got, err := d.DigestRecipients(context.Background(), tid); len(got) != 0 || !errors.Is(err, extension.ErrDigestRecipientsMissing) {
		t.Fatal("worker used global recipient fallback")
	}
	t.Setenv("AG_KN_DIGEST_RECIPIENTS_JSON", fmt.Sprintf(`[{"tenant_id":%q,"to":["center@example.vn"]}]`, tid))
	d, err = notifyDeliveryDeps(ch)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AG_KN_DIGEST_RECIPIENTS_JSON", "")
	got, err := d.DigestRecipients(context.Background(), tid)
	if err != nil || !slices.Equal(got, []string{"center@example.vn"}) {
		t.Fatal("worker mapping is not a frozen startup snapshot")
	}
}

func TestNotifyWorkerRejectsBadDigestMappingBeforeOpeningPool(t *testing.T) {
	t.Setenv("AG_KN_DIGEST_RECIPIENTS_JSON", `[{"tenant_id":"bad","to":["secret@example.vn"]}]`)
	t.Setenv("AG_SMTP_HOST", "")
	t.Setenv("AG_FCM_CREDENTIALS_FILE", "")
	t.Setenv("AG_FCM_PROJECT_ID", "")
	kept := false
	_, err := buildNotify(context.Background(), slog.New(slog.DiscardHandler), "invalid-db-address", func(*pgxpool.Pool) { kept = true })
	if !errors.Is(err, extension.ErrDigestRecipientsConfig) || kept {
		t.Fatal("worker did not validate configuration before opening pool")
	}
}
