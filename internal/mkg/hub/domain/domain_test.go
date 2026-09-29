package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func kgs(ss ...string) []mass.Kg {
	out := make([]mass.Kg, len(ss))
	for i, s := range ss {
		out[i] = mass.MustKg(s)
	}
	return out
}

func sum(rs []decimal.Decimal) decimal.Decimal {
	s := decimal.Zero
	for _, r := range rs {
		s = s.Add(r)
	}
	return s
}

// ARCH §16 mkg/hub: lô ra có N nguồn kèm tỷ lệ, tỷ lệ tồn tại kể cả N = 1, Σ = 1 CHÍNH XÁC.
func TestRatiosFromMassesSumExactlyOne(t *testing.T) {
	cases := [][]string{
		{"1"}, {"1", "1", "1"}, {"1", "2"}, {"333.333", "333.333", "333.334"}, {"0.001", "999.999"},
		{"7", "7", "7", "7", "7", "7", "7"}, {"1520.500", "980.250", "12.001", "3"},
	}
	for _, c := range cases {
		rs, err := RatiosFromMasses(kgs(c...))
		if err != nil {
			t.Fatalf("%v: %v", c, err)
		}
		if !sum(rs).Equal(one) {
			t.Fatalf("%v: Σ = %s, phải đúng 1", c, sum(rs))
		}
		for _, r := range rs {
			if r.Sign() <= 0 || !r.Equal(r.Truncate(RatioScale)) {
				t.Fatalf("%v: tỷ lệ %s phải > 0, tối đa 6 chữ số", c, r)
			}
		}
	}
	// 1:1:1 → dư lớn nhất, hòa thì nguồn đầu được trước: tất định.
	rs, _ := RatiosFromMasses(kgs("1", "1", "1"))
	if rs[0].String() != "0.333334" || rs[1].String() != "0.333333" || rs[2].String() != "0.333333" {
		t.Fatalf("1:1:1 = %v", rs)
	}
	if rs, _ := RatiosFromMasses(kgs("5")); !rs[0].Equal(one) {
		t.Fatalf("N = 1 phải có tỷ lệ 1: %v", rs)
	}
	if _, err := RatiosFromMasses(kgs("0.001", "99999999.999")); !errors.Is(err, ErrShareTooSmall) {
		t.Fatalf("phần đóng góp < 1e-6 phải bị từ chối: %v", err)
	}
	if _, err := RatiosFromMasses(kgs("1", "0")); err == nil {
		t.Fatal("khối lượng 0 phải bị từ chối")
	}
}

func TestCheckRatios(t *testing.T) {
	d := func(ss ...string) []decimal.Decimal {
		out := make([]decimal.Decimal, len(ss))
		for i, s := range ss {
			out[i] = decimal.RequireFromString(s)
		}
		return out
	}
	if err := CheckRatios(d("0.5", "0.25", "0.25")); err != nil {
		t.Fatal(err)
	}
	if err := CheckRatios(d("0.333333", "0.333333", "0.333333")); !errors.Is(err, ErrRatioSum) {
		t.Fatalf("Σ = 0.999999 phải bị từ chối: %v", err)
	}
	if err := CheckRatios(d("0.3333333", "0.6666667")); !errors.Is(err, ErrRatioPrecision) {
		t.Fatalf("7 chữ số phải bị từ chối: %v", err)
	}
	if err := CheckRatios(d("1.2", "-0.2")); !errors.Is(err, ErrRatioPrecision) {
		t.Fatalf("tỷ lệ âm phải bị từ chối: %v", err)
	}
}

func TestBuildSourcesRules(t *testing.T) {
	ref := ids.New()
	m := mass.MustKg("10")
	r := decimal.RequireFromString("1")
	if _, err := BuildSources([]SourceInput{{Kind: SourcePlot, Ref: ref, MassKg: &m}, {Kind: SourcePlot, Ref: ref, MassKg: &m}}); err == nil {
		t.Fatal("nguồn lặp phải bị từ chối")
	}
	if _, err := BuildSources([]SourceInput{{Kind: SourcePlot, Ref: ref, MassKg: &m}, {Kind: SourceCooperative, Ref: ids.New(), Ratio: &r}}); !errors.Is(err, ErrMixedSourceWeights) {
		t.Fatalf("trộn mass_kg và ratio phải bị từ chối: %v", err)
	}
	if _, err := BuildSources([]SourceInput{{Kind: "farmer_name", Ref: ref, MassKg: &m}}); err == nil {
		t.Fatal("loại nguồn lạ phải bị từ chối")
	}
	if _, err := BuildSources(nil); err == nil {
		t.Fatal("lô không nguồn phải bị từ chối")
	}
}

func TestTotalsLossNonNegative(t *testing.T) {
	a, b := ids.New(), ids.New()
	in := []BatchInput{{LotID: a, MassKg: mass.MustKg("600")}, {LotID: b, MassKg: mass.MustKg("400")}}
	_, _, loss, err := Totals(in, []BatchOutput{{Product: ProductDriedStraw, MassKg: mass.MustKg("870.5")}})
	if err != nil || loss.String() != "129.500" {
		t.Fatalf("hao hụt = %s, %v", loss, err)
	}
	if _, _, _, err := Totals(in, []BatchOutput{{Product: ProductDriedStraw, MassKg: mass.MustKg("1000.001")}}); !errors.Is(err, ErrNegativeLoss) {
		t.Fatalf("ra > vào phải bị từ chối: %v", err)
	}
	if _, _, _, err := Totals(append(in, in[0]), []BatchOutput{{Product: ProductDriedStraw, MassKg: mass.MustKg("1")}}); !errors.Is(err, ErrDuplicateLotInput) {
		t.Fatalf("lô vào lặp phải bị từ chối: %v", err)
	}
	srcs, err := OutputSources(in)
	if err != nil || srcs[0].Ratio.String() != "0.6" || srcs[1].Ratio.String() != "0.4" || srcs[0].Kind != SourceLot {
		t.Fatalf("nguồn lô ra: %+v %v", srcs, err)
	}
}

// WH FN-12/FN-19 (prototype src/erp/warehouse): xuất theo rủi ro xuống cấp, không FEFO/FIFO.
func TestRiskRankingNotFIFO(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	wet := decimal.RequireFromString("28")
	dry := decimal.RequireFromString("12")
	old := Balance{LotID: ids.New(), ReceivedAt: now.Add(-60 * 24 * time.Hour), Remaining: mass.MustKg("500"), MoisturePct: &dry}
	fresh := Balance{LotID: ids.New(), ReceivedAt: now.Add(-24 * time.Hour), Remaining: mass.MustKg("300"), MoisturePct: &wet}
	empty := Balance{LotID: ids.New(), ReceivedAt: now.Add(-100 * 24 * time.Hour), Remaining: mass.MustKg("0")}
	r := RiskV1.Rank([]Balance{old, fresh, empty}, now)
	if len(r) != 2 || r[0].LotID != fresh.LotID {
		t.Fatalf("lô ẩm (rủi ro cao) phải xuất trước lô cũ khô; lô hết bị bỏ: %+v", r)
	}
	if r[0].Risk.String() != "35.4" || r[1].Risk.String() != "26.7" {
		t.Fatalf("điểm rủi ro: %s, %s", r[0].Risk, r[1].Risk)
	}
	al, err := Allocate(r, mass.MustKg("450"))
	if err != nil || len(al) != 2 || al[0].MassKg.String() != "300.000" || al[1].MassKg.String() != "150.000" {
		t.Fatalf("phân bổ: %+v %v", al, err)
	}
	if _, err := Allocate(r, mass.MustKg("800.001")); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("không đủ tồn theo lô phải bị từ chối: %v", err)
	}
}

func TestCompensateAndApply(t *testing.T) {
	orig := Movement{Kind: MoveReceive, Qty: mass.MustKg("100")}
	q, err := Compensate(orig, "cân nhầm xe, nhập trùng phiếu")
	if err != nil || q.String() != "-100.000" {
		t.Fatalf("bù: %s %v", q, err)
	}
	if _, err := Compensate(Movement{Kind: MoveTransformOut, Qty: mass.MustKg("-5")}, "sai mẻ sấy số 3"); !errors.Is(err, ErrCorrectionKind) {
		t.Fatalf("mẻ không bù trực tiếp: %v", err)
	}
	if _, err := Compensate(orig, "ngắn"); err == nil {
		t.Fatal("lý do ngắn phải bị từ chối")
	}
	if _, err := Apply(mass.MustKg("50"), q); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("tồn âm phải bị từ chối: %v", err)
	}
}

func TestSnapshotAvailableNeverNegative(t *testing.T) {
	c := Snapshot(uuid.Nil, time.Time{}, mass.MustKg("1000"), mass.MustKg("1200.5"))
	if c.Available.String() != "0.000" || c.Used.Tonnes() != "1.200500" {
		t.Fatalf("snapshot: %+v", c)
	}
}

func TestFacilitySpecValidate(t *testing.T) {
	pt, err := ParsePoint("10.0452", "105.7469")
	if err != nil {
		t.Fatal(err)
	}
	s := FacilitySpec{Code: "HUB-CT-01", Name: "Hub Cờ Đỏ", Kind: KindHub, Location: pt, ProvinceCode: "92", Status: StatusActive,
		Capacity: mass.MustKg("500000"), EffectiveFrom: time.Now()}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := s
	bad.Code = "hub lower"
	if bad.Validate() == nil {
		t.Fatal("mã sai dạng phải bị từ chối")
	}
	if _, err := ParsePoint("91", "105"); err == nil {
		t.Fatal("vĩ độ ngoài khoảng phải bị từ chối")
	}
	if _, err := ParsePoint("10.123456789", "105"); err == nil {
		t.Fatal("quá 7 chữ số phải bị từ chối")
	}
}
