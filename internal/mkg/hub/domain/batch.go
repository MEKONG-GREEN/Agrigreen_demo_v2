package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// BatchKind là loại mẻ biến đổi. Số tầng biến đổi là DỮ LIỆU (mục 16): lô ra
// của mẻ này làm lô vào của mẻ sau, nguồn gốc nối qua nguồn kind = "lot".
type BatchKind string

// Loại mẻ.
const (
	BatchDrying      BatchKind = "drying"
	BatchCompression BatchKind = "compression"
	BatchSorting     BatchKind = "sorting"
)

// ValidBatchKind kiểm loại mẻ.
func ValidBatchKind(k BatchKind) bool {
	return k == BatchDrying || k == BatchCompression || k == BatchSorting
}

// BatchInput là một lô đầu vào: lượng lấy từ lô và ảnh chụp thành phần của lô
// TẠI THỜI ĐIỂM MẺ CHẠY (mục 16) — không tra lại sau.
type BatchInput struct {
	LotID       uuid.UUID        `json:"lot_id"`
	MassKg      mass.Kg          `json:"mass_kg"`
	MoisturePct *decimal.Decimal `json:"moisture_pct,omitempty"`
	Product     Product          `json:"product"`
	Sources     []Source         `json:"sources"`
}

// BatchOutput là một lô đầu ra.
type BatchOutput struct {
	LotID       uuid.UUID        `json:"lot_id"`
	Code        string           `json:"code"`
	Product     Product          `json:"product"`
	MassKg      mass.Kg          `json:"mass_kg"`
	MoisturePct *decimal.Decimal `json:"moisture_pct,omitempty"`
}

// Batch là một mẻ biến đổi (dòng sổ mkg_hub.batch).
type Batch struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	FacilityID uuid.UUID
	Seq        int64
	Kind       BatchKind
	Inputs     []BatchInput
	Outputs    []BatchOutput
	Input      mass.Kg
	Output     mass.Kg
	Loss       mass.Kg
	EventTime  time.Time
	RecordedAt time.Time
	ActorRef   uuid.UUID
	IdemKey    string
}

// Totals tính tổng vào, tổng ra, hao hụt; hao hụt âm (ra > vào) bị từ chối.
func Totals(inputs []BatchInput, outputs []BatchOutput) (in, out, loss mass.Kg, err error) {
	if len(inputs) == 0 {
		return in, out, loss, fieldErr("inputs", "cần ít nhất một lô đầu vào")
	}
	if len(outputs) == 0 {
		return in, out, loss, fieldErr("outputs", "cần ít nhất một lô đầu ra")
	}
	seen := map[uuid.UUID]bool{}
	for i, x := range inputs {
		if x.MassKg.Decimal().Sign() <= 0 {
			return in, out, loss, fieldErr("inputs["+itoa(i)+"].mass_kg", "phải > 0")
		}
		if seen[x.LotID] {
			return in, out, loss, ErrDuplicateLotInput
		}
		seen[x.LotID] = true
		in = in.Add(x.MassKg)
	}
	for i, y := range outputs {
		if y.MassKg.Decimal().Sign() <= 0 {
			return in, out, loss, fieldErr("outputs["+itoa(i)+"].mass_kg", "phải > 0")
		}
		if !ValidProduct(y.Product) {
			return in, out, loss, fieldErr("outputs["+itoa(i)+"].product", "dạng hàng không hợp lệ")
		}
		out = out.Add(y.MassKg)
	}
	loss = in.Sub(out)
	if loss.Decimal().IsNegative() {
		return in, out, loss, ErrNegativeLoss
	}
	return in, out, loss, nil
}

// OutputSources: mọi lô ra của một mẻ có cùng thành phần — tỷ lệ theo khối
// lượng mỗi lô vào đóng góp (Σ = 1 chính xác, RatiosFromMasses).
func OutputSources(inputs []BatchInput) ([]Source, error) {
	masses := make([]mass.Kg, len(inputs))
	for i, x := range inputs {
		masses[i] = x.MassKg
	}
	rs, err := RatiosFromMasses(masses)
	if err != nil {
		return nil, err
	}
	out := make([]Source, len(inputs))
	for i, x := range inputs {
		m := x.MassKg
		out[i] = Source{Kind: SourceLot, Ref: x.LotID, Ratio: rs[i], MassKg: &m}
	}
	return out, nil
}
