package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Điểm mở rộng lớp bản đồ của pf/geo (cap/geo: "hình học thực thể thuộc module chủ; cap/geo chỉ cung cấp phép
// tính và hiển thị" — §26). Module chủ lớp (pf/waterway: tuyến đường thủy đi được) hiện thực Layer qua gốc lắp
// ráp — phụ thuộc chỉ đi xuống (R-MOD-05).

// LayerPoint là một điểm WGS84 của lớp.
type LayerPoint struct {
	Lat float64
	Lng float64
}

// LayerSegment là một tuyến của phiên bản mạng đang phát hành trong khung nhìn — lớp "đường
// thủy đi được" của bản đồ chuẩn hóa (GIS US-INT-02 / FN-17).
type LayerSegment struct {
	SegmentRef  uuid.UUID
	Name        string
	Class       string
	MaxLoadTons *decimal.Decimal // nil = chưa kết luận được (không coi là đi được)
	Certain     bool
	Passable    bool // đi được với tải trọng yêu cầu
	Line        []LayerPoint
}

// LayerPage là các tuyến trong khung nhìn của phiên bản đang phát hành.
type LayerPage struct {
	VersionNo     int // 0 = chưa có phiên bản phát hành
	Items         []LayerSegment
	Truncated     bool
	PassableCount int
}

// Layer đọc lớp tuyến đường thủy theo khung nhìn cho module khác (pf/geo — bản đồ chuẩn hóa
// cho TMS). Tenant lấy từ context. minLoadTons = 0 → không lọc tải trọng.
type Layer interface {
	SegmentsInBBox(ctx context.Context, minLng, minLat, maxLng, maxLat float64, minLoadTons decimal.Decimal, limit int) (LayerPage, error)
}
