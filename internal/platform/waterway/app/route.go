package app

import (
	"context"
	"math"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// resolveVersion: no = 0 → phiên bản đang phát hành (nil nếu chưa có); no > 0 →
// đúng phiên bản đó nếu đã từng phát hành (kể cả đã bị thay) — ghim phiên bản.
func resolveVersion(ctx context.Context, tx pgx.Tx, no int) (*domain.NetworkVersion, error) {
	if no == 0 {
		return store.PublishedVersion(ctx, tx, false)
	}
	v, err := store.VersionByNo(ctx, tx, no, false)
	if err != nil {
		return nil, err
	}
	if v == nil || !v.Routable() {
		return nil, ErrVersionNotFound
	}
	return v, nil
}

// SegmentView là một đoạn của lớp bản đồ kèm cờ phù hợp tải trọng.
type SegmentView struct {
	domain.VersionSegment
	Passable bool
}

// SegmentsResult là kết quả tra lớp đường thủy (GIS US-WATER-01).
type SegmentsResult struct {
	Version       *domain.NetworkVersion
	Items         []SegmentView
	Next          *string
	Total         int // số đoạn trong khung (mọi trang)
	PassableCount int // số đoạn phù hợp tải trọng trong khung (mọi trang)
}

// Segments trả các tuyến của phiên bản đã phát hành (hoặc ghim) trong khung
// bbox, đánh dấu tuyến phù hợp tải trọng sà lan — GIS US-WATER-01 AC-1; AC-2:
// PassableCount = 0 → không có tuyến phù hợp. Tuyến nháp không bao giờ xuất hiện.
func (s *Service) SegmentLayer(ctx context.Context, box *store.BBox, minLoad decimal.Decimal, version int, after uuid.UUID, limit int) (SegmentsResult, error) {
	p, err := reader(ctx)
	if err != nil {
		return SegmentsResult{}, err
	}
	if minLoad.IsNegative() {
		return SegmentsResult{}, problem.Validation.WithField("min_load_tons", domain.MsgPositiveNumbers)
	}
	limit = ClampLimit(limit)
	var out SegmentsResult
	err = s.readTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, err := resolveVersion(ctx, tx, version)
		if err != nil || v == nil {
			return err
		}
		out.Version = v
		if out.Total, out.PassableCount, err = store.CountVersionSegments(ctx, tx, v.ID, box, minLoad); err != nil {
			return err
		}
		page, err := store.ListVersionSegments(ctx, tx, v.ID, box, after, limit+1)
		if err != nil {
			return err
		}
		if len(page) > limit {
			page = page[:limit]
			c := EncodeUUIDCursor(page[limit-1].SegmentRef)
			out.Next = &c
		}
		for _, sg := range page {
			out.Items = append(out.Items, SegmentView{VersionSegment: sg, Passable: sg.Passable(minLoad)})
		}
		return nil
	})
	return out, err
}

// Route định tuyến giữa hai điểm (Application API, GIS US-WATER-01 / Simulation FN-04).
func (s *Service) Route(ctx context.Context, from, to port.LatLng, minLoad decimal.Decimal, version int) (port.Route, error) {
	if _, err := reader(ctx); err != nil {
		return port.Route{}, err
	}
	return s.RouteBetween(ctx, []port.LatLng{from, to}, minLoad, version)
}

// RouteBetween thực hiện port.Router cho pf/simulation: tenant lấy từ context.
func (s *Service) RouteBetween(ctx context.Context, points []port.LatLng, minLoad decimal.Decimal, version int) (port.Route, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.Route{}, problem.Unauthenticated
	}
	if len(points) < 2 {
		return port.Route{}, problem.Validation.WithField("points", "cần ít nhất 2 điểm")
	}
	for _, pt := range points {
		if math.Abs(pt.Lat) > 90 || math.Abs(pt.Lng) > 180 || math.IsNaN(pt.Lat) || math.IsNaN(pt.Lng) {
			return port.Route{}, problem.Validation.WithField("points", "Toạ độ không hợp lệ (WGS84).")
		}
	}
	if minLoad.IsNegative() {
		return port.Route{}, problem.Validation.WithField("min_load_tons", domain.MsgPositiveNumbers)
	}
	now := s.now()
	factor, err := s.decParam(ctx, port.ParamHaversineFactor, domain.DefaultHaversineFactor, now)
	if err != nil {
		return port.Route{}, err
	}
	if factor.LessThan(decimal.NewFromInt(1)) {
		factor = decimal.NewFromInt(1)
	}
	radius, err := s.decParam(ctx, port.ParamAccessRadiusM, domain.DefaultAccessRadiusM, now)
	if err != nil {
		return port.Route{}, err
	}
	var out port.Route
	err = s.readTx(ctx, tid, func(tx pgx.Tx) error {
		v, err := resolveVersion(ctx, tx, version)
		if err != nil {
			return err
		}
		if v != nil {
			out.VersionNo = v.VersionNo
		}
		for i := 1; i < len(points); i++ {
			var leg port.Leg
			if v == nil {
				leg = haversineLeg(points[i-1], points[i], factor, "Chưa có phiên bản mạng đường thủy được phát hành — khoảng cách là Haversine hiệu chỉnh.")
			} else if leg, err = routeLeg(ctx, tx, v.ID, points[i-1], points[i], minLoad, radius.InexactFloat64(), factor); err != nil {
				return err
			}
			out.Legs = append(out.Legs, leg)
		}
		return nil
	})
	if err != nil {
		return port.Route{}, err
	}
	out.Found = true
	out.Source = port.SourceRouted
	for _, l := range out.Legs {
		out.DistanceKm = out.DistanceKm.Add(l.DistanceKm)
		out.Found = out.Found && l.Found
		out.Source = weaker(out.Source, l.Source)
		if l.Limiting != nil {
			out.Limiting = worseLimit(out.Limiting, l.Limiting)
		}
	}
	return out, nil
}

// CurrentVersion thực hiện port.Router: số phiên bản đang phát hành (0 nếu chưa có).
func (s *Service) CurrentVersion(ctx context.Context) (int, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return 0, problem.Unauthenticated
	}
	n := 0
	err = s.readTx(ctx, tid, func(tx pgx.Tx) error {
		v, err := store.PublishedVersion(ctx, tx, false)
		if v != nil {
			n = v.VersionNo
		}
		return err
	})
	return n, err
}

var sourceRank = map[string]int{port.SourceRouted: 0, port.SourceDigitised: 1, port.SourceHaversine: 2}

func weaker(a, b string) string {
	if sourceRank[b] > sourceRank[a] {
		return b
	}
	return a
}

func km(m float64) decimal.Decimal { return decimal.NewFromFloat(m / 1000).Round(3) }

func haversineLeg(a, b port.LatLng, factor decimal.Decimal, reason string) port.Leg {
	d := domain.HaversineM(domain.Point{Lng: a.Lng, Lat: a.Lat}, domain.Point{Lng: b.Lng, Lat: b.Lat})
	return port.Leg{From: a, To: b, Found: false, DistanceKm: km(d).Mul(factor).Round(3), AccessKm: decimal.Zero,
		Source: port.SourceHaversine, Reason: reason}
}

func limitOf(e store.EdgeRow) *port.Limiting {
	l := &port.Limiting{MaxLoadTons: e.MaxLoadTons, Certain: e.Certain, Reason: e.Limiting, SegmentRef: e.SegmentRef, StructureRef: e.StructureRef}
	return l
}

// worseLimit: ràng buộc bất lợi hơn — chưa kết luận thắng mọi giá trị.
func worseLimit(a, b *port.Limiting) *port.Limiting {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case a.MaxLoadTons == nil:
		return a
	case b.MaxLoadTons == nil:
		return b
	case b.MaxLoadTons.LessThan(*a.MaxLoadTons):
		return b
	}
	return a
}

func toLatLng(pts []domain.Point) []port.LatLng {
	out := make([]port.LatLng, len(pts))
	for i, p := range pts {
		out[i] = port.LatLng{Lat: p.Lat, Lng: p.Lng}
	}
	return out
}

// routeLeg tìm đường ngắn nhất trong số các cạnh ĐI ĐƯỢC với tải trọng yêu cầu:
// bám điểm đầu/cuối vào cạnh đi được gần nhất, chạy pgr_dijkstra nhiều-nhiều từ
// hai đầu cạnh xuất phát tới hai đầu cạnh đích, cộng phần cạnh dở dang.
func routeLeg(ctx context.Context, tx pgx.Tx, vid uuid.UUID, a, b port.LatLng, minLoad decimal.Decimal, radiusM float64, factor decimal.Decimal) (port.Leg, error) {
	pa, pb := domain.Point{Lng: a.Lng, Lat: a.Lat}, domain.Point{Lng: b.Lng, Lat: b.Lat}
	f, err := store.NearestEdge(ctx, tx, vid, pa, minLoad)
	if err != nil {
		return port.Leg{}, err
	}
	t, err := store.NearestEdge(ctx, tx, vid, pb, minLoad)
	if err != nil {
		return port.Leg{}, err
	}
	noRoute := "Không có tuyến đường thủy liên thông nào đủ điều kiện tải trọng giữa hai điểm — khoảng cách là Haversine hiệu chỉnh, không phải cự ly thực tế."
	if f == nil || t == nil {
		return haversineLeg(a, b, factor, noRoute), nil
	}
	if f.AccessM > radiusM || t.AccessM > radiusM {
		return haversineLeg(a, b, factor, "Điểm đầu hoặc cuối không tiếp giáp mạng đường thủy (vượt bán kính tiếp cận) — khoảng cách là Haversine hiệu chỉnh."), nil
	}
	var path []domain.Point
	var used []store.EdgeRow
	var netM float64
	if f.Edge.ID == t.Edge.ID {
		path = domain.Substring(f.Edge.Points, f.Fraction, t.Fraction)
		netM = math.Abs(t.Fraction-f.Fraction) * f.Edge.CostM
		used = []store.EdgeRow{f.Edge}
	} else {
		starts := map[int64]float64{}
		ends := map[int64]float64{}
		put := func(m map[int64]float64, k int64, v float64) {
			if old, ok := m[k]; !ok || v < old {
				m[k] = v
			}
		}
		put(starts, f.Edge.Source, f.Fraction*f.Edge.CostM)
		put(starts, f.Edge.Target, (1-f.Fraction)*f.Edge.CostM)
		put(ends, t.Edge.Source, t.Fraction*t.Edge.CostM)
		put(ends, t.Edge.Target, (1-t.Fraction)*t.Edge.CostM)
		sv, ev := keys(starts), keys(ends)
		steps, err := store.Dijkstra(ctx, tx, vid, minLoad, sv, ev)
		if err != nil {
			return port.Leg{}, err
		}
		type pair struct{ s, e int64 }
		agg := map[pair]float64{}
		route := map[pair][]store.PathStep{}
		for _, st := range steps {
			k := pair{st.Start, st.End}
			if st.Edge == -1 {
				agg[k] = st.AggCost
				continue
			}
			route[k] = append(route[k], st)
		}
		for _, s := range sv {
			for _, e := range ev {
				if s == e {
					agg[pair{s, e}] = 0
				}
			}
		}
		best, bestCost, found := pair{}, math.Inf(1), false
		for _, s := range sv {
			for _, e := range ev {
				c, ok := agg[pair{s, e}]
				if !ok {
					continue
				}
				total := starts[s] + c + ends[e]
				if total < bestCost-1e-9 {
					best, bestCost, found = pair{s, e}, total, true
				}
			}
		}
		if !found {
			return haversineLeg(a, b, factor, noRoute), nil
		}
		netM = bestCost
		var edgeIDs []int64
		for _, st := range route[best] {
			edgeIDs = append(edgeIDs, st.Edge)
		}
		rows, err := store.EdgesByID(ctx, tx, vid, edgeIDs)
		if err != nil {
			return port.Leg{}, err
		}
		if best.s == f.Edge.Source {
			path = domain.Substring(f.Edge.Points, f.Fraction, 0)
		} else {
			path = domain.Substring(f.Edge.Points, f.Fraction, 1)
		}
		used = append(used, f.Edge)
		for _, st := range route[best] {
			e := rows[st.Edge]
			pts := e.Points
			if e.Source != st.Node {
				pts = domain.Reverse(pts)
			}
			path = domain.AppendPath(path, pts)
			used = append(used, e)
		}
		if best.e == t.Edge.Source {
			path = domain.AppendPath(path, domain.Substring(t.Edge.Points, 0, t.Fraction))
		} else {
			path = domain.AppendPath(path, domain.Substring(t.Edge.Points, 1, t.Fraction))
		}
		used = append(used, t.Edge)
	}
	leg := port.Leg{From: a, To: b, Found: true, Source: port.SourceRouted, Path: toLatLng(path),
		AccessKm: km(f.AccessM + t.AccessM), DistanceKm: km(netM + f.AccessM + t.AccessM)}
	seen := map[uuid.UUID]bool{}
	for _, e := range used {
		if e.DataSource == domain.SourceDigitised {
			leg.Source = port.SourceDigitised
		}
		if !seen[e.SegmentRef] {
			seen[e.SegmentRef] = true
			leg.SegmentRefs = append(leg.SegmentRefs, e.SegmentRef)
		}
		leg.Limiting = worseLimit(leg.Limiting, limitOf(e))
	}
	return leg, nil
}

func keys(m map[int64]float64) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CurrentPublished đọc phiên bản đang phát hành (Application API); nil nếu chưa có.
func (s *Service) CurrentPublished(ctx context.Context) (*domain.NetworkVersion, error) {
	p, err := reader(ctx)
	if err != nil {
		return nil, err
	}
	var v *domain.NetworkVersion
	err = s.readTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = store.PublishedVersion(ctx, tx, false)
		return e
	})
	return v, err
}

var _ port.Layer = (*Service)(nil)

// SegmentsInBBox thực hiện port.Layer: một trang (tối đa limit) tuyến của phiên bản đang phát
// hành giao khung nhìn, kèm cờ đi được theo tải trọng (GIS US-INT-02).
func (s *Service) SegmentsInBBox(ctx context.Context, minLng, minLat, maxLng, maxLat float64, minLoad decimal.Decimal, limit int) (port.LayerPage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	box := &store.BBox{MinLng: minLng, MinLat: minLat, MaxLng: maxLng, MaxLat: maxLat}
	var items []SegmentView
	after := uuid.Nil
	out := port.LayerPage{}
	for {
		res, err := s.SegmentLayer(ctx, box, minLoad, 0, after, min(200, limit-len(items)))
		if err != nil {
			return port.LayerPage{}, err
		}
		if res.Version != nil {
			out.VersionNo = res.Version.VersionNo
		}
		out.PassableCount = res.PassableCount
		items = append(items, res.Items...)
		if res.Next == nil || len(res.Items) == 0 {
			break
		}
		if len(items) >= limit {
			out.Truncated = true
			break
		}
		after = res.Items[len(res.Items)-1].SegmentRef
	}
	out.Items = make([]port.LayerSegment, 0, len(items))
	for _, sg := range items {
		line := make([]port.LatLng, 0, len(sg.Points))
		for _, pt := range sg.Points {
			line = append(line, port.LatLng{Lat: pt.Lat, Lng: pt.Lng})
		}
		out.Items = append(out.Items, port.LayerSegment{SegmentRef: sg.SegmentRef, Name: sg.Name, Class: sg.Class, MaxLoadTons: sg.MaxLoadTons,
			Certain: sg.Certain, Passable: sg.Passable, Line: line})
	}
	return out, nil
}
