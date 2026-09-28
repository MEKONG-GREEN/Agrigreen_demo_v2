// Package domain: mẫu biểu, tập dữ liệu báo cáo, định dạng số kiểu Việt Nam,
// kỳ báo cáo và mốc lịch gửi. Không phụ thuộc hạ tầng; không time.Now() (R-12).
package domain

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// FormatVN định dạng số thập phân theo quy ước Việt Nam: dấu "." ngăn nghìn,
// dấu "," thập phân, làm tròn nửa lên xa số 0 tới scale chữ số. Tính trên
// decimal — không qua float, nên không có sai số biểu diễn (vd 0,1 + 0,2 = 0,30).
// scale < 0: giữ nguyên số chữ số thập phân đã lưu (bỏ số 0 thừa).
func FormatVN(d decimal.Decimal, scale int32) string {
	var s string
	if scale < 0 {
		s = d.String()
	} else {
		s = d.StringFixed(scale)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	if neg && strings.Trim(s, "0.") != "" {
		b.WriteByte('-')
	}
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if hasFrac && frac != "" {
		b.WriteByte(',')
		b.WriteString(frac)
	}
	return b.String()
}

// FormatIntVN định dạng số nguyên với dấu "." ngăn nghìn.
func FormatIntVN(n int64) string { return FormatVN(decimal.NewFromInt(n), 0) }

// FormatDateTimeVN là "dd/mm/yyyy HH:MM" theo giờ Việt Nam (dòng "Ngày xuất").
func FormatDateTimeVN(t time.Time) string { return t.In(clock.Zone()).Format("02/01/2006 15:04") }

// FormatDateVN là "dd/mm/yyyy" theo giờ Việt Nam.
func FormatDateVN(t time.Time) string { return t.In(clock.Zone()).Format("02/01/2006") }
