package port

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ---------------------------------------------------------------------------
// Tham số G2 (đọc qua masterdata/port.Params; thiếu phiên bản duyệt → mặc định trong code)
// ---------------------------------------------------------------------------
const (
	// ParamHubCapacityWarnPct: % lấp đầy từ đó widget Hub chuyển "sắp đầy" (GIS US-HUB-01,
	// BR-50, ngưỡng % của FN-03); mặc định 60.
	ParamHubCapacityWarnPct = "geo.hub_capacity_warn_pct"
	// ParamHubCapacityFullPct: % lấp đầy từ đó widget Hub là "đầy"; mặc định 85.
	ParamHubCapacityFullPct = "geo.hub_capacity_full_pct"
	// ParamPlotLayerMinZoom: mức zoom tối thiểu để lớp bản đồ trả polygon thửa (GIS BR-24,
	// cận cảnh 1:10.000 → 1:500); mặc định 13.
	ParamPlotLayerMinZoom = "geo.plot_layer_min_zoom"
)

// ---------------------------------------------------------------------------
// Log GPS (HTX US-GPS-03): module khác ghi qua port, geo ghi trong giao dịch riêng
// ---------------------------------------------------------------------------

// Mục đích của một lần lấy toạ độ.
const (
	GpsPurposeSeasonOpen = "season_open"
	GpsPurposeFarmLog    = "farm_log"
	GpsPurposeHarvest    = "harvest"
	GpsPurposeSupport    = "support"
	GpsPurposePlot       = "plot"
)

// Nguồn toạ độ (HTX US-GPS-02 AC-2: luôn ghi rõ nguồn xác thực).
const (
	GpsSourceDevice     = "device"      // GPS thiết bị
	GpsSourcePhotoEXIF  = "photo_exif"  // toạ độ trong ảnh chụp
	GpsSourceManualPick = "manual_pick" // chọn lô thủ công (có thể không có điểm)
)

// GpsFix là một lần lấy toạ độ cần ghi log. Không có dữ liệu cá nhân: người
// thao tác chỉ là ActorRef.
type GpsFix struct {
	// ID: UUIDv7 do máy sinh lúc lấy toạ độ — gửi lại (retry, HTX US-GPS-03 AC-3) an toàn.
	// Nil → geo sinh; khi đó bắt buộc có IdemKey.
	ID       uuid.UUID
	ActorRef uuid.UUID
	DeviceID string
	Purpose  string
	// TargetRef: bản ghi nghiệp vụ của module gọi (vụ, nhật ký, khai báo, yêu cầu hỗ trợ).
	TargetRef uuid.UUID
	// PlotRef (tùy chọn): lô được đối chiếu — geo tính khoảng cách tới ranh giới hiện hành
	// và lấy xã/HTX của lô làm phạm vi đọc.
	PlotRef uuid.UUID
	// CommuneCode (tùy chọn): phạm vi khi không có PlotRef; thiếu cả hai → geo tìm xã chứa điểm.
	CommuneCode string
	Lon, Lat    *decimal.Decimal // WGS84; nil khi chọn lô thủ công
	AccuracyM   *decimal.Decimal
	Source      string
	CapturedAt  time.Time
	IdemKey     string // rỗng → "gps:" + ID
}

// GpsFixReceipt là kết quả ghi log.
type GpsFixReceipt struct {
	ID              uuid.UUID
	Seq             int64
	RecordedAt      time.Time
	DistanceToPlotM *decimal.Decimal // nil nếu không có điểm hoặc lô
	Duplicate       bool             // đã ghi trước đó (gọi lặp)
}

// GpsLog là cổng ghi log GPS cho module khác (cropcycle, extension). Tenant lấy
// từ context. geo ghi trong GIAO DỊCH RIÊNG của pool geo_app — không dùng chung
// giao dịch của module gọi (R-05); module gọi nên ghi log SAU khi commit nghiệp
// vụ chính và không để lỗi log chặn nghiệp vụ (AC-3: đưa vào hàng đợi/outbox của
// mình rồi gọi lại — ID giữ nguyên nên không trùng).
type GpsLog interface {
	RecordFix(ctx context.Context, fix GpsFix) (GpsFixReceipt, error)
}

// ErrGpsFixInvalid bọc lỗi kiểm dữ liệu của RecordFix (module gọi không nên thử lại).
var ErrGpsFixInvalid = errors.New("geo: log GPS không hợp lệ")

// ---------------------------------------------------------------------------
// Hub MKG qua van công bố (hợp đồng hub_facility v1, hub_capacity v1 do mkg định nghĩa)
// ---------------------------------------------------------------------------

// ValveActorRef là actor_ref ghi vào sổ cho dữ liệu nhận qua van (không phải người).
var ValveActorRef = uuid.MustParse("019a0000-0000-7000-8000-00000000fa1e")

// HubFacility là một hub MKG đã công bố. Tenant lấy từ context.
type HubFacility struct {
	HubRef       uuid.UUID
	Version      int64 // phiên bản công bố, tăng dần; bản cũ hơn bị bỏ qua
	Code         string
	Name         string
	Kind         string // kho | hub | nha_may (mặc định hub)
	Lon, Lat     decimal.Decimal
	Polygon      json.RawMessage // GeoJSON Polygon/MultiPolygon WGS84, tùy chọn
	ProvinceCode string
	CommuneCode  string
	CapacityTons *decimal.Decimal
	Status       string // active | retired
	PublishedAt  time.Time
}

// HubCapacity là một ảnh chụp sức chứa hub MKG đã công bố.
type HubCapacity struct {
	HubRef       uuid.UUID
	AsOf         time.Time
	CapacityTons decimal.Decimal
	UsedTons     decimal.Decimal
}

// HubSink nhận dữ liệu hub MKG từ consumer của van (đợt 2 nối). Mọi phương thức
// idempotent: UpsertHubFacility theo (hub_ref, version) — bản cũ/trùng trả
// applied = false; AppendHubCapacity theo (hub_ref, as_of). Số van KHÔNG ghi đè
// số Admin nhập tay (BR-52) — xem pf_geo.capacity_latest.
type HubSink interface {
	UpsertHubFacility(ctx context.Context, h HubFacility) (applied bool, err error)
	AppendHubCapacity(ctx context.Context, c HubCapacity) (applied bool, err error)
}

// ---------------------------------------------------------------------------
// Marker HTX từ pf/party (điểm pf_party.cooperative.location) — module party cài
// ---------------------------------------------------------------------------

// CoopMarker là điểm vị trí HTX (không có dữ liệu cá nhân).
type CoopMarker struct {
	Ref         uuid.UUID
	Code        string
	Name        string
	CommuneCode string
	Lon, Lat    decimal.Decimal
}

// CoopMarkers cho geo đọc điểm HTX làm marker/dự phòng khi HTX chưa có ranh giới
// (GIS US-BND-02 "gắn Marker"). Tenant lấy từ context; không kiểm phạm vi.
type CoopMarkers interface {
	MarkersInBBox(ctx context.Context, minLon, minLat, maxLon, maxLat float64, limit int) ([]CoopMarker, error)
}

// ---------------------------------------------------------------------------
// Sự kiện G2
// ---------------------------------------------------------------------------
const (
	EventCooperativeBoundaryRecorded = "pf.geo.cooperative_boundary.recorded"
	EventFacilityRecorded            = "pf.geo.facility.recorded"
)

// CooperativeBoundaryRecordedV1 là payload của pf.geo.cooperative_boundary.recorded.
// Không mang hình (consumer đọc qua API).
//
//ag:event pf.geo.cooperative_boundary.recorded v1
type CooperativeBoundaryRecordedV1 struct {
	CooperativeRef   uuid.UUID  `json:"cooperative_ref"`
	BoundaryID       uuid.UUID  `json:"boundary_id"`
	Version          int64      `json:"version"`
	Action           string     `json:"action"`
	Status           string     `json:"status"`
	AreaHa           string     `json:"area_ha"`
	PointCount       int        `json:"point_count"`
	Source           string     `json:"source"`
	CommuneCode      string     `json:"commune_code"`
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	CorrectionReason *string    `json:"correction_reason"`
	ActorRef         uuid.UUID  `json:"actor_ref"`
	EventTime        string     `json:"event_time"`
}

// FacilityRecordedV1 là payload của pf.geo.facility.recorded (tạo, sửa, ngừng dùng,
// khôi phục một cơ sở kho/Hub/nhà máy của tenant).
//
//ag:event pf.geo.facility.recorded v1
type FacilityRecordedV1 struct {
	FacilityRef      uuid.UUID  `json:"facility_ref"`
	EventID          uuid.UUID  `json:"event_id"`
	Version          int64      `json:"version"`
	Action           string     `json:"action"`
	Kind             string     `json:"kind"`
	Code             string     `json:"code"`
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	ProvinceCode     string     `json:"province_code"`
	CommuneCode      *string    `json:"commune_code"`
	Lon              string     `json:"lon"`
	Lat              string     `json:"lat"`
	HasPolygon       bool       `json:"has_polygon"`
	Capacity         string     `json:"capacity"`
	Unit             string     `json:"unit"`
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	CorrectionReason *string    `json:"correction_reason"`
	ActorRef         uuid.UUID  `json:"actor_ref"`
	EventTime        string     `json:"event_time"`
}
