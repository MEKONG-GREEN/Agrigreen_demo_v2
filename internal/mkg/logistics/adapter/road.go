package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
)

// HaversineRoad là port.RoadRoutes khi chưa có OSRM trong lãnh thổ (RB-7): km = Haversine × Factor (#10,
// mặc định 1,3), nhãn haversine. Raw là JSON tất định của phép tính (điểm, hệ số, km) để route_snapshot
// lưu nguyên văn như với nền tảng — tái hiện được mà không tính lại.
type HaversineRoad struct {
	Factor decimal.Decimal
	Now    func() time.Time
}

var _ port.RoadRoutes = HaversineRoad{}

type haversineLeg struct {
	From        [2]string `json:"from"` // [lat, lng]
	To          [2]string `json:"to"`
	HaversineKm string    `json:"haversine_km"`
	DistanceKm  string    `json:"distance_km"`
}

type haversineRaw struct {
	Method     string         `json:"method"`
	Rule       string         `json:"rule_version"`
	Factor     string         `json:"factor"`
	DistanceKm string         `json:"distance_km"`
	Legs       []haversineLeg `json:"legs"`
}

// Route cài đặt port.RoadRoutes.
func (h HaversineRoad) Route(_ context.Context, _ uuid.UUID, req port.RouteRequest) (port.RouteResult, error) {
	if len(req.Points) < 2 {
		return port.RouteResult{}, unavailable("cần ít nhất 2 điểm")
	}
	f := h.Factor
	if f.Sign() <= 0 {
		f = decimal.RequireFromString("1.3")
	}
	raw := haversineRaw{Method: "haversine_x_factor", Rule: domain.RuleRoute, Factor: f.String()}
	res := port.RouteResult{Found: true, Source: port.RouteHaversine}
	total := decimal.Zero
	for i := 1; i < len(req.Points); i++ {
		a, b := req.Points[i-1], req.Points[i]
		hk := domain.HaversineKm(a, b)
		km := hk.Mul(f).Round(3)
		total = total.Add(km)
		res.Legs = append(res.Legs, port.RouteLeg{From: a, To: b, Found: true, DistanceKm: km, Source: port.RouteHaversine,
			Path: []port.GeoPoint{a, b}})
		raw.Legs = append(raw.Legs, haversineLeg{From: [2]string{a.Lat.String(), a.Lng.String()}, To: [2]string{b.Lat.String(), b.Lng.String()},
			HaversineKm: hk.StringFixed(3), DistanceKm: km.StringFixed(3)})
	}
	res.DistanceKm = total
	raw.DistanceKm = total.StringFixed(3)
	b, err := json.Marshal(raw)
	if err != nil {
		return port.RouteResult{}, err
	}
	sum := sha256.Sum256(b)
	res.Raw, res.RawSHA256 = b, hex.EncodeToString(sum[:])
	if h.Now != nil {
		res.FetchedAt = h.Now()
	}
	return res, nil
}

// FakeWaterway là WaterwayRoutes trong bộ nhớ (kiểm thử, dữ liệu tổng hợp R-95): trả thân
// PartnerWaterwayRoute dựng sẵn qua ParseWaterwayRoute (cùng đường đọc với WaterwayHTTP).
type FakeWaterway struct {
	mu sync.Mutex
	// Down: mô phỏng nền tảng lỗi (ErrRouteUnavailable). NoGrant: 403 cấp quyền (ErrRouteGrantMissing).
	Down, NoGrant bool
	// KmPerLeg: km mỗi chặng trả về (mặc định 42.500); NotFound: mạng không có đường giữa hai điểm.
	KmPerLeg string
	NotFound bool
	Version  int
	Grantor  uuid.UUID
	Calls    int
	Now      func() time.Time
}

type fakeLeg struct {
	From        map[string]json.Number `json:"from"`
	To          map[string]json.Number `json:"to"`
	Found       bool                   `json:"found"`
	DistanceKm  string                 `json:"distance_km"`
	AccessKm    string                 `json:"access_km"`
	Source      string                 `json:"source"`
	Geometry    any                    `json:"geometry"`
	SegmentRefs []string               `json:"segment_refs"`
	Limiting    any                    `json:"limiting"`
	Reason      *string                `json:"reason"`
}

// Route cài đặt port.WaterwayRoutes.
func (f *FakeWaterway) Route(_ context.Context, _ uuid.UUID, req port.RouteRequest) (port.RouteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Down {
		return port.RouteResult{}, unavailable("nền tảng giả đang lỗi")
	}
	if f.NoGrant {
		return port.RouteResult{}, port.ErrRouteGrantMissing
	}
	km := f.KmPerLeg
	if km == "" {
		km = "42.500"
	}
	per := decimal.RequireFromString(km)
	total := decimal.Zero
	var legs []fakeLeg
	for i := 1; i < len(req.Points); i++ {
		a, b := req.Points[i-1], req.Points[i]
		l := fakeLeg{From: map[string]json.Number{"lat": json.Number(a.Lat.String()), "lng": json.Number(a.Lng.String())},
			To: map[string]json.Number{"lat": json.Number(b.Lat.String()), "lng": json.Number(b.Lng.String())}, AccessKm: "0.000",
			SegmentRefs: []string{}}
		if f.NotFound {
			l.DistanceKm, l.Source = "0.000", "haversine"
			r := "không có kênh tiếp cận trong bán kính"
			l.Reason = &r
		} else {
			l.Found, l.DistanceKm, l.Source = true, per.StringFixed(3), "routed"
			l.Geometry = map[string]any{"type": "LineString", "coordinates": [][2]json.Number{
				{json.Number(a.Lng.String()), json.Number(a.Lat.String())}, {json.Number(b.Lng.String()), json.Number(b.Lat.String())}}}
			total = total.Add(per)
		}
		legs = append(legs, l)
	}
	v := f.Version
	if v == 0 {
		v = 3
	}
	g := f.Grantor
	if g == uuid.Nil {
		g = uuid.MustParse("00000000-0000-7000-8000-00000000cafe")
	}
	src := "routed"
	if f.NotFound {
		src = "haversine"
	}
	body := map[string]any{"grantor_tenant_id": g.String(), "network_version": v, "found": !f.NotFound,
		"distance_km": total.StringFixed(3), "source": src, "limiting": nil, "legs": legs}
	raw, err := json.Marshal(body)
	if err != nil {
		return port.RouteResult{}, err
	}
	res, err := ParseWaterwayRoute(raw)
	if err != nil {
		return port.RouteResult{}, err
	}
	if f.Now != nil {
		res.FetchedAt = f.Now()
	}
	return res, nil
}

// CallCount trả số lần gọi (đồng bộ).
func (f *FakeWaterway) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls
}

// Set đổi trạng thái giả (đồng bộ).
func (f *FakeWaterway) Set(down, noGrant, notFound bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Down, f.NoGrant, f.NotFound = down, noGrant, notFound
}
