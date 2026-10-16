package domain

// Báo cáo giá (W11.77; BRD Báo giá nông sản v1.3 FN-TW-01.01 Bảng giá tổng hợp, FN-TW-01.02 Biến động giá, FN-TW-01.03
// Dashboard vùng, FN-TI-02.01 Tiến độ nhập giá, FN-TI-02.08 Thống kê phiếu; Mục II.4 CT-04). Thuần: tăng / giảm / đi
// ngang, trung bình kỳ, kỳ tháng, đếm phiếu theo cán bộ Xã. Mọi số là decimal (R-CODE-01).

import (
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Hướng biến động so với kỳ trước (CT-04).
const (
	TrendUp   = "up"   // Tăng
	TrendDown = "down" // Giảm
	TrendFlat = "flat" // Đi ngang: |Δ%| < ngưỡng đi ngang
	TrendNone = ""     // kỳ trước không có giá — không so được
)

// Trend so giá trung bình kỳ này cur với kỳ trước prev theo ngưỡng đi ngang flatPct (CT-04, AC-02 FN-TW-01.02:
// ngưỡng 1 %, 10.000 → 10.080 (+0,8 %) Đi ngang; → 10.100 (+1,0 %) Tăng). Trả hướng và Δ% (4 số lẻ); ok = false khi
// kỳ trước không có giá.
func Trend(prev, cur, flatPct decimal.Decimal) (string, decimal.Decimal, bool) {
	pct, ok := ChangePct(cur, prev)
	if !ok {
		return TrendNone, decimal.Zero, false
	}
	switch {
	case Flat(pct, flatPct):
		return TrendFlat, pct, true
	case pct.IsPositive():
		return TrendUp, pct, true
	}
	return TrendDown, pct, true
}

// Mean là trung bình cộng các giá trị (4 số lẻ); rỗng → false. Giá so sánh của kỳ tuần / tháng = trung bình Giá trung
// bình của các ngày công bố trong kỳ (BR-02 FN-TW-01.02).
func Mean(vs []decimal.Decimal) (decimal.Decimal, bool) {
	if len(vs) == 0 {
		return decimal.Zero, false
	}
	sum := decimal.Zero
	for _, v := range vs {
		sum = sum.Add(v)
	}
	return sum.DivRound(decimal.NewFromInt(int64(len(vs))), 4), true
}

// MonthOf trả ngày đầu và ngày cuối của tháng chứa d (00:00 UTC).
func MonthOf(d time.Time) (time.Time, time.Time) {
	from := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, -1)
}

// Kết quả gửi phiếu của một (ngày báo giá, xã) trong thống kê (FN-TI-02.08).
const (
	StatOnTime = "on_time" // phiếu Đã gửi trước giờ khóa
	StatLate   = "late"    // Nộp muộn (Cấp Tỉnh nhập thay — BR-02)
	StatMissed = "missed"  // Không nộp
)

// StatOf xếp phiếu còn hiệu lực của (ngày, xã) đã qua giờ khóa: status = trạng thái sổ ("" = chưa có phiếu).
func StatOf(status string) string {
	switch status {
	case TicketSubmitted:
		return StatOnTime
	case TicketLate:
		return StatLate
	}
	return StatMissed
}

// StatDay là một ngày phải nộp của một xã: ngày báo giá đã qua giờ khóa, xã hoạt động, có cán bộ Xã phụ trách (BR-01).
type StatDay struct {
	Day      time.Time
	Point    string
	Region   string
	Officer  uuid.UUID
	Result   string    // StatOnTime | StatLate | StatMissed
	Ticket   uuid.UUID // phiếu còn hiệu lực (Nil = chưa có)
	Deleted  int       // số phiếu Đã xóa của (ngày, xã)
	Adjusted int       // số dòng Cấp Tỉnh hiệu chỉnh trên phiếu còn hiệu lực
}

// StatRow là một dòng thống kê: một cán bộ Xã tại một xã (cán bộ đổi giữa kỳ → mỗi cán bộ một dòng, theo ngày phụ trách).
type StatRow struct {
	Officer       uuid.UUID
	Point         string
	Region        string
	Required      int
	OnTime        int
	Late          int
	Missed        int
	Deleted       int
	AdjustedLines int
}

// TallyStats gom các ngày phải nộp theo (cán bộ, xã), sắp theo xã rồi mã cán bộ.
func TallyStats(days []StatDay) []StatRow {
	type key struct {
		officer uuid.UUID
		point   string
	}
	by := map[key]*StatRow{}
	for _, d := range days {
		k := key{d.Officer, d.Point}
		r, ok := by[k]
		if !ok {
			r = &StatRow{Officer: d.Officer, Point: d.Point, Region: d.Region}
			by[k] = r
		}
		r.Region = d.Region // nhóm vùng tại ngày gần nhất
		r.Required++
		switch d.Result {
		case StatOnTime:
			r.OnTime++
		case StatLate:
			r.Late++
		default:
			r.Missed++
		}
		r.Deleted += d.Deleted
		r.AdjustedLines += d.Adjusted
	}
	out := make([]StatRow, 0, len(by))
	for _, r := range by {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Point != out[j].Point {
			return out[i].Point < out[j].Point
		}
		return out[i].Officer.String() < out[j].Officer.String()
	})
	return out
}

// Trạng thái khóa tháng của thống kê phiếu (Q-085, pf 20261013164235).
const (
	StatMonthClose   = "close"
	StatMonthReopen  = "reopen"
	StatMonthClosed  = "closed"
	StatMonthOpen    = "open"
	StatMonthPending = "" // chưa khóa lần nào
)

// StatMonth là phiên bản hiện hành của khóa tháng.
type StatMonth struct {
	Seq       int64
	Status    string
	Version   int
	Reason    string
	Rows      int
	Actor     uuid.UUID
	EventTime time.Time
}

// Closed cho biết tháng đang khóa (đọc ảnh chụp).
func (m StatMonth) Closed() bool { return m.Seq > 0 && m.Status == StatMonthClosed }
