package port

import (
	"context"

	"github.com/google/uuid"
)

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
