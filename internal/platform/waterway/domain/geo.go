package domain

import (
	"math"
)

// EarthRadiusM là bán kính Trái Đất trung bình (IUGG) dùng cho Haversine và phép chiếu cục bộ.
const EarthRadiusM = 6371008.8

// HaversineM là khoảng cách vòng lớn giữa hai điểm (m).
func HaversineM(a, b Point) float64 {
	rad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLng := (b.Lng - a.Lng) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * EarthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Round7 làm tròn tọa độ tới 1e-7 độ (~1 cm) — mọi tọa độ đầu ra của bộ dựng đi
// qua đây để nội dung lưu và băm đồ thị trùng nhau.
func Round7(v float64) float64 { return math.Round(v*1e7) / 1e7 }

// projection là phép chiếu phẳng cục bộ (equirectangular quanh tâm dữ liệu) —
// ở quy mô ngưỡng vài chục đến vài trăm mét sai số dưới 1 %.
type projection struct {
	lat0, lng0 float64
	kx, ky     float64
}

func newProjection(center Point) projection {
	ky := EarthRadiusM * math.Pi / 180
	return projection{lat0: center.Lat, lng0: center.Lng, ky: ky, kx: ky * math.Cos(center.Lat*math.Pi/180)}
}

func (p projection) xy(pt Point) (float64, float64) {
	return (pt.Lng - p.lng0) * p.kx, (pt.Lat - p.lat0) * p.ky
}

func (p projection) inv(x, y float64) Point {
	return Point{Lng: p.lng0 + x/p.kx, Lat: p.lat0 + y/p.ky}
}

// projectOnPiece chiếu điểm (px, py) lên đoạn thẳng A→B: khoảng cách, tham số t ∈ [0,1], điểm chiếu.
func projectOnPiece(px, py, ax, ay, bx, by float64) (d, t, cx, cy float64) {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 > 0 {
		t = ((px-ax)*dx + (py-ay)*dy) / l2
		t = math.Max(0, math.Min(1, t))
	}
	cx, cy = ax+t*dx, ay+t*dy
	return math.Hypot(px-cx, py-cy), t, cx, cy
}

// intersect trả tham số (t, u) giao điểm của A0→A1 và B0→B1 nếu có (không song song).
func intersect(a0x, a0y, a1x, a1y, b0x, b0y, b1x, b1y float64) (t, u float64, ok bool) {
	rx, ry := a1x-a0x, a1y-a0y
	sx, sy := b1x-b0x, b1y-b0y
	den := rx*sy - ry*sx
	if math.Abs(den) < 1e-9 {
		return 0, 0, false
	}
	qx, qy := b0x-a0x, b0y-a0y
	t = (qx*sy - qy*sx) / den
	u = (qx*ry - qy*rx) / den
	const eps = 1e-12
	if t < -eps || t > 1+eps || u < -eps || u > 1+eps {
		return 0, 0, false
	}
	return math.Max(0, math.Min(1, t)), math.Max(0, math.Min(1, u)), true
}

// Substring cắt một polyline theo tỷ lệ chiều dài PHẲNG trên tọa độ độ (cùng
// thước đo với ST_LineLocatePoint trên geometry 4326). from > to → đảo chiều.
func Substring(pts []Point, from, to float64) []Point {
	if len(pts) < 2 {
		return append([]Point(nil), pts...)
	}
	rev := from > to
	if rev {
		from, to = to, from
	}
	from = math.Max(0, math.Min(1, from))
	to = math.Max(0, math.Min(1, to))
	cum := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		cum[i] = cum[i-1] + math.Hypot(pts[i].Lng-pts[i-1].Lng, pts[i].Lat-pts[i-1].Lat)
	}
	total := cum[len(pts)-1]
	at := func(f float64) (Point, int) {
		target := f * total
		for i := 1; i < len(pts); i++ {
			if cum[i] >= target {
				seg := cum[i] - cum[i-1]
				r := 0.0
				if seg > 0 {
					r = (target - cum[i-1]) / seg
				}
				return Point{Lng: pts[i-1].Lng + r*(pts[i].Lng-pts[i-1].Lng), Lat: pts[i-1].Lat + r*(pts[i].Lat-pts[i-1].Lat)}, i
			}
		}
		return pts[len(pts)-1], len(pts) - 1
	}
	p0, i0 := at(from)
	p1, i1 := at(to)
	out := []Point{p0}
	for i := i0; i < i1; i++ {
		if pts[i] != out[len(out)-1] {
			out = append(out, pts[i])
		}
	}
	if p1 != out[len(out)-1] || len(out) == 1 {
		out = append(out, p1)
	}
	if rev {
		for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
			out[l], out[r] = out[r], out[l]
		}
	}
	return out
}

// Reverse trả polyline đảo chiều (bản sao).
func Reverse(pts []Point) []Point {
	out := make([]Point, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}

// AppendPath nối polyline b vào a, bỏ điểm đầu của b nếu trùng điểm cuối của a.
func AppendPath(a, b []Point) []Point {
	for _, p := range b {
		if len(a) > 0 && a[len(a)-1] == p {
			continue
		}
		a = append(a, p)
	}
	return a
}
