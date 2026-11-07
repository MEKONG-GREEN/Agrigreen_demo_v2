package handler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type unavailablePlots struct{ calls atomic.Int32 }

func (p *unavailablePlots) DigitizedByCommune(context.Context, []string) ([]knport.CommunePlots, error) {
	p.calls.Add(1)
	return nil, errors.New("HTX is unavailable in the standalone KN profile")
}

// FN-18: no HTX dependency or obsolete payload; keep the CNTS endpoint and commune scope.
func TestCNTSMapStandaloneWithoutHTX(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "empty"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			pool := pgtest.Get(t).AdminPool(t, migrations.PF)
			var count int
			if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pf_cooperative_pub.v1_cooperatives`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("isolated HTX publication must be empty: count=%d, err=%v", count, err)
			}
			if missing {
				if _, err := pool.Exec(context.Background(), `ALTER SCHEMA pf_cooperative_pub RENAME TO cnts266_cooperative_pub`); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := pool.Exec(context.Background(), `ALTER SCHEMA cnts266_cooperative_pub RENAME TO pf_cooperative_pub`); err != nil {
						t.Error(err)
					}
				})
				var absent bool
				if err := pool.QueryRow(context.Background(), `SELECT to_regnamespace('pf_cooperative_pub') IS NULL`).Scan(&absent); err != nil || !absent {
					t.Fatalf("HTX namespace must be absent: %v, %v", absent, err)
				}
			}
			plots := &unavailablePlots{}
			f := newFixWithDeps(t, extension.Deps{Units: units{}, Plots: plots})
			f.seedSite("30001", "Điểm KN xã 1", 10.4, 105.1)
			f.seedSite("30002", "Điểm KN xã 2", 10.5, 105.2)
			for _, tc := range []struct {
				name     string
				communes []string
				want     int
			}{
				{name: "province", want: 2},
				{name: "commune", communes: []string{"30001"}, want: 1},
			} {
				caller := f.user(tinh("vien_chuc"))
				if tc.communes != nil {
					caller = f.user(xa(tc.communes...))
				}
				layers := want(t, caller.get(base+"layers"), 200, "", tc.name+" standalone map")
				if got := plots.calls.Load(); got != 0 {
					t.Fatalf("CNTS map read HTX plot statistics %d time(s)", got)
				}
				for _, key := range []string{"plot_stats", "plots_ready"} {
					if _, exists := layers[key]; exists {
						t.Fatalf("removed HTX layer %q is still in CNTS payload", key)
					}
				}
				for _, key := range []string{"zones", "outbreaks", "farms", "zone_impacts"} {
					if _, ok := layers[key].([]any); !ok {
						t.Fatalf("CNTS layer %q missing or not an array", key)
					}
				}
				if len(list(layers, "places")) != tc.want {
					t.Fatalf("%s site scope: %v", tc.name, layers["places"])
				}
				if tc.communes != nil && str(list(layers, "places")[0].(map[string]any), "commune_code") != tc.communes[0] {
					t.Fatal("commune map exposed another commune's site")
				}
			}
		})
	}
}
