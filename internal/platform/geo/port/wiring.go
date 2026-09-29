package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ---------------------------------------------------------------------------
// Cổng đọc cho module khác (G2 đợt 2, nối ở gốc lắp ráp). Tenant lấy từ context;
// không kiểm phạm vi người gọi — module gọi tự giới hạn.
// ---------------------------------------------------------------------------

// PlotHouseholds cho biết nông hộ nào có thửa đang dùng (nháp hoặc đã gán HTX,
// chưa ngừng dùng) trong các xã — pf/notify tìm nông dân nhận cảnh báo khẩn theo
// địa bàn (HTX US-NEWS-03). communes nil = mọi xã của tenant.
type PlotHouseholds interface {
	HouseholdsInCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error)
}

// CoopCentroid là tâm vùng nguyên liệu của HTX (tâm ranh giới HTX đang dùng).
type CoopCentroid struct {
	CooperativeRef uuid.UUID
	Lon, Lat       decimal.Decimal
}

// CoopCentroids trả tâm ranh giới HTX đang dùng (pf_geo.cooperative_area) — nguồn
// toạ độ #3 của mô phỏng chọn Hub (pf/simulation SupplySource). HTX chưa có ranh
// giới (hoặc đã ngừng dùng) không có trong kết quả; coops nil = mọi HTX.
type CoopCentroids interface {
	CoopCentroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]CoopCentroid, error)
}

// RoleFarmer: nông dân App HTX (hằng chủ ở accounts/port) — chỉ đọc log GPS của mình.
const RoleFarmer = "farmer"

// FarmerHouseholds ánh xạ tài khoản nông dân (actor_ref) sang nông hộ liên kết
// (pf/accounts, qua gốc lắp ráp) — nông dân đọc log GPS của mình và của thửa nhà
// mình (HTX US-GPS-03). Chưa nối → vai farmer không đọc được log.
type FarmerHouseholds interface {
	HouseholdsOf(ctx context.Context, actorRef uuid.UUID) ([]uuid.UUID, error)
}

// ---------------------------------------------------------------------------
// Van công bố MKG → pf/geo (hợp đồng contracts/publication hub_facility v1,
// hub_capacity v1). Tên sự kiện khai lại ở đây vì platform không import mkg (R-03).
// ---------------------------------------------------------------------------
const (
	EventMKGHubFacilityPublished = "mkg.hub.facility.published"
	EventMKGHubCapacityPublished = "mkg.hub.capacity.published"
	// ContractHubFacility, ContractHubCapacity: tên hợp đồng van (contracts/publication).
	ContractHubFacility = "hub_facility"
	ContractHubCapacity = "hub_capacity"
	// HubValveConsumer: consumer của worker nhận hai sự kiện van, kiểm payload bằng
	// bộ kiểm hợp đồng lúc chạy rồi ghi qua HubSink (idempotent theo event_id + phiên bản).
	HubValveConsumer = "pf.geo.hub_valve"
)
