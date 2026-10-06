package domain

import (
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

var (
	hundred  = decimal.NewFromInt(100)
	thousand = decimal.NewFromInt(1000)
)

// ErrOutOfRange: số tiền / khối lượng tính ra vượt độ rộng cột lưu (numeric(18,2) cho tiền, cột kg cho
// khối lượng) — vd đơn giá thỏa thuận quá lớn × số cuộn. Đầu vào từ request ánh xạ 422 (app.mapErr).
var ErrOutOfRange = errors.New("costing: số tiền / khối lượng tính ra vượt giới hạn lưu trữ")

// amountOf dựng money.Amount từ decimal đã làm tròn 2 số; vượt numeric(18,2) → ErrOutOfRange.
func amountOf(d decimal.Decimal) (money.Amount, error) {
	a, err := money.ParseAmount(d.Round(money.AmountScale).StringFixed(money.AmountScale))
	if err != nil {
		return money.Amount{}, fmt.Errorf("%w: %w", ErrOutOfRange, err)
	}
	return a, nil
}

func kgOf(d decimal.Decimal) (mass.Kg, error) {
	k, err := mass.ParseKg(d.Round(mass.Scale).StringFixed(mass.Scale))
	if err != nil {
		return mass.Kg{}, fmt.Errorf("%w: %w", ErrOutOfRange, err)
	}
	return k, nil
}

// Weight là trọng số của một lô trong phép phân bổ.
type Weight struct {
	LotRef uuid.UUID
	MassKg mass.Kg
}

// AllocLine là một dòng kết quả phân bổ về lô (lưu jsonb, số là chuỗi).
type AllocLine struct {
	LotRef uuid.UUID `json:"lot_ref"`
	MassKg string    `json:"mass_kg"`
	Ratio  string    `json:"ratio"`
	Amount string    `json:"amount"`
}

// ErrNoLots: phép phân bổ không có lô nào.
var ErrNoLots = errors.New("costing: không có lô để phân bổ")

// SplitAmount chia total về các lô theo khóa phân bổ (mass | equal). Lô trùng được gộp; dòng
// sắp theo lot_ref; mỗi dòng làm tròn nửa lên 2 số, dòng cuối nhận phần dư để Σ = total đúng
// tuyệt đối. Tổng khối lượng 0 → chia đều. Tất định: cùng đầu vào → cùng dòng.
func SplitAmount(total money.Amount, ws []Weight, basis string) ([]AllocLine, string, error) {
	merged := map[uuid.UUID]decimal.Decimal{}
	for _, w := range ws {
		merged[w.LotRef] = merged[w.LotRef].Add(w.MassKg.Decimal())
	}
	if len(merged) == 0 {
		return nil, basis, ErrNoLots
	}
	lots := make([]uuid.UUID, 0, len(merged))
	sum := decimal.Zero
	for id, kg := range merged {
		lots = append(lots, id)
		sum = sum.Add(kg)
	}
	sort.Slice(lots, func(i, j int) bool { return lots[i].String() < lots[j].String() })
	used := basis
	if basis != AllocByEqual && sum.Sign() <= 0 {
		used = AllocByEqual
	}
	n := decimal.NewFromInt(int64(len(lots)))
	out := make([]AllocLine, 0, len(lots))
	rest := total.Decimal()
	for i, id := range lots {
		kg := merged[id]
		var ratio decimal.Decimal
		if used == AllocByEqual {
			ratio = decimal.NewFromInt(1).DivRound(n, 12)
		} else {
			ratio = kg.DivRound(sum, 12)
		}
		var amt decimal.Decimal
		if i == len(lots)-1 {
			amt = rest
		} else {
			if used == AllocByEqual {
				amt = total.Decimal().DivRound(n, money.AmountScale)
			} else {
				amt = total.Decimal().Mul(kg).DivRound(sum, money.AmountScale)
			}
			rest = rest.Sub(amt)
		}
		out = append(out, AllocLine{LotRef: id, MassKg: kg.StringFixed(mass.Scale), Ratio: ratio.StringFixed(6),
			Amount: amt.StringFixed(money.AmountScale)})
	}
	return out, used, nil
}

// HubProcessingCost = kg / 1000 × đơn giá (đ/t, #55), làm tròn nửa lên 2 số. Vượt numeric(18,2) → ErrOutOfRange.
func HubProcessingCost(kg mass.Kg, ratePerTonne decimal.Decimal) (money.Amount, error) {
	return amountOf(kg.Decimal().Mul(ratePerTonne).DivRound(thousand, money.AmountScale))
}

// Source là một nguồn của lô kèm tỷ lệ (hub/port.LotSource, Σ = 1).
type Source struct {
	Kind  string
	Ref   uuid.UUID
	Ratio decimal.Decimal
}

// LossLine là phần hao hụt quy kết về một nguồn.
type LossLine struct {
	SourceKind string    `json:"source_kind"`
	SourceRef  uuid.UUID `json:"source_ref"`
	Ratio      string    `json:"ratio"`
	AbnormalKg string    `json:"abnormal_kg"`
	ExpectedKg string    `json:"expected_kg"`
	NormKg     string    `json:"norm_kg"` // hao hụt theo định mức costing #19 + #56 (chỉ báo)
}

// ErrBadRatios: tỷ lệ nguồn không cộng thành 1.
var ErrBadRatios = errors.New("costing: tỷ lệ nguồn của lô phải cộng thành 1")

// AttributeLoss quy kết hao hụt bất thường và dự kiến về nguồn theo tỷ lệ nguồn (Σ = 1, sai lệch
// làm tròn ≤ 1e-5). Dòng sắp theo (kind, ref); dòng cuối nhận phần dư để Σ đúng tuyệt đối.
func AttributeLoss(abnormal, expected, norm mass.Kg, sources []Source) ([]LossLine, error) {
	if len(sources) == 0 {
		return nil, ErrBadRatios
	}
	ss := append([]Source(nil), sources...)
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].Kind != ss[j].Kind {
			return ss[i].Kind < ss[j].Kind
		}
		return ss[i].Ref.String() < ss[j].Ref.String()
	})
	sum := decimal.Zero
	for _, s := range ss {
		if s.Ratio.IsNegative() {
			return nil, ErrBadRatios
		}
		sum = sum.Add(s.Ratio)
	}
	if sum.Sub(decimal.NewFromInt(1)).Abs().GreaterThan(decimal.New(1, -5)) {
		return nil, ErrBadRatios
	}
	restA, restE, restN := abnormal.Decimal(), expected.Decimal(), norm.Decimal()
	out := make([]LossLine, 0, len(ss))
	for i, s := range ss {
		var a, e, n decimal.Decimal
		if i == len(ss)-1 {
			a, e, n = restA, restE, restN
		} else {
			a = abnormal.Decimal().Mul(s.Ratio).Round(mass.Scale)
			e = expected.Decimal().Mul(s.Ratio).Round(mass.Scale)
			n = norm.Decimal().Mul(s.Ratio).Round(mass.Scale)
			restA, restE, restN = restA.Sub(a), restE.Sub(e), restN.Sub(n)
		}
		out = append(out, LossLine{SourceKind: s.Kind, SourceRef: s.Ref, Ratio: s.Ratio.StringFixed(6),
			AbnormalKg: a.StringFixed(mass.Scale), ExpectedKg: e.StringFixed(mass.Scale), NormKg: n.StringFixed(mass.Scale)})
	}
	return out, nil
}

// ExpectedLossKg = kg × (định mức #19 + #56) / 100 — chỉ báo tham chiếu định mức của costing.
// Vượt độ rộng cột kg → ErrOutOfRange.
func ExpectedLossKg(kg mass.Kg, pcts ...decimal.Decimal) (mass.Kg, error) {
	p := decimal.Zero
	for _, x := range pcts {
		p = p.Add(x)
	}
	return kgOf(kg.Decimal().Mul(p).Div(hundred))
}

// Conversion là kết quả quy đổi cuộn → tấn của một lệnh gom (mục 13.3).
type Conversion struct {
	YieldKgPerRoll mass.Kg
	ConvertedKg    mass.Kg
	MoistureRule   string
}

// ErrNoRolls: chưa có số cuộn để quy đổi.
var ErrNoRolls = errors.New("costing: chưa có số cuộn")

// Convert tính yield_kg_per_roll = kg / cuộn (3 số, nửa lên). Ẩm: quy tắc none → không quy đổi (C-4).
func Convert(weighed mass.Kg, rolls int, moisture ParamValue) (Conversion, error) {
	if rolls <= 0 {
		return Conversion{}, ErrNoRolls
	}
	y, err := weighed.PerUnit(int64(rolls))
	if err != nil {
		return Conversion{}, err
	}
	return Conversion{YieldKgPerRoll: y, ConvertedKg: weighed, MoistureRule: moisture.Value}, nil
}

// SettlementInput là số liệu để tính quyết toán HTX.
type SettlementInput struct {
	PriceBasis   string       // per_tonne | per_bale | "" (chưa có giá)
	UnitPrice    *money.Price // VND/tấn hoặc VND/cuộn
	Rolls        *int
	WeighedKg    *mass.Kg
	PendingLoads bool // còn chuyến/ghe của lệnh gom chưa cân (proto PM-02)
	Deductions   money.Amount
}

// SettlementFigures là trạng thái + số tiền tính được.
type SettlementFigures struct {
	Status  port.SettlementStatus
	Blocked string // no_contract | awaiting_weighing | no_rolls | ""
	Gross   *money.Amount
	Amount  *money.Amount
}

// ComputeSettlement tính quyết toán (proto PM-02/03): theo cuộn → số cuộn × giá ngay; theo tấn →
// chờ cân đủ mọi ghe; không có giá → không có số tiền (không đoán giá). Số tiền vượt numeric(18,2)
// (đơn giá thỏa thuận quá lớn) → ErrOutOfRange.
func ComputeSettlement(in SettlementInput) (SettlementFigures, error) {
	out := SettlementFigures{Status: port.SettlementPendingConfirmation}
	if in.PriceBasis == "" || in.UnitPrice == nil {
		// PM-03: không có hợp đồng → không có số tiền; người lập nhập giá thỏa thuận, không đoán.
		out.Blocked = "no_contract"
		return out, nil
	}
	var gross decimal.Decimal
	switch in.PriceBasis {
	case "per_bale":
		if in.Rolls == nil || *in.Rolls <= 0 {
			out.Blocked = "no_rolls"
			return out, nil
		}
		gross = decimal.NewFromInt(int64(*in.Rolls)).Mul(in.UnitPrice.Decimal())
	default: // per_tonne
		if in.WeighedKg == nil || in.PendingLoads {
			out.Status, out.Blocked = port.SettlementAwaitingWeighing, "awaiting_weighing"
			return out, nil
		}
		gross = in.WeighedKg.Decimal().Mul(in.UnitPrice.Decimal()).DivRound(thousand, money.AmountScale)
	}
	g, err := amountOf(gross)
	if err != nil {
		return SettlementFigures{}, err
	}
	a, err := amountOf(g.Decimal().Sub(in.Deductions.Decimal()))
	if err != nil {
		return SettlementFigures{}, err
	}
	out.Gross, out.Amount = &g, &a
	return out, nil
}

// CarbonSplit là phần chia doanh thu carbon.
type CarbonSplit struct {
	Retained, Farmers, Authority, Verifier money.Amount
	RetainedPct, FarmersPct, AuthorityPct  decimal.Decimal
	VerifierPct                            decimal.Decimal
}

// ErrCarbonSplit: tỷ lệ chia 55 % không cộng thành 100.
var ErrCarbonSplit = errors.New("costing: tỷ lệ chia carbon (nông hộ + cơ quan + VVB) phải bằng 100")

// SplitCarbon chia doanh thu carbon (proto carbonRevenueShare): MKG giữ retained %; phần còn lại
// chia farmers / authority / verifier %, VVB nhận phần dư làm tròn. Chỉ gọi khi có doanh thu
// carbon được ghi; KHÔNG tính tCO2e (pf/mrv, chờ C-1).
func SplitCarbon(revenue money.Amount, retained, farmers, authority, verifier decimal.Decimal) (CarbonSplit, error) {
	if !farmers.Add(authority).Add(verifier).Equal(hundred) {
		return CarbonSplit{}, ErrCarbonSplit
	}
	r := revenue.Decimal().Mul(retained).DivRound(hundred, money.AmountScale)
	pool := revenue.Decimal().Sub(r)
	f := pool.Mul(farmers).DivRound(hundred, money.AmountScale)
	a := pool.Mul(authority).DivRound(hundred, money.AmountScale)
	v := pool.Sub(f).Sub(a)
	out := CarbonSplit{RetainedPct: retained, FarmersPct: farmers, AuthorityPct: authority, VerifierPct: verifier}
	for _, x := range []struct {
		dst *money.Amount
		v   decimal.Decimal
	}{{&out.Retained, r}, {&out.Farmers, f}, {&out.Authority, a}, {&out.Verifier, v}} {
		amt, err := amountOf(x.v)
		if err != nil {
			return CarbonSplit{}, err
		}
		*x.dst = amt
	}
	return out, nil
}

// CostCategories là các nhóm CHI PHÍ của giá thành lô (proto buildCostChain): không gồm doanh
// thu, carbon và giá mua rơm (tính riêng — quyết toán HTX; phải trả không cộng vào chi phí, D03).
var CostCategories = map[port.CostCategory]bool{
	port.CatCollectionLabour: true, port.CatConsumables: true, port.CatTransportRoad: true, port.CatTransportWaterway: true,
	port.CatHubProcessing: true, port.CatStorage: true, port.CatHandling: true, port.CatLoss: true,
}

// ManualCategories là nhóm được ghi tay (không gồm doanh thu, carbon, mua rơm — có luồng riêng).
var ManualCategories = CostCategories

// ValidCategory cho biết nhóm có trong port.
func ValidCategory(c port.CostCategory) bool {
	switch c {
	case port.CatStrawPurchase, port.CatSalesRevenue, port.CatCarbonRevenue, port.CatCarbonShare:
		return true
	}
	return CostCategories[c]
}

// CostPerTonne = tổng chi phí / tấn giao (proto buildCostChain). Tấn giao = 0 → nil ("không xác định").
// Tấn giao quá nhỏ so với chi phí (đ/t vượt numeric(18,2)) → ErrOutOfRange.
func CostPerTonne(total money.Amount, deliveredKg mass.Kg) (*money.Amount, error) {
	if deliveredKg.Decimal().Sign() <= 0 {
		return nil, nil
	}
	a, err := amountOf(total.Decimal().Mul(thousand).DivRound(deliveredKg.Decimal(), money.AmountScale))
	if err != nil {
		return nil, err
	}
	return &a, nil
}
