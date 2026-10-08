package domain

import (
	"encoding/json"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleCooperativeBoundary: phiên bản quy tắc ghim vào sổ cooperative_boundary (mục 8.1).
const RuleCooperativeBoundary = "geo.cooperative_boundary.v1"

// ActionRecorded: hành động trên sổ phiên bản ranh giới HTX (ActionRetired, ActionRestored dùng chung với plot_event).
const ActionRecorded = "recorded"

// Trạng thái ranh giới HTX (cooperative_area).
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

// CoopStreamKey là stream_key của sổ cooperative_boundary.
func CoopStreamKey(ref uuid.UUID) string { return "cooperative:" + ref.String() }

// MsgCoopRequired: GIS US-BND-02 AC-2 (HTX chọn từ sổ hồ sơ HTX).
const MsgCoopRequired = "Vui lòng nhập đầy đủ Tên HTX và Địa chỉ."

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
