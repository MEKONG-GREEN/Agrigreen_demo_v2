// Package port là cửa công khai DUY NHẤT của module mkg/logistics (R-02): chuyến
// (xe, ghe, sà lan), chặng đường thủy / đường bộ, mốc chuyến, phương tiện, chứng từ
// chuyến (docs/design/mkg.md §5.4; docs/dev/g4-conventions.md).
//
// ĐÓNG BĂNG ở G4-0 để các agent G4 làm song song: đổi chữ ký hoặc ngữ nghĩa của
// một interface dưới đây phải qua người tích hợp. Thêm phương thức mới vào interface
// MỚI, không vào interface đã đóng băng; thêm trường vào DTO chỉ khi tương thích ngược.
//
// Nguyên tắc chung:
//   - Mọi phương thức nhận tenantID tường minh, chạy trong giao dịch tenant (RB-2).
//   - Khối lượng mass.Kg, tiền money.Amount / money.Price, km và tọa độ decimal —
//     KHÔNG float (R-10; toàn module nằm trong agnofloat).
//   - Kết quả định tuyến ĐÃ DÙNG được lưu bền vững vào chặng (tái hiện chi phí,
//     mục 16) — không bao giờ tính lại từ mạng hiện hành khi đọc chặng cũ.
//   - Dữ liệu nền tảng (mạng đường thủy) chỉ tới qua bề mặt đối tác /v1/partner/*
//     (ADR-0020, loại waterway_routes) — không import internal/platform/* (R-03).
//   - Không gì trong package này qua van công bố: chuyến, cước, tài xế, phương tiện
//     ở lại vùng mkg. Tài xế là *_ref (định danh ở vault tenant MKG, RB-3).
package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// DBRole là vai của module ở cụm mkg (schema mkg_logistics, mkg 000014/000015).
const DBRole = "logistics_app"

// Vai nghiệp vụ (realm Keycloak; tài khoản tenant MKG).
const (
	// RoleDispatcher: điều phối — lập/hủy chuyến, gán phương tiện và tài xế, chọn
	// phương thức theo chặng (đường bộ mặc định; đường thủy khi xa hơn ngưỡng và có
	// kênh tiếp cận — proto decideMode), xác nhận cước thực tế, quản lý danh mục ghe.
	RoleDispatcher = "dispatcher"
	// RoleDriver: tài xế / chủ ghe — ghi mốc chuyến (xuất phát, đến, hoàn tất) trên app.
	RoleDriver = "driver"
)

// Mode là phương thức vận tải của một chặng.
type Mode string

// Phương thức.
const (
	ModeRoad     Mode = "road"
	ModeWaterway Mode = "waterway"
)

// TripStatus là trạng thái chuyến. Chuyển trạng thái chỉ tiến (proto ke_hoach →
// dang_chay → hoan_thanh; huy): planned → departed → arrived → completed, hoặc
// planned|departed → cancelled (có lý do). Go cưỡng chế chuyển trạng thái (prototype không).
type TripStatus string

// Trạng thái chuyến.
const (
	TripPlanned   TripStatus = "planned"
	TripDeparted  TripStatus = "departed"
	TripArrived   TripStatus = "arrived"
	TripCompleted TripStatus = "completed" // đã có khối lượng thật (phiếu cân) và cước thực tế
	TripCancelled TripStatus = "cancelled"
)

// CostSource là nguồn của cước thực tế (proto actual_cost_source).
type CostSource string

// Nguồn cước.
const (
	CostByParamRate  CostSource = "param_rate"  // tấn thật × km × đơn giá tham số theo phương thức (proto theo_don_gia)
	CostManual       CostSource = "manual"      // nhập tay có chứng từ (proto nhap_tay)
	CostByVesselRate CostSource = "vessel_rate" // đơn giá thuê ghe per_ton | per_trip | per_ton_km (proto GH-03 don_gia_ghe)
)

// RouteSource là nguồn tính km của chặng.
type RouteSource string

// Nguồn tính km (khớp nhãn pf/waterway + đường bộ).
const (
	RouteRouted    RouteSource = "routed"    // định tuyến trên mạng có dữ liệu chính thức/khảo sát
	RouteDigitised RouteSource = "digitised" // định tuyến đi qua tuyến số hóa nội bộ
	RouteHaversine RouteSource = "haversine" // Haversine × hệ số hiệu chỉnh (#10) — không có tuyến
	RouteManual    RouteSource = "manual"    // điều phối nhập km có lý do
)

// GeoPoint là một điểm WGS84 (decimal, 7 chữ số thập phân — R-10).
type GeoPoint struct {
	Lat decimal.Decimal
	Lng decimal.Decimal
}

// ---------------------------------------------------------------------------
// Sự kiện nội bộ (ADR-0018): outbox.Event{Kind: outbox.KindInternal} → vhost mkg.internal.
// ---------------------------------------------------------------------------

// EventTripCompleted: chuyến hoàn tất (khối lượng thật + cước thực tế đã chốt) — hoặc bản bù.
const EventTripCompleted = "mkg.logistics.trip.completed"

// Consumer của logistics (hàng đợi quorum trên mkg.internal, DLQ = tên + ".dlq").
const (
	// RollCountConsumer: mkg.fieldops.roll_count.recorded (stage boat_load) → mỗi lượt
	// xuống ghe một chuyến đường thủy ở trạng thái planned, tấn ước theo cuộn (proto FM-05).
	// Lỗi lập chuyến KHÔNG chặn ghi đếm cuộn (chỉ cảnh báo, như prototype).
	RollCountConsumer = "mkg.logistics.roll_count"
	// WeighTicketConsumer: mkg.measurement.weigh_ticket.recorded (HUB_IN / CUSTOMER_IN của lô
	// trên chuyến) → đóng chuyến với tấn thật = phiếu cân (proto plant weighing đóng chuyến).
	WeighTicketConsumer = "mkg.logistics.weigh_ticket"
)

// TripCompletedV1 là payload khi một chuyến hoàn tất (kể cả bản bù sửa sai — SupersedesID).
// Người nhận nội bộ: mkg/costing (phân bổ cước đa chặng về lô theo khối lượng; CO2 vận chuyển
// là tham số của costing/MRV — không tính ở đây). Chỉ vhost mkg.internal: cước, phương tiện, tài xế
// KHÔNG BAO GIỜ qua van.
//
//ag:internal-event mkg.logistics.trip.completed v1
type TripCompletedV1 struct {
	TripID             uuid.UUID        `json:"trip_id"`
	Mode               Mode             `json:"mode"` // phương thức chính (chặng dài nhất)
	VehicleRef         *uuid.UUID       `json:"vehicle_ref"`
	CollectionOrderRef *uuid.UUID       `json:"collection_order_ref"` // lệnh gom (FM-05), nếu chuyến sinh từ xuống ghe
	DeliveryRef        *uuid.UUID       `json:"delivery_ref"`         // giao nhận mkg/orders, nếu chuyến giao cho khách
	Legs               []TripLegV1      `json:"legs"`
	Cargo              []TripCargoV1    `json:"cargo"`
	PlannedKg          *mass.Kg         `json:"planned_kg"`
	ActualKg           mass.Kg          `json:"actual_kg"`
	ActualCost         money.Amount     `json:"actual_cost"` // VND, numeric(18,2)
	CostSource         CostSource       `json:"cost_source"`
	DepartedAt         time.Time        `json:"departed_at"`
	ArrivedAt          time.Time        `json:"arrived_at"`
	RuleVersion        string           `json:"rule_version"` // vd "logistics.trip_cost.v1"
	SupersedesID       *uuid.UUID       `json:"supersedes_id"`
	EventTime          time.Time        `json:"event_time"`
	Vessel             *TripVesselRefV1 `json:"vessel"`
}

// TripLegV1 là một chặng của chuyến, kèm tham chiếu kết quả định tuyến đã lưu.
type TripLegV1 struct {
	LegID       uuid.UUID   `json:"leg_id"`
	Seq         int         `json:"seq"`
	Mode        Mode        `json:"mode"`
	FromKind    string      `json:"from_kind"` // plot | collection_point | facility | customer_site | point
	FromRef     *uuid.UUID  `json:"from_ref"`
	ToKind      string      `json:"to_kind"`
	ToRef       *uuid.UUID  `json:"to_ref"`
	DistanceKm  string      `json:"distance_km"` // decimal 3 chữ số
	RouteSource RouteSource `json:"route_source"`
	// RouteSnapshotRef: bản ghi mkg_logistics.route_snapshot (nguyên văn phản hồi định tuyến + SHA-256).
	RouteSnapshotRef *uuid.UUID `json:"route_snapshot_ref"`
	// NetworkVersion: phiên bản mạng đường thủy của bên cấp đã dùng (nil = không định tuyến mạng).
	NetworkVersion *int     `json:"network_version"`
	MassKg         *mass.Kg `json:"mass_kg"` // khối lượng thật trên chặng (nil = bằng ActualKg)
}

// TripCargoV1 là một lô trên chuyến.
type TripCargoV1 struct {
	LotRef    uuid.UUID  `json:"lot_ref"`
	MassKg    mass.Kg    `json:"mass_kg"`
	TicketRef *uuid.UUID `json:"ticket_ref"` // phiếu cân đã dùng làm tấn thật (measurement)
}

// TripVesselRefV1 là phương tiện của chuyến (không có dữ liệu cá nhân chủ ghe).
type TripVesselRefV1 struct {
	VesselRef uuid.UUID `json:"vessel_ref"`
	Code      string    `json:"code"` // số hiệu chuẩn hóa (proto GH-01)
	Kind      string    `json:"kind"` // ghe | sa_lan | truck
}

// ---------------------------------------------------------------------------
// Cổng cho module mkg khác (cùng tiến trình).
// ---------------------------------------------------------------------------

// LegView là một chặng đã chốt.
type LegView struct {
	LegID            uuid.UUID
	Seq              int
	Mode             Mode
	FromKind, ToKind string
	FromRef, ToRef   *uuid.UUID
	DistanceKm       decimal.Decimal
	RouteSource      RouteSource
	RouteSnapshotRef *uuid.UUID
	NetworkVersion   *int
	MassKg           *mass.Kg
}

// CargoLine là một lô trên chuyến.
type CargoLine struct {
	LotRef    uuid.UUID
	MassKg    mass.Kg
	TicketRef *uuid.UUID
}

// TripView là chuyến theo bảng chiếu trip_current (phiên bản hiệu lực — đã tính bản bù).
type TripView struct {
	TripID             uuid.UUID
	Code               string // CH + 6 số (proto)
	Status             TripStatus
	Mode               Mode
	VehicleRef         *uuid.UUID
	CollectionOrderRef *uuid.UUID
	DeliveryRef        *uuid.UUID
	Legs               []LegView
	Cargo              []CargoLine
	PlannedKg          *mass.Kg
	ActualKg           *mass.Kg
	PlannedCost        *money.Amount
	ActualCost         *money.Amount
	CostSource         CostSource
	DepartedAt         *time.Time
	ArrivedAt          *time.Time
	CompletedAt        *time.Time
	RuleVersion        string
	// RecordedAt: recorded_at của bản ghi sổ mới nhất đã tính (costing ghim vào ảnh chụp đầu vào).
	RecordedAt time.Time
}

// TripPage là một trang chuyến hoàn tất theo con trỏ (recorded_at, trip_id).
type TripPage struct {
	Items []TripView
	Next  string // rỗng = hết
}

// Trips cho mkg/costing (và orders) đọc chuyến. Completed trả chuyến có trạng thái completed
// với completed_at ∈ [from, to) và bản ghi có recorded_at < asOf (ảnh chụp đầu vào bất biến của
// đóng kỳ, R-13), sắp theo (completed_at, trip_id). ErrTripNotFound khi không có.
type Trips interface {
	Trip(ctx context.Context, tenantID, tripID uuid.UUID) (TripView, error)
	TripsForLot(ctx context.Context, tenantID, lotRef uuid.UUID) ([]TripView, error)
	Completed(ctx context.Context, tenantID uuid.UUID, from, to, asOf time.Time, cursor string, limit int) (TripPage, error)
}

// ---------------------------------------------------------------------------
// Nguồn định tuyến (client phía MKG; hiện thực ở logistics/adapter).
// ---------------------------------------------------------------------------

// RouteRequest là yêu cầu định tuyến qua dãy điểm (2..25).
type RouteRequest struct {
	Points      []GeoPoint
	MinLoadTons decimal.Decimal // 0 = không lọc tải trọng
	// NetworkVersion: 0 = phiên bản đang phát hành của bên cấp; > 0 = ghim (tính lại chặng cũ).
	NetworkVersion int
}

// RouteLeg là một chặng của kết quả định tuyến.
type RouteLeg struct {
	From, To    GeoPoint
	Found       bool
	DistanceKm  decimal.Decimal
	AccessKm    decimal.Decimal
	Source      RouteSource
	SegmentRefs []uuid.UUID
	Path        []GeoPoint
	Reason      string
}

// RouteResult là kết quả định tuyến, kèm dữ liệu để lưu bền vững vào route_snapshot.
type RouteResult struct {
	// GrantorTenant: tenant chương trình (bên cấp) sở hữu mạng — nil với đường bộ.
	GrantorTenant  *uuid.UUID
	NetworkVersion *int // nil = bên cấp chưa phát hành mạng (Haversine)
	Found          bool
	DistanceKm     decimal.Decimal
	Source         RouteSource
	Legs           []RouteLeg
	// MaxLoadTons: ràng buộc tải trọng bất lợi nhất (nil = chưa kết luận / không có).
	MaxLoadTons *decimal.Decimal
	// Raw: nguyên văn thân phản hồi (lưu vào route_snapshot); RawSHA256: hex thường.
	Raw       []byte
	RawSHA256 string
	FetchedAt time.Time
}

// WaterwayRoutes là nguồn định tuyến đường thủy của nền tảng: gọi
// GET/POST /v1/partner/waterway/route của Application API công khai bằng tài khoản dịch vụ
// tenant MKG (vai mkg_sync, OIDC client credentials), chỉ được khi tenant chương trình đã duyệt
// cấp quyền loại waterway_routes (ADR-0020). Lỗi: ErrRouteUnavailable (chưa cấu hình, nền tảng
// lỗi — thử lại / cho điều phối nhập km tay có lý do), ErrRouteGrantMissing (403 cấp quyền).
// POST gửi Idempotency-Key tất định = SHA-256 của thân yêu cầu.
type WaterwayRoutes interface {
	Route(ctx context.Context, tenantID uuid.UUID, req RouteRequest) (RouteResult, error)
}

// RoadRoutes là nguồn định tuyến đường bộ (OSRM trong lãnh thổ khi có — RB-7; tới lúc đó
// Haversine × hệ số #10 đường bộ, RouteHaversine). Cùng ngữ nghĩa lưu bền vững như WaterwayRoutes.
type RoadRoutes interface {
	Route(ctx context.Context, tenantID uuid.UUID, req RouteRequest) (RouteResult, error)
}

// Lỗi ổn định của port (ánh xạ problem.* mã logistics.* — docs/errors.md).
var (
	ErrTripNotFound      = errors.New("logistics: không có chuyến")
	ErrRouteUnavailable  = errors.New("logistics: nguồn định tuyến không sẵn sàng")
	ErrRouteGrantMissing = errors.New("logistics: chưa có cấp quyền chia sẻ waterway_routes (ADR-0020)")
)

// Tham số có phiên bản của module (param_version, người đặt ≠ người duyệt — R-56). Mặc định
// lấy theo prototype, TẠM, CHỜ KHÁCH (docs/dev/g4-conventions.md §1.2).
const (
	ParamRoadRatePerTonKm     = "logistics.rate.road_vnd_per_t_km"     // 2200 (#22)
	ParamWaterwayRatePerTonKm = "logistics.rate.waterway_vnd_per_t_km" // 900 (#23)
	ParamRoadFeasibleKm       = "logistics.mode.road_feasible_km"      // #27: xa hơn + có kênh → gợi ý đường thủy
	ParamStrawPayloadTons     = "logistics.vessel.straw_payload_tons"  // 90 (#51): tấn rơm thực chở của ghe
	ParamPayloadWarnFactor    = "logistics.vessel.payload_warn_factor" // 1.1: tấn ước > 1,1 × tải → cảnh báo
	ParamExpiryWarnDays       = "logistics.vessel.expiry_warn_days"    // 30 (GH-02): chỉ cảnh báo, không chặn
	ParamUnweighedAlertHours  = "logistics.trip.unweighed_alert_hours" // 48: xuống ghe quá 48 giờ chưa cân → cảnh báo
)
