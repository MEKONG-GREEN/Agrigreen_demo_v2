package domain

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Phiên bản quy tắc ghim vào các sổ G2 (mục 8.1).
const (
	RuleCooperativeBoundary = "geo.cooperative_boundary.v1"
	RuleFacility            = "geo.facility.v1"
	RuleCapacity            = "geo.capacity.v1"
	RuleRoad                = "geo.road.v1"
	RuleGpsFix              = "geo.gps_fix.v1"
)

// Hành động trên các sổ phiên bản hình (ranh giới HTX, đường bộ).
const (
	ActionRecorded = "recorded"
	// ActionRetired, ActionRestored dùng chung với plot_event.
)

// Hành động trên sổ cơ sở.
const (
	ActionFacilityCreated = "created"
	ActionFacilityUpdated = "updated"
)

// Trạng thái đối tượng nền G2.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// Nguồn ranh giới HTX.
const (
	CoopSourceGeoWeb     = "geo_web"
	CoopSourceHTXApp     = "htx_app"
	CoopSourceImportFile = "import_file"
	CoopSourcePartySync  = "party_sync" // đồng bộ từ API HTX (US-BND-01, đợt sau)
)

// KnownCoopSource cho biết nguồn ranh giới HTX hợp lệ khi ghi qua API.
func KnownCoopSource(s string) bool {
	switch s {
	case CoopSourceGeoWeb, CoopSourceHTXApp, CoopSourceImportFile:
		return true
	}
	return false
}

// Giới hạn diện tích hợp lý của ranh giới HTX (ha).
var (
	MinCoopAreaHa = decimal.RequireFromString("0.01")
	MaxCoopAreaHa = decimal.RequireFromString("100000")
)

// StreamKey của các sổ G2.
func CoopStreamKey(ref uuid.UUID) string     { return "cooperative:" + ref.String() }
func FacilityStreamKey(ref uuid.UUID) string { return "facility:" + ref.String() }
func RoadStreamKey(ref uuid.UUID) string     { return "road:" + ref.String() }
func GpsStreamKey(actor uuid.UUID) string    { return "gps:" + actor.String() }

// CapacityStreamKey: chuỗi ảnh chụp sức chứa của một cơ sở/hub.
func CapacityStreamKey(kind string, ref uuid.UUID) string {
	return "capacity:" + kind + ":" + ref.String()
}

// ---------------------------------------------------------------------------
// Cơ sở (kho / Hub / nhà máy) — GIS US-BND-05/06/08
// ---------------------------------------------------------------------------

// Loại cơ sở.
const (
	FacilityKho    = "kho"
	FacilityHub    = "hub"
	FacilityNhaMay = "nha_may"
)

// KnownFacilityKind cho biết loại cơ sở hợp lệ.
func KnownFacilityKind(k string) bool {
	return k == FacilityKho || k == FacilityHub || k == FacilityNhaMay
}

// Đơn vị sức chứa (BRD FN-06: Tấn/M3/Kệ).
const (
	UnitTon = "tan"
	UnitM3  = "m3"
	UnitKe  = "ke"
)

// KnownUnit cho biết đơn vị sức chứa hợp lệ.
func KnownUnit(u string) bool { return u == UnitTon || u == UnitM3 || u == UnitKe }

// Thông báo của GIS US-BND-05/06 AC-2/AC-3.
const (
	MsgFacilityRequired = "Vui lòng nhập đầy đủ Tên Hub và Địa chỉ."
	MsgCapacityPositive = "Sức chứa phải là số lớn hơn 0."
	MsgCoopRequired     = "Vui lòng nhập đầy đủ Tên HTX và Địa chỉ." // GIS US-BND-02 AC-2 (HTX chọn từ sổ pf/party)
	MsgRoadRequired     = "Vui lòng nhập đầy đủ Tên tuyến và Phân loại."
	MsgRoadLoadPositive = "Tải trọng tối đa phải là số lớn hơn 0."
	MsgRoadNoneMatched  = "Không tìm thấy tuyến đường phù hợp với tải trọng phương tiện đã nhập."
	MsgGpsReadOnly      = "Dữ liệu log chỉ đọc, không thể chỉnh sửa hoặc xóa."
)

// ---------------------------------------------------------------------------
// Sức chứa (GIS US-WH-01, US-HUB-01, BR-50, BR-52)
// ---------------------------------------------------------------------------

// Loại đối tượng có sức chứa.
const (
	SubjectFacility = "facility" // pf_geo.facility của tenant
	SubjectHub      = "hub"      // hub MKG (pf_geo.hub_view)
)

// Nguồn ảnh chụp sức chứa.
const (
	CapacityManual = "manual" // Admin nhập form
	CapacityImport = "import" // Admin nhập tệp
	CapacityValve  = "valve"  // van công bố MKG
)

// Hành động trên sổ sức chứa.
const (
	CapacitySnapshot = "snapshot"
	CapacityRelease  = "release" // Admin xác nhận áp dụng số đồng bộ thay số nhập tay (BR-52)
)

// AdminEntered: nguồn do Admin nhập — không bị lần đồng bộ sau ghi đè (BR-52).
func AdminEntered(source string) bool { return source == CapacityManual || source == CapacityImport }

// Ngưỡng mặc định của widget (nguyên mẫu gis/service.ts DEFAULT_CAPACITY_THRESHOLDS).
var (
	DefaultHubWarnPct = decimal.NewFromInt(60)
	DefaultHubFullPct = decimal.NewFromInt(85)
)

// Lớp màu widget.
const (
	CapacityAvailable = "available" // còn nhiều chỗ
	CapacityNearFull  = "near_full" // sắp đầy
	CapacityFull      = "full"      // gần đầy / đầy
	CapacityUnknown   = "unknown"   // chưa có số liệu
)

// FillPct là used / capacity × 100, làm tròn 1 chữ số; 0 nếu capacity không dương.
func FillPct(used, capacity decimal.Decimal) decimal.Decimal {
	if !capacity.IsPositive() {
		return decimal.Zero
	}
	return used.Mul(decimal.NewFromInt(100)).Div(capacity).Round(1)
}

// CapacityClass xếp % lấp đầy vào lớp màu: < warn còn chỗ, < full sắp đầy, còn lại đầy.
func CapacityClass(pct, warn, full decimal.Decimal) string {
	switch {
	case pct.LessThan(warn):
		return CapacityAvailable
	case pct.LessThan(full):
		return CapacityNearFull
	}
	return CapacityFull
}

// ---------------------------------------------------------------------------
// Đường bộ — GIS US-ROAD-01/02/03, BR-29/30
// ---------------------------------------------------------------------------

// Phân loại tuyến đường bộ (BRD FN-08 B2).
const (
	RoadQuocLo  = "quoc_lo"
	RoadTinhLo  = "tinh_lo"
	RoadHuyenLo = "huyen_lo"
	RoadTrucXa  = "truc_xa"
	RoadBoRuong = "bo_ruong"
)

// KnownRoadClass cho biết phân loại hợp lệ.
func KnownRoadClass(c string) bool {
	switch c {
	case RoadQuocLo, RoadTinhLo, RoadHuyenLo, RoadTrucXa, RoadBoRuong:
		return true
	}
	return false
}

// MaxLinePoints: số đỉnh tối đa của một tuyến.
const MaxLinePoints = 10000

// ---------------------------------------------------------------------------
// Log GPS — HTX US-GPS-03
// ---------------------------------------------------------------------------

// KnownGpsPurpose cho biết mục đích hợp lệ.
func KnownGpsPurpose(p string) bool {
	switch p {
	case "season_open", "farm_log", "harvest", "support", "plot":
		return true
	}
	return false
}

// KnownGpsSource cho biết nguồn toạ độ hợp lệ.
func KnownGpsSource(s string) bool { return s == "device" || s == "photo_exif" || s == "manual_pick" }

// ---------------------------------------------------------------------------
// Điểm và đường (GeoJSON Point / LineString, WGS84)
// ---------------------------------------------------------------------------

func checkPosition(p Position) error {
	if math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.IsInf(p[0], 0) || math.IsInf(p[1], 0) {
		return violation(CodeInvalidGeometry, "toạ độ không phải số hữu hạn")
	}
	if p[0] < -180 || p[0] > 180 || p[1] < -90 || p[1] > 90 {
		return ErrNotWGS84
	}
	return nil
}

// ParsePoint đọc GeoJSON Point (WGS84, kiểm crs nếu có).
func ParsePoint(raw json.RawMessage) (*Position, error) {
	var g rawGeometry
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &g) != nil {
		return nil, violation(CodeInvalidGeometry, "vị trí phải là GeoJSON Point")
	}
	if err := CheckCRS(g.CRS); err != nil {
		return nil, err
	}
	if g.Type != "Point" {
		return nil, violation(CodeInvalidGeometry, "vị trí phải là GeoJSON Point, không nhận "+strconvQuote(g.Type))
	}
	var c []float64
	if err := json.Unmarshal(g.Coordinates, &c); err != nil || len(c) < 2 {
		return nil, violation(CodeInvalidGeometry, "toạ độ Point phải là [kinh độ, vĩ độ]")
	}
	p := Position{c[0], c[1]}
	if err := checkPosition(p); err != nil {
		return nil, err
	}
	return &p, nil
}

// NewPosition dựng điểm từ kinh/vĩ độ decimal và kiểm miền WGS84.
func NewPosition(lon, lat decimal.Decimal) (Position, error) {
	p := Position{lon.InexactFloat64(), lat.InexactFloat64()}
	return p, checkPosition(p)
}

// PointGeoJSON xuất điểm dạng GeoJSON cho PostGIS.
func PointGeoJSON(p Position) string {
	return `{"type":"Point","coordinates":[` + strconv.FormatFloat(p[0], 'f', -1, 64) + "," +
		strconv.FormatFloat(p[1], 'f', -1, 64) + "]}"
}

// LineString là tuyến (đường bộ).
type LineString []Position

// ParseLineString đọc GeoJSON LineString WGS84, bỏ đỉnh trùng liên tiếp; cần ≥ 2 đỉnh phân biệt.
func ParseLineString(raw json.RawMessage) (LineString, error) {
	var g rawGeometry
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &g) != nil {
		return nil, violation(CodeInvalidGeometry, "tuyến phải là GeoJSON LineString")
	}
	if err := CheckCRS(g.CRS); err != nil {
		return nil, err
	}
	if g.Type != "LineString" {
		return nil, violation(CodeInvalidGeometry, "tuyến phải là GeoJSON LineString, không nhận "+strconvQuote(g.Type))
	}
	var c [][]float64
	if err := json.Unmarshal(g.Coordinates, &c); err != nil {
		return nil, violation(CodeInvalidGeometry, "toạ độ LineString không hợp lệ")
	}
	var ls LineString
	for _, pos := range c {
		if len(pos) < 2 {
			return nil, violation(CodeInvalidGeometry, "mỗi đỉnh phải có kinh độ và vĩ độ")
		}
		p := Position{pos[0], pos[1]}
		if err := checkPosition(p); err != nil {
			return nil, err
		}
		if len(ls) > 0 && ls[len(ls)-1] == p {
			continue
		}
		ls = append(ls, p)
	}
	if len(ls) < 2 {
		return nil, violation(CodeInvalidGeometry, "tuyến cần ít nhất 2 điểm phân biệt")
	}
	if len(ls) > MaxLinePoints {
		return nil, violation(CodeInvalidGeometry, "tuyến có quá "+strconv.Itoa(MaxLinePoints)+" đỉnh")
	}
	return ls, nil
}

// GeoJSON xuất tuyến dạng GeoJSON LineString.
func (ls LineString) GeoJSON() string {
	var b strings.Builder
	b.WriteString(`{"type":"LineString","coordinates":`)
	writeRing(&b, Ring(ls))
	b.WriteString("}")
	return b.String()
}
