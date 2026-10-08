package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/pubgolden"
)

// pf/metrics cc.season_area_ha, cc.yield_t_per_ha theo vụ (Scope.Dimension "DX-2026"):
// sự việc mới nhất trước cutoff; bản ghi bù sản lượng ghi nhận sau cutoff không đổi giá trị cũ.
func TestMetricsSeasonAreaYieldAsOf(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1)
	ctx := context.Background()
	src := pubgolden.Compare(t, "pf/cropcycle", f.mod.MetricsSource()) // W6.3: đối chứng nguồn view _pub
	dbNow := func() time.Time {
		var x time.Time
		if err := f.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	day := clock.BusinessDate(startTime)
	val := func(key metricsport.Key, at time.Time, units ...string) (string, error) {
		v, err := src.AsOfAt(ctx, key, day, at, metricsport.Scope{TenantID: f.tenant, Dimension: "DX-2026", Units: units})
		return v.Value.String(), err
	}
	if _, err := val(metricsport.Key("cc.season_area_ha"), dbNow()); !errors.Is(err, metricsport.ErrNoData) {
		t.Fatalf("chưa có vụ → ErrNoData: %v", err)
	}
	season := fa.mustOpen(f.p1)
	f.advanceDays(91)
	r := fa.post("/v1/cropcycle/seasons/"+season+"/harvest", map[string]any{"mass_kg": "9750", "confirm_anomaly": true})
	expect(t, r, http.StatusCreated, "khai sản lượng")
	cut := dbNow()
	r = fa.post("/v1/cropcycle/harvests/"+str(r.body, "harvest", "id")+"/corrections", map[string]any{"mass_kg": "9000", "correction_reason": "Cân lại tại kho HTX"})
	expect(t, r, http.StatusCreated, "sửa sản lượng")
	late := dbNow().Add(time.Second)

	if got, err := val("cc.season_area_ha", cut); err != nil || got != "1.5" {
		t.Fatalf("diện tích vụ: %s %v", got, err)
	}
	if got, err := val("cc.yield_t_per_ha", cut); err != nil || got != "6.5" {
		t.Fatalf("năng suất tại cutoff (bản sửa ghi nhận muộn bị loại): %s %v", got, err)
	}
	if got, err := val("cc.yield_t_per_ha", late); err != nil || got != "6" {
		t.Fatalf("năng suất sau bản sửa: %s %v", got, err)
	}
	if _, err := val("cc.yield_t_per_ha", late, "31120"); !errors.Is(err, metricsport.ErrNoData) {
		t.Fatalf("xã khác → ErrNoData: %v", err)
	}
	scopes, err := src.Scopes(ctx, "cc.season_area_ha", clock.BusinessDate(time.Now()), f.tenant)
	if err != nil || len(scopes) != 2 || scopes[0].Dimension != "DX-2026" || len(scopes[1].Units) != 1 {
		t.Fatalf("scopes: %+v %v", scopes, err)
	}
}
