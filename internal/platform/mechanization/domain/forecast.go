package domain

import (
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ForecastRuleVersion là phiên bản phương pháp dự báo nhu cầu (FN-09). Phương
// pháp TẠM: BRD không nêu — cần DCRD xác nhận phương pháp (docs/dev/cgh-forecast.md).
const ForecastRuleVersion = "cgh.forecast/v1"

// Tham số của phương pháp cgh.forecast/v1.
const (
	ForecastMaxHistory = 3 // số vụ cùng loại gần nhất dùng tính tăng trưởng
)

// ForecastGrowthCap là mức chặn tăng trưởng ±20 % (tỷ lệ, không phải %).
var ForecastGrowthCap = decimal.RequireFromString("0.2")

// Trạng thái một dòng dự báo.
const (
	ForecastOK     = "ok"
	ForecastNoData = "no_data" // chưa có diện tích cùng vụ năm trước
	ForecastNoNorm = "no_norm" // chưa cấu hình định mức tại ngày bắt đầu vụ dự báo
)

// ForecastMethod là mô tả phương pháp lưu kèm lần chạy.
type ForecastMethod struct {
	Formula    string `json:"formula"`
	MaxHistory int    `json:"max_history"`
	GrowthCap  string `json:"growth_cap"`
	Note       string `json:"note"`
}

// DefaultForecastMethod là mô tả của cgh.forecast/v1.
func DefaultForecastMethod() ForecastMethod {
	return ForecastMethod{
		Formula:    "area(S,Y) = area(S,Y-1) × (1 + clamp(mean(growth(S,Y-i), i=1..3), ±20%)); required = area ÷ norm(start of S,Y)",
		MaxHistory: ForecastMaxHistory, GrowthCap: ForecastGrowthCap.String(),
		Note: "Phương pháp tạm — BRD chưa quy định, cần DCRD xác nhận phương pháp",
	}
}

// AreaProjection là kết quả chiếu diện tích một HTX cho vụ dự báo.
type AreaProjection struct {
	Base        *decimal.Decimal           // diện tích cùng vụ năm trước
	Growth      decimal.Decimal            // tỷ lệ tăng đã áp (sau chặn)
	RawGrowth   *decimal.Decimal           // trung bình trước khi chặn; nil = không đủ dữ liệu
	Capped      bool                       // đã chặn ±20 %
	Rates       map[string]decimal.Decimal // năm → tỷ lệ tăng so với năm trước
	Forecast    *decimal.Decimal           // diện tích dự báo; nil = chưa có dữ liệu
	HistoryUsed map[string]decimal.Decimal // năm → diện tích đã dùng
}

// ProjectArea áp cgh.forecast/v1: history là diện tích cùng vụ theo năm.
// Tăng trưởng năm y = (A[y] − A[y−1]) ÷ A[y−1], y = Y−1 … Y−3, chỉ khi có đủ
// hai năm và A[y−1] > 0. Không có tỷ lệ nào → tăng trưởng 0.
func ProjectArea(history map[int]decimal.Decimal, year int) AreaProjection {
	p := AreaProjection{Rates: map[string]decimal.Decimal{}, HistoryUsed: map[string]decimal.Decimal{}}
	base, ok := history[year-1]
	if !ok {
		return p
	}
	b := base
	p.Base = &b
	p.HistoryUsed[strconv.Itoa(year-1)] = base
	var sum decimal.Decimal
	n := 0
	for i := 1; i <= ForecastMaxHistory; i++ {
		y := year - i
		cur, ok1 := history[y]
		prev, ok2 := history[y-1]
		if !ok1 || !ok2 || !prev.IsPositive() {
			continue
		}
		r := cur.Sub(prev).DivRound(prev, 6)
		p.Rates[strconv.Itoa(y)] = r
		p.HistoryUsed[strconv.Itoa(y)] = cur
		p.HistoryUsed[strconv.Itoa(y-1)] = prev
		sum = sum.Add(r)
		n++
	}
	if n > 0 {
		raw := sum.DivRound(decimal.NewFromInt(int64(n)), 6)
		p.RawGrowth = &raw
		p.Growth = raw
		switch {
		case raw.GreaterThan(ForecastGrowthCap):
			p.Growth, p.Capped = ForecastGrowthCap, true
		case raw.LessThan(ForecastGrowthCap.Neg()):
			p.Growth, p.Capped = ForecastGrowthCap.Neg(), true
		}
	}
	f := base.Mul(decimal.NewFromInt(1).Add(p.Growth)).Round(4)
	p.Forecast = &f
	return p
}

// ForecastRun là một lần chạy dự báo đã lưu (bất biến).
type ForecastRun struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Season        Season
	ReferenceDate time.Time
	PilotStages   []string
	Method        ForecastMethod
	ResultCount   int
	MissingNorms  []string
	EventTime     time.Time
	ActorRef      uuid.UUID
	IdemKey       string
	RuleVersion   string
	Seq           int64
	Recorded      time.Time
}

// ForecastInputs là đầu vào của một dòng dự báo lưu kèm kết quả.
type ForecastInputs struct {
	History   map[string]string `json:"history"`    // năm → diện tích (ha)
	Rates     map[string]string `json:"rates"`      // năm → tỷ lệ tăng so với năm trước
	RawGrowth *string           `json:"raw_growth"` // trung bình trước khi chặn
	Capped    bool              `json:"capped"`
	NormAt    string            `json:"norm_at"` // ngày tra định mức
}

// ForecastResult là một dòng dự báo HTX × chủng loại máy.
type ForecastResult struct {
	ID             uuid.UUID
	RunID          uuid.UUID
	Season         Season
	CooperativeRef uuid.UUID
	CommuneCode    string
	ProvinceCode   string
	FarmStage      string
	MachineType    string
	BaseAreaHa     *decimal.Decimal
	GrowthPct      *decimal.Decimal
	ForecastAreaHa *decimal.Decimal
	NormKey        string
	Norm           *ParamRef
	Required       *decimal.Decimal
	RequiredWhole  *int
	CapacityNow    int
	Gap            *int
	Status         string
	Inputs         ForecastInputs
	Seq            int64
}

// ForecastRequired tính số máy cần từ diện tích dự báo và định mức.
func ForecastRequired(area *decimal.Decimal, norm *ParamRef, capacity int) (required *decimal.Decimal, whole, gap *int, status string) {
	if area == nil {
		return nil, nil, nil, ForecastNoData
	}
	if norm == nil || !norm.Value.IsPositive() {
		return nil, nil, nil, ForecastNoNorm
	}
	r := area.DivRound(norm.Value, 4)
	w := int(area.Div(norm.Value).Ceil().IntPart())
	g := capacity - w
	return &r, &w, &g, ForecastOK
}

// SortedYears trả các năm của history theo thứ tự tăng (tiện kiểm thử, hiển thị).
func SortedYears(m map[int]decimal.Decimal) []int {
	out := make([]int, 0, len(m))
	for y := range m {
		out = append(out, y)
	}
	sort.Ints(out)
	return out
}

// ForecastRunStreamKey: mỗi vụ dự báo một chuỗi.
func ForecastRunStreamKey(s Season) string { return "forecast:" + s.Key() }

// ForecastResultStreamKey: mỗi lần chạy một chuỗi dòng kết quả.
func ForecastResultStreamKey(run uuid.UUID) string { return "frun:" + run.String() }
