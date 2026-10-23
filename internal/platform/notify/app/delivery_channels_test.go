package app

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestDraftChannelsCopiedAndInvalidPlansAtomic(t *testing.T) {
	ref := uuid.New()
	channels := &port.DeliveryChannels{Email: true}
	draft := port.NotificationDraft{RecipientRef: &ref, Channels: channels, Email: false}
	s := &Service{}
	s.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.delivery"}, Render: func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
		return []port.NotificationDraft{draft}, nil
	}}}, []string{"fixture.delivery"}, nil)
	ns, ok, err := s.eventRenderers.render("fixture.delivery", nil, time.Time{})
	if err != nil || !ok || len(ns) != 1 || !ns[0].SuppressInApp || !ns[0].SuppressPush || !ns[0].Email || !ns[0].RequireEmail {
		t.Fatal("explicit email choice lost", err)
	}
	*channels = port.DeliveryChannels{InApp: true, Push: true}
	if !ns[0].SuppressInApp || !ns[0].SuppressPush || !ns[0].Email {
		t.Fatal("channel selection aliases owner")
	}
	for _, bad := range []port.NotificationDraft{{Channels: &port.DeliveryChannels{}}, {Channels: &port.DeliveryChannels{Email: true}}, {RecipientRef: new(uuid.UUID), Channels: &port.DeliveryChannels{Email: true}}} {
		s2 := &Service{}
		s2.RegisterEventBuilders([]port.EventBuilderRegistration{{Events: []string{"fixture.delivery"}, Plan: func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) (port.NotificationPlan, error) {
			return port.NotificationPlan{Drafts: []port.NotificationDraft{{RecipientRef: &ref}, bad}, Digests: []port.DigestDraft{{Reference: ref}}}, nil
		}}}, []string{"fixture.delivery"}, nil)
		ns, digests, ok, err := s2.eventBuilders.build(context.Background(), uuid.New(), "fixture.delivery", nil, time.Time{})
		if err == nil || !ok || ns != nil || digests != nil {
			t.Fatal("invalid plan leaked sibling/digest")
		}
	}
}
