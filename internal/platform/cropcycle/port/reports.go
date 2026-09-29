package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// ProductionQuery là truy vấn báo cáo sản lượng HTX (HTX US-DASH-02, US-DASH-03)
// của pf/reports. Phạm vi ĐÃ GIẢI lúc yêu cầu xuất (worker không có token):
//   - CoopOnly: quản lý HTX — chỉ HTX CooperativeRef (bắt buộc), bỏ qua Scope;
//   - ngược lại Scope (All = toàn bộ tenant; Units = mã xã/tỉnh của cán bộ KN;
//     rỗng = không thấy gì) và CooperativeRef (nếu có) là bộ lọc thêm.
//
// Chiều phân nhóm: By* = false thì chiều đó được gộp (trống trong dòng).
type ProductionQuery struct {
	TenantID       uuid.UUID
	Scope          authz.Scope
	CoopOnly       bool
	CooperativeRef uuid.UUID
	SeasonCode     string // rỗng = mọi vụ
	SeasonYear     int    // 0 = mọi năm
	VarietyCode    string // rỗng = mọi giống
	ByCooperative  bool
	BySeason       bool
	ByVariety      bool
}

// ProductionFigures là số liệu dự kiến – thực tế của một nhóm, cùng cách tính với
// bảng điều hành (HTX US-DASH-01): vụ bị hủy không tính; tiến độ SOP chỉ tính nhật
// ký ĐÃ DUYỆT (HTX US-LOG-05 AC-2); năng suất thực tế = Σ thực tế / Σ diện tích
// đã khai sản lượng. Households đếm phân biệt trong nhóm (không cộng được giữa nhóm).
type ProductionFigures struct {
	Seasons           int64
	OpenSeasons       int64
	CompletedSeasons  int64
	Households        int64
	AreaHa            decimal.Decimal
	HarvestedAreaHa   decimal.Decimal
	ExpectedKg        mass.Kg
	ActualKg          mass.Kg
	ActualYieldTPerHa *decimal.Decimal // nil = chưa có khai sản lượng
	SOPSteps          int64
	SOPStepsDone      int64
	SOPProgressPct    *decimal.Decimal // nil = không có bước SOP
}

// ProductionRow là một dòng theo HTX × vụ × giống (chiều không chọn để trống).
type ProductionRow struct {
	CooperativeRef uuid.UUID
	SeasonCode     string
	SeasonYear     int
	VarietyCode    string
	ProductionFigures
}

// ProductionData là kết quả: các dòng và dòng tổng (tính lại trên toàn tập, không
// cộng các dòng — số hộ phân biệt). Không có dữ liệu cá nhân.
type ProductionData struct {
	Rows   []ProductionRow
	Totals ProductionFigures
}

// ReportData là cổng đọc số liệu báo cáo của pf/cropcycle cho pf/reports (qua gốc
// lắp ráp). Không kiểm vai người gọi — pf/reports đã kiểm vai và giải phạm vi.
type ReportData interface {
	Production(ctx context.Context, q ProductionQuery) (ProductionData, error)
}
