package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

type directiveLegacyLookup struct {
	NilStaff, NilUnits, StaffError bool
	Staff                          []port.StaffRef
	Children                       map[string][]string
	NotFound                       []string
	UnitsError                     string
}
type directiveLegacyDirectory struct {
	seed  directiveLegacyLookup
	calls []string
}

func (d *directiveLegacyDirectory) ActiveStaff(_ context.Context, tid uuid.UUID, units []string) ([]port.StaffRef, error) {
	d.calls = append(d.calls, "staff|"+tid.String()+"|"+strings.Join(units, ","))
	if d.seed.StaffError {
		return nil, errors.New("fixture staff failure")
	}
	return d.seed.Staff, nil
}
func (d *directiveLegacyDirectory) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	d.calls = append(d.calls, "unexpected-get|"+code)
	return partyport.AdminUnit{}, errors.New("unexpected Get")
}
func (d *directiveLegacyDirectory) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	d.calls = append(d.calls, "unexpected-current|"+code)
	return nil, errors.New("unexpected Current")
}
func (d *directiveLegacyDirectory) Descendants(_ context.Context, code string, at time.Time) ([]string, error) {
	d.calls = append(d.calls, "desc|"+code+"|"+at.Format(time.RFC3339Nano))
	if d.seed.UnitsError == code {
		return nil, errors.New("fixture units failure")
	}
	if slices.Contains(d.seed.NotFound, code) {
		return nil, partyport.ErrUnitNotFound
	}
	return d.seed.Children[code], nil
}

type legacyDigestProjection struct {
	Reference     uuid.UUID
	AudienceUnits []string
	IssuedAt      time.Time
}

func TestDirectiveLegacyPlanVectors(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
		TenantID             uuid.UUID
		Lookup               directiveLegacyLookup
	}
	raw, err := os.ReadFile("../../../../tests/golden/kn-notify-directive-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 52 {
		t.Fatal("missing directive inputs")
	}
	for _, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			directory := &directiveLegacyDirectory{seed: in.Lookup}
			var staff port.StaffDirectory = directory
			var units partyport.AdminUnits = directory
			if in.Lookup.NilStaff {
				staff = nil
			}
			if in.Lookup.NilUnits {
				units = nil
			}
			svc := NewService(nil, clock.System{}, Deps{Staff: staff, Units: units})
			plan, err := svc.legacyDirectivePlan(context.Background(), outbox.Envelope{TenantID: in.TenantID, EventType: in.Event, Payload: json.RawMessage(in.Payload), OccurredAt: in.At})
			var drafts []port.NotificationDraft
			if plan.Notifications != nil {
				drafts = make([]port.NotificationDraft, len(plan.Notifications))
			}
			for i, n := range plan.Notifications {
				drafts[i] = port.NotificationDraft{RecipientRef: n.RecipientRef, Audience: port.DraftAudience{National: n.Audience.National, Units: n.Audience.Units, OrgRef: n.Audience.OrgRef, Roles: n.Audience.Roles}, Kind: n.Kind, Severity: n.Severity, Title: n.Title, Body: n.Body, DeepLink: n.DeepLink, SubjectRef: n.SubjectRef, CreatedAt: n.CreatedAt, Email: n.Email}
			}
			var digests []legacyDigestProjection
			if d := plan.Digest; d != nil {
				digests = []legacyDigestProjection{{d.DirectiveID, d.AudienceUnits, d.IssuedAt}}
			}
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			data, err := json.Marshal(struct {
				Name    string
				Drafts  []port.NotificationDraft
				Digests []legacyDigestProjection
				Error   string
				Calls   []string
			}{in.Name, drafts, digests, errText, directory.calls})
			if err != nil {
				t.Fatal(err)
			}
			t.Log("OLD_DIRECTIVE=" + string(data))
		})
	}
}
func TestDigestLegacyFormatterVectors(t *testing.T) {
	var inputs []struct {
		Name  string
		Items []legacyDigestProjection
	}
	raw, err := os.ReadFile("../../../../tests/golden/kn-notify-digest-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 11 {
		t.Fatal("missing digest inputs")
	}
	for _, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			var items []domain.DigestItem
			if in.Items != nil {
				items = make([]domain.DigestItem, len(in.Items))
			}
			for i, it := range in.Items {
				items[i] = domain.DigestItem{DirectiveID: it.Reference, AudienceUnits: it.AudienceUnits, IssuedAt: it.IssuedAt}
			}
			subject, body := DigestMessage(items)
			data, err := json.Marshal(struct{ Name, Subject, Body string }{in.Name, subject, body})
			if err != nil {
				t.Fatal(err)
			}
			t.Log("OLD_DIGEST=" + string(data))
		})
	}
}
