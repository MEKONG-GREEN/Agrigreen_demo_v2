// Package domain: quy tắc thuần của mkg/logistics — máy trạng thái chuyến chỉ tiến, danh mục
// phương tiện (proto GH-01..03), cước chặng và chuyến, cảnh báo tải / đăng kiểm / chưa cân
// (proto FM-05), Haversine thập phân. Không I/O, không time.Now(), không float (R-10, agnofloat).
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Phiên bản quy tắc ghim vào bản ghi sổ.
const (
	RuleTrip     = "logistics.trip.v1"      // máy trạng thái chuyến, FM-05
	RuleTripCost = "logistics.trip_cost.v1" // cước kế hoạch / thực tế (proto createTrip / completeTrip, GH-03)
	RuleRoute    = "logistics.route.v1"     // định tuyến + dự phòng Haversine × #10
	RuleVessel   = "logistics.vessel.v1"    // danh mục ghe GH-01..03
	RuleParam    = "logistics.param.v1"
)

// Cờ cảnh báo (chỉ cảnh báo, không chặn — proto FM-05, GH-02).
const (
	FlagNoVehicle          = "no_vehicle"            // FM-05: chuyến sinh từ xuống ghe, chưa gán phương tiện
	FlagNoRoute            = "no_route"              // chưa có chặng → cước theo km = 0, điều phối lập chặng / xác nhận cước
	FlagVesselUnregistered = "vessel_unregistered"   // phương tiện không có trong danh mục
	FlagVesselInactive     = "vessel_inactive"       // phương tiện ngừng hoạt động
	FlagRegistrationExpire = "registration_expired"  // GH-02: hết hạn đăng kiểm
	FlagRegistrationSoon   = "registration_expiring" // GH-02: còn ≤ 30 ngày
	FlagOverload           = "payload_exceeded"      // FM-05: tấn ước > 1,1 × tải rơm thực chở
	FlagWeighVariance      = "weigh_variance"        // plant weighing: lệch > 5 % so với tấn ước
	FlagRoutingUnavailable = "routing_unavailable"   // nền tảng lỗi / thiếu cấp quyền → dự phòng
	FlagNoWaterwayRoute    = "no_waterway_route"     // mạng không có đường thủy giữa hai điểm → dự phòng
	FlagWaterwaySuggested  = "waterway_suggested"    // decideMode: đường bộ xa hơn #27 và có kênh tiếp cận
	FlagRollsCorrected     = "rolls_corrected"       // đếm cuộn được bù → tấn ước cập nhật
)

// WeighVariancePct là ngưỡng cờ lệch cân ↔ tấn ước (proto plant weighing, 5 %).
var WeighVariancePct = decimal.NewFromInt(5)

// ErrTransition: chuyển trạng thái chuyến bị cấm (chỉ tiến).
var ErrTransition = errors.New("logistics: chuyển trạng thái chuyến bị cấm")

// ErrTerminal: chuyến đã hoàn tất / đã hủy.
var ErrTerminal = errors.New("logistics: chuyến đã kết thúc")

// FieldError là lỗi kiểm dữ liệu một trường.
type FieldError struct{ Field, Msg string }

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

func rank(s port.TripStatus) int {
	switch s {
	case port.TripPlanned:
		return 1
	case port.TripDeparted:
		return 2
	case port.TripArrived:
		return 3
	case port.TripCompleted:
		return 4
	}
	return 0
}

// CanTransition kiểm chuyển trạng thái (L-02): planned → departed → arrived → completed,
// planned|departed → cancelled. Tiến nhảy bậc được phép duy nhất khi đóng chuyến bằng phiếu cân /
// xác nhận hoàn tất (to = completed) — proto plant weighing đóng chuyến chưa ghi mốc. Mốc tài xế
// phải đi từng bậc.
func CanTransition(from, to port.TripStatus) error {
	if from == port.TripCompleted || from == port.TripCancelled {
		return ErrTerminal
	}
	switch to {
	case port.TripCancelled:
		if from == port.TripPlanned || from == port.TripDeparted {
			return nil
		}
	case port.TripCompleted:
		if rank(from) >= 1 {
			return nil
		}
	case port.TripDeparted, port.TripArrived:
		if rank(to) == rank(from)+1 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s → %s", ErrTransition, from, to)
}

// TripCode là mã chuyến CH + 6 số (proto sequenceCode('CH', n, 6)).
func TripCode(n int64) string { return fmt.Sprintf("CH%06d", n) }

// CheckReason kiểm lý do 10..500 ký tự; required=false cho phép rỗng.
func CheckReason(field, s string, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		if required {
			return "", FieldError{field, "bắt buộc, 10–500 ký tự"}
		}
		return "", nil
	}
	if n := len([]rune(s)); n < 10 || n > 500 {
		return "", FieldError{field, "10–500 ký tự"}
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Danh mục phương tiện (proto GH-01..03)
// ---------------------------------------------------------------------------

// Loại phương tiện.
const (
	KindBoat  = "ghe"
	KindBarge = "sa_lan"
	KindTruck = "truck"
)

// Loại đơn giá thuê (GH-03).
const (
	RatePerTon   = "per_ton"
	RatePerTrip  = "per_trip"
	RatePerTonKm = "per_ton_km"
)

// NormalizeCode chuẩn hóa số hiệu phương tiện: bỏ mọi khoảng trắng, chữ hoa (proto GH-01).
func NormalizeCode(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// Vessel là một phiên bản danh mục phương tiện.
type Vessel struct {
	Code               string
	Kind               string
	Name               string
	RegisteredTons     *decimal.Decimal
	StrawPayloadTons   *decimal.Decimal
	RegistrationExpiry *time.Time
	RateType           string
	RateVND            *decimal.Decimal
	Active             bool
}

// ValidateVessel kiểm GH-01: số hiệu có, loại ghe/sà lan/xe, tải > 0, có loại đơn giá thì đơn giá > 0.
func ValidateVessel(v Vessel) error {
	if v.Code == "" {
		return FieldError{"code", "GH-01: phải ghi số hiệu ghe / sà lan"}
	}
	if len(v.Code) < 2 || len(v.Code) > 32 || strings.Trim(v.Code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-") != "" ||
		strings.ContainsAny(v.Code[:1], ".-") {
		return FieldError{"code", "2–32 ký tự A–Z, 0–9, '.', '-'"}
	}
	switch v.Kind {
	case KindBoat, KindBarge, KindTruck:
	default:
		return FieldError{"kind", "ghe | sa_lan | truck"}
	}
	if v.RegisteredTons != nil && v.RegisteredTons.Sign() <= 0 {
		return FieldError{"registered_tons", "tải trọng đăng ký phải lớn hơn 0"}
	}
	if v.StrawPayloadTons != nil && v.StrawPayloadTons.Sign() <= 0 {
		return FieldError{"straw_payload_tons", "tải rơm thực chở phải lớn hơn 0"}
	}
	switch v.RateType {
	case "":
		if v.RateVND != nil {
			return FieldError{"rate_type", "có đơn giá thì phải có loại đơn giá"}
		}
	case RatePerTon, RatePerTrip, RatePerTonKm:
		if v.RateVND == nil || v.RateVND.Sign() <= 0 {
			return FieldError{"rate_vnd", "GH-01: có loại đơn giá thì đơn giá phải lớn hơn 0"}
		}
	default:
		return FieldError{"rate_type", "per_ton | per_trip | per_ton_km"}
	}
	return nil
}

// Trạng thái đăng kiểm (GH-02).
const (
	ExpiryUnknown  = "unknown"
	ExpiryValid    = "valid"
	ExpirySoon     = "expiring"
	ExpiryExpired  = "expired"
	dayDurationSec = 86400
)

// ExpiryStatus trả trạng thái đăng kiểm tại ngày today (ngày nghiệp vụ) và số ngày còn lại.
// Chỉ cảnh báo — không bao giờ chặn xếp hàng (proto GH-02).
func ExpiryStatus(expiry *time.Time, today time.Time, warnDays int) (string, *int) {
	if expiry == nil {
		return ExpiryUnknown, nil
	}
	e := time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	days := int(e.Sub(t) / (dayDurationSec * time.Second))
	switch {
	case days < 0:
		return ExpiryExpired, &days
	case days <= warnDays:
		return ExpirySoon, &days
	}
	return ExpiryValid, &days
}

// ExpiryFlag quy trạng thái đăng kiểm về cờ cảnh báo ("" = không cảnh báo).
func ExpiryFlag(state string) string {
	switch state {
	case ExpiryExpired:
		return FlagRegistrationExpire
	case ExpirySoon:
		return FlagRegistrationSoon
	}
	return ""
}

// Overloaded cho biết tấn ước vượt factor × tải rơm thực chở (proto FM-05: > 1,1 × tải).
func Overloaded(est mass.Kg, payloadTons, factor decimal.Decimal) bool {
	limit := payloadTons.Mul(factor).Mul(decimal.NewFromInt(1000))
	return est.Decimal().GreaterThan(limit)
}

// VariancePct là (thật − ước) / ước × 100, làm tròn 0,1 (nil khi ước = 0).
func VariancePct(planned, actual mass.Kg) *decimal.Decimal {
	if planned.Decimal().Sign() <= 0 {
		return nil
	}
	v := actual.Decimal().Sub(planned.Decimal()).Div(planned.Decimal()).Mul(decimal.NewFromInt(100)).Round(1)
	return &v
}

// ---------------------------------------------------------------------------
// Cước (proto createTrip / completeTrip, GH-03)
// ---------------------------------------------------------------------------

// Tonnes đổi kg sang tấn (decimal, không làm tròn).
func Tonnes(k mass.Kg) decimal.Decimal { return k.Decimal().Div(decimal.NewFromInt(1000)) }

// Amount làm tròn về đồng (numeric(18,2), làm tròn tới đồng như prototype Math.round).
func Amount(d decimal.Decimal) money.Amount {
	a, err := money.ParseAmount(d.Round(0).StringFixed(2))
	if err != nil {
		return money.MustAmount("0")
	}
	return a
}

// LegCost là cước một chặng = tấn × km × đơn giá (đ/t·km).
func LegCost(k mass.Kg, km, rate decimal.Decimal) money.Amount {
	return Amount(Tonnes(k).Mul(km).Mul(rate))
}

// VesselCost là cước theo đơn giá thuê ghe (GH-03): per_ton = tấn × giá; per_trip = giá;
// per_ton_km = tấn × Σkm × giá. ok=false khi phương tiện không có đơn giá.
func VesselCost(rateType string, rate *decimal.Decimal, actual mass.Kg, totalKm decimal.Decimal) (money.Amount, bool) {
	if rateType == "" || rate == nil || rate.Sign() <= 0 {
		return money.Amount{}, false
	}
	switch rateType {
	case RatePerTon:
		return Amount(Tonnes(actual).Mul(*rate)), true
	case RatePerTrip:
		return Amount(*rate), true
	case RatePerTonKm:
		return Amount(Tonnes(actual).Mul(totalKm).Mul(*rate)), true
	}
	return money.Amount{}, false
}

// EstimateKg là tấn ước theo cuộn = cuộn × kg/cuộn (proto FM-09).
func EstimateKg(rolls int, kgPerRoll mass.Kg) mass.Kg {
	d := kgPerRoll.Decimal().Mul(decimal.NewFromInt(int64(rolls))).Round(mass.Scale)
	k, err := mass.ParseKg(d.StringFixed(mass.Scale))
	if err != nil {
		return mass.MustKg("0")
	}
	return k
}

// Kg dựng mass.Kg từ decimal (làm tròn 3 chữ số).
func Kg(d decimal.Decimal) mass.Kg {
	k, err := mass.ParseKg(d.Round(mass.Scale).StringFixed(mass.Scale))
	if err != nil {
		return mass.MustKg("0")
	}
	return k
}

// AllocateKg chia tổng theo trọng số (tấn ước của từng chặng / lô); phần dư làm tròn vào phần
// cuối để Σ = tổng. Trọng số toàn 0 → chia đều.
func AllocateKg(total mass.Kg, weights []mass.Kg) []mass.Kg {
	out := make([]mass.Kg, len(weights))
	if len(weights) == 0 {
		return out
	}
	sum := decimal.Zero
	for _, w := range weights {
		sum = sum.Add(w.Decimal())
	}
	acc := decimal.Zero
	for i, w := range weights {
		if i == len(weights)-1 {
			out[i] = Kg(total.Decimal().Sub(acc))
			break
		}
		var share decimal.Decimal
		if sum.Sign() > 0 {
			share = total.Decimal().Mul(w.Decimal()).Div(sum).Round(mass.Scale)
		} else {
			share = total.Decimal().Div(decimal.NewFromInt(int64(len(weights)))).Round(mass.Scale)
		}
		out[i] = Kg(share)
		acc = acc.Add(share)
	}
	return out
}

// ---------------------------------------------------------------------------
// Haversine thập phân (R-10) — dự phòng đường bộ / khi định tuyến đường thủy lỗi (#10)
// ---------------------------------------------------------------------------

var (
	earthRadiusKm = decimal.RequireFromString("6371.0088") // IUGG, cùng bán kính với pf/waterway
	pi            = decimal.RequireFromString("3.14159265358979323846")
	two           = decimal.NewFromInt(2)
)

func radians(deg decimal.Decimal) decimal.Decimal { return deg.Mul(pi).Div(decimal.NewFromInt(180)) }

// Sqrt là căn bậc hai thập phân (Newton, 20 chữ số) — không float.
func Sqrt(x decimal.Decimal) decimal.Decimal {
	if x.Sign() <= 0 {
		return decimal.Zero
	}
	g := x
	if x.LessThan(decimal.NewFromInt(1)) {
		g = decimal.NewFromInt(1)
	}
	for range 60 {
		n := g.Add(x.DivRound(g, 20)).Div(two).Round(20)
		if n.Equal(g) {
			break
		}
		g = n
	}
	return g
}

// HaversineKm là khoảng cách vòng lớn (km, chưa nhân hệ số, 3 chữ số thập phân).
func HaversineKm(a, b port.GeoPoint) decimal.Decimal {
	dLat := radians(b.Lat.Sub(a.Lat)).Div(two)
	dLng := radians(b.Lng.Sub(a.Lng)).Div(two)
	sLat, sLng := dLat.Sin(), dLng.Sin()
	h := sLat.Mul(sLat).Add(radians(a.Lat).Cos().Mul(radians(b.Lat).Cos()).Mul(sLng).Mul(sLng))
	if h.GreaterThan(decimal.NewFromInt(1)) {
		h = decimal.NewFromInt(1)
	}
	y, x := Sqrt(h), Sqrt(decimal.NewFromInt(1).Sub(h))
	var c decimal.Decimal
	if x.Sign() == 0 {
		c = pi // atan2(y, 0) × 2 = π
	} else {
		c = two.Mul(y.DivRound(x, 20).Atan())
	}
	return earthRadiusKm.Mul(c).Round(3)
}

// ValidPoint kiểm tọa độ WGS84.
func ValidPoint(p port.GeoPoint) bool {
	return p.Lat.GreaterThanOrEqual(decimal.NewFromInt(-90)) && p.Lat.LessThanOrEqual(decimal.NewFromInt(90)) &&
		p.Lng.GreaterThanOrEqual(decimal.NewFromInt(-180)) && p.Lng.LessThanOrEqual(decimal.NewFromInt(180))
}

// ---------------------------------------------------------------------------
// Tham số (tạm, chờ khách — g4-conventions §1.2)
// ---------------------------------------------------------------------------

// ParamSpec là mô tả một khóa tham số.
type ParamSpec struct {
	Key     string
	Default decimal.Decimal
	Min     decimal.Decimal
	Max     decimal.Decimal
	Integer bool
}

// Params liệt kê khóa tham số của module với mặc định tạm của prototype.
var Params = []ParamSpec{
	{port.ParamRoadRatePerTonKm, decimal.NewFromInt(2200), decimal.NewFromInt(1), decimal.NewFromInt(100000), false},       // #22
	{port.ParamWaterwayRatePerTonKm, decimal.NewFromInt(900), decimal.NewFromInt(1), decimal.NewFromInt(100000), false},    // #23
	{port.ParamRoadFeasibleKm, decimal.NewFromInt(120), decimal.NewFromInt(1), decimal.NewFromInt(2000), false},            // #27 (prototype 120)
	{port.ParamStrawPayloadTons, decimal.NewFromInt(90), decimal.NewFromInt(1), decimal.NewFromInt(5000), false},           // #51
	{port.ParamPayloadWarnFactor, decimal.RequireFromString("1.1"), decimal.NewFromInt(1), decimal.NewFromInt(3), false},   // FM-05
	{port.ParamExpiryWarnDays, decimal.NewFromInt(30), decimal.NewFromInt(1), decimal.NewFromInt(365), true},               // GH-02
	{port.ParamUnweighedAlertHours, decimal.NewFromInt(48), decimal.NewFromInt(1), decimal.NewFromInt(720), true},          // notify
	{port.ParamRoadHaversineFactor, decimal.RequireFromString("1.3"), decimal.NewFromInt(1), decimal.NewFromInt(3), false}, // #10
}

// Spec trả mô tả khóa; ok=false khi khóa lạ.
func Spec(key string) (ParamSpec, bool) {
	for _, p := range Params {
		if p.Key == key {
			return p, true
		}
	}
	return ParamSpec{}, false
}

// CheckParamValue kiểm giá trị tham số.
func CheckParamValue(s ParamSpec, v decimal.Decimal) error {
	if v.LessThan(s.Min) || v.GreaterThan(s.Max) {
		return FieldError{"value", fmt.Sprintf("trong khoảng %s–%s", s.Min, s.Max)}
	}
	if s.Integer && !v.Equal(v.Truncate(0)) {
		return FieldError{"value", "số nguyên"}
	}
	if !v.Equal(v.Truncate(4)) {
		return FieldError{"value", "tối đa 4 chữ số thập phân"}
	}
	return nil
}
