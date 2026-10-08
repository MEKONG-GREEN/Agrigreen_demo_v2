package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// GIS US-BASE-01 AC-2 (W3.3: lớp thửa do pf/cooperative dựng cho bản đồ gộp của pf/geo qua geoport.MapLayers): lớp
// thửa trong khung nhìn ở zoom cận cảnh (1.000 polygon) p95 ≤ 1,5 s. Cách đo: docs/runbooks/gis-performance.md.
func TestPlotLayer1000Latency_GIS_US_BASE_01(t *testing.T) {
	g := newG2(t)
	adm := g.national()
	n := 0
	for batch := range 5 {
		feats := make([]any, 0, 200)
		for range 200 {
			i, j := n%32, n/32
			feats = append(feats, map[string]any{"type": "Feature", "properties": map[string]any{"name": fmt.Sprintf("T%04d", n), "commune_code": "31117"},
				"geometry": rect(float64(i)*70, float64(j)*70, 60, 60)})
			n++
		}
		b, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
		r := adm.adminPost(fmt.Sprintf("/v1/geo/plots/import?file_name=thua_%d.geojson", batch), "application/geo+json", b)
		if r.status != http.StatusOK || num(r.body, "accepted") != 200 {
			t.Fatalf("nhập thửa lần %d: %d", batch, r.status)
		}
	}
	layers := g.mod.MapLayers()
	q := geoport.MapLayerQuery{MinLon: baseLon - 0.03, MinLat: baseLat - 0.03, MaxLon: baseLon + 0.06, MaxLat: baseLat + 0.06, Zoom: 15}
	ctx := authz.WithPrincipal(hubCtx(g.fixture), authz.Principal{ActorRef: g.tenant, TenantID: g.tenant, Roles: []string{authz.RolePlatformAdmin}})
	run := func() (map[string]any, time.Duration) {
		start := time.Now()
		fc, err := layers.PlotLayer(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return fc, time.Since(start)
	}
	run() // khởi động
	ds := make([]time.Duration, 0, 30)
	var fc map[string]any
	for range 30 {
		var d time.Duration
		fc, d = run()
		ds = append(ds, d)
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	p95 := ds[(len(ds)*95+99)/100-1]
	feats, _ := fc["features"].([]map[string]any)
	t.Logf("GIS US-BASE-01: lớp thửa (zoom 15, %d polygon) p95 = %v", len(feats), p95)
	if len(feats) != 1000 {
		t.Fatalf("1.000 polygon trong khung nhìn: %d", len(feats))
	}
	if p95 > 1500*time.Millisecond {
		t.Fatalf("GIS US-BASE-01: p95 %v > 1,5 s", p95)
	}
}
