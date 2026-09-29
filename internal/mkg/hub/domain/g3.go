package domain

import (
	"errors"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Phiên bản quy tắc G3 ghim vào sổ.
const (
	RuleWeighLink = "hub.weigh_link.v1" // Q-MKG-04: phiếu cân ↔ lô
	RuleStocktake = "hub.stocktake.v1"  // Q-MKG-06
	RuleTransfer  = "hub.transfer.v1"   // Q-MKG-10
	RuleLotLoss   = "hub.lot_loss.v1"
	RuleParam     = "hub.param.v1"
)

// Loại dịch chuyển G3.
const (
	MoveAdjustment  MovementKind = "adjustment"
	MoveTransferOut MovementKind = "transfer_out"
	MoveTransferIn  MovementKind = "transfer_in"
)

// Nguồn gốc lô G3.
const (
	OriginStocktake = "stocktake"
	OriginTransfer  = "transfer"
)

// SourceStocktake: nguồn của lô điều chỉnh khi kiểm kê thừa (ref = phiên kiểm kê).
const SourceStocktake SourceKind = "stocktake"

// Lỗi nghiệp vụ G3.
var (
	ErrStocktakeLines = errors.New("hub: dòng kiểm kê phải phủ đủ mọi dạng hàng đang có tồn, không lặp")
)

// Căn cứ khối lượng nhận của lô (Q-MKG-04).
const (
	BasisManual = "manual" // nhập tay ở hub — phiếu cân chỉ điều chỉnh khi lệch ngoài dung sai
	BasisTicket = "ticket" // theo phiếu cân — phiếu bù luôn được theo đúng
)

// HubInDecision quyết định áp một phiếu HUB_IN vào lô đã có.
//
// Lô nhập tay (basis manual) giữ số nhập tay nếu |cân − sổ| ≤ dung sai; ngoài
// dung sai → điều chỉnh (dịch chuyển bù) bằng đúng chênh và từ đó lô theo phiếu.
// Lô đã theo phiếu (basis ticket) → mọi chênh ≠ 0 (phiếu bù) đều điều chỉnh.
func HubInDecision(p WeighParams, basis string, book, measured mass.Kg) (diff mass.Kg, adjust bool) {
	diff = measured.Sub(book)
	if diff.Decimal().IsZero() {
		return diff, false
	}
	if basis == BasisTicket {
		return diff, true
	}
	return diff, !p.Within(measured, diff)
}

// CountLine là số đếm của một dạng hàng.
type CountLine struct {
	Product Product `json:"product"`
	Counted mass.Kg `json:"counted_kg"`
}

// StocktakeLine là dòng kiểm kê đã so với sổ lúc đếm.
type StocktakeLine struct {
	Product  Product `json:"product"`
	Book     mass.Kg `json:"book_kg"`
	Counted  mass.Kg `json:"counted_kg"`
	Variance mass.Kg `json:"variance_kg"`
}

// BuildStocktake so số đếm với sổ theo dạng hàng. Mọi dạng hàng đang có tồn
// phải được đếm (đếm được 0 thì khai 0) — không ngầm coi thiếu là 0.
func BuildStocktake(book map[Product]mass.Kg, counts []CountLine) (lines []StocktakeLine, total StocktakeLine, err error) {
	if len(counts) == 0 {
		return nil, total, fieldErr("lines", "cần ít nhất một dòng")
	}
	seen := map[Product]bool{}
	for i, c := range counts {
		f := "lines[" + itoa(i) + "]"
		if !ValidProduct(c.Product) {
			return nil, total, fieldErr(f+".product", "straw_bale, straw_loose, dried_straw hoặc compressed_bale")
		}
		if seen[c.Product] {
			return nil, total, ErrStocktakeLines
		}
		seen[c.Product] = true
		if c.Counted.Decimal().IsNegative() {
			return nil, total, fieldErr(f+".counted_kg", "phải ≥ 0")
		}
		b := book[c.Product]
		lines = append(lines, StocktakeLine{Product: c.Product, Book: b, Counted: c.Counted, Variance: c.Counted.Sub(b)})
	}
	for p, b := range book {
		if b.Decimal().Sign() > 0 && !seen[p] {
			return nil, total, ErrStocktakeLines
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Product < lines[j].Product })
	for _, l := range lines {
		total.Book, total.Counted, total.Variance = total.Book.Add(l.Book), total.Counted.Add(l.Counted), total.Variance.Add(l.Variance)
	}
	return lines, total, nil
}

// SplitProportional chia total theo tỷ lệ các phần (vd khối lượng nhận ở đích
// theo khối lượng xuất của từng lô nguồn): làm tròn xuống gam rồi phân dư lớn
// nhất — Σ kết quả ĐÚNG bằng total, tất định (hòa → phần đứng trước).
func SplitProportional(total mass.Kg, parts []mass.Kg) ([]mass.Kg, error) {
	sum := decimal.Zero
	for _, p := range parts {
		if p.Decimal().Sign() <= 0 {
			return nil, fieldErr("parts", "phải > 0")
		}
		sum = sum.Add(p.Decimal())
	}
	if len(parts) == 0 || total.Decimal().IsNegative() {
		return nil, fieldErr("parts", "cần ít nhất một phần")
	}
	grams := total.Decimal().Shift(mass.Scale) // số nguyên gam
	type rem struct {
		i int
		r decimal.Decimal
	}
	out := make([]decimal.Decimal, len(parts))
	rems := make([]rem, len(parts))
	acc := decimal.Zero
	for i, p := range parts {
		exact := grams.Mul(p.Decimal()).DivRound(sum, 12)
		fl := exact.Floor()
		out[i], rems[i] = fl, rem{i: i, r: exact.Sub(fl)}
		acc = acc.Add(fl)
	}
	left := grams.Sub(acc).IntPart()
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r.GreaterThan(rems[b].r) })
	for k := int64(0); k < left; k++ {
		idx := rems[k%int64(len(rems))].i
		out[idx] = out[idx].Add(decimal.NewFromInt(1))
	}
	res := make([]mass.Kg, len(parts))
	for i, g := range out {
		k, err := mass.ParseKg(g.Shift(-mass.Scale).StringFixed(mass.Scale))
		if err != nil {
			return nil, err
		}
		res[i] = k
	}
	return res, nil
}
