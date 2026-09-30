// Package port là phần công khai của pf/extdata — proxy khử định danh cho nguồn dữ
// liệu ngoài (thời tiết, độ mặn, tile bản đồ/vệ tinh) theo ADR-0007 và RB-7.
//
// Nhà cung cấp (Provider) CHỈ nhận ô lưới thô (PointQuery.Cell) hoặc tọa độ tile đã
// giới hạn zoom (TileQuery) — không bao giờ nhận người gọi, token, tenant, thiết bị,
// mã thửa/nông hộ hay tọa độ gốc. Context truyền cho Provider đã bị gỡ mọi giá trị
// (chỉ giữ hạn chờ/hủy), nên Provider không đọc được Principal hay token.
package port

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

// Kind là loại nguồn.
type Kind string

// Loại nguồn được hỗ trợ.
const (
	KindWeather  Kind = "weather"  // thời tiết (HTX US-WEATHER-01, GIS US-WX-01/02)
	KindSalinity Kind = "salinity" // độ mặn (GIS US-NAT-01)
	KindTile     Kind = "tile"     // tile bản đồ nền / ảnh vệ tinh (HTX US-PLOT-03, GIS FN-07)
)

// Valid cho biết k là loại đã biết.
func (k Kind) Valid() bool { return k == KindWeather || k == KindSalinity || k == KindTile }

// Point cho biết loại nguồn trả dữ liệu theo điểm (ô lưới).
func (k Kind) Point() bool { return k == KindWeather || k == KindSalinity }

// Cell là ô lưới thô (tâm ô + cạnh ô, độ thập phân WGS84).
type Cell struct {
	Lat     decimal.Decimal
	Lon     decimal.Decimal
	SizeDeg decimal.Decimal
}

// PointQuery là tất cả những gì nhà cung cấp dạng điểm được thấy.
type PointQuery struct {
	Source string
	Cell   Cell
}

// TileQuery là tất cả những gì nhà cung cấp tile được thấy (zoom ≤ MaxZoom của nguồn).
type TileQuery struct {
	Source  string
	Z, X, Y int
}

// Tile là một tile nhà cung cấp trả về.
type Tile struct {
	Body        []byte
	ContentType string
}

// PointProvider là nhà cung cấp dữ liệu theo ô lưới (thời tiết, độ mặn).
type PointProvider interface {
	Point(ctx context.Context, q PointQuery) (json.RawMessage, error)
}

// TileProvider là nhà cung cấp tile.
type TileProvider interface {
	Tile(ctx context.Context, q TileQuery) (Tile, error)
}

// SourceInfo mô tả một nguồn đã bật (không có URL — URL có thể chứa khóa API).
type SourceInfo struct {
	ID          string
	Kind        Kind
	InTerritory bool   // đích thuộc AG_RESIDENCY_ALLOWLIST
	ADR         string // ADR riêng của nguồn ngoài lãnh thổ (ADR-0007: bắt buộc ở production)
	CellDeg     decimal.Decimal
	MaxZoom     int
	TTL         time.Duration
	Fake        bool // nhà cung cấp giả (Local/Dev, kiểm thử) — cấm ở production
}
