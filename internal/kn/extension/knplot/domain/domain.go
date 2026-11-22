// Package domain: quy tắc thửa ruộng của KN (Trồng trọt FN-XA-03.05 "Vẽ ranh giới thửa ruộng") — KN-STANDALONE-5-TT-DATA
// (#271, quyết định D1): thửa ở kho KN (pf_extension.kn_plot), không còn ở module HTX. Giữ đúng hành vi FN đã có: hình
// GeoJSON Polygon/MultiPolygon WGS84, tối thiểu 3 điểm không thẳng hàng (quyết định A2), diện tích tự tính trong PostGIS,
// chặn mọi chồng lấn vượt ngưỡng; mã lỗi geo.* giữ nguyên để web, field-app không đổi.
package domain

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// RuleVersion ghi vào sổ kn_plot_version.
const RuleVersion = "kn-plot-1"

// Sự việc trên sổ (ảnh chụp đủ thửa sau sự việc).
const (
	EventMigrated          = "migrated"
	EventCreated           = "created"
	EventBoundaryCorrected = "boundary_corrected"
	EventAttributesChanged = "attributes_changed"
)

// Nguồn ghi.
const (
	SourceWeb      = "kn_web"
	SourceFieldApp = "kn_field_app"
)

// Cách vẽ: chấm điểm, hình tròn, hình vuông (máy đổi tròn/vuông thành đa giác trước khi gửi).
const (
	ShapePolygon = "polygon"
	ShapeCircle  = "circle"
	ShapeSquare  = "square"
)

// KnownShape cho biết cách vẽ hợp lệ.
func KnownShape(s string) bool { return s == ShapePolygon || s == ShapeCircle || s == ShapeSquare }

// LegacyDepartment: thửa không mang phòng ban (có trước v7.7) đọc là Phòng Trồng trọt (X-12).
const LegacyDepartment = "trong_trot"

// Giới hạn.
const (
	MaxLotName    = 200
	MaxNote       = 1000
	MaxFarmerName = 200
	MaxReason     = 500
	MinReason     = 10
	MinPoints     = 3
	MaxPoints     = 5000
)

// Ngưỡng diện tích hợp lý và ngưỡng chồng lấn mặc định (tham số geo.overlap_tolerance_pct).
var (
	MinAreaHa                  = decimal.RequireFromString("0.001")
	MaxAreaHa                  = decimal.RequireFromString("1000")
	DefaultOverlapTolerancePct = decimal.NewFromInt(1)
)

// ParamOverlapTolerancePct là khóa tham số ngưỡng chồng lấn (pf/masterdata).
const ParamOverlapTolerancePct = "geo.overlap_tolerance_pct"

// Mã vi phạm hình học (giữ mã geo.* của FN-XA-03.05).
const (
	CodeTooFewPoints    = "geo.too_few_points"
	CodeInvalidGeometry = "geo.invalid_geometry"
	CodeCRSUnsupported  = "geo.crs_unsupported"
)

// Violation là lỗi hình học có mã ổn định.
type Violation struct{ Code, Msg string }

func (v *Violation) Error() string { return v.Code + ": " + v.Msg }

func bad(code, msg string) *Violation { return &Violation{Code: code, Msg: msg} }

var errTooFew = bad(CodeTooFewPoints, "Cần tối thiểu 3 điểm ranh giới không thẳng hàng để khép vùng.")

// Shape là hình đã kiểm, dạng GeoJSON MultiPolygon cho PostGIS.
type Shape struct {
	GeoJSON string
	Points  int
}

type position [2]float64

// ParseGeometry đọc GeoJSON Polygon/MultiPolygon WGS84, khép vòng, kiểm ≥ 3 đỉnh không thẳng hàng.
func ParseGeometry(raw json.RawMessage) (Shape, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Shape{}, bad(CodeInvalidGeometry, "thiếu hình ranh giới")
	}
	var g struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
		CRS         json.RawMessage `json:"crs"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return Shape{}, bad(CodeInvalidGeometry, "hình phải là GeoJSON Polygon hoặc MultiPolygon")
	}
	if !wgs84(g.CRS) {
		return Shape{}, bad(CodeCRSUnsupported, "Hệ toạ độ không được hỗ trợ — chỉ nhận WGS84 (EPSG:4326)")
	}
	var polys [][][][]float64
	switch g.Type {
	case "Polygon":
		var c [][][]float64
		if err := json.Unmarshal(g.Coordinates, &c); err != nil {
			return Shape{}, bad(CodeInvalidGeometry, "toạ độ Polygon không hợp lệ")
		}
		polys = [][][][]float64{c}
	case "MultiPolygon":
		if err := json.Unmarshal(g.Coordinates, &polys); err != nil {
			return Shape{}, bad(CodeInvalidGeometry, "toạ độ MultiPolygon không hợp lệ")
		}
	default:
		t := strconv.Quote(g.Type)
		if g.Type == "" {
			t = "hình không có type"
		}
		return Shape{}, bad(CodeInvalidGeometry, "chỉ nhận vùng khép kín (Polygon/MultiPolygon), không nhận "+t)
	}
	if len(polys) == 0 {
		return Shape{}, errTooFew
	}
	var b strings.Builder
	b.WriteString(`{"type":"MultiPolygon","coordinates":[`)
	total := 0
	for i, p := range polys {
		if len(p) == 0 {
			return Shape{}, errTooFew
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		for j, rc := range p {
			ring, n, err := ring(rc, j == 0)
			if err != nil {
				return Shape{}, err
			}
			total += n
			if j > 0 {
				b.WriteByte(',')
			}
			writeRing(&b, ring)
		}
		b.WriteByte(']')
	}
	b.WriteString("]}")
	if total > MaxPoints {
		return Shape{}, bad(CodeInvalidGeometry, "ranh giới có quá "+strconv.Itoa(MaxPoints)+" đỉnh")
	}
	return Shape{GeoJSON: b.String(), Points: total}, nil
}

// wgs84 kiểm thành phần "crs" (GeoJSON 2008) nếu có: chỉ tên tương đương WGS84 kinh/vĩ độ.
func wgs84(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var c struct {
		Properties struct {
			Name string `json:"name"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(c.Properties.Name)) {
	case "urn:ogc:def:crs:ogc:1.3:crs84", "urn:ogc:def:crs:epsg::4326", "epsg:4326", "crs84",
		"http://www.opengis.net/def/crs/ogc/1.3/crs84", "http://www.opengis.net/def/crs/epsg/0/4326":
		return true
	}
	return false
}

func ring(rc [][]float64, outer bool) ([]position, int, error) {
	r := make([]position, 0, len(rc)+1)
	for _, pos := range rc {
		if len(pos) < 2 {
			return nil, 0, bad(CodeInvalidGeometry, "mỗi đỉnh phải có kinh độ và vĩ độ")
		}
		x, y := pos[0], pos[1]
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return nil, 0, bad(CodeInvalidGeometry, "toạ độ không phải số hữu hạn")
		}
		if x < -180 || x > 180 || y < -90 || y > 90 {
			return nil, 0, bad(CodeCRSUnsupported, "Chỉ nhận toạ độ WGS84 (EPSG:4326) dạng kinh độ, vĩ độ")
		}
		r = append(r, position{x, y})
	}
	if len(r) > 0 && r[0] != r[len(r)-1] {
		r = append(r, r[0])
	}
	seen := map[position]bool{}
	for _, p := range r {
		seen[p] = true
	}
	if len(seen) < MinPoints || (outer && collinear(r)) {
		if outer {
			return nil, 0, errTooFew
		}
		return nil, 0, bad(CodeInvalidGeometry, "lỗ trong thửa phải có ít nhất 3 đỉnh")
	}
	return r, len(seen), nil
}

// collinear: mọi đỉnh nằm trên một đường thẳng (1e-9 độ ≈ 0,1 mm).
func collinear(r []position) bool {
	a := r[0]
	for _, b := range r[1:] {
		if b == a {
			continue
		}
		dx, dy := b[0]-a[0], b[1]-a[1]
		norm := math.Hypot(dx, dy)
		for _, p := range r {
			if math.Abs(dx*(p[1]-a[1])-dy*(p[0]-a[0]))/norm > 1e-9 {
				return false
			}
		}
		return true
	}
	return true
}

func writeRing(b *strings.Builder, r []position) {
	b.WriteByte('[')
	for i, p := range r {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("[" + strconv.FormatFloat(p[0], 'f', -1, 64) + "," + strconv.FormatFloat(p[1], 'f', -1, 64) + "]")
	}
	b.WriteByte(']')
}

// Dấu hiệu dữ liệu cá nhân trong văn bản tự do: SĐT Việt Nam, CCCD 12 chữ số.
var (
	phoneRe      = regexp.MustCompile(`(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b`)
	nationalIDRe = regexp.MustCompile(`\b\d{12}\b`)
)

// ContainsPII phát hiện SĐT/CCCD (R-04: dữ liệu cá nhân chỉ ở kho định danh).
func ContainsPII(s string) bool { return phoneRe.MatchString(s) || nationalIDRe.MatchString(s) }

// Text kiểm một trường văn bản tự do; trả giá trị đã cắt khoảng trắng hoặc lý do lỗi.
func Text(v string, max int, required bool) (string, string) {
	v = strings.TrimSpace(v)
	switch {
	case required && v == "":
		return "", "bắt buộc"
	case utf8.RuneCountInString(v) > max:
		return "", "quá dài"
	case ContainsPII(v):
		return "", "không ghi SĐT hoặc số CCCD vào trường này"
	}
	return v, ""
}

// AreaDeviationPct là |đo − khai báo| / khai báo × 100.
func AreaDeviationPct(measured, declared decimal.Decimal) decimal.Decimal {
	if !declared.IsPositive() {
		return decimal.Zero
	}
	return measured.Sub(declared).Abs().Mul(decimal.NewFromInt(100)).Div(declared).Round(4)
}

// OverlapPct là phần giao / diện tích nhỏ hơn của hai thửa × 100.
func OverlapPct(intersect, a, b decimal.Decimal) decimal.Decimal {
	base := decimal.Min(a, b)
	if !base.IsPositive() {
		return decimal.Zero
	}
	return intersect.Mul(decimal.NewFromInt(100)).Div(base).Round(4)
}

// StreamKey của thửa trên sổ kn_plot_version.
func StreamKey(ref string) string { return "kn_plot:" + ref }

var declaredRe = regexp.MustCompile(`^[0-9]{1,7}(\.[0-9]{1,4})?$`)

// ParseDeclared đọc diện tích khai báo (ha) từ JSON (chuỗi hoặc số): kiểm cú pháp thập phân thường (không số mũ, không
// dấu) và độ dài TRƯỚC khi tạo decimal (1e100000, chuỗi rất dài); phải dương. Vắng/null → nil.
func ParseDeclared(raw json.RawMessage) (*decimal.Decimal, string) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil, ""
	}
	if len(s) > 32 {
		return nil, "phải là số thập phân dương, tối đa 7 chữ số phần nguyên và 4 chữ số thập phân"
	}
	s = strings.TrimSpace(strings.Trim(s, `"`))
	if !declaredRe.MatchString(s) {
		return nil, "phải là số thập phân dương, tối đa 7 chữ số phần nguyên và 4 chữ số thập phân"
	}
	d, err := decimal.NewFromString(s)
	if err != nil || !d.IsPositive() {
		return nil, "phải dương"
	}
	return &d, ""
}
