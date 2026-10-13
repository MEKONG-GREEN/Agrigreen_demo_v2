package domain

// Phiếu báo giá + xác minh (W11.75, pf 000148; BRD Báo giá nông sản v1.3 FN-XA-03.01…03, 03.05, FN-TI-02.02…04, 02.09;
// Mục II.3 luồng trạng thái, II.4 CT-01…CT-03): quy tắc thuần — kiểm giá một dòng, giá xã P, trạng thái phiếu và cảnh
// báo nhìn tại một thời điểm (giờ khóa, hạn phản hồi), đánh giá cảnh báo của một dòng. Không I/O, không đồng hồ, không
// float (R-CODE-01).

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// Trạng thái phiếu ghi ở sổ (pf_price.ticket.status).
const (
	TicketDraft     = "draft"
	TicketSubmitted = "submitted"
	TicketLate      = "late"
	TicketDeleted   = "deleted"
)

// Nguồn phiếu.
const (
	SourceXa    = "xa"
	SourceProxy = "proxy"
)

// Thao tác ghi phiếu (pf_price.ticket.op).
const (
	OpSave        = "save"
	OpSubmit      = "submit"
	OpProxySave   = "proxy_save"
	OpProxySubmit = "proxy_submit"
	OpCorrect     = "correct"
	OpRedate      = "redate"
	OpDelete      = "delete"
)

// Trạng thái phiếu NHÌN tại một thời điểm (Mục II.3.1) — chiếu từ trạng thái sổ + giờ khóa + bảng giá ngày.
const (
	ViewNotEntered = "not_entered" // Chưa nhập
	ViewDraft      = "draft"       // Bản nháp
	ViewSubmitted  = "submitted"   // Đã gửi (còn sửa, gửi lại được trước giờ khóa)
	ViewLocked     = "locked"      // Đã khóa
	ViewMissed     = "missed"      // Không nộp (không gửi trước giờ khóa; bản nháp tính là Không nộp — BR-06)
	ViewLate       = "late"        // Nộp muộn (Cấp Tỉnh nhập thay)
	ViewApproved   = "approved"    // Đã duyệt (thuộc bảng giá đã duyệt — W11.76)
	ViewDeleted    = "deleted"     // Đã xóa
)

// Trạng thái bảng giá ngày (W11.76 — Mục II.3.3). Khung W11.75 chưa có bảng giá: mọi ngày BoardOpen (Chưa duyệt).
const (
	BoardOpen      = "open"      // Chưa duyệt
	BoardApproving = "approving" // Đang duyệt (một cán bộ đang thao tác)
	BoardApproved  = "approved"  // Đã duyệt (vN)
	BoardReopened  = "reopened"  // Đang mở duyệt
)

// BoardEditable cho biết Cấp Tỉnh còn sửa số liệu ngày đó được: Chưa duyệt hoặc Đang mở duyệt (FN-TI-02.04, 02.09).
func BoardEditable(b string) bool { return b == "" || b == BoardOpen || b == BoardReopened }

// TicketView chiếu trạng thái phiếu tại thời điểm đọc: status = trạng thái sổ ("" = chưa có phiếu), locked = đã qua giờ
// khóa của ngày, board = trạng thái bảng giá ngày.
func TicketView(status string, locked bool, board string) string {
	approved := board == BoardApproved
	switch status {
	case TicketDeleted:
		return ViewDeleted
	case TicketLate:
		if approved {
			return ViewApproved
		}
		return ViewLate
	case TicketSubmitted:
		switch {
		case !locked:
			return ViewSubmitted
		case approved:
			return ViewApproved
		}
		return ViewLocked
	case TicketDraft:
		if locked {
			return ViewMissed
		}
		return ViewDraft
	}
	if locked {
		return ViewMissed
	}
	return ViewNotEntered
}

// Live cho biết phiếu còn hiệu lực (BR-03: mỗi xã một phiếu cho một ngày, không tính phiếu Đã xóa).
func Live(status string) bool { return status != "" && status != TicketDeleted }

// Sent cho biết phiếu đã gửi chính thức (Đã gửi / Nộp muộn) — được đưa vào kiểm duyệt, bảng giá (BR-06 FN-XA-03.01).
func Sent(status string) bool { return status == TicketSubmitted || status == TicketLate }

// Trạng thái cảnh báo ghi ở sổ (pf_price.price_alert.status, Mục II.3.2).
const (
	AlertProvisional = "provisional" // Tạm tính
	AlertCancelled   = "cancelled"   // tự hủy
	AlertRequested   = "requested"   // Đã yêu cầu xác minh
	AlertResponded   = "responded"   // Xã đã phản hồi
	AlertAdjusted    = "adjusted"    // Đã hiệu chỉnh
	AlertKept        = "kept"        // Giữ nguyên
	AlertExcluded    = "excluded"    // Đã loại
	AlertReopened    = "reopened"    // mở lại khi bảng giá đang mở duyệt (đọc là Chờ rà soát)
)

// Trạng thái cảnh báo NHÌN tại một thời điểm (thêm hai trạng thái chiếu).
const (
	AlertViewPending = "pending" // Chờ rà soát: Tạm tính sau giờ khóa, hoặc đã mở lại
	AlertViewOverdue = "overdue" // Quá hạn phản hồi: đã yêu cầu xác minh, hết hạn, Xã chưa phản hồi
)

// AlertView chiếu trạng thái cảnh báo tại now.
func AlertView(status string, locked bool, respondBy, now time.Time) string {
	switch status {
	case AlertProvisional:
		if locked {
			return AlertViewPending
		}
	case AlertReopened:
		return AlertViewPending
	case AlertRequested:
		if !respondBy.IsZero() && !now.Before(respondBy) {
			return AlertViewOverdue
		}
	}
	return status
}

// AlertResolved: chỉ Đã hiệu chỉnh, Giữ nguyên, Đã loại tính là đã xử lý (BR-02 FN-TI-02.03); tự hủy không còn là cảnh báo.
func AlertResolved(status string) bool {
	return status == AlertAdjusted || status == AlertKept || status == AlertExcluded || status == AlertCancelled
}

// AlertOpenBeforeLock: cảnh báo còn mở trước giờ khóa — Xã gửi lại về dưới ngưỡng thì tự hủy (Mục II.3.2).
func AlertOpenBeforeLock(status string) bool {
	return status == AlertProvisional || status == AlertRequested || status == AlertResponded
}

// Lựa chọn phản hồi của Xã (FN-XA-03.05 B3).
const (
	ResponseKeep    = "keep"    // Giữ giá đã gửi
	ResponsePropose = "propose" // Đề xuất giá đúng
)

// Quyết định của Cấp Tỉnh trên một cảnh báo (FN-TI-02.03 B6).
const (
	DecisionKeep    = "keep"    // Giữ nguyên (ghi chú bắt buộc)
	DecisionExclude = "exclude" // Loại khỏi bảng giá (lý do bắt buộc)
	DecisionReopen  = "reopen"  // Mở lại cảnh báo đã xử lý (chỉ khi bảng giá đang mở duyệt)
)

// Giới hạn chữ.
const (
	MaxNote    = 500
	MaxMessage = 1000
)

// ErrPrice: giá / dòng giá không hợp lệ (app đổi sang price.price_invalid).
var ErrPrice = errors.New("giá không hợp lệ")

// MaxPrice là giá lớn nhất ghi được (numeric(14,2)).
var MaxPrice = decimal.RequireFromString("999999999999.99")

var two = decimal.NewFromInt(2)

// Line là giá một mặt hàng trên phiếu (giá cán bộ Xã gửi hoặc nhập thay).
type Line struct {
	ItemCode string
	Min, Max *decimal.Decimal // nil = chưa nhập
	NoGoods  bool
	Note     string
}

// Filled cho biết dòng đã cập nhật (có giá hoặc "Không có hàng") — BR-03 FN-XA-03.01.
func (l Line) Filled() bool { return l.NoGoods || l.Min != nil }

// Priced cho biết dòng có giá.
func (l Line) Priced() bool { return !l.NoGoods && l.Min != nil && l.Max != nil }

// Same so sánh nội dung hai dòng (không ghi phiên bản mới khi không đổi).
func (l Line) Same(o Line) bool {
	return l.NoGoods == o.NoGoods && strings.TrimSpace(l.Note) == strings.TrimSpace(o.Note) && eqPtr(l.Min, o.Min) && eqPtr(l.Max, o.Max)
}

func eqPtr(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// ValidPair kiểm một cặp giá: số dương, tối đa 2 số lẻ, thấp nhất ≤ cao nhất (BR-02 FN-XA-03.01; luồng ngoại lệ
// FN-TI-02.04, FN-XA-03.05).
func ValidPair(lo, hi decimal.Decimal) error {
	if !lo.IsPositive() || !hi.IsPositive() {
		return fmt.Errorf("%w: giá phải là số dương", ErrPrice)
	}
	if !lo.Equal(lo.Round(2)) || !hi.Equal(hi.Round(2)) {
		return fmt.Errorf("%w: giá tối đa 2 chữ số thập phân", ErrPrice)
	}
	if lo.GreaterThan(MaxPrice) || hi.GreaterThan(MaxPrice) {
		return fmt.Errorf("%w: giá quá lớn", ErrPrice)
	}
	if lo.GreaterThan(hi) {
		return fmt.Errorf("%w: giá thấp nhất lớn hơn giá cao nhất", ErrPrice)
	}
	return nil
}

// ValidateLine kiểm một dòng (cùng ràng buộc CHECK của pf 000148).
func ValidateLine(l Line) error {
	if !ValidItemCode(l.ItemCode) {
		return fmt.Errorf("%w: mã mặt hàng %q", ErrPrice, l.ItemCode)
	}
	if (l.Min == nil) != (l.Max == nil) {
		return fmt.Errorf("%w: %s nhập đủ giá thấp nhất và cao nhất (một mức giá thì nhập hai ô bằng nhau)", ErrPrice, l.ItemCode)
	}
	if l.NoGoods && l.Min != nil {
		return fmt.Errorf("%w: %s đã đánh dấu Không có hàng thì không nhập giá", ErrPrice, l.ItemCode)
	}
	if l.Min != nil {
		if err := ValidPair(*l.Min, *l.Max); err != nil {
			return fmt.Errorf("%s: %w", l.ItemCode, err)
		}
	}
	if utf8.RuneCountInString(strings.TrimSpace(l.Note)) > MaxNote {
		return fmt.Errorf("%w: %s ghi chú tối đa %d ký tự", ErrPrice, l.ItemCode, MaxNote)
	}
	return nil
}

// Mid trả giá xã P = (thấp nhất + cao nhất) / 2 (CT-01), 4 số lẻ.
func Mid(lo, hi decimal.Decimal) decimal.Decimal { return lo.Add(hi).DivRound(two, 4) }

// Effective trả giá dùng cho bảng giá / giá tham chiếu của một dòng: giá hiệu chỉnh nếu có, không thì giá Xã gửi
// (BR-05 FN-TI-02.04). ok = false: dòng chưa có giá hoặc Không có hàng.
func Effective(l Line, adjMin, adjMax *decimal.Decimal) (lo, hi decimal.Decimal, ok bool) {
	if l.NoGoods {
		return decimal.Zero, decimal.Zero, false
	}
	if adjMin != nil && adjMax != nil {
		return *adjMin, *adjMax, true
	}
	if l.Min == nil || l.Max == nil {
		return decimal.Zero, decimal.Zero, false
	}
	return *l.Min, *l.Max, true
}

// Evaluation là kết quả so giá kỳ này với giá tham chiếu (CT-02, CT-03).
type Evaluation struct {
	Price     decimal.Decimal // P
	Ref       decimal.Decimal // P_ref (zero = "Giá mới")
	ChangePct decimal.Decimal
	Threshold decimal.Decimal
	Direction string // AlertHigh | AlertLow | AlertNone
	HasRef    bool
}

// Evaluate so một dòng có giá với giá tham chiếu ref (zero/âm = không có — "Giá mới", không cảnh báo) theo ngưỡng T.
func Evaluate(lo, hi, ref, threshold decimal.Decimal) Evaluation {
	e := Evaluation{Price: Mid(lo, hi), Ref: ref, Threshold: threshold}
	pct, ok := ChangePct(e.Price, ref)
	if !ok {
		return e
	}
	e.HasRef, e.ChangePct = true, pct
	e.Direction = Classify(pct, threshold)
	return e
}

// TextRequired kiểm một nội dung bắt buộc (lý do, ghi chú, giải trình): khác rỗng, tối đa max ký tự.
func TextRequired(s string, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= 1 && n <= max
}

// DefaultRequestMessage là nội dung mặc định của yêu cầu xác minh (FN-TI-02.03 B3).
func DefaultRequestMessage(item, point string, pct decimal.Decimal) string {
	return fmt.Sprintf("Giá %s tại %s chênh %s%% so với giá tham chiếu, đề nghị xã kiểm tra lại", item, point, pct.Round(1).String())
}
