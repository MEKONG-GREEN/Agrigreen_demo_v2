package notifyconsumers_test

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/google/uuid"
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
func TestPriceDirectoryMatchesLegacyAndCopiesProfiles(t *testing.T) {
	tid := uuid.New()
	for _, mode := range []string{"normal", "error", "nil-holders"} {
		for _, profile := range []string{"first", "", "second", "unknown"} {
			t.Run(mode+"/"+profile, func(t *testing.T) {
				profiles := []priceport.Profile{{Name: "first", Department: "dept1", StewardDuty: "duty1"}, {Name: "second", Department: "dept2"}}
				a, b := &priceHolders{failure: mode == "error", refs: []uuid.UUID{tid}}, &priceHolders{failure: mode == "error", refs: []uuid.UUID{tid}}
				var x, y accport.RoleHolders = a, b
				if mode == "nil-holders" {
					x, y = nil, nil
				}
				legacy := assembly.NotifyPriceDesk{Holders: x, Profiles: append([]priceport.Profile(nil), profiles...)}
				owned := price.NewNotificationDirectory(y, profiles)
				profiles[0].Name = "corrupt"
				profiles[0].Department = "corrupt"
				profiles[0].StewardDuty = "corrupt"
				for _, kind := range []string{"officers", "stewards"} {
					var got, want []uuid.UUID
					var errA, errB error
					if kind == "officers" {
						want, errA = legacy.ProvinceOfficers(context.Background(), tid, profile)
						got, errB = owned.ProvinceOfficers(context.Background(), tid, profile)
					} else {
						want, errA = legacy.Stewards(context.Background(), tid, profile)
						got, errB = owned.Stewards(context.Background(), tid, profile)
					}
					ea, eb := "", ""
					if errA != nil {
						ea = errA.Error()
					}
					if errB != nil {
						eb = errB.Error()
					}
					if !reflect.DeepEqual(got, want) || ea != eb || !reflect.DeepEqual(a.calls, b.calls) {
						t.Fatalf("directory mismatch %v %v %v %v", got, want, a.calls, b.calls)
					}
				}
			})
		}
	}
}
