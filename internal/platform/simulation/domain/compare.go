package domain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ResultSummary là cột tóm tắt của một kết quả đã lưu (bảng result).
type ResultSummary struct {
	SimulationID           uuid.UUID
	ScenarioID             uuid.UUID
	ScenarioCode           string
	ScenarioName           string
	Official               bool
	ParameterSetVersion    string
	LifecycleYears         int
	Discounted             bool
	HubCount               int
	DeliveredTons          decimal.Decimal
	Capex                  decimal.Decimal
	OpexYear1              decimal.Decimal
	CollectionCost         decimal.Decimal
	TransportationCost     decimal.Decimal
	WarehouseCost          decimal.Decimal
	HandlingCost           decimal.Decimal
	TotalLogisticsCost     decimal.Decimal
	CostPerTon             *decimal.Decimal
	TCO                    decimal.Decimal
	TCOPerTon              *decimal.Decimal
	LifecycleCostPerTon    *decimal.Decimal
	ROIPct                 *decimal.Decimal
	PaybackYears           *int
	PaybackStatus          string
	PlantAnnualDemandTons  decimal.Decimal
	PlantDemandCoveragePct decimal.Decimal
	ShortfallWarnings      int
	BaselineAvailable      bool
	BaselineDeliveredTons  decimal.Decimal
	BaselineCollection     decimal.Decimal
	BaselineTransportation decimal.Decimal
	BaselineTotalCost      decimal.Decimal
	BaselineCostPerTon     *decimal.Decimal
}

// CompareRow là một cột của bảng so sánh (FN-14).
type CompareRow struct {
	ResultSummary
	IsBaselineColumn bool
}

// Comparison là bảng so sánh.
type Comparison struct {
	Rows     []CompareRow
	SortedBy string
	Blocked  *string
	Warnings []string
}

// MaxReadableScenarios: FN-14 — hơn 6 kịch bản làm bảng khó đọc (chỉ cảnh báo).
const MaxReadableScenarios = 6

// Compare dựng bảng so sánh FN-14 từ các kết quả đã lưu:
//   - BR-01: kịch bản chưa có kết quả bị loại, kèm cảnh báo (missing);
//   - BR-02: khác vòng đời hoặc chế độ chiết khấu → chặn so sánh;
//   - BR-03: xếp theo TCO per Ton tăng dần; cột No-Hub Baseline ghim cuối;
//   - BR-04: cảnh báo khi khác phiên bản bộ tham số;
//   - AC-04: cột đối chứng No-Hub Baseline (CAPEX = 0), lấy theo kết quả đầu tiên.
func Compare(results []ResultSummary, missing []string, requested int, includeBaseline bool) Comparison {
	c := Comparison{SortedBy: "tco_per_ton", Rows: []CompareRow{}, Warnings: []string{}}
	if requested > MaxReadableScenarios {
		c.Warnings = append(c.Warnings, fmt.Sprintf("Chọn hơn %d kịch bản làm bảng so sánh khó đọc — gợi ý thu hẹp lựa chọn.", MaxReadableScenarios))
	}
	for _, m := range missing {
		c.Warnings = append(c.Warnings, "Mô phỏng "+m+" chưa có kết quả — đã loại khỏi bảng so sánh.")
	}
	lifecycles, discounts := map[int]bool{}, map[bool]bool{}
	for _, r := range results {
		lifecycles[r.LifecycleYears] = true
		discounts[r.Discounted] = true
	}
	if len(lifecycles) > 1 || len(discounts) > 1 {
		var why []string
		if len(lifecycles) > 1 {
			ys := make([]string, 0, len(lifecycles))
			for y := range lifecycles {
				ys = append(ys, fmt.Sprint(y))
			}
			sort.Strings(ys)
			why = append(why, "số năm vòng đời ("+strings.Join(ys, ", ")+")")
		}
		if len(discounts) > 1 {
			why = append(why, "chế độ chiết khấu (có/không)")
		}
		msg := "Không thể so sánh: các kịch bản khác nhau về " + strings.Join(why, " và ") + "."
		c.Blocked = &msg
		return c
	}
	versions := map[string]bool{}
	for _, r := range results {
		versions[r.ParameterSetVersion] = true
	}
	if len(versions) > 1 {
		c.Warnings = append(c.Warnings, fmt.Sprintf("So sánh giữa %d bộ tham số khác nhau. Chạy lại từng kịch bản theo bộ tham số hiện hành để so sánh công bằng.", len(versions)))
	}
	for _, r := range results {
		c.Rows = append(c.Rows, CompareRow{ResultSummary: r})
	}
	if includeBaseline && len(results) > 0 && results[0].BaselineAvailable {
		ref := results[0]
		zero := decimal.Zero
		coverage := decimal.Zero
		if ref.PlantAnnualDemandTons.IsPositive() {
			coverage = ref.BaselineDeliveredTons.Div(ref.PlantAnnualDemandTons).Mul(decimal.NewFromInt(100)).Round(2)
		}
		c.Rows = append(c.Rows, CompareRow{IsBaselineColumn: true, ResultSummary: ResultSummary{
			ScenarioCode: "BASELINE", ScenarioName: "No-Hub Baseline (Ruộng → Nhà máy)", ParameterSetVersion: ref.ParameterSetVersion,
			LifecycleYears: ref.LifecycleYears, Discounted: ref.Discounted, DeliveredTons: ref.BaselineDeliveredTons,
			CollectionCost: ref.BaselineCollection, TransportationCost: ref.BaselineTransportation, TotalLogisticsCost: ref.BaselineTotalCost,
			CostPerTon: ref.BaselineCostPerTon, TCO: zero, TCOPerTon: &zero, LifecycleCostPerTon: ref.BaselineCostPerTon,
			PaybackStatus: "thieu_baseline", PlantDemandCoveragePct: coverage,
		}})
	}
	sort.SliceStable(c.Rows, func(i, j int) bool {
		a, b := c.Rows[i], c.Rows[j]
		if a.IsBaselineColumn != b.IsBaselineColumn {
			return b.IsBaselineColumn
		}
		switch {
		case a.TCOPerTon == nil:
			return false
		case b.TCOPerTon == nil:
			return true
		}
		return a.TCOPerTon.LessThan(*b.TCOPerTon)
	})
	for _, r := range c.Rows {
		if r.IsBaselineColumn {
			c.Warnings = append(c.Warnings, "Cột No-Hub Baseline có CAPEX = 0 và OPEX Hub = 0 nên TCO per Ton theo công thức FN-12 BR-01 "+
				"bằng 0 và không so sánh được. Dùng cột \"Chi phí vòng đời đầy đủ chuỗi / tấn\" để đối chiếu "+
				"phương án Hub với phương án không đầu tư Hub.")
		}
	}
	return c
}
