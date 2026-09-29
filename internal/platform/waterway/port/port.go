// Package port là giao diện công khai DUY NHẤT của pf/waterway mà module khác
// được import (R-02): tên vai, khóa tham số, sự kiện và cổng định tuyến Router
// cho pf/simulation.
//
// Mạng đường thủy (mục 16) là đồ thị node/edge trong pgRouting, CÓ PHIÊN BẢN:
// tuyến nháp không bao giờ vào đồ thị; mỗi lần dựng tạo một phiên bản mới từ
// tập đoạn ĐÃ XÁC NHẬN; chỉ một phiên bản được phát hành có hiệu lực tại một
// thời điểm, các phiên bản cũ được giữ nguyên để kịch bản mô phỏng đã ghim
// phiên bản vẫn định tuyến lại được.
package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Vai nghiệp vụ (tên vai trong Keycloak).
const (
	// RoleEditor vẽ/sửa đoạn sông/kênh, công trình vượt sông và yêu cầu dựng mạng
	// (Admin API, GIS US-WATER-02/03).
	RoleEditor = "waterway_editor"
	// RolePublisher phát hành một phiên bản mạng đã dựng; không được là người dựng (R-56).
	RolePublisher = "waterway_publisher"
)

// Phân loại tuyến (GIS BR-35 + sông).
const (
	ClassMainCanal  = "kenh_chinh"
	ClassFieldCanal = "noi_dong"
	ClassDitch      = "muong"
	ClassRiver      = "song"
)

// Loại công trình vượt sông.
const (
	StructureBridge    = "cau"
	StructureCulvert   = "cong"
	StructureLock      = "au_thuyen"
	StructurePowerLine = "duong_day_dien"
)

// Nhãn nguồn tính khoảng cách (Simulation FN-04 BR-04, FN-20 BR-05).
const (
	SourceRouted    = "routed"    // định tuyến trên mạng có dữ liệu chính thức/khảo sát
	SourceDigitised = "digitised" // định tuyến có đi qua tuyến số hóa nội bộ
	SourceHaversine = "haversine" // Haversine × hệ số hiệu chỉnh (#10) — không có đường thủy liên thông
)

// Khóa tham số đọc qua pf/masterdata (thiếu → giá trị mặc định trong mã, xem domain.DefaultParams).
const (
	ParamSnapToleranceM     = "waterway.snap_tolerance_m"       // 25: hai điểm cách ≤ giá trị là cùng một đỉnh
	ParamAutoJoinMaxM       = "waterway.auto_join_max_m"        // 150: khe hở ≤ giá trị được nối tự động
	ParamReportGapMaxM      = "waterway.report_gap_max_m"       // 1000: khe hở ≤ giá trị được báo cáo
	ParamOnSegmentTolM      = "waterway.on_segment_tolerance_m" // 20: đầu tuyến cách thân tuyến khác ≤ giá trị → nối chữ T
	ParamStructureMaxOffset = "waterway.structure_max_offset_m" // 500: công trình phải nằm cách tuyến ≤ giá trị
	ParamAccessRadiusM      = "waterway.access_radius_m"        // 5000: điểm đầu/cuối cách mạng ≤ giá trị mới định tuyến
	ParamWidthFactor        = "waterway.safety.width_factor"    // 1.5: kênh rộng ≥ thân tàu × hệ số
	ParamUnderKeelM         = "waterway.safety.under_keel_m"    // 0.4: nước dưới đáy tàu
	ParamAirGapM            = "waterway.safety.air_gap_m"       // 0.5: dự trữ tĩnh không
	ParamHaversineFactor    = "waterway.haversine_factor"       // 1.3: tham số #10 cho đường thủy
	// ParamVesselPrefix + ":<mã lớp>:<thuộc tính>" với thuộc tính tons | beam_m | draft_m | air_draft_m.
	ParamVesselPrefix = "waterway.vessel"
)

// CatalogVesselClass là danh mục lớp phương tiện (pf/masterdata): mã mục là mã
// lớp, attrs tons, beam_m, draft_m, air_draft_m (decimal). Không có danh mục →
// ba lớp mặc định (ghe 100 t, sà lan 1.000 t, sà lan 2.000 t).
const CatalogVesselClass = "vessel_class"

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventBuildRequested   = "pf.waterway.network_build.requested"
	EventNetworkPublished = "pf.waterway.network.published"
)

// BuildConsumer là tên consumer của worker dựng mạng (shared.inbox).
const BuildConsumer = "pf.waterway.build"

// BuildRequestedV1 là payload của pf.waterway.network_build.requested, schema_version 1.
// Worker nạp tham số đã chốt từ bảng build_job, không từ sự kiện.
//
//ag:event pf.waterway.network_build.requested v1
type BuildRequestedV1 struct {
	BuildID     string `json:"build_id"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
}

// NetworkPublishedV1 là payload của pf.waterway.network.published, schema_version 1.
// pf/simulation ghim version_no vào ảnh chụp đầu vào của kịch bản (mục 16).
//
//ag:event pf.waterway.network.published v1
type NetworkPublishedV1 struct {
	NetworkVersionID    string  `json:"network_version_id"`
	VersionNo           int     `json:"version_no"`
	SegmentSetSHA256    string  `json:"segment_set_sha256"`
	GraphSHA256         string  `json:"graph_sha256"`
	EffectiveFrom       string  `json:"effective_from"`
	PublishedBy         string  `json:"published_by"`
	BuiltBy             string  `json:"built_by"`
	SupersedesVersionNo *int    `json:"supersedes_version_no"`
	VertexCount         int     `json:"vertex_count"`
	EdgeCount           int     `json:"edge_count"`
	RuleVersion         string  `json:"rule_version"`
	SupersededVersionID *string `json:"superseded_version_id"`
}

// LatLng là một điểm WGS84.
type LatLng struct {
	Lat float64
	Lng float64
}

// Limiting là ràng buộc bất lợi nhất trên một tuyến đường đi (GIS BR-37/38).
type Limiting struct {
	// MaxLoadTons nil = chưa kết luận được (thiếu số liệu) — không bao giờ coi là đi được.
	MaxLoadTons  *decimal.Decimal
	Certain      bool
	Reason       string // width | depth | clearance | declared | none | unknown:<thông số>
	SegmentRef   uuid.UUID
	StructureRef *uuid.UUID
}

// Leg là một chặng giữa hai điểm liên tiếp.
type Leg struct {
	From, To LatLng
	Found    bool
	// DistanceKm gồm chặng tiếp cận từ điểm tới mạng; Haversine × hệ số khi Found = false.
	DistanceKm  decimal.Decimal
	AccessKm    decimal.Decimal
	Source      string // SourceRouted | SourceDigitised | SourceHaversine
	Path        []LatLng
	SegmentRefs []uuid.UUID
	Limiting    *Limiting
	Reason      string // vì sao không có đường thủy (Found = false)
}

// Route là kết quả định tuyến qua dãy điểm trên MỘT phiên bản mạng.
type Route struct {
	// VersionNo 0 = tenant chưa có phiên bản mạng phát hành (mọi chặng là Haversine).
	VersionNo  int
	Found      bool // mọi chặng đều có đường thủy
	DistanceKm decimal.Decimal
	Source     string // nguồn yếu nhất trong các chặng
	Legs       []Leg
	Limiting   *Limiting
}

// ErrVersionNotFound: phiên bản mạng không có, hoặc chưa từng được phát hành.
var ErrVersionNotFound = errors.New("waterway: không có phiên bản mạng đã phát hành với số này")

// Router định tuyến đường thủy cho module khác (pf/simulation). Tenant lấy từ
// context (tenant.WithID). version = 0 → phiên bản đang phát hành; > 0 → ghim
// đúng phiên bản đó (kể cả đã bị thay thế). minLoadTons = 0 → không lọc tải trọng.
type Router interface {
	RouteBetween(ctx context.Context, points []LatLng, minLoadTons decimal.Decimal, version int) (Route, error)
	// CurrentVersion trả số phiên bản đang phát hành; 0 nếu chưa có.
	CurrentVersion(ctx context.Context) (int, error)
}
