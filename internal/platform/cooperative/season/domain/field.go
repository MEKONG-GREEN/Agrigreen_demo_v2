package domain

import (
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// ---------------------------------------------------------------------------
// GPS (HTX US-GPS-01/02)
// ---------------------------------------------------------------------------

// Nguồn xác thực vị trí — luôn có, không để trống (HTX US-GPS-02 AC-2).
const (
	GPSDevice     = "device"      // GPS của thiết bị
	GPSPhotoEXIF  = "photo_exif"  // toạ độ đính trong ảnh chụp
	GPSManualPick = "manual_pick" // chọn lô thủ công từ danh sách
	GPSDenied     = "denied"      // người dùng từ chối quyền vị trí ("Thiếu GPS")
	GPSNoDevice   = "no_device"   // thiết bị không có GPS ("Không có GPS thiết bị")
)

// Nhãn vị trí.
const (
	LocMatch    = "match"    // ≤ ngưỡng (mặc định 50 m)
	LocMismatch = "mismatch" // "Vị trí không khớp" — vẫn lưu, quản lý thấy khi duyệt
	LocMissing  = "missing"  // không có toạ độ — không bao giờ chặn lưu
)

// DefaultGPSToleranceM là ngưỡng khớp mặc định (m).
var DefaultGPSToleranceM = decimal.NewFromInt(50)

// GPS là vị trí khai báo kèm bản ghi.
type GPS struct {
	Lon, Lat  *decimal.Decimal
	AccuracyM *decimal.Decimal
	Source    string
}

// HasPoint cho biết có toạ độ.
func (g GPS) HasPoint() bool { return g.Lon != nil && g.Lat != nil }

var (
	minLon, maxLon = decimal.NewFromInt(-180), decimal.NewFromInt(180)
	minLat, maxLat = decimal.NewFromInt(-90), decimal.NewFromInt(90)
)

// Normalize kiểm GPS. Nguồn rỗng: có toạ độ → device, không có → denied. Nguồn
// device/photo_exif cần toạ độ; nguồn khác không nhận toạ độ.
func (g *GPS) Normalize() error {
	g.Source = strings.TrimSpace(g.Source)
	if g.Source == "" {
		if g.HasPoint() {
			g.Source = GPSDevice
		} else {
			g.Source = GPSDenied
		}
	}
	switch g.Source {
	case GPSDevice, GPSPhotoEXIF:
		if !g.HasPoint() {
			return FieldError{"gps", "nguồn " + g.Source + " cần lon và lat"}
		}
	case GPSManualPick, GPSDenied, GPSNoDevice:
		if g.Lon != nil || g.Lat != nil {
			return FieldError{"gps", "nguồn " + g.Source + " không kèm toạ độ"}
		}
	default:
		return FieldError{"gps.source", "phải là device, photo_exif, manual_pick, denied hoặc no_device"}
	}
	if g.HasPoint() {
		if g.Lon.LessThan(minLon) || g.Lon.GreaterThan(maxLon) {
			return FieldError{"gps.lon", "ngoài khoảng -180..180"}
		}
		if g.Lat.LessThan(minLat) || g.Lat.GreaterThan(maxLat) {
			return FieldError{"gps.lat", "ngoài khoảng -90..90"}
		}
	}
	if g.AccuracyM != nil && g.AccuracyM.IsNegative() {
		return FieldError{"gps.accuracy_m", "không được âm"}
	}
	return nil
}

// LocationStatus gắn nhãn vị trí từ khoảng cách tới ranh giới thửa (nil = không đo được).
func LocationStatus(distanceM *decimal.Decimal, tolerance decimal.Decimal) string {
	if distanceM == nil {
		return LocMissing
	}
	if distanceM.LessThanOrEqual(tolerance) {
		return LocMatch
	}
	return LocMismatch
}

// ---------------------------------------------------------------------------
// Nhật ký canh tác (HTX US-LOG-01)
// ---------------------------------------------------------------------------

// Giới hạn của nhật ký.
const (
	MaxMachines = 20
	MaxInputs   = 20
	MaxFiles    = 20
	MaxNoteLen  = 2000
)

// Input là vật tư dùng trong hoạt động (tên, lượng decimal, đơn vị).
type Input struct {
	Name string          `json:"name"`
	Qty  decimal.Decimal `json:"qty"`
	Unit string          `json:"unit"`
}

var maxQty = decimal.New(1, 9)

// NormalizeInputs kiểm danh sách vật tư.
func NormalizeInputs(in []Input) ([]Input, error) {
	if len(in) > MaxInputs {
		return nil, FieldError{"inputs", "tối đa 20 vật tư"}
	}
	out := make([]Input, 0, len(in))
	for i, x := range in {
		f := "inputs[" + strconv.Itoa(i) + "]."
		x.Name, x.Unit = strings.TrimSpace(x.Name), strings.TrimSpace(x.Unit)
		if x.Name == "" || len([]rune(x.Name)) > 200 {
			return nil, FieldError{f + "name", "bắt buộc, tối đa 200 ký tự"}
		}
		if ContainsPII(x.Name) {
			return nil, FieldError{f + "name", "không được chứa số điện thoại hay số CCCD"}
		}
		if x.Unit == "" || len([]rune(x.Unit)) > 20 {
			return nil, FieldError{f + "unit", "bắt buộc, tối đa 20 ký tự"}
		}
		if x.Qty.IsNegative() || x.Qty.GreaterThanOrEqual(maxQty) || !x.Qty.Equal(x.Qty.Truncate(4)) {
			return nil, FieldError{f + "qty", "số không âm, tối đa 4 chữ số lẻ"}
		}
		out = append(out, x)
	}
	return out, nil
}

// NormalizeNote kiểm ghi chú: ≤ 2000 ký tự, không chứa SĐT/CCCD.
func NormalizeNote(field, s string) (string, error) {
	s, err := CleanText(field, s, MaxNoteLen)
	if err != nil {
		return "", err
	}
	if ContainsPII(s) {
		return "", FieldError{field, "không được chứa số điện thoại hay số CCCD (dữ liệu cá nhân chỉ ở kho định danh)"}
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Sản lượng (HTX US-YIELD-01..03)
// ---------------------------------------------------------------------------

// Loại bất thường sản lượng.
const (
	AnomalyZero  = "zero"
	AnomalyAbove = "above_threshold"
)

// DefaultYieldAnomalyMax là ngưỡng năng suất bất thường mặc định (t/ha, nguyên mẫu: 12).
var DefaultYieldAnomalyMax = decimal.NewFromInt(12)

var (
	kgPerTonne = decimal.NewFromInt(1000)
	maxYield   = decimal.New(1, 5) // numeric(9,4)
)

// YieldTPerHa tính năng suất t/ha = kg / 1000 / ha, làm tròn 4 chữ số.
func YieldTPerHa(massKg mass.Kg, areaHa decimal.Decimal) decimal.Decimal {
	if !areaHa.IsPositive() {
		return decimal.Zero
	}
	return massKg.Decimal().Div(kgPerTonne).Div(areaHa).Round(4)
}

// YieldTPerHaDec tính năng suất t/ha từ khối lượng kg dạng decimal (tổng hợp).
func YieldTPerHaDec(kg, areaHa decimal.Decimal) decimal.Decimal {
	if !areaHa.IsPositive() {
		return decimal.Zero
	}
	return kg.Div(kgPerTonne).Div(areaHa).Round(4)
}

// ValidYield kiểm năng suất vừa cột numeric(9,4).
func ValidYield(y decimal.Decimal) bool { return y.LessThan(maxYield) }

// ExpectedKg tính sản lượng dự kiến (kg) = ha × t/ha × 1000, làm tròn 3 chữ số.
func ExpectedKg(areaHa, tPerHa decimal.Decimal) decimal.Decimal {
	return areaHa.Mul(tPerHa).Mul(kgPerTonne).Round(mass.Scale)
}

// Anomaly phân loại sản lượng (HTX US-YIELD-03): 0 → zero; năng suất > ngưỡng →
// above_threshold; còn lại rỗng. Âm bị từ chối trước đó (không phải bất thường).
func Anomaly(massKg mass.Kg, yield, threshold decimal.Decimal) string {
	if massKg.Decimal().IsZero() {
		return AnomalyZero
	}
	if threshold.IsPositive() && yield.GreaterThan(threshold) {
		return AnomalyAbove
	}
	return ""
}
