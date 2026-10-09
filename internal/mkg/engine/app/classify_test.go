package app

import (
	"slices"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
)

func TestClassifyNeverMixesStaleIntoFresh(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	obs := []port.Observation{
		{EntityKey: "fresh", AsOf: now.Add(-10 * time.Minute), MaxAge: 15 * time.Minute},
		{EntityKey: "stale", AsOf: now.Add(-16 * time.Minute), MaxAge: 15 * time.Minute},
		// Quá hạn theo as_of của chính nó nhưng feed đã đọc tới đầu sau đó → còn hiệu lực tới as_of của feed.
		{EntityKey: "carried", AsOf: now.Add(-2 * time.Hour), MaxAge: 15 * time.Minute},
		{EntityKey: "gone", AsOf: now, MaxAge: time.Hour, Retracted: true},
	}
	feedAsOf := now.Add(-5 * time.Minute)
	snap := Classify(port.Query{Kind: port.KindPlot, EntityKeys: []string{"fresh", "stale", "carried", "gone", "never"}},
		obs, []*time.Time{nil, nil, &feedAsOf, nil}, now)
	keys := func(os []port.Observation) []string {
		var out []string
		for _, o := range os {
			out = append(out, o.EntityKey)
		}
		return out
	}
	if got := keys(snap.Fresh); !slices.Equal(got, []string{"fresh", "carried"}) {
		t.Errorf("Fresh = %v", got)
	}
	if got := keys(snap.Stale); !slices.Equal(got, []string{"stale"}) {
		t.Errorf("Stale = %v", got)
	}
	if !slices.Equal(snap.Missing, []string{"gone", "never"}) {
		t.Errorf("Missing = %v", snap.Missing)
	}
	if !snap.Fresh[1].EffectiveAsOf.Equal(feedAsOf) || !snap.Fresh[0].EffectiveAsOf.Equal(obs[0].AsOf) {
		t.Errorf("EffectiveAsOf: %v %v", snap.Fresh[0].EffectiveAsOf, snap.Fresh[1].EffectiveAsOf)
	}
}

func TestPullableNeedsFlagForProposed(t *testing.T) {
	s := &Service{}
	if why, ok := s.pullable("harvest_progress_by_commune"); ok || why != "contract_not_approved" {
		t.Errorf("proposed không cờ: %q %v", why, ok)
	}
	s.AllowProposed = true
	if _, ok := s.pullable("harvest_progress_by_commune"); !ok {
		t.Error("proposed + cờ dev/test phải kéo được")
	}
	s.Production = true
	if _, ok := s.pullable("harvest_progress_by_commune"); ok {
		t.Error("production không kéo hợp đồng proposed kể cả có cờ")
	}
	if why, ok := (&Service{AllowProposed: true}).pullable("collection_volume_by_commune"); ok || why != "no_mapper" {
		t.Errorf("feed không có bộ ánh xạ: %q %v", why, ok)
	}
}
