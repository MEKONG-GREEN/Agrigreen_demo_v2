package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CooperativeStatistics chỉ đọc số HTX/hộ theo xã; nil = mọi xã, tập rỗng = không xã nào.
type CooperativeStatistics interface {
	ByUnit(context.Context, []string) ([]UnitCooperatives, error)
}
type UnitCooperatives struct {
	UnitCode     string
	Cooperatives int64
	Households   int64
}

// PlotStatistics không cung cấp hồ sơ, hình học hoặc thao tác ghi HTX.
type PlotStatistics interface {
	DigitizedByCommune(context.Context, []string) ([]CommunePlots, error)
}
type CommunePlots struct {
	CommuneCode string
	Plots       int64
	AreaHa      decimal.Decimal
}

// HTXData is the KN-owned read contract exposed at the module composition boundary.
// It offers no write operation or HTX implementation handle.
type HTXData interface {
	CooperativeStatistics
	PlotStatistics
	HTXConfirmations
	HTXConfirmationHistory
}

// PlotDirectory tra vị trí một thửa qua publication pf_cooperative_pub.plots/v1 (BF-SRV-KN-ATTRS, #119): chỉ xã,
// HTX, trạng thái — không hình học, tên, hộ. Thửa ngoài phạm vi RLS của người gọi coi như không có (ok = false).
type PlotDirectory interface {
	PlotLocation(ctx context.Context, plotRef uuid.UUID) (PlotLocation, bool, error)
}

// PlotLocation là vị trí công bố của một thửa.
type PlotLocation struct {
	PlotRef        uuid.UUID
	CommuneCode    string
	CooperativeRef uuid.UUID // Nil = thửa chưa gán HTX
	Status         string    // draft | active | retired
}

// PlotRetired là trạng thái thửa đã ngừng dùng trong publication.
const PlotRetired = "retired"
