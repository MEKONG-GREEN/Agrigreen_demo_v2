package notifyconsumers_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/google/uuid"
	"os"
	"reflect"
	"testing"
)

type priceHolders struct {
	calls   []string
	failure bool
	refs    []uuid.UUID
}

func (h *priceHolders) RoleHolders(_ context.Context, tid uuid.UUID, role, dept string) ([]uuid.UUID, error) {
	h.calls = append(h.calls, "role|"+tid.String()+"|"+role+"|"+dept)
	if h.failure {
		return nil, errors.New("fixture holders failure")
	}
	return h.refs, nil
}
func (h *priceHolders) DutyHolders(_ context.Context, tid uuid.UUID, role, dept, duty string) ([]uuid.UUID, error) {
	h.calls = append(h.calls, "duty|"+tid.String()+"|"+role+"|"+dept+"|"+duty)
	if h.failure {
		return nil, errors.New("fixture holders failure")
	}
	return h.refs, nil
}

type priceDirectoryProfileInput struct{ Name, Department, StewardDuty string }
type priceDirectoryStep struct {
	Operation  string
	Recipients []uuid.UUID
	Error      string
	Calls      []string
}
type priceDirectoryContract struct {
	Mode, Profile string
	TenantID      uuid.UUID
	Profiles      []priceDirectoryProfileInput
	Steps         []priceDirectoryStep
}

// The expected selector calls and results were captured from the old assembly adapter in PRICE-A.
// Read only: this test must not regenerate the legacy oracle from the owned implementation.
func TestPriceDirectoryMatchesFrozenLegacyAndCopiesProfiles(t *testing.T) {
	raw, err := os.ReadFile("../golden/kn-notify-price-directory.json")
	if err != nil {
		t.Fatal(err)
	}
	var contracts []priceDirectoryContract
	if err := json.Unmarshal(raw, &contracts); err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 16 {
		t.Fatal("missing directory contracts")
	}
	operations := 0
	for _, contract := range contracts {
		if len(contract.Steps) != 2 {
			t.Fatal("missing selector operations")
		}
		operations += len(contract.Steps)
		t.Run(contract.Mode+"/"+contract.Profile, func(t *testing.T) {
			profiles := make([]priceport.Profile, len(contract.Profiles))
			for i, p := range contract.Profiles {
				profiles[i] = priceport.Profile{Name: p.Name, Department: p.Department, StewardDuty: p.StewardDuty}
			}
			holder := &priceHolders{failure: contract.Mode == "error", refs: []uuid.UUID{contract.TenantID}}
			var lookup accport.RoleHolders = holder
			if contract.Mode == "nil-holders" {
				lookup = nil
			}
			directory := price.NewNotificationDirectory(lookup, profiles)
			if len(profiles) > 0 {
				profiles[0].Name = "corrupt"
				profiles[0].Department = "corrupt"
				profiles[0].StewardDuty = "corrupt"
			}
			for _, step := range contract.Steps {
				var got []uuid.UUID
				var err error
				switch step.Operation {
				case "officers":
					got, err = directory.ProvinceOfficers(context.Background(), contract.TenantID, contract.Profile)
				case "stewards":
					got, err = directory.Stewards(context.Background(), contract.TenantID, contract.Profile)
				default:
					t.Fatalf("unknown captured operation %q", step.Operation)
				}
				errText := ""
				if err != nil {
					errText = err.Error()
				}
				if !reflect.DeepEqual(got, step.Recipients) || errText != step.Error || !reflect.DeepEqual(holder.calls, step.Calls) {
					t.Fatalf("%s: got %v / %q / %v; want %v / %q / %v", step.Operation, got, errText, holder.calls, step.Recipients, step.Error, step.Calls)
				}
			}
		})
	}
	if operations != 32 {
		t.Fatal("missing directory operations")
	}
}
