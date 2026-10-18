package port

import (
	"context"
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
