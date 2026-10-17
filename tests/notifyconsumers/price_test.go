package notifyconsumers_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/google/uuid"
	"os"
	"reflect"
	"testing"
	"time"
)

type priceLookupSeed struct {
	Nil                bool
	Officers, Stewards []uuid.UUID
	Error              string
}
type priceFixtureDirectory struct {
	seed  priceLookupSeed
	calls []string
}

func (d *priceFixtureDirectory) ProvinceOfficers(_ context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	d.calls = append(d.calls, "officers|"+tid.String()+"|"+profile)
	if d.seed.Error == "officers" {
		return nil, errors.New("fixture officers failure")
	}
	return d.seed.Officers, nil
}
func (d *priceFixtureDirectory) Stewards(_ context.Context, tid uuid.UUID, profile string) ([]uuid.UUID, error) {
	d.calls = append(d.calls, "stewards|"+tid.String()+"|"+profile)
	if d.seed.Error == "stewards" {
		return nil, errors.New("fixture stewards failure")
	}
	return d.seed.Stewards, nil
}
func TestPriceOwnedBuilderMatchesLegacy(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
		TenantID             uuid.UUID
		Lookup               priceLookupSeed
	}
	var expected []struct {
		Name   string
		Drafts []notifyport.NotificationDraft
		Error  string
		Calls  []string
	}
	for path, target := range map[string]any{"../golden/kn-notify-price-inputs.json": &inputs, "../golden/kn-notify-price.json": &expected} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 118 || len(expected) != len(inputs) {
		t.Fatal("missing price vectors")
	}
	for i, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			directory := &priceFixtureDirectory{seed: in.Lookup}
			var lookup priceport.PriceDesk
			if !in.Lookup.Nil {
				lookup = directory
			}
			registrations := price.NotificationBuilders(lookup)
			if len(registrations) != 1 || len(registrations[0].Events) != 9 {
				t.Fatal("price builder catalog")
			}
			got, err := registrations[0].Build(context.Background(), in.TenantID, in.Event, json.RawMessage(in.Payload), in.At)
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			want := expected[i]
			if want.Name != in.Name || !reflect.DeepEqual(got, want.Drafts) || errText != want.Error || !reflect.DeepEqual(directory.calls, want.Calls) {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(want.Drafts)
				t.Fatalf("got %s / %q / %v; want %s / %q / %v", a, errText, directory.calls, b, want.Error, want.Calls)
			}
		})
	}
}
