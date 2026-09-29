// Package port là giao diện công khai DUY NHẤT của mkg/hub mà module khác được
// import (R-02). Module của vùng nền tảng (internal/platform/*) KHÔNG được
// import package này (R-03): chúng chỉ thấy payload đi qua van công bố.
package port

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Vai nghiệp vụ của module (tên vai trong Keycloak, tài khoản tenant MKG).
const (
	// RoleHubOperator: vận hành hub — nhận lô, chạy mẻ, xuất, sửa sai bằng bản
	// ghi bù, chụp sức chứa; đề xuất thay đổi dữ liệu chủ cơ sở (Admin API, MFA).
	RoleHubOperator = "hub_operator"
	// RoleHubApprover: phê duyệt / từ chối phiên bản dữ liệu chủ cơ sở — không
	// duyệt đề xuất của chính mình (R-56).
	RoleHubApprover = "hub_approver"
)

// SystemActorRef là actor_ref của tác nhân hệ thống (job công bố sức chứa định kỳ theo ngày,
// GIS US-WH-01 AC-2) — không phải người.
var SystemActorRef = uuid.MustParse("00000000-0000-0000-0000-00000000c0b1")

// JobDailyCapacity là tên job công bố sức chứa định kỳ của api-internal (AG_MKG_JOBS).
const JobDailyCapacity = "hub_capacity_daily"

// Sự kiện module phát ra. CẢ HAI đi qua van công bố (mặc định từ chối): mọi
// sự kiện của vùng mkg lên RabbitMQ đều thấy được từ vùng nền tảng, nên payload
// phải là hợp đồng van (tools/contractcheck cưỡng chế).
const (
	EventFacilityPublished = "mkg.hub.facility.published"
	EventCapacityPublished = "mkg.hub.capacity.published"
)

// Tên hợp đồng van (contracts/publication/<tên>.json).
const (
	ContractHubFacility = "hub_facility"
	ContractHubCapacity = "hub_capacity"
)

// Publication là payload được phép đi qua van. Chỉ struct có chú thích
// //ag:publication <tên> mới được cài đặt interface này (contractcheck kiểm).
type Publication interface {
	PublicationName() string
}

// GeoPoint là tọa độ WGS84 dạng số JSON — json.Number giữ nguyên chuỗi thập
// phân, không đi qua float (R-10).
type GeoPoint struct {
	Lat json.Number `json:"lat"`
	Lng json.Number `json:"lng"`
}

// HubFacilityPublishedV1 là payload của van hub_facility v1.
//
//ag:publication hub_facility
//ag:event mkg.hub.facility.published v1
type HubFacilityPublishedV1 struct {
	HubRef        uuid.UUID       `json:"hub_ref"`
	Code          string          `json:"code"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Location      GeoPoint        `json:"location"`
	Boundary      json.RawMessage `json:"boundary,omitempty"`
	ProvinceCode  string          `json:"province_code"`
	Status        string          `json:"status"`
	CapacityTons  string          `json:"capacity_tons"`
	Unit          string          `json:"unit"`
	EffectiveFrom string          `json:"effective_from"`
}

// PublicationName cài đặt Publication.
func (HubFacilityPublishedV1) PublicationName() string { return "hub_facility" }

// HubCapacityPublishedV1 là payload của van hub_capacity v1.
//
//ag:publication hub_capacity
//ag:event mkg.hub.capacity.published v1
type HubCapacityPublishedV1 struct {
	HubRef        uuid.UUID `json:"hub_ref"`
	AsOf          string    `json:"as_of"`
	CapacityTons  string    `json:"capacity_tons"`
	UsedTons      string    `json:"used_tons"`
	AvailableTons string    `json:"available_tons"`
	Unit          string    `json:"unit"`
}

// PublicationName cài đặt Publication.
func (HubCapacityPublishedV1) PublicationName() string { return "hub_capacity" }

// LotSource là một nguồn của lô (cho module mkg khác, vd mkg/costing quy kết).
type LotSource struct {
	Kind  string    // plot | cooperative | roll_count | supplier | lot
	Ref   uuid.UUID //
	Ratio string    // decimal, 6 chữ số, Σ = 1
}

// LotInfo là thông tin lô cho module mkg khác.
type LotInfo struct {
	LotID       uuid.UUID
	FacilityID  uuid.UUID
	Product     string
	InitialKg   string // mass.Kg dạng chuỗi
	RemainingKg string
	Sources     []LotSource
}

// Lots cho module mkg khác (costing, logistics, measurement) đọc nguồn gốc và
// tồn của lô. Tenant lấy từ context.
type Lots interface {
	LotInfo(ctx context.Context, lotID uuid.UUID) (LotInfo, error)
}

// ---------------------------------------------------------------------------
// G3: consumer sự kiện nội bộ (cmd/worker-internal, vhost mkg.internal) và cảm biến
// ---------------------------------------------------------------------------

// Tên consumer (hàng đợi quorum trên vhost mkg.internal, DLQ = tên + ".dlq").
const (
	// WeighTicketConsumer: mkg.measurement.weigh_ticket.recorded → khối lượng lô nhận
	// (HUB_IN, Q-MKG-04) và gắn phiếu HUB_OUT với phiếu xuất.
	WeighTicketConsumer = "mkg.hub.weigh_ticket"
	// RollCountConsumer: mkg.fieldops.roll_count.recorded → hàng dự kiến về (tùy chọn).
	RollCountConsumer = "mkg.hub.roll_count"
	// ReconciliationConsumer: mkg.measurement.reconciliation.completed → quy kết hao hụt theo cơ sở.
	ReconciliationConsumer = "mkg.hub.reconciliation"
)

// EnvReading là một số đo môi trường kho (độ ẩm %, nhiệt độ °C) của một cơ sở.
type EnvReading struct {
	FacilityID  uuid.UUID
	SensorID    string
	HumidityPct *decimal.Decimal
	TempC       *decimal.Decimal
	At          time.Time
}

// EnvReadings là nguồn số đo cảm biến kho cho hub.risk.v2. G3 chỉ có stub
// NoEnvReadings (chưa có cảm biến); nguồn thật đọc cụm ts schema ts_env (ts
// 000002 dự phòng) khi có — không đổi quy tắc chấm điểm.
type EnvReadings interface {
	Readings(ctx context.Context, tenantID, facilityID uuid.UUID, from, to time.Time) ([]EnvReading, error)
}

// NoEnvReadings là stub: không có cảm biến → không có cảnh báo môi trường.
type NoEnvReadings struct{}

// Readings cài đặt EnvReadings.
func (NoEnvReadings) Readings(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]EnvReading, error) {
	return nil, nil
}
