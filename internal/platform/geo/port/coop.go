package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ---------------------------------------------------------------------------
// W3.3: thửa, ranh giới thửa và ranh giới/vùng HTX chuyển sang pf/cooperative (schema pf_coop). pf/geo
// (capability) không import module domain: các interface dưới đây do pf/geo KHAI (bên tiêu thụ hoặc bên
// cung cấp), gốc lắp ráp nối. Tenant lấy từ context; không kiểm phạm vi người gọi trừ khi ghi rõ.
// ---------------------------------------------------------------------------

// PlotPlace là dữ kiện thửa cho một lần ghi log GPS.
type PlotPlace struct {
	CommuneCode    string
	CooperativeRef uuid.UUID        // Nil = thửa chưa gán HTX
	DistanceM      *decimal.Decimal // nil khi không có điểm
}

// PlotGeometry là dữ kiện thửa mà log GPS cần (HTX US-GPS-01/03): hiện thực ở pf/cooperative, gốc lắp ráp nối
// (W3.3). Chưa nối → log GPS không có khoảng cách tới lô, phạm vi lấy theo xã gửi lên / xã chứa điểm; vai
// farmer không đọc được log.
type PlotGeometry interface {
	// PlotPlace trả xã, HTX của thửa và khoảng cách trắc địa (mét, làm tròn 1 chữ số thập phân) từ điểm WGS84 tới
	// ranh giới hiện hành (0 nếu điểm nằm trong thửa); lon/lat nil → DistanceM nil. found = false khi không có thửa.
	PlotPlace(ctx context.Context, ref uuid.UUID, lon, lat *decimal.Decimal) (place PlotPlace, found bool, err error)
	// PlotsOfHouseholds trả ref các thửa (mọi trạng thái) của các nông hộ; households rỗng → rỗng.
	PlotsOfHouseholds(ctx context.Context, households []uuid.UUID) ([]uuid.UUID, error)
}

// MapLayerQuery là khung nhìn của một lớp bản đồ.
type MapLayerQuery struct {
	MinLon, MinLat, MaxLon, MaxLat float64
	Zoom                           int
	Limit                          int // 0 → 1.000 (GIS BR-25)
	// MapConsumer: bản đồ chuẩn hóa (GET /v1/geo/standard-map) — người gọi có vai map_consumer đọc lớp HTX toàn
	// tenant (lớp nền không định danh). Bên hiện thực chỉ áp khi người gọi thật sự có vai đó.
	MapConsumer bool
}

// MapLayers là lớp ranh giới HTX và lớp thửa cho bản đồ gộp (GET /v1/geo/layers) và bản đồ chuẩn hóa của pf/geo:
// hiện thực ở pf/cooperative, gốc lắp ráp nối (W3.3). Bên hiện thực tự kiểm vai, phạm vi của người gọi (context)
// và trả FeatureCollection GeoJSON ĐÃ DỰNG (đối tượng JSON: "type", "features" là []map[string]any với
// "properties" là map[string]any, "truncated" bool, và "limit" với lớp HTX) — JSON giữ nguyên như trước W3.3.
type MapLayers interface {
	// CooperativeLayer: ranh giới HTX đang dùng trong khung nhìn + marker HTX chưa có ranh giới (GIS US-BND-02).
	CooperativeLayer(ctx context.Context, q MapLayerQuery) (map[string]any, error)
	// PlotLayer: polygon thửa trong khung nhìn (như GET /v1/plots); không có "limit".
	PlotLayer(ctx context.Context, q MapLayerQuery) (map[string]any, error)
}

// AdminGeometry là phép tính hình học trên ranh giới hành chính của pf/geo cho module khác (pf/cooperative kiểm thửa
// nằm trong địa giới xã). pf/geo HIỆN THỰC (geo.Module.AdminGeometry), tính trong giao dịch riêng của pf/geo.
type AdminGeometry interface {
	// OutsideCommunePct trả phần trăm diện tích hình (GeoJSON Polygon/MultiPolygon WGS84) nằm ngoài ranh giới
	// hiện hành của xã; nil nếu chưa nhập ranh giới xã đó.
	OutsideCommunePct(ctx context.Context, geojson, commune string) (*decimal.Decimal, error)
}
