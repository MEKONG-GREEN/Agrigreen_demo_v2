package domain

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Bộ tham số có phiên bản của hub (Q-MKG-09): người đặt ≠ người duyệt (R-56).
// Chưa có phiên bản nào được duyệt → dùng mặc định TẠM (chờ khách) với số
// phiên bản 0; phiên bản đã dùng ghim vào sổ dạng "<quy tắc>#<số>".
const (
	ParamSetRisk  = "risk"
	ParamSetWeigh = "weigh"

	RuleRiskV2  = "hub.risk.v2"
	RuleWeighV1 = "hub.weigh.v1"
)

// RuleForSet trả quy tắc của bộ tham số; "" nếu bộ không tồn tại.
func RuleForSet(set string) string {
	switch set {
	case ParamSetRisk:
		return RuleRiskV2
	case ParamSetWeigh:
		return RuleWeighV1
	}
	return ""
}

// PinnedVersion là chuỗi ghim vào sổ: "hub.risk.v2#3" (0 = mặc định tạm).
func PinnedVersion(rule string, seq int64) string { return rule + "#" + strconv.FormatInt(seq, 10) }

// RiskParams là tham số của hub.risk.v2: hệ số của hub.risk.v1 (prototype
// FN-12/FN-19) + ngưỡng môi trường kho của prototype (độ ẩm 18/22 %, nhiệt độ
// 40/55 °C). Mỗi số đo cảm biến vượt ngưỡng cảnh báo tính 1 cảnh báo, vượt
// ngưỡng nguy hiểm tính CritAlertWeight cảnh báo (mặc định 1 = prototype).
type RiskParams struct {
	AgeHorizonDays  decimal.Decimal `json:"age_horizon_days"`
	AgeWeight       decimal.Decimal `json:"age_weight"`
	MoistureWarnPct decimal.Decimal `json:"moisture_warn_pct"`
	MoistureSpanPct decimal.Decimal `json:"moisture_span_pct"`
	MoistureWeight  decimal.Decimal `json:"moisture_weight"`
	AlertSaturation decimal.Decimal `json:"alert_saturation"`
	AlertWeight     decimal.Decimal `json:"alert_weight"`
	HumidityWarnPct decimal.Decimal `json:"humidity_warn_pct"`
	HumidityCritPct decimal.Decimal `json:"humidity_crit_pct"`
	TempWarnC       decimal.Decimal `json:"temp_warn_c"`
	TempCritC       decimal.Decimal `json:"temp_crit_c"`
	CritAlertWeight int             `json:"crit_alert_weight"`
}

// DefaultRiskV2 là mặc định TẠM (chờ khách): số của prototype mekong-green.
var DefaultRiskV2 = RiskParams{
	AgeHorizonDays: decimal.NewFromInt(90), AgeWeight: decimal.NewFromInt(40),
	MoistureWarnPct: decimal.NewFromInt(18), MoistureSpanPct: decimal.NewFromInt(10), MoistureWeight: decimal.NewFromInt(35),
	AlertSaturation: decimal.NewFromInt(10), AlertWeight: decimal.NewFromInt(25),
	HumidityWarnPct: decimal.NewFromInt(18), HumidityCritPct: decimal.NewFromInt(22),
	TempWarnC: decimal.NewFromInt(40), TempCritC: decimal.NewFromInt(55), CritAlertWeight: 1,
}

// Validate kiểm bộ tham số rủi ro.
func (p RiskParams) Validate() error {
	pos := map[string]decimal.Decimal{
		"params.age_horizon_days": p.AgeHorizonDays, "params.moisture_span_pct": p.MoistureSpanPct,
		"params.alert_saturation": p.AlertSaturation,
	}
	for f, d := range pos {
		if d.Sign() <= 0 {
			return fieldErr(f, "phải > 0")
		}
	}
	nonNeg := map[string]decimal.Decimal{
		"params.age_weight": p.AgeWeight, "params.moisture_weight": p.MoistureWeight, "params.alert_weight": p.AlertWeight,
		"params.moisture_warn_pct": p.MoistureWarnPct,
	}
	for f, d := range nonNeg {
		if d.IsNegative() {
			return fieldErr(f, "phải ≥ 0")
		}
	}
	if !p.AgeWeight.Add(p.MoistureWeight).Add(p.AlertWeight).Equal(decimal.NewFromInt(100)) {
		return fieldErr("params", "tổng age_weight + moisture_weight + alert_weight phải bằng 100 (điểm 0..100)")
	}
	if p.HumidityWarnPct.IsNegative() || p.HumidityCritPct.LessThanOrEqual(p.HumidityWarnPct) || p.HumidityCritPct.GreaterThan(decimal.NewFromInt(100)) {
		return fieldErr("params.humidity_crit_pct", "0 ≤ ngưỡng cảnh báo < ngưỡng nguy hiểm ≤ 100")
	}
	if p.TempCritC.LessThanOrEqual(p.TempWarnC) {
		return fieldErr("params.temp_crit_c", "ngưỡng nguy hiểm phải lớn hơn ngưỡng cảnh báo")
	}
	if p.CritAlertWeight < 1 || p.CritAlertWeight > 10 {
		return fieldErr("params.crit_alert_weight", "1..10")
	}
	return nil
}

// Rule dựng quy tắc chấm điểm từ bộ tham số; version là chuỗi ghim.
func (p RiskParams) Rule(version string) RiskRule {
	return RiskRule{
		Version: version, AgeHorizonDays: p.AgeHorizonDays, AgeWeight: p.AgeWeight, MoistureWarnPct: p.MoistureWarnPct,
		MoistureSpanPct: p.MoistureSpanPct, MoistureWeight: p.MoistureWeight, AlertSaturation: p.AlertSaturation,
		AlertWeight: p.AlertWeight,
	}
}

// EnvReading là một số đo cảm biến môi trường kho (ts_env, G3 dự phòng).
type EnvReading struct {
	FacilityID  uuid.UUID
	SensorID    string
	HumidityPct *decimal.Decimal
	TempC       *decimal.Decimal
	At          time.Time
}

// alertPoints là số cảnh báo của một số đo (mỗi chỉ số một lần, nguy hiểm nhân hệ số).
func (p RiskParams) alertPoints(r EnvReading) int {
	n := 0
	if r.HumidityPct != nil {
		switch {
		case r.HumidityPct.GreaterThanOrEqual(p.HumidityCritPct):
			n += p.CritAlertWeight
		case r.HumidityPct.GreaterThanOrEqual(p.HumidityWarnPct):
			n++
		}
	}
	if r.TempC != nil {
		switch {
		case r.TempC.GreaterThanOrEqual(p.TempCritC):
			n += p.CritAlertWeight
		case r.TempC.GreaterThanOrEqual(p.TempWarnC):
			n++
		}
	}
	return n
}

// Alerts đếm cảnh báo môi trường của một lô: số đo của cơ sở từ lúc lô nhận
// (prototype: cảnh báo của khu chứa lô kể từ received_at).
func (p RiskParams) Alerts(readings []EnvReading, since time.Time) int {
	n := 0
	for _, r := range readings {
		if !r.At.Before(since) {
			n += p.alertPoints(r)
		}
	}
	return n
}

// RankV2 sắp lô theo hub.risk.v2: như Rank nhưng có cảnh báo môi trường.
func (p RiskParams) RankV2(version string, lots []Balance, readings []EnvReading, now time.Time) []Ranked {
	rule := p.Rule(version)
	alerts := make(map[uuid.UUID]int, len(lots))
	for _, b := range lots {
		alerts[b.LotID] = p.Alerts(readings, b.ReceivedAt)
	}
	return rule.rank(lots, now, alerts)
}

// WeighParams là tham số hub.weigh.v1 (Q-MKG-04; tạm, chờ khách C-4):
//   - dung sai phiếu cân HUB_IN / HUB_OUT so với số hub đang ghi:
//     max(ToleranceMinKg, TolerancePct % × khối lượng cân);
//   - TicketRequired: lô receipt bắt buộc weigh_ticket_ref, nhập tay chỉ khi có
//     lý do (cân hỏng) — bật khi hub đã có cân;
//   - KgPerRollDefault: kg/cuộn để ước lượng hàng dự kiến về (prototype 20).
type WeighParams struct {
	TolerancePct     decimal.Decimal `json:"tolerance_pct"`
	ToleranceMinKg   mass.Kg         `json:"tolerance_min_kg"`
	TicketRequired   bool            `json:"ticket_required"`
	KgPerRollDefault mass.Kg         `json:"kg_per_roll_default"`
}

// DefaultWeighV1 là mặc định TẠM (chờ khách C-4): 3 % (ngưỡng phiếu nhận so
// với đơn của prototype), không sàn tuyệt đối, chưa bắt buộc phiếu, 20 kg/cuộn.
var DefaultWeighV1 = WeighParams{
	TolerancePct: decimal.NewFromInt(3), ToleranceMinKg: mass.MustKg("0"), TicketRequired: false,
	KgPerRollDefault: mass.MustKg("20"),
}

// Validate kiểm bộ tham số cân.
func (p WeighParams) Validate() error {
	if p.TolerancePct.IsNegative() || p.TolerancePct.GreaterThan(decimal.NewFromInt(50)) || !p.TolerancePct.Equal(p.TolerancePct.Truncate(2)) {
		return fieldErr("params.tolerance_pct", "0..50, tối đa 2 chữ số thập phân")
	}
	if p.ToleranceMinKg.Decimal().IsNegative() {
		return fieldErr("params.tolerance_min_kg", "phải ≥ 0")
	}
	if p.KgPerRollDefault.Decimal().Sign() <= 0 {
		return fieldErr("params.kg_per_roll_default", "phải > 0")
	}
	return nil
}

// Tolerance là dung sai tuyệt đối cho một khối lượng cân.
func (p WeighParams) Tolerance(measured mass.Kg) mass.Kg {
	d := measured.Decimal().Abs().Mul(p.TolerancePct).Div(decimal.NewFromInt(100)).Round(mass.Scale)
	if d.LessThan(p.ToleranceMinKg.Decimal()) {
		return p.ToleranceMinKg
	}
	return mass.MustKg(d.StringFixed(mass.Scale))
}

// Within cho biết |diff| ≤ dung sai của measured.
func (p WeighParams) Within(measured, diff mass.Kg) bool {
	return diff.Decimal().Abs().LessThanOrEqual(p.Tolerance(measured).Decimal())
}

// DecodeParams đọc jsonb params theo bộ và kiểm hợp lệ; trả giá trị đã chuẩn hóa để lưu.
func DecodeParams(set string, raw json.RawMessage) (any, error) {
	switch set {
	case ParamSetRisk:
		p := DefaultRiskV2
		if err := strictDecode(raw, &p); err != nil {
			return nil, err
		}
		return p, p.Validate()
	case ParamSetWeigh:
		p := DefaultWeighV1
		if err := strictDecode(raw, &p); err != nil {
			return nil, err
		}
		return p, p.Validate()
	}
	return nil, fieldErr("param_set", "risk hoặc weigh")
}

func strictDecode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return fieldErr("params", "bắt buộc, object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fieldErr("params", "object đúng khóa của bộ tham số; số là chuỗi thập phân: "+err.Error())
	}
	return nil
}
