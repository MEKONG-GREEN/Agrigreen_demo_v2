package port

// Phần THÊM của G3-2 (tích hợp fieldops ↔ measurement ↔ hub). Chỉ interface và hằng
// mới — không đổi hợp đồng đóng băng ở port.go (docs/dev/g3-conventions.md §6).
//
// ÁNH XẠ LÔ (quyết định G3-2, docs/design/mkg.md §5.2):
//   - mkg/fieldops: lô gom = lệnh gom (roll_count stream 'lot:<order_id>'; RollCountRecordedV1.LotRef = order_id).
//   - mkg/measurement: phiếu cân mang lot_ref của LÔ CÂN; khi qua hub, lô hub nhận bằng
//     weigh_ticket_ref nên id lô hub = lot_ref của phiếu HUB_IN.
//   - Liên kết TƯỜNG MINH lô cân ↔ lệnh gom nằm ở measurement (sổ lot_collection): ghi khi
//     mở phiên cân có collection_order_ref, hoặc qua POST /v1/measurement/lots/{id}/collection
//     (lô DIRECT chỉ có CUSTOMER_IN). Một lệnh gom thuộc tối đa một lô cân và ngược lại.
//   - Không có liên kết → quy ước ĐỒNG NHẤT: lot_ref của lô cân chính là mã lệnh gom
//     (hành vi G3-1 của fieldops LotMassWeighed và hub expected_inbound giữ nguyên).
//   - Lệnh gom nhiều ghe/xe (prototype: 4 400 + 1 300 cuộn) ở G3 phải tách thành nhiều lệnh
//     gom (mỗi lần xếp hàng một lệnh); lô chuyến hàng thật thuộc mkg/logistics (G4).

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CollectionLink là liên kết hiện hành lô cân ↔ lệnh gom.
type CollectionLink struct {
	LotRef             uuid.UUID
	CollectionOrderRef uuid.UUID
	// Explicit = false: không có dòng sổ, áp quy ước đồng nhất (LotRef = CollectionOrderRef).
	Explicit bool
}

// CollectionLinks phân giải hai chiều lô cân ↔ lệnh gom trong tenant (tenant tường minh,
// không cần principal — dùng từ adapter cùng tiến trình và consumer nội bộ). Không bao
// giờ lỗi "không có": thiếu liên kết trả quy ước đồng nhất với Explicit = false.
type CollectionLinks interface {
	CollectionOfLot(ctx context.Context, tenantID, lotRef uuid.UUID) (CollectionLink, error)
	LotOfCollection(ctx context.Context, tenantID, collectionOrderRef uuid.UUID) (CollectionLink, error)
}

// RollTotalsAsOf là RollTotals có mốc thời điểm cân: ước lượng kg/cuộn chỉ học từ các
// lô gom đã cân TRƯỚC weighedAt và không gồm chính lô gom đó — để đối soát tất định
// (chạy lại cùng phiếu → cùng ước lượng) và không tự so với chính mình. measurement
// dùng interface này nếu hiện thực RollTotals có (kiểm bằng type assertion).
//
// Tham số lotRef của RollTotals/RollTotalsAsOf là MÃ LỆNH GOM (lô gom) — measurement
// đã phân giải lô cân → lệnh gom qua CollectionLinks trước khi gọi.
type RollTotalsAsOf interface {
	RollTotalAsOf(ctx context.Context, tenantID, collectionOrderRef uuid.UUID, weighedAt time.Time) (RollTotal, error)
}

// PlotShare là tỷ lệ của một thửa trong lô gom (decimal 6 chữ số, Σ = 1).
type PlotShare struct {
	PlotRef uuid.UUID
	Ratio   string
}

// CollectionPlots cho van MRV tuyến DIRECT (không có lô hub): tỷ lệ thửa của lệnh gom
// theo số cuộn hiệu lực ở công đoạn cuối (mkg/fieldops). ErrNotFound khi không có lệnh gom.
type CollectionPlots interface {
	CollectionPlots(ctx context.Context, tenantID, collectionOrderRef uuid.UUID) ([]PlotShare, error)
}

// Căn cứ khối lượng của bằng chứng MRV (trường mass_basis của van mrv_collection_evidence).
const (
	// MassBasisHubIn: phiếu HUB_IN của lô qua hub.
	MassBasisHubIn = "HUB_IN"
	// MassBasisCustomerInAggregated: tuyến DIRECT — phiếu CUSTOMER_IN của khách, GỘP (không
	// tách hao hụt vận chuyển / ẩm), chỉ dùng làm chỉ báo.
	MassBasisCustomerInAggregated = "CUSTOMER_IN_AGGREGATED"
)
