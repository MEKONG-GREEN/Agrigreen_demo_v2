// Package domain: quy tắc nghiệp vụ của mkg/hub — không phụ thuộc hạ tầng,
// không time.Now() (R-12), không float (R-10: khối lượng mass.Kg, tỷ lệ và độ
// ẩm decimal).
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Phiên bản quy tắc ghim vào mỗi dòng sổ (cột rule_version).
const (
	RuleFacility = "hub.facility.v1"
	RuleLot      = "hub.lot.v1"   // tỷ lệ nguồn: dư lớn nhất, 6 chữ số, Σ = 1 chính xác
	RuleBatch    = "hub.batch.v1" // hao hụt = vào − ra ≥ 0
	RuleMovement = "hub.movement.v1"
	RuleValve    = "hub.valve.v1"
)

// FieldError là lỗi dữ liệu vào của một trường.
type FieldError struct {
	Field string
	Msg   string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

func fieldErr(field, msg string) error { return FieldError{Field: field, Msg: msg} }

// Lỗi nghiệp vụ; app/ quy về problem.
var (
	ErrInsufficientStock  = errors.New("hub: không đủ tồn theo lô")
	ErrNegativeLoss       = errors.New("hub: khối lượng ra lớn hơn khối lượng vào")
	ErrLotOtherFacility   = errors.New("hub: lô không thuộc cơ sở của thao tác")
	ErrCorrectionKind     = errors.New("hub: loại dịch chuyển này không sửa bằng bù trực tiếp")
	ErrFacilityInactive   = errors.New("hub: cơ sở đã ngừng dùng")
	ErrDuplicateLotInput  = errors.New("hub: một lô xuất hiện hai lần")
	ErrRatioSum           = errors.New("hub: tổng tỷ lệ nguồn phải đúng bằng 1")
	ErrRatioPrecision     = errors.New("hub: tỷ lệ tối đa 6 chữ số thập phân và phải > 0")
	ErrShareTooSmall      = errors.New("hub: phần đóng góp của một nguồn nhỏ hơn 0.000001")
	ErrFutureEventTime    = errors.New("hub: event_time ở tương lai")
	ErrMixedSourceWeights = errors.New("hub: mọi nguồn phải cùng khai mass_kg hoặc cùng khai ratio")
)

// maxClockSkew là độ lệch đồng hồ thiết bị chấp nhận cho event_time.
const maxClockSkew = 5 * time.Minute

// CheckEventTime: event_time bắt buộc, không ở tương lai quá độ lệch cho phép (R-13).
func CheckEventTime(field string, t, now time.Time) error {
	if t.IsZero() {
		return fieldErr(field, "bắt buộc")
	}
	if t.After(now.Add(maxClockSkew)) {
		return FieldError{Field: field, Msg: ErrFutureEventTime.Error()}
	}
	return nil
}

var (
	codeRe     = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,31}$`)
	lotCodeRe  = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,39}$`)
	provinceRe = regexp.MustCompile(`^[0-9]{2}$`)
)

// ParsePct đọc phần trăm (độ ẩm) dạng decimal 0..100, tối đa 2 chữ số thập phân.
func ParsePct(field, s string) (*decimal.Decimal, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil, fieldErr(field, "phải là số thập phân dạng chuỗi, ví dụ \"14.50\"")
	}
	if d.IsNegative() || d.GreaterThan(decimal.NewFromInt(100)) || !d.Equal(d.Truncate(2)) {
		return nil, fieldErr(field, "phải trong 0..100, tối đa 2 chữ số thập phân")
	}
	return &d, nil
}

// LotCode sinh mã lô mặc định từ id (không dùng bộ đếm COUNT — R-11).
func LotCode(id uuid.UUID) string {
	h := strings.ToUpper(strings.ReplaceAll(id.String(), "-", ""))
	return "LOT-" + h[20:]
}

// ValidLotCode kiểm mã lô do client đặt.
func ValidLotCode(code string) error {
	if !lotCodeRe.MatchString(code) {
		return fieldErr("code", "mã lô phải khớp ^[A-Z0-9][A-Z0-9_-]{1,39}$")
	}
	return nil
}
