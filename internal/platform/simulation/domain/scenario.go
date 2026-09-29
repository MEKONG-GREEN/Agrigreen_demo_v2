package domain

import (
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
)

// RuleVersion ghim vào mọi dòng sổ scenario_change.
const RuleVersion = "pf.simulation/2026.09.29"

// Loại bản ghi sổ kịch bản.
const (
	ChangeCreated             = "created"
	ChangeHubAttached         = "hub_attached"
	ChangeHubUpdated          = "hub_updated"
	ChangeHubDetached         = "hub_detached"
	ChangeSimulationRequested = "simulation_requested"
	ChangeSimulationCompleted = "simulation_completed"
	ChangeSimulationFailed    = "simulation_failed"
	ChangeMarkedOfficial      = "marked_official"
)

// Trạng thái việc mô phỏng.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Chế độ baseline (FN-12/FN-17).
const (
	BaselineNoHub    = "no_hub"
	BaselineManual   = "manual"
	BaselineScenario = "scenario"
)

// CandidateHub là Hub ứng viên (FN-02/03).
type CandidateHub struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Code          string
	Name          string
	Location      port.LatLng
	UnitCode      *string
	Status        string
	RetiredReason *string
	CreatedBy     uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Scenario là bảng chiếu kịch bản.
type Scenario struct {
	ID                       uuid.UUID
	TenantID                 uuid.UUID
	Code                     string
	Name                     string
	PlantRef                 *uuid.UUID
	PlantLocation            port.LatLng
	PlantAnnualDemandTons    *decimal.Decimal
	LifecycleYears           int
	Discounted               bool
	BaselineMode             string
	BaselineManualCostPerTon *decimal.Decimal
	BaselineScenarioID       *uuid.UUID
	ReferenceYear            int
	Version                  int64
	CreatedBy                uuid.UUID
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Hubs                     []ScenarioHub
}

// ScenarioHub là cấu hình Hub × kịch bản (FN-02 BR-03).
type ScenarioHub struct {
	ID                 uuid.UUID
	ScenarioID         uuid.UUID
	HubID              uuid.UUID
	HubCode            string
	HubLocation        port.LatLng
	ServiceRadiusKm    decimal.Decimal
	DesignCapacityTons decimal.Decimal
	BargePayloadTons   *decimal.Decimal
	ModeFieldHub       string
	ModeHubPlant       string
	LandMode           string
	SortOrder          int
	Active             bool
	ChangeID           uuid.UUID
}

// HubConfig là phần cấu hình gửi lên; trường nil = giữ giá trị đang lưu, rồi mới
// tới mặc định của bộ tham số (prototype: sửa một ô không reset các ô khác).
type HubConfig struct {
	ServiceRadiusKm    *decimal.Decimal
	DesignCapacityTons *decimal.Decimal
	BargePayloadTons   *decimal.Decimal
	ModeFieldHub       *string
	ModeHubPlant       *string
	LandMode           *string
}

// Lỗi kiểm cấu hình Hub.
var (
	ErrRadius   = errors.New("bán kính phục vụ phải trong 0–500 km")
	ErrCapacity = errors.New("công suất (tồn kho tối đa) không được âm")
	ErrBarge    = errors.New("tải trọng sà lan phải lớn hơn 0")
	ErrMode     = errors.New("phương thức phải là auto, road hoặc waterway")
	ErrLandMode = errors.New("phương án đất phải là mua hoặc thue")
)

var modes = []string{"auto", port.ModeRoad, port.ModeWaterway}

// Apply trộn cấu hình gửi lên vào cấu hình hiện tại (hoặc mặc định) và kiểm.
func (c HubConfig) Apply(cur ScenarioHub) (ScenarioHub, error) {
	out := cur
	if c.ServiceRadiusKm != nil {
		out.ServiceRadiusKm = *c.ServiceRadiusKm
	}
	if c.DesignCapacityTons != nil {
		out.DesignCapacityTons = *c.DesignCapacityTons
	}
	if c.BargePayloadTons != nil {
		out.BargePayloadTons = c.BargePayloadTons
	}
	if c.ModeFieldHub != nil {
		out.ModeFieldHub = *c.ModeFieldHub
	}
	if c.ModeHubPlant != nil {
		out.ModeHubPlant = *c.ModeHubPlant
	}
	if c.LandMode != nil {
		out.LandMode = *c.LandMode
	}
	switch {
	case out.ServiceRadiusKm.IsNegative() || out.ServiceRadiusKm.GreaterThan(decimal.NewFromInt(500)):
		return out, ErrRadius
	case out.DesignCapacityTons.IsNegative():
		return out, ErrCapacity
	case out.BargePayloadTons != nil && !out.BargePayloadTons.IsPositive():
		return out, ErrBarge
	case !slices.Contains(modes, out.ModeFieldHub) || !slices.Contains(modes, out.ModeHubPlant):
		return out, ErrMode
	case out.LandMode != "mua" && out.LandMode != "thue":
		return out, ErrLandMode
	}
	return out, nil
}

// Simulation là việc mô phỏng (= job).
type Simulation struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ScenarioID      uuid.UUID
	ScenarioVersion int64
	Options         SnapOptions
	RequestedBy     uuid.UUID
	RequestedAt     time.Time
	IdemKey         string
	Status          string
	SnapshotID      *uuid.UUID
	ErrorCode       *string
	ErrorDetail     *string
	CompletedAt     *time.Time
	Official        bool
	OfficialBy      *uuid.UUID
	OfficialAt      *time.Time
}

// SnapshotMeta là phần mô tả của ảnh chụp (không kèm payload).
type SnapshotMeta struct {
	ID                uuid.UUID
	SimulationID      uuid.UUID
	SHA256            string
	ParamSetVersion   string
	AllParamsApproved bool
	NetworkVersion    string
	AlgorithmVersion  string
	CooperativeCount  int
	RouteCount        int
	CreatedAt         time.Time
}
