package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleVersion ghim vào mọi bản ghi sổ của pf/geo (mục 8.1).
const RuleVersion = "geo.plot.v1"

// Nguồn của một phiên bản ranh giới.
const (
	SourceKNWeb      = "kn_web"
	SourceKNFieldApp = "kn_field_app"
	SourceHTXApp     = "htx_app"
	SourceImportFile = "import_file"
	SourceCadastral  = "cadastral"
)

// KnownSource cho biết nguồn hợp lệ.
func KnownSource(s string) bool {
	switch s {
	case SourceKNWeb, SourceKNFieldApp, SourceHTXApp, SourceImportFile, SourceCadastral:
		return true
	}
	return false
}

// Hành động trên sổ plot_event.
const (
	ActionCreated  = "created"
	ActionAssigned = "assigned"
	ActionRetired  = "retired"
)

// Loại xung đột ranh giới.
const (
	ConflictSameCoop   = "same_coop_overlap"  // chồng lấn cùng HTX / với thửa nháp — đã được xác nhận giữ nguyên, chờ xem xét
	ConflictCrossCoop  = "cross_coop_overlap" // chồng lấn với thửa của HTX khác — chỉ có từ đồng bộ ngoại tuyến (API chặn)
	ConflictConcurrent = "concurrent_edit"    // hai bản sửa ngoại tuyến cùng dựa trên một phiên bản cũ
)

// ConflictSLA là hạn xử lý xung đột — tạm 72 giờ theo C-8 (mục 15, chờ MKG ký).
const ConflictSLA = 72 * time.Hour

// Giới hạn diện tích hợp lý của một thửa (ha).
var (
	MinAreaHa = decimal.RequireFromString("0.001") // 10 m²
	MaxAreaHa = decimal.RequireFromString("1000")
)

// DefaultOverlapTolerancePct dùng khi chưa có tham số geo.overlap_tolerance_pct được duyệt.
var DefaultOverlapTolerancePct = decimal.NewFromInt(1)

// DefaultAreaDeviationMaxPct: HTX US-PLOT-02 AC-3 — sai lệch diện tích ước tính dưới 5 %
// so với diện tích khai báo (tham số geo.area_deviation_max_pct).
var DefaultAreaDeviationMaxPct = decimal.NewFromInt(5)

// AreaDeviationPct là |đo − khai báo| / khai báo × 100; 0 nếu khai báo không dương.
func AreaDeviationPct(measured, declared decimal.Decimal) decimal.Decimal {
	if !declared.IsPositive() {
		return decimal.Zero
	}
	return measured.Sub(declared).Abs().Mul(decimal.NewFromInt(100)).Div(declared).Round(4)
}

// StreamKey của thửa trên cả hai sổ.
func StreamKey(plotRef uuid.UUID) string { return "plot:" + plotRef.String() }

// ValidReason kiểm lý do sửa/ngừng dùng: ít nhất 10 ký tự sau khi bỏ khoảng trắng.
func ValidReason(s string) bool { return utf8.RuneCountInString(strings.TrimSpace(s)) >= 10 }

// OverlapPct là phần trăm diện tích chồng lấn so với thửa NHỎ HƠN trong hai thửa —
// bắt được cả thửa nhỏ nằm gọn trong thửa lớn.
func OverlapPct(intersectHa, aHa, bHa decimal.Decimal) decimal.Decimal {
	base := decimal.Min(aHa, bHa)
	if !base.IsPositive() {
		return decimal.Zero
	}
	return intersectHa.Mul(decimal.NewFromInt(100)).Div(base).Round(4)
}

// Overlap là một thửa đã có bị ranh giới mới đè lên.
type Overlap struct {
	PlotRef        uuid.UUID
	Code           string
	CooperativeRef uuid.UUID // Nil = thửa nháp chưa gán HTX
	IntersectHa    decimal.Decimal
	Pct            decimal.Decimal
}

// ClassifyOverlaps tách chồng lấn vượt ngưỡng theo KN US-PLOT-03, HTX US-PLOT-04 AC-2
// và nguyên mẫu (mdm/lifecycle.ts createPlotChecked):
//   - same (cần xác nhận): thửa cùng HTX, hoặc một trong hai bên là thửa nháp chưa gán HTX;
//   - cross (bị chặn): cả hai đã gán HTX và khác HTX.
func ClassifyOverlaps(coop uuid.UUID, hits []Overlap, tolerancePct decimal.Decimal) (same, cross []Overlap) {
	for _, h := range hits {
		if h.Pct.LessThanOrEqual(tolerancePct) {
			continue
		}
		if coop == uuid.Nil || h.CooperativeRef == uuid.Nil || h.CooperativeRef == coop {
			same = append(same, h)
		} else {
			cross = append(cross, h)
		}
	}
	return same, cross
}

// MaxPct là phần trăm chồng lấn lớn nhất.
func MaxPct(hs []Overlap) decimal.Decimal {
	m := decimal.Zero
	for _, h := range hs {
		m = decimal.Max(m, h.Pct)
	}
	return m
}

// StatusFor trả trạng thái thửa theo việc đã gán HTX hay chưa.
func StatusFor(coop uuid.UUID) string {
	if coop == uuid.Nil {
		return "draft"
	}
	return "active"
}
