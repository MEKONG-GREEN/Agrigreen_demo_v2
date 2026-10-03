// Package domain: quy tắc nghiệp vụ của pf/geo — đọc và kiểm hình ranh giới thửa
// (GeoJSON, KML, Shapefile), quy tắc tối thiểu 3 điểm, hệ toạ độ WGS84, nguồn và trạng thái
// thửa. Không phụ thuộc hạ tầng; kiểm hình học chính xác (ST_IsValid, diện tích
// trắc địa, chồng lấn) chạy trong PostGIS ở tầng store.
package domain

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// Position là một đỉnh [kinh độ, vĩ độ] theo WGS84 (thứ tự GeoJSON).
type Position [2]float64

// Ring là một vòng đỉnh; sau Normalize luôn khép kín (đỉnh cuối = đỉnh đầu).
type Ring []Position

// Polygon: vòng ngoài và các lỗ.
type Polygon struct {
	Outer Ring
	Holes []Ring
}

// MultiPolygon là hình ranh giới của một thửa (một hay nhiều mảnh).
type MultiPolygon []Polygon

// Giới hạn hình học.
const (
	// MinPoints: tối thiểu 3 điểm phân biệt không thẳng hàng (tam giác hợp lệ) — BRD v7.7 FN-XA-03.05, quyết định A2
	// chốt 03/10/2026 (trước đó 4 theo KN US-PLOT-01 AC-1, HTX US-PLOT-02 AC-2). DB: CHECK point_count >= 3 (pf 000084).
	MinPoints = 3
	// MaxPoints mỗi thửa — chặn dữ liệu bất thường, giữ truy vấn dưới 2 giây (R-52).
	MaxPoints = 5000
)

// Violation là lỗi nghiệp vụ có mã ổn định (docs/errors.md); tầng app quy về problem.
type Violation struct {
	Code string
	Msg  string
}

func (v *Violation) Error() string { return v.Code + ": " + v.Msg }

// Mã vi phạm hình học.
const (
	CodeTooFewPoints    = "geo.too_few_points"
	CodeInvalidGeometry = "geo.invalid_geometry"
	CodeCRSUnsupported  = "geo.crs_unsupported"
	CodeFormat          = "geo.format_invalid"
)

func violation(code, msg string) *Violation { return &Violation{Code: code, Msg: msg} }

// ErrTooFewPoints là thông báo của HTX US-PLOT-02 AC-2.
var ErrTooFewPoints = violation(CodeTooFewPoints, "Cần tối thiểu 3 điểm ranh giới không thẳng hàng để khép vùng.")

// ErrNotWGS84: toạ độ nằm ngoài miền kinh/vĩ độ — gần như chắc chắn là hệ chiếu (VN-2000, UTM).
var ErrNotWGS84 = violation(CodeCRSUnsupported, "Chỉ nhận toạ độ WGS84 (EPSG:4326) dạng kinh độ, vĩ độ; tệp có vẻ dùng hệ toạ độ phẳng (VN-2000/UTM) — hãy chuyển sang EPSG:4326 trước khi tải lên")

// AsViolation lấy Violation từ err.
func AsViolation(err error) (*Violation, bool) {
	var v *Violation
	ok := errors.As(err, &v)
	return v, ok
}

// ---------------------------------------------------------------------------
// GeoJSON
// ---------------------------------------------------------------------------

type rawGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
	CRS         json.RawMessage `json:"crs"`
}

// acceptedCRS là các tên hệ toạ độ tương đương WGS84 kinh/vĩ độ.
var acceptedCRS = []string{
	"urn:ogc:def:crs:ogc:1.3:crs84", "urn:ogc:def:crs:epsg::4326", "epsg:4326", "crs84",
	"http://www.opengis.net/def/crs/ogc/1.3/crs84", "http://www.opengis.net/def/crs/epsg/0/4326",
}

// CheckCRS kiểm thành phần "crs" (GeoJSON 2008) nếu có: chỉ nhận WGS84.
func CheckCRS(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil // RFC 7946: không có crs = WGS84
	}
	var c struct {
		Type       string `json:"type"`
		Properties struct {
			Name string `json:"name"`
			Href string `json:"href"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return violation(CodeCRSUnsupported, "thành phần crs không đọc được")
	}
	name := strings.ToLower(strings.TrimSpace(c.Properties.Name))
	for _, a := range acceptedCRS {
		if name == a {
			return nil
		}
	}
	shown := c.Properties.Name
	if shown == "" {
		shown = c.Properties.Href
	}
	return violation(CodeCRSUnsupported, "Hệ toạ độ "+shown+" không được hỗ trợ — chỉ nhận WGS84 (EPSG:4326)")
}

// ParseGeometry đọc hình GeoJSON Polygon hoặc MultiPolygon, kiểm và chuẩn hóa.
// Trả hình đã khép vòng và tổng số đỉnh phân biệt.
func ParseGeometry(raw json.RawMessage) (MultiPolygon, int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, 0, violation(CodeInvalidGeometry, "thiếu hình ranh giới")
	}
	var g rawGeometry
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, 0, violation(CodeInvalidGeometry, "hình phải là GeoJSON Polygon hoặc MultiPolygon")
	}
	if err := CheckCRS(g.CRS); err != nil {
		return nil, 0, err
	}
	var mp MultiPolygon
	switch g.Type {
	case "Polygon":
		var c [][][]float64
		if err := json.Unmarshal(g.Coordinates, &c); err != nil {
			return nil, 0, violation(CodeInvalidGeometry, "toạ độ Polygon không hợp lệ")
		}
		p, err := polygonFrom(c)
		if err != nil {
			return nil, 0, err
		}
		mp = MultiPolygon{p}
	case "MultiPolygon":
		var c [][][][]float64
		if err := json.Unmarshal(g.Coordinates, &c); err != nil {
			return nil, 0, violation(CodeInvalidGeometry, "toạ độ MultiPolygon không hợp lệ")
		}
		for _, pc := range c {
			p, err := polygonFrom(pc)
			if err != nil {
				return nil, 0, err
			}
			mp = append(mp, p)
		}
	default:
		return nil, 0, violation(CodeInvalidGeometry, "chỉ nhận vùng khép kín (Polygon/MultiPolygon), không nhận "+strconvQuote(g.Type))
	}
	n, err := mp.Normalize()
	return mp, n, err
}

func strconvQuote(s string) string {
	if s == "" {
		return "hình không có type"
	}
	return strconv.Quote(s)
}

func polygonFrom(c [][][]float64) (Polygon, error) {
	if len(c) == 0 {
		return Polygon{}, ErrTooFewPoints
	}
	var p Polygon
	for i, rc := range c {
		r := make(Ring, 0, len(rc))
		for _, pos := range rc {
			if len(pos) < 2 {
				return Polygon{}, violation(CodeInvalidGeometry, "mỗi đỉnh phải có kinh độ và vĩ độ")
			}
			r = append(r, Position{pos[0], pos[1]})
		}
		if i == 0 {
			p.Outer = r
		} else {
			p.Holes = append(p.Holes, r)
		}
	}
	return p, nil
}

// Normalize kiểm toạ độ, khép vòng và áp quy tắc tối thiểu 3 đỉnh phân biệt,
// không thẳng hàng cho vòng ngoài của mỗi mảnh. Trả tổng số đỉnh phân biệt.
func (mp MultiPolygon) Normalize() (int, error) {
	if len(mp) == 0 {
		return 0, ErrTooFewPoints
	}
	total := 0
	for i := range mp {
		p := &mp[i]
		n, err := checkRing(&p.Outer, MinPoints)
		if err != nil {
			return 0, err
		}
		if collinear(p.Outer) {
			return 0, ErrTooFewPoints
		}
		total += n
		for j := range p.Holes {
			hn, err := checkRing(&p.Holes[j], 3)
			if err != nil {
				return 0, violation(CodeInvalidGeometry, "lỗ trong thửa phải có ít nhất 3 đỉnh")
			}
			total += hn
		}
	}
	if total > MaxPoints {
		return 0, violation(CodeInvalidGeometry, "ranh giới có quá "+strconv.Itoa(MaxPoints)+" đỉnh")
	}
	return total, nil
}

// checkRing kiểm miền toạ độ, khép vòng, đếm đỉnh phân biệt.
func checkRing(r *Ring, minDistinct int) (int, error) {
	for _, p := range *r {
		if math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.IsInf(p[0], 0) || math.IsInf(p[1], 0) {
			return 0, violation(CodeInvalidGeometry, "toạ độ không phải số hữu hạn")
		}
		if p[0] < -180 || p[0] > 180 || p[1] < -90 || p[1] > 90 {
			return 0, ErrNotWGS84
		}
	}
	ring := *r
	if len(ring) > 0 && ring[0] != ring[len(ring)-1] {
		ring = append(ring, ring[0])
	}
	*r = ring
	seen := map[Position]bool{}
	for _, p := range ring {
		seen[p] = true
	}
	if len(seen) < minDistinct {
		if minDistinct == MinPoints {
			return 0, ErrTooFewPoints
		}
		return 0, violation(CodeInvalidGeometry, "vòng có quá ít đỉnh")
	}
	return len(seen), nil
}

// collinear: diện tích phẳng (shoelace) gần như bằng 0 so với khung bao — các
// điểm thẳng hàng hoặc trùng nhau. Hình tự cắt đối xứng (nơ bướm) cũng có diện
// tích 0 nhưng bị PostGIS ST_IsValid bắt ở tầng store với thông báo riêng, nên
// chỉ coi là thẳng hàng khi mọi đỉnh nằm trên một đường thẳng.
func collinear(r Ring) bool {
	if len(r) < 3 {
		return true
	}
	a := r[0]
	var b Position
	found := false
	for _, p := range r[1:] {
		if p != a {
			b, found = p, true
			break
		}
	}
	if !found {
		return true
	}
	dx, dy := b[0]-a[0], b[1]-a[1]
	norm := math.Hypot(dx, dy)
	for _, p := range r {
		// khoảng cách từ p tới đường ab (độ); 1e-9 độ ≈ 0,1 mm
		if math.Abs(dx*(p[1]-a[1])-dy*(p[0]-a[0]))/norm > 1e-9 {
			return false
		}
	}
	return true
}

// GeoJSON xuất hình dạng MultiPolygon (RFC 7946) để PostGIS đọc (ST_GeomFromGeoJSON).
func (mp MultiPolygon) GeoJSON() string {
	var b strings.Builder
	b.WriteString(`{"type":"MultiPolygon","coordinates":[`)
	for i, p := range mp {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		writeRing(&b, p.Outer)
		for _, h := range p.Holes {
			b.WriteByte(',')
			writeRing(&b, h)
		}
		b.WriteByte(']')
	}
	b.WriteString("]}")
	return b.String()
}

func writeRing(b *strings.Builder, r Ring) {
	b.WriteByte('[')
	for i, p := range r {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		b.WriteString(strconv.FormatFloat(p[0], 'f', -1, 64))
		b.WriteByte(',')
		b.WriteString(strconv.FormatFloat(p[1], 'f', -1, 64))
		b.WriteByte(']')
	}
	b.WriteByte(']')
}
