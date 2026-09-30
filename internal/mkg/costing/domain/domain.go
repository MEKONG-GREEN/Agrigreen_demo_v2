// Package domain: quy tắc thuần của mkg/costing — kỳ tháng, tham số có phiên bản, phân bổ theo
// khối lượng, quy kết hao hụt theo tỷ lệ nguồn, quy đổi cuộn → tấn, quyết toán HTX, chia doanh
// thu carbon, ảnh chụp đầu vào + tổng hợp đóng kỳ tất định. Không I/O, không time.Now (R-12),
// không float (R-10, agnofloat): tiền shared/money, khối lượng shared/mass, tỷ lệ decimal.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Phiên bản quy tắc ghim vào mọi kết quả (mục 16: tái hiện chi phí).
const (
	RuleParams        = "costing.params.v1"
	RuleInput         = "costing.input.v1"
	RuleHubProcessing = "costing.hub_processing.v1" // kg/1000 × đơn giá #55, làm tròn nửa lên 2 số
	RuleTripCost      = "costing.trip_alloc.v1"     // cước thực tế chia theo khóa phân bổ, dư làm tròn vào dòng cuối
	RuleDelivery      = "costing.delivery_alloc.v1" // doanh thu giao nhận chia theo khối lượng lô
	RuleLoss          = "costing.loss_attr.v1"      // hao hụt bất thường × tỷ lệ nguồn (Σ = 1), dư vào dòng cuối
	RuleConversion    = "costing.conversion.v1"     // yield = kg(HUB_IN) / cuộn, 3 số; ẩm: không quy đổi (C-4)
	RuleManual        = "costing.manual.v1"
	RuleAdjusting     = "costing.adjusting.v1"
	RuleSettlement    = "costing.settlement.v1" // proto PM-01..04
	RuleCarbonSplit   = "costing.carbon_split.v1"
	RulePeriod        = "costing.period.v1"
	RulePeriodClose   = "costing.period_close.v1"
)

// ProvisionalLabel đánh dấu mọi mặc định chưa được khách chốt.
const ProvisionalLabel = "tạm, chờ khách"

// SystemActorRef là actor_ref của tác nhân hệ thống (consumer sự kiện nội bộ, job) — không phải người.
var SystemActorRef = uuid.MustParse("00000000-0000-0000-0000-0000000c0571")

// FieldError là lỗi dữ liệu vào theo trường.
type FieldError struct{ Field, Msg string }

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

func fieldErr(f, m string) error { return FieldError{Field: f, Msg: m} }

// ErrInvalidPeriodCode: mã kỳ không đúng YYYY-MM.
var ErrInvalidPeriodCode = errors.New("costing: mã kỳ phải là YYYY-MM")

// PeriodCode trả mã kỳ tháng (giờ Việt Nam) của một thời điểm.
func PeriodCode(t time.Time) string { return t.In(clock.Zone()).Format("2006-01") }

// PeriodBounds trả [from, to) theo UTC của kỳ tháng.
func PeriodBounds(code string) (from, to time.Time, err error) {
	t, err := time.ParseInLocation("2006-01", code, clock.Zone())
	if err != nil || t.Format("2006-01") != code {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %q", ErrInvalidPeriodCode, code)
	}
	return t.UTC(), t.AddDate(0, 1, 0).UTC(), nil
}

// NextPeriodCode trả mã kỳ tháng kế tiếp.
func NextPeriodCode(code string) (string, error) {
	_, to, err := PeriodBounds(code)
	if err != nil {
		return "", err
	}
	return PeriodCode(to), nil
}

// BusinessDate trả ngày nghiệp vụ (giờ Việt Nam) dạng YYYY-MM-DD.
func BusinessDate(t time.Time) string { return t.In(clock.Zone()).Format("2006-01-02") }
