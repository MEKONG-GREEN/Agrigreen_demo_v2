package handler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

type mechAsOfAt interface {
	AsOfAt(ctx context.Context, key metricsport.Key, d clock.Date, cutoff time.Time, sc metricsport.Scope) (metricsport.Value, error)
}

// CGH FN-10/11 (pf/metrics cgh.*): giá trị theo vụ đọc lần cân đối ĐÃ LƯU mới nhất có
// recorded_at < cutoff — lần tính lại ghi nhận sau cutoff không đổi giá trị cũ.
func TestCGHFN11MetricsAsOfLatestStoredRun(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	ctx := context.Background()
	src := f.mod.MetricsSource().(mechAsOfAt)
	dbNow := func() time.Time {
		var x time.Time
		if err := f.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	day := clock.BusinessDate(time.Now())
	at := func(key metricsport.Key, cut time.Time) (metricsport.Value, error) {
		return src.AsOfAt(ctx, key, day, cut, metricsport.Scope{TenantID: f.tenant, Dimension: "DX-2025"})
	}
	if _, err := at("cgh.coop_sufficiency_pct", dbNow()); !errors.Is(err, metricsport.ErrNoData) {
		t.Fatalf("chưa có lần cân đối → ErrNoData: %v", err)
	}
	o := adm.owner(f.c1, "0907000001")
	adm.machine(f.c1, o, "MLD", "IM-1", "2025-01-01")
	adm.area(f.c1, "DX", 2025, "100", false)
	run1 := adm.compute("DX", 2025)["id"].(string)
	cut := dbNow()
	adm.area(f.c1, "DX", 2025, "1000", false)
	run2 := adm.compute("DX", 2025)["id"].(string)
	late := dbNow().Add(time.Second)

	v1, err := at("cgh.coop_sufficiency_pct", cut)
	if err != nil || v1.Watermark.LastEventID.String() != run1 {
		t.Fatalf("tại cutoff dùng lần cân đối 1: %+v %v", v1, err)
	}
	again, _ := at("cgh.coop_sufficiency_pct", cut)
	if !again.Value.Equal(v1.Value) {
		t.Fatal("tất định: gọi lại cùng cutoff phải cùng giá trị")
	}
	v2, err := at("cgh.coops_shortage", late)
	if err != nil || v2.Watermark.LastEventID.String() != run2 {
		t.Fatalf("sau cutoff dùng lần cân đối 2: %+v %v", v2, err)
	}
	if _, err := src.AsOfAt(ctx, "cgh.coop_sufficiency_pct", day, late, metricsport.Scope{TenantID: f.tenant, Dimension: "HT-2025"}); !errors.Is(err, metricsport.ErrNoData) {
		t.Fatalf("vụ khác → ErrNoData: %v", err)
	}
	scopes, err := f.mod.MetricsSource().(metricsport.ScopeLister).Scopes(ctx, "cgh.coops_shortage", day, f.tenant)
	if err != nil || len(scopes) == 0 || scopes[0].Dimension != "DX-2025" {
		t.Fatalf("scopes: %+v %v", scopes, err)
	}
}
