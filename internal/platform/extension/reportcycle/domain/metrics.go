package domain

import (
	"sort"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Chỉ số dashboard Tỉnh/TW (FN-TI-02.01, FN-TW-01.01) dựng SẴN từ lần tổng hợp (bảng consolidation_metric) —
// dashboard chỉ đọc bảng này nên số luôn trùng "Báo cáo tổng hợp các xã" cùng kỳ (FN-TI-02.01 AC).
//
// Ánh xạ cột → chỉ số là bảng có phiên bản, TẠM theo 14 mẫu seed (chờ khách Q-01): tra cột theo khóa ổn định
// (FormColumn.Key) rồi theo tiền tố tiêu đề. Khi khách gửi mẫu gốc chỉ cần sửa bảng này (tăng phiên bản).

// MetricsVersion là phiên bản bảng ánh xạ chỉ số.
const MetricsVersion = "kn.dashboard.metrics/v1-tam"

// Mã chỉ số.
const (
	MetricSownPlan       = "sown_plan"        // T01 cột KH tỉnh giao (ha)
	MetricSownArea       = "sown_area"        // T01 cột Lũy kế xuống giống (ha)
	MetricSownWeek       = "sown_week"        // T01 khối cột theo kỳ: "sown_week:<khe>"
	MetricTra            = "tra"              // T03 cột Trà n: "tra:<n>"
	MetricHarvestArea    = "harvest_area"     // T07 DT thu hoạch lũy kế (ha)
	MetricHarvestOutput  = "harvest_output"   // T07 Sản lượng lũy kế (tấn)
	MetricVegSown        = "veg_sown"         // T04 DT xuống giống rau màu (ha)
	MetricVegHarvestArea = "veg_harvest_area" // T04 DT thu hoạch rau màu (ha)
	MetricVegOutput      = "veg_output"       // T04 tổng sản lượng rau màu (tấn)
	MetricPestArea       = "pest_area"        // T05 tổng DT nhiễm (ha); "pest_area:<chỉ tiêu>" theo đối tượng
	MetricPestSevere     = "pest_severe"      // T05 DT nhiễm mức nặng (ha)
	MetricFruitArea      = "fruit_area"       // M01 Σ diện tích hiện có (ha)
)

// colMatch tìm cột theo khóa hoặc tiền tố tiêu đề.
type colMatch struct {
	metric      string
	key         string
	titlePrefix string
	all         bool // cộng mọi cột khớp (M01: mọi "Diện tích hiện có")
}

// metricMap là bảng ánh xạ (mã mẫu → chỉ số).
var metricMap = map[string][]colMatch{
	"T01": {{metric: MetricSownPlan, key: "kh_tinh_giao"}, {metric: MetricSownArea, key: "luy_ke"}},
	"T04": {{metric: MetricVegSown, key: "dt_xg"}, {metric: MetricVegHarvestArea, key: "dt_th"}, {metric: MetricVegOutput, key: "san_luong"}},
	"T05": {{metric: MetricPestArea, titlePrefix: "Tổng diện tích nhiễm"}, {metric: MetricPestSevere, titlePrefix: "Mức Nặng"}},
	"T07": {{metric: MetricHarvestArea, titlePrefix: "DT thu hoạch lũy kế"}, {metric: MetricHarvestOutput, titlePrefix: "Sản lượng lũy kế"}},
	"M01": {{metric: MetricFruitArea, titlePrefix: "Diện tích hiện có", all: true}},
}

// Scope của giá trị chỉ số.
const (
	ScopeProvince = "province"
	ScopeRegion   = "region"
	ScopeCommune  = "commune"
)

// MetricValue là một giá trị chỉ số.
type MetricValue struct {
	Metric string
	Scope  string
	Unit   string // mã tỉnh / mã khu vực ('' = chưa gán) / mã xã
	Value  decimal.Decimal
}

func num(s string) (decimal.Decimal, bool) {
	if strings.TrimSpace(s) == "" {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	return d, err == nil
}

// letters trả chữ cái các cột khớp.
func (m colMatch) letters(def repport.FormDefinition) []string {
	var out []string
	for _, c := range def.Columns {
		if (m.key != "" && c.Key == m.key) || (m.titlePrefix != "" && strings.HasPrefix(c.Title, m.titlePrefix)) {
			out = append(out, c.Letter)
			if !m.all {
				break
			}
		}
	}
	return out
}

// ExtractMetrics dựng chỉ số của một lần tổng hợp: mỗi chỉ số ở 3 phạm vi — xã, khu vực (party/port.SumByRegion
// trên khu vực ghi lúc chạy) và toàn tỉnh (= Σ khu vực = Σ xã). Mẫu có dòng chỉ tiêu (T05) cộng mọi chỉ tiêu của
// xã; thêm chỉ số theo từng chỉ tiêu ở phạm vi tỉnh ("pest_area:<mã>") cho "sâu bệnh tăng mạnh nhất".
func ExtractMetrics(formCode, province string, def repport.FormDefinition, rows []Row, assign map[string]partyport.CommuneRegion) []MetricValue {
	type spec struct {
		metric  string
		letters []string
	}
	var specs []spec
	for _, m := range metricMap[formCode] {
		if ls := m.letters(def); len(ls) > 0 {
			specs = append(specs, spec{m.metric, ls})
		}
	}
	if formCode == "T01" {
		for _, c := range def.Columns {
			if c.Kind == repport.ColumnPerPeriod && c.PeriodSlot > 0 {
				specs = append(specs, spec{MetricSownWeek + ":" + strconv.Itoa(c.PeriodSlot), []string{c.Letter}})
			}
		}
	}
	if formCode == "T03" {
		n := 0
		for _, c := range def.Columns {
			if strings.HasPrefix(c.Title, "Trà ") {
				n++
				specs = append(specs, spec{MetricTra + ":" + strconv.Itoa(n), []string{c.Letter}})
			}
		}
	}
	var out []MetricValue
	for _, sp := range specs {
		values := map[string]decimal.Decimal{}
		for _, r := range rows {
			for _, l := range sp.letters {
				if v, ok := num(r.Cells[l]); ok {
					values[r.Commune] = values[r.Commune].Add(v)
				}
			}
		}
		out = append(out, scoped(sp.metric, province, values, assign)...)
	}
	// Theo từng chỉ tiêu (đối tượng sâu bệnh) ở phạm vi tỉnh.
	if formCode == "T05" {
		for _, m := range metricMap["T05"][:1] {
			ls := m.letters(def)
			if len(ls) == 0 {
				break
			}
			byInd := map[string]decimal.Decimal{}
			for _, r := range rows {
				if v, ok := num(r.Cells[ls[0]]); ok && r.Indicator != "" {
					byInd[r.Indicator] = byInd[r.Indicator].Add(v)
				}
			}
			inds := make([]string, 0, len(byInd))
			for k := range byInd {
				inds = append(inds, k)
			}
			sort.Strings(inds)
			for _, k := range inds {
				out = append(out, MetricValue{Metric: MetricPestArea + ":" + k, Scope: ScopeProvince, Unit: province, Value: byInd[k]})
			}
		}
	}
	return out
}

func scoped(metric, province string, values map[string]decimal.Decimal, assign map[string]partyport.CommuneRegion) []MetricValue {
	t := partyport.SumByRegion(values, assign)
	out := []MetricValue{{Metric: metric, Scope: ScopeProvince, Unit: province, Value: t.Province}}
	for _, r := range t.Regions() {
		out = append(out, MetricValue{Metric: metric, Scope: ScopeRegion, Unit: r, Value: t.ByRegion[r]})
	}
	communes := make([]string, 0, len(values))
	for c := range values {
		communes = append(communes, c)
	}
	sort.Strings(communes)
	for _, c := range communes {
		out = append(out, MetricValue{Metric: metric, Scope: ScopeCommune, Unit: c, Value: values[c]})
	}
	return out
}

// Ratio = a / b × 100 làm tròn 1 chữ số (0 nếu b = 0) — % kế hoạch, % thu hoạch.
func Ratio(a, b decimal.Decimal) decimal.Decimal {
	if b.IsZero() {
		return decimal.Zero
	}
	return a.Mul(decimal.NewFromInt(100)).Div(b).Round(1)
}

// Yield = sản lượng / diện tích làm tròn 2 chữ số (bình quân gia quyền, FN-TW-01.02 quy tắc).
func Yield(output, area decimal.Decimal) decimal.Decimal {
	if area.IsZero() {
		return decimal.Zero
	}
	return output.Div(area).Round(2)
}
