package domain

import (
	"github.com/shopspring/decimal"
)

// BalanceRuleVersion là phiên bản quy tắc cân đối (QT-01/QT-02) ghi kèm mọi kết quả.
const BalanceRuleVersion = "cgh.balance/v1"

// Coverage là mức đáp ứng (QT-02).
type Coverage string

// Các mức đáp ứng.
const (
	CoverageSufficient   Coverage = "sufficient"   // Đủ ≥ sufficient_min_pct
	CoverageAttention    Coverage = "attention"    // Cần chú ý ≥ attention_min_pct
	CoverageShortage     Coverage = "shortage"     // Thiếu < attention_min_pct
	CoverageSurplus      Coverage = "surplus"      // Thừa ≥ surplus_min_pct
	CoverageNoData       Coverage = "no_data"      // Chưa có dữ liệu diện tích — không tô màu, không cảnh báo
	CoverageNoNorm       Coverage = "no_norm"      // Chưa cấu hình định mức cho chủng loại × khâu
	CoverageUnclassified Coverage = "unclassified" // Chưa cấu hình ngưỡng — có số liệu, không phân loại
)

// CoverageLabels là nhãn tiếng Việt.
var CoverageLabels = map[Coverage]string{
	CoverageSufficient: "Đủ", CoverageAttention: "Cần chú ý", CoverageShortage: "Thiếu", CoverageSurplus: "Thừa",
	CoverageNoData: "Chưa có dữ liệu", CoverageNoNorm: "Chưa cấu hình định mức", CoverageUnclassified: "Chưa cấu hình ngưỡng cảnh báo",
}

// CoverageColors là màu tô bản đồ (FN-08 BR-01).
var CoverageColors = map[Coverage]string{
	CoverageSufficient: "#1F7A38", CoverageAttention: "#9C6414", CoverageShortage: "#A3372A", CoverageSurplus: "#2A78D6",
	CoverageNoData: "#9AA5A0", CoverageNoNorm: "#9AA5A0", CoverageUnclassified: "#9AA5A0",
}

// AllCoverages liệt kê mọi mức theo thứ tự hiển thị.
var AllCoverages = []Coverage{
	CoverageShortage, CoverageAttention, CoverageSufficient, CoverageSurplus, CoverageNoData, CoverageNoNorm, CoverageUnclassified,
}

// Classified cho biết mức là một phân loại có màu cảnh báo.
func (c Coverage) Classified() bool {
	switch c {
	case CoverageSufficient, CoverageAttention, CoverageShortage, CoverageSurplus:
		return true
	}
	return false
}

// ParamRef là một giá trị tham số đã duyệt kèm phiên bản — lưu trong kết quả
// để truy vết (BR-05 FN-09: cân đối dùng định mức/ngưỡng hiệu lực tại vụ).
type ParamRef struct {
	Key       string          `json:"key"`
	Value     decimal.Decimal `json:"value"`
	Version   int             `json:"version"`
	SourceDoc string          `json:"source_doc"`
}

// Thresholds là bộ ngưỡng QT-02; nil = chưa cấu hình.
type Thresholds struct {
	Sufficient *ParamRef `json:"sufficient"`
	Attention  *ParamRef `json:"attention"`
	Surplus    *ParamRef `json:"surplus"`
}

// Complete cho biết đủ ba mốc.
func (t Thresholds) Complete() bool {
	return t.Sufficient != nil && t.Attention != nil && t.Surplus != nil
}

// Classify phân loại mức đáp ứng pct (%) theo QT-02. Không giới hạn trên 100%:
// BRD cho phép Thừa ≥ 120%. Mốc đúng bằng ngưỡng thuộc mức cao hơn (≥).
func Classify(pct decimal.Decimal, t Thresholds) Coverage {
	if !t.Complete() {
		return CoverageUnclassified
	}
	switch {
	case pct.GreaterThanOrEqual(t.Surplus.Value):
		return CoverageSurplus
	case pct.GreaterThanOrEqual(t.Sufficient.Value):
		return CoverageSufficient
	case pct.GreaterThanOrEqual(t.Attention.Value):
		return CoverageAttention
	}
	return CoverageShortage
}

// MaxPct là giá trị lớn nhất lưu được ở cột numeric(9,4); mức đáp ứng lớn hơn
// (diện tích rất nhỏ) được lưu bão hòa — phân loại vẫn đúng là Thừa.
var MaxPct = decimal.RequireFromString("99999.9999")

var hundred = decimal.NewFromInt(100)

// BalanceInput là đầu vào tính một dòng HTX × khâu × chủng loại.
type BalanceInput struct {
	AreaHa        *decimal.Decimal // nil = chưa có diện tích vụ (cả hai nguồn)
	Norm          *ParamRef        // nil = chưa cấu hình định mức
	MachinesTotal int              // máy thuộc HTX tại T (mọi tình trạng)
	Capacity      int              // máy "Hoạt động" tại T (FN-05 BR-04)
	Thresholds    Thresholds
}

// BalanceFigures là kết quả của một dòng.
type BalanceFigures struct {
	Required      *decimal.Decimal // diện tích ÷ định mức, 4 chữ số thập phân
	RequiredWhole *int             // làm tròn lên
	CoveragePct   *decimal.Decimal // năng lực ÷ nhu cầu × 100, 4 chữ số thập phân
	Gap           *int             // năng lực − nhu cầu làm tròn lên (âm = thiếu)
	Status        Coverage
}

// ComputeBalance thực hiện QT-01 và QT-02 cho một dòng:
// số máy cần = diện tích canh tác ÷ định mức (ha/máy/vụ); mức đáp ứng =
// số máy Hoạt động ÷ số máy cần.
func ComputeBalance(in BalanceInput) BalanceFigures {
	if in.AreaHa == nil || !in.AreaHa.IsPositive() {
		return BalanceFigures{Status: CoverageNoData}
	}
	if in.Norm == nil || !in.Norm.Value.IsPositive() {
		return BalanceFigures{Status: CoverageNoNorm}
	}
	area, norm := *in.AreaHa, in.Norm.Value
	required := area.DivRound(norm, 4)
	whole := int(area.Div(norm).Ceil().IntPart())
	// pct = năng lực × định mức × 100 ÷ diện tích — tránh sai số của nhu cầu đã làm tròn.
	pct := decimal.NewFromInt(int64(in.Capacity)).Mul(norm).Mul(hundred).DivRound(area, 4)
	if pct.GreaterThan(MaxPct) {
		pct = MaxPct
	}
	gap := in.Capacity - whole
	return BalanceFigures{
		Required: &required, RequiredWhole: &whole, CoveragePct: &pct, Gap: &gap, Status: Classify(pct, in.Thresholds),
	}
}

// ResultRow là phần của một kết quả đã lưu cần cho tổng hợp.
type ResultRow struct {
	Stage         string
	MachineType   string
	Required      *decimal.Decimal
	Capacity      int
	MachinesTotal int
	Status        Coverage
}

// Group là số liệu tổng hợp của một nhóm dòng (một khâu, hoặc cả HTX/xã/tỉnh).
type Group struct {
	Required      decimal.Decimal
	RequiredWhole int
	Capacity      int
	MachinesTotal int
	CoveragePct   *decimal.Decimal
	Status        Coverage
}

func groupOf(rows []ResultRow, t Thresholds) Group {
	var g Group
	has, noNorm := false, false
	for _, r := range rows {
		g.MachinesTotal += r.MachinesTotal
		if r.Status == CoverageNoNorm {
			noNorm = true
		}
		if r.Required == nil {
			continue
		}
		has = true
		g.Required = g.Required.Add(*r.Required)
		g.Capacity += r.Capacity
	}
	switch {
	case has && g.Required.IsPositive():
		pct := decimal.NewFromInt(int64(g.Capacity)).Mul(hundred).DivRound(g.Required, 4)
		if pct.GreaterThan(MaxPct) {
			pct = MaxPct
		}
		g.CoveragePct = &pct
		g.RequiredWhole = int(g.Required.Ceil().IntPart())
		g.Status = Classify(pct, t)
	case noNorm:
		g.Status = CoverageNoNorm
	default:
		g.Status = CoverageNoData
	}
	return g
}

// Summarize tổng hợp các dòng của một đơn vị: theo khâu (tổng nhu cầu, tổng
// năng lực của các chủng loại cùng khâu) và tổng thể. Mức đáp ứng tổng thể là
// mức của khâu THẤP NHẤT (khâu "nút thắt") — một HTX thừa máy gặt nhưng thiếu
// máy làm đất vẫn là "Thiếu"; nhu cầu/năng lực tổng thể là tổng các khâu.
func Summarize(rows []ResultRow, t Thresholds) (overall Group, stages map[string]Group) {
	byStage := map[string][]ResultRow{}
	for _, r := range rows {
		byStage[r.Stage] = append(byStage[r.Stage], r)
	}
	stages = map[string]Group{}
	var worst *decimal.Decimal
	noNorm := false
	for s, rs := range byStage {
		g := groupOf(rs, t)
		stages[s] = g
		overall.Required = overall.Required.Add(g.Required)
		overall.Capacity += g.Capacity
		overall.MachinesTotal += g.MachinesTotal
		if g.Status == CoverageNoNorm {
			noNorm = true
		}
		if g.CoveragePct != nil && (worst == nil || g.CoveragePct.LessThan(*worst)) {
			p := *g.CoveragePct
			worst = &p
		}
	}
	overall.RequiredWhole = int(overall.Required.Ceil().IntPart())
	switch {
	case worst != nil:
		overall.CoveragePct = worst
		overall.Status = Classify(*worst, t)
	case noNorm:
		overall.Status = CoverageNoNorm
	default:
		overall.Status = CoverageNoData
	}
	return overall, stages
}
