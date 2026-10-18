package notify

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestPlannerAndDigestConstructorOwnership(t *testing.T) {
	planner := func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) (port.NotificationPlan, error) {
		return port.NotificationPlan{}, nil
	}
	for _, event := range []string{"fixture.absent", "pf.accounts.user.dormant_warning", "pf.notify.push.requested", "pf.notify.email.requested"} {
		t.Run(event, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("native or unsubscribed planner accepted")
				}
			}()
			New(nil, clock.System{}, Deps{Builders: []port.EventBuilderRegistration{{Events: []string{event}, Plan: planner}}})
		})
	}
	for _, event := range []string{"fixture.absent", "pf.accounts.user.dormant_warning"} {
		t.Run("digest/"+event, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("native or unsubscribed digest source accepted")
				}
			}()
			New(nil, clock.System{}, Deps{Digests: []port.DigestRegistration{{Events: []string{event}, Job: port.DigestJobDefinition{Name: "fixture.digest", Every: time.Hour, Timeout: time.Minute}, Format: func([]port.DigestDraft) (string, string) { return "s", "b" }}}})
		})
	}
	reg := sampleConsumerRegistration()
	reg.Definitions[0].Order = 1
	module := New(nil, clock.System{}, Deps{Consumers: []port.ConsumerRegistration{reg}, Builders: []port.EventBuilderRegistration{{Events: []string{"fixture.owned.event"}, Plan: planner}}, Digests: []port.DigestRegistration{{Events: []string{"fixture.owned.event"}, Job: port.DigestJobDefinition{Name: "fixture.digest", Every: time.Hour, Timeout: time.Minute}, Format: func([]port.DigestDraft) (string, string) { return "s", "b" }}}})
	jobs := module.Jobs(nil, nil)
	if len(jobs) != 1 || jobs[0].Name != "fixture.digest" || jobs[0].Timeout != time.Minute {
		t.Fatal("registered job metadata not used")
	}
}
