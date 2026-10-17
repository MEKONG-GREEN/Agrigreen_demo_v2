package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/google/uuid"
	"os"
	"testing"
	"time"
)

type legacyPriceLookup struct {
	Nil                bool
	Officers, Stewards []uuid.UUID
	Error              string
}
type legacyPriceDirectory struct {
	seed  legacyPriceLookup
	calls []string
}

func (d *legacyPriceDirectory) ProvinceOfficers(_ context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	d.calls = append(d.calls, "officers|"+tid.String()+"|"+profile)
	if d.seed.Error == "officers" {
		return nil, errors.New("fixture officers failure")
	}
	return d.seed.Officers, nil
}
func (d *legacyPriceDirectory) Stewards(_ context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	d.calls = append(d.calls, "stewards|"+tid.String()+"|"+profile)
	if d.seed.Error == "stewards" {
		return nil, errors.New("fixture stewards failure")
	}
	return d.seed.Stewards, nil
}

func TestPriceLegacyBuilderVectors(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
		TenantID             uuid.UUID
		Lookup               legacyPriceLookup
	}
	raw, err := os.ReadFile("../../../../tests/golden/kn-notify-price-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 118 {
		t.Fatalf("vector count %d", len(inputs))
	}
	type result struct {
		Name   string
		Drafts []port.NotificationDraft
		Error  string
		Calls  []string
	}
	results := make([]result, 0, len(inputs))
	for _, in := range inputs {
		directory := &legacyPriceDirectory{seed: in.Lookup}
		svc := &Service{}
		if !in.Lookup.Nil {
			svc.PriceDesk = directory
		}
		messages, _, err := domain.PriceMessages(in.Event, json.RawMessage(in.Payload), in.At)
		var ns []domain.Notification
		if err == nil {
			for _, m := range messages {
				var recipients []uuid.UUID
				if m.Tinh {
					recipients, err = svc.priceTinh(context.Background(), in.TenantID, m)
					if err != nil {
						ns = nil
						break
					}
				}
				ns = append(ns, m.Notifications(recipients)...)
			}
		}
		var drafts []port.NotificationDraft
		if ns != nil {
			drafts = make([]port.NotificationDraft, len(ns))
		}
		for i, n := range ns {
			drafts[i] = port.NotificationDraft{RecipientRef: n.RecipientRef, Audience: port.DraftAudience{National: n.Audience.National, Units: n.Audience.Units, OrgRef: n.Audience.OrgRef, Roles: n.Audience.Roles}, Kind: n.Kind, Severity: n.Severity, Title: n.Title, Body: n.Body, DeepLink: n.DeepLink, SubjectRef: n.SubjectRef, CreatedAt: n.CreatedAt, Email: n.Email}
		}
		out := result{Name: in.Name, Drafts: drafts, Calls: directory.calls}
		if err != nil {
			out.Error = err.Error()
		}
		results = append(results, out)
	}
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("OLD_PRICE=" + string(data))
}
