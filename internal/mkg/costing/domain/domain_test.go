package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

var (
	lotA = uuid.MustParse("00000000-0000-7000-8000-00000000000a")
	lotB = uuid.MustParse("00000000-0000-7000-8000-00000000000b")
	lotC = uuid.MustParse("00000000-0000-7000-8000-00000000000c")
	htx1 = uuid.MustParse("00000000-0000-7000-8000-0000000000f1")
	cust = uuid.MustParse("00000000-0000-7000-8000-0000000000c1")
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// C-06: cước chia theo khối lượng lô, Σ dòng = tổng tuyệt đối (dư làm tròn vào dòng cuối), tất định.
func TestSplitAmountByMass_C06(t *testing.T) {
	ws := []Weight{{lotC, mass.MustKg("3000")}, {lotA, mass.MustKg("1000")}, {lotB, mass.MustKg("2000")}}
	lines, used, err := SplitAmount(money.MustAmount("1000.00"), ws, AllocByMass)
	if err != nil || used != AllocByMass {
		t.Fatal(err, used)
	}
	want := []struct {
		lot        uuid.UUID
		amt, ratio string
	}{
		{lotA, "166.67", "0.166667"}, {lotB, "333.33", "0.333333"}, {lotC, "500.00", "0.500000"},
	}
	for i, w := range want {
		if lines[i].LotRef != w.lot || lines[i].Amount != w.amt || lines[i].Ratio != w.ratio {
			t.Errorf("dòng %d = %+v, muốn %+v", i, lines[i], w)
		}
	}
	// Khóa "equal" (tham số có phiên bản) chia đều; tổng khối lượng 0 → chia đều.
	eq, _, _ := SplitAmount(money.MustAmount("100.00"), ws, AllocByEqual)
	if eq[0].Amount != "33.33" || eq[2].Amount != "33.34" {
		t.Errorf("chia đều = %+v", eq)
	}
	zero, used, _ := SplitAmount(money.MustAmount("10.00"), []Weight{{lotA, mass.MustKg("0")}, {lotB, mass.MustKg("0")}}, AllocByMass)
	if used != AllocByEqual || zero[0].Amount != "5.00" {
		t.Errorf("khối lượng 0 → chia đều: %s %+v", used, zero)
	}
	if _, _, err := SplitAmount(money.MustAmount("1"), nil, AllocByMass); !errors.Is(err, ErrNoLots) {
		t.Errorf("không lô: %v", err)
	}
}

// C-07: quy kết hao hụt theo TỶ LỆ NGUỒN của lô (hub/port.LotOrigins, Σ = 1).
func TestAttributeLossBySourceProportions_C07(t *testing.T) {
	srcs := []Source{{"plot", lotB, dec("0.300000")}, {"plot", lotA, dec("0.500000")}, {"plot", lotC, dec("0.200000")}}
	lines, err := AttributeLoss(mass.MustKg("100"), mass.MustKg("30"), mass.MustKg("7"), srcs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID][3]string{lotA: {"50.000", "15.000", "3.500"}, lotB: {"30.000", "9.000", "2.100"}, lotC: {"20.000", "6.000", "1.400"}}
	for _, l := range lines {
		w := want[l.SourceRef]
		if l.AbnormalKg != w[0] || l.ExpectedKg != w[1] || l.NormKg != w[2] {
			t.Errorf("%s: %+v, muốn %v", l.SourceRef, l, w)
		}
	}
	thirds := []Source{{"plot", lotA, dec("0.333333")}, {"plot", lotB, dec("0.333333")}, {"plot", lotC, dec("0.333334")}}
	lines, _ = AttributeLoss(mass.MustKg("100"), mass.MustKg("0"), mass.MustKg("0"), thirds)
	sum := decimal.Zero
	for _, l := range lines {
		sum = sum.Add(dec(l.AbnormalKg))
	}
	if !sum.Equal(dec("100")) || lines[2].AbnormalKg != "33.334" {
		t.Errorf("Σ = %s, dòng cuối %s", sum, lines[2].AbnormalKg)
	}
	if _, err := AttributeLoss(mass.MustKg("1"), mass.MustKg("0"), mass.MustKg("0"), []Source{{"plot", lotA, dec("0.9")}}); !errors.Is(err, ErrBadRatios) {
		t.Errorf("Σ ≠ 1 phải lỗi: %v", err)
	}
}

// C-04 / C-05 (mục 13.3): yield = kg(HUB_IN) / cuộn; ẩm: không quy đổi (C-4).
func TestConvertRollsToTonnes_C04_C05(t *testing.T) {
	c, err := Convert(mass.MustKg("96800"), 4400, Default("costing.moisture.base_pct"))
	if err != nil {
		t.Fatal(err)
	}
	if c.YieldKgPerRoll.String() != "22.000" || c.ConvertedKg.String() != "96800.000" || c.MoistureRule != MoistureNone {
		t.Errorf("%+v", c)
	}
	if _, err := Convert(mass.MustKg("1"), 0, Default("costing.moisture.base_pct")); !errors.Is(err, ErrNoRolls) {
		t.Error(err)
	}
	if _, err := NormalizeParam("costing.moisture.base_pct", "12"); err == nil {
		t.Error("chưa có công thức ẩm (C-4) — chỉ nhận none")
	}
}

// proto PM-02 / PM-03: theo cuộn → ngay; theo tấn → chờ cân đủ; không hợp đồng → không có tiền.
func TestComputeSettlement_PM02_PM03(t *testing.T) {
	price, _ := money.ParsePrice("150000")
	rolls := 4400
	kg := mass.MustKg("96800")
	bale, err := ComputeSettlement(SettlementInput{PriceBasis: "per_bale", UnitPrice: &price, Rolls: &rolls, Deductions: money.MustAmount("0")})
	if err != nil {
		t.Fatal(err)
	}
	if bale.Status != port.SettlementPendingConfirmation || bale.Amount.String() != "660000000.00" {
		t.Errorf("theo cuộn: %+v", bale)
	}
	wait, err := ComputeSettlement(SettlementInput{PriceBasis: "per_tonne", UnitPrice: &price, Rolls: &rolls, WeighedKg: &kg, PendingLoads: true})
	if err != nil {
		t.Fatal(err)
	}
	if wait.Status != port.SettlementAwaitingWeighing || wait.Amount != nil {
		t.Errorf("theo tấn còn ghe chưa cân: %+v", wait)
	}
	ton, err := ComputeSettlement(SettlementInput{PriceBasis: "per_tonne", UnitPrice: &price, WeighedKg: &kg, Deductions: money.MustAmount("20000.00")})
	if err != nil {
		t.Fatal(err)
	}
	if ton.Gross.String() != "14520000.00" || ton.Amount.String() != "14500000.00" {
		t.Errorf("theo tấn: %+v", ton)
	}
	none, err := ComputeSettlement(SettlementInput{Rolls: &rolls, WeighedKg: &kg})
	if err != nil {
		t.Fatal(err)
	}
	if none.Amount != nil || none.Blocked != "no_contract" {
		t.Errorf("PM-03 không đoán giá: %+v", none)
	}
}

// Đơn giá thỏa thuận quá lớn × số cuộn vượt numeric(18,2): lỗi ErrOutOfRange (422), không panic.
func TestComputeSettlementOutOfRange(t *testing.T) {
	price, err := money.ParsePrice("99999999999999.9999") // tối đa numeric(18,4)
	if err != nil {
		t.Fatal(err)
	}
	rolls := 1000
	kg := mass.MustKg("100000000")
	for _, in := range []SettlementInput{
		{PriceBasis: "per_bale", UnitPrice: &price, Rolls: &rolls},
		{PriceBasis: "per_tonne", UnitPrice: &price, WeighedKg: &kg},
	} {
		if _, err := ComputeSettlement(in); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("%s: lỗi = %v, muốn ErrOutOfRange", in.PriceBasis, err)
		}
	}
	// Gross hợp lệ nhưng gross − khấu trừ vượt độ rộng (khấu trừ âm sát trần) cũng là ErrOutOfRange.
	small, _ := money.ParsePrice("1")
	one := 1
	if _, err := ComputeSettlement(SettlementInput{PriceBasis: "per_bale", UnitPrice: &small, Rolls: &one,
		Deductions: money.MustAmount("-9999999999999999.99")}); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("khấu trừ: lỗi = %v", err)
	}
}

func TestOutOfRangeNoPanic(t *testing.T) {
	if _, err := HubProcessingCost(mass.MustKg("100000000"), dec("1e15")); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("HubProcessingCost: %v", err)
	}
	if v, err := HubProcessingCost(mass.MustKg("1000"), dec("55000")); err != nil || v.String() != "55000.00" {
		t.Errorf("HubProcessingCost hợp lệ: %v, %v", v, err)
	}
	if _, err := CostPerTonne(money.MustAmount("9999999999999999.99"), mass.MustKg("0.001")); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("CostPerTonne: %v", err)
	}
	if _, err := ExpectedLossKg(mass.MustKg("1000"), dec("1e30")); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("ExpectedLossKg: %v", err)
	}
}

// C-14: chia carbon 45 % / 70-15-15, VVB nhận phần dư làm tròn; tổng khớp tuyệt đối.
func TestSplitCarbon_C14(t *testing.T) {
	sp, err := SplitCarbon(money.MustAmount("1000000.00"), dec("45"), dec("70"), dec("15"), dec("15"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Retained.String() != "450000.00" || sp.Farmers.String() != "385000.00" || sp.Authority.String() != "82500.00" ||
		sp.Verifier.String() != "82500.00" {
		t.Errorf("%+v", sp)
	}
	sp, _ = SplitCarbon(money.MustAmount("1000.01"), dec("45"), dec("70"), dec("15"), dec("15"))
	sum := sp.Retained.Add(sp.Farmers).Add(sp.Authority).Add(sp.Verifier)
	if sum.String() != "1000.01" || sp.Verifier.String() != "82.50" {
		t.Errorf("Σ = %s, %+v", sum, sp)
	}
	if _, err := SplitCarbon(money.MustAmount("1"), dec("45"), dec("70"), dec("20"), dec("15")); !errors.Is(err, ErrCarbonSplit) {
		t.Error(err)
	}
}

// C-08 (proto buildCostChain): đ/t = tổng / tấn giao; tấn giao 0 → không xác định.
func TestCostPerTonne_C08(t *testing.T) {
	if v, err := CostPerTonne(money.MustAmount("12000000.00"), mass.MustKg("80000")); err != nil || v == nil || v.String() != "150000.00" {
		t.Errorf("%v, %v", v, err)
	}
	if v, err := CostPerTonne(money.MustAmount("1"), mass.MustKg("0")); v != nil || err != nil {
		t.Errorf("tấn giao 0 → nil, nhận %v, %v", v, err)
	}
}

func TestPeriodCodeVietnamTime(t *testing.T) {
	if c := PeriodCode(time.Date(2026, 9, 30, 17, 30, 0, 0, time.UTC)); c != "2026-10" {
		t.Errorf("00:30 giờ VN ngày 1/10 thuộc kỳ 2026-10, nhận %s", c)
	}
	from, to, err := PeriodBounds("2026-09")
	if err != nil || from.Format(time.RFC3339) != "2026-08-31T17:00:00Z" || to.Format(time.RFC3339) != "2026-09-30T17:00:00Z" {
		t.Errorf("%v %v %v", from, to, err)
	}
	if _, _, err := PeriodBounds("2026-13"); err == nil {
		t.Error("mã kỳ sai phải lỗi")
	}
}

func TestParamsDefaultsProvisional_C03(t *testing.T) {
	want := map[string]string{
		port.ParamAllocationKey: "mass", port.ParamMoistureBasePct: "none", port.ParamLossRatioPct: "5.00",
		port.ParamHubProcessingLossPct: "2.00", port.ParamHubProcessingRate: "120000.0000", port.ParamSettlementTermDays: "15",
		port.ParamCarbonRetainedPct: "45.00", port.ParamCarbonSplitFarmers: "70.00", port.ParamCarbonSplitAuthority: "15.00",
		port.ParamCarbonSplitVerifier: "15.00",
	}
	for k, v := range want {
		d := Default(k)
		if d.Value != v || d.Seq != 0 || d.Pinned() != k+"#0" {
			t.Errorf("%s = %+v, muốn %s#0", k, d, v)
		}
	}
	if len(Params) != len(want) {
		t.Errorf("thiếu khóa: %d", len(Params))
	}
	for _, bad := range [][2]string{{port.ParamLossRatioPct, "101"}, {port.ParamHubProcessingRate, "0"}, {port.ParamSettlementTermDays, "0"},
		{port.ParamAllocationKey, "revenue"}} {
		if _, err := NormalizeParam(bad[0], bad[1]); err == nil {
			t.Errorf("%v phải bị từ chối", bad)
		}
	}
}

// goldenSnapshot là ảnh chụp cố định cho kiểm thử tất định đóng kỳ.
func goldenSnapshot() Snapshot {
	kg := func(s string) *string { return &s }
	id := func(n int) uuid.UUID {
		return uuid.MustParse("00000000-0000-7000-8000-0000000001" + []string{"01", "02", "03", "04", "05", "06", "07"}[n])
	}
	actor := uuid.MustParse("00000000-0000-7000-8000-0000000000aa")
	c := cust
	return Snapshot{
		PeriodID: uuid.MustParse("00000000-0000-7000-8000-000000000999"), PeriodCode: "2026-08",
		From: time.Date(2026, 7, 31, 17, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 31, 17, 0, 0, 0, time.UTC),
		AsOf: time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC), RuleVersion: RulePeriodClose,
		Entries: []SnapEntry{
			{ID: id(3), LotRef: lotA, Category: "transport_waterway", DimensionKind: "none", DeltaAmount: "3000000.00", DeltaMassKg: kg("60000.000"), EntryKind: "normal", ActorRef: actor, RuleVersion: RuleTripCost},
			{ID: id(0), LotRef: lotA, Category: "hub_processing", DimensionKind: "none", DeltaAmount: "11616000.00", DeltaMassKg: kg("96800.000"), EntryKind: "normal", ActorRef: actor, RuleVersion: RuleHubProcessing},
			{ID: id(1), LotRef: lotB, Category: "hub_processing", DimensionKind: "none", DeltaAmount: "6000000.00", DeltaMassKg: kg("50000.000"), EntryKind: "normal", ActorRef: actor, RuleVersion: RuleHubProcessing},
			{ID: id(2), LotRef: lotB, Category: "hub_processing", DimensionKind: "none", DeltaAmount: "-120000.00", DeltaMassKg: kg("-1000.000"), EntryKind: "adjusting", ActorRef: actor, RuleVersion: RuleAdjusting},
			{ID: id(4), LotRef: lotA, Category: "sales_revenue", DimensionKind: "customer", DimensionRef: &c, DeltaAmount: "180000000.00", DeltaMassKg: kg("90000.000"), EntryKind: "normal", ActorRef: actor, RuleVersion: RuleDelivery},
		},
		Settlements: []SnapSettlement{{ID: id(5), SettlementID: id(6), HTXRef: htx1, Kind: "original", Amount: "660000000.00", RuleVersion: RuleSettlement}},
		Carbon:      []SnapCarbon{{ID: id(6), Retained: "450000.00", Shares: "550000.00", RuleVersion: RuleCarbonSplit}},
	}
}

// C-09 golden: ảnh chụp cố định → đúng từng dòng tổng hợp (chuỗi số) và băm ổn định, không phụ
// thuộc thứ tự nạp; điều chỉnh (delta âm) cộng vào kỳ; quyết toán HTX là straw_purchase × htx một lần (D03).
func TestPeriodCloseGoldenDeterministic_C09(t *testing.T) {
	s := goldenSnapshot()
	lines := Summarize(s)
	type row struct {
		cat, kind, amount, kg string
		n                     int
	}
	want := []row{
		{"carbon_revenue", "none", "450000.00", "", 1},
		{"carbon_share", "none", "550000.00", "", 1},
		{"hub_processing", "none", "17496000.00", "145800.000", 3},
		{"sales_revenue", "customer", "180000000.00", "90000.000", 1},
		{"straw_purchase", "htx", "660000000.00", "", 1},
		{"transport_waterway", "none", "3000000.00", "60000.000", 1},
	}
	if len(lines) != len(want) {
		t.Fatalf("số dòng %d: %+v", len(lines), lines)
	}
	for i, w := range want {
		l := lines[i]
		gotKg := ""
		if l.MassKg != nil {
			gotKg = *l.MassKg
		}
		if l.Category != w.cat || l.DimensionKind != w.kind || l.Amount != w.amount || gotKg != w.kg || l.Entries != w.n {
			t.Errorf("dòng %d = %+v (kg %s), muốn %+v", i, l, gotKg, w)
		}
	}
	h1, _ := ResultSetSHA256(s.PeriodCode, lines)
	// Đảo thứ tự nạp → cùng dòng, cùng băm (tất định).
	r := goldenSnapshot()
	r.Entries[0], r.Entries[4] = r.Entries[4], r.Entries[0]
	h2, _ := ResultSetSHA256(r.PeriodCode, Summarize(r))
	sa, _ := s.SHA256()
	sb, _ := r.SHA256()
	if h1 != h2 || sa != sb {
		t.Fatalf("băm không ổn định: %s %s / %s %s", h1, h2, sa, sb)
	}
	// Băm golden: đổi quy tắc tổng hợp / dạng tất định phải đổi rule_version (costing.period_close.vN).
	const goldenResult = "9f97366863ca7d21096bc11c1bb7edd95c311b46a2ed09d62901142de5c0d904"
	const goldenInput = "5e4d3225f2240e94a6c5af8e3e2213bee3a92155ad174faee6e7d95ebbbf9944"
	if h1 != goldenResult || sa != goldenInput {
		t.Errorf("result_set_sha256 = %s, input_snapshot_sha256 = %s — khác golden", h1, sa)
	}
	if got := s.AdjustingAuthors(); len(got) != 1 {
		t.Errorf("người lập điều chỉnh: %v", got)
	}
	pl, err := ToPort(lines)
	if err != nil || pl[4].Category != port.CatStrawPurchase || *pl[4].DimensionRef != htx1 || pl[4].Amount.String() != "660000000.00" {
		t.Errorf("ToPort: %+v %v", pl, err)
	}
}
