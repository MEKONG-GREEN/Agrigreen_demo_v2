package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// L-02: trạng thái chuyến chỉ tiến (Go cưỡng chế — prototype không).
func TestLogisticsL02CanTransitionForwardOnly(t *testing.T) {
	ok := [][2]port.TripStatus{
		{port.TripPlanned, port.TripDeparted}, {port.TripDeparted, port.TripArrived}, {port.TripArrived, port.TripCompleted},
		{port.TripPlanned, port.TripCancelled}, {port.TripDeparted, port.TripCancelled},
		{port.TripPlanned, port.TripCompleted}, // phiếu cân đóng chuyến chưa ghi mốc (proto plant weighing)
	}
	for _, c := range ok {
		if err := CanTransition(c[0], c[1]); err != nil {
			t.Errorf("%s → %s phải được: %v", c[0], c[1], err)
		}
	}
	bad := [][2]port.TripStatus{
		{port.TripDeparted, port.TripPlanned}, {port.TripArrived, port.TripDeparted}, {port.TripArrived, port.TripCancelled},
		{port.TripPlanned, port.TripArrived}, {port.TripDeparted, port.TripDeparted},
	}
	for _, c := range bad {
		if err := CanTransition(c[0], c[1]); !errors.Is(err, ErrTransition) {
			t.Errorf("%s → %s phải bị cấm, nhận %v", c[0], c[1], err)
		}
	}
	for _, from := range []port.TripStatus{port.TripCompleted, port.TripCancelled} {
		if err := CanTransition(from, port.TripDeparted); !errors.Is(err, ErrTerminal) {
			t.Errorf("%s là trạng thái cuối, nhận %v", from, err)
		}
	}
}

// proto GH-01: số hiệu chuẩn hóa, loại, tải > 0, đơn giá > 0 khi có loại đơn giá.
func TestLogisticsL07VesselValidation(t *testing.T) {
	if got := NormalizeCode("  ct 12 345 "); got != "CT12345" {
		t.Fatalf("chuẩn hóa số hiệu: %q", got)
	}
	neg := dec("-1")
	cases := map[string]Vessel{
		"code":               {Kind: KindBoat},
		"kind":               {Code: "CT01", Kind: "tau"},
		"straw_payload_tons": {Code: "CT01", Kind: KindBoat, StrawPayloadTons: &neg},
		"rate_vnd":           {Code: "CT01", Kind: KindBoat, RateType: RatePerTon},
	}
	for field, v := range cases {
		var fe FieldError
		if err := ValidateVessel(v); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: nhận %v", field, err)
		}
	}
	r := dec("150000")
	if err := ValidateVessel(Vessel{Code: "CT01", Kind: KindBarge, RateType: RatePerTon, RateVND: &r}); err != nil {
		t.Fatal(err)
	}
}

// proto GH-02: hết hạn / sắp hết hạn (≤ 30 ngày) chỉ cảnh báo.
func TestLogisticsL08ExpiryStatus(t *testing.T) {
	today := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		exp   time.Time
		state string
	}{
		{today.AddDate(0, 0, -1), ExpiryExpired}, {today, ExpirySoon}, {today.AddDate(0, 0, 30), ExpirySoon},
		{today.AddDate(0, 0, 31), ExpiryValid},
	} {
		e := c.exp
		if s, _ := ExpiryStatus(&e, today, 30); s != c.state {
			t.Errorf("%s: %s (muốn %s)", c.exp.Format("2006-01-02"), s, c.state)
		}
	}
	if s, d := ExpiryStatus(nil, today, 30); s != ExpiryUnknown || d != nil {
		t.Fatal("không có hạn → unknown")
	}
}

// L-10 / proto FM-05: tấn ước > 1,1 × tải rơm thực chở → cảnh báo.
func TestLogisticsL10OverloadWarning(t *testing.T) {
	if Overloaded(mass.MustKg("99000"), dec("90"), dec("1.1")) {
		t.Fatal("99 t = 1,1 × 90 không vượt")
	}
	if !Overloaded(mass.MustKg("99000.001"), dec("90"), dec("1.1")) {
		t.Fatal("> 99 t phải cảnh báo")
	}
}

// proto createTrip / GH-03: cước chặng và cước thuê ghe.
func TestLogisticsL09Costs(t *testing.T) {
	if got := LegCost(mass.MustKg("96800"), dec("42.5"), dec("900")).String(); got != "3702600.00" {
		t.Fatalf("cước chặng: %s", got)
	}
	r := dec("50000")
	if a, ok := VesselCost(RatePerTon, &r, mass.MustKg("96800"), dec("42.5")); !ok || a.String() != "4840000.00" {
		t.Fatalf("per_ton: %s", a)
	}
	if a, ok := VesselCost(RatePerTrip, &r, mass.MustKg("96800"), dec("42.5")); !ok || a.String() != "50000.00" {
		t.Fatalf("per_trip: %s", a)
	}
	if _, ok := VesselCost("", nil, mass.MustKg("1"), dec("1")); ok {
		t.Fatal("không có đơn giá → không có cước thuê ghe")
	}
	if v := VariancePct(mass.MustKg("88000"), mass.MustKg("96800")); v == nil || v.String() != "10" {
		t.Fatalf("lệch: %v", v)
	}
}

// L-05: Haversine thập phân (không float) khớp khoảng cách tham chiếu.
func TestLogisticsL05HaversineDecimal(t *testing.T) {
	// Cần Thơ → Long Xuyên ~ 54,6 km vòng lớn.
	a := port.GeoPoint{Lat: dec("10.0452"), Lng: dec("105.7469")}
	b := port.GeoPoint{Lat: dec("10.3860"), Lng: dec("105.4352")}
	km := HaversineKm(a, b)
	if km.LessThan(dec("50")) || km.GreaterThan(dec("52")) {
		t.Fatalf("Haversine: %s km", km)
	}
	if !HaversineKm(a, a).IsZero() {
		t.Fatal("cùng điểm → 0")
	}
	if s := Sqrt(dec("2")).StringFixed(10); s != "1.4142135624" {
		t.Fatalf("sqrt: %s", s)
	}
}

func TestLogisticsAllocateKg(t *testing.T) {
	out := AllocateKg(mass.MustKg("100"), []mass.Kg{mass.MustKg("1"), mass.MustKg("1"), mass.MustKg("1")})
	sum := decimal.Zero
	for _, o := range out {
		sum = sum.Add(o.Decimal())
	}
	if !sum.Equal(dec("100")) || out[0].String() != "33.333" {
		t.Fatalf("chia: %v", out)
	}
}
