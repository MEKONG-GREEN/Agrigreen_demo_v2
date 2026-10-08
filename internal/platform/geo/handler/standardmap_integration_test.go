package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// fakeWaterways là lớp đường thủy giả (pf/waterway có kiểm thử riêng cho truy vấn khung nhìn).
type fakeWaterways struct{ segs []port.LayerSegment }

func (f fakeWaterways) SegmentsInBBox(_ context.Context, minLng, minLat, maxLng, maxLat float64, minLoad decimal.Decimal, limit int) (port.LayerPage, error) {
	out := port.LayerPage{VersionNo: 3}
	for _, s := range f.segs {
		p := s.Line[0]
		if p.Lng < minLng || p.Lng > maxLng || p.Lat < minLat || p.Lat > maxLat {
			continue
		}
		s.Passable = !minLoad.IsPositive() || (s.MaxLoadTons != nil && !s.MaxLoadTons.LessThan(minLoad))
		if s.Passable {
			out.PassableCount++
		}
		if len(out.Items) == limit {
			out.Truncated = true
			continue
		}
		out.Items = append(out.Items, s)
	}
	return out, nil
}

// synthetic dựng bộ dữ liệu tổng hợp của một xã (31117, Cần Thơ): 1.000 thửa 60×60 m, 60 HTX
// có ranh giới, 200 kho/Hub, 100 tuyến đường bộ, 300 đoạn đường thủy, ranh giới xã và tỉnh.
func synthetic(t *testing.T, g *g2) {
	t.Helper()
	adm := g.national()
	fc := func(feats []any) []byte {
		b, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
		return b
	}
	// Ranh giới hành chính (xã + tỉnh) bao trọn vùng dữ liệu.
	expect(t, adm.adminPost("/v1/geo/admin-boundaries/import?source=T%E1%BB%95ng%20h%E1%BB%A3p", "application/geo+json", fc([]any{
		map[string]any{"type": "Feature", "properties": map[string]any{"code": "31117"}, "geometry": rect(-1000, -1000, 6000, 6000)},
		map[string]any{"type": "Feature", "properties": map[string]any{"code": "92"}, "geometry": rect(-2000, -2000, 9000, 9000)},
	})), http.StatusOK, "nhập ranh giới hành chính")
	// 1.000 thửa: 5 lần nhập × 200 (giới hạn mỗi lần nhập).
	n := 0
	for batch := range 5 {
		feats := make([]any, 0, 200)
		for range 200 {
			i, j := n%32, n/32
			feats = append(feats, map[string]any{"type": "Feature", "properties": map[string]any{"name": fmt.Sprintf("T%04d", n), "commune_code": "31117"},
				"geometry": rect(float64(i)*70, float64(j)*70, 60, 60)})
			n++
		}
		r := adm.adminPost(fmt.Sprintf("/v1/geo/plots/import?file_name=thua_%d.geojson", batch), "application/geo+json", fc(feats))
		if r.status != http.StatusOK || num(r.body, "accepted") != 200 {
			t.Fatalf("nhập thửa lần %d: %d %s", batch, r.status, string(r.raw[:min(len(r.raw), 400)]))
		}
	}
	// 60 HTX có ranh giới.
	coops := make([]any, 0, 60)
	for k := range 60 {
		ref := ids.New()
		g.orgs.add(ref, fmt.Sprintf("HTXS%02d", k), "31117")
		coops = append(coops, map[string]any{"type": "Feature", "properties": map[string]any{"cooperative_ref": ref.String()},
			"geometry": rect(float64(k%8)*300, float64(k/8)*300, 280, 280)})
	}
	if r := g.geoAdmin92().adminPost("/v1/geo/cooperative-boundaries/import?format=geojson", "application/geo+json", fc(coops)); num(r.body, "accepted") != 60 {
		t.Fatalf("nhập HTX: %s", r.raw)
	}
	// 200 kho/Hub.
	facs := make([]any, 0, 200)
	for k := range 200 {
		facs = append(facs, map[string]any{"type": "Feature", "geometry": point(float64(k%20)*110, float64(k/20)*110),
			"properties": map[string]any{"ten": fmt.Sprintf("Kho %03d", k), "suc_chua": "500", "ma": fmt.Sprintf("KHO-%03d", k), "loai": "kho"}})
	}
	if r := g.geoAdmin92().adminPost("/v1/geo/facilities/import?province_code=92&commune_code=31117", "application/geo+json", fc(facs)); num(r.body, "accepted") != 200 {
		t.Fatalf("nhập kho: %s", r.raw)
	}
	// 100 tuyến đường bộ.
	for k := range 100 {
		y := float64(k) * 22
		expect(t, adm.adminJSON("/v1/geo/roads", map[string]any{"name": fmt.Sprintf("Đường %03d", k), "road_class": "truc_xa", "max_load_tons": "10",
			"geometry": line([2]float64{0, y}, [2]float64{1100, y}, [2]float64{2200, y + 5}), "province_code": "92"}), http.StatusCreated, "tạo đường")
	}
	// 300 đoạn đường thủy (giả).
	dLat, dLon := degPerMetre(baseLat)
	segs := make([]port.LayerSegment, 0, 300)
	for k := range 300 {
		x := baseLon + float64(k%30)*70*dLon
		y := baseLat + float64(k/30)*220*dLat
		load := decimal.NewFromInt(int64(50 + 10*(k%20)))
		segs = append(segs, port.LayerSegment{SegmentRef: ids.New(), Name: fmt.Sprintf("Kênh %03d", k), Class: wwport.ClassFieldCanal,
			MaxLoadTons: &load, Certain: true, Line: []port.LayerPoint{{Lat: y, Lng: x}, {Lat: y + 200*dLat, Lng: x}}})
	}
	g.mod.UseWaterways(fakeWaterways{segs: segs})
}

func p95(ds []time.Duration) time.Duration {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[(len(s)*95+99)/100-1]
}

func timeIt(t *testing.T, n int, fn func() resp) (time.Duration, resp) {
	t.Helper()
	fn() // khởi động (kế hoạch truy vấn, kết nối)
	ds := make([]time.Duration, 0, n)
	var last resp
	for range n {
		start := time.Now()
		last = fn()
		ds = append(ds, time.Since(start))
		if last.status != http.StatusOK {
			t.Fatalf("lỗi khi đo: %d %s", last.status, last.raw)
		}
	}
	return p95(ds), last
}

// GIS US-INT-02 AC-1 (FN-17): bản đồ chuẩn hóa chỉ đọc cho TMS — MỘT lần gọi trả mọi lớp nền
// (ranh giới hành chính, HTX, kho/Hub kèm widget sức chứa, đường bộ, đường thủy đi được) dạng
// GeoJSON trong khung nhìn, theo quyền (tài khoản dịch vụ vai map_consumer, không có thửa),
// p95 < 500 ms trên bộ dữ liệu tổng hợp. GIS US-BASE-01 AC-2: lớp thửa trong khung nhìn ở zoom
// cận cảnh (1.000 polygon) p95 ≤ 1,5 s. Cách đo: docs/runbooks/gis-performance.md.
func TestStandardMapForTMSLatency_GIS_US_INT_02_US_BASE_01(t *testing.T) {
	g := newG2(t)
	synthetic(t, g)
	tms := g.as(g.tenant, uuid.Nil, port.RoleMapConsumer)
	bbox := fmt.Sprintf("%f,%f,%f,%f", baseLon-0.03, baseLat-0.03, baseLon+0.06, baseLat+0.06)
	path := "/v1/geo/standard-map?zoom=13&bbox=" + bbox

	r := tms.appDo(http.MethodGet, path, nil)
	expect(t, r, http.StatusOK, "bản đồ chuẩn hóa")
	layers := get(r.body, "layers").(map[string]any)
	count := func(l string) float64 { return num(layers, l, "count") }
	if r.body["type"] != "FeatureCollection" || r.body["schema"] != "agrigreen.standard_map/v1" || count("admin_boundaries") != 1 ||
		count("cooperatives") != 60 || count("facilities") != 200 || count("roads") != 100 || count("waterways") != 300 {
		t.Fatalf("các lớp: %v", layers)
	}
	byLayer := map[string]int{}
	for _, f := range list(r.body, "features") {
		byLayer[str(f, "properties", "layer")]++
	}
	if byLayer["plots"] != 0 || byLayer["facilities"] != 200 || len(list(r.body, "features")) != 661 {
		t.Fatalf("không có thửa, đủ đối tượng: %v", byLayer)
	}
	if get(r.body, "capacity_thresholds") == nil || len(list(r.body, "basemaps")) != 0 || str(r.body, "basemap_notice") == "" {
		t.Fatalf("widget kho/Hub và bản đồ nền: %v %v", r.body["capacity_thresholds"], r.body["basemap_notice"])
	}
	// Lọc tải trọng đường thủy/đường bộ và chọn lớp.
	f := tms.appDo(http.MethodGet, path+"&layers=waterways&min_load_tons=200", nil)
	if l := get(f.body, "layers").(map[string]any); len(l) != 1 || num(l, "waterways", "passable_count") != 75 {
		t.Fatalf("lọc tải trọng: %v", l)
	}
	expect(t, tms.appDo(http.MethodGet, path+"&layers=plots", nil), http.StatusUnprocessableEntity, "TMS không lấy lớp thửa")
	// Theo quyền: vai không liên quan bị chặn; tenant khác không thấy gì.
	expect(t, g.as(g.tenant, uuid.Nil, "cgh_viewer").appDo(http.MethodGet, path, nil), http.StatusForbidden, "vai không được đọc")
	other := g.as(ids.New(), uuid.Nil, port.RoleMapConsumer)
	if o := other.appDo(http.MethodGet, path+"&layers=admin_boundaries,cooperatives,facilities,roads", nil); len(list(o.body, "features")) != 0 {
		t.Fatalf("rò tenant: %d đối tượng", len(list(o.body, "features")))
	}

	// Đo độ trễ (30 lần, bỏ lần khởi động).
	tmsP95, _ := timeIt(t, 30, func() resp { return tms.appDo(http.MethodGet, path, nil) })
	t.Logf("GIS US-INT-02: GET /v1/geo/standard-map (661 đối tượng, 5 lớp) p95 = %v", tmsP95)
	if tmsP95 >= 500*time.Millisecond {
		t.Fatalf("GIS US-INT-02: p95 %v ≥ 500 ms", tmsP95)
	}
	plotPath := "/v1/geo/layers?zoom=15&layers=plots&bbox=" + bbox
	plotP95, pr := timeIt(t, 30, func() resp { return g.national().appDo(http.MethodGet, plotPath, nil) })
	t.Logf("GIS US-BASE-01: GET /v1/geo/layers?layers=plots (zoom 15, %d polygon) p95 = %v", len(list(pr.body, "plots", "features")), plotP95)
	if len(list(pr.body, "plots", "features")) != 1000 {
		t.Fatalf("1.000 polygon trong khung nhìn: %d", len(list(pr.body, "plots", "features")))
	}
	if plotP95 > 1500*time.Millisecond {
		t.Fatalf("GIS US-BASE-01: p95 %v > 1,5 s", plotP95)
	}
}
