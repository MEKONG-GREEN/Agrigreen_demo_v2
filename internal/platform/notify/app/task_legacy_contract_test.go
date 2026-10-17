package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

type taskLegacyLookupSeed struct {
	Nil     bool
	Records []port.StaffRef
	Error   bool
}
type taskLegacyDirectory struct {
	seed  taskLegacyLookupSeed
	calls []string
}

func (d *taskLegacyDirectory) ActiveStaff(_ context.Context, tid uuid.UUID, units []string) ([]port.StaffRef, error) {
	d.calls = append(d.calls, "staff|"+tid.String()+"|"+strings.Join(units, ","))
	if d.seed.Error {
		return nil, errors.New("fixture staff failure")
	}
	return d.seed.Records, nil
}
func TestTaskLegacyBuilderVectors(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
		TenantID             uuid.UUID
		Lookup               taskLegacyLookupSeed
	}
	raw, err := os.ReadFile("../../../../tests/golden/kn-notify-task-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 83 {
		t.Fatal("missing task inputs")
	}
	for _, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			directory := &taskLegacyDirectory{seed: in.Lookup}
			var lookup port.StaffDirectory = directory
			if in.Lookup.Nil {
				lookup = nil
			}
			svc := NewService(nil, clock.System{}, Deps{Staff: lookup})
			ns, err := svc.legacyTaskNotifications(context.Background(), outbox.Envelope{TenantID: in.TenantID, EventType: in.Event, Payload: json.RawMessage(in.Payload), OccurredAt: in.At})
			var drafts []port.NotificationDraft
			if ns != nil {
				drafts = make([]port.NotificationDraft, len(ns))
			}
			for i, n := range ns {
				drafts[i] = port.NotificationDraft{RecipientRef: n.RecipientRef, Audience: port.DraftAudience{National: n.Audience.National, Units: n.Audience.Units, OrgRef: n.Audience.OrgRef, Roles: n.Audience.Roles}, Kind: n.Kind, Severity: n.Severity, Title: n.Title, Body: n.Body, DeepLink: n.DeepLink, SubjectRef: n.SubjectRef, CreatedAt: n.CreatedAt, Email: n.Email}
			}
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			data, err := json.Marshal(struct {
				Name   string
				Drafts []port.NotificationDraft
				Error  string
				Calls  []string
			}{in.Name, drafts, errText, directory.calls})
			if err != nil {
				t.Fatal(err)
			}
			t.Log("OLD_TASK=" + string(data))
		})
	}
}
