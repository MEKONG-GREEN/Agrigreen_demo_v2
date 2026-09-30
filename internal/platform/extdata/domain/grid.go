// Package domain: quy tắc làm thô tọa độ và giới hạn vùng của pf/extdata (ADR-0007).
package domain

import (
	"errors"
	"math"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/port"
)

// Lỗi miền.
var (
	ErrOutsideTerritory = errors.New("extdata: tọa độ ngoài vùng phục vụ")
	ErrZoomTooHigh      = errors.New("extdata: zoom vượt giới hạn của nguồn")
	ErrInvalidTile      = errors.New("extdata: tọa độ tile không hợp lệ")
	ErrInvalidCell      = errors.New("extdata: cạnh ô lưới ngoài khoảng cho phép")
)

// Vùng phục vụ (độ): lãnh thổ Việt Nam kể cả vùng biển, làm tròn ra khung chữ nhật.
// Proxy không phải proxy mở toàn cầu: điểm/tile ngoài khung bị từ chối.
var (
	TerritoryMinLat = decimal.NewFromInt(6)
	TerritoryMaxLat = decimal.NewFromInt(24)
	TerritoryMinLon = decimal.NewFromInt(102)
	TerritoryMaxLon = decimal.NewFromInt(118)
)

// Giới hạn cạnh ô lưới: nhỏ nhất 0,05° (≈ 5,5 km) để không lộ vị trí thửa; lớn nhất 1°.
var (
	MinCellDeg     = decimal.RequireFromString("0.05")
	MaxCellDeg     = decimal.NewFromInt(1)
	DefaultCellDeg = decimal.RequireFromString("0.1")
)

// MaxTileZoom là trần zoom tuyệt đối của tile qua proxy; mặc định mỗi nguồn là DefaultTileZoom.
const (
	MaxTileZoom     = 18
	DefaultTileZoom = 16
)

// ValidCellDeg kiểm cạnh ô lưới cấu hình.
func ValidCellDeg(size decimal.Decimal) error {
	if size.LessThan(MinCellDeg) || size.GreaterThan(MaxCellDeg) {
		return ErrInvalidCell
	}
	return nil
}

// InTerritory cho biết điểm nằm trong vùng phục vụ.
func InTerritory(lat, lon decimal.Decimal) bool {
	return !lat.LessThan(TerritoryMinLat) && !lat.GreaterThan(TerritoryMaxLat) &&
		!lon.LessThan(TerritoryMinLon) && !lon.GreaterThan(TerritoryMaxLon)
}

// Snap làm thô điểm về TÂM ô lưới cạnh size (ADR-0007). Mọi điểm trong cùng ô cho
// cùng kết quả — nhà cung cấp và bộ nhớ đệm chỉ thấy ô, không thấy điểm gốc.
func Snap(lat, lon, size decimal.Decimal) (port.Cell, error) {
	if err := ValidCellDeg(size); err != nil {
		return port.Cell{}, err
	}
	if !InTerritory(lat, lon) {
		return port.Cell{}, ErrOutsideTerritory
	}
	half := size.Div(decimal.NewFromInt(2))
	center := func(v decimal.Decimal) decimal.Decimal {
		return v.Div(size).Floor().Mul(size).Add(half)
	}
	return port.Cell{Lat: center(lat), Lon: center(lon), SizeDeg: size}, nil
}

// CheckTile kiểm tile: zoom trong [0, maxZoom], x/y trong lưới, và tile giao vùng phục vụ.
func CheckTile(z, x, y, maxZoom int) error {
	if z < 0 || x < 0 || y < 0 || z > MaxTileZoom {
		return ErrInvalidTile
	}
	n := 1 << z
	if x >= n || y >= n {
		return ErrInvalidTile
	}
	if z > maxZoom {
		return ErrZoomTooHigh
	}
	west, east := tileLon(x, n), tileLon(x+1, n)
	north, south := tileLat(y, n), tileLat(y+1, n)
	minLat, _ := TerritoryMinLat.Float64()
	maxLat, _ := TerritoryMaxLat.Float64()
	minLon, _ := TerritoryMinLon.Float64()
	maxLon, _ := TerritoryMaxLon.Float64()
	if east < minLon || west > maxLon || north < minLat || south > maxLat {
		return ErrOutsideTerritory
	}
	return nil
}

func tileLon(x, n int) float64 { return float64(x)/float64(n)*360 - 180 }

func tileLat(y, n int) float64 {
	return math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/float64(n)))) * 180 / math.Pi
}
