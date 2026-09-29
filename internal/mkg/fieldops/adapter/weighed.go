package adapter

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// LotMassWeighed là port.WeighedMass qua measurement/port.LotMass (cùng tiến
// trình, chỉ port — R-02). Khối lượng cân đầu tiên của lô: HUB_IN; tuyến DIRECT
// (không qua hub) dùng CUSTOMER_IN.
//
// Giả định (người tích hợp G3-2 chốt): lotRef của measurement là mã lô gom
// (= lệnh gom). Nếu measurement cân theo lô hub, cần ánh xạ lô gom → lô hub.
type LotMassWeighed struct{ Lots mport.LotMass }

var _ port.WeighedMass = LotMassWeighed{}

// WeighedKg cài đặt port.WeighedMass.
func (a LotMassWeighed) WeighedKg(ctx context.Context, tenantID, lotRef uuid.UUID) (mass.Kg, time.Time, bool, error) {
	v, err := a.Lots.LotMass(ctx, tenantID, lotRef)
	if errors.Is(err, mport.ErrNotFound) {
		return mass.Kg{}, time.Time{}, false, nil
	}
	if err != nil {
		return mass.Kg{}, time.Time{}, false, err
	}
	k := v.HubInKg
	if v.Route == mport.RouteDirect || k == nil {
		k = v.CustomerInKg
	}
	if k == nil {
		return mass.Kg{}, time.Time{}, false, nil
	}
	return *k, v.AsOf, true, nil
}

// FakeWeighed là WeighedMass trong bộ nhớ.
type FakeWeighed struct {
	mu   sync.Mutex
	lots map[uuid.UUID]fakeWeight
}

type fakeWeight struct {
	kg mass.Kg
	at time.Time
}

// NewFakeWeighed tạo bản giả rỗng.
func NewFakeWeighed() *FakeWeighed { return &FakeWeighed{lots: map[uuid.UUID]fakeWeight{}} }

// Set đặt khối lượng cân của một lô gom.
func (f *FakeWeighed) Set(lot uuid.UUID, kg string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lots[lot] = fakeWeight{kg: mass.MustKg(kg), at: at}
}

// WeighedKg cài đặt port.WeighedMass.
func (f *FakeWeighed) WeighedKg(_ context.Context, _, lot uuid.UUID) (mass.Kg, time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.lots[lot]
	return w.kg, w.at, ok, nil
}
