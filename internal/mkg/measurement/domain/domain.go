// Package domain: quy tắc thuần của mkg/measurement — kiểm phiếu cân, tham số có
// phiên bản (dung sai C-4, định mức hao hụt sấy), phép toán đối soát (RB-6, mục
// 13.1), sản lượng quy đổi (mục 13.3), vòng đời tranh chấp, bậc khối lượng van MRV.
//
// Không float (R-10, archlint agnofloat): khối lượng mass.Kg, phần trăm decimal.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Phiên bản quy tắc ghim vào sổ.
const (
	RuleTicketManual = "measurement.ticket.v1" // phiếu không từ luồng thô (tay / hệ cân khách)
	RuleDevice       = "measurement.device.v1"
	RuleSession      = "measurement.session.v1"
	RuleTerms        = "measurement.terms.v1"
	RuleParam        = "measurement.param.v1"
	RuleReconcile    = "measurement.reconcile.v1"
	RuleDispute      = "measurement.dispute.v1"
	RuleImport       = "measurement.customer_import.v1" // C-5 tạm
	RuleValve        = "measurement.valve.v1"
	RuleMRVBucket    = "mrv.bucket.v1"
	// RuleCollectionLink: liên kết lô cân ↔ lệnh gom (G3-2). Không có liên kết = quy ước đồng nhất.
	RuleCollectionLink = "measurement.collection_link.v1"
	ProvisionalLabel   = "provisional-default"
	MinReasonLen       = 10
	MaxReasonLen       = 500
	PctScale           = 2
	defaultKgPerRoll   = "20"
)

// FieldError là lỗi dữ liệu vào gắn với một trường.
type FieldError struct{ Field, Msg string }

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", port.ErrInvalidTicket, fmt.Sprintf(format, a...))
}

var hundred = decimal.NewFromInt(100)

// ParsePct đọc phần trăm 0..100, tối đa 2 chữ số thập phân.
func ParsePct(field, s string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil || !d.Equal(d.Truncate(PctScale)) || d.IsNegative() || d.GreaterThan(hundred) {
		return decimal.Zero, FieldError{field, "phần trăm 0..100 dạng chuỗi, tối đa 2 chữ số thập phân"}
	}
	return d, nil
}

// ValidReason kiểm lý do 10..500 ký tự.
func ValidReason(s string) bool {
	n := len([]rune(strings.TrimSpace(s)))
	return n >= MinReasonLen && n <= MaxReasonLen
}

// ---------------------------------------------------------------------------
// Phiếu cân
// ---------------------------------------------------------------------------

// NormalizeDraft kiểm một TicketDraft theo hợp đồng TicketWriter (không cần DB)
// và trả trạng thái ẩm đã suy ra. Mọi lỗi bọc port.ErrInvalidTicket.
func NormalizeDraft(d *port.TicketDraft) error {
	switch {
	case d.ID == uuid.Nil || d.ID.Version() != 7:
		return invalid("id phải là UUIDv7")
	case d.IdemKey == "":
		return invalid("thiếu idem_key")
	case d.LotRef == uuid.Nil:
		return invalid("thiếu lot_ref")
	case d.ActorRef == uuid.Nil:
		return invalid("thiếu actor_ref")
	case d.EventTime.IsZero():
		return invalid("thiếu event_time")
	}
	switch d.Point {
	case port.PointHubIn, port.PointHubOut, port.PointCustomerIn:
	default:
		return invalid("điểm cân %q không hợp lệ", d.Point)
	}
	switch d.Route {
	case port.RouteHub, port.RouteDirect:
	default:
		return invalid("tuyến %q không hợp lệ", d.Route)
	}
	if d.Route == port.RouteDirect && d.Point != port.PointCustomerIn {
		return invalid("lô DIRECT chỉ có CUSTOMER_IN")
	}
	if err := d.MassKg.NonNegative(); err != nil {
		return invalid("khối lượng âm")
	}
	if _, err := mass.ParseKg(d.MassKg.String()); err != nil {
		return invalid("khối lượng: %v", err)
	}
	if d.MoisturePct != "" {
		p, err := ParsePct("moisture_pct", d.MoisturePct)
		if err != nil {
			return invalid("độ ẩm 0..100, 2 chữ số")
		}
		d.MoisturePct = p.StringFixed(PctScale)
	}
	switch d.MoistureState {
	case "":
		d.MoistureState = port.MoistureNotMeasured
		if d.MoisturePct != "" {
			d.MoistureState = port.MoistureAsReceived
		}
	case port.MoistureNotMeasured:
		if d.MoisturePct != "" {
			return invalid("not_measured nhưng có độ ẩm")
		}
	case port.MoistureAsReceived, port.MoistureAfterDrying:
	default:
		return invalid("trạng thái ẩm %q không hợp lệ", d.MoistureState)
	}
	if (d.Supersedes == nil) != (d.CorrectionReason == "") {
		return invalid("phiếu bù cần cả supersedes và lý do")
	}
	if d.Supersedes != nil && !ValidReason(d.CorrectionReason) {
		return invalid("lý do bù 10..500 ký tự")
	}
	switch d.Source {
	case port.SourceEdge:
		if d.Point == port.PointCustomerIn {
			return invalid("CUSTOMER_IN không từ agent biên")
		}
		in := d.Integration
		if in == nil || in.RuleVersion == "" {
			return invalid("phiếu edge cần Integration và RuleVersion (R-70)")
		}
		if in.Window.DeviceID == "" || in.Window.Epoch < 1 || in.Window.CounterTo < in.Window.CounterFrom || in.Window.CounterFrom < 0 {
			return invalid("cửa sổ bộ đếm không hợp lệ")
		}
		if len(in.Gaps) > 0 {
			return fmt.Errorf("%w: %d bộ đếm thiếu", port.ErrWindowIncomplete, len(in.Gaps))
		}
		if in.NetKg.Cmp(d.MassKg) != 0 {
			return invalid("mass_kg phải bằng NetKg của phép tích phân")
		}
		if len(in.BatchIDs) == 0 {
			return invalid("thiếu reading_batch đã dùng (R-70)")
		}
		if in.ReadingsSHA256 != "" && !isHex64(in.ReadingsSHA256) {
			return invalid("readings_sha256 phải là 64 ký tự hex thường")
		}
		if d.EvidenceRef != nil || d.Reason != "" {
			return invalid("phiếu edge không mang bằng chứng tay")
		}
	case port.SourceManualEvidence:
		if d.EvidenceRef == nil || *d.EvidenceRef == uuid.Nil {
			return invalid("nhập tay bắt buộc bằng chứng")
		}
		if !ValidReason(d.Reason) {
			return invalid("nhập tay bắt buộc lý do 10..500 ký tự")
		}
		if d.Integration != nil {
			return invalid("phiếu tay không có Integration")
		}
	case port.SourceCustomerIntegration:
		if d.Point != port.PointCustomerIn {
			return invalid("hệ cân khách chỉ ghi CUSTOMER_IN")
		}
		if d.Integration != nil {
			return invalid("phiếu hệ cân khách không có Integration")
		}
	default:
		return invalid("nguồn %q không hợp lệ", d.Source)
	}
	if d.CustomerRef != nil && d.Point != port.PointCustomerIn {
		return invalid("customer_ref chỉ ở CUSTOMER_IN")
	}
	if d.Reason != "" && !ValidReason(d.Reason) {
		return invalid("lý do 10..500 ký tự")
	}
	return nil
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// RuleVersionOf trả rule_version ghi vào sổ phiếu.
func RuleVersionOf(d port.TicketDraft) string {
	if d.Source == port.SourceEdge && d.Integration != nil {
		return d.Integration.RuleVersion
	}
	return RuleTicketManual
}

// ---------------------------------------------------------------------------
// Tham số có phiên bản — TẠM, chờ khách
// ---------------------------------------------------------------------------

// Tolerance là bảng dung sai (C-4).
type Tolerance struct {
	VersionID      *uuid.UUID
	Label          string
	CommercialFlag decimal.Decimal
	CommercialCrit decimal.Decimal
	RollFlag       decimal.Decimal
	RollCrit       decimal.Decimal
	EffectiveFrom  time.Time
	Provisional    bool
}

// DefaultTolerance: mặc định prototype plant-weighing (cờ > 5 %, nghiêm trọng > 15 %) — TẠM, chờ khách C-4.
func DefaultTolerance() Tolerance {
	five, fifteen := decimal.NewFromInt(5), decimal.NewFromInt(15)
	return Tolerance{Label: ProvisionalLabel, CommercialFlag: five, CommercialCrit: fifteen, RollFlag: five, RollCrit: fifteen, Provisional: true}
}

// Validate kiểm ngưỡng.
func (t Tolerance) Validate() error {
	if t.CommercialCrit.LessThan(t.CommercialFlag) {
		return FieldError{"commercial_critical_pct", "phải ≥ commercial_flag_pct"}
	}
	if t.RollCrit.LessThan(t.RollFlag) {
		return FieldError{"roll_critical_pct", "phải ≥ roll_flag_pct"}
	}
	return nil
}

// Band là một khoảng độ ẩm [From, To) với tỷ lệ hao hụt sấy dự kiến.
type Band struct {
	MoistureFrom string `json:"moisture_from"`
	MoistureTo   string `json:"moisture_to"`
	LossPct      string `json:"loss_pct"`
}

// LossNorm là định mức hao hụt sấy theo độ ẩm (chờ khách).
type LossNorm struct {
	VersionID     *uuid.UUID
	Label         string
	Bands         []Band
	DefaultLoss   decimal.Decimal // khi lô không đo ẩm
	AlertPct      decimal.Decimal // hao hụt bất thường > AlertPct % của HUB_IN mới cảnh báo
	EffectiveFrom time.Time
	Provisional   bool
}

// DefaultLossNorm: định mức TẠM (không có trong prototype — chờ khách).
func DefaultLossNorm() LossNorm {
	return LossNorm{
		Label: ProvisionalLabel,
		Bands: []Band{
			{"0.00", "15.00", "1.00"}, {"15.00", "20.00", "3.00"}, {"20.00", "25.00", "6.00"}, {"25.00", "100.00", "10.00"},
		},
		DefaultLoss: decimal.NewFromInt(3), AlertPct: decimal.NewFromInt(1), Provisional: true,
	}
}

// Validate kiểm các khoảng: liền nhau, tăng dần, tỷ lệ 0..100.
func (n LossNorm) Validate() error {
	if len(n.Bands) == 0 {
		return FieldError{"bands", "cần ít nhất một khoảng"}
	}
	var prevTo *decimal.Decimal
	for i, b := range n.Bands {
		f := fmt.Sprintf("bands[%d]", i)
		from, err1 := ParsePct(f+".moisture_from", b.MoistureFrom)
		to, err2 := ParsePct(f+".moisture_to", b.MoistureTo)
		if _, err3 := ParsePct(f+".loss_pct", b.LossPct); err1 != nil || err2 != nil || err3 != nil {
			return FieldError{f, "phần trăm 0..100, tối đa 2 chữ số"}
		}
		if !to.GreaterThan(from) {
			return FieldError{f, "moisture_to phải > moisture_from"}
		}
		if prevTo != nil && !from.Equal(*prevTo) {
			return FieldError{f, "các khoảng phải liền nhau"}
		}
		t := to
		prevTo = &t
	}
	return nil
}

// LossPctFor trả tỷ lệ hao hụt dự kiến cho độ ẩm (nil = không đo → DefaultLoss).
// Độ ẩm ngoài mọi khoảng → khoảng cuối cùng chứa biên trên (100 %) hoặc DefaultLoss.
func (n LossNorm) LossPctFor(moisture *decimal.Decimal) decimal.Decimal {
	if moisture == nil {
		return n.DefaultLoss
	}
	for i, b := range n.Bands {
		from, _ := decimal.NewFromString(b.MoistureFrom)
		to, _ := decimal.NewFromString(b.MoistureTo)
		last := i == len(n.Bands)-1
		if moisture.GreaterThanOrEqual(from) && (moisture.LessThan(to) || (last && moisture.Equal(to))) {
			p, _ := decimal.NewFromString(b.LossPct)
			return p
		}
	}
	return n.DefaultLoss
}

// ---------------------------------------------------------------------------
// Đối soát
// ---------------------------------------------------------------------------

// Trạng thái thành phần của đối soát.
const (
	StatusNotApplicable = "not_applicable"
	InternalNormal      = "normal"
	InternalAbnormal    = "abnormal"
	InternalGain        = "gain" // HUB_OUT > HUB_IN: cân không nhất quán → cảnh báo
	CheckWithin         = "within"
	CheckFlag           = "flag"
	CheckCritical       = "critical"
	RollPending         = "pending_rolls"
	RollInformational   = "informational"
)

// ReconcileInput là đầu vào đối soát một lô.
type ReconcileInput struct {
	Route         port.Route
	Basis         port.PricingBasis
	HubIn         *mass.Kg
	HubOut        *mass.Kg
	CustomerIn    *mass.Kg
	HubInMoisture *decimal.Decimal
	Rolls         *port.RollTotal // nil = chưa có số cuộn
	Tolerance     Tolerance
	LossNorm      LossNorm
}

// ReconcileResult là kết quả (mọi số là decimal/mass, tất định).
type ReconcileResult struct {
	InternalLoss      *mass.Kg
	LossNormPct       *decimal.Decimal
	ExpectedLoss      *mass.Kg
	AbnormalLoss      *mass.Kg
	InternalStatus    string
	CommercialDiff    *mass.Kg
	CommercialDiffPct *decimal.Decimal
	CommercialStatus  string
	Rolls             *int64
	KgPerRollEstimate *mass.Kg
	RollEstimate      *mass.Kg
	RollVariancePct   *decimal.Decimal
	RollCheck         string
	Yield             *mass.Kg
	YieldAggregated   bool
	YieldBasis        port.WeighPoint
	YieldBasisKg      mass.Kg
	Outcome           string
}

// ErrIncomplete: lô chưa đủ phiếu để đối soát.
var ErrIncomplete = errors.New("measurement: lô chưa đủ phiếu cân để đối soát")

// pctOf trả (a − b) / b × 100, làm tròn 2 chữ số (nửa ra xa 0).
func pctOf(a, b mass.Kg) decimal.Decimal {
	return a.Decimal().Sub(b.Decimal()).Div(b.Decimal()).Mul(hundred).Round(PctScale)
}

func classify(pct, flag, crit decimal.Decimal) string {
	abs := pct.Abs()
	switch {
	case abs.GreaterThan(crit):
		return CheckCritical
	case abs.GreaterThan(flag):
		return CheckFlag
	}
	return CheckWithin
}

func kgPtr(k mass.Kg) *mass.Kg                  { return &k }
func decPtr(d decimal.Decimal) *decimal.Decimal { return &d }

// Reconcile tính đối soát (mục 13.1):
//   - HUB: HUB_IN → HUB_OUT tách hao hụt dự kiến (định mức sấy theo ẩm HUB_IN) và
//     bất thường; chỉ bất thường > AlertPct % HUB_IN (hoặc HUB_OUT > HUB_IN) cảnh báo.
//     HUB_OUT → CUSTOMER_IN so dung sai thương mại: vượt cờ → tranh chấp.
//   - DIRECT: chỉ CUSTOMER_IN; không tách hao hụt; sản lượng quy đổi đánh dấu gộp.
//   - Cuộn ↔ khối lượng (RB-6): ước lượng = cuộn × kg/cuộn; per_tonne → cờ theo
//     dung sai cuộn; per_bale → chỉ tham khảo.
func Reconcile(in ReconcileInput) (ReconcileResult, error) {
	var r ReconcileResult
	var basis mass.Kg
	switch in.Route {
	case port.RouteDirect:
		if in.CustomerIn == nil {
			return r, ErrIncomplete
		}
		r.InternalStatus, r.CommercialStatus = StatusNotApplicable, StatusNotApplicable
		r.YieldAggregated, r.YieldBasis, basis = true, port.PointCustomerIn, *in.CustomerIn
		r.Outcome = port.OutcomeDirectAggregate
	case port.RouteHub:
		if in.HubIn == nil || in.HubOut == nil || in.CustomerIn == nil {
			return r, ErrIncomplete
		}
		hin, hout, cin := *in.HubIn, *in.HubOut, *in.CustomerIn
		if hin.Decimal().IsZero() || hout.Decimal().IsZero() {
			return r, fmt.Errorf("%w: khối lượng HUB_IN/HUB_OUT bằng 0", ErrIncomplete)
		}
		// Nội bộ.
		actual := hin.Sub(hout)
		normPct := in.LossNorm.LossPctFor(in.HubInMoisture)
		expected, err := mass.ParseKg(hin.Decimal().Mul(normPct).Div(hundred).Round(mass.Scale).String())
		if err != nil {
			return r, err
		}
		abnormal := mass.MustKg("0")
		if actual.Cmp(expected) > 0 {
			abnormal = actual.Sub(expected)
		}
		r.InternalLoss, r.LossNormPct, r.ExpectedLoss, r.AbnormalLoss = kgPtr(actual), decPtr(normPct), kgPtr(expected), kgPtr(abnormal)
		abnormalPct := abnormal.Decimal().Div(hin.Decimal()).Mul(hundred).Round(PctScale)
		switch {
		case actual.Decimal().IsNegative():
			r.InternalStatus = InternalGain
		case abnormalPct.GreaterThan(in.LossNorm.AlertPct):
			r.InternalStatus = InternalAbnormal
		default:
			r.InternalStatus = InternalNormal
		}
		// Thương mại.
		diff := cin.Sub(hout)
		pct := pctOf(cin, hout)
		r.CommercialDiff, r.CommercialDiffPct = kgPtr(diff), decPtr(pct)
		r.CommercialStatus = classify(pct, in.Tolerance.CommercialFlag, in.Tolerance.CommercialCrit)
		switch {
		case r.CommercialStatus != CheckWithin:
			r.Outcome = port.OutcomeDisputeOpened
		case r.InternalStatus != InternalNormal:
			r.Outcome = port.OutcomeLossAlert
		default:
			r.Outcome = port.OutcomeWithinTolerance
		}
		r.YieldBasis, basis = port.PointHubIn, hin
	default:
		return r, fmt.Errorf("tuyến %q không hợp lệ", in.Route)
	}
	r.YieldBasisKg = basis

	// Cuộn ↔ khối lượng.
	r.RollCheck = RollPending
	if in.Rolls != nil && in.Rolls.Rolls > 0 {
		n := in.Rolls.Rolls
		kpr := in.Rolls.KgPerRollEstimate
		if kpr.Decimal().IsZero() {
			kpr = mass.MustKg(defaultKgPerRoll)
		}
		est, err := mass.ParseKg(kpr.Decimal().Mul(decimal.NewFromInt(n)).String())
		if err != nil {
			return r, err
		}
		y, err := basis.PerUnit(n)
		if err != nil {
			return r, err
		}
		v := pctOf(basis, est)
		r.Rolls, r.KgPerRollEstimate, r.RollEstimate, r.RollVariancePct, r.Yield = &n, kgPtr(kpr), kgPtr(est), decPtr(v), kgPtr(y)
		if in.Basis == port.PricingPerBale {
			r.RollCheck = RollInformational
		} else {
			r.RollCheck = classify(v, in.Tolerance.RollFlag, in.Tolerance.RollCrit)
		}
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// Tranh chấp
// ---------------------------------------------------------------------------

// Trạng thái và hành động tranh chấp.
const (
	DisputeOpen        = "open"
	DisputeUnderReview = "under_review"
	DisputeResolved    = "resolved"
	DisputeCancelled   = "cancelled"

	ActOpen        = "open"
	ActAddEvidence = "add_evidence"
	ActStartReview = "start_review"
	ActResolve     = "resolve"
	ActCancel      = "cancel"
)

// ErrDisputeTransition: hành động không hợp lệ ở trạng thái hiện tại.
var ErrDisputeTransition = errors.New("measurement: chuyển trạng thái tranh chấp không hợp lệ")

// NextDisputeStatus trả trạng thái sau hành động.
func NextDisputeStatus(cur, action string) (string, error) {
	active := cur == DisputeOpen || cur == DisputeUnderReview
	switch action {
	case ActAddEvidence:
		if active {
			return cur, nil
		}
	case ActStartReview:
		if cur == DisputeOpen {
			return DisputeUnderReview, nil
		}
	case ActResolve:
		if cur == DisputeUnderReview {
			return DisputeResolved, nil
		}
	case ActCancel:
		if active {
			return DisputeCancelled, nil
		}
	}
	return "", fmt.Errorf("%w: %s ở trạng thái %s", ErrDisputeTransition, action, cur)
}

// ---------------------------------------------------------------------------
// Van MRV: bậc khối lượng (mrv.bucket.v1)
// ---------------------------------------------------------------------------

// Bucket trả bậc khối lượng kg theo hợp đồng mrv_collection_evidence v1.
func Bucket(k mass.Kg) string {
	d := k.Decimal()
	switch {
	case d.LessThan(decimal.NewFromInt(500)):
		return "0-500"
	case d.LessThan(decimal.NewFromInt(1000)):
		return "500-1000"
	case d.LessThan(decimal.NewFromInt(2000)):
		return "1000-2000"
	case d.LessThan(decimal.NewFromInt(5000)):
		return "2000-5000"
	case d.LessThan(decimal.NewFromInt(10000)):
		return "5000-10000"
	}
	return "10000+"
}

// ShareOf trả k × ratio (ratio decimal chuỗi), làm tròn 3 chữ số.
func ShareOf(k mass.Kg, ratio string) (mass.Kg, error) {
	r, err := decimal.NewFromString(ratio)
	if err != nil || r.IsNegative() || r.GreaterThan(decimal.NewFromInt(1)) {
		return mass.Kg{}, fmt.Errorf("tỷ lệ nguồn %q không hợp lệ", ratio)
	}
	return mass.ParseKg(k.Decimal().Mul(r).Round(mass.Scale).String())
}
