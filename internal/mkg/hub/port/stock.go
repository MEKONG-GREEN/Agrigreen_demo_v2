package port

import (
	"context"

	"github.com/google/uuid"
)

// StockLevel là tồn hiện hành của một dạng hàng tại một cơ sở (Σ remaining_kg của lô còn tồn).
type StockLevel struct {
	FacilityID  uuid.UUID
	Product     string // straw_bale | straw_loose | dried_straw | compressed_bale
	AvailableKg string // mass.Kg dạng chuỗi (3 chữ số thập phân)
	Lots        int    // số lô còn tồn
}

// Stock (G4, cổng ĐỌC thêm — không đổi cổng đã đóng băng) cho module mkg khác đọc tồn theo dạng hàng với
// tenant TƯỜNG MINH, không kiểm vai hub: mkg/orders cảnh báo thiếu tồn khi lập / xác nhận đơn bán (proto
// stockWarning, O-05). facilityID nil = mọi cơ sở của tenant. Tồn ở đây là tồn sổ (lot_balance) — chưa trừ
// phiếu xuất đang chờ duyệt; hub kiểm lại tồn theo lô trong giao dịch khi duyệt xuất (approveGoodsIssue).
// Chỉ đọc; RLS theo tenantID. Sắp theo cơ sở.
type Stock interface {
	StockByProduct(ctx context.Context, tenantID uuid.UUID, product string, facilityID *uuid.UUID) ([]StockLevel, error)
}
