package domain

import (
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// RiskRule là quy tắc điểm rủi ro xuống cấp có phiên bản. Rơm không có hạn
// dùng: xuất theo RỦI RO (tuổi lưu trữ + độ ẩm + cảnh báo môi trường), KHÔNG
// theo FEFO (prototype src/erp/warehouse FN-12/FN-19).
//
//	điểm = min(1, tuổi_ngày / AgeHorizonDays) × AgeWeight
//	     + (độ_ẩm > MoistureWarnPct ? min(1, (độ_ẩm − MoistureWarnPct) / MoistureSpanPct) × MoistureWeight : 0)
//	     + min(1, số_cảnh_báo / AlertSaturation) × AlertWeight
//
// G2 chưa có cảm biến môi trường ở mkg (G3): số cảnh báo = 0.
type RiskRule struct {
	Version         string
	AgeHorizonDays  decimal.Decimal
	AgeWeight       decimal.Decimal
	MoistureWarnPct decimal.Decimal
	MoistureSpanPct decimal.Decimal
	MoistureWeight  decimal.Decimal
	AlertSaturation decimal.Decimal
	AlertWeight     decimal.Decimal
}

// RiskV1 là quy tắc mặc định, bằng hệ số của prototype.
var RiskV1 = RiskRule{
	Version:         "hub.risk.v1",
	AgeHorizonDays:  decimal.NewFromInt(90),
	AgeWeight:       decimal.NewFromInt(40),
	MoistureWarnPct: decimal.NewFromInt(18),
	MoistureSpanPct: decimal.NewFromInt(10),
	MoistureWeight:  decimal.NewFromInt(35),
	AlertSaturation: decimal.NewFromInt(10),
	AlertWeight:     decimal.NewFromInt(25),
}

func clamp01(d decimal.Decimal) decimal.Decimal {
	if d.IsNegative() {
		return decimal.Zero
	}
	if d.GreaterThan(one) {
		return one
	}
	return d
}

// Score tính điểm rủi ro 0..100 của một lô tại thời điểm now, làm tròn 1 chữ số.
func (r RiskRule) Score(b Balance, now time.Time, alerts int) decimal.Decimal {
	ageDays := decimal.NewFromInt(int64(now.Sub(b.ReceivedAt) / time.Second)).Div(decimal.NewFromInt(86400))
	score := clamp01(ageDays.Div(r.AgeHorizonDays)).Mul(r.AgeWeight)
	if b.MoisturePct != nil && b.MoisturePct.GreaterThan(r.MoistureWarnPct) {
		score = score.Add(clamp01(b.MoisturePct.Sub(r.MoistureWarnPct).Div(r.MoistureSpanPct)).Mul(r.MoistureWeight))
	}
	if alerts > 0 {
		score = score.Add(clamp01(decimal.NewFromInt(int64(alerts)).Div(r.AlertSaturation)).Mul(r.AlertWeight))
	}
	return score.Round(1)
}

// Ranked là một lô kèm điểm rủi ro.
type Ranked struct {
	Balance
	Risk    decimal.Decimal
	AgeDays int64
	Alerts  int // số cảnh báo môi trường đã tính (hub.risk.v2; v1 luôn 0)
}

// Rank sắp lô còn tồn theo rủi ro giảm dần, rồi nhận sớm trước, rồi id (tất định).
func (r RiskRule) Rank(lots []Balance, now time.Time) []Ranked { return r.rank(lots, now, nil) }

func (r RiskRule) rank(lots []Balance, now time.Time, alerts map[uuid.UUID]int) []Ranked {
	out := make([]Ranked, 0, len(lots))
	for _, b := range lots {
		if b.Remaining.Decimal().Sign() <= 0 {
			continue
		}
		n := alerts[b.LotID]
		out = append(out, Ranked{Balance: b, Risk: r.Score(b, now, n), AgeDays: int64(now.Sub(b.ReceivedAt) / (24 * time.Hour)), Alerts: n})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := out[i].Risk.Cmp(out[j].Risk); c != 0 {
			return c > 0
		}
		if !out[i].ReceivedAt.Equal(out[j].ReceivedAt) {
			return out[i].ReceivedAt.Before(out[j].ReceivedAt)
		}
		return out[i].LotID.String() < out[j].LotID.String()
	})
	return out
}

// Allocation là phần lấy từ một lô khi xuất.
type Allocation struct {
	LotID  uuid.UUID        `json:"lot_id"`
	MassKg mass.Kg          `json:"mass_kg"`
	Risk   *decimal.Decimal `json:"risk_score,omitempty"`
}

// Allocate phân bổ ĐỦ lượng xuất theo thứ tự đã xếp; không đủ tồn theo lô →
// ErrInsufficientStock ("không có lô thì không có hàng").
func Allocate(ranked []Ranked, qty mass.Kg) ([]Allocation, error) {
	if qty.Decimal().Sign() <= 0 {
		return nil, fieldErr("mass_kg", "phải > 0")
	}
	left := qty
	var out []Allocation
	for _, l := range ranked {
		if left.Decimal().Sign() <= 0 {
			break
		}
		take := l.Remaining
		if take.Cmp(left) > 0 {
			take = left
		}
		risk := l.Risk
		out = append(out, Allocation{LotID: l.LotID, MassKg: take, Risk: &risk})
		left = left.Sub(take)
	}
	if left.Decimal().Sign() > 0 {
		return nil, ErrInsufficientStock
	}
	return out, nil
}

// Capacity là ảnh chụp sức chứa của một cơ sở.
type Capacity struct {
	FacilityID uuid.UUID
	AsOf       time.Time
	Capacity   mass.Kg
	Used       mass.Kg
	Available  mass.Kg
}

// Snapshot tính sức chứa còn trống: max(0, sức chứa − tồn theo lô).
func Snapshot(facility uuid.UUID, asOf time.Time, capacity, used mass.Kg) Capacity {
	avail := capacity.Sub(used)
	if avail.Decimal().IsNegative() {
		avail = mass.Kg{}
	}
	return Capacity{FacilityID: facility, AsOf: asOf, Capacity: capacity, Used: used, Available: avail}
}
