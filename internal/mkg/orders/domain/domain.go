// Package domain: quy tắc nghiệp vụ của mkg/orders — không phụ thuộc hạ tầng, không time.Now()
// (R-12), không float (R-10: khối lượng mass.Kg, tiền money.Amount, đơn giá money.Price, phần trăm
// decimal).
//
// Quy tắc lấy từ prototype mekong-green (chép QUY TẮC, không chép mã — docs/dev/g4-conventions.md §1.1):
//   - proto CT-01: một HTX tối đa một hợp đồng mua rơm hiệu lực chồng thời gian;
//   - proto CT-02: giá > 0, tấn cam kết > 0, ngày kết thúc ≥ ngày bắt đầu;
//   - proto CT-03: cách tính per_tonne (theo tấn nhà máy cân) | per_bale (theo cuộn); trần ẩm lưu, chưa cưỡng chế (C-4);
//   - proto createPurchaseOrder: khối lượng > 0, giá > 0; draft → approved (+ rejected, cancelled);
//   - proto createGoodsReceipt: lệch đơn mua > 3 % (tham số, tạm chờ khách) → cần biên bản chênh lệch +
//     ghi chú kiểm toán, KHÔNG chặn;
//   - proto createSalesOrder / advanceSalesOrder: khối lượng > 0 (Go thêm đơn giá > 0); trạng thái
//     chỉ tiến draft → confirmed → delivering → delivered → completed.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Phiên bản quy tắc ghim vào mỗi dòng sổ (cột rule_version) và vào sự kiện.
const (
	RuleContract = "orders.contract.v1" // CT-01..03
	RulePO       = "orders.po.v1"
	RuleReceipt  = "orders.receipt.v1" // kiểm ↔ đơn mua: |nhận − đặt| / đặt × 100 > ngưỡng → biên bản (không chặn)
	RulePayable  = "orders.payable.v1" // một khoản phải trả mỗi phiếu nhận đã duyệt của đơn mua KHÁC
	RuleSales    = "orders.sales.v1"
	RuleDelivery = "orders.delivery.v1" // khối lượng = phiếu CUSTOMER_IN; tiền = kg/1000 × đơn giá, làm tròn nửa lên 2 số
	RuleTicket   = "orders.ticket.v1"
	RuleParam    = "orders.param.v1"
)

// FieldError là lỗi dữ liệu vào của một trường.
type FieldError struct {
	Field string
	Msg   string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

// Field tạo FieldError.
func Field(field, msg string) error { return FieldError{Field: field, Msg: msg} }

// Lỗi nghiệp vụ; app/ quy về problem.
var (
	ErrTransition = errors.New("orders: chuyển trạng thái không hợp lệ")
)

var (
	hundred    = decimal.NewFromInt(100)
	kgPerTonne = decimal.NewFromInt(1000)
)

// Day trả ngày nghiệp vụ (giờ Việt Nam) của t, biểu diễn 00:00 UTC.
func Day(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// ParseDate đọc ngày YYYY-MM-DD.
func ParseDate(field, s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, Field(field, "ngày YYYY-MM-DD")
	}
	return t, nil
}

// CheckReason: lý do 10..500 ký tự.
func CheckReason(field, s string, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		if required {
			return "", Field(field, "bắt buộc, 10–500 ký tự")
		}
		return "", nil
	}
	if n := len([]rune(s)); n < 10 || n > 500 {
		return "", Field(field, "10–500 ký tự")
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Hợp đồng mua rơm (CT-01..03)
// ---------------------------------------------------------------------------

// ContractSpec là nội dung một phiên bản hợp đồng.
type ContractSpec struct {
	Basis          port.PricingBasis
	UnitPrice      money.Price
	CommittedKg    mass.Kg
	MaxMoisturePct *decimal.Decimal
	ValidFrom      time.Time
	ValidTo        time.Time
	Note           string
}

// ValidateContract kiểm CT-02/CT-03.
func ValidateContract(c ContractSpec) error {
	if c.Basis != port.PricingPerTonne && c.Basis != port.PricingPerBale {
		return Field("basis", "per_tonne | per_bale (proto CT-03)")
	}
	if c.UnitPrice.Decimal().Sign() <= 0 {
		return Field("unit_price", "đơn giá phải lớn hơn 0 (proto CT-02)")
	}
	if c.CommittedKg.Decimal().Sign() <= 0 {
		return Field("committed_kg", "khối lượng cam kết phải lớn hơn 0 (proto CT-02)")
	}
	if c.ValidFrom.IsZero() || c.ValidTo.IsZero() || c.ValidTo.Before(c.ValidFrom) {
		return Field("valid_to", "ngày kết thúc không được trước ngày bắt đầu (proto CT-02)")
	}
	if m := c.MaxMoisturePct; m != nil && (m.Sign() < 0 || m.GreaterThan(hundred) || !m.Equal(m.Truncate(2))) {
		return Field("max_moisture_pct", "0..100, tối đa 2 chữ số thập phân")
	}
	if len([]rune(c.Note)) > 1000 {
		return Field("note", "tối đa 1000 ký tự")
	}
	return nil
}

// Overlaps cho biết hai khoảng ngày [a1, a2] và [b1, b2] chồng nhau (CT-01).
func Overlaps(a1, a2, b1, b2 time.Time) bool { return !a1.After(b2) && !b1.After(a2) }

// ---------------------------------------------------------------------------
// Đơn mua và phiếu nhận hàng
// ---------------------------------------------------------------------------

// Đơn vị đơn mua.
const (
	UnitKg   = "kg"   // đơn giá VND/tấn, kiểm theo khối lượng phiếu cân HUB_IN
	UnitBale = "bale" // đơn giá VND/cuộn, kiểm theo số cuộn đếm
)

// Loại đơn mua.
const (
	CategoryStraw = "straw" // rơm của HTX — phải trả CHỈ từ quyết toán costing
	CategoryOther = "other" // mua khác — phải trả từ phiếu nhận đã duyệt
)

// UnitForBasis trả đơn vị đơn mua ứng với cách tính giá của hợp đồng.
func UnitForBasis(b port.PricingBasis) string {
	if b == port.PricingPerBale {
		return UnitBale
	}
	return UnitKg
}

// CheckQty kiểm số lượng đặt/nhận theo đơn vị: > 0 (hoặc ≥ 0 khi allowZero), cuộn là số nguyên, kg ≤ 3 số lẻ.
func CheckQty(field, unit string, q decimal.Decimal, allowZero bool) error {
	if q.Sign() < 0 || (!allowZero && q.Sign() == 0) {
		return Field(field, "phải lớn hơn 0")
	}
	if unit == UnitBale && !q.Equal(q.Truncate(0)) {
		return Field(field, "số cuộn nguyên")
	}
	if !q.Equal(q.Truncate(mass.Scale)) || q.GreaterThanOrEqual(decimal.New(1, 11)) {
		return Field(field, "tối đa 11 chữ số nguyên, 3 chữ số thập phân")
	}
	return nil
}

// Variance là kết quả kiểm phiếu nhận hàng ↔ đơn mua.
type Variance struct {
	Pct          decimal.Decimal // có dấu, làm tròn nửa lên 2 số
	Threshold    decimal.Decimal
	NeedsReport  bool
	AuditNote    string
	ParamVersion string
}

// CheckReceipt so số nhận với số đặt (cùng đơn vị): |chênh %| > ngưỡng → cần biên bản chênh lệch và
// ghi chú kiểm toán. KHÔNG bao giờ chặn (proto createGoodsReceipt, g3-acceptance "ngoài G3 → G4").
func CheckReceipt(unit string, ordered, received, threshold decimal.Decimal, paramVersion string) Variance {
	pct := received.Sub(ordered).Mul(hundred).Div(ordered).Round(2)
	v := Variance{Pct: pct, Threshold: threshold, ParamVersion: paramVersion}
	if pct.Abs().GreaterThan(threshold) {
		v.NeedsReport = true
		u := "kg"
		if unit == UnitBale {
			u = "cuộn"
		}
		v.AuditNote = fmt.Sprintf("Chênh lệch %s%% so với đơn mua (nhận %s %s, đặt %s %s) — vượt ngưỡng %s%% (%s), cần lập biên bản chênh lệch.",
			pct.StringFixed(2), received.String(), u, ordered.String(), u, threshold.String(), paramVersion)
	}
	return v
}

// LineAmount là thành tiền: kg → kg/1000 × đơn giá/tấn; cuộn → số cuộn × đơn giá/cuộn. Làm tròn nửa lên 2 số.
func LineAmount(unit string, qty decimal.Decimal, price money.Price) (money.Amount, error) {
	d := qty.Mul(price.Decimal())
	if unit == UnitKg {
		d = d.Div(kgPerTonne)
	}
	return money.ParseAmount(d.Round(money.AmountScale).StringFixed(money.AmountScale))
}

// KgAmount là thành tiền của khối lượng kg theo đơn giá VND/tấn.
func KgAmount(kg mass.Kg, price money.Price) (money.Amount, error) {
	return LineAmount(UnitKg, kg.Decimal(), price)
}

// ---------------------------------------------------------------------------
// Đơn bán
// ---------------------------------------------------------------------------

// Trạng thái đơn bán — chỉ tiến (proto advanceSalesOrder); cancelled chỉ từ draft/confirmed.
const (
	SODraft      = "draft"
	SOConfirmed  = "confirmed"
	SODelivering = "delivering"
	SODelivered  = "delivered"
	SOCompleted  = "completed"
	SOCancelled  = "cancelled"
)

var soFlow = []string{SODraft, SOConfirmed, SODelivering, SODelivered, SOCompleted}

func soIndex(s string) int {
	for i, x := range soFlow {
		if x == s {
			return i
		}
	}
	return -1
}

// CanAdvance cho biết đơn bán chuyển được từ from sang to (chỉ tiến, không lùi, không nhảy về trước).
func CanAdvance(from, to string) bool {
	if to == SOCancelled {
		return from == SODraft || from == SOConfirmed
	}
	i, j := soIndex(from), soIndex(to)
	return i >= 0 && j > i
}

// Products hợp lệ của đơn bán (khớp hub).
var Products = []string{"straw_bale", "straw_loose", "dried_straw", "compressed_bale"}

// ValidProduct cho biết p hợp lệ.
func ValidProduct(p string) bool {
	for _, x := range Products {
		if x == p {
			return true
		}
	}
	return false
}

// NetPrice = đơn giá × (1 − chiết khấu/100), làm tròn nửa lên 4 số.
func NetPrice(unit money.Price, discountPct decimal.Decimal) (money.Price, error) {
	d := unit.Decimal().Mul(hundred.Sub(discountPct)).Div(hundred).Round(money.PriceScale)
	return money.ParsePrice(d.StringFixed(money.PriceScale))
}

// PriceApproval là kết quả kiểm ngưỡng giá / chiết khấu (mkg.md §5.5, tham số chờ khách).
type PriceApproval struct {
	Needed   bool
	DeltaPct *decimal.Decimal // |đơn giá − giá bảng| / giá bảng × 100 (nil khi không có giá bảng)
	Reasons  []string
}

// CheckPriceApproval: chiết khấu > ngưỡng chiết khấu, hoặc đơn giá lệch giá bảng > ngưỡng lệch giá →
// cần sales_approver (người khác người lập) xác nhận đơn.
func CheckPriceApproval(list *money.Price, unit money.Price, discountPct, deltaThreshold, discountThreshold decimal.Decimal) PriceApproval {
	var r PriceApproval
	if discountPct.GreaterThan(discountThreshold) {
		r.Needed = true
		r.Reasons = append(r.Reasons, fmt.Sprintf("chiết khấu %s%% > ngưỡng %s%%", discountPct.StringFixed(2), discountThreshold.String()))
	}
	if list != nil && list.Decimal().Sign() > 0 {
		d := unit.Decimal().Sub(list.Decimal()).Abs().Mul(hundred).Div(list.Decimal()).Round(2)
		r.DeltaPct = &d
		if d.GreaterThan(deltaThreshold) {
			r.Needed = true
			r.Reasons = append(r.Reasons, fmt.Sprintf("đơn giá lệch giá bảng %s%% > ngưỡng %s%%", d.StringFixed(2), deltaThreshold.String()))
		}
	}
	return r
}

// CheckDiscount kiểm chiết khấu 0 ≤ x < 100, tối đa 2 số lẻ.
func CheckDiscount(d decimal.Decimal) error {
	if d.Sign() < 0 || !d.LessThan(hundred) || !d.Equal(d.Truncate(2)) {
		return Field("discount_pct", "0 ≤ chiết khấu < 100, tối đa 2 chữ số thập phân")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Giao nhận
// ---------------------------------------------------------------------------

// ValidRoute cho biết tuyến hợp lệ (HUB | DIRECT).
func ValidRoute(r string) bool { return r == port.RouteHub || r == port.RouteDirect }

// ValidEvidenceKind cho biết loại chứng từ biên bản hợp lệ.
func ValidEvidenceKind(k string) bool { return k == "epod" || k == "minutes" || k == "ebill" }

// ---------------------------------------------------------------------------
// Tham số có phiên bản (g4-conventions §1.2) — mặc định TẠM, CHỜ KHÁCH
// ---------------------------------------------------------------------------

// ParamDef là định nghĩa một khóa tham số.
type ParamDef struct {
	Key     string
	Default decimal.Decimal
	Max     decimal.Decimal
	Integer bool
	Note    string
}

// Params là các khóa tham số của orders.
var Params = []ParamDef{
	{Key: port.ParamReceiptVariancePct, Default: decimal.NewFromInt(3), Max: decimal.NewFromInt(100),
		Note: "proto RECEIPT_VARIANCE_TOLERANCE_PCT — tạm, chờ khách"},
	{Key: port.ParamPriceApprovalDelta, Default: decimal.Zero, Max: decimal.NewFromInt(100),
		Note: "chờ khách (không có trong prototype) — mặc định 0: mọi lệch giá bảng cần sales_approver"},
	{Key: port.ParamDiscountApprovalPct, Default: decimal.Zero, Max: decimal.NewFromInt(100),
		Note: "chờ khách (không có trong prototype) — mặc định 0: mọi chiết khấu cần sales_approver"},
	{Key: port.ParamPaymentTermDays, Default: decimal.NewFromInt(15), Max: decimal.NewFromInt(365), Integer: true,
		Note: "proto PAYMENT_TERM_DAYS — tạm, chờ khách"},
	{Key: port.ParamStockShortageBlocking, Default: decimal.Zero, Max: decimal.NewFromInt(1), Integer: true,
		Note: "0 = thiếu tồn chỉ cảnh báo (proto stockWarning)"},
}

// ParamByKey trả định nghĩa của khóa.
func ParamByKey(k string) (ParamDef, bool) {
	for _, p := range Params {
		if p.Key == k {
			return p, true
		}
	}
	return ParamDef{}, false
}

// CheckParamValue kiểm giá trị đề xuất của khóa.
func CheckParamValue(def ParamDef, v decimal.Decimal) error {
	if v.Sign() < 0 || v.GreaterThan(def.Max) || !v.Equal(v.Truncate(2)) {
		return Field("value", fmt.Sprintf("0..%s, tối đa 2 chữ số thập phân", def.Max.String()))
	}
	if def.Integer && !v.Equal(v.Truncate(0)) {
		return Field("value", "số nguyên")
	}
	return nil
}

// ParamVersionLabel là nhãn phiên bản ghim vào kết quả: "<key>:v<n>" hoặc "<key>:default".
func ParamVersionLabel(key string, seq int) string {
	if seq <= 0 {
		return key + ":default"
	}
	return fmt.Sprintf("%s:v%d", key, seq)
}

// Code tạo mã chứng từ tiền tố + số thứ tự đệm 0.
func Code(prefix string, n int64, width int) string { return fmt.Sprintf("%s%0*d", prefix, width, n) }
