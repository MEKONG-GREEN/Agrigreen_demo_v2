package domain

import (
	"errors"

	"github.com/shopspring/decimal"
)

// RuleVersion là phiên bản quy tắc dựng topology và suy tải trọng; ghim vào sổ
// và phiên bản mạng. Đổi thuật toán → tăng số.
const RuleVersion = "waterway/v1"

// Tolerances là ngưỡng dựng mạng (đặc tả prototype gis/waterwayNetwork.ts).
type Tolerances struct {
	// SnapM: hai điểm cách ≤ giá trị được coi là cùng một đỉnh (25 m).
	SnapM decimal.Decimal `json:"snap_m"`
	// AutoJoinMaxM: khe hở giữa hai đầu tuyến ≤ giá trị được nối tự động (150 m) —
	// lỗi số hóa. Xa hơn rất có thể là hai con kênh khác nhau: chỉ báo cáo.
	AutoJoinMaxM decimal.Decimal `json:"auto_join_max_m"`
	// ReportGapMaxM: khe hở ≤ giá trị được báo cáo (1 km); xa hơn không báo (nhiễu).
	ReportGapMaxM decimal.Decimal `json:"report_gap_max_m"`
	// OnSegmentM: đầu tuyến cách thân tuyến khác ≤ giá trị là nối chữ T thiếu đỉnh (20 m).
	OnSegmentM decimal.Decimal `json:"on_segment_m"`
}

// BuildParams là ảnh chụp tham số của một lần dựng (lưu trên job và phiên bản).
type BuildParams struct {
	Tolerances Tolerances    `json:"tolerances"`
	Safety     SafetyMargins `json:"safety"`
	Vessels    []VesselClass `json:"vessels"`
}

// DefaultTolerances là ngưỡng mặc định khi pf/masterdata chưa có tham số.
func DefaultTolerances() Tolerances {
	return Tolerances{
		SnapM:         decimal.NewFromInt(25),
		AutoJoinMaxM:  decimal.NewFromInt(150),
		ReportGapMaxM: decimal.NewFromInt(1000),
		OnSegmentM:    decimal.NewFromInt(20),
	}
}

// DefaultParams là tham số mặc định đầy đủ.
func DefaultParams() BuildParams {
	return BuildParams{Tolerances: DefaultTolerances(), Safety: DefaultSafety(), Vessels: DefaultVessels()}
}

// Mặc định của các tham số không thuộc lần dựng.
var (
	DefaultStructureMaxOffsetM = decimal.NewFromInt(500)
	DefaultAccessRadiusM       = decimal.NewFromInt(5000)
	DefaultHaversineFactor     = decimal.RequireFromString("1.3")
)

// Validate kiểm tham số trước khi dựng.
func (p BuildParams) Validate() error {
	t := p.Tolerances
	switch {
	case !t.SnapM.IsPositive() || !t.OnSegmentM.IsPositive():
		return errors.New("ngưỡng snap và nối chữ T phải > 0")
	case t.AutoJoinMaxM.LessThan(t.SnapM) || t.ReportGapMaxM.LessThan(t.AutoJoinMaxM):
		return errors.New("cần snap ≤ ngưỡng nối tự động ≤ ngưỡng báo cáo")
	case !p.Safety.WidthFactor.IsPositive() || p.Safety.UnderKeelM.IsNegative() || p.Safety.AirGapM.IsNegative():
		return errors.New("biên an toàn không hợp lệ")
	case len(p.Vessels) == 0:
		return errors.New("cần ít nhất một lớp phương tiện")
	}
	for _, v := range p.Vessels {
		if v.Code == "" || !v.Tons.IsPositive() || !v.BeamM.IsPositive() || !v.DraftM.IsPositive() || !v.AirDraftM.IsPositive() {
			return errors.New("lớp phương tiện " + v.Code + " thiếu kích thước")
		}
	}
	return nil
}
