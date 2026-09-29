package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func dp(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

// Q-MKG-09 / proto WH FN-10: ngưỡng mặc định hub.risk.v2 = prototype (độ ẩm 18/22, nhiệt 40/55).
func TestRiskV2DefaultsFromPrototype(t *testing.T) {
	p := DefaultRiskV2
	if !p.HumidityWarnPct.Equal(decimal.NewFromInt(18)) || !p.HumidityCritPct.Equal(decimal.NewFromInt(22)) ||
		!p.TempWarnC.Equal(decimal.NewFromInt(40)) || !p.TempCritC.Equal(decimal.NewFromInt(55)) {
		t.Fatalf("ngưỡng mặc định: %+v", p)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := p
	bad.HumidityCritPct = decimal.NewFromInt(18)
	if bad.Validate() == nil {
		t.Fatal("nguy hiểm ≤ cảnh báo phải bị từ chối")
	}
	bad = p
	bad.AgeWeight = decimal.NewFromInt(50)
	if bad.Validate() == nil {
		t.Fatal("tổng trọng số ≠ 100 phải bị từ chối")
	}
	if _, err := DecodeParams(ParamSetRisk, json.RawMessage(`{"unknown":"1"}`)); err == nil {
		t.Fatal("khóa lạ phải bị từ chối")
	}
}

// Q-MKG-09: hub.risk.v2 xếp lô có cảnh báo môi trường lên trước; không có cảm biến → bằng v1.
func TestRiskV2RankingWithEnvAlerts(t *testing.T) {
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	f := uuid.New()
	old := Balance{LotID: uuid.New(), FacilityID: f, ReceivedAt: now.Add(-30 * 24 * time.Hour), Remaining: mass.MustKg("100")}
	fresh := Balance{LotID: uuid.New(), FacilityID: f, ReceivedAt: now.Add(-2 * 24 * time.Hour), Remaining: mass.MustKg("100")}
	lots := []Balance{old, fresh}
	v2 := DefaultRiskV2.RankV2("hub.risk.v2#0", lots, nil, now)
	v1 := RiskV1.Rank(lots, now)
	for i := range v1 {
		if v1[i].LotID != v2[i].LotID || !v1[i].Risk.Equal(v2[i].Risk) {
			t.Fatalf("không cảm biến: v2 phải bằng v1 (%v vs %v)", v1[i].Risk, v2[i].Risk)
		}
	}
	if v2[0].LotID != old.LotID {
		t.Fatal("không cảnh báo: lô cũ trước")
	}
	// 10 số đo nguy hiểm (ẩm 23 %) sau khi lô mới về → cả hai lô; 1 số đo nhiệt cảnh báo
	// trước khi lô mới về → chỉ lô cũ.
	var rs []EnvReading
	for i := 0; i < 10; i++ {
		rs = append(rs, EnvReading{FacilityID: f, HumidityPct: dp("23"), At: now.Add(-24 * time.Hour)})
	}
	rs = append(rs, EnvReading{FacilityID: f, TempC: dp("41"), At: now.Add(-10 * 24 * time.Hour)}) // chỉ lô cũ
	if n := DefaultRiskV2.Alerts(rs, fresh.ReceivedAt); n != 10 {
		t.Fatalf("cảnh báo lô mới = %d, muốn 10", n)
	}
	if n := DefaultRiskV2.Alerts(rs, old.ReceivedAt); n != 11 {
		t.Fatalf("cảnh báo lô cũ = %d, muốn 11", n)
	}
	// Lô mới: tuổi 2/90×40 = 0.9 + 25 → 25.9; lô cũ: 30/90×40 = 13.3 + 25 (bão hòa) = 38.3.
	r := DefaultRiskV2.RankV2("hub.risk.v2#0", lots, rs, now)
	if r[0].LotID != old.LotID || r[0].Risk.String() != "38.3" || r[1].Risk.String() != "25.9" || r[1].Alerts != 10 {
		t.Fatalf("xếp hạng v2: %+v", r)
	}
	// Nguy hiểm nhân hệ số: crit_alert_weight = 3 → lô ẩm mới vượt lô cũ không cảnh báo.
	p := DefaultRiskV2
	p.CritAlertWeight = 3
	only := []EnvReading{{FacilityID: f, HumidityPct: dp("22"), At: now.Add(-time.Hour)}}
	if n := p.Alerts(only, fresh.ReceivedAt); n != 3 {
		t.Fatalf("nguy hiểm × 3 = %d", n)
	}
	warn := []EnvReading{{FacilityID: f, HumidityPct: dp("18"), TempC: dp("40"), At: now.Add(-time.Hour)}}
	if n := p.Alerts(warn, fresh.ReceivedAt); n != 2 {
		t.Fatalf("hai chỉ số cảnh báo = %d", n)
	}
}

// Q-MKG-04: dung sai phiếu cân (tạm 3 %); lô nhập tay giữ số trong dung sai, lô theo phiếu theo đúng phiếu bù.
func TestHubInDecisionTolerance(t *testing.T) {
	p := DefaultWeighV1
	if got := p.Tolerance(mass.MustKg("1000")).String(); got != "30.000" {
		t.Fatalf("dung sai 3%% của 1000 = %s", got)
	}
	if _, adj := HubInDecision(p, BasisManual, mass.MustKg("1000"), mass.MustKg("1025")); adj {
		t.Fatal("trong dung sai: không điều chỉnh")
	}
	if d, adj := HubInDecision(p, BasisManual, mass.MustKg("1000"), mass.MustKg("1100")); !adj || d.String() != "100.000" {
		t.Fatalf("ngoài dung sai: %s %v", d, adj)
	}
	if d, adj := HubInDecision(p, BasisTicket, mass.MustKg("1100"), mass.MustKg("1090")); !adj || d.String() != "-10.000" {
		t.Fatalf("theo phiếu: %s %v", d, adj)
	}
	p.ToleranceMinKg = mass.MustKg("50")
	if _, adj := HubInDecision(p, BasisManual, mass.MustKg("1000"), mass.MustKg("1045")); adj {
		t.Fatal("sàn tuyệt đối 50 kg")
	}
}

// Q-MKG-06 / proto WH FN-21: số đếm phải phủ mọi dạng hàng có tồn; chênh = đếm − sổ.
func TestBuildStocktake(t *testing.T) {
	book := map[Product]mass.Kg{ProductStrawBale: mass.MustKg("1000"), ProductDriedStraw: mass.MustKg("200")}
	if _, _, err := BuildStocktake(book, []CountLine{{Product: ProductStrawBale, Counted: mass.MustKg("990")}}); !errors.Is(err, ErrStocktakeLines) {
		t.Fatalf("thiếu dạng hàng: %v", err)
	}
	lines, total, err := BuildStocktake(book, []CountLine{
		{Product: ProductStrawBale, Counted: mass.MustKg("990")}, {Product: ProductDriedStraw, Counted: mass.MustKg("215.5")},
	})
	if err != nil || len(lines) != 2 || total.Variance.String() != "5.500" || total.Book.String() != "1200.000" {
		t.Fatalf("%v %+v %v", lines, total, err)
	}
	if _, _, err := BuildStocktake(book, []CountLine{{Product: ProductStrawBale}, {Product: ProductStrawBale}}); !errors.Is(err, ErrStocktakeLines) {
		t.Fatal("lặp dạng hàng")
	}
}

// Q-MKG-10: khối lượng nhận chia theo khối lượng xuất, Σ đúng bằng khối lượng nhận.
func TestSplitProportionalExact(t *testing.T) {
	parts := []mass.Kg{mass.MustKg("1"), mass.MustKg("1"), mass.MustKg("1")}
	out, err := SplitProportional(mass.MustKg("100"), parts)
	if err != nil {
		t.Fatal(err)
	}
	sum := mass.Kg{}
	for _, k := range out {
		sum = sum.Add(k)
	}
	if sum.String() != "100.000" || out[0].String() != "33.334" || out[2].String() != "33.333" {
		t.Fatalf("%v", out)
	}
}
