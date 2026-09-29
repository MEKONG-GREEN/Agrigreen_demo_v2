// Package port là giao diện công khai DUY NHẤT của pf/simulation (mô phỏng chọn
// Hub, mục 12 và 16) mà module khác được import (R-02).
//
// pf/simulation KHÔNG tính: engine nằm ở optimizer/ (Python 3.12, ngoại lệ bắt
// buộc mục 4). Module này giữ Hub ứng viên, kịch bản, ảnh chụp đầu vào bất biến,
// kết quả bất biến, và dựng ảnh chụp ở worker qua ba cổng khai ở đây:
// SupplySource (nguồn cung HTX × vụ), Router (khoảng cách theo phiên bản mạng),
// ParamSet (tham số đã phê duyệt).
package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Vai của module (tên vai trong Keycloak).
const (
	// RoleSimulationAnalyst dựng Hub ứng viên, kịch bản, chạy và so sánh mô phỏng.
	RoleSimulationAnalyst = "simulation_analyst"
)

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	// EventRunSubmitted: API đã ghi yêu cầu mô phỏng; worker Go dựng ảnh chụp (bước worker).
	EventRunSubmitted = "pf.simulation.run.submitted"
	// EventJobRequested: ảnh chụp bất biến đã sẵn sàng; optimizer nhận và tính.
	EventJobRequested = "pf.simulation.job.requested"
	// EventJobCompleted: kết quả (hoặc lỗi) đã ghi; notify báo người yêu cầu.
	EventJobCompleted = "pf.simulation.job.completed"
	Producer          = "pf/simulation"
)

// Consumer của worker (shared.inbox).
const (
	// SnapshotConsumer dựng ảnh chụp cho pf.simulation.run.submitted.
	SnapshotConsumer = "pf.simulation.snapshot"
)

// RunSubmittedV1 là payload của pf.simulation.run.submitted. Worker đọc yêu cầu
// từ bảng simulation, không từ sự kiện.
//
//ag:event pf.simulation.run.submitted v1
type RunSubmittedV1 struct {
	SimulationID string `json:"simulation_id"`
	ScenarioID   string `json:"scenario_id"`
	RequestedBy  string `json:"requested_by"`
	RequestedAt  string `json:"requested_at"`
}

// JobRequestedV1 là payload của pf.simulation.job.requested. Optimizer lấy ảnh
// chụp qua Internal API (snapshot_path, mTLS) và kiểm snapshot_sha256 trước khi
// tính. Không chứa dữ liệu cá nhân.
//
//ag:event pf.simulation.job.requested v1
type JobRequestedV1 struct {
	SimulationID     string `json:"simulation_id"`
	SnapshotID       string `json:"snapshot_id"`
	SnapshotSHA256   string `json:"snapshot_sha256"`
	SnapshotPath     string `json:"snapshot_path"`
	ResultPath       string `json:"result_path"`
	AlgorithmVersion string `json:"algorithm_version"`
	Mode             string `json:"mode"`
	RequestedAt      string `json:"requested_at"`
}

// JobCompletedV1 là payload của pf.simulation.job.completed.
//
//ag:event pf.simulation.job.completed v1
type JobCompletedV1 struct {
	SimulationID     string  `json:"simulation_id"`
	ScenarioID       string  `json:"scenario_id"`
	RequestedBy      string  `json:"requested_by"`
	Status           string  `json:"status"` // completed | failed
	ErrorCode        *string `json:"error_code"`
	ResultSHA256     *string `json:"result_sha256"`
	OfficialEligible bool    `json:"official_eligible"`
	CompletedAt      string  `json:"completed_at"`
}

// LatLng là toạ độ WGS84 dùng để định tuyến — CHỈ ở phía nền tảng, không bao giờ
// vào ảnh chụp gửi optimizer (RB-7).
type LatLng struct {
	Lat float64
	Lng float64
}

// ---------------------------------------------------------------------------
// SupplySource — nguồn cung theo HTX × vụ
// ---------------------------------------------------------------------------

// SupplyQuery là phạm vi nguồn cung cần cho một kịch bản.
type SupplyQuery struct {
	ReferenceYear int       // năm vụ tham chiếu (thống kê mùa vụ trước)
	At            time.Time // thời điểm yêu cầu mô phỏng
}

// CoopSeason là sản lượng lúa và diện tích của một HTX trong một vụ.
type CoopSeason struct {
	SeasonKey     string
	PaddyTons     decimal.Decimal // #5 — nguồn duy nhất của Total Available (quyết định B3)
	PlantedAreaHa decimal.Decimal // #4 — chỉ hiển thị/đối chiếu
}

// CooperativeSupply là nguồn cung của một HTX. Location nil (thiếu toạ độ) → bị
// loại trước khi định tuyến.
type CooperativeSupply struct {
	CooperativeRef uuid.UUID
	Location       *LatLng // tâm vùng nguyên liệu (ranh giới HTX của pf/geo)
	MemberCount    int
	Seasons        []CoopSeason
}

// SeasonDef là một vụ trong lịch mùa vụ (#7).
type SeasonDef struct {
	Key       string
	Type      string // DX | HT | TD … (tiền tố loại vụ)
	EndMonth  int
	SortOrder int
}

// HarvestObservation là tấn thu hoạch tại một ngày trong năm (1–365).
type HarvestObservation struct {
	DayOfYear int
	Tons      decimal.Decimal
}

// SupplyData là nguồn cung đã giải cho một kịch bản.
type SupplyData struct {
	Cooperatives []CooperativeSupply
	Seasons      []SeasonDef
	// Observations: đường cong thu hoạch thực tế theo loại vụ (file điều tra); rỗng → suy diễn.
	Observations map[string][]HarvestObservation
}

// SupplySource trả nguồn cung HTX × vụ (sẽ đến từ pf/cropcycle coop_season_area +
// pf/mechanization/masterdata). Tenant truyền tường minh vì chạy ở worker.
type SupplySource interface {
	Supply(ctx context.Context, tenantID uuid.UUID, q SupplyQuery) (SupplyData, error)
}

// ---------------------------------------------------------------------------
// Router — khoảng cách theo phiên bản mạng
// ---------------------------------------------------------------------------

// Mode là phương thức vận chuyển.
const (
	ModeRoad     = "road"
	ModeWaterway = "waterway"
)

// Nguồn tính khoảng cách (FN-04 BR-04, FN-20 BR-05).
const (
	SourceRouted    = "routing_thuc_te"
	SourceDigitized = "tuyen_so_hoa_noi_bo"
	SourceHaversine = "haversine_hieu_chinh"
)

// RouteRequest là một cặp điểm cần định tuyến.
type RouteRequest struct {
	From, To LatLng
	Mode     string
}

// RouteResult là kết quả định tuyến. Found = false → nền tảng dùng Haversine ×
// hệ số #10 (FN-04 BR-03) và, với đường thủy, coi như không tiếp giáp mạng (FN-08 BR-04b).
type RouteResult struct {
	Found      bool
	DistanceKm decimal.Decimal
	Source     string // SourceRouted | SourceDigitized
}

// Router định tuyến trên mạng có phiên bản (pf/waterway cho đường thủy; OSRM
// hoặc pf/geo cho đường bộ). Kết quả được ghi vào ảnh chụp — không cache.
type Router interface {
	// NetworkVersion trả phiên bản mạng đã phát hành dùng cho lần định tuyến này.
	NetworkVersion(ctx context.Context, tenantID uuid.UUID) (string, error)
	// Route định tuyến theo lô trên đúng phiên bản; len(kết quả) = len(reqs).
	Route(ctx context.Context, tenantID uuid.UUID, networkVersion string, reqs []RouteRequest) ([]RouteResult, error)
}

// ---------------------------------------------------------------------------
// ParamSet — tham số đã phê duyệt
// ---------------------------------------------------------------------------

// ParamKeyPrefix: khóa tham số trong pf/masterdata = "simulation.<mã>" (vd
// simulation.road_freight_rate); giá trị theo chiều thêm ":<chiều>" (vd
// simulation.distance_correction:road, simulation.equipment_price:bale_press).
const ParamKeyPrefix = "simulation."

// ApprovedParam là một phiên bản tham số đã phê duyệt có hiệu lực.
type ApprovedParam struct {
	Value   decimal.Decimal
	Version int
}

// ParamSet tra tham số ĐÃ PHÊ DUYỆT có hiệu lực tại at. found = false → chưa có
// phiên bản phê duyệt: nền tảng dùng giá trị danh mục và kết quả không thể là
// "Chính thức" (FN-01 BR-04).
type ParamSet interface {
	Approved(ctx context.Context, tenantID uuid.UUID, key string, at time.Time) (p ApprovedParam, found bool, err error)
}
