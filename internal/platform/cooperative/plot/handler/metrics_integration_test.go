package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/pubgolden"
)

// pf/metrics geo.plots_digitised: thửa đã số hoá như hệ biết trước cutoff — thửa tạo
// và ngừng dùng ghi nhận sau cutoff không đổi giá trị. Hình học as-of theo id dòng sổ
// (GIS US-REPLAY-01, geometry_ref).
func TestMetricsPlotsDigitisedAsOf(t *testing.T) {
	f := newFixture(t)
	nat := f.national()
	ctx := context.Background()
	src := pubgolden.Compare(t, "pf/geo", f.mod.MetricsSource()) // W6.3: đối chứng nguồn view _pub
	now := func() time.Time {
		var x time.Time
		if err := f.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	day := clock.BusinessDate(time.Now())
	val := func(at time.Time, units ...string) string {
		t.Helper()
		v, err := src.AsOfAt(ctx, "geo.plots_digitised", day, at, metricsport.Scope{TenantID: f.tenant, Units: units})
		if err != nil {
			t.Fatal(err)
		}
		return v.Value.String()
	}
	p1, _ := nat.createPlot("31117", f.coopA, rect(0, 0, 60, 60))
	cut := now()
	nat.createPlot("31120", uuid.Nil, rect(200, 0, 260, 60))
	r := nat.appDo(http.MethodPost, "/v1/plots/"+p1.String()+"/retire", map[string]any{"reason": "Thửa đã chuyển đổi sang đất ở"})
	expect(t, r, http.StatusOK, "ngừng dùng")
	late := now().Add(time.Second)

	if got := val(cut); got != "1" {
		t.Fatalf("tại cutoff: %s", got)
	}
	if got := val(late); got != "1" {
		t.Fatalf("sau: thửa 1 ngừng dùng, thửa 2 mới → 1, nhận %s", got)
	}
	if got := val(late, "31117"); got != "0" {
		t.Fatalf("xã 31117 sau ngừng dùng: %s", got)
	}
	if got := val(cut, "31117"); got != "1" {
		t.Fatalf("xã 31117 tại cutoff: %s", got)
	}

	var bid uuid.UUID
	if err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT current_boundary_id FROM pf_coop.plot WHERE ref = $1", p1).Scan(&bid)
	}); err != nil {
		t.Fatal(err)
	}
	ref := metricsport.GeometryRef{Kind: "plot_boundary", ID: bid}
	g, err := src.Geometries(tenant.WithID(ctx, f.tenant), f.mod.LayerGeometry(), []metricsport.GeometryRef{ref, {Kind: "unknown", ID: bid}})
	if err != nil || len(g) != 1 || len(g[ref]) == 0 {
		t.Fatalf("hình học: %v %v", g, err)
	}
}
