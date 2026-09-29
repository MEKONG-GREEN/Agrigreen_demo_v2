package domain

import (
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Product là dạng hàng của lô.
type Product string

// Dạng hàng.
const (
	ProductStrawBale      Product = "straw_bale"      // cuộn rơm từ đồng
	ProductStrawLoose     Product = "straw_loose"     // rơm rời
	ProductDriedStraw     Product = "dried_straw"     // rơm đã sấy
	ProductCompressedBale Product = "compressed_bale" // kiện nén
)

// ValidProduct kiểm dạng hàng.
func ValidProduct(p Product) bool {
	switch p {
	case ProductStrawBale, ProductStrawLoose, ProductDriedStraw, ProductCompressedBale:
		return true
	}
	return false
}

// SourceKind là loại nguồn của lô.
type SourceKind string

// Loại nguồn. Mọi ref là UUID thuần (R-06); dữ liệu cá nhân không ở đây (R-04).
const (
	SourcePlot        SourceKind = "plot"        // thửa (pf/geo plot_ref, qua lệnh gom của mkg/fieldops)
	SourceCooperative SourceKind = "cooperative" // HTX (htx_ref)
	SourceRollCount   SourceKind = "roll_count"  // bản ghi đếm cuộn mkg_fieldops.roll_count
	SourceSupplier    SourceKind = "supplier"    // bên cung cấp khác
	SourceLot         SourceKind = "lot"         // lô đầu vào của mẻ biến đổi (chuỗi nguồn gốc nhiều tầng)
)

func validSourceKind(k SourceKind) bool {
	switch k {
	case SourcePlot, SourceCooperative, SourceRollCount, SourceSupplier, SourceLot, SourceStocktake:
		return true
	}
	return false
}

// RatioScale là số chữ số thập phân của tỷ lệ (numeric(9,6), R-10).
const RatioScale = 6

var (
	one      = decimal.NewFromInt(1)
	ratioULP = decimal.New(1, -RatioScale)
)

// Source là một nguồn của lô: tỷ lệ LUÔN tồn tại, kể cả khi chỉ có một nguồn.
type Source struct {
	Kind   SourceKind      `json:"kind"`
	Ref    uuid.UUID       `json:"ref"`
	Ratio  decimal.Decimal `json:"ratio"`
	MassKg *mass.Kg        `json:"mass_kg,omitempty"`
}

// SourceInput là nguồn do client khai: hoặc khối lượng đóng góp, hoặc tỷ lệ.
type SourceInput struct {
	Kind   SourceKind
	Ref    uuid.UUID
	MassKg *mass.Kg
	Ratio  *decimal.Decimal
}

// RatiosFromMasses chia tỷ lệ theo khối lượng đóng góp, làm tròn xuống 6 chữ
// số rồi phân phần dư theo phương pháp dư lớn nhất: Σ tỷ lệ ĐÚNG bằng 1, mỗi tỷ
// lệ lệch tỷ lệ thật < 0.000001. Hòa → nguồn đứng trước được trước (tất định).
func RatiosFromMasses(masses []mass.Kg) ([]decimal.Decimal, error) {
	if len(masses) == 0 {
		return nil, fieldErr("sources", "cần ít nhất một nguồn")
	}
	total := decimal.Zero
	for i, m := range masses {
		if m.Decimal().Sign() <= 0 {
			return nil, FieldError{Field: "sources[" + itoa(i) + "].mass_kg", Msg: "phải > 0"}
		}
		total = total.Add(m.Decimal())
	}
	type rem struct {
		i int
		r decimal.Decimal
	}
	out := make([]decimal.Decimal, len(masses))
	rems := make([]rem, len(masses))
	sum := decimal.Zero
	for i, m := range masses {
		exact := m.Decimal().DivRound(total, 18)
		floor := exact.Truncate(RatioScale)
		out[i] = floor
		rems[i] = rem{i: i, r: exact.Sub(floor)}
		sum = sum.Add(floor)
	}
	left := one.Sub(sum).Div(ratioULP).IntPart() // số đơn vị 1e-6 còn thiếu, 0 ≤ left < n
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r.GreaterThan(rems[b].r) })
	for k := int64(0); k < left; k++ {
		idx := rems[k%int64(len(rems))].i
		out[idx] = out[idx].Add(ratioULP)
	}
	for _, r := range out {
		if r.Sign() <= 0 {
			return nil, ErrShareTooSmall
		}
	}
	return out, nil
}

// CheckRatios kiểm tỷ lệ khai trực tiếp: > 0, tối đa 6 chữ số, Σ ĐÚNG bằng 1 (so decimal).
func CheckRatios(rs []decimal.Decimal) error {
	if len(rs) == 0 {
		return fieldErr("sources", "cần ít nhất một nguồn")
	}
	sum := decimal.Zero
	for _, r := range rs {
		if r.Sign() <= 0 || !r.Equal(r.Truncate(RatioScale)) {
			return ErrRatioPrecision
		}
		sum = sum.Add(r)
	}
	if !sum.Equal(one) {
		return ErrRatioSum
	}
	return nil
}

// BuildSources dựng danh sách nguồn có tỷ lệ từ khai báo của client.
func BuildSources(in []SourceInput) ([]Source, error) {
	if len(in) == 0 {
		return nil, fieldErr("sources", "cần ít nhất một nguồn (tỷ lệ tồn tại kể cả N = 1)")
	}
	byMass := in[0].MassKg != nil
	seen := map[string]bool{}
	var masses []mass.Kg
	var ratios []decimal.Decimal
	for i, s := range in {
		f := "sources[" + itoa(i) + "]"
		if !validSourceKind(s.Kind) {
			return nil, fieldErr(f+".kind", "phải là plot, cooperative, roll_count, supplier, lot hoặc stocktake")
		}
		if s.Ref == uuid.Nil {
			return nil, fieldErr(f+".ref", "bắt buộc, UUID")
		}
		key := string(s.Kind) + ":" + s.Ref.String()
		if seen[key] {
			return nil, fieldErr(f, "nguồn bị lặp")
		}
		seen[key] = true
		if (s.MassKg != nil) == (s.Ratio != nil) || (s.MassKg != nil) != byMass {
			return nil, ErrMixedSourceWeights
		}
		if byMass {
			masses = append(masses, *s.MassKg)
		} else {
			ratios = append(ratios, *s.Ratio)
		}
	}
	if byMass {
		var err error
		if ratios, err = RatiosFromMasses(masses); err != nil {
			return nil, err
		}
	} else if err := CheckRatios(ratios); err != nil {
		return nil, err
	}
	out := make([]Source, len(in))
	for i, s := range in {
		out[i] = Source{Kind: s.Kind, Ref: s.Ref, Ratio: ratios[i], MassKg: s.MassKg}
	}
	return out, nil
}

// Lot là một lô (dòng sổ mkg_hub.lot).
type Lot struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	FacilityID  uuid.UUID
	Seq         int64
	Code        string
	Product     Product
	Origin      string // receipt | transform | stocktake | transfer
	BatchID     *uuid.UUID
	Initial     mass.Kg
	MoisturePct *decimal.Decimal
	Sources     []Source
	EventTime   time.Time
	RecordedAt  time.Time
	ActorRef    uuid.UUID
	IdemKey     string
}

// Lot origins.
const (
	OriginReceipt   = "receipt"
	OriginTransform = "transform"
)

// Balance là tồn còn lại của một lô (bảng chiếu lot_balance).
type Balance struct {
	LotID       uuid.UUID
	FacilityID  uuid.UUID
	Code        string
	Product     Product
	MoisturePct *decimal.Decimal
	ReceivedAt  time.Time
	Initial     mass.Kg
	Remaining   mass.Kg
	LastSeq     int64
}

// Depleted cho biết lô đã hết.
func (b Balance) Depleted() bool { return b.Remaining.Decimal().IsZero() }

func itoa(i int) string { return strconv.Itoa(i) }
