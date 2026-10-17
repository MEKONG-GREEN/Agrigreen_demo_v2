package extension

import (
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/google/uuid"
	"time"
	"unicode/utf8"
)

// NotificationRenderers assembles all pure source-content builders owned by this module.
func NotificationRenderers() []notifyport.EventRendererRegistration {
	return append(CNTSNotificationRenderers(), WorkflowNotificationRenderers()...)
}

type sourceNotificationTemplate struct {
	Kind, Severity, Title, Body, DeepLink string
	SubjectRef                            *uuid.UUID
	CreatedAt                             time.Time
}

func (t sourceNotificationTemplate) For(recipient uuid.UUID) notifyport.NotificationDraft {
	r := recipient
	return t.base(&r, notifyport.DraftAudience{})
}
func (t sourceNotificationTemplate) ForAudience(a notifyport.DraftAudience) notifyport.NotificationDraft {
	return t.base(nil, a)
}
func (t sourceNotificationTemplate) base(r *uuid.UUID, a notifyport.DraftAudience) notifyport.NotificationDraft {
	return notifyport.NotificationDraft{RecipientRef: r, Audience: a, Kind: t.Kind, Severity: t.Severity,
		Title: t.Title, Body: t.Body, DeepLink: t.DeepLink, SubjectRef: t.SubjectRef, CreatedAt: t.CreatedAt.UTC()}
}
func sourceNotificationTime(s string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return fallback.UTC()
}
func sourceNotificationVietnamTime(t time.Time) string {
	return t.In(clock.Zone()).Format("02/01/2006 15:04")
}
func sourceNotificationRef(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}
func sourceNotificationRecipients(refs []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if r == uuid.Nil || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}
func sourceNotificationTruncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}
