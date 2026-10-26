// Package domain: quy tắc thuần của bản đồ vùng nuôi, vùng dịch CNTS — W11.52, D4 CN12, BRD CNTS v3.7 FN-18, UR-TINH-17.
//
// Một VÙNG là chuỗi phiên bản chỉ ghi thêm (pf_extension.cnts_map_zone): mỗi lần sửa hình, đổi thuộc tính, ngưng hay khôi
// phục là một phiên bản mới có lý do và NGÀY ÁP DỤNG (effective_on). Bản đồ của ngày D lấy phiên bản mới nhất có
// effective_on <= D (FN-18 BR-01, AC-03).
package domain

import (
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ.
const RuleVersion = "cnts-map/1"

// Loại vùng (UR-TINH-17: vùng chăn nuôi, vùng nuôi thủy sản, vùng dịch, vùng đệm, vùng bị uy hiếp, hướng di chuyển).
const (
	KindLivestock   = "vung_chan_nuoi"
	KindAquaculture = "vung_thuy_san"
	KindOutbreak    = "vung_dich"
	KindThreatened  = "vung_uy_hiep"
	KindBuffer      = "vung_dem"
	KindDirection   = "huong_di_chuyen"
)

// Kinds là mọi loại vùng theo thứ tự hiển thị.
var Kinds = []string{KindLivestock, KindAquaculture, KindOutbreak, KindThreatened, KindBuffer, KindDirection}

// KindLabel là nhãn tiếng Việt của loại vùng.
var KindLabel = map[string]string{
	KindLivestock: "Vùng chăn nuôi", KindAquaculture: "Vùng nuôi thủy sản", KindOutbreak: "Vùng dịch",
	KindThreatened: "Vùng bị uy hiếp", KindBuffer: "Vùng đệm", KindDirection: "Hướng di chuyển",
}

// EpidemicKind: loại vùng gắn được ổ dịch BM-10.
func EpidemicKind(k string) bool {
	return k == KindOutbreak || k == KindThreatened || k == KindBuffer || k == KindDirection
}

// Cách vẽ.
const (
	ModePolygon = "polygon" // vẽ tay
	ModeRadius  = "radius"  // theo bán kính quanh một tâm
	ModeLine    = "line"    // hướng di chuyển
)

// Sự việc của phiên bản.
const (
	EventCreated  = "created"
	EventUpdated  = "updated"
	EventRetired  = "retired"
	EventRestored = "restored"
)

// Giới hạn.
const (
	MinRadiusM  = 10
	MaxRadiusM  = 50000
	MaxVertices = 5000
	MaxCommunes = 200
	MinReason   = 5
	MaxText     = 1000
	MaxName     = 300
)

// DefaultRadii là bán kính mặc định khi khoanh vùng từ một ổ dịch (giả định W11.52: ổ dịch 500 m, vùng bị uy hiếp 3 km,
// vùng đệm 10 km — đổi theo từng lần khoanh).
var DefaultRadii = map[string]float64{KindOutbreak: 500, KindThreatened: 3000, KindBuffer: 10000}

// FieldErr là một lỗi trường.
type FieldErr struct {
	Field  string
	Reason string
}

// Errors là danh sách lỗi trường (422).
type Errors []FieldErr

func (e Errors) Error() string {
	parts := make([]string, 0, len(e))
	for _, x := range e {
		parts = append(parts, x.Field+": "+x.Reason)
	}
	return strings.Join(parts, "; ")
}

func (e *Errors) add(field, reason string) { *e = append(*e, FieldErr{Field: field, Reason: reason}) }

var (
	unitRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,20}$`)
	codeRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// ValidUnitCode kiểm dạng mã đơn vị hành chính.
func ValidUnitCode(s string) bool { return unitRe.MatchString(s) }

// ValidReason kiểm lý do sửa/ngưng/khôi phục.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= MinReason && n <= MaxText
}

// Point là tọa độ WGS84.
type Point struct{ Lat, Lng float64 }

// InVietnam: trong khung lãnh thổ Việt Nam mở rộng (như tọa độ điểm/hộ W11.47).
func (p Point) InVietnam() bool { return p.Lat >= 5 && p.Lat <= 25 && p.Lng >= 100 && p.Lng <= 118 }

// Zone là phần do người dùng nhập của một phiên bản vùng.
type Zone struct {
	Kind        string
	Name        string
	Communes    []string
	Mode        string
	Geometry    json.RawMessage // GeoJSON Polygon | MultiPolygon | LineString (ModePolygon, ModeLine)
	Center      *Point          // ModeRadius
	RadiusM     *float64        // ModeRadius
	Species     string
	Disease     string
	EffectiveOn *time.Time
	Note        string
}

// Normalize chuẩn hóa và kiểm trường (không kiểm danh mục, phạm vi — ở app).
func (z *Zone) Normalize() Errors {
	var e Errors
	z.Kind, z.Name, z.Mode = strings.TrimSpace(z.Kind), strings.TrimSpace(z.Name), strings.TrimSpace(z.Mode)
	z.Species, z.Disease, z.Note = strings.TrimSpace(z.Species), strings.TrimSpace(z.Disease), strings.TrimSpace(z.Note)
	if !slices.Contains(Kinds, z.Kind) {
		e.add("kind", "một trong "+strings.Join(Kinds, ", "))
	}
	if n := utf8.RuneCountInString(z.Name); n < 1 || n > MaxName {
		e.add("name", "1–"+strconv.Itoa(MaxName)+" ký tự")
	}
	if utf8.RuneCountInString(z.Note) > MaxText {
		e.add("note", "tối đa "+strconv.Itoa(MaxText)+" ký tự")
	}
	z.Communes = uniqStrings(z.Communes)
	switch {
	case len(z.Communes) == 0:
		e.add("communes", "chọn ít nhất một xã")
	case len(z.Communes) > MaxCommunes:
		e.add("communes", "tối đa "+strconv.Itoa(MaxCommunes)+" xã")
	}
	for _, c := range z.Communes {
		if !ValidUnitCode(c) {
			e.add("communes", "mã xã không hợp lệ: "+c)
			break
		}
	}
	if z.Species != "" && !codeRe.MatchString(z.Species) {
		e.add("species_code", "mã danh mục không hợp lệ")
	}
	if z.Disease != "" && !codeRe.MatchString(z.Disease) {
		e.add("disease_code", "mã danh mục không hợp lệ")
	}
	if z.Kind == KindDirection && z.Mode == "" {
		z.Mode = ModeLine
	}
	switch z.Mode {
	case ModeLine:
		if z.Kind != KindDirection {
			e.add("draw_mode", "chỉ hướng di chuyển vẽ bằng đường")
		}
		e = append(e, z.checkGeometry("geometry", "LineString")...)
	case ModePolygon:
		if z.Kind == KindDirection {
			e.add("draw_mode", "hướng di chuyển vẽ bằng đường (line)")
		}
		e = append(e, z.checkGeometry("geometry", "Polygon", "MultiPolygon")...)
	case ModeRadius:
		if z.Kind == KindDirection {
			e.add("draw_mode", "hướng di chuyển vẽ bằng đường (line)")
		}
		if len(z.Geometry) > 0 && string(z.Geometry) != "null" {
			e.add("geometry", "vùng theo bán kính chỉ gửi center và radius_m")
		}
		switch {
		case z.Center == nil:
			e.add("center", "bắt buộc với vùng theo bán kính")
		case !z.Center.InVietnam():
			e.add("center", "tọa độ ngoài phạm vi Việt Nam")
		}
		switch {
		case z.RadiusM == nil:
			e.add("radius_m", "bắt buộc với vùng theo bán kính")
		case math.IsNaN(*z.RadiusM) || *z.RadiusM < MinRadiusM || *z.RadiusM > MaxRadiusM:
			e.add("radius_m", "từ "+strconv.Itoa(MinRadiusM)+" đến "+strconv.Itoa(MaxRadiusM)+" m")
		default:
			r := math.Round(*z.RadiusM*10) / 10
			z.RadiusM = &r
		}
	default:
		e.add("draw_mode", "polygon, radius hoặc line")
	}
	if z.Mode != ModeRadius {
		z.Center, z.RadiusM = nil, nil
	}
	return e
}

type geoJSON struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// checkGeometry kiểm GeoJSON: loại, số đỉnh, vòng khép kín, tọa độ trong Việt Nam. Tính hợp lệ topo (tự cắt…) kiểm ở
// PostGIS (ST_IsValid) trước khi ghi.
func (z *Zone) checkGeometry(field string, types ...string) Errors {
	var e Errors
	if len(z.Geometry) == 0 || string(z.Geometry) == "null" {
		e.add(field, "bắt buộc (GeoJSON "+strings.Join(types, "/")+")")
		return e
	}
	var g geoJSON
	if err := json.Unmarshal(z.Geometry, &g); err != nil || !slices.Contains(types, g.Type) {
		e.add(field, "phải là GeoJSON "+strings.Join(types, "/"))
		return e
	}
	var rings [][][]float64
	switch g.Type {
	case "LineString":
		var line [][]float64
		if err := json.Unmarshal(g.Coordinates, &line); err != nil || len(line) < 2 {
			e.add(field, "đường cần ít nhất 2 điểm")
			return e
		}
		rings = [][][]float64{line}
	case "Polygon":
		if err := json.Unmarshal(g.Coordinates, &rings); err != nil || len(rings) == 0 {
			e.add(field, "đa giác không hợp lệ")
			return e
		}
	case "MultiPolygon":
		var polys [][][][]float64
		if err := json.Unmarshal(g.Coordinates, &polys); err != nil || len(polys) == 0 {
			e.add(field, "đa giác không hợp lệ")
			return e
		}
		for _, p := range polys {
			if len(p) == 0 {
				e.add(field, "đa giác rỗng")
				return e
			}
			rings = append(rings, p...)
		}
	}
	n := 0
	for _, r := range rings {
		if g.Type != "LineString" {
			if len(r) < 4 {
				e.add(field, "mỗi vòng cần ít nhất 3 đỉnh và khép kín")
				return e
			}
			if !samePos(r[0], r[len(r)-1]) {
				e.add(field, "vòng đa giác phải khép kín (điểm đầu = điểm cuối)")
				return e
			}
		}
		for _, p := range r {
			if len(p) < 2 || math.IsNaN(p[0]) || math.IsNaN(p[1]) || !(Point{Lat: p[1], Lng: p[0]}).InVietnam() {
				e.add(field, "tọa độ ngoài phạm vi Việt Nam (kinh độ, vĩ độ WGS84)")
				return e
			}
		}
		n += len(r)
	}
	if n > MaxVertices {
		e.add(field, "tối đa "+strconv.Itoa(MaxVertices)+" đỉnh")
	}
	return e
}

func samePos(a, b []float64) bool { return len(a) >= 2 && len(b) >= 2 && a[0] == b[0] && a[1] == b[1] }

func uniqStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// Status trả trạng thái hiển thị của phiên bản: retired → "retired", còn lại "active".
func Status(event string) string {
	if event == EventRetired {
		return "retired"
	}
	return "active"
}
