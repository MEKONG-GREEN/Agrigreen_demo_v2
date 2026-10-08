// Package params là hợp đồng tra tham số đã phê duyệt có hiệu lực tại thời điểm của sự việc (R-DB-09, mục 16).
// pf/masterdata (cap/masterdata — chủ tham số, phiên bản, phê duyệt) hiện thực Params; module ở mọi tầng tra
// qua gói này nên phụ thuộc chỉ đi xuống (R-MOD-05). Khóa tham số do module dùng nó khai (hằng Param*).
package params

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// ParamValue là giá trị của một tham số tại thời điểm tra.
type ParamValue struct {
	Key           string
	Value         decimal.Decimal // tham số số học dùng decimal (R-10)
	Text          string          // tham số chuỗi (vd tên nguồn bản đồ nền)
	Unit          string
	Version       int
	EffectiveFrom time.Time
	SourceDoc     string // số văn bản ban hành (vd văn bản của DCRD)
}

// ErrParamNotFound: không có phiên bản ĐÃ PHÊ DUYỆT có hiệu lực tại thời điểm tra.
var ErrParamNotFound = errors.New("masterdata: không có tham số đã phê duyệt có hiệu lực")

// Params tra tham số đã phê duyệt có hiệu lực tại thời điểm của sự việc.
// Khóa tham số có thể mang chiều, vd "cgh.norm_ha_per_machine:<loại máy>:<khâu>".
type Params interface {
	At(ctx context.Context, key string, at time.Time) (ParamValue, error)
}
